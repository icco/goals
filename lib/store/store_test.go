package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go.icco.me/goals/lib/dbtest"
)

var now = time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.Context(), dbtest.New(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func newUser(t *testing.T, s *Store, phone string) *User {
	t.Helper()
	u, err := s.EnsureUser(t.Context(), phone, true, now)
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	return u
}

func addGoal(t *testing.T, s *Store, userID uint, text string) *Goal {
	t.Helper()
	g, err := s.AddGoal(t.Context(), userID, text)
	if err != nil {
		t.Fatalf("AddGoal(%q): %v", text, err)
	}
	return g
}

func TestNormalizePhone(t *testing.T) {
	for in, want := range map[string]string{
		"(555) 555-0100":  "+15555550100",
		"5555550100":      "+15555550100",
		"1-555-555-0100":  "+15555550100",
		"+1 555 555 0100": "+15555550100",
	} {
		got, err := NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "+44 20 7946 0000", "555", "555-555-01000", "abc"} {
		if _, err := NormalizePhone(in); !errors.Is(err, ErrInvalidPhone) {
			t.Errorf("NormalizePhone(%q) err = %v, want ErrInvalidPhone", in, err)
		}
	}
}

func TestLocalDate(t *testing.T) {
	got, err := LocalDate(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), "Pacific/Honolulu")
	if err != nil || got != "2026-11-12" {
		t.Errorf("LocalDate = %q, %v; want 2026-11-12", got, err)
	}
	if _, err := LocalDate(now, "Mars/Olympus"); err == nil {
		t.Error("LocalDate with bad tz: want error")
	}
}

func TestEnsureUserRequiresConsent(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	if _, err := s.EnsureUser(ctx, "+15555550100", false, now); !errors.Is(err, ErrNoConsent) {
		t.Fatalf("err = %v, want ErrNoConsent", err)
	}
	if _, err := s.UserByPhone(ctx, "+15555550100"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UserByPhone err = %v, want ErrNotFound", err)
	}
	u := newUser(t, s, "+15555550100")
	if u.Timezone != DefaultTimezone || u.RemindMinute != DefaultRemindMinute || !u.ConsentedAt.Equal(now) {
		t.Errorf("defaults = %+v", u)
	}
	again, err := s.EnsureUser(ctx, "+15555550100", false, now.Add(time.Hour))
	if err != nil || again.ID != u.ID {
		t.Fatalf("existing user without consent: %v, %v", again, err)
	}
	byID, err := s.UserByID(ctx, u.ID)
	if err != nil || byID.Phone != u.Phone {
		t.Fatalf("UserByID: %v, %v", byID, err)
	}
}

func TestUpdateSettings(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u := newUser(t, s, "+15555550100")
	if err := s.UpdateSettings(ctx, u.ID, "Pacific/Honolulu", 7*60, true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.UserByID(ctx, u.ID)
	if got.Timezone != "Pacific/Honolulu" || got.RemindMinute != 420 || !got.Paused {
		t.Errorf("settings = %+v", got)
	}
	if err := s.UpdateSettings(ctx, u.ID, "Nowhere/Land", 0, false); !errors.Is(err, ErrInvalidSettings) {
		t.Errorf("bad tz err = %v", err)
	}
	if err := s.UpdateSettings(ctx, u.ID, "UTC", 1440, false); !errors.Is(err, ErrInvalidSettings) {
		t.Errorf("bad minute err = %v", err)
	}
	if err := s.SetOptedOut(ctx, u.Phone, true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.UserByID(ctx, u.ID)
	if !got.OptedOut {
		t.Error("OptedOut not set")
	}
	if err := s.SetOptedOut(ctx, "+15555550199", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown phone err = %v", err)
	}
}

func TestAddGoalLimits(t *testing.T) {
	s := newStore(t)
	u := newUser(t, s, "+15555550100")
	for _, bad := range []string{"  ", strings.Repeat("x", 141)} {
		if _, err := s.AddGoal(t.Context(), u.ID, bad); !errors.Is(err, ErrInvalidGoal) {
			t.Errorf("AddGoal(%q) err = %v", bad, err)
		}
	}
	addGoal(t, s, u.ID, strings.Repeat("é", 140))
	for i := 1; i < MaxGoals; i++ {
		addGoal(t, s, u.ID, "goal")
	}
	if _, err := s.AddGoal(t.Context(), u.ID, "one too many"); !errors.Is(err, ErrTooManyGoals) {
		t.Errorf("11th goal err = %v", err)
	}
}

func TestGoalEditing(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u := newUser(t, s, "+15555550100")
	other := newUser(t, s, "+15555550101")
	a := addGoal(t, s, u.ID, "read")
	b := addGoal(t, s, u.ID, "run")
	c := addGoal(t, s, u.ID, "write")

	if err := s.MoveGoal(ctx, u.ID, c.ID, -1); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveGoal(ctx, u.ID, a.ID, -1); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, s, u.ID, "read", "write", "run")

	if err := s.UpdateGoal(ctx, u.ID, b.ID, "  jog "); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateGoal(ctx, other.ID, b.ID, "stolen"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user's goal err = %v", err)
	}
	if err := s.UpdateGoal(ctx, u.ID, b.ID, ""); !errors.Is(err, ErrInvalidGoal) {
		t.Errorf("empty text err = %v", err)
	}
	if err := s.ArchiveGoal(ctx, u.ID, a.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveGoal(ctx, other.ID, b.ID, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("archive other's err = %v", err)
	}
	if err := s.MoveGoal(ctx, other.ID, b.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("move other's err = %v", err)
	}
	assertOrder(t, s, u.ID, "write", "jog")
}

func assertOrder(t *testing.T, s *Store, userID uint, want ...string) {
	t.Helper()
	goals, err := s.ActiveGoals(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, g := range goals {
		got = append(got, g.Text)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestGoalsByIDKeepsOrderAndArchived(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u := newUser(t, s, "+15555550100")
	other := newUser(t, s, "+15555550101")
	a := addGoal(t, s, u.ID, "read")
	b := addGoal(t, s, u.ID, "run")
	x := addGoal(t, s, other.ID, "theirs")
	if err := s.ArchiveGoal(ctx, u.ID, a.ID, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.GoalsByID(ctx, u.ID, []uint{b.ID, x.ID, a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("GoalsByID = %+v", got)
	}
}

func TestSetCheckInUpsertAndOwnership(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u := newUser(t, s, "+15555550100")
	other := newUser(t, s, "+15555550101")
	g := addGoal(t, s, u.ID, "read")
	if err := s.SetCheckIn(ctx, u.ID, g.ID, "2026-11-12", true, SourceSMS); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckIn(ctx, u.ID, g.ID, "2026-11-12", false, SourceWeb); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckIn(ctx, u.ID, g.ID, "2026-11-10", true, SourceWeb); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckIn(ctx, other.ID, g.ID, "2026-11-12", true, SourceWeb); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user's goal err = %v", err)
	}
	got, err := s.CheckIns(ctx, u.ID, "2026-11-11", "2026-11-12")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got[g.ID]["2026-11-12"]; !ok || v {
		t.Errorf("check-in = %v, %v; want false, true", v, ok)
	}
	if _, ok := got[g.ID]["2026-11-10"]; ok {
		t.Error("range not inclusive-bounded")
	}
}

func TestDueUsersFilters(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	due := newUser(t, s, "+15555550100")
	addGoal(t, s, due.ID, "read")
	paused := newUser(t, s, "+15555550101")
	addGoal(t, s, paused.ID, "read")
	if err := s.UpdateSettings(ctx, paused.ID, DefaultTimezone, DefaultRemindMinute, true); err != nil {
		t.Fatal(err)
	}
	stopped := newUser(t, s, "+15555550102")
	addGoal(t, s, stopped.ID, "read")
	if err := s.SetOptedOut(ctx, stopped.Phone, true); err != nil {
		t.Fatal(err)
	}
	newUser(t, s, "+15555550103")
	archived := newUser(t, s, "+15555550104")
	g := addGoal(t, s, archived.ID, "read")
	if err := s.ArchiveGoal(ctx, archived.ID, g.ID, now); err != nil {
		t.Fatal(err)
	}
	users, err := s.DueUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != due.ID {
		t.Errorf("DueUsers = %+v", users)
	}
}

func TestClaimReminderOnce(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u := newUser(t, s, "+15555550100")
	if _, err := s.LatestReminder(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LatestReminder err = %v", err)
	}
	r, ok, err := s.ClaimReminder(ctx, u.ID, "2026-11-12", []uint{3, 1}, now)
	if err != nil || !ok {
		t.Fatalf("first claim: %v, %v", ok, err)
	}
	if _, ok, err := s.ClaimReminder(ctx, u.ID, "2026-11-12", []uint{3}, now); err != nil || ok {
		t.Fatalf("second claim: %v, %v", ok, err)
	}
	if err := s.SetReminderSID(ctx, r.ID, "SM123"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimReminder(ctx, u.ID, "2026-11-11", []uint{1}, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	latest, err := s.LatestReminder(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Date != "2026-11-12" || latest.MessageSID != "SM123" || len(latest.GoalIDs) != 2 || latest.GoalIDs[0] != 3 {
		t.Errorf("latest = %+v", latest)
	}
}

func TestSessionExpiry(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u := newUser(t, s, "+15555550100")
	tok, err := s.CreateSession(ctx, u.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionUser(ctx, tok, now.Add(29*24*time.Hour))
	if err != nil || got.ID != u.ID {
		t.Fatalf("SessionUser: %v, %v", got, err)
	}
	if _, err := s.SessionUser(ctx, tok, now.Add(31*24*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired err = %v", err)
	}
	if _, err := s.SessionUser(ctx, "nope", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown err = %v", err)
	}
	if err := s.DeleteSession(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, tok, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted err = %v", err)
	}
}

func TestDeleteUserCascades(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u := newUser(t, s, "+15555550100")
	keep := newUser(t, s, "+15555550101")
	g := addGoal(t, s, u.ID, "read")
	kg := addGoal(t, s, keep.ID, "read")
	for _, c := range []struct{ user, goal uint }{{u.ID, g.ID}, {keep.ID, kg.ID}} {
		if err := s.SetCheckIn(ctx, c.user, c.goal, "2026-11-12", true, SourceWeb); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.ClaimReminder(ctx, u.ID, "2026-11-12", []uint{g.ID}, now); err != nil {
		t.Fatal(err)
	}
	tok, err := s.CreateSession(ctx, u.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	for _, m := range []any{&User{}, &Goal{}, &CheckIn{}, &Reminder{}, &Session{}} {
		var n int64
		if err := s.db.Model(m).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		want := int64(1)
		if _, isRem := m.(*Reminder); isRem {
			want = 0
		}
		if _, isSess := m.(*Session); isSess {
			want = 0
		}
		if n != want {
			t.Errorf("%T rows = %d, want %d", m, n, want)
		}
	}
	if _, err := s.SessionUser(ctx, tok, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("session survived: %v", err)
	}
}

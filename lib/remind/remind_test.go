package remind

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.icco.me/goals/lib/dbtest"
	"go.icco.me/goals/lib/sms"
	"go.icco.me/goals/lib/store"
)

func setup(t *testing.T, tz string) (*Scheduler, *sms.Fake, *store.Store, *store.User, *time.Time) {
	t.Helper()
	ctx := t.Context()
	st, err := store.New(ctx, dbtest.New(t))
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.EnsureUser(ctx, "+15555550100", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSettings(ctx, u.ID, tz, 20*60, false); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"read 20 pages", "run"} {
		if _, err := st.AddGoal(ctx, u.ID, g); err != nil {
			t.Fatal(err)
		}
	}
	fake := &sms.Fake{}
	now := new(time.Time)
	return &Scheduler{Store: st, Sender: fake, Now: func() time.Time { return *now }}, fake, st, u, now
}

func TestMessage(t *testing.T) {
	got := Message([]store.Goal{{Text: "read 20 pages"}, {Text: "run"}})
	want := "goals: did you do these today?\n1. read 20 pages\n2. run\nReply y, n, or the numbers you did (e.g. \"1 3\")."
	if got != want {
		t.Errorf("Message =\n%s\nwant\n%s", got, want)
	}
}

func TestTickSendsOnceAtLocalTime(t *testing.T) {
	s, fake, st, u, now := setup(t, "America/Los_Angeles")
	ctx := t.Context()
	*now = time.Date(2026, 11, 13, 3, 59, 0, 0, time.UTC)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.Messages()); n != 0 {
		t.Fatalf("sent %d before reminder time", n)
	}
	*now = time.Date(2026, 11, 13, 4, 1, 0, 0, time.UTC)
	for range 2 {
		if err := s.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	msgs := fake.Messages()
	if len(msgs) != 1 || msgs[0].To != u.Phone || msgs[0].Body != Message(mustGoals(t, st, u.ID)) {
		t.Fatalf("messages = %+v", msgs)
	}
	r, err := st.LatestReminder(ctx, u.ID)
	if err != nil || r.Date != "2026-11-12" || r.MessageSID != "SM1" || len(r.GoalIDs) != 2 {
		t.Fatalf("reminder = %+v, %v", r, err)
	}
	*now = time.Date(2026, 11, 14, 4, 1, 0, 0, time.UTC)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.Messages()); n != 2 {
		t.Fatalf("next day sent %d total, want 2", n)
	}
}

func mustGoals(t *testing.T, st *store.Store, userID uint) []store.Goal {
	t.Helper()
	g, err := st.ActiveGoals(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestTickHonolulu(t *testing.T) {
	s, fake, st, u, now := setup(t, "Pacific/Honolulu")
	*now = time.Date(2026, 11, 13, 6, 30, 0, 0, time.UTC)
	if err := s.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, err := st.LatestReminder(t.Context(), u.ID)
	if err != nil || r.Date != "2026-11-12" || len(fake.Messages()) != 1 {
		t.Fatalf("reminder = %+v, %v, sent %d", r, err, len(fake.Messages()))
	}
}

func TestTickSendErrorKeepsClaim(t *testing.T) {
	s, fake, st, u, now := setup(t, "UTC")
	fake.FailSend = errors.New("twilio down")
	*now = time.Date(2026, 11, 12, 21, 0, 0, 0, time.UTC)
	if err := s.Tick(t.Context()); err == nil {
		t.Fatal("Tick swallowed the send error")
	}
	if _, err := st.LatestReminder(t.Context(), u.ID); err != nil {
		t.Fatalf("claim not kept: %v", err)
	}
	fake.FailSend = nil
	if err := s.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.Messages()); n != 0 {
		t.Fatalf("resent %d after failure", n)
	}
}

func TestRunStopsWithContext(t *testing.T) {
	s, fake, _, _, now := setup(t, "UTC")
	*now = time.Date(2026, 11, 12, 21, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		s.Run(ctx, time.Millisecond)
		close(done)
	}()
	deadline := time.After(5 * time.Second)
	for len(fake.Messages()) == 0 {
		select {
		case <-deadline:
			t.Fatal("Run never ticked")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
}

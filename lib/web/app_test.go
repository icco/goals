package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"go.icco.me/goals/lib/store"
)

func TestDashboardShowsStreaks(t *testing.T) {
	h := newHarness(t)
	u, c := h.login(phone)
	ctx := t.Context()
	expect(t, h.do(http.MethodGet, "/app", nil, c), http.StatusOK, `href="/app/goals"`)
	g, err := h.store.AddGoal(ctx, u.ID, "read <20> pages")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-11-09", "2026-11-10", "2026-11-11"} {
		if err := h.store.SetCheckIn(ctx, u.ID, g.ID, d, true, store.SourceWeb); err != nil {
			t.Fatal(err)
		}
	}
	rec := h.do(http.MethodGet, "/app", nil, c)
	expect(t, rec, http.StatusOK, "read &lt;20&gt; pages", "Thursday, November 12", "3 day streak", "best 3")
	if strings.Contains(rec.Body.String(), "<20>") {
		t.Error("goal text not escaped")
	}
}

func TestCheckInWindow(t *testing.T) {
	h := newHarness(t)
	u, c := h.login(phone)
	g, err := h.store.AddGoal(t.Context(), u.ID, "read")
	if err != nil {
		t.Fatal(err)
	}
	post := func(date, done string) int {
		return h.do(http.MethodPost, "/app/checkin", url.Values{"goal": {fmt.Sprint(g.ID)}, "date": {date}, "done": {done}}, c).Code
	}
	for date, want := range map[string]int{
		"2026-11-05": http.StatusBadRequest,
		"2026-11-06": http.StatusSeeOther,
		"2026-11-12": http.StatusSeeOther,
		"2026-11-13": http.StatusBadRequest,
		"tomorrow":   http.StatusBadRequest,
	} {
		if got := post(date, "true"); got != want {
			t.Errorf("check in %s: status %d, want %d", date, got, want)
		}
	}
	if got := post("2026-11-12", "false"); got != http.StatusSeeOther {
		t.Fatalf("undo: %d", got)
	}
	got, err := h.store.CheckIns(t.Context(), u.ID, "2026-11-01", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	if done, ok := got[g.ID]["2026-11-12"]; !ok || done {
		t.Errorf("2026-11-12 = %v, %v", done, ok)
	}
	if _, ok := got[g.ID]["2026-11-05"]; ok {
		t.Error("check-in outside window saved")
	}
}

func TestCheckInOtherUsersGoal(t *testing.T) {
	h := newHarness(t)
	other, _ := h.login("+15555550101")
	g, err := h.store.AddGoal(t.Context(), other.ID, "theirs")
	if err != nil {
		t.Fatal(err)
	}
	_, c := h.login(phone)
	rec := h.do(http.MethodPost, "/app/checkin", url.Values{"goal": {fmt.Sprint(g.ID)}, "date": {"2026-11-12"}, "done": {"true"}}, c)
	expect(t, rec, http.StatusNotFound)
}

func TestGoalCRUD(t *testing.T) {
	h := newHarness(t)
	u, c := h.login(phone)
	expect(t, h.do(http.MethodGet, "/app/goals", nil, c), http.StatusOK)
	for _, text := range []string{"read", "run", "write"} {
		expect(t, h.do(http.MethodPost, "/app/goals", url.Values{"text": {text}}, c), http.StatusSeeOther)
	}
	expect(t, h.do(http.MethodPost, "/app/goals", url.Values{"text": {"  "}}, c), http.StatusBadRequest, "1 to 140 characters")
	goals, _ := h.store.ActiveGoals(t.Context(), u.ID)
	id := func(i int) string { return fmt.Sprint(goals[i].ID) }

	expect(t, h.do(http.MethodPost, "/app/goals/"+id(1), url.Values{"text": {"jog"}}, c), http.StatusSeeOther)
	expect(t, h.do(http.MethodPost, "/app/goals/"+id(2)+"/move", url.Values{"delta": {"-1"}}, c), http.StatusSeeOther)
	expect(t, h.do(http.MethodPost, "/app/goals/"+id(0)+"/archive", url.Values{}, c), http.StatusSeeOther)
	expect(t, h.do(http.MethodPost, "/app/goals/"+id(0)+"/move", url.Values{"delta": {"sideways"}}, c), http.StatusBadRequest)
	expect(t, h.do(http.MethodPost, "/app/goals/abc", url.Values{"text": {"x"}}, c), http.StatusNotFound)
	expect(t, h.do(http.MethodPost, "/app/goals/18446744073709551617", url.Values{"text": {"x"}}, c), http.StatusNotFound)
	expect(t, h.do(http.MethodPost, "/app/checkin", url.Values{"goal": {"4294967297"}, "date": {"2026-11-12"}, "done": {"true"}}, c), http.StatusNotFound)
	expect(t, h.do(http.MethodPost, "/app/goals/9999/archive", url.Values{}, c), http.StatusNotFound)
	expect(t, h.do(http.MethodPost, "/app/goals/"+id(1), url.Values{"text": {""}}, c), http.StatusBadRequest)

	rec := h.do(http.MethodGet, "/app/goals", nil, c)
	expect(t, rec, http.StatusOK, "write", "jog")
	body := rec.Body.String()
	if strings.Index(body, "write") > strings.Index(body, "jog") || strings.Contains(body, `value="read"`) {
		t.Errorf("goals page order/archive wrong:\n%s", body)
	}
}

func TestSettingsValidation(t *testing.T) {
	h := newHarness(t)
	u, c := h.login(phone)
	expect(t, h.do(http.MethodGet, "/settings", nil, c), http.StatusOK, `value="20:00"`, "America/New_York")
	expect(t, h.do(http.MethodPost, "/settings", url.Values{"timezone": {"Nowhere/Land"}, "time": {"08:00"}}, c), http.StatusBadRequest)
	expect(t, h.do(http.MethodPost, "/settings", url.Values{"timezone": {"UTC"}, "time": {"25:00"}}, c), http.StatusBadRequest)
	expect(t, h.do(http.MethodPost, "/settings", url.Values{"timezone": {"America/Chicago"}, "time": {"07:30"}, "paused": {"on"}}, c), http.StatusSeeOther)
	got, err := h.store.UserByID(t.Context(), u.ID)
	if err != nil || got.Timezone != "America/Chicago" || got.RemindMinute != 450 || !got.Paused {
		t.Fatalf("user = %+v, %v", got, err)
	}
	expect(t, h.do(http.MethodGet, "/settings", nil, c), http.StatusOK, `value="07:30"`, "checked")
}

func TestDeleteAccount(t *testing.T) {
	h := newHarness(t)
	u, c := h.login(phone)
	if _, err := h.store.AddGoal(t.Context(), u.ID, "read"); err != nil {
		t.Fatal(err)
	}
	expect(t, h.do(http.MethodPost, "/settings/delete", url.Values{"confirm": {"nope"}}, c), http.StatusBadRequest)
	rec := h.do(http.MethodPost, "/settings/delete", url.Values{"confirm": {"delete"}}, c)
	expect(t, rec, http.StatusSeeOther)
	if _, err := h.store.UserByID(t.Context(), u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("user still there: %v", err)
	}
	cleared := false
	for _, ck := range rec.Result().Cookies() {
		cleared = cleared || (ck.Name == cookieName && ck.MaxAge < 0)
	}
	if !cleared {
		t.Error("cookie not cleared")
	}
}

func TestLegalPages(t *testing.T) {
	h := newHarness(t)
	expect(t, h.do(http.MethodGet, "/terms", nil, nil), http.StatusOK, "STOP", "HELP", "Message and data rates may apply")
	expect(t, h.do(http.MethodGet, "/privacy", nil, nil), http.StatusOK, "not be shared with third parties")
	expect(t, h.do(http.MethodGet, "/nope", nil, nil), http.StatusNotFound)
}

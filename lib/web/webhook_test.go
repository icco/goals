package web

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"testing"

	"go.icco.me/goals/lib/remind"
	"go.icco.me/goals/lib/store"
)

func reply(t *testing.T, h *harness, form url.Values) string {
	t.Helper()
	rec := h.do(http.MethodPost, "/sms", form, nil)
	expect(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "text/xml; charset=utf-8" {
		t.Errorf("content type = %q", ct)
	}
	var resp struct {
		XMLName xml.Name `xml:"Response"`
		Message string   `xml:"Message"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid TwiML %q: %v", rec.Body.String(), err)
	}
	return resp.Message
}

func withReminder(t *testing.T, h *harness, texts ...string) (*store.User, []store.Goal) {
	t.Helper()
	u, _ := h.login(phone)
	var ids []uint
	for _, text := range texts {
		g, err := h.store.AddGoal(t.Context(), u.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, g.ID)
	}
	if _, _, err := h.store.ClaimReminder(t.Context(), u.ID, "2026-11-12", ids, h.now); err != nil {
		t.Fatal(err)
	}
	goals, err := h.store.GoalsByID(t.Context(), u.ID, ids)
	if err != nil {
		t.Fatal(err)
	}
	return u, goals
}

func TestWebhookBadSignature(t *testing.T) {
	h := newHarness(t)
	u, goals := withReminder(t, h, "read")
	rec := h.do(http.MethodPost, "/sms", url.Values{"From": {phone}, "Body": {"y"}}, nil)
	expect(t, rec, http.StatusForbidden)
	got, _ := h.store.CheckIns(t.Context(), u.ID, "2026-11-01", "2026-11-30")
	if len(got[goals[0].ID]) != 0 {
		t.Error("check-in saved from forged request")
	}
}

func TestWebhookReplyNumbers(t *testing.T) {
	h := newHarness(t)
	h.sms.Valid = true
	u, goals := withReminder(t, h, "read 20 pages", "run", "write")
	ctx := t.Context()
	if err := h.store.SetCheckIn(ctx, u.ID, goals[0].ID, "2026-11-11", true, store.SourceWeb); err != nil {
		t.Fatal(err)
	}
	if err := h.store.ArchiveGoal(ctx, u.ID, goals[1].ID, h.now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.AddGoal(ctx, u.ID, "new goal"); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.AddDate(0, 0, 1)
	msg := reply(t, h, url.Values{"From": {phone}, "Body": {"1 3"}})
	want := "Saved: 2 of 3 done for Nov 12.\nStreaks: read 20 pages 2, run 0, write 1"
	if msg != want {
		t.Errorf("reply = %q, want %q", msg, want)
	}
	got, err := h.store.CheckIns(ctx, u.ID, "2026-11-12", "2026-11-12")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{true, false, true} {
		if done, ok := got[goals[i].ID]["2026-11-12"]; !ok || done != want {
			t.Errorf("goal %d = %v, %v; want %v", i+1, done, ok, want)
		}
	}
}

func TestWebhookStopStart(t *testing.T) {
	h := newHarness(t)
	h.sms.Valid = true
	u, _ := h.login(phone)
	if msg := reply(t, h, url.Values{"From": {phone}, "Body": {"STOP"}, "OptOutType": {"STOP"}}); msg != "" {
		t.Errorf("STOP reply = %q", msg)
	}
	got, _ := h.store.UserByID(t.Context(), u.ID)
	if !got.OptedOut {
		t.Fatal("STOP not recorded")
	}
	reply(t, h, url.Values{"From": {phone}, "Body": {"HELP"}, "OptOutType": {"HELP"}})
	reply(t, h, url.Values{"From": {phone}, "Body": {"START"}, "OptOutType": {"START"}})
	got, _ = h.store.UserByID(t.Context(), u.ID)
	if got.OptedOut {
		t.Fatal("START not recorded")
	}
	reply(t, h, url.Values{"From": {"+15555550199"}, "Body": {"STOP"}, "OptOutType": {"STOP"}})
}

func TestWebhookUnknownNumber(t *testing.T) {
	h := newHarness(t)
	h.sms.Valid = true
	if msg := reply(t, h, url.Values{"From": {"+15555550199"}, "Body": {"y"}}); msg != "" {
		t.Errorf("reply to stranger = %q", msg)
	}
}

func TestWebhookNoReminderYet(t *testing.T) {
	h := newHarness(t)
	h.sms.Valid = true
	h.login(phone)
	if msg := reply(t, h, url.Values{"From": {phone}, "Body": {"y"}}); msg != "No check-in to answer yet." {
		t.Errorf("reply = %q", msg)
	}
}

func TestWebhookUnparsed(t *testing.T) {
	h := newHarness(t)
	h.sms.Valid = true
	withReminder(t, h, "read")
	if msg := reply(t, h, url.Values{"From": {phone}, "Body": {"maybe"}}); msg != remind.ReplyHint {
		t.Errorf("reply = %q", msg)
	}
}

func TestWebhookEscapesGoalText(t *testing.T) {
	h := newHarness(t)
	h.sms.Valid = true
	withReminder(t, h, "a<b & c")
	if msg := reply(t, h, url.Values{"From": {phone}, "Body": {"yes"}}); msg != "Saved: 1 of 1 done for Nov 12.\nStreaks: a<b & c 1" {
		t.Errorf("reply = %q", msg)
	}
}

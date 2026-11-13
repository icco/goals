package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.icco.me/goals/lib/dbtest"
	"go.icco.me/goals/lib/sms"
	"go.icco.me/goals/lib/store"
)

const phone = "+15555550100"

type harness struct {
	t     *testing.T
	srv   *Server
	sms   *sms.Fake
	store *store.Store
	now   time.Time
	h     http.Handler
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.New(t.Context(), dbtest.New(t))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, sms: &sms.Fake{}, store: st, now: time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)}
	h.srv = &Server{Store: st, SMS: h.sms, BaseURL: "https://goals.example.com", Now: func() time.Time { return h.now }}
	h.h = h.srv.Routes()
	return h
}

func (h *harness) do(method, target string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	h.t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(h.t.Context(), method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec
}

func (h *harness) login(p string) (*store.User, *http.Cookie) {
	h.t.Helper()
	u, err := h.store.EnsureUser(h.t.Context(), p, true, h.now)
	if err != nil {
		h.t.Fatal(err)
	}
	tok, err := h.store.CreateSession(h.t.Context(), u.ID, h.now)
	if err != nil {
		h.t.Fatal(err)
	}
	return u, &http.Cookie{Name: cookieName, Value: tok, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, status int, contains ...string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body:\n%s", rec.Code, status, rec.Body.String())
	}
	for _, s := range contains {
		if !strings.Contains(rec.Body.String(), s) {
			t.Errorf("body missing %q:\n%s", s, rec.Body.String())
		}
	}
}

func TestLanding(t *testing.T) {
	h := newHarness(t)
	expect(t, h.do(http.MethodGet, "/", nil, nil), http.StatusOK, "Reply STOP to cancel", `href="/terms"`)
	expect(t, h.do(http.MethodGet, "/static/style.css", nil, nil), http.StatusOK)
	_, c := h.login(phone)
	expect(t, h.do(http.MethodGet, "/", nil, c), http.StatusOK, `href="/app"`)
}

func TestSignupRequiresConsent(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/signup", url.Values{"phone": {"555-555-0100"}}, nil)
	expect(t, rec, http.StatusBadRequest, "agree")
	if h.sms.Started(phone) {
		t.Error("code sent without consent")
	}
	expect(t, h.do(http.MethodPost, "/signup", url.Values{"phone": {"+44 20 7946 0000"}, "consent": {"on"}}, nil), http.StatusBadRequest, "US phone number")
}

func TestSignupAndVerify(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/signup", url.Values{"phone": {"(555) 555-0100"}, "consent": {"on"}}, nil)
	expect(t, rec, http.StatusOK, `value="&#43;15555550100"`, `name="consent" value="on"`)
	if !h.sms.Started(phone) {
		t.Fatal("no code sent")
	}
	if _, err := h.store.UserByPhone(t.Context(), phone); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("user created before verification: %v", err)
	}

	expect(t, h.do(http.MethodPost, "/verify", url.Values{"phone": {phone}, "consent": {"on"}, "code": {"000000"}}, nil), http.StatusBadRequest, "match")
	rec = h.do(http.MethodPost, "/verify", url.Values{"phone": {phone}, "consent": {"on"}, "code": {sms.FakeCode}}, nil)
	expect(t, rec, http.StatusSeeOther)
	if rec.Header().Get("Location") != "/app" {
		t.Fatalf("location = %q", rec.Header().Get("Location"))
	}
	var c *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == cookieName {
			c = ck
		}
	}
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", c)
	}
	u, err := h.store.UserByPhone(t.Context(), phone)
	if err != nil || !u.ConsentedAt.Equal(h.now) {
		t.Fatalf("user = %+v, %v", u, err)
	}
	expect(t, h.do(http.MethodGet, "/app", nil, c), http.StatusOK)
}

func TestLoginUnknownPhone(t *testing.T) {
	h := newHarness(t)
	expect(t, h.do(http.MethodPost, "/login", url.Values{"phone": {phone}}, nil), http.StatusBadRequest, "sign up first")
	if h.sms.Started(phone) {
		t.Error("code sent to unknown phone")
	}
	expect(t, h.do(http.MethodPost, "/verify", url.Values{"phone": {phone}, "code": {sms.FakeCode}}, nil), http.StatusBadRequest)
	if err := h.sms.StartVerify(t.Context(), phone); err != nil {
		t.Fatal(err)
	}
	expect(t, h.do(http.MethodPost, "/verify", url.Values{"phone": {phone}, "code": {sms.FakeCode}}, nil), http.StatusBadRequest, "sign up first")
}

func TestLoginExistingUser(t *testing.T) {
	h := newHarness(t)
	h.login(phone)
	expect(t, h.do(http.MethodGet, "/login", nil, nil), http.StatusOK)
	rec := h.do(http.MethodPost, "/login", url.Values{"phone": {"555.555.0100"}}, nil)
	expect(t, rec, http.StatusOK, `value="&#43;15555550100"`)
	if !h.sms.Started(phone) || strings.Contains(rec.Body.String(), `name="consent"`) {
		t.Fatal("login: no code sent, or consent carried")
	}
	expect(t, h.do(http.MethodGet, "/verify", nil, nil), http.StatusSeeOther)
	expect(t, h.do(http.MethodPost, "/verify", url.Values{"phone": {phone}, "code": {sms.FakeCode}}, nil), http.StatusSeeOther)
}

func TestRequireUserRedirects(t *testing.T) {
	h := newHarness(t)
	for _, c := range []*http.Cookie{nil, {Name: cookieName, Value: "bogus", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}} {
		rec := h.do(http.MethodGet, "/app", nil, c)
		expect(t, rec, http.StatusSeeOther)
		if rec.Header().Get("Location") != "/login" {
			t.Errorf("location = %q", rec.Header().Get("Location"))
		}
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t)
	_, c := h.login(phone)
	rec := h.do(http.MethodPost, "/logout", url.Values{}, c)
	expect(t, rec, http.StatusSeeOther)
	expect(t, h.do(http.MethodGet, "/app", nil, c), http.StatusSeeOther)
}

func TestCrossOriginPostRejected(t *testing.T) {
	h := newHarness(t)
	_, c := h.login(phone)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/logout", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSMSDisabledMessage(t *testing.T) {
	h := newHarness(t)
	h.sms.FailSend = sms.ErrDisabled
	expect(t, h.do(http.MethodPost, "/signup", url.Values{"phone": {phone}, "consent": {"on"}}, nil), http.StatusServiceUnavailable, "not set up yet")
	h.sms.FailSend = errors.New("twilio 500")
	expect(t, h.do(http.MethodPost, "/signup", url.Values{"phone": {phone}, "consent": {"on"}}, nil), http.StatusBadGateway, "try again")
}

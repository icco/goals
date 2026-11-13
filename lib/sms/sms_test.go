package sms

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Twilio signs webhooks with HMAC-SHA1.
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
)

func TestConfigEnabled(t *testing.T) {
	env := map[string]string{
		"TWILIO_ACCOUNT_SID":           "AC123",
		"TWILIO_AUTH_TOKEN":            "secret",
		"TWILIO_MESSAGING_SERVICE_SID": "MG123",
		"TWILIO_VERIFY_SERVICE_SID":    "VA123",
	}
	c := ConfigFromEnv(func(k string) string { return env[k] })
	if !c.Enabled() || c.AccountSID != "AC123" || c.VerifyServiceSID != "VA123" {
		t.Fatalf("config = %+v", c)
	}
	for k := range env {
		partial := ConfigFromEnv(func(key string) string {
			if key == k {
				return ""
			}
			return env[key]
		})
		if partial.Enabled() {
			t.Errorf("enabled without %s", k)
		}
	}
}

func TestDisabledTwilio(t *testing.T) {
	tw := New(Config{})
	ctx := t.Context()
	if _, err := tw.Send(ctx, "+15555550100", "hi"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Send err = %v", err)
	}
	if err := tw.StartVerify(ctx, "+15555550100"); !errors.Is(err, ErrDisabled) {
		t.Errorf("StartVerify err = %v", err)
	}
	if _, err := tw.CheckVerify(ctx, "+15555550100", "123456"); !errors.Is(err, ErrDisabled) {
		t.Errorf("CheckVerify err = %v", err)
	}
	if tw.ValidRequest("https://example.com/sms", nil, "x") {
		t.Error("ValidRequest true while disabled")
	}
}

func sign(token, url string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	mac := hmac.New(sha1.New, []byte(token))
	mac.Write([]byte(url))
	for _, k := range keys {
		mac.Write([]byte(k + params[k]))
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestTwilioValidRequest(t *testing.T) {
	tw := New(Config{AccountSID: "AC123", AuthToken: "secret", MessagingServiceSID: "MG123", VerifyServiceSID: "VA123"})
	url := "https://goals.example.com/sms"
	params := map[string]string{"From": "+15555550100", "Body": "1 3"}
	sig := sign("secret", url, params)
	if !tw.ValidRequest(url, params, sig) {
		t.Fatal("valid signature rejected")
	}
	params["Body"] = "y"
	if tw.ValidRequest(url, params, sig) {
		t.Fatal("altered params accepted")
	}
	if tw.ValidRequest(url, params, sign("wrong", url, params)) {
		t.Fatal("wrong token accepted")
	}
}

func TestFake(t *testing.T) {
	f := &Fake{}
	ctx := t.Context()
	if _, err := f.Send(ctx, "+15555550100", "hi"); err != nil {
		t.Fatal(err)
	}
	if len(f.Messages()) != 1 || f.Messages()[0].Body != "hi" {
		t.Errorf("Sent = %+v", f.Messages())
	}
	if err := f.StartVerify(ctx, "+15555550100"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.CheckVerify(ctx, "+15555550100", "000000"); ok {
		t.Error("wrong code accepted")
	}
	if ok, _ := f.CheckVerify(ctx, "+15555550100", FakeCode); !ok {
		t.Error("right code rejected")
	}
	if ok, _ := f.CheckVerify(ctx, "+15555550101", FakeCode); ok {
		t.Error("code for unverified phone accepted")
	}
	f.FailSend = errors.New("boom")
	if _, err := f.Send(ctx, "+15555550100", "hi"); err == nil {
		t.Error("FailSend ignored")
	}
	if err := f.StartVerify(ctx, "+15555550101"); err == nil {
		t.Error("FailSend ignored by StartVerify")
	}
	f.Valid = true
	if !f.ValidRequest("", nil, "") {
		t.Error("Valid ignored")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubTwilio(t *testing.T, status int, body string) (*Twilio, *[]*http.Request) {
	t.Helper()
	var reqs []*http.Request
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		reqs = append(reqs, r)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return newTwilio(Config{AccountSID: "AC123", AuthToken: "secret", MessagingServiceSID: "MG123", VerifyServiceSID: "VA123"}, hc), &reqs
}

func TestTwilioSend(t *testing.T) {
	tw, reqs := stubTwilio(t, http.StatusCreated, `{"sid":"SM42"}`)
	sid, err := tw.Send(t.Context(), "+15555550100", "hi")
	if err != nil || sid != "SM42" {
		t.Fatalf("Send = %q, %v", sid, err)
	}
	r := (*reqs)[0]
	if !strings.HasSuffix(r.URL.Path, "/Accounts/AC123/Messages.json") || r.PostForm.Get("To") != "+15555550100" ||
		r.PostForm.Get("Body") != "hi" || r.PostForm.Get("MessagingServiceSid") != "MG123" {
		t.Errorf("request = %s %v", r.URL, r.PostForm)
	}
	bad, _ := stubTwilio(t, http.StatusBadRequest, `{"code":21211,"message":"bad To","status":400}`)
	if _, err := bad.Send(t.Context(), "+15555550100", "hi"); err == nil {
		t.Error("Send error swallowed")
	}
}

func TestTwilioVerify(t *testing.T) {
	tw, reqs := stubTwilio(t, http.StatusCreated, `{"sid":"VE1","status":"pending"}`)
	if err := tw.StartVerify(t.Context(), "+15555550100"); err != nil {
		t.Fatal(err)
	}
	r := (*reqs)[0]
	if !strings.HasSuffix(r.URL.Path, "/Services/VA123/Verifications") || r.PostForm.Get("Channel") != "sms" {
		t.Errorf("request = %s %v", r.URL, r.PostForm)
	}
	if ok, err := tw.CheckVerify(t.Context(), "+15555550100", "1"); err != nil || ok {
		t.Errorf("pending check = %v, %v", ok, err)
	}
	approved, _ := stubTwilio(t, http.StatusOK, `{"status":"approved"}`)
	if ok, err := approved.CheckVerify(t.Context(), "+15555550100", "123456"); err != nil || !ok {
		t.Errorf("approved check = %v, %v", ok, err)
	}
	bad, _ := stubTwilio(t, http.StatusTooManyRequests, `{"code":60203,"message":"Max send attempts reached","status":429}`)
	if err := bad.StartVerify(t.Context(), "+15555550100"); err == nil {
		t.Error("StartVerify error swallowed")
	}
	if _, err := bad.CheckVerify(t.Context(), "+15555550100", "1"); err == nil {
		t.Error("CheckVerify error swallowed")
	}
}

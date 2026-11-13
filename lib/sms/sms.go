// Package sms sends texts, verifies phone numbers, and checks webhook
// signatures through Twilio.
package sms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/twilio/twilio-go"
	"github.com/twilio/twilio-go/client"
	api "github.com/twilio/twilio-go/rest/api/v2010"
	verify "github.com/twilio/twilio-go/rest/verify/v2"
)

// ErrDisabled means Twilio is not configured, so nothing can be texted.
var ErrDisabled = errors.New("sms: twilio is not configured")

// Sender sends a text message.
type Sender interface {
	Send(ctx context.Context, to, body string) (sid string, err error)
}

// Verifier texts and checks one-time login codes.
type Verifier interface {
	StartVerify(ctx context.Context, phone string) error
	CheckVerify(ctx context.Context, phone, code string) (bool, error)
}

// Validator checks that a webhook request came from Twilio.
type Validator interface {
	ValidRequest(url string, params map[string]string, signature string) bool
}

// Client is everything the app needs from Twilio.
type Client interface {
	Sender
	Verifier
	Validator
}

// Config holds Twilio credentials and service SIDs.
type Config struct {
	AccountSID          string
	AuthToken           string
	MessagingServiceSID string
	VerifyServiceSID    string
}

// ConfigFromEnv reads the TWILIO_* variables.
func ConfigFromEnv(getenv func(string) string) Config {
	return Config{
		AccountSID:          getenv("TWILIO_ACCOUNT_SID"),
		AuthToken:           getenv("TWILIO_AUTH_TOKEN"),
		MessagingServiceSID: getenv("TWILIO_MESSAGING_SERVICE_SID"),
		VerifyServiceSID:    getenv("TWILIO_VERIFY_SERVICE_SID"),
	}
}

// Enabled reports whether every value is set.
func (c Config) Enabled() bool {
	return c.AccountSID != "" && c.AuthToken != "" && c.MessagingServiceSID != "" && c.VerifyServiceSID != ""
}

// Twilio is the real Client. Without a full Config every call fails with
// ErrDisabled.
type Twilio struct {
	cfg       Config
	rest      *twilio.RestClient
	validator client.RequestValidator
}

// New returns a Twilio client for c.
func New(c Config) *Twilio {
	return newTwilio(c, &http.Client{Timeout: 30 * time.Second})
}

func newTwilio(c Config, hc *http.Client) *Twilio {
	t := &Twilio{cfg: c}
	if c.Enabled() {
		base := &client.Client{Credentials: client.NewCredentials(c.AccountSID, c.AuthToken), HTTPClient: hc}
		base.SetAccountSid(c.AccountSID)
		t.rest = twilio.NewRestClientWithParams(twilio.ClientParams{Username: c.AccountSID, Password: c.AuthToken, Client: base})
		t.validator = client.NewRequestValidator(c.AuthToken)
	}
	return t
}

// Send texts body to the E.164 number to through the messaging service.
func (t *Twilio) Send(_ context.Context, to, body string) (string, error) {
	if t.rest == nil {
		return "", ErrDisabled
	}
	params := (&api.CreateMessageParams{}).SetTo(to).SetBody(body).SetMessagingServiceSid(t.cfg.MessagingServiceSID)
	msg, err := t.rest.Api.CreateMessage(params)
	if err != nil {
		return "", fmt.Errorf("send sms: %w", err)
	}
	if msg.Sid == nil {
		return "", nil
	}
	return *msg.Sid, nil
}

// StartVerify texts a login code to phone.
func (t *Twilio) StartVerify(_ context.Context, phone string) error {
	if t.rest == nil {
		return ErrDisabled
	}
	params := (&verify.CreateVerificationParams{}).SetTo(phone).SetChannel("sms")
	if _, err := t.rest.VerifyV2.CreateVerification(t.cfg.VerifyServiceSID, params); err != nil {
		return fmt.Errorf("start verification: %w", err)
	}
	return nil
}

// CheckVerify reports whether code is the one most recently texted to phone.
func (t *Twilio) CheckVerify(_ context.Context, phone, code string) (bool, error) {
	if t.rest == nil {
		return false, ErrDisabled
	}
	params := (&verify.CreateVerificationCheckParams{}).SetTo(phone).SetCode(code)
	res, err := t.rest.VerifyV2.CreateVerificationCheck(t.cfg.VerifyServiceSID, params)
	if err != nil {
		return false, fmt.Errorf("check verification: %w", err)
	}
	return res.Status != nil && *res.Status == "approved", nil
}

// ValidRequest checks a webhook's X-Twilio-Signature against its full URL and
// form parameters.
func (t *Twilio) ValidRequest(url string, params map[string]string, signature string) bool {
	if t.rest == nil {
		return false
	}
	return t.validator.Validate(url, params, signature)
}

// FakeCode is the login code Fake accepts.
const FakeCode = "123456"

// Message is a text the Fake sent.
type Message struct {
	To, Body string
}

// Fake is an in-memory Client for tests.
type Fake struct {
	// FailSend, when set, is returned by Send and StartVerify.
	FailSend error
	// Valid is what ValidRequest returns.
	Valid bool

	mu       sync.Mutex
	sent     []Message
	verified map[string]bool
}

// Send records the message.
func (f *Fake) Send(_ context.Context, to, body string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailSend != nil {
		return "", f.FailSend
	}
	f.sent = append(f.sent, Message{To: to, Body: body})
	return fmt.Sprintf("SM%d", len(f.sent)), nil
}

// Messages returns what Send recorded.
func (f *Fake) Messages() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Message(nil), f.sent...)
}

// StartVerify records that FakeCode was sent to phone.
func (f *Fake) StartVerify(_ context.Context, phone string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailSend != nil {
		return f.FailSend
	}
	if f.verified == nil {
		f.verified = map[string]bool{}
	}
	f.verified[phone] = true
	return nil
}

// Started reports whether StartVerify was called for phone.
func (f *Fake) Started(phone string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verified[phone]
}

// CheckVerify accepts FakeCode for a phone StartVerify was called with.
func (f *Fake) CheckVerify(_ context.Context, phone, code string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verified[phone] && code == FakeCode, nil
}

// ValidRequest returns f.Valid.
func (f *Fake) ValidRequest(string, map[string]string, string) bool {
	return f.Valid
}

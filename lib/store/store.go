// Package store keeps users, goals, check-ins, reminders, and sessions in
// Postgres.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
)

// Defaults and limits from the design.
const (
	// DefaultTimezone is the timezone a new user starts in.
	DefaultTimezone = "America/New_York"
	// DefaultRemindMinute is the local minute of the day (20:00) a new user is texted.
	DefaultRemindMinute = 20 * 60
	// MaxGoals caps a user's active goals.
	MaxGoals = 10
	// MaxGoalLen caps a goal's text, in runes.
	MaxGoalLen = 140
	// SessionTTL is how long a login lasts.
	SessionTTL = 30 * 24 * time.Hour
	// DateLayout formats the local dates check-ins and reminders are keyed by.
	DateLayout = "2006-01-02"
)

// Check-in sources.
const (
	// SourceSMS marks a check-in recorded from a text reply.
	SourceSMS = "sms"
	// SourceWeb marks a check-in recorded on the website.
	SourceWeb = "web"
)

// Errors returned by Store methods.
var (
	// ErrNotFound means the row does not exist or belongs to someone else.
	ErrNotFound = errors.New("not found")
	// ErrNoConsent means a new user tried to sign up without agreeing to texts.
	ErrNoConsent = errors.New("consent to texts is required to sign up")
	// ErrTooManyGoals means the user already has MaxGoals active goals.
	ErrTooManyGoals = fmt.Errorf("you can have at most %d goals", MaxGoals)
	// ErrInvalidGoal means the goal text is empty or too long.
	ErrInvalidGoal = fmt.Errorf("a goal must be 1 to %d characters", MaxGoalLen)
	// ErrInvalidPhone means the number is not a US mobile number.
	ErrInvalidPhone = errors.New("enter a US phone number, like 555-555-0100")
	// ErrInvalidSettings means the timezone or reminder time is invalid.
	ErrInvalidSettings = errors.New("invalid timezone or reminder time")
)

// Store is the goals database.
type Store struct {
	db *gorm.DB
}

// New migrates the schema and returns a Store.
func New(ctx context.Context, db *gorm.DB) (*Store, error) {
	if err := db.WithContext(ctx).AutoMigrate(&User{}, &Goal{}, &CheckIn{}, &Reminder{}, &Session{}); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// NormalizePhone turns a US number in any common format into E.164.
func NormalizePhone(s string) (string, error) {
	s = strings.TrimSpace(s)
	plus := strings.HasPrefix(s, "+")
	var digits strings.Builder
	for i, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits.WriteRune(r)
		case r == '+' && i == 0, strings.ContainsRune(" ()-.", r):
		default:
			return "", ErrInvalidPhone
		}
	}
	d := digits.String()
	switch {
	case len(d) == 11 && d[0] == '1':
		d = d[1:]
	case len(d) == 10 && !plus:
	default:
		return "", ErrInvalidPhone
	}
	if d[0] < '2' {
		return "", ErrInvalidPhone
	}
	return "+1" + d, nil
}

// LocalDate returns t's date in timezone tz as YYYY-MM-DD.
func LocalDate(t time.Time, tz string) (string, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", fmt.Errorf("load timezone %q: %w", tz, err)
	}
	return t.In(loc).Format(DateLayout), nil
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

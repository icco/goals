// Package remind texts each user their goals once a day at their local
// reminder time.
package remind

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.icco.me/goals/lib/sms"
	"go.icco.me/goals/lib/store"
	"go.icco.me/gutil/logging"
	"go.uber.org/zap"
)

// ReplyHint is the last line of every reminder.
const ReplyHint = `Reply y, n, or the numbers you did (e.g. "1 3").`

// Scheduler sends due reminders on each Tick.
type Scheduler struct {
	Store  *store.Store
	Sender sms.Sender
	Now    func() time.Time
}

// Message is the reminder text listing goals by number.
func Message(goals []store.Goal) string {
	var b strings.Builder
	b.WriteString("goals: did you do these today?\n")
	for i, g := range goals {
		fmt.Fprintf(&b, "%d. %s\n", i+1, g.Text)
	}
	b.WriteString(ReplyHint)
	return b.String()
}

// Tick texts every due user whose local time has reached their reminder time
// and who has not been texted today. A reminder is claimed before it is sent,
// so a failed send is not retried that day.
func (s *Scheduler) Tick(ctx context.Context) error {
	users, err := s.Store.DueUsers(ctx)
	if err != nil {
		return err
	}
	now := s.Now()
	var errs []error
	for _, u := range users {
		if err := s.remind(ctx, u, now); err != nil {
			errs = append(errs, fmt.Errorf("remind user %d: %w", u.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Scheduler) remind(ctx context.Context, u store.User, now time.Time) error {
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return fmt.Errorf("load timezone %q: %w", u.Timezone, err)
	}
	local := now.In(loc)
	if local.Hour()*60+local.Minute() < u.RemindMinute {
		return nil
	}
	goals, err := s.Store.ActiveGoals(ctx, u.ID)
	if err != nil || len(goals) == 0 {
		return err
	}
	ids := make([]uint, len(goals))
	for i, g := range goals {
		ids[i] = g.ID
	}
	r, claimed, err := s.Store.ClaimReminder(ctx, u.ID, local.Format(store.DateLayout), ids, now)
	if err != nil || !claimed {
		return err
	}
	sid, err := s.Sender.Send(ctx, u.Phone, Message(goals))
	if err != nil {
		return err
	}
	return s.Store.SetReminderSID(ctx, r.ID, sid)
}

// Run calls Tick every interval until ctx is done, logging failures.
func (s *Scheduler) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).Errorw("send reminders", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

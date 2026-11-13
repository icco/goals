package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm/clause"
)

// DueUsers returns users who may be texted: not paused, not opted out, and
// with at least one active goal.
func (s *Store) DueUsers(ctx context.Context) ([]User, error) {
	var users []User
	err := s.db.WithContext(ctx).
		Where("NOT paused AND NOT opted_out").
		Where("EXISTS (?)", s.db.Model(&Goal{}).Select("1").Where("goals.user_id = users.id AND goals.archived_at IS NULL")).
		Order("id").Find(&users).Error
	if err != nil {
		return nil, fmt.Errorf("load due users: %w", err)
	}
	return users, nil
}

// ClaimReminder records that the user's reminder for date is being sent. It
// returns false when one was already claimed, so each day sends at most once.
func (s *Store) ClaimReminder(ctx context.Context, userID uint, date string, goalIDs []uint, now time.Time) (*Reminder, bool, error) {
	r := &Reminder{UserID: userID, Date: date, GoalIDs: goalIDs, SentAt: now}
	res := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(r)
	if res.Error != nil {
		return nil, false, fmt.Errorf("claim reminder: %w", res.Error)
	}
	return r, res.RowsAffected == 1, nil
}

// SetReminderSID stores the Twilio message SID of a sent reminder.
func (s *Store) SetReminderSID(ctx context.Context, id uint, sid string) error {
	if err := s.db.WithContext(ctx).Model(&Reminder{}).Where("id = ?", id).Update("message_sid", sid).Error; err != nil {
		return fmt.Errorf("set reminder sid: %w", err)
	}
	return nil
}

// LatestReminder returns the user's most recent reminder.
func (s *Store) LatestReminder(ctx context.Context, userID uint) (*Reminder, error) {
	var r Reminder
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("date DESC, id DESC").Take(&r).Error; err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

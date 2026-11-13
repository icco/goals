package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// EnsureUser returns the user with phone, creating them only when consented
// is true.
func (s *Store) EnsureUser(ctx context.Context, phone string, consented bool, now time.Time) (*User, error) {
	u, err := s.UserByPhone(ctx, phone)
	if !errors.Is(err, ErrNotFound) {
		return u, err
	}
	if !consented {
		return nil, ErrNoConsent
	}
	u = &User{Phone: phone, Timezone: DefaultTimezone, RemindMinute: DefaultRemindMinute, ConsentedAt: now}
	if err := s.db.WithContext(ctx).Create(u).Error; err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// UserByPhone looks a user up by E.164 phone number.
func (s *Store) UserByPhone(ctx context.Context, phone string) (*User, error) {
	var u User
	if err := s.db.WithContext(ctx).Where("phone = ?", phone).Take(&u).Error; err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

// UserByID looks a user up by ID.
func (s *Store) UserByID(ctx context.Context, id uint) (*User, error) {
	var u User
	if err := s.db.WithContext(ctx).Take(&u, id).Error; err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

// UpdateSettings changes a user's timezone, reminder minute, and pause state.
func (s *Store) UpdateSettings(ctx context.Context, id uint, tz string, remindMinute int, paused bool) error {
	if _, err := time.LoadLocation(tz); err != nil || tz == "" || tz == "Local" {
		return ErrInvalidSettings
	}
	if remindMinute < 0 || remindMinute >= 24*60 {
		return ErrInvalidSettings
	}
	res := s.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).
		Updates(map[string]any{"timezone": tz, "remind_minute": remindMinute, "paused": paused})
	if res.Error != nil {
		return fmt.Errorf("update settings: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetOptedOut records a STOP or START from phone.
func (s *Store) SetOptedOut(ctx context.Context, phone string, optedOut bool) error {
	res := s.db.WithContext(ctx).Model(&User{}).Where("phone = ?", phone).Update("opted_out", optedOut)
	if res.Error != nil {
		return fmt.Errorf("set opted out: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes a user and everything that belongs to them.
func (s *Store) DeleteUser(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		goals := tx.Model(&Goal{}).Select("id").Where("user_id = ?", id)
		steps := []*gorm.DB{
			tx.Where("user_id = ?", id).Delete(&Session{}),
			tx.Where("user_id = ?", id).Delete(&Reminder{}),
			tx.Where("goal_id IN (?)", goals).Delete(&CheckIn{}),
			tx.Where("user_id = ?", id).Delete(&Goal{}),
			tx.Delete(&User{}, id),
		}
		for _, step := range steps {
			if step.Error != nil {
				return fmt.Errorf("delete user: %w", step.Error)
			}
		}
		return nil
	})
}

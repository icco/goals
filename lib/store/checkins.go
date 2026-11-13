package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm/clause"
)

// SetCheckIn records whether the user's goal was done on date, replacing any
// earlier answer for that day.
func (s *Store) SetCheckIn(ctx context.Context, userID, goalID uint, date string, done bool, source string) error {
	if _, err := time.Parse(DateLayout, date); err != nil {
		return fmt.Errorf("check-in date %q: %w", date, err)
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Goal{}).Where("id = ? AND user_id = ?", goalID, userID).Count(&n).Error; err != nil {
		return fmt.Errorf("find goal: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	c := CheckIn{GoalID: goalID, Date: date, Done: done, Source: source}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "goal_id"}, {Name: "date"}},
		DoUpdates: clause.AssignmentColumns([]string{"done", "source", "updated_at"}),
	}).Create(&c).Error
	if err != nil {
		return fmt.Errorf("save check-in: %w", err)
	}
	return nil
}

// CheckIns returns the user's answers from one date to another, inclusive,
// keyed by goal ID then date.
func (s *Store) CheckIns(ctx context.Context, userID uint, from, to string) (map[uint]map[string]bool, error) {
	var rows []CheckIn
	err := s.db.WithContext(ctx).
		Where("goal_id IN (?) AND date BETWEEN ? AND ?", s.db.Model(&Goal{}).Select("id").Where("user_id = ?", userID), from, to).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load check-ins: %w", err)
	}
	out := map[uint]map[string]bool{}
	for _, c := range rows {
		if out[c.GoalID] == nil {
			out[c.GoalID] = map[string]bool{}
		}
		out[c.GoalID][c.Date] = c.Done
	}
	return out, nil
}

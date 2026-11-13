package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

func cleanGoal(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxGoalLen {
		return "", ErrInvalidGoal
	}
	return text, nil
}

func active(db *gorm.DB, userID uint) *gorm.DB {
	return db.Where("user_id = ? AND archived_at IS NULL", userID)
}

// ActiveGoals returns a user's unarchived goals in display order.
func (s *Store) ActiveGoals(ctx context.Context, userID uint) ([]Goal, error) {
	var goals []Goal
	if err := active(s.db.WithContext(ctx), userID).Order("position, id").Find(&goals).Error; err != nil {
		return nil, fmt.Errorf("load goals: %w", err)
	}
	return goals, nil
}

// GoalsByID returns the user's goals with ids, archived or not, in the order
// of ids. IDs that are not the user's are skipped.
func (s *Store) GoalsByID(ctx context.Context, userID uint, ids []uint) ([]Goal, error) {
	var found []Goal
	if err := s.db.WithContext(ctx).Where("user_id = ? AND id IN ?", userID, ids).Find(&found).Error; err != nil {
		return nil, fmt.Errorf("load goals: %w", err)
	}
	byID := make(map[uint]Goal, len(found))
	for _, g := range found {
		byID[g.ID] = g
	}
	out := make([]Goal, 0, len(found))
	for _, id := range ids {
		if g, ok := byID[id]; ok {
			out = append(out, g)
		}
	}
	return out, nil
}

// AddGoal appends a goal to the user's list.
func (s *Store) AddGoal(ctx context.Context, userID uint, text string) (*Goal, error) {
	text, err := cleanGoal(text)
	if err != nil {
		return nil, err
	}
	g := &Goal{UserID: userID, Text: text}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := active(tx.Model(&Goal{}), userID).Count(&n).Error; err != nil {
			return fmt.Errorf("count goals: %w", err)
		}
		if n >= MaxGoals {
			return ErrTooManyGoals
		}
		var maxPos *int
		if err := tx.Model(&Goal{}).Where("user_id = ?", userID).Select("MAX(position)").Scan(&maxPos).Error; err != nil {
			return fmt.Errorf("max position: %w", err)
		}
		if maxPos != nil {
			g.Position = *maxPos + 1
		}
		if err := tx.Create(g).Error; err != nil {
			return fmt.Errorf("create goal: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Store) updateOwned(ctx context.Context, userID, goalID uint, values map[string]any) error {
	res := s.db.WithContext(ctx).Model(&Goal{}).Where("id = ? AND user_id = ?", goalID, userID).Updates(values)
	if res.Error != nil {
		return fmt.Errorf("update goal: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateGoal changes a goal's text.
func (s *Store) UpdateGoal(ctx context.Context, userID, goalID uint, text string) error {
	text, err := cleanGoal(text)
	if err != nil {
		return err
	}
	return s.updateOwned(ctx, userID, goalID, map[string]any{"text": text})
}

// ArchiveGoal hides a goal from the list and reminders but keeps its history.
func (s *Store) ArchiveGoal(ctx context.Context, userID, goalID uint, now time.Time) error {
	return s.updateOwned(ctx, userID, goalID, map[string]any{"archived_at": now})
}

// MoveGoal swaps an active goal with its neighbour delta places away (+1 or -1).
// Moving past either end does nothing.
func (s *Store) MoveGoal(ctx context.Context, userID, goalID uint, delta int) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var goals []Goal
		if err := active(tx, userID).Order("position, id").Find(&goals).Error; err != nil {
			return fmt.Errorf("load goals: %w", err)
		}
		i := -1
		for j, g := range goals {
			if g.ID == goalID {
				i = j
			}
		}
		if i < 0 {
			return ErrNotFound
		}
		j := i + delta
		if delta == 0 || j < 0 || j >= len(goals) {
			return nil
		}
		goals[i], goals[j] = goals[j], goals[i]
		for pos, g := range goals {
			if err := tx.Model(&Goal{}).Where("id = ?", g.ID).Update("position", pos).Error; err != nil {
				return fmt.Errorf("reorder goals: %w", err)
			}
		}
		return nil
	})
}

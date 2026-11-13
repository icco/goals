package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession starts a login for the user and returns its secret token.
func (s *Store) CreateSession(ctx context.Context, userID uint, now time.Time) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	sess := Session{TokenHash: hashToken(token), UserID: userID, ExpiresAt: now.Add(SessionTTL)}
	if err := s.db.WithContext(ctx).Create(&sess).Error; err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// SessionUser returns the user a live session token belongs to.
func (s *Store) SessionUser(ctx context.Context, token string, now time.Time) (*User, error) {
	var sess Session
	err := s.db.WithContext(ctx).Where("token_hash = ? AND expires_at > ?", hashToken(token), now).Take(&sess).Error
	if err != nil {
		return nil, notFound(err)
	}
	return s.UserByID(ctx, sess.UserID)
}

// DeleteSession logs a session out.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if err := s.db.WithContext(ctx).Where("token_hash = ?", hashToken(token)).Delete(&Session{}).Error; err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

package store

import "time"

// User is someone who signed up with a phone number.
type User struct {
	ID           uint
	Phone        string `gorm:"uniqueIndex;not null"`
	Timezone     string `gorm:"not null"`
	RemindMinute int    `gorm:"not null"`
	Paused       bool   `gorm:"not null;default:false"`
	OptedOut     bool   `gorm:"not null;default:false"`
	ConsentedAt  time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Goal is one daily goal of a user. Archived goals keep their history.
type Goal struct {
	ID         uint
	UserID     uint   `gorm:"index;not null"`
	Text       string `gorm:"not null"`
	Position   int    `gorm:"not null"`
	ArchivedAt *time.Time
	CreatedAt  time.Time
}

// CheckIn records whether a goal was done on a local date.
type CheckIn struct {
	ID        uint
	GoalID    uint   `gorm:"uniqueIndex:idx_goal_date;not null"`
	Date      string `gorm:"uniqueIndex:idx_goal_date;not null"`
	Done      bool   `gorm:"not null"`
	Source    string `gorm:"not null"`
	UpdatedAt time.Time
}

// Reminder is the daily text sent to a user, with the goals it listed in
// order so a numbered reply maps back to them.
type Reminder struct {
	ID         uint
	UserID     uint   `gorm:"uniqueIndex:idx_user_date;not null"`
	Date       string `gorm:"uniqueIndex:idx_user_date;not null"`
	GoalIDs    []uint `gorm:"serializer:json"`
	SentAt     time.Time
	MessageSID string `gorm:"column:message_sid"`
}

// Session is a web login. Only the token's hash is stored.
type Session struct {
	TokenHash string `gorm:"primaryKey"`
	UserID    uint   `gorm:"index;not null"`
	ExpiresAt time.Time
}

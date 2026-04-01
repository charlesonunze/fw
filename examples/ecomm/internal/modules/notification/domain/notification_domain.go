package domain

import (
	"context"
	"time"
)

// Notification is a message delivered to a user.
type Notification struct {
	ID      string
	UserID  string
	Message string
	SentAt  time.Time
}

// NotificationRepository defines the persistence contract for notifications.
type NotificationRepository interface {
	Save(ctx context.Context, n *Notification) error
}

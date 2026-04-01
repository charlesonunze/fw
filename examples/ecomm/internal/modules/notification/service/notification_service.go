package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/domain"
)

type subscriber struct {
	userID string
	ch     chan *domain.Notification
}

// NotificationService handles notification delivery and live streaming.
type NotificationService struct {
	repo domain.NotificationRepository
	mu   sync.RWMutex
	subs []*subscriber
}

// New creates a new NotificationService.
func New(repo domain.NotificationRepository) *NotificationService {
	return &NotificationService{repo: repo}
}

// Name returns the service registry key.
func (s *NotificationService) Name() string { return "notification.service" }

func (m *NotificationService) Close() error { return nil }

// Send persists a notification and pushes it to any active Stream subscribers.
func (s *NotificationService) Send(ctx context.Context, userID, message string) (*domain.Notification, error) {
	n := &domain.Notification{
		ID:      generateID(),
		UserID:  userID,
		Message: message,
		SentAt:  time.Now(),
	}

	if err := s.repo.Save(ctx, n); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.subs {
		if sub.userID == userID {
			select {
			case sub.ch <- n:
			default: // don't block if the subscriber is slow
			}
		}
	}

	return n, nil
}

// Subscribe registers a live channel for a user's incoming notifications.
// The returned cancel func must be called when the caller is done.
func (s *NotificationService) Subscribe(userID string) (<-chan *domain.Notification, func()) {
	sub := &subscriber{
		userID: userID,
		ch:     make(chan *domain.Notification, 16),
	}

	s.mu.Lock()
	s.subs = append(s.subs, sub)
	s.mu.Unlock()

	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, s2 := range s.subs {
			if s2 == sub {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				break
			}
		}
		close(sub.ch)
	}

	return sub.ch, cancel
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

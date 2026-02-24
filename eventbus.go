package fw

import (
	"context"
	"fmt"
	"sync"
)

// Event represents a domain event published by a module.
type Event struct {
	// Type identifies the event (e.g., "user.created", "order.completed").
	Type string

	// Payload holds the event data.
	Payload any
}

// EventHandler is a function that handles an event.
type EventHandler func(ctx context.Context, event Event) error

// EventBus is a simple in-memory pub/sub event bus.
// In a monolith, events are delivered synchronously.
// When decoupled, this can be swapped for NATS/Kafka/RabbitMQ.
type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]EventHandler
}

// NewEventBus creates a new in-memory event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]EventHandler),
	}
}

// Subscribe registers a handler for a given event type.
func (b *EventBus) Subscribe(eventType string, handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Publish sends an event to all registered handlers for that event type.
// Handlers are called synchronously in the order they were subscribed.
// Returns the first error encountered.
func (b *EventBus) Publish(ctx context.Context, event Event) error {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			return fmt.Errorf("fw: event handler for %q failed: %w", event.Type, err)
		}
	}

	return nil
}

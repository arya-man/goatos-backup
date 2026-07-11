// Package eventbus is the transport-agnostic dispatch seam for domain events that drive the
// state-machine handlers (SM-1 generation, SM-3 cancel, SM-7 booster, …).
//
// State-machine handlers subscribe to event types and must be idempotent (at-least-once
// delivery). Phase 1 uses InProcessBus (synchronous in-process dispatch). The deployed Pub/Sub
// path will implement the same Bus interface — Publish enqueues to the outbox/topic and delivery
// invokes the SAME handlers — so handler code never changes between local and deployed transports.
package eventbus

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Event is a domain event. Key is the aggregate key (e.g. goat_id) used for ordering/idempotency.
type Event struct {
	ID         string
	Type       string
	TenantID   string
	Key        string
	Payload    []byte
	OccurredAt time.Time
	RecordedAt time.Time
}

type permanentError struct {
	err error
}

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// PermanentError marks a handler failure as non-retryable for durable event delivery.
func PermanentError(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanentError reports whether err, including an errors.Join tree, contains only
// permanent handler failures.
func IsPermanentError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(permanentError); ok {
		return true
	}
	if _, ok := err.(*permanentError); ok {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !IsPermanentError(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return IsPermanentError(wrapped.Unwrap())
	}
	return false
}

// Handler consumes an event. Implementations must be idempotent.
type Handler interface {
	HandleEvent(ctx context.Context, e Event) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, e Event) error

// HandleEvent calls the wrapped function.
func (f HandlerFunc) HandleEvent(ctx context.Context, e Event) error { return f(ctx, e) }

// Bus subscribes handlers to event types and publishes events to them.
type Bus interface {
	Subscribe(eventType string, h Handler)
	Publish(ctx context.Context, e Event) error
}

// InProcessBus dispatches synchronously to all handlers subscribed to an event's type.
type InProcessBus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

// NewInProcessBus constructs an empty in-process bus.
func NewInProcessBus() *InProcessBus {
	return &InProcessBus{handlers: make(map[string][]Handler)}
}

var _ Bus = (*InProcessBus)(nil)

// Subscribe registers a handler for an event type.
func (b *InProcessBus) Subscribe(eventType string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], h)
}

// Publish dispatches the event to every handler subscribed to its type, synchronously. Unknown
// event types are a no-op. Handler errors are joined and returned (the caller decides retry).
func (b *InProcessBus) Publish(ctx context.Context, e Event) error {
	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers[e.Type]...)
	b.mu.RUnlock()

	var errs []error
	for _, h := range handlers {
		if err := h.HandleEvent(ctx, e); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

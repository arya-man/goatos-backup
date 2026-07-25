package app

import (
	"context"
	"errors"
)

// ErrBusy is returned when the assistant is shedding load past its concurrency
// bound. The orchestrator maps this to a friendly "busy" answer, never a 500.
var ErrBusy = errors.New("ceoai: at capacity")

// Semaphore bounds concurrent in-flight assistant work (Vertex calls + the
// readonly DB pool) to protect finite downstream capacity. Acquire with a
// context deadline so queued work sheds instead of piling up.
type Semaphore struct {
	slots chan struct{}
}

// NewSemaphore builds a bounded semaphore. n<=0 disables bounding.
func NewSemaphore(n int) *Semaphore {
	if n <= 0 {
		return &Semaphore{}
	}
	return &Semaphore{slots: make(chan struct{}, n)}
}

// Acquire blocks until a slot is free or the context is done (shed => ErrBusy).
func (s *Semaphore) Acquire(ctx context.Context) error {
	if s.slots == nil {
		return nil
	}
	select {
	case s.slots <- struct{}{}:
		return nil
	default:
	}
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ErrBusy
	}
}

// Release frees a slot. Safe to call only after a successful Acquire.
func (s *Semaphore) Release() {
	if s.slots == nil {
		return
	}
	select {
	case <-s.slots:
	default:
	}
}

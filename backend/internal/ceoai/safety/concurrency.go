package safety

import (
	"context"
	"time"
)

// Semaphore bounds in-flight assistant work so unbounded concurrent Vertex/SQL
// calls cannot exhaust connection pools. It is a counting semaphore with a
// bounded acquire wait; past the wait it sheds load with ErrOverloaded rather
// than queueing unboundedly.
type Semaphore struct {
	tokens chan struct{}
	// acquireTimeout bounds how long a caller waits for a slot before load is
	// shed. Zero means fail-fast (no wait).
	acquireTimeout time.Duration
}

// SemaphoreConfig configures the bound.
type SemaphoreConfig struct {
	// MaxConcurrent is the number of simultaneous guarded operations allowed.
	MaxConcurrent int
	// AcquireTimeout is the max wait for a slot before shedding load.
	AcquireTimeout time.Duration
}

// DefaultSemaphoreConfig returns conservative defaults sized to protect the
// read-only DB pool and Vertex quota.
func DefaultSemaphoreConfig() SemaphoreConfig {
	return SemaphoreConfig{
		MaxConcurrent:  16,
		AcquireTimeout: 2 * time.Second,
	}
}

// NewSemaphore builds a Semaphore. MaxConcurrent < 1 is coerced to 1.
func NewSemaphore(cfg SemaphoreConfig) *Semaphore {
	n := cfg.MaxConcurrent
	if n < 1 {
		n = 1
	}
	s := &Semaphore{
		tokens:         make(chan struct{}, n),
		acquireTimeout: cfg.AcquireTimeout,
	}
	for i := 0; i < n; i++ {
		s.tokens <- struct{}{}
	}
	return s
}

// Acquire takes a slot, waiting up to AcquireTimeout (and honoring ctx
// cancellation). On success it returns a release func the caller MUST invoke
// (typically via defer). On timeout/shed it returns ErrOverloaded.
func (s *Semaphore) Acquire(ctx context.Context) (release func(), err error) {
	if s == nil {
		return func() {}, nil
	}
	// Fast path: a slot is immediately available.
	select {
	case <-s.tokens:
		return s.releaser(), nil
	default:
	}

	if s.acquireTimeout <= 0 {
		// Fail-fast shed.
		return nil, ErrOverloaded
	}

	timer := time.NewTimer(s.acquireTimeout)
	defer timer.Stop()
	select {
	case <-s.tokens:
		return s.releaser(), nil
	case <-timer.C:
		return nil, ErrOverloaded
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// releaser returns a single-use release closure that returns the token exactly
// once even if called multiple times.
func (s *Semaphore) releaser() func() {
	released := false
	return func() {
		if released {
			return
		}
		released = true
		select {
		case s.tokens <- struct{}{}:
		default:
			// Should never happen: capacity is fixed. Drop silently rather than
			// block, to avoid a leak wedging the caller.
		}
	}
}

// Available reports the number of free slots (for metrics/tests).
func (s *Semaphore) Available() int {
	if s == nil {
		return 0
	}
	return len(s.tokens)
}

// OverloadedVerdict is the friendly "busy" verdict to surface when load is shed.
func OverloadedVerdict() Verdict {
	return Verdict{
		Decision:    DecisionThrottle,
		Reason:      "backpressure:overloaded",
		RetryAfter:  time.Second,
		UserMessage: "The assistant is busy right now. Please try again in a few seconds.",
	}
}

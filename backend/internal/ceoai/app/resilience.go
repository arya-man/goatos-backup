package app

import (
	"context"
	"sync"
	"time"
)

// Breaker is a minimal per-dependency circuit breaker. After failureThreshold
// consecutive failures it opens for cooldown, causing calls to fail fast so the
// orchestrator can fall to the next tier or degrade gracefully instead of
// hanging. Never swallows errors into an empty answer.
//
// State machine:
//   - CLOSED (failures < threshold): every call is allowed.
//   - OPEN (failures >= threshold, within cooldown): calls are rejected.
//   - HALF-OPEN (cooldown elapsed): exactly ONE probe is admitted. Its
//     Success() closes the breaker; its Failure() re-opens for a fresh cooldown.
//
// The half-open probe gate is essential: without it, once the cooldown elapses
// every call would be admitted at full downstream-timeout latency against a
// still-dead dependency, and — because Failure() only ever advanced openedAt at
// the exact threshold — the breaker could never re-open. Failure() now refreshes
// openedAt on every failure while open, and Allow() admits only a single probe
// per cooldown window.
type Breaker struct {
	mu               sync.Mutex
	failures         int
	openedAt         time.Time
	probeInFlight    bool // a half-open probe has been admitted and not yet resolved
	failureThreshold int
	cooldown         time.Duration
	now              func() time.Time
}

// NewBreaker builds a breaker.
func NewBreaker(failureThreshold int, cooldown time.Duration) *Breaker {
	return &Breaker{failureThreshold: failureThreshold, cooldown: cooldown, now: time.Now}
}

// Allow reports whether a call may proceed. Closed => always; open within
// cooldown => never; half-open (cooldown elapsed) => exactly one probe.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < b.failureThreshold {
		return true
	}
	if b.now().Sub(b.openedAt) < b.cooldown {
		return false // still open
	}
	// Half-open: admit a single probe until it resolves via Success/Failure.
	if b.probeInFlight {
		return false
	}
	b.probeInFlight = true
	return true
}

// Success resets the failure count and closes the breaker.
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.probeInFlight = false
}

// Failure records a failure and (re)opens the breaker. openedAt is refreshed on
// every failure at/above the threshold so a persistently-down dependency keeps
// the breaker open (fresh cooldown per failed probe), and the half-open probe
// slot is released so the next cooldown window can admit one more probe.
func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.failureThreshold {
		b.openedAt = b.now()
	}
	b.probeInFlight = false
}

// retryTransient runs fn up to attempts times with linear backoff, honoring
// ctx. It returns the last error. Used for transient tool/planner failures.
func retryTransient(ctx context.Context, attempts int, backoff time.Duration, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if i < attempts-1 {
			select {
			case <-time.After(backoff * time.Duration(i+1)):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return err
}

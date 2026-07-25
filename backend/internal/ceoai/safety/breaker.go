package safety

import (
	"context"
	"errors"
	"sync"
	"time"
)

// BreakerState is the circuit-breaker state machine.
type BreakerState int

const (
	// BreakerClosed lets calls through and counts failures.
	BreakerClosed BreakerState = iota
	// BreakerOpen fast-fails all calls until the cooldown elapses.
	BreakerOpen
	// BreakerHalfOpen lets a single probe through to test recovery.
	BreakerHalfOpen
)

func (s BreakerState) String() string {
	switch s {
	case BreakerClosed:
		return "closed"
	case BreakerOpen:
		return "open"
	case BreakerHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// BreakerConfig tunes the breaker around a single dependency (e.g. Vertex).
type BreakerConfig struct {
	// FailureThreshold is the number of consecutive failures that trips the
	// breaker open.
	FailureThreshold int
	// Cooldown is how long the breaker stays open before allowing a half-open
	// probe.
	Cooldown time.Duration
	// HalfOpenSuccesses is the number of consecutive successful probes required
	// to close the breaker again.
	HalfOpenSuccesses int
	// CallTimeout bounds a single guarded call; exceeding it counts as a
	// failure. Zero disables the per-call timeout.
	CallTimeout time.Duration
}

// DefaultBreakerConfig returns Vertex-oriented defaults.
func DefaultBreakerConfig() BreakerConfig {
	return BreakerConfig{
		FailureThreshold:  5,
		Cooldown:          15 * time.Second,
		HalfOpenSuccesses: 2,
		CallTimeout:       20 * time.Second,
	}
}

// CircuitBreaker guards a dependency and degrades deterministically when it is
// failing or slow. It is safe for concurrent use.
type CircuitBreaker struct {
	cfg   BreakerConfig
	clock Clock
	name  string

	mu             sync.Mutex
	state          BreakerState
	consecFailures int
	halfOpenOK     int
	openedAt       time.Time
	// probeInFlight ensures only one half-open probe runs at a time.
	probeInFlight bool
}

// NewCircuitBreaker builds a breaker for a named dependency.
func NewCircuitBreaker(name string, cfg BreakerConfig, clock Clock) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.HalfOpenSuccesses <= 0 {
		cfg.HalfOpenSuccesses = 1
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 15 * time.Second
	}
	return &CircuitBreaker{cfg: cfg, clock: clock, name: name, state: BreakerClosed}
}

// State returns the current state (evaluating cooldown expiry).
func (b *CircuitBreaker) State() BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evaluateLocked()
	return b.state
}

// Execute runs fn under the breaker. If the breaker is open it returns
// ErrBreakerOpen immediately without calling fn. A CallTimeout is enforced via
// context. Success/failure updates the state machine.
func (b *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	admitted, halfOpenProbe := b.admit()
	if !admitted {
		return ErrBreakerOpen
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if b.cfg.CallTimeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, b.cfg.CallTimeout)
		defer cancel()
	}

	err := fn(callCtx)
	// A deadline exceeded on our call timeout is a dependency failure signal.
	if err != nil || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		b.onFailure(halfOpenProbe)
		if err == nil {
			return context.DeadlineExceeded
		}
		return err
	}
	b.onSuccess(halfOpenProbe)
	return nil
}

// admit decides whether a call may proceed and whether it is the half-open
// probe. It transitions Open->HalfOpen when cooldown elapses.
func (b *CircuitBreaker) admit() (admitted bool, halfOpenProbe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evaluateLocked()
	switch b.state {
	case BreakerClosed:
		return true, false
	case BreakerHalfOpen:
		if b.probeInFlight {
			// Only one probe at a time; others fast-fail as degraded.
			return false, false
		}
		b.probeInFlight = true
		return true, true
	default: // BreakerOpen
		return false, false
	}
}

// evaluateLocked promotes Open->HalfOpen once the cooldown has elapsed. Caller
// must hold the lock.
func (b *CircuitBreaker) evaluateLocked() {
	if b.state == BreakerOpen && b.clock.now().Sub(b.openedAt) >= b.cfg.Cooldown {
		b.state = BreakerHalfOpen
		b.halfOpenOK = 0
		b.probeInFlight = false
	}
}

func (b *CircuitBreaker) onSuccess(halfOpenProbe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if halfOpenProbe {
		b.probeInFlight = false
	}
	switch b.state {
	case BreakerHalfOpen:
		b.halfOpenOK++
		if b.halfOpenOK >= b.cfg.HalfOpenSuccesses {
			b.state = BreakerClosed
			b.consecFailures = 0
			b.halfOpenOK = 0
		}
	default:
		b.consecFailures = 0
	}
}

func (b *CircuitBreaker) onFailure(halfOpenProbe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if halfOpenProbe {
		b.probeInFlight = false
	}
	switch b.state {
	case BreakerHalfOpen:
		// A failed probe re-opens immediately.
		b.tripLocked()
	default:
		b.consecFailures++
		if b.consecFailures >= b.cfg.FailureThreshold {
			b.tripLocked()
		}
	}
}

func (b *CircuitBreaker) tripLocked() {
	b.state = BreakerOpen
	b.openedAt = b.clock.now()
	b.halfOpenOK = 0
	b.probeInFlight = false
}

// DegradeVerdict is the honest degraded-mode verdict to surface when the breaker
// is open. Never an empty answer.
func DegradeVerdict(dependency string) Verdict {
	return Verdict{
		Decision:    DecisionDegrade,
		Reason:      "circuit_open:" + dependency,
		UserMessage: "The assistant is temporarily unavailable while a backend service recovers. Please try again shortly.",
	}
}

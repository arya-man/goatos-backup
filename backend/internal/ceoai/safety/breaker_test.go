package safety

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBreakerOpensAfterThreshold(t *testing.T) {
	clk := fixedClock(time.Now())
	b := NewCircuitBreaker("dep", BreakerConfig{FailureThreshold: 3, Cooldown: time.Second, HalfOpenSuccesses: 1}, clk)
	fail := func(context.Context) error { return errors.New("boom") }

	for i := 0; i < 3; i++ {
		if err := b.Execute(context.Background(), fail); err == nil {
			t.Fatalf("call %d should return the failure", i)
		}
	}
	if b.State() != BreakerOpen {
		t.Fatalf("breaker should be open after threshold, got %s", b.State())
	}
	// While open, fn is NOT called and ErrBreakerOpen is returned.
	called := false
	err := b.Execute(context.Background(), func(context.Context) error { called = true; return nil })
	if err != ErrBreakerOpen {
		t.Fatalf("expected ErrBreakerOpen, got %v", err)
	}
	if called {
		t.Fatal("fn must not be called while breaker is open")
	}
}

func TestBreakerHalfOpenRecovers(t *testing.T) {
	var now time.Time
	clk := func() time.Time { return now }
	now = time.Unix(1000, 0)
	b := NewCircuitBreaker("dep", BreakerConfig{FailureThreshold: 1, Cooldown: 10 * time.Second, HalfOpenSuccesses: 2}, clk)

	// Trip open.
	_ = b.Execute(context.Background(), func(context.Context) error { return errors.New("x") })
	if b.State() != BreakerOpen {
		t.Fatal("should be open")
	}
	// Before cooldown: still open.
	now = now.Add(5 * time.Second)
	if b.State() != BreakerOpen {
		t.Fatal("should still be open before cooldown")
	}
	// After cooldown: half-open, probe allowed.
	now = now.Add(6 * time.Second)
	if b.State() != BreakerHalfOpen {
		t.Fatalf("should be half-open after cooldown, got %s", b.State())
	}
	// First successful probe: still half-open (needs 2).
	if err := b.Execute(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("probe should succeed: %v", err)
	}
	if b.State() != BreakerHalfOpen {
		t.Fatalf("still half-open after 1 success, got %s", b.State())
	}
	// Second success: closed.
	if err := b.Execute(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("probe should succeed: %v", err)
	}
	if b.State() != BreakerClosed {
		t.Fatalf("should close after required successes, got %s", b.State())
	}
}

func TestBreakerHalfOpenFailureReopens(t *testing.T) {
	var now time.Time
	clk := func() time.Time { return now }
	now = time.Unix(1000, 0)
	b := NewCircuitBreaker("dep", BreakerConfig{FailureThreshold: 1, Cooldown: 5 * time.Second, HalfOpenSuccesses: 1}, clk)

	_ = b.Execute(context.Background(), func(context.Context) error { return errors.New("x") })
	now = now.Add(6 * time.Second) // -> half-open
	if b.State() != BreakerHalfOpen {
		t.Fatal("expected half-open")
	}
	// Failing probe re-opens immediately.
	_ = b.Execute(context.Background(), func(context.Context) error { return errors.New("still bad") })
	if b.State() != BreakerOpen {
		t.Fatalf("failed probe should reopen, got %s", b.State())
	}
}

func TestBreakerCallTimeoutCountsAsFailure(t *testing.T) {
	clk := fixedClock(time.Now())
	b := NewCircuitBreaker("dep", BreakerConfig{FailureThreshold: 1, Cooldown: time.Second, CallTimeout: 20 * time.Millisecond}, clk)
	slow := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			return nil
		}
	}
	err := b.Execute(context.Background(), slow)
	if err == nil {
		t.Fatal("slow call should error on timeout")
	}
	if b.State() != BreakerOpen {
		t.Fatalf("timeout should count as failure and open, got %s", b.State())
	}
}

func TestDegradeVerdict(t *testing.T) {
	v := DegradeVerdict("vertex")
	if v.Decision != DecisionDegrade || v.UserMessage == "" {
		t.Fatal("degrade verdict must be honest and non-empty")
	}
}

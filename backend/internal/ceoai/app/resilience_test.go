package app

import (
	"testing"
	"time"
)

// TestBreakerHalfOpenReopensOnPersistentFailure reproduces the stuck-half-open
// defect: once the first cooldown elapsed the old breaker admitted EVERY call
// forever (no single-probe gate) and never refreshed openedAt past the exact
// threshold, so it could never re-open against a persistently-down dependency.
func TestBreakerHalfOpenReopensOnPersistentFailure(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBreaker(2, 10*time.Millisecond)
	b.now = func() time.Time { return now }

	// Trip it open.
	b.Failure()
	b.Failure()
	if b.Allow() {
		t.Fatal("breaker should be OPEN immediately after reaching threshold")
	}

	// Cooldown elapses -> exactly ONE probe admitted, not a flood.
	now = now.Add(11 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("half-open should admit the first probe")
	}
	if b.Allow() {
		t.Fatal("half-open must admit only a SINGLE probe until it resolves")
	}

	// The probe fails: breaker must RE-OPEN for a fresh cooldown (the old code
	// never refreshed openedAt past the threshold, so this stayed open-forever-
	// allow-everything).
	b.Failure()
	if b.Allow() {
		t.Fatal("breaker must re-open after a failed probe, not stay permanently half-open")
	}

	// Next cooldown -> one probe -> success closes it.
	now = now.Add(11 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("second half-open window should admit a probe")
	}
	b.Success()
	if !b.Allow() {
		t.Fatal("breaker should be CLOSED after a successful probe")
	}
}

package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Two of 43 held-out questions were answered in FALLBACK mode while the other
// 41 planned: the planner was briefly unavailable and two attempts 150ms apart
// was not long enough to ride it out. A leader must not get a hard-coded metric
// because Vertex hiccupped for a quarter of a second.
func TestABriefPlannerOutageIsRiddenOutRatherThanFallenFrom(t *testing.T) {
	calls := 0
	err := retryTransientIf(context.Background(), 3, time.Millisecond, transientPlannerError, func() error {
		calls++
		if calls < 3 {
			return errors.New("vertex: status 503: model overloaded")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("a blip that cleared on the third attempt still failed: %v", err)
	}
	if calls != 3 {
		t.Errorf("attempts = %d, want the planner given three tries", calls)
	}
}

// A permanent rejection is not a blip. Asking twice more spends a second of a
// leader's wait on the same answer.
func TestAPermanentRejectionIsNotRetried(t *testing.T) {
	calls := 0
	err := retryTransientIf(context.Background(), 3, time.Millisecond, transientPlannerError, func() error {
		calls++
		return errors.New("vertex: status 400: invalid request schema")
	})
	if err == nil {
		t.Fatal("a rejected request reported success")
	}
	if calls != 1 {
		t.Errorf("a permanent 400 was tried %d times", calls)
	}
}

func TestPlannerFailuresAreClassified(t *testing.T) {
	for _, e := range []string{
		"vertex: status 429: rate limited", "vertex: status 500: internal",
		"vertex: empty candidate", "context deadline exceeded", "read: connection reset by peer",
	} {
		if !transientPlannerError(errors.New(e)) {
			t.Errorf("%q should be worth another try", e)
		}
	}
	for _, e := range []string{
		"vertex: status 401: unauthorized", "vertex: status 403: permission denied",
		"vertex: status 404: model not found",
	} {
		if transientPlannerError(errors.New(e)) {
			t.Errorf("%q is permanent and must not be retried", e)
		}
	}
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetryOnDeadlockRetriesOnlyDeadlocksAndIsBounded(t *testing.T) {
	deadlock := fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: SQLStateDeadlockDetected})
	calls := 0
	err := RetryOnDeadlock(context.Background(), func(context.Context) error {
		calls++
		if calls < 2 {
			return deadlock
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("recovering deadlock: err=%v calls=%d, want nil/2", err, calls)
	}
	calls = 0
	err = RetryOnDeadlock(context.Background(), func(context.Context) error { calls++; return deadlock })
	if !IsDeadlock(err) || calls != deadlockRetryAttempts {
		t.Fatalf("persistent deadlock: err=%v calls=%d, want 40P01 after %d", err, calls, deadlockRetryAttempts)
	}
	calls = 0
	other := errors.New("boom")
	if err := RetryOnDeadlock(context.Background(), func(context.Context) error { calls++; return other }); !errors.Is(err, other) || calls != 1 {
		t.Fatalf("non-deadlock must not retry: err=%v calls=%d", err, calls)
	}
}

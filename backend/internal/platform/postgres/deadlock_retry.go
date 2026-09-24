package postgres

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// SQLStateDeadlockDetected is PostgreSQL's deadlock_detected (40P01).
const SQLStateDeadlockDetected = "40P01"

// deadlockRetryAttempts bounds RetryOnDeadlock: the first try plus two retries.
const deadlockRetryAttempts = 3

// IsDeadlock reports whether err is (or wraps) a PostgreSQL 40P01.
func IsDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == SQLStateDeadlockDetected
}

// RetryOnDeadlock runs fn -- which must own its WHOLE transaction (begin..commit) so a retry
// replays it from scratch -- and re-runs it when PostgreSQL aborted it as a deadlock victim.
// Bounded (3 attempts) with jittered backoff (20-60 ms, then 40-120 ms) so two victims do not
// collide again in lockstep. Any other error, or ctx ending, returns at once.
//
// It is a BACKSTOP: lock ordering is what prevents deadlocks (e.g. migration 000403's counter
// rows are locked in sorted order, one counter-touching statement per transaction).
func RetryOnDeadlock(ctx context.Context, fn func(context.Context) error) error {
	var err error
	for attempt := 1; attempt <= deadlockRetryAttempts; attempt++ {
		err = fn(ctx)
		if err == nil || !IsDeadlock(err) || attempt == deadlockRetryAttempts {
			return err
		}
		base := time.Duration(attempt) * 20 * time.Millisecond
		wait := base + time.Duration(rand.Int64N(int64(2*base)))
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
	}
	return err
}

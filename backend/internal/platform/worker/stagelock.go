package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// heldLock pairs a dedicated pooled connection with the advisory-lock salt it
// holds. The salt is retained so the lock can be explicitly released with
// pg_advisory_unlock BEFORE the connection is returned to the pool.
type heldLock struct {
	conn *pgxpool.Conn
	salt int64
}

// stageLockStore holds acquired connections keyed by stage name.
// When a stage acquires a lock, the dedicated connection is stored here
// and released only when the stage completes (via ReleaseStageLock).
// This prevents the connection from returning to the pool while the lock
// is still held (which would release the lock on an arbitrary pooled conn).
// Access is protected by stageLockMu.
var (
	stageLockStore = make(map[string]*heldLock)
	stageLockMu    sync.Mutex
)

// AcquireStageLock attempts to acquire a Postgres session-level advisory lock
// for the given stage. If the lock is acquired, it is held on a DEDICATED
// connection obtained via pool.Acquire(), and the connection is stored
// internally to be released by ReleaseStageLock. This ensures the session
// lock is not released when the connection is returned to the pool.
//
// REQ-2 CRITICAL: The lock is held only while the dedicated connection is
// held. If the connection dies or is closed, the lock is released by Postgres.
// pgxpool does NOT reset session state on Release, so a graceful release MUST
// explicitly pg_advisory_unlock (see ReleaseStageLock) — merely returning the
// conn to the pool would leave the session lock live on that pooled conn.
//
// Returns (true, nil) if the lock was acquired, (false, nil) if the lock
// was already held by another session, or (false, err) if an error occurred.
func AcquireStageLock(ctx context.Context, pool *pgxpool.Pool, lockSalt int64, stageName string) (bool, error) {
	if pool == nil {
		return false, fmt.Errorf("pool is nil")
	}

	// Acquire a dedicated connection from the pool.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire connection for stage lock: %w", err)
	}

	// Try to acquire the advisory lock on this dedicated connection.
	// pg_try_advisory_lock(bigint) returns true if the lock was acquired,
	// false if it was already held by another session.
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockSalt).Scan(&locked); err != nil {
		conn.Release()
		return false, fmt.Errorf("execute advisory lock query: %w", err)
	}

	if !locked {
		// Another session holds the lock; release the connection and return.
		conn.Release()
		return false, nil
	}

	// Lock acquired; store the connection (and its salt) so it is not returned
	// to the pool until ReleaseStageLock is called.
	stageLockMu.Lock()
	stageLockStore[stageName] = &heldLock{conn: conn, salt: lockSalt}
	stageLockMu.Unlock()

	return true, nil
}

// ReleaseStageLock releases the advisory lock for the given stage. It first
// issues an explicit pg_advisory_unlock on the dedicated connection that holds
// the lock, then returns that connection to the pool.
//
// REQ-2 CRITICAL: The explicit unlock is required. pgxpool does not reset
// session state on Release, so a session-level advisory lock acquired with
// pg_try_advisory_lock persists for the life of the physical connection. If we
// relied on conn.Release() alone, the lock would leak onto the pooled conn and
// subsequent AcquireStageLock calls on that reused conn would re-enter the same
// lock (or fail on a different conn), silently breaking serialization.
func ReleaseStageLock(ctx context.Context, pool *pgxpool.Pool, stageName string) error {
	stageLockMu.Lock()
	held, exists := stageLockStore[stageName]
	if exists {
		delete(stageLockStore, stageName)
	}
	stageLockMu.Unlock()

	if !exists {
		return fmt.Errorf("no lock held for stage %q", stageName)
	}

	// Explicitly release the session-level advisory lock BEFORE returning the
	// connection to the pool. Use a bounded timeout so a wedged connection does
	// not block the release path indefinitely. On a crashed/dead connection the
	// unlock fails (Postgres already released the lock on connection death); we
	// still return the connection to the pool so pgxpool can reap it.
	unlockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, unlockErr := held.conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", held.salt)
	held.conn.Release()
	if unlockErr != nil {
		return fmt.Errorf("release advisory lock for stage %q: %w", stageName, unlockErr)
	}
	return nil
}

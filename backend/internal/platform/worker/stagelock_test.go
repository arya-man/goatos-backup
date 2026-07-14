package worker

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// secondPoolSameDB opens a second, independent pgx pool to the SAME database
// as base. This simulates a second worker process/instance: a distinct set of
// Postgres sessions against one database, which is what advisory-lock
// serialization must be proven against. Config() returns a copy of base's
// config, so the new pool connects to the same DSN with its own connections.
func secondPoolSameDB(t *testing.T, ctx context.Context, base *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.NewWithConfig(ctx, base.Config())
	if err != nil {
		t.Fatalf("open second pool to same db: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// TestStageLockSerializesAcrossSessions verifies that the advisory-lock salt
// serializes to exactly one holder across two independent sessions (two pools
// to the same database), and that a second session can acquire only AFTER the
// first releases. This is the core serialization + release guarantee and, by
// using a DIFFERENT session for the second acquire, it genuinely proves the
// lock was released (a same-connection re-acquire would falsely pass because a
// session advisory lock is re-entrant on its own connection).
func TestStageLockSerializesAcrossSessions(t *testing.T) {
	t.Parallel()
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	poolA := pgtest.StartPostgres(t, ctx)
	poolB := secondPoolSameDB(t, ctx, poolA)

	salt := int64(91000)

	// Session A acquires the lock.
	lockedA, err := AcquireStageLock(ctx, poolA, salt, "instance-a")
	if err != nil {
		t.Fatalf("A acquire failed: %v", err)
	}
	if !lockedA {
		t.Fatal("A should have acquired the lock")
	}

	// Session B (a different pool/session) must NOT be able to acquire while A holds it.
	lockedB, err := AcquireStageLock(ctx, poolB, salt, "instance-b")
	if err != nil {
		t.Fatalf("B acquire (while A holds) failed: %v", err)
	}
	if lockedB {
		t.Fatal("B acquired the lock while A holds it — serialization broken")
	}

	// A releases. With the explicit pg_advisory_unlock, the lock must now be
	// free for a DIFFERENT session. If release relied only on conn.Release()
	// (the REQ-2 bug), the lock would leak on A's pooled conn and B would still
	// fail to acquire below.
	if err := ReleaseStageLock(ctx, poolA, "instance-a"); err != nil {
		t.Fatalf("A release failed: %v", err)
	}

	lockedB2, err := AcquireStageLock(ctx, poolB, salt, "instance-b")
	if err != nil {
		t.Fatalf("B re-acquire (after A released) failed: %v", err)
	}
	if !lockedB2 {
		t.Fatal("B could not acquire after A released — lock leaked (REQ-2 not satisfied)")
	}

	if err := ReleaseStageLock(ctx, poolB, "instance-b"); err != nil {
		t.Fatalf("B release failed: %v", err)
	}
}

// acquireStageLockWithin polls AcquireStageLock until it succeeds or the
// deadline elapses. Postgres frees a session-level advisory lock only once it
// finishes reaping the terminated backend, and that reaping is ASYNCHRONOUS to
// the client-side connection close — a standby that polls on an already-warm
// pooled connection can beat the reaper by a few milliseconds and observe the
// lock still held. This mirrors the production standby, which retries
// AcquireStageLock on its next cadence tick (see Supervisor.runCadence /
// runStageOnce and TestFailoverTakeoverIsIdempotent) rather than giving up on a
// single miss. If the lock never frees within the deadline the loop returns
// false and the caller fails the test — the release guarantee is still proven,
// just with realistic (bounded) timing tolerance.
func acquireStageLockWithin(t *testing.T, ctx context.Context, pool *pgxpool.Pool, salt int64, stageName string, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		locked, err := AcquireStageLock(ctx, pool, salt, stageName)
		if err != nil {
			t.Fatalf("acquire stage lock %q: %v", stageName, err)
		}
		if locked {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestStageLockReleasedOnConnectionDeath verifies that Postgres releases the
// session-level advisory lock when the holding connection dies (crash path),
// so a standby session can take over. It acquires the lock on a dedicated
// connection, hijacks it out of the pool and closes it (simulating process/TCP
// death), then proves a second session can acquire the same salt.
//
// Backend reaping is asynchronous to the connection close, so the standby uses a
// bounded retry (acquireStageLockWithin) exactly as the production supervisor
// does on its cadence ticks — a single immediate poll can lose the race with the
// reaper. The test still fails closed: if the lock is never released within the
// timeout, acquireStageLockWithin returns false and the assertion below trips.
func TestStageLockReleasedOnConnectionDeath(t *testing.T) {
	t.Parallel()
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	poolA := pgtest.StartPostgres(t, ctx)
	poolB := secondPoolSameDB(t, ctx, poolA)

	salt := int64(91001)

	// Acquire the lock directly on a dedicated connection from poolA.
	connA, err := poolA.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	var locked bool
	if err := connA.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", salt).Scan(&locked); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}
	if !locked {
		t.Fatal("expected to acquire lock on fresh session")
	}

	// A second session must not be able to acquire while A's session is alive.
	if lockedB, err := AcquireStageLock(ctx, poolB, salt, "b-before-death"); err != nil {
		t.Fatalf("B acquire (A alive) failed: %v", err)
	} else if lockedB {
		t.Fatal("B acquired while A's session alive — serialization broken")
	}

	// Simulate A crashing: hijack the connection out of the pool and close the
	// underlying session. Postgres releases session advisory locks on death.
	raw := connA.Hijack()
	_ = raw.Close(ctx)

	// The standby session must now be able to acquire the lock. Retry with a
	// bounded deadline: Postgres reaps the dead backend (and frees its session
	// advisory lock) asynchronously, so a warm-pool single-shot poll can race
	// ahead of the reaper. A few seconds is far beyond the observed reap lag
	// (single-digit milliseconds) yet still fails closed on a genuine leak.
	if !acquireStageLockWithin(t, ctx, poolB, salt, "b-after-death", 5*time.Second) {
		t.Fatal("lock not released after holding connection died — standby cannot take over")
	}
	if err := ReleaseStageLock(ctx, poolB, "b-after-death"); err != nil {
		t.Fatalf("B release failed: %v", err)
	}
}

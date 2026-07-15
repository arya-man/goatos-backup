package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

// visitShotLockNamespace prefixes every per-visit advisory-lock key so it cannot collide with
// other advisory-lock users in the database (e.g. CreateBatchWithObligations' own scope lock, or
// platform/worker's kernel-stage lock).
const visitShotLockNamespace = "goatos:obligation:visit-shot:"

// visitShotLockKeyArg is the stable advisory-lock key argument for one (tenant, target, date)
// visit. It is a pure function of its inputs, so any two processes (two sweep passes, two
// concurrent worker replicas) compute the identical Postgres advisory-lock key for the same
// animal's visit and therefore mutually exclude regardless of process identity or call order.
func visitShotLockKeyArg(tenantID, targetID, dateKey string) string {
	return visitShotLockNamespace + tenantID + ":" + targetID + ":" + dateKey
}

// pgxQuerier is satisfied by both *pgxpool.Pool and *pgxpool.Conn, letting countVisitShots run
// identically whether or not the caller already holds a dedicated locked connection.
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// countVisitShots reads the persisted, already-committed shot count per target for one planned
// visit date: obligations attached (via batch_id) to a NON-canceled, non-superseded batch whose
// planned_date equals date. Bounded by the caller's explicit targetIDs (never a full-table scan),
// so it stays scale-safe at 1-5M animals regardless of how many total obligations/batches exist
// for the tenant.
func countVisitShots(ctx context.Context, q pgxQuerier, tenantID string, targetIDs []string, date time.Time) (map[string]int32, error) {
	out := make(map[string]int32, len(targetIDs))
	if len(targetIDs) == 0 {
		return out, nil
	}
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	ids, err := obligationUUIDs(targetIDs)
	if err != nil {
		return nil, fmt.Errorf("obligation: visit shot target ids: %w", err)
	}
	day := biztime.BusinessDayStart(date)
	rows, err := q.Query(ctx, `
SELECT oi.target_id::text, count(*)::int
FROM obligation_instances oi
JOIN obligation_batches ob
  ON ob.tenant_id = oi.tenant_id
 AND ob.batch_id = oi.batch_id
WHERE oi.tenant_id = $1
  AND oi.target_id = ANY($2::uuid[])
  AND ob.planned_date = $3::date
  AND ob.status NOT IN ('canceled', 'superseded')
  AND oi.status NOT IN ('canceled')
GROUP BY oi.target_id`, tenant, ids, pgconv.Date(&day))
	if err != nil {
		return nil, fmt.Errorf("obligation: count visit shots: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var targetID string
		var count int32
		if err := rows.Scan(&targetID, &count); err != nil {
			return nil, fmt.Errorf("obligation: scan visit shot count: %w", err)
		}
		out[targetID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: visit shot count rows: %w", err)
	}
	return out, nil
}

// CountVisitShotsForTargets is the read-only half of the VAX-REV-01 fix (see
// app.visitShotCounter): the persisted shot count already committed for each of targetIDs on one
// planned visit date, from ANY prior sweep pass. Used to seed a fresh SweepSession so
// MaxShotsPerAnimalPerDrive is enforced across passes, not just within one. Safe to call without
// any lock -- callers that also need write-time atomicity against concurrent workers use
// LockVisitShots instead.
func (r *Repository) CountVisitShotsForTargets(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (map[string]int32, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return countVisitShots(ctx, r.pool, tenantID, targetIDs, date)
}

// LockVisitShots is the write-path half of the VAX-REV-01 fix (see app.visitShotLocker). It takes
// a session-level Postgres advisory lock (pg_advisory_lock, hashtext(visitShotLockKeyArg(...)))
// for every distinct target in targetIDs, in ascending sorted order (a stable order shared by
// every caller, so two workers racing over an overlapping target set always attempt their locks
// in the same order and cannot deadlock each other), on ONE dedicated connection acquired from the
// pool. It then reads the fresh persisted shot count for those now-locked targets and returns it
// alongside a release func.
//
// The lock is held on the dedicated connection until the caller invokes release, NOT scoped to
// any one transaction -- the caller's subsequent CreateBatchWithObligations call runs in its own,
// separate transaction/connection, and the advisory lock (a global, connection-independent
// Postgres primitive) is what actually serializes a concurrent caller attempting the very same
// (tenant, target, date) key, regardless of which connection eventually performs the write. A
// second caller's own LockVisitShots call for an overlapping key blocks on pg_advisory_lock until
// this caller's release() runs, so it always observes the first caller's committed write in its
// own fresh count read.
func (r *Repository) LockVisitShots(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (map[string]int32, func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	keys := dedupNonBlank(targetIDs)
	if len(keys) == 0 {
		return map[string]int32{}, noop, nil
	}
	sort.Strings(keys)
	dateKey := biztime.BusinessDayStart(date).Format("2006-01-02")

	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("obligation: acquire visit-shot lock connection: %w", err)
	}

	lockArgs := make([]string, 0, len(keys))
	for _, targetID := range keys {
		keyArg := visitShotLockKeyArg(tenantID, targetID, dateKey)
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", keyArg); err != nil {
			_ = unlockAndRelease(context.Background(), conn, lockArgs)
			return nil, nil, fmt.Errorf("obligation: lock visit shot for target %s: %w", targetID, err)
		}
		lockArgs = append(lockArgs, keyArg)
	}

	release := func(releaseCtx context.Context) error {
		return unlockAndRelease(releaseCtx, conn, lockArgs)
	}

	counts, err := countVisitShots(ctx, conn, tenantID, keys, date)
	if err != nil {
		_ = release(context.Background())
		return nil, nil, err
	}
	return counts, release, nil
}

// unlockAndRelease releases every held advisory lock (best-effort: it keeps going on error so one
// failed unlock cannot leak the rest) and returns the dedicated connection to the pool. The first
// error, if any, is returned so a caller can still tell a release genuinely failed.
func unlockAndRelease(ctx context.Context, conn *pgxpool.Conn, keyArgs []string) error {
	var firstErr error
	for _, keyArg := range keyArgs {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtext($1))", keyArg); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("obligation: unlock visit shot: %w", err)
		}
	}
	conn.Release()
	return firstErr
}

func dedupNonBlank(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

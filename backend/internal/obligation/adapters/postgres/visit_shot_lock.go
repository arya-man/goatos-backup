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
	// projection-review: membership=obligation_instances attached via oi.batch_id to obligation_batches (same tenant); group_key=oi.target_id for a single planned_date; join_cardinality=1:1 (each oi carries one batch_id and obligation_batches is keyed by (tenant_id, batch_id), so the JOIN is a semijoin to the batch's planned_date/status and COUNT(*) counts obligation rows, never a batch fan-out); pagination=bounded by the caller's explicit target_ids + one planned_date, computed whole (no user page — this is the cap-enforcement count, not a paged UI projection); scope=n/a (explicit target-id list, not a park/shed/cohort hierarchy)
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
		lockArgs = append(lockArgs, visitShotLockKeyArg(tenantID, targetID, dateKey))
	}
	// Acquire every per-visit advisory lock in ONE round trip. unnest preserves array element order
	// and lockArgs is built from the already-sorted key set, so every caller acquires any shared
	// locks in the same global order and cannot deadlock. pg_advisory_lock is parallel-unsafe, so the
	// function scan runs sequentially in array order -- this is the batched, single-statement
	// equivalent of a per-target lock loop, without the per-target round trips.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext(k)) FROM unnest($1::text[]) AS t(k)", lockArgs); err != nil {
		_ = releaseVisitShotConn(context.Background(), conn)
		return nil, nil, fmt.Errorf("obligation: lock visit shots: %w", err)
	}

	release := func(releaseCtx context.Context) error {
		return releaseVisitShotConn(releaseCtx, conn)
	}

	counts, err := countVisitShots(ctx, conn, tenantID, keys, date)
	if err != nil {
		_ = release(context.Background())
		return nil, nil, err
	}
	return counts, release, nil
}

// tenantSweepLockNamespace prefixes the single per-tenant whole-sweep advisory-lock key so it
// cannot collide with the per-visit locks or any other advisory-lock user.
const tenantSweepLockNamespace = "goatos:obligation:tenant-sweep:"

// LockTenantSweep takes ONE tenant-scoped session advisory lock that serializes the ENTIRE
// obligation sweep (preflight + every version) for a tenant across processes (RV-03). It uses
// pg_try_advisory_lock (non-blocking): if another sweeper already holds it -- a second kernel-worker
// replica, or the standalone cmd/obligation-sweeper running alongside the kernel stage -- acquired
// is false and the caller SKIPS this run, since the in-progress writer already covers the tenant.
// When acquired, this sweep is the single priority-ordered writer for the tenant, so its in-memory
// priority arbitration (SortSweepVersionsByPriority + SweepSession.visitClaims) is authoritative and
// no concurrent lower-priority writer can commit a competing shot mid-sweep and invert the medical
// plan by lock-acquisition order. The caller MUST call the returned release exactly once.
func (r *Repository) LockTenantSweep(ctx context.Context, tenantID string) (bool, func(context.Context) error, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("obligation: acquire tenant-sweep lock connection: %w", err)
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", tenantSweepLockNamespace+tenantID).Scan(&acquired); err != nil {
		conn.Release()
		return false, nil, fmt.Errorf("obligation: try tenant-sweep lock: %w", err)
	}
	if !acquired {
		// No advisory lock is held on this connection, so a plain Release (no unlock) is correct.
		conn.Release()
		return false, func(context.Context) error { return nil }, nil
	}
	return true, func(releaseCtx context.Context) error { return releaseVisitShotConn(releaseCtx, conn) }, nil
}

// visitShotUnlockTimeout bounds the independent cleanup context releaseVisitShotConn uses so a
// canceled caller context can never skip pg_advisory_unlock_all and leak session locks (RV-05).
const visitShotUnlockTimeout = 5 * time.Second

// releaseVisitShotConn releases every session advisory lock this dedicated connection holds in ONE
// round trip (pg_advisory_unlock_all frees all locks the session owns) and returns the connection
// to the pool. Using unlock_all rather than a per-key unlock loop keeps release a single call and
// cannot leak a lock on the pooled connection regardless of how large the key set was.
//
// RV-05: the unlock runs on an independent, bounded context derived with context.WithoutCancel, so
// a caller whose context is already canceled (a shut-down sweep, a timed-out request) still frees
// its session locks instead of silently skipping the unlock. If the unlock nonetheless fails, the
// connection may still own session advisory locks; returning it to the pool would leave an
// invisible lock that blocks every later worker on that visit key forever, so it is DESTROYED
// (hijacked out of the pool and closed) rather than released back.
func releaseVisitShotConn(ctx context.Context, conn *pgxpool.Conn) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), visitShotUnlockTimeout)
	defer cancel()
	if _, err := conn.Exec(cleanupCtx, "SELECT pg_advisory_unlock_all()"); err != nil {
		hijacked := conn.Hijack()
		_ = hijacked.Close(cleanupCtx)
		return fmt.Errorf("obligation: unlock visit shots (connection destroyed to avoid leaking session locks): %w", err)
	}
	conn.Release()
	return nil
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

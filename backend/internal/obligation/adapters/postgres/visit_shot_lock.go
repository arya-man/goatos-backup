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

// driveCapacityLockNamespace serializes the whole park/date vaccination drive capacity ledger.
// The unit is vaccine administrations/dose cells for one tenant+park+planned_date, across all
// sheds, vaccines, and planner paths.
const driveCapacityLockNamespace = "goatos:obligation:drive-capacity:"

// canonicalUUID parses id and re-renders it in Postgres's canonical lowercase-hex form (RV-03).
// hashtext() -- used by every advisory-lock key in this file -- hashes the RAW BYTES of its input
// text argument, so two textually different representations of the SAME Postgres uuid value (e.g.
// an uppercase-hex tenant id one caller passes vs the lowercase form another caller passes for the
// identical tenant) hash to two DIFFERENT lock ids unless every caller first normalizes to one
// canonical string form. Without this, two sweepers racing the same tenant/visit under
// differently-cased UUID strings would each acquire what they believe is "the" lock for that
// tenant/visit and run concurrently, defeating the single-writer guarantee LockTenantSweep and
// LockVisitShots exist to provide.
func canonicalUUID(id string) (string, error) {
	u, err := pgconv.UUID(id)
	if err != nil {
		return "", fmt.Errorf("invalid uuid %q: %w", id, err)
	}
	canon := pgconv.UUIDString(u)
	if canon == "" {
		return "", fmt.Errorf("empty or nil uuid %q", id)
	}
	return canon, nil
}

// visitShotLockKeyArg is the stable advisory-lock key argument for one (tenant, target, date)
// visit. tenantID and targetID are canonicalized (RV-03) before being folded into the key, so any
// two processes (two sweep passes, two concurrent worker replicas) compute the identical Postgres
// advisory-lock key for the same animal's visit and therefore mutually exclude regardless of
// process identity, call order, OR the textual case/representation of the UUIDs each caller holds.
func visitShotLockKeyArg(tenantID, targetID, dateKey string) (string, error) {
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("obligation: visit-shot lock tenant id: %w", err)
	}
	targetCanon, err := canonicalUUID(targetID)
	if err != nil {
		return "", fmt.Errorf("obligation: visit-shot lock target id: %w", err)
	}
	return visitShotLockNamespace + tenantCanon + ":" + targetCanon + ":" + dateKey, nil
}

func driveCapacityLockKeyArg(tenantID, parkID, dateKey string) (string, error) {
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("obligation: drive-capacity lock tenant id: %w", err)
	}
	parkCanon, err := canonicalUUID(parkID)
	if err != nil {
		return "", fmt.Errorf("obligation: drive-capacity lock park id: %w", err)
	}
	return driveCapacityLockNamespace + tenantCanon + ":" + parkCanon + ":" + dateKey, nil
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
		key, keyErr := visitShotLockKeyArg(tenantID, targetID, dateKey)
		if keyErr != nil {
			conn.Release()
			return nil, nil, fmt.Errorf("obligation: build visit-shot lock key: %w", keyErr)
		}
		lockArgs = append(lockArgs, key)
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

func countDriveCellsForParkDate(ctx context.Context, q pgxQuerier, tenantID, parkID string, date time.Time) (int32, error) {
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	park, err := pgconv.UUID(parkID)
	if err != nil {
		return 0, fmt.Errorf("obligation: drive capacity park id: %w", err)
	}
	day := biztime.BusinessDayStart(date)
	rows, err := q.Query(ctx, `
WITH batch_cells AS (
  SELECT ob.batch_id,
         GREATEST(
           COALESCE(ob.planned_quantity, 0)::int,
           count(*)::int
         ) AS cells
  FROM obligation_instances oi
  JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
  LEFT JOIN locations scope_loc
    ON scope_loc.tenant_id = ob.tenant_id
   AND scope_loc.location_id = ob.scope_id
  WHERE oi.tenant_id = $1
    AND ob.planned_date = $3::date
    AND ob.status NOT IN ('canceled', 'superseded')
    AND oi.status NOT IN ('canceled')
    AND (
      (ob.scope_type = 'park' AND ob.scope_id = $2)
      OR (ob.scope_type = 'shed' AND scope_loc.parent_location_id = $2)
      OR g.park_id = $2
    )
  GROUP BY ob.batch_id, ob.planned_quantity
)
SELECT COALESCE(sum(cells), 0)::int FROM batch_cells`, tenant, park, pgconv.Date(&day))
	if err != nil {
		return 0, fmt.Errorf("obligation: count drive cells: %w", err)
	}
	defer rows.Close()
	var count int32
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("obligation: scan drive cell count: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("obligation: drive cell count rows: %w", err)
	}
	return count, nil
}

// CountDriveCellsForParkDate returns the persisted planned vaccination cell count for one
// park/date drive, across all shed/park batch rows. This is the read-only proof path; writers use
// LockDriveCapacity so they cannot race between the count and attach/update.
func (r *Repository) CountDriveCellsForParkDate(ctx context.Context, tenantID, parkID string, date time.Time) (int32, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return countDriveCellsForParkDate(ctx, r.pool, tenantID, parkID, date)
}

// LockDriveCapacity serializes capacity planning for one tenant/park/date. The caller must hold
// the returned release until its batch attach/update is committed or abandoned.
func (r *Repository) LockDriveCapacity(ctx context.Context, tenantID, parkID string, date time.Time) (int32, func(context.Context) error, error) {
	dateKey := biztime.BusinessDayStart(date).Format("2006-01-02")
	lockArg, err := driveCapacityLockKeyArg(tenantID, parkID, dateKey)
	if err != nil {
		return 0, nil, err
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("obligation: acquire drive-capacity lock connection: %w", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", lockArg); err != nil {
		_ = releaseVisitShotConn(context.Background(), conn)
		return 0, nil, fmt.Errorf("obligation: lock drive capacity: %w", err)
	}
	release := func(releaseCtx context.Context) error {
		return releaseVisitShotConn(releaseCtx, conn)
	}
	count, err := countDriveCellsForParkDate(ctx, conn, tenantID, parkID, date)
	if err != nil {
		_ = release(context.Background())
		return 0, nil, err
	}
	return count, release, nil
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
	// RV-03: canonicalize tenantID BEFORE folding it into the lock key. Without this, an uppercase-
	// and a lowercase-hex string for the SAME tenant uuid hash to two different advisory-lock ids
	// (hashtext hashes raw text bytes, not the parsed uuid value), so two sweepers for the same
	// tenant could both observe pg_try_advisory_lock == true and run concurrently -- exactly the
	// single-writer race this lock exists to prevent.
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return false, nil, fmt.Errorf("obligation: tenant-sweep lock tenant id: %w", err)
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("obligation: acquire tenant-sweep lock connection: %w", err)
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", tenantSweepLockNamespace+tenantCanon).Scan(&acquired); err != nil {
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

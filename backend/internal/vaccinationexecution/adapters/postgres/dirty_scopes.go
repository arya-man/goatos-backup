package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// This file backs the durable coalescing dirty-scope queue (migration 000179,
// vaccination_projection_dirty_scopes) for the bounded incremental vaccination-shed projector
// (P0-B). Writes enqueue the affected shed(s) here; cmd/vaccination-projection-worker claims
// pending rows with the same `FOR UPDATE SKIP LOCKED` lease pattern
// backend/internal/outbox/adapters/postgres/repository.go ClaimPending uses, then calls
// RebuildShedShard (incremental_shed_projection.go) for exactly the claimed shed.

const (
	defaultDirtyScopeLeaseTimeout = 2 * time.Minute
	// defaultShedShardStaleTTL bounds how long a shed's shard state may go unrefreshed before
	// EnqueueDueTransitions re-enqueues it purely on elapsed time (not because anything changed),
	// so scheduled->due->overdue recomputation keeps happening even for a shed with no writes.
	// Matches the tenant-level maxVaccinationProjectionLiveLag TTL used elsewhere in this package.
	defaultShedShardStaleTTL = maxVaccinationProjectionLiveLag
)

// EnqueueDirtyShed enqueues one shed for incremental rebuild. Thin wrapper over EnqueueDirtySheds.
func (r *Repository) EnqueueDirtyShed(ctx context.Context, tenantID, shedID, reason string) error {
	return r.EnqueueDirtySheds(ctx, tenantID, []string{shedID}, reason)
}

// EnqueueDirtySheds enqueues a batch of sheds in ONE set-based statement (UNNEST), never a loop of
// per-shed Exec calls -- required even for a handful of sheds (a goat shift touches at most two:
// old + new), and essential for any future caller that dirties many sheds at once. Re-enqueuing an
// already pending/leased shed is a no-op that only bumps reason/updated_at (the partial unique
// index `vaccination_projection_dirty_scopes_coalesce_uidx` is the coalescing key); a shed already
// leased by a worker is left leased, never reset to pending (that would risk a second concurrent
// claim of the same shed).
func (r *Repository) EnqueueDirtySheds(ctx context.Context, tenantID string, shedIDs []string, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("vaccination execution: enqueue dirty sheds: tenant id is required")
	}
	ids := dedupeNonEmptyStrings(shedIDs)
	if len(ids) == 0 {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unspecified"
	}
	if _, err := r.pool.Exec(ctx, `
INSERT INTO vaccination_projection_dirty_scopes (tenant_id, projection_kind, shed_id, reason, status, next_attempt_at, updated_at)
SELECT $1::uuid, $2::text, shed_id, $3::text, 'pending', now(), now()
FROM UNNEST($4::text[]) AS shed_id
ON CONFLICT (tenant_id, projection_kind, shed_id) WHERE status IN ('pending', 'leased')
DO UPDATE SET
  reason = EXCLUDED.reason,
  updated_at = now()`,
		tenantID, domain.ProjectionKindShed, reason, ids); err != nil {
		return fmt.Errorf("vaccination execution: enqueue dirty sheds: %w", err)
	}
	return nil
}

// ClaimDirtyScopes claims up to limit pending, ready (next_attempt_at <= now) dirty scopes with
// `FOR UPDATE SKIP LOCKED` (the outbox ClaimPending shape) so multiple concurrent worker instances
// claim disjoint scopes -- never the same shed twice. A scope already at its attempt budget is
// dead-lettered here rather than claimed, mirroring outbox's ClaimPending pre-publish dead-letter
// check.
//
// tenantID is an OPTIONAL claim filter (C5-003): empty means the global/all-tenant fair claim a
// shared multi-tenant worker uses; a non-empty tenantID filters BEFORE the ORDER BY/LIMIT so foreign-
// tenant rows never enter the claim set at all and never consume the caller's claim budget. Before
// this filter existed, a single-tenant deployment job (the normal shape: one worker configured with
// GOATOS_TENANT_ID) claimed across ALL tenants in one query -- another tenant's dirty rows could fill
// the whole -limit batch and starve the configured tenant. cmd/vaccination-projection-worker still
// keeps its defensive re-queue of any out-of-scope claimed row as belt-and-suspenders, but with this
// filter in place that path should never actually trigger for a tenant-scoped worker.
func (r *Repository) ClaimDirtyScopes(ctx context.Context, owner, tenantID string, limit int, now time.Time) ([]domain.DirtyScope, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = "vaccination-projection-worker"
	}
	tenantID = strings.TrimSpace(tenantID)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: claim dirty scopes: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	rows, err := tx.Query(ctx, `
SELECT dirty_scope_id, tenant_id::text, projection_kind, shed_id, reason, status, attempt_count, max_attempts, enqueued_at
FROM vaccination_projection_dirty_scopes
WHERE status = 'pending'
  AND next_attempt_at <= $1
  AND ($3::text = '' OR tenant_id = $3::uuid)
ORDER BY next_attempt_at, dirty_scope_id
LIMIT $2
FOR UPDATE SKIP LOCKED`, now, limit, tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: claim dirty scopes: select: %w", err)
	}
	var candidates []domain.DirtyScope
	for rows.Next() {
		var scope domain.DirtyScope
		var status string
		if err := rows.Scan(&scope.DirtyScopeID, &scope.TenantID, &scope.ProjectionKind, &scope.ShedID,
			&scope.Reason, &status, &scope.AttemptCount, &scope.MaxAttempts, &scope.EnqueuedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("vaccination execution: claim dirty scopes: scan: %w", err)
		}
		scope.Status = domain.DirtyScopeStatus(status)
		candidates = append(candidates, scope)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("vaccination execution: claim dirty scopes: rows: %w", err)
	}
	rows.Close()

	// Per-scope Exec calls below are bounded by len(candidates) <= limit (ClaimDirtyScopes' own
	// caller-supplied, capped claim batch size -- the worker's -limit flag), the same bounded-batch
	// shape the outbox ClaimPending loop uses for the identical claim-transition problem (one row
	// can be dead-lettered OR leased, never a single UNNEST-able CASE across both branches because
	// the two branches touch disjoint columns and one recurses through max_attempts). Not an
	// unbounded per-row loop over a full table scan.
	claimed := make([]domain.DirtyScope, 0, len(candidates))
	for _, scope := range candidates {
		if scope.AttemptCount >= scope.MaxAttempts {
			// scale-guard:ignore: bounded by len(candidates) <= caller's claim limit (see comment above); dead-letters a scope that hit its attempt budget before it could be leased.
			if _, err := tx.Exec(ctx, `
UPDATE vaccination_projection_dirty_scopes
SET status = 'dead_letter',
    last_error = 'max_attempts_exhausted_before_claim',
    updated_at = $2::timestamptz
WHERE dirty_scope_id = $1
  AND status = 'pending'`, scope.DirtyScopeID, now); err != nil {
				return nil, fmt.Errorf("vaccination execution: claim dirty scopes: dead-letter: %w", err)
			}
			continue
		}
		// scale-guard:ignore: bounded by len(candidates) <= caller's claim limit (see comment above); leases exactly the row FOR UPDATE SKIP LOCKED already selected, one UPDATE per already-claimed row.
		tag, err := tx.Exec(ctx, `
UPDATE vaccination_projection_dirty_scopes
SET status = 'leased',
    lease_owner = $2,
    leased_at = $3::timestamptz,
    lease_expires_at = $3::timestamptz + $4::interval,
    updated_at = $3::timestamptz
WHERE dirty_scope_id = $1
  AND status = 'pending'`, scope.DirtyScopeID, owner, now, defaultDirtyScopeLeaseTimeout.String())
		if err != nil {
			return nil, fmt.Errorf("vaccination execution: claim dirty scopes: lease: %w", err)
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		scope.Status = domain.DirtyScopeStatusLeased
		claimed = append(claimed, scope)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("vaccination execution: claim dirty scopes: commit: %w", err)
	}
	committed = true
	return claimed, nil
}

// MarkDirtyScopeDone deletes a successfully rebuilt scope. Deleting (rather than keeping a 'done'
// row forever) keeps the queue table bounded by outstanding + recently dead-lettered work, not
// unboundedly growing with every historical rebuild.
//
// Guarded re-dirty check (P2): EnqueueDirtySheds' coalescing UPSERT can update THIS SAME row (via
// the partial unique index) while the worker's rebuild for it is still in flight -- a write for shed
// A lands, re-dirtying it, WHILE a worker is mid-RebuildShedShard(A) for the ORIGINAL reason. The
// coalescing UPDATE bumps updated_at (and reason) but deliberately leaves the row leased and leaves
// leased_at untouched ("a shed already leased by a worker is left leased, never reset to pending" --
// EnqueueDirtySheds' own comment), so a naive delete-by-(dirty_scope_id,status='leased') here would
// silently discard that newer signal the moment the in-flight rebuild completes, even though the
// rebuild may predate it and not reflect it. Comparing updated_at to leased_at (both stamped equal by
// ClaimDirtyScopes at lease time, and ONLY updated_at moves on a coalesced re-dirty) tells us, purely
// from the row's own columns, whether that happened: if unchanged, this rebuild is known to cover the
// latest signal and it is safe to delete; if changed, do NOT delete -- reset the scope back to
// pending (clearing the lease) so the next claim re-runs RebuildShedShard for the newer write instead
// of losing it.
func (r *Repository) MarkDirtyScopeDone(ctx context.Context, dirtyScopeID int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
DELETE FROM vaccination_projection_dirty_scopes
WHERE dirty_scope_id = $1
  AND status = 'leased'
  AND updated_at = leased_at`, dirtyScopeID)
	if err != nil {
		return fmt.Errorf("vaccination execution: mark dirty scope done: delete: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	// Either this scope was coalesced-re-dirtied while its lease was in flight (updated_at moved
	// past leased_at) -- the case this guard exists for -- or it is no longer in the state we
	// expect (already reclaimed, dead-lettered, or completed by something else), in which case this
	// UPDATE's WHERE clause matches nothing and is a safe no-op.
	if _, err := r.pool.Exec(ctx, `
UPDATE vaccination_projection_dirty_scopes
SET status = 'pending',
    lease_owner = NULL,
    leased_at = NULL,
    lease_expires_at = NULL,
    next_attempt_at = now(),
    updated_at = now()
WHERE dirty_scope_id = $1
  AND status = 'leased'
  AND updated_at <> leased_at`, dirtyScopeID); err != nil {
		return fmt.Errorf("vaccination execution: mark dirty scope done: re-enqueue after concurrent re-dirty: %w", err)
	}
	return nil
}

// MarkDirtyScopeFailed increments the attempt count and either reschedules with backoff (pending)
// or dead-letters the scope once it has exhausted max_attempts. Bounded exponential-ish backoff:
// 30s * min(attempt, 6), capped at 180s.
func (r *Repository) MarkDirtyScopeFailed(ctx context.Context, dirtyScopeID int64, cause string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	message := strings.TrimSpace(cause)
	if len(message) > 2000 {
		message = message[:2000]
	}
	if _, err := r.pool.Exec(ctx, `
UPDATE vaccination_projection_dirty_scopes
SET attempt_count = attempt_count + 1,
    status = CASE WHEN attempt_count + 1 >= max_attempts THEN 'dead_letter' ELSE 'pending' END,
    next_attempt_at = CASE
      WHEN attempt_count + 1 >= max_attempts THEN next_attempt_at
      ELSE $3::timestamptz + (LEAST(attempt_count + 1, 6) * interval '30 seconds')
    END,
    last_error = $2::text,
    lease_owner = NULL,
    leased_at = NULL,
    lease_expires_at = NULL,
    updated_at = $3::timestamptz
WHERE dirty_scope_id = $1
  AND status = 'leased'`, dirtyScopeID, message, now); err != nil {
		return fmt.Errorf("vaccination execution: mark dirty scope failed: %w", err)
	}
	return nil
}

// ReclaimExpiredDirtyScopeLeases resets scopes whose lease expired without completing (a crashed
// or killed worker) back to pending so another worker instance can claim them. Bounded by the same
// WHERE clause every run; no per-row loop.
//
// This ALSO increments attempt_count and dead-letters once max_attempts is exhausted, exactly like
// MarkDirtyScopeFailed's explicit-failure path. Without this, a worker that reliably crashes
// mid-rebuild for the SAME shed (e.g. a poison-pill row that panics the shard-scoped query) would
// loop pending -> leased -> expired -> pending forever: only an explicit MarkDirtyScopeFailed call
// bumped attempt_count, and a crash never reaches that call, so the scope would never reach
// max_attempts and never dead-letter -- an unbounded retry loop hammering the same broken shed
// indefinitely instead of eventually surfacing as a DLQ item for operator attention.
func (r *Repository) ReclaimExpiredDirtyScopeLeases(ctx context.Context, now time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
UPDATE vaccination_projection_dirty_scopes
SET attempt_count = attempt_count + 1,
    status = CASE WHEN attempt_count + 1 >= max_attempts THEN 'dead_letter' ELSE 'pending' END,
    last_error = CASE
      WHEN attempt_count + 1 >= max_attempts THEN 'lease_expired_max_attempts_exhausted'
      ELSE last_error
    END,
    lease_owner = NULL,
    leased_at = NULL,
    lease_expires_at = NULL,
    updated_at = $1::timestamptz
WHERE status = 'leased'
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at <= $1::timestamptz`, now)
	if err != nil {
		return 0, fmt.Errorf("vaccination execution: reclaim expired dirty scope leases: %w", err)
	}
	return tag.RowsAffected(), nil
}

// EnqueueDueTransitions is the TIME-DRIVEN pass: it enqueues sheds whose computed status will
// change purely because time passed (next_transition_at reached: scheduled->due or due->overdue),
// plus any shed whose shard state has gone stale (projected_at older than the TTL) even with no
// known transition, so every shed's freshness clock keeps moving forward without a dirty write.
// Bounded per call (LIMIT on each half); a shed not caught this cycle is caught on the next.
func (r *Repository) EnqueueDueTransitions(ctx context.Context, tenantID string, limit int) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return 0, fmt.Errorf("vaccination execution: enqueue due transitions: tenant id is required")
	}
	if limit <= 0 {
		limit = 200
	}

	rows, err := r.pool.Query(ctx, `
(
  SELECT shed_id
  FROM vaccination_shed_shard_state
  WHERE tenant_id = $1::uuid
    AND row_present
    AND next_transition_at IS NOT NULL
    AND next_transition_at <= now()
  ORDER BY next_transition_at, shed_id
  LIMIT $2
)
UNION
(
  SELECT shed_id
  FROM vaccination_shed_shard_state
  WHERE tenant_id = $1::uuid
    AND row_present
    AND projected_at < now() - $3::interval
  ORDER BY projected_at, shed_id
  LIMIT $2
)`, tenantID, limit, defaultShedShardStaleTTL.String())
	if err != nil {
		return 0, fmt.Errorf("vaccination execution: enqueue due transitions: select: %w", err)
	}
	var shedIDs []string
	for rows.Next() {
		var shedID string
		if err := rows.Scan(&shedID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("vaccination execution: enqueue due transitions: scan: %w", err)
		}
		shedIDs = append(shedIDs, shedID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("vaccination execution: enqueue due transitions: rows: %w", err)
	}
	rows.Close()
	if len(shedIDs) > limit {
		shedIDs = shedIDs[:limit]
	}
	if len(shedIDs) == 0 {
		return 0, nil
	}
	if err := r.EnqueueDirtySheds(ctx, tenantID, shedIDs, "time_driven_transition"); err != nil {
		return 0, err
	}
	return len(shedIDs), nil
}

// AnyShedShardStale reports whether any served (row_present) shed's per-shed shard-freshness row
// for tenantID is older than ttl -- a single indexed EXISTS check
// (vaccination_shed_shard_state_projected_idx), never a per-row scan. Used by the read-time
// freshness invariant so the shed read does not report green while some served shed is stale, even
// though the tenant-wide serving projection_version itself is fresh (Serving-Read Freshness
// Contract, docs/decisions/high-scale-dashboard-projections.md). A tenant with no shard-state rows
// yet (never touched by the incremental worker; still solely on the full RecomputeShedProjection
// bootstrap path, which is uniformly fresh by construction) reports not-stale.
func (r *Repository) AnyShedShardStale(ctx context.Context, tenantID string, ttl time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var stale bool
	if err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM vaccination_shed_shard_state
  WHERE tenant_id = $1::uuid
    AND row_present
    AND projected_at < now() - $2::interval
)`, tenantID, ttl.String()).Scan(&stale); err != nil {
		return false, fmt.Errorf("vaccination execution: any shed shard stale: %w", err)
	}
	return stale, nil
}

// AnyShedShardStaleInScope is the SCOPED counterpart to AnyShedShardStale (Serving-Read Freshness
// Contract, docs/decisions/high-scale-dashboard-projections.md). AnyShedShardStale gates on ANY
// served shed anywhere in the tenant being behind -- correct for an unfiltered tenant-wide list, but
// wrong for a single shed detail read or a park-scoped list: one stale or dead-lettered shed
// elsewhere in the tenant must not 503 every OTHER shed's read. When shedID is set (the common
// ShedDetail case), this is a single indexed (tenant_id, shed_id) lookup on the shard_state primary
// key -- no different in cost from checking any other single row. When only parkID is set, staleness
// is scoped to that park's sheds via the locations parent-child join
// (locations_tenant_parent_order_idx), still bounded by shed cardinality (a tenant has at most a few
// hundred sheds, never per-animal scale). When neither is set (an unfiltered read), this falls back to
// the tenant-wide check -- a genuinely unscoped read still needs the tenant-wide guarantee.
func (r *Repository) AnyShedShardStaleInScope(ctx context.Context, tenantID string, shedID, parkID *string, ttl time.Duration) (bool, error) {
	shed := ""
	if shedID != nil {
		shed = strings.TrimSpace(*shedID)
	}
	park := ""
	if parkID != nil {
		park = strings.TrimSpace(*parkID)
	}
	if shed == "" && park == "" {
		return r.AnyShedShardStale(ctx, tenantID, ttl)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var stale bool
	if err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM vaccination_shed_shard_state s
  WHERE s.tenant_id = $1::uuid
    AND s.row_present
    AND s.projected_at < now() - $2::interval
    AND ($3::text = '' OR s.shed_id = $3)
    AND ($4::text = '' OR EXISTS (
      SELECT 1
      FROM locations l
      WHERE l.tenant_id = $1::uuid
        AND l.parent_location_id = $4::uuid
        AND l.location_type = 'shed'
        AND l.location_id = s.shed_id::uuid
    ))
)`, tenantID, ttl.String(), shed, park).Scan(&stale); err != nil {
		return false, fmt.Errorf("vaccination execution: any shed shard stale in scope: %w", err)
	}
	return stale, nil
}

// ShedProjectionMaxShardAge returns the oldest (now - projected_at) age across the tenant's
// row_present shard-freshness rows, and whether any such rows exist. Exposed for operational
// visibility / an admin freshness surface; AnyShedShardStale is the gate the read path uses.
func (r *Repository) ShedProjectionMaxShardAge(ctx context.Context, tenantID string) (time.Duration, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var maxAgeSeconds *float64
	if err := r.pool.QueryRow(ctx, `
SELECT EXTRACT(EPOCH FROM (now() - MIN(projected_at)))
FROM vaccination_shed_shard_state
WHERE tenant_id = $1::uuid
  AND row_present`, tenantID).Scan(&maxAgeSeconds); err != nil {
		return 0, false, fmt.Errorf("vaccination execution: shed projection max shard age: %w", err)
	}
	if maxAgeSeconds == nil {
		return 0, false, nil
	}
	return time.Duration(*maxAgeSeconds * float64(time.Second)), true, nil
}

// ShedIDForObligationGoat resolves the shed_id of a goat-target obligation's target goat, for
// vaccination.completed dirty-shed enqueue (that event's payload carries only the obligation id).
// A single indexed lookup (obligation_instances PK join to goats PK) -- not a fanout loop -- called
// once per completion event, never per row of a batch.
func (r *Repository) ShedIDForObligationGoat(ctx context.Context, tenantID, obligationID string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tenantID = strings.TrimSpace(tenantID)
	obligationID = strings.TrimSpace(obligationID)
	if tenantID == "" || obligationID == "" {
		return "", false, nil
	}
	var shedID *string
	err := r.pool.QueryRow(ctx, `
SELECT g.shed_id::text
FROM obligation_instances oi
JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
WHERE oi.tenant_id = $1::uuid
  AND oi.obligation_id = $2::uuid
  AND oi.target_type = 'goat'`, tenantID, obligationID).Scan(&shedID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("vaccination execution: shed id for obligation goat: %w", err)
	}
	if shedID == nil || strings.TrimSpace(*shedID) == "" {
		return "", false, nil
	}
	return *shedID, true, nil
}

func dedupeNonEmptyStrings(values []string) []string {
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

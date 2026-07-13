package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// This file is the BOUNDED INCREMENTAL half of the shed-wise vaccination projector (P0-B,
// context/execution/api-projection-performance-handoff-2026-07-13.md). RebuildShedShard recomputes
// and upserts ONLY the one claimed shed's row into the tenant's CURRENT serving projection_version
// -- it never scans or copies any other shed's row, and it never restamps a tenant-wide as_of.
// Per-shed freshness truth lives in vaccination_shed_shard_state (migration 000179), never a mixed
// tenant timestamp. Contrast with RecomputeShedProjection (shed_projection.go), the full-tenant
// bootstrap/repair rebuild that stamps a brand-new projection_version for every shed at once; that
// command remains the explicit, unchanged bootstrap/repair path.
//
// Two rejected designs this file must not reproduce (handoff doc "Rejected Unsafe Work"):
//  1. a "bounded" worker that copied every unchanged tenant row into a new global version per dirty
//     shed -- still O(total tenant rows) per dirty shed. vaccinationShedShardSelectSQL below scopes
//     every base CTE (shed_goats/shed_obligations/completions/asof_terminal) to exactly one shed via
//     a shed_goats/shed_obligations join, never a tenant-wide GROUP BY.
//  2. an in-place refresh that stamped ONE tenant-wide as_of/freshness over sheds rebuilt at
//     different times -- vaccination_shed_shard_state is keyed (tenant_id, shed_id): only the
//     rebuilt shed's row is touched.
const shardAdvisoryLockSalt = 86174

// RebuildShedShard recomputes ONLY shedID's row (goats/obligations/completions filtered to that
// shed) and UPSERTs it into the tenant's serving projection_version. When the shed has no
// qualifying alive animals, its row is DELETEd from the projection and the shard state is stamped
// row_present=false, rather than left stale. When the tenant has no serving projection_version yet
// (never bootstrapped by RecomputeShedProjection), the rebuild is deferred: there is no serving
// version to UPSERT a row into, so it is a safe no-op until the next explicit bootstrap/repair run.
func (r *Repository) RebuildShedShard(ctx context.Context, req domain.RebuildShedShardRequest) (domain.RebuildShedShardResult, error) {
	tenantID := strings.TrimSpace(req.TenantID)
	shedID := strings.TrimSpace(req.ShedID)
	if tenantID == "" || shedID == "" {
		return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: tenant id and shed id are required")
	}
	cfg, err := r.CapacityConfig(ctx, tenantID)
	if err != nil {
		return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: capacity config: %w", err)
	}
	asOf := req.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	dueBefore := req.DueBefore
	if dueBefore.IsZero() {
		dueBefore = asOf.Add(defaultExecutionHorizon)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Per-SHED advisory lock (tenant+shed) -- never the tenant-wide lock RecomputeShedProjection
	// takes. Concurrent rebuilds of DIFFERENT sheds must never block each other; concurrent
	// rebuilds of the SAME shed (e.g. a re-dirty racing the worker that is already rebuilding it)
	// serialize here instead of interleaving UPSERTs.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text || ':' || $2::text, $3::int))`,
		tenantID, shedID, shardAdvisoryLockSalt); err != nil {
		return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: lock: %w", err)
	}

	var servingVersion int64
	err = tx.QueryRow(ctx, `
SELECT serving_projection_version
FROM vaccination_shed_projection_state
WHERE tenant_id = $1::uuid
  AND serving_projection_version IS NOT NULL`, tenantID).Scan(&servingVersion)
	if err != nil {
		if err == pgx.ErrNoRows {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: commit deferred: %w", commitErr)
			}
			committed = true
			return domain.RebuildShedShardResult{TenantID: tenantID, ShedID: shedID, Deferred: true}, nil
		}
		return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: serving version: %w", err)
	}

	var (
		parkID, parkName, shedName               string
		animals, dueAnimals, openCells, sessions int
		capacityStatus, shedStatus               string
		lastDone, nextDue, nextTransition        pgtype.Timestamptz
	)
	row := tx.QueryRow(ctx, vaccinationShedShardSelectSQL, tenantID, shedID, asOf, dueBefore, cfg.MaxPerDay, cfg.MaxBufferDays)
	scanErr := row.Scan(&parkID, &parkName, &shedName, &animals, &dueAnimals, &openCells, &sessions,
		&capacityStatus, &shedStatus, &lastDone, &nextDue, &nextTransition)
	found := true
	switch {
	case scanErr == nil:
		found = true
	case scanErr == pgx.ErrNoRows:
		found = false
	default:
		return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: select: %w", scanErr)
	}

	projectedAt := time.Now()
	if found {
		if _, err := tx.Exec(ctx, `
INSERT INTO vaccination_shed_projection_rows (
  tenant_id, park_id, park_name, shed_id, shed_name,
  animals, due_animals, open_cells, sessions,
  capacity_status, shed_status, last_done, next_due,
  projection_version, projected_at
) VALUES (
  $1::uuid, $2, $3, $4, $5,
  $6, $7, $8, $9,
  $10, $11, $12, $13,
  $14::bigint, $15::timestamptz
)
ON CONFLICT (tenant_id, projection_version, shed_id) DO UPDATE SET
  park_id = EXCLUDED.park_id,
  park_name = EXCLUDED.park_name,
  shed_name = EXCLUDED.shed_name,
  animals = EXCLUDED.animals,
  due_animals = EXCLUDED.due_animals,
  open_cells = EXCLUDED.open_cells,
  sessions = EXCLUDED.sessions,
  capacity_status = EXCLUDED.capacity_status,
  shed_status = EXCLUDED.shed_status,
  last_done = EXCLUDED.last_done,
  next_due = EXCLUDED.next_due,
  projected_at = EXCLUDED.projected_at,
  updated_at = now()`,
			tenantID, parkID, parkName, shedID, shedName,
			animals, dueAnimals, openCells, sessions,
			capacityStatus, shedStatus, timePtr(lastDone), timePtr(nextDue),
			servingVersion, projectedAt); err != nil {
			return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: upsert row: %w", err)
		}
	} else {
		// No qualifying alive animals for this shed under the serving version -- delete rather
		// than leave a stale row (row_present=false below records this for the shard state).
		// scale-guard:ignore: single-shed-scoped delete (tenant_id + projection_version + shed_id), never a whole-tenant refresh -- the opposite of full-mv-refresh; matches the accepted shape ON CONFLICT ... DO UPDATE already uses one row above.
		if _, err := tx.Exec(ctx, `
DELETE FROM vaccination_shed_projection_rows
WHERE tenant_id = $1::uuid
  AND projection_version = $2::bigint
  AND shed_id = $3`, tenantID, servingVersion, shedID); err != nil {
			return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: delete row: %w", err)
		}
	}

	nextTransitionPtr := timePtr(nextTransition)
	if _, err := tx.Exec(ctx, `
INSERT INTO vaccination_shed_shard_state (
  tenant_id, shed_id, projected_at, as_of, next_transition_at, source_watermark, row_present, serving_state, updated_at
) VALUES (
  $1::uuid, $2, $3::timestamptz, $4::timestamptz, $5::timestamptz, NULL, $6::boolean, 'fresh', now()
)
ON CONFLICT (tenant_id, shed_id) DO UPDATE SET
  projected_at = EXCLUDED.projected_at,
  as_of = EXCLUDED.as_of,
  next_transition_at = EXCLUDED.next_transition_at,
  row_present = EXCLUDED.row_present,
  serving_state = 'fresh',
  updated_at = now()`,
		tenantID, shedID, projectedAt, asOf, nextTransitionPtr, found); err != nil {
		return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: upsert shard state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.RebuildShedShardResult{}, fmt.Errorf("vaccination execution: rebuild shed shard: commit: %w", err)
	}
	committed = true
	return domain.RebuildShedShardResult{
		TenantID:          tenantID,
		ShedID:            shedID,
		ProjectionVersion: servingVersion,
		ProjectedAt:       projectedAt,
		AsOf:              asOf,
		RowPresent:        found,
		NextTransitionAt:  nextTransitionPtr,
	}, nil
}

// vaccinationShedShardSelectSQL is a SHARD-SCOPED rewrite of vaccinationShedProjectionInsertSQL
// (shed_projection.go): every base CTE (shed_goats, shed_obligations, completions, asof_terminal)
// is filtered to exactly one shed via a join to shed_goats/shed_obligations, so this query only
// ever reads that shed's goats/obligations/completions/status-events -- never a tenant-wide scan,
// never a copy of another shed's row. Returns zero rows when the shed has no alive animals (the
// caller deletes any existing projection row for it) or is not an active shed location. Also
// computes next_transition_at: the earliest timestamp at which some cell in this shed will next
// cross a scheduled->due or due->overdue boundary, so the worker's time-driven pass
// (EnqueueDueTransitions) can re-enqueue this shed exactly when it will next change, without
// polling every shed on a fixed schedule. Kept textually independent from
// vaccinationShedProjectionInsertSQL on purpose (same rationale as that file's own comment): a bug
// in one is very unlikely to be mirrored in the other.
// scale-guard:ignore: off-request incremental-projector recompute only (RebuildShedShard, called by the leased worker, never a request handler); every base CTE is additionally scoped to one shed via shed_goats/shed_obligations (see comment above), unlike a request-path god-CTE over the whole tenant.
const vaccinationShedShardSelectSQL = `
WITH shed_goats AS (
  SELECT g.goat_id, g.shed_id
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.shed_id = $2::uuid
    AND g.lifecycle_status = 'alive'
    AND g.merged_into_goat_id IS NULL
),
alive AS (
  SELECT shed_id AS shed_uuid, COUNT(*)::bigint AS animals
  FROM shed_goats
  GROUP BY shed_id
),
shed_obligations AS (
  SELECT oi.obligation_id, oi.due_at, oi.window_start, oi.completed_at, oi.status AS stored_status, oi.target_id AS goat_id
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN shed_goats sg
    ON sg.goat_id = oi.target_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND oi.due_at <= $4::timestamptz
),
completions AS (
  SELECT
    obligation_id,
    (ARRAY_AGG(asof_status ORDER BY
      CASE WHEN asof_status IN ('recorded', 'accepted') THEN 0 ELSE 1 END,
      administered_at DESC,
      created_at DESC))[1] AS effective_status,
    MAX(administered_at) FILTER (WHERE asof_status = 'accepted') AS last_accepted_at
  FROM (
    SELECT
      vc.obligation_id, vc.administered_at, vc.created_at,
      CASE
        WHEN vc.status IN ('accepted', 'rejected') AND vc.verified_at IS NOT NULL AND vc.verified_at > $3::timestamptz THEN 'recorded'
        ELSE vc.status
      END AS asof_status
    FROM vaccination_completions vc
    JOIN shed_obligations so ON so.obligation_id = vc.obligation_id
    WHERE vc.tenant_id = $1::uuid
      AND COALESCE(vc.administered_at, vc.created_at) <= $3::timestamptz
  ) c
  GROUP BY obligation_id
),
asof_terminal AS (
  SELECT
    ose.obligation_id,
    (ARRAY_AGG(ose.event_type ORDER BY ose.occurred_at DESC, ose.obligation_event_id DESC)
       FILTER (WHERE ose.occurred_at <= $3::timestamptz))[1] AS asof_terminal_type,
    true AS has_terminal_event
  FROM obligation_status_events ose
  JOIN shed_obligations so ON so.obligation_id = ose.obligation_id
  WHERE ose.tenant_id = $1::uuid
    AND ose.event_type IN ('missed', 'waived', 'deferred')
  GROUP BY ose.obligation_id
),
effective AS (
  SELECT
    so.goat_id,
    so.due_at,
    so.window_start,
    c.last_accepted_at,
    c.effective_status AS completion_status,
    CASE
      WHEN so.stored_status = 'completed' THEN
        CASE
          WHEN so.completed_at IS NOT NULL AND so.completed_at <= $3::timestamptz THEN 'completed'
          WHEN so.completed_at IS NULL AND c.effective_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN so.due_at < $3::timestamptz THEN 'overdue' WHEN COALESCE(so.window_start, so.due_at) <= $3::timestamptz THEN 'due' ELSE 'scheduled' END)
        END
      WHEN so.stored_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          WHEN te.asof_terminal_type IS NOT NULL THEN te.asof_terminal_type
          WHEN te.has_terminal_event THEN (CASE WHEN so.due_at < $3::timestamptz THEN 'overdue' WHEN COALESCE(so.window_start, so.due_at) <= $3::timestamptz THEN 'due' ELSE 'scheduled' END)
          ELSE so.stored_status
        END
      WHEN so.stored_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN so.due_at < $3::timestamptz THEN 'overdue' WHEN COALESCE(so.window_start, so.due_at) <= $3::timestamptz THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM shed_obligations so
  LEFT JOIN completions c ON c.obligation_id = so.obligation_id
  LEFT JOIN asof_terminal te ON te.obligation_id = so.obligation_id
),
due_agg AS (
  SELECT
    COUNT(DISTINCT goat_id) FILTER (
      WHERE eff_status IN ('overdue', 'due', 'in_progress')
         OR completion_status IN ('recorded', 'rejected')
    )::bigint AS due_animals,
    COUNT(DISTINCT goat_id) FILTER (WHERE eff_status = 'overdue')::bigint AS overdue_animals,
    COUNT(DISTINCT goat_id) FILTER (WHERE eff_status = 'scheduled')::bigint AS scheduled_animals,
    COUNT(*) FILTER (WHERE eff_status IN ('overdue', 'due', 'in_progress'))::bigint AS open_cells,
    MAX(last_accepted_at) AS last_done,
    MIN(due_at) FILTER (WHERE eff_status IN ('overdue', 'due', 'in_progress', 'scheduled')) AS next_due,
    MIN(CASE
      WHEN eff_status = 'scheduled' THEN COALESCE(window_start, due_at)
      WHEN eff_status IN ('due', 'in_progress') THEN due_at
      ELSE NULL
    END) AS next_transition_at
  FROM effective
),
shed_row AS (
  SELECT
    park.location_id::text AS park_id,
    park.name AS park_name,
    shed.name AS shed_name,
    COALESCE(alive.animals, 0)::int AS animals,
    COALESCE(due_agg.due_animals, 0)::int AS due_animals,
    COALESCE(due_agg.overdue_animals, 0)::int AS overdue_animals,
    COALESCE(due_agg.scheduled_animals, 0)::int AS scheduled_animals,
    COALESCE(due_agg.open_cells, 0)::int AS open_cells,
    due_agg.last_done,
    due_agg.next_due,
    due_agg.next_transition_at
  FROM locations shed
  JOIN locations park
    ON park.tenant_id = $1::uuid
   AND park.location_id = shed.parent_location_id
   AND park.location_type = 'park'
   AND park.status = 'active'
  LEFT JOIN alive ON true
  LEFT JOIN due_agg ON true
  WHERE shed.tenant_id = $1::uuid
    AND shed.location_id = $2::uuid
    AND shed.location_type = 'shed'
    AND shed.status = 'active'
),
scored AS (
  SELECT
    shed_row.*,
    CASE WHEN open_cells <= 0 THEN 0 ELSE CEIL(open_cells::numeric / GREATEST($5::numeric, 1))::int END AS sessions
  FROM shed_row
),
classified AS (
  SELECT
    scored.*,
    CASE
      WHEN sessions <= 1 THEN 'within_cap'
      WHEN sessions <= ($6::int + 1) THEN 'over_cap'
      ELSE 'capacity_breach'
    END AS capacity_status,
    CASE
      WHEN overdue_animals > 0 THEN 'overdue'
      WHEN sessions > ($6::int + 1) THEN 'needs_review'
      WHEN sessions > 1 THEN 'split'
      WHEN due_animals > 0 THEN 'due'
      WHEN scheduled_animals > 0 THEN 'scheduled'
      ELSE 'on_track'
    END AS shed_status
  FROM scored
)
SELECT park_id, park_name, shed_name, animals, due_animals, open_cells, sessions,
  capacity_status, shed_status, last_done, next_due, next_transition_at
FROM classified
WHERE animals > 0;
`

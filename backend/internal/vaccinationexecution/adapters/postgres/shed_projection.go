package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// This file is the projector half of C35-002 for vaccinationexecution: it builds and refreshes
// vaccination_shed_projection_rows / vaccination_shed_projection_state (migration 000167) off the request
// path. ShedSummary (repository.go) is UNCHANGED by this file and still serves GET /vaccination/sheds from
// the live shedSummarySQL compute-on-read CTE -- flipping that read to this projection is an explicit,
// separate follow-up once the scheduled recompute job (vaccination-shed-projection-recompute, run every 5
// minutes, mirroring process-integrity-projection-recompute) has been populating it in each environment.
// See context/execution/vaccexec-readmodel-design.md for the full design + rollout plan.

const (
	defaultShedProjectionFresh   = 5 * time.Minute
	shedProjectionPruneBatchSize = 5000
)

// RecomputeShedProjection rebuilds the tenant's vaccination_shed_projection_rows from the same canonical
// obligation/goat/capacity-config tables shedSummarySQL reads, then atomically flips
// vaccination_shed_projection_state.serving_projection_version to the new version inside the same
// transaction as the row insert (version-swap, never a live whole-tenant delete+reinsert). This is a
// projector method: it is allowed to replay raw source state exactly like shedSummarySQL because it runs
// off the request path and publishes one indexed serving table.
func (r *Repository) RecomputeShedProjection(ctx context.Context, req domain.ShedProjectionRecomputeRequest) (result domain.ShedProjectionRecomputeResult, retErr error) {
	if strings.TrimSpace(req.TenantID) == "" {
		return domain.ShedProjectionRecomputeResult{}, fmt.Errorf("vaccination execution: recompute shed projection: tenant id is required")
	}
	cfg, err := r.CapacityConfig(ctx, req.TenantID)
	if err != nil {
		return domain.ShedProjectionRecomputeResult{}, fmt.Errorf("vaccination execution: recompute shed projection: capacity config: %w", err)
	}
	asOf := req.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	dueBefore := req.DueBefore
	if dueBefore.IsZero() {
		dueBefore = asOf.Add(defaultExecutionHorizon)
	}

	var projectionVersion int64
	var projectedAt time.Time
	if err := r.pool.QueryRow(ctx, `SELECT (extract(epoch FROM now()) * 1000)::bigint, now()`).Scan(&projectionVersion, &projectedAt); err != nil {
		return domain.ShedProjectionRecomputeResult{}, fmt.Errorf("vaccination execution: recompute shed projection: stamp: %w", err)
	}
	if err := r.markShedProjectionRebuilding(ctx, req.TenantID, projectionVersion, projectedAt, asOf, dueBefore); err != nil {
		return domain.ShedProjectionRecomputeResult{}, err
	}
	committed := false
	defer func() {
		// A post-commit maintenance failure must be visible, but must not mark the newly committed
		// serving projection as failed -- readers (once flipped) can continue from last-known-good
		// while the scheduled projector retries cleanup on its next run.
		if retErr != nil && !committed {
			r.markShedProjectionFailed(req.TenantID, retErr)
		}
	}()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ShedProjectionRecomputeResult{}, fmt.Errorf("vaccination execution: recompute shed projection: begin: %w", err)
	}
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, vaccinationShedProjectionInsertSQL,
		req.TenantID, asOf, dueBefore, cfg.MaxPerDay, cfg.MaxBufferDays, projectionVersion, projectedAt); err != nil {
		return domain.ShedProjectionRecomputeResult{}, fmt.Errorf("vaccination execution: recompute shed projection: insert rows: %w", err)
	}

	var rowCount int64
	if err := tx.QueryRow(ctx, `
SELECT COUNT(*)::bigint
FROM vaccination_shed_projection_rows
WHERE tenant_id = $1::uuid
  AND projection_version = $2::bigint`, req.TenantID, projectionVersion).Scan(&rowCount); err != nil {
		return domain.ShedProjectionRecomputeResult{}, fmt.Errorf("vaccination execution: recompute shed projection: count rows: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO vaccination_shed_projection_state (
  tenant_id, projection_version, serving_projection_version, projected_at, as_of, due_before, row_count,
  freshness_status, serving_state, last_error, updated_at
) VALUES (
  $1::uuid, $2::bigint, $2::bigint, $3::timestamptz, $4::timestamptz, $5::timestamptz, $6::bigint,
  'green', 'fresh', NULL, now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
  projection_version = EXCLUDED.projection_version,
  serving_projection_version = EXCLUDED.serving_projection_version,
  projected_at = EXCLUDED.projected_at,
  as_of = EXCLUDED.as_of,
  due_before = EXCLUDED.due_before,
  row_count = EXCLUDED.row_count,
  freshness_status = EXCLUDED.freshness_status,
  serving_state = EXCLUDED.serving_state,
  last_error = NULL,
  updated_at = now()`,
		req.TenantID, projectionVersion, projectedAt, asOf, dueBefore, rowCount); err != nil {
		return domain.ShedProjectionRecomputeResult{}, fmt.Errorf("vaccination execution: recompute shed projection: upsert state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ShedProjectionRecomputeResult{}, fmt.Errorf("vaccination execution: recompute shed projection: commit: %w", err)
	}
	committed = true
	result = domain.ShedProjectionRecomputeResult{
		TenantID:           req.TenantID,
		ProjectionVersion:  projectionVersion,
		ProjectedAt:        projectedAt,
		AsOf:               asOf,
		Rows:               rowCount,
		ProjectionFreshFor: defaultShedProjectionFresh,
	}
	if err := r.pruneOldShedProjectionRows(ctx, req.TenantID); err != nil {
		r.markShedProjectionMaintenanceError(req.TenantID, err)
		return result, fmt.Errorf("vaccination execution: recompute shed projection: serving projection committed but stale-row prune failed: %w", err)
	}
	return result, nil
}

func (r *Repository) markShedProjectionRebuilding(ctx context.Context, tenantID string, projectionVersion int64, projectedAt, asOf, dueBefore time.Time) error {
	if _, err := r.pool.Exec(ctx, `
INSERT INTO vaccination_shed_projection_state (
  tenant_id, projection_version, serving_projection_version, projected_at, as_of, due_before, row_count,
  freshness_status, serving_state, last_error, updated_at
) VALUES (
  $1::uuid, $2::bigint, NULL, $3::timestamptz, $4::timestamptz, $5::timestamptz, 0,
  'unknown', 'rebuilding', NULL, now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
  freshness_status = CASE
    WHEN vaccination_shed_projection_state.row_count > 0 THEN 'yellow'
    ELSE 'unknown'
  END,
  serving_state = 'rebuilding',
  last_error = NULL,
  updated_at = now()`, tenantID, projectionVersion, projectedAt, asOf, dueBefore); err != nil {
		return fmt.Errorf("vaccination execution: recompute shed projection: mark rebuilding: %w", err)
	}
	return nil
}

func (r *Repository) markShedProjectionFailed(tenantID string, cause error) {
	if strings.TrimSpace(tenantID) == "" || cause == nil {
		return
	}
	stateCtx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, _ = r.pool.Exec(stateCtx, `
UPDATE vaccination_shed_projection_state
SET freshness_status = 'red',
    serving_state = 'failed',
    last_error = $2::text,
    updated_at = now()
WHERE tenant_id = $1::uuid`, tenantID, message)
}

func (r *Repository) markShedProjectionMaintenanceError(tenantID string, cause error) {
	if strings.TrimSpace(tenantID) == "" || cause == nil {
		return
	}
	stateCtx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	message := "projection_maintenance: " + cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, _ = r.pool.Exec(stateCtx, `
UPDATE vaccination_shed_projection_state
SET freshness_status = 'yellow',
    last_error = $2::text,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND serving_projection_version IS NOT NULL`, tenantID, message)
}

func (r *Repository) pruneOldShedProjectionRows(ctx context.Context, tenantID string) error {
	return r.pruneOldShedProjectionRowsWithBatchSize(ctx, tenantID, shedProjectionPruneBatchSize)
}

// pruneOldShedProjectionRowsWithBatchSize deletes rows left behind by earlier projection_versions in
// bounded batches (never one unbounded whole-tenant DELETE), the same chunked-loop shape the
// obligation/idempotency sweepers use elsewhere in this codebase.
func (r *Repository) pruneOldShedProjectionRowsWithBatchSize(ctx context.Context, tenantID string, batchSize int32) error {
	if strings.TrimSpace(tenantID) == "" || batchSize <= 0 {
		return fmt.Errorf("invalid prune request: tenant_id and positive batch_size are required")
	}
	const (
		maxBatchesPerCall = 1000      // prevent runaway prune loop
		maxRowsPerCall    = 1_000_000 // maximum rows to prune in one call
	)
	var totalRowsDeleted int64

	for i := 0; i < maxBatchesPerCall; i++ {
		if ctx.Err() != nil {
			return fmt.Errorf("prune canceled after %d rows: %w", totalRowsDeleted, ctx.Err())
		}
		if totalRowsDeleted >= maxRowsPerCall {
			return fmt.Errorf("prune row budget exhausted after %d rows; retry required", totalRowsDeleted)
		}

		// scale-guard:ignore: bounded per-run batch-delete loop (maxBatchesPerCall/maxRowsPerCall/ctx-deadline guards below cap it; same shape as the obligation/idempotency sweepers), not an unbounded per-row DB call.
		tag, err := r.pool.Exec(ctx, `
WITH serving AS (
  SELECT serving_projection_version
  FROM vaccination_shed_projection_state
  WHERE tenant_id = $1::uuid
    AND serving_projection_version IS NOT NULL
),
doomed AS (
  SELECT rows.vaccination_shed_projection_row_id
  FROM vaccination_shed_projection_rows rows
  JOIN serving ON true
  WHERE rows.tenant_id = $1::uuid
    AND rows.projection_version <> serving.serving_projection_version
  ORDER BY rows.projection_version, rows.vaccination_shed_projection_row_id
  LIMIT $2::int
)
DELETE FROM vaccination_shed_projection_rows rows
USING doomed
WHERE rows.vaccination_shed_projection_row_id = doomed.vaccination_shed_projection_row_id`,
			tenantID, batchSize)
		if err != nil {
			return fmt.Errorf("delete stale shed projection batch after %d rows: %w", totalRowsDeleted, err)
		}
		rowsAffected := tag.RowsAffected()
		totalRowsDeleted += rowsAffected
		if rowsAffected < int64(batchSize) {
			return nil
		}
	}
	return fmt.Errorf("prune batch budget exhausted after %d rows; retry required", totalRowsDeleted)
}

// shedProjectionServingVersion returns the tenant's serving vaccination-shed projection version and
// whether one exists. Not called by ShedSummary today (the request path is unchanged by this file); it
// exists so the parity test and the future request-path flip have one shared "is the projection usable"
// check, mirroring processintegrity's servingProjectionVersion.
func (r *Repository) shedProjectionServingVersion(ctx context.Context, tenantID string) (int64, bool) {
	var servingVersion int64
	err := r.pool.QueryRow(ctx, `
SELECT serving_projection_version
FROM vaccination_shed_projection_state
WHERE tenant_id = $1::uuid
  AND serving_projection_version IS NOT NULL
  AND serving_state IN ('fresh', 'stale', 'rebuilding')`, tenantID).Scan(&servingVersion)
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Default().WarnContext(ctx, "vaccination execution: shed projection serving-version lookup failed", "tenant_id", tenantID, "error", err)
		}
		return 0, false
	}
	return servingVersion, servingVersion > 0
}

func (r *Repository) compatibleShedProjection(ctx context.Context, tenantID string, asOf, dueBefore time.Time, historical bool) (int64, *domain.ProjectionFreshness, bool) {
	var version int64
	var projectedAt, projectedAsOf time.Time
	var status string
	err := r.pool.QueryRow(ctx, `
SELECT serving_projection_version,projected_at,as_of,freshness_status
FROM vaccination_shed_projection_state
WHERE tenant_id=$1::uuid AND serving_projection_version IS NOT NULL
  AND serving_state IN ('fresh','stale','rebuilding')
  AND (
    ($4::boolean AND as_of=$2::timestamptz AND due_before=$3::timestamptz)
    OR
    (NOT $4::boolean AND as_of <= $2::timestamptz
      AND $2::timestamptz-as_of <= $5::interval
      AND (as_of AT TIME ZONE 'Asia/Kolkata')::date=($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
      AND (due_before AT TIME ZONE 'Asia/Kolkata')::date=($3::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
  )`, tenantID, asOf, dueBefore, historical, maxVaccinationProjectionLiveLag.String()).Scan(&version, &projectedAt, &projectedAsOf, &status)
	if err != nil || version <= 0 {
		return 0, nil, false
	}
	lag := asOf.Sub(projectedAsOf)
	if lag < 0 {
		lag = 0
	}
	return version, &domain.ProjectionFreshness{ProjectionVersion: version, ProjectedAt: projectedAt, AsOf: projectedAsOf, Status: status, LagSeconds: int64(lag.Seconds())}, true
}

func (r *Repository) listShedProjection(ctx context.Context, q domain.ShedSummaryQuery) ([]domain.ShedSummaryProjection, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	dueBefore := q.DueBefore
	if dueBefore.IsZero() {
		dueBefore = asOf.Add(defaultExecutionHorizon)
	}
	version, freshness, ok := r.compatibleShedProjection(ctx, q.TenantID, asOf, dueBefore, q.HistoricalAsOf)
	if !ok {
		return nil, domain.ErrProjectionUnavailable
	}
	status, capacity := "", ""
	if q.Status != nil {
		status = string(*q.Status)
	}
	if q.Capacity != nil {
		capacity = string(*q.Capacity)
	}
	query := strings.Replace(shedProjectionReadSQL, "__ORDER_BY__", shedSummaryOrderBy(q.Sort), 1)
	rows, err := r.pool.Query(ctx, query, q.TenantID, version, optStr(q.ParkID), optStr(q.ShedID), optStr(q.Search), status, capacity, limit, q.Offset)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: shed projection read: %w", err)
	}
	defer rows.Close()
	out := []domain.ShedSummaryProjection{}
	for rows.Next() {
		var row domain.ShedSummaryProjection
		var lastDone, nextDue pgtype.Timestamptz
		var capacityStatus, shedStatus string
		if err := rows.Scan(&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName, &row.Animals, &row.DueAnimals, &row.OpenCells, &row.Sessions, &capacityStatus, &shedStatus, &lastDone, &nextDue, &row.TotalCount); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan shed projection: %w", err)
		}
		row.Capacity, row.Status = domain.CapacityStatus(capacityStatus), domain.ShedStatus(shedStatus)
		row.LastDone, row.NextDue, row.Freshness = timePtr(lastDone), timePtr(nextDue), freshness
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

const shedProjectionReadSQL = `
SELECT park_id,park_name,shed_id,shed_name,animals,due_animals,open_cells,sessions,
  capacity_status,shed_status,last_done,next_due,COUNT(*) OVER()::bigint
FROM vaccination_shed_projection_rows
WHERE tenant_id=$1::uuid AND projection_version=$2::bigint
  AND ($3::text='' OR park_id=$3) AND ($4::text='' OR shed_id=$4)
  AND ($5::text='' OR shed_name ILIKE '%'||$5||'%' OR park_name ILIKE '%'||$5||'%')
  AND ($6::text='' OR shed_status=$6) AND ($7::text='' OR capacity_status=$7)
ORDER BY __ORDER_BY__ LIMIT $8 OFFSET $9;`

// vaccinationShedProjectionInsertSQL replays the exact same alive/completions/asof_terminal/raw/
// effective/due_agg/shed_rows/scored/classified chain as shedSummarySQL (repository.go), with the
// park/shed/search/status/capacity filters and LIMIT/OFFSET removed -- this is a full, unfiltered
// per-tenant materialization, not a filtered page. Keeping the two chains textually independent (rather
// than sharing a Go string fragment) mirrors how vaccinationExecutionSQL/vaccinationOperationsSQL already
// duplicate similar CTE shapes in this package; it also means this file can never accidentally change
// ShedSummary's live request-path behavior.
// scale-guard:ignore: off-request projector recompute only (RecomputeShedProjection); ShedSummary's own request-path god-CTE stays the baselined C35-002 debt in repository.go until the explicit read flip.
const vaccinationShedProjectionInsertSQL = `
WITH alive AS (
  SELECT g.shed_id AS shed_uuid, COUNT(*)::bigint AS animals
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.lifecycle_status = 'alive'
    AND g.merged_into_goat_id IS NULL
    AND g.shed_id IS NOT NULL
  GROUP BY g.shed_id
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
      obligation_id, administered_at, created_at,
      CASE
        WHEN status IN ('accepted', 'rejected') AND verified_at IS NOT NULL AND verified_at > $2::timestamptz THEN 'recorded'
        ELSE status
      END AS asof_status
    FROM vaccination_completions
    WHERE tenant_id = $1::uuid
      AND COALESCE(administered_at, created_at) <= $2::timestamptz
  ) c
  GROUP BY obligation_id
),
asof_terminal AS (
  SELECT
    obligation_id,
    (ARRAY_AGG(event_type ORDER BY occurred_at DESC, obligation_event_id DESC)
       FILTER (WHERE occurred_at <= $2::timestamptz))[1] AS asof_terminal_type,
    true AS has_terminal_event
  FROM obligation_status_events
  WHERE tenant_id = $1::uuid
    AND event_type IN ('missed', 'waived', 'deferred')
  GROUP BY obligation_id
),
raw AS (
  SELECT
    oi.obligation_id,
    oi.due_at,
    oi.window_start,
    oi.completed_at,
    oi.status AS stored_status,
    te.asof_terminal_type,
    te.has_terminal_event,
    oi.target_id AS goat_id,
    g.shed_id AS shed_uuid,
    c.effective_status AS completion_status,
    c.last_accepted_at
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
   AND g.lifecycle_status = 'alive'
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND oi.due_at <= $3::timestamptz
    AND g.shed_id IS NOT NULL
),
effective AS (
  SELECT
    raw.goat_id,
    raw.shed_uuid,
    raw.due_at,
    raw.last_accepted_at,
    raw.completion_status,
    CASE
      WHEN raw.stored_status = 'completed' THEN
        CASE
          WHEN raw.completed_at IS NOT NULL AND raw.completed_at <= $2::timestamptz THEN 'completed'
          WHEN raw.completed_at IS NULL AND raw.completion_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN raw.due_at < $2::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due' ELSE 'scheduled' END)
        END
      WHEN raw.stored_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          WHEN raw.asof_terminal_type IS NOT NULL THEN raw.asof_terminal_type
          WHEN raw.has_terminal_event THEN (CASE WHEN raw.due_at < $2::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due' ELSE 'scheduled' END)
          ELSE raw.stored_status
        END
      WHEN raw.stored_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN raw.due_at < $2::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM raw
),
due_agg AS (
  SELECT
    effective.shed_uuid,
    COUNT(DISTINCT effective.goat_id) FILTER (
      WHERE effective.eff_status IN ('overdue', 'due', 'in_progress')
         OR effective.completion_status IN ('recorded', 'rejected')
    )::bigint AS due_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'overdue')::bigint AS overdue_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'scheduled')::bigint AS scheduled_animals,
    COUNT(*) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress'))::bigint AS open_cells,
    MAX(effective.last_accepted_at) AS last_done,
    MIN(effective.due_at) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress', 'scheduled')) AS next_due
  FROM effective
  GROUP BY effective.shed_uuid
),
shed_rows AS (
  SELECT
    park.location_id::text AS park_id,
    park.name AS park_name,
    shed.location_id::text AS shed_id,
    shed.name AS shed_name,
    alive.animals,
    COALESCE(due_agg.due_animals, 0) AS due_animals,
    COALESCE(due_agg.overdue_animals, 0) AS overdue_animals,
    COALESCE(due_agg.scheduled_animals, 0) AS scheduled_animals,
    COALESCE(due_agg.open_cells, 0) AS open_cells,
    due_agg.last_done,
    due_agg.next_due
  FROM alive
  JOIN locations shed
    ON shed.tenant_id = $1::uuid
   AND shed.location_id = alive.shed_uuid
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  JOIN locations park
    ON park.tenant_id = $1::uuid
   AND park.location_id = shed.parent_location_id
   AND park.location_type = 'park'
   AND park.status = 'active'
  LEFT JOIN due_agg ON due_agg.shed_uuid = alive.shed_uuid
),
scored AS (
  SELECT
    shed_rows.*,
    CASE WHEN open_cells <= 0 THEN 0 ELSE CEIL(open_cells::numeric / GREATEST($4::numeric, 1))::int END AS sessions
  FROM shed_rows
),
classified AS (
  SELECT
    scored.*,
    CASE
      WHEN sessions <= 1 THEN 'within_cap'
      WHEN sessions <= ($5::int + 1) THEN 'over_cap'
      ELSE 'capacity_breach'
    END AS capacity_status,
    CASE
      WHEN overdue_animals > 0 THEN 'overdue'
      WHEN sessions > ($5::int + 1) THEN 'needs_review'
      WHEN sessions > 1 THEN 'split'
      WHEN due_animals > 0 THEN 'due'
      WHEN scheduled_animals > 0 THEN 'scheduled'
      ELSE 'on_track'
    END AS shed_status
  FROM scored
)
INSERT INTO vaccination_shed_projection_rows (
  tenant_id, park_id, park_name, shed_id, shed_name,
  animals, due_animals, open_cells, sessions,
  capacity_status, shed_status, last_done, next_due,
  projection_version, projected_at
)
SELECT
  $1::uuid, park_id, park_name, shed_id, shed_name,
  animals, due_animals, open_cells, sessions,
  capacity_status, shed_status, last_done, next_due,
  $6::bigint, $7::timestamptz
FROM classified
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
  updated_at = now();
`

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// This file is the projector half of the Calendar completed-history + date-marker projection: it
// builds and refreshes calendar_history_projection_rows / calendar_history_date_markers /
// calendar_history_projection_state (migration 000178) off the request path, replacing the
// completed_history CTE in calendarListSQL, the vaccination_completions aggregate branch in
// calendarDateMarkersSQL, and calendarCompletedHistoryDetailSQL -- all three used to reconstruct
// accepted vaccination history from vaccination_completions/obligation_instances/protocol_* on every
// request (repository.go). The request path now serves history exclusively from this indexed,
// tenant + serving-version scoped projection. RecomputeVaccinationHistoryProjection is the only
// writer. See docs/decisions/high-scale-dashboard-projections.md (Serving-Read Freshness Contract)
// and the sibling vaccinationexecution/adapters/postgres/shed_projection.go, whose advisory-lock /
// version-stamp / insert / state-upsert / bounded-prune / mark-failed-on-error shape this file mirrors.

const (
	// defaultHistoryProjectionFresh bounds how old calendar_history_projection_state.projected_at may
	// be before a completed/history read is treated as stale (C5-002). This intentionally does NOT
	// reuse the 7-minute defaultProjectionFresh TTL used by the fast-moving UPCOMING (vaccination-
	// execution/shed) gate in repository.go: history is append-mostly (accepted completions never
	// change after the fact) and this projector runs on a much less frequent schedule (C5-001), so a
	// 7-minute TTL would falsely flag history stale on every normal cycle. 90 minutes gives generous
	// headroom over the projector's own refresh cadence while still catching a genuinely broken
	// projector. A stale history projection is NEVER fail-closed here -- last-known-good rows are
	// still served, with Stale surfaced in the response's HistoryProjection metadata; only a projection
	// that has NEVER been built (no serving_projection_version at all) fails closed with
	// ErrProjectionUnavailable, see historyProjectionFreshness in repository.go.
	defaultHistoryProjectionFresh            = 90 * time.Minute
	calendarHistoryProjectionPruneBatchSize  = 5000
	defaultCalendarHistoryProjectionLookback = 400 * 24 * time.Hour
	defaultCalendarHistoryProjectionLookahd  = 24 * time.Hour
)

// RecomputeVaccinationHistoryProjection rebuilds the tenant's calendar_history_projection_rows and
// calendar_history_date_markers from the canonical vaccination_completions/obligation_instances/
// protocol_* tables the request path used to join on read, then atomically flips
// calendar_history_projection_state.serving_projection_version to the new version inside the same
// transaction as the row/marker inserts (version-swap, never a live whole-tenant delete+reinsert).
func (r *Repository) RecomputeVaccinationHistoryProjection(ctx context.Context, in ports.RefreshVaccinationHistoryProjection) (rows int64, retErr error) {
	if strings.TrimSpace(in.TenantID) == "" {
		return 0, fmt.Errorf("calendar: recompute history projection: tenant id is required")
	}

	committed := false
	defer func() {
		// A post-commit maintenance failure must be visible, but must not mark the newly committed
		// serving projection as failed -- readers (once flipped) can continue from last-known-good
		// while the scheduled projector retries cleanup on its next run.
		if retErr != nil && !committed {
			r.markHistoryProjectionFailed(in.TenantID, retErr)
		}
	}()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("calendar: recompute history projection: begin: %w", err)
	}
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 86173))`, in.TenantID); err != nil {
		return 0, fmt.Errorf("calendar: recompute history projection: lock: %w", err)
	}

	// Derive defaults AFTER acquiring the advisory lock so a queued build never publishes an
	// already-stale window computed before it had to wait for a prior writer.
	dateFrom := in.DateFrom
	dateTo := in.DateTo
	if dateFrom.IsZero() {
		dateFrom = time.Now().In(biztime.DefaultLocation()).Add(-defaultCalendarHistoryProjectionLookback)
	}
	if dateTo.IsZero() {
		dateTo = time.Now().In(biztime.DefaultLocation()).Add(defaultCalendarHistoryProjectionLookahd)
	}

	var projectionVersion int64
	var projectedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT (extract(epoch FROM clock_timestamp()) * 1000000)::bigint, clock_timestamp()`).Scan(&projectionVersion, &projectedAt); err != nil {
		return 0, fmt.Errorf("calendar: recompute history projection: stamp: %w", err)
	}

	if _, err := tx.Exec(ctx, calendarHistoryProjectionInsertSQL,
		in.TenantID, dateFrom, dateTo, projectionVersion, projectedAt); err != nil {
		return 0, fmt.Errorf("calendar: recompute history projection: insert rows: %w", err)
	}

	var rowCount int64
	if err := tx.QueryRow(ctx, `
SELECT COUNT(*)::bigint
FROM calendar_history_projection_rows
WHERE tenant_id = $1::uuid
  AND projection_version = $2::bigint`, in.TenantID, projectionVersion).Scan(&rowCount); err != nil {
		return 0, fmt.Errorf("calendar: recompute history projection: count rows: %w", err)
	}

	if _, err := tx.Exec(ctx, calendarHistoryDateMarkersInsertSQL, in.TenantID, projectionVersion); err != nil {
		return rowCount, fmt.Errorf("calendar: recompute history projection: insert markers: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO calendar_history_projection_state (
  tenant_id, projection_version, serving_projection_version, projected_at, date_from, date_to,
  freshness_status, serving_state, last_error, updated_at
) VALUES (
  $1::uuid, $2::bigint, $2::bigint, $3::timestamptz, $4::timestamptz, $5::timestamptz,
  'green', 'fresh', NULL, now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
  projection_version = EXCLUDED.projection_version,
  serving_projection_version = EXCLUDED.serving_projection_version,
  projected_at = EXCLUDED.projected_at,
  date_from = EXCLUDED.date_from,
  date_to = EXCLUDED.date_to,
  freshness_status = 'green',
  serving_state = 'fresh',
  last_error = NULL,
  updated_at = now()`,
		in.TenantID, projectionVersion, projectedAt, dateFrom, dateTo); err != nil {
		return rowCount, fmt.Errorf("calendar: recompute history projection: upsert state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return rowCount, fmt.Errorf("calendar: recompute history projection: commit: %w", err)
	}
	committed = true

	if err := r.pruneOldHistoryProjection(ctx, in.TenantID); err != nil {
		r.markHistoryProjectionMaintenanceError(in.TenantID, err)
		return rowCount, fmt.Errorf("calendar: recompute history projection: serving projection committed but stale-row prune failed: %w", err)
	}
	return rowCount, nil
}

func (r *Repository) markHistoryProjectionFailed(tenantID string, cause error) {
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
UPDATE calendar_history_projection_state
SET freshness_status = CASE WHEN serving_projection_version IS NULL THEN 'red' ELSE freshness_status END,
    serving_state = CASE WHEN serving_projection_version IS NULL THEN 'failed' ELSE serving_state END,
    last_error = $2::text,
    updated_at = now()
WHERE tenant_id = $1::uuid`, tenantID, message)
}

func (r *Repository) markHistoryProjectionMaintenanceError(tenantID string, cause error) {
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
UPDATE calendar_history_projection_state
SET freshness_status = 'yellow',
    last_error = $2::text,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND serving_projection_version IS NOT NULL`, tenantID, message)
}

func (r *Repository) pruneOldHistoryProjection(ctx context.Context, tenantID string) error {
	if err := r.pruneOldHistoryProjectionRowsWithBatchSize(ctx, tenantID, calendarHistoryProjectionPruneBatchSize); err != nil {
		return fmt.Errorf("prune history projection rows: %w", err)
	}
	if err := r.pruneOldHistoryDateMarkersWithBatchSize(ctx, tenantID, calendarHistoryProjectionPruneBatchSize); err != nil {
		return fmt.Errorf("prune history date markers: %w", err)
	}
	return nil
}

// pruneOldHistoryProjectionRowsWithBatchSize deletes rows left behind by earlier projection_versions
// in bounded batches (never one unbounded whole-tenant DELETE), the same chunked-loop shape the
// obligation/idempotency sweepers and vaccinationexecution's pruneOldShedProjectionRowsWithBatchSize
// use elsewhere in this codebase. It re-derives serving_projection_version INSIDE the DELETE so an
// overlapping newer build that already published cannot have its live rows deleted by a stale prune.
func (r *Repository) pruneOldHistoryProjectionRowsWithBatchSize(ctx context.Context, tenantID string, batchSize int32) error {
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

		// scale-guard:ignore: bounded per-run batch-delete loop (maxBatchesPerCall/maxRowsPerCall/ctx-deadline guards below cap it; same shape as the obligation/idempotency sweepers and vaccinationexecution's shed-projection prune), not an unbounded per-row DB call.
		tag, err := r.pool.Exec(ctx, `
WITH serving AS (
  SELECT serving_projection_version
  FROM calendar_history_projection_state
  WHERE tenant_id = $1::uuid
    AND serving_projection_version IS NOT NULL
),
doomed AS (
  SELECT rows.tenant_id, rows.projection_version, rows.event_id
  FROM calendar_history_projection_rows rows
  JOIN serving ON true
  WHERE rows.tenant_id = $1::uuid
    AND rows.projection_version <> serving.serving_projection_version
  ORDER BY rows.projection_version, rows.event_id
  LIMIT $2::int
)
DELETE FROM calendar_history_projection_rows rows
USING doomed
WHERE rows.tenant_id = doomed.tenant_id
  AND rows.projection_version = doomed.projection_version
  AND rows.event_id = doomed.event_id`,
			tenantID, batchSize)
		if err != nil {
			return fmt.Errorf("delete stale history projection row batch after %d rows: %w", totalRowsDeleted, err)
		}
		rowsAffected := tag.RowsAffected()
		totalRowsDeleted += rowsAffected
		if rowsAffected < int64(batchSize) {
			return nil
		}
	}
	return fmt.Errorf("prune batch budget exhausted after %d rows; retry required", totalRowsDeleted)
}

// pruneOldHistoryDateMarkersWithBatchSize is the marker-table twin of
// pruneOldHistoryProjectionRowsWithBatchSize; the two tables are pruned in independent bounded loops
// so a large marker cleanup can never block/starve the row cleanup or vice versa.
func (r *Repository) pruneOldHistoryDateMarkersWithBatchSize(ctx context.Context, tenantID string, batchSize int32) error {
	if strings.TrimSpace(tenantID) == "" || batchSize <= 0 {
		return fmt.Errorf("invalid prune request: tenant_id and positive batch_size are required")
	}
	const (
		maxBatchesPerCall = 1000
		maxRowsPerCall    = 1_000_000
	)
	var totalRowsDeleted int64

	for i := 0; i < maxBatchesPerCall; i++ {
		if ctx.Err() != nil {
			return fmt.Errorf("prune canceled after %d rows: %w", totalRowsDeleted, ctx.Err())
		}
		if totalRowsDeleted >= maxRowsPerCall {
			return fmt.Errorf("prune row budget exhausted after %d rows; retry required", totalRowsDeleted)
		}

		// scale-guard:ignore: bounded per-run batch-delete loop (maxBatchesPerCall/maxRowsPerCall/ctx-deadline guards below cap it; same shape as pruneOldHistoryProjectionRowsWithBatchSize above), not an unbounded per-row DB call.
		tag, err := r.pool.Exec(ctx, `
WITH serving AS (
  SELECT serving_projection_version
  FROM calendar_history_projection_state
  WHERE tenant_id = $1::uuid
    AND serving_projection_version IS NOT NULL
),
doomed AS (
  SELECT m.tenant_id, m.projection_version, m.business_date, m.park_key, m.shed_key
  FROM calendar_history_date_markers m
  JOIN serving ON true
  WHERE m.tenant_id = $1::uuid
    AND m.projection_version <> serving.serving_projection_version
  ORDER BY m.projection_version, m.business_date, m.park_key, m.shed_key
  LIMIT $2::int
)
DELETE FROM calendar_history_date_markers m
USING doomed
WHERE m.tenant_id = doomed.tenant_id
  AND m.projection_version = doomed.projection_version
  AND m.business_date = doomed.business_date
  AND m.park_key = doomed.park_key
  AND m.shed_key = doomed.shed_key`,
			tenantID, batchSize)
		if err != nil {
			return fmt.Errorf("delete stale history date marker batch after %d rows: %w", totalRowsDeleted, err)
		}
		rowsAffected := tag.RowsAffected()
		totalRowsDeleted += rowsAffected
		if rowsAffected < int64(batchSize) {
			return nil
		}
	}
	return fmt.Errorf("prune batch budget exhausted after %d rows; retry required", totalRowsDeleted)
}

// historyProjectionServingVersion returns the tenant's serving calendar-history projection version
// and whether one exists. Exposed for tests and callers that want to assert on the published version
// directly rather than through a full read.
func (r *Repository) historyProjectionServingVersion(ctx context.Context, tenantID string) (int64, bool) {
	var servingVersion int64
	err := r.pool.QueryRow(ctx, `
SELECT serving_projection_version
FROM calendar_history_projection_state
WHERE tenant_id = $1::uuid
  AND serving_projection_version IS NOT NULL`, tenantID).Scan(&servingVersion)
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Default().WarnContext(ctx, "calendar: history projection serving-version lookup failed", "tenant_id", tenantID, "error", err)
		}
		return 0, false
	}
	return servingVersion, servingVersion > 0
}

// calendarHistoryProjectionInsertSQL ports the exact same completed_history grouping (joins, scope
// hierarchy, filters) as the completed_history CTE that used to live inline in calendarListSQL
// (repository.go), plus the exact detail jsonb that calendarCompletedHistoryDetailSQL used to build --
// so projected rows are byte-parity with the old live query's output, just computed off the request
// path. Keeping this textually independent from repository.go's read-side SQL (rather than sharing a
// Go string fragment) mirrors how vaccinationexecution's vaccinationShedProjectionInsertSQL stays
// independent of ShedSummary's read SQL: this file can never accidentally change a live read path.
// scale-guard:ignore: off-request projector recompute only (RecomputeVaccinationHistoryProjection); this replays the canonical join once per run, not on the request path.
const calendarHistoryProjectionInsertSQL = `
WITH grouped AS (
  SELECT
    'history:' || ((vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date)::text || ':' ||
      COALESCE(loc.park_id::text, 'none') || ':' ||
      COALESCE(loc.shed_id::text, 'none') || ':' ||
      pr.rule_id::text AS event_id,
    ((vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date) AS business_date,
    loc.park_id,
    loc.park_code,
    loc.shed_id,
    loc.shed_name,
    pd.protocol_id,
    pv.protocol_version_id,
    pr.rule_id,
    COALESCE(NULLIF(prd.vaccine_json->>'name', ''), pd.name) AS vaccine_name,
    pr.dose_code,
    COALESCE(NULLIF(prd.vaccine_json->>'name', ''), pd.name) || ' · ' ||
      CASE
        WHEN COALESCE(NULLIF(prd.source_dose_code, ''), pr.dose_code) LIKE '%_first' THEN 'First dose'
        WHEN COALESCE(NULLIF(prd.source_dose_code, ''), pr.dose_code) LIKE '%_booster' THEN 'Booster'
        WHEN COALESCE(NULLIF(prd.source_dose_code, ''), pr.dose_code) LIKE '%adult_revac%' THEN 'Revaccination'
        ELSE COALESCE(NULLIF(prd.source_dose_code, ''), pr.dose_code)
      END || ' completed' AS title,
    COALESCE(loc.shed_name, loc.park_code, 'Accepted vaccination history') AS subtitle,
    count(*)::int AS target_count,
    min(vc.administered_at) AS window_start,
    max(vc.administered_at) AS window_end,
    jsonb_build_object(
      'summary', jsonb_build_object('owner', 'PC', 'target_count', count(*), 'history', true),
      'source_and_rule', jsonb_build_object('protocol_version_id', pv.protocol_version_id, 'rule_id', pr.rule_id, 'source_backed', true),
      'execution', jsonb_build_object('work_state', 'completed', 'completed_count', count(*)),
      'stock', jsonb_build_object(),
      'proof', jsonb_build_object('state', 'accepted'),
      'verification', jsonb_build_object('state', 'accepted'),
      'notification_channels', jsonb_build_array(),
      'notification_policy', jsonb_build_object('nudge_allowed', false),
      'links', jsonb_build_object('vaccination', '/vaccination')
    ) AS detail
  FROM vaccination_completions vc
  JOIN obligation_instances oi
    ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
  LEFT JOIN protocol_rule_dimensions prd
    ON prd.tenant_id = pr.tenant_id AND prd.rule_id = pr.rule_id
  LEFT JOIN locations scope_loc
    ON scope_loc.tenant_id = oi.tenant_id
   AND scope_loc.location_id = oi.scope_id
   AND oi.scope_type IN ('park', 'shed', 'cohort')
  LEFT JOIN locations scope_parent
    ON scope_parent.tenant_id = oi.tenant_id
   AND scope_parent.location_id = scope_loc.parent_location_id
  LEFT JOIN locations scope_grand
    ON scope_grand.tenant_id = oi.tenant_id
   AND scope_grand.location_id = scope_parent.parent_location_id
  LEFT JOIN LATERAL (
    SELECT
      CASE
        WHEN oi.scope_type = 'park' THEN scope_loc.location_id
        WHEN oi.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id
        WHEN oi.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_id
      END AS park_id,
      CASE
        WHEN oi.scope_type = 'park' THEN scope_loc.location_code
        WHEN oi.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_code
        WHEN oi.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_code
      END AS park_code,
      CASE
        WHEN oi.scope_type = 'shed' THEN scope_loc.location_id
        WHEN oi.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.location_id
      END AS shed_id,
      CASE
        WHEN oi.scope_type = 'shed' THEN scope_loc.name
        WHEN oi.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.name
      END AS shed_name
  ) loc ON true
  WHERE vc.tenant_id = $1::uuid
    AND vc.status = 'accepted'
    AND oi.status = 'completed'
    AND oi.target_type = 'goat'
    AND pd.category = 'vaccination'
    AND vc.administered_at >= $2::timestamptz
    AND vc.administered_at < $3::timestamptz
  GROUP BY
    loc.park_id, loc.park_code, loc.shed_id, loc.shed_name,
    pd.protocol_id, pv.protocol_version_id, pr.rule_id, pd.name, pr.dose_code,
    prd.vaccine_json, prd.source_dose_code,
    (vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date
)
INSERT INTO calendar_history_projection_rows (
  tenant_id, event_id, business_date, park_id, park_code, shed_id, shed_name,
  protocol_id, protocol_version_id, rule_id, vaccine_name, dose_code,
  title, subtitle, target_count, window_start, window_end, detail,
  projection_version, projected_at
)
SELECT
  $1::uuid, event_id, business_date, park_id, park_code, shed_id, shed_name,
  protocol_id, protocol_version_id, rule_id, vaccine_name, dose_code,
  title, subtitle, target_count, window_start, window_end, detail,
  $4::bigint, $5::timestamptz
FROM grouped
ON CONFLICT (tenant_id, projection_version, event_id) DO UPDATE SET
  business_date = EXCLUDED.business_date,
  park_id = EXCLUDED.park_id,
  park_code = EXCLUDED.park_code,
  shed_id = EXCLUDED.shed_id,
  shed_name = EXCLUDED.shed_name,
  protocol_id = EXCLUDED.protocol_id,
  protocol_version_id = EXCLUDED.protocol_version_id,
  rule_id = EXCLUDED.rule_id,
  vaccine_name = EXCLUDED.vaccine_name,
  dose_code = EXCLUDED.dose_code,
  title = EXCLUDED.title,
  subtitle = EXCLUDED.subtitle,
  target_count = EXCLUDED.target_count,
  window_start = EXCLUDED.window_start,
  window_end = EXCLUDED.window_end,
  detail = EXCLUDED.detail,
  projected_at = EXCLUDED.projected_at,
  updated_at = now();
`

// calendarHistoryDateMarkersInsertSQL derives date markers from the SAME grouped set just written to
// calendar_history_projection_rows for this run: completion_count = SUM(target_count) per
// (business_date, park, shed), which equals the live marker branch's per-date/scope count(*) over
// accepted completions (each projection row's target_count is already a count(*) of completions for
// that park/shed/rule/day group, so summing across rule/protocol groups for a shed reproduces the
// shed's total for that day).
const calendarHistoryDateMarkersInsertSQL = `
INSERT INTO calendar_history_date_markers (
  tenant_id, projection_version, business_date, park_key, shed_key, park_id, shed_id,
  completion_count, projected_at
)
SELECT
  tenant_id, projection_version, business_date,
  COALESCE(park_id::text, 'none') AS park_key,
  COALESCE(shed_id::text, 'none') AS shed_key,
  park_id, shed_id,
  SUM(target_count)::bigint AS completion_count,
  MAX(projected_at) AS projected_at
FROM calendar_history_projection_rows
WHERE tenant_id = $1::uuid AND projection_version = $2::bigint
GROUP BY tenant_id, projection_version, business_date, park_id, shed_id
ON CONFLICT (tenant_id, projection_version, business_date, park_key, shed_key) DO UPDATE SET
  completion_count = EXCLUDED.completion_count,
  park_id = EXCLUDED.park_id,
  shed_id = EXCLUDED.shed_id,
  projected_at = EXCLUDED.projected_at;
`

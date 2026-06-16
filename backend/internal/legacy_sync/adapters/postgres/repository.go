package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/legacy_sync/domain"
	"github.com/vgoats/goatos/backend/internal/legacy_sync/ports"
)

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = 3 * time.Second
	}
	return &Repository{pool: pool, queryTimeout: queryTimeout}
}

func (r *Repository) ListSources(ctx context.Context, filter ports.SourceFilter) ([]domain.Source, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := uuidParam(filter.TenantID); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
WITH registered_base AS (
  SELECT
    s.source_id,
    s.source_name,
    s.domain,
    s.source_kind,
    concat_ws(':', s.source_kind, s.source_id) AS source_ref,
    s.cadence_seconds,
    s.green_within_seconds,
    s.yellow_within_seconds,
    s.criticality,
    s.enabled,
    s.known_degraded,
    s.notes,
    COALESCE(st.source_watermark_at, w.last_success_window_end, w.last_success_at) AS source_watermark_at,
    COALESCE(st.observed_at, w.updated_at) AS observed_at,
    COALESCE(st.rows_seen, 0) AS rows_seen,
    false AS is_unknown_source
  FROM legacy_sync_sources s
  LEFT JOIN legacy_sync_source_status st
    ON st.tenant_id = $1::uuid
   AND st.source_id = s.source_id
  LEFT JOIN legacy_sync_source_watermarks w
    ON w.tenant_id = $1::uuid
   AND w.source_id = s.source_id
  WHERE ($2::text = '' OR s.domain = $2::text)
),
registered AS (
  SELECT
    source_id,
    source_name,
    domain,
    source_kind,
    source_ref,
    cadence_seconds,
    green_within_seconds,
    yellow_within_seconds,
    criticality,
    enabled,
    known_degraded,
    notes,
    CASE
      WHEN known_degraded THEN 'red'
      WHEN source_watermark_at IS NULL THEN 'unknown'
      WHEN EXTRACT(EPOCH FROM now() - source_watermark_at)::int <= green_within_seconds THEN 'green'
      WHEN EXTRACT(EPOCH FROM now() - source_watermark_at)::int <= yellow_within_seconds THEN 'yellow'
      ELSE 'red'
    END AS freshness_status,
    CASE
      WHEN known_degraded THEN 'source is marked known degraded'
      WHEN source_watermark_at IS NULL THEN 'no successful watermark recorded'
      WHEN EXTRACT(EPOCH FROM now() - source_watermark_at)::int <= green_within_seconds THEN 'source watermark is within green threshold'
      WHEN EXTRACT(EPOCH FROM now() - source_watermark_at)::int <= yellow_within_seconds THEN 'source watermark is within yellow threshold'
      ELSE 'source watermark is older than yellow threshold'
    END AS status_reason,
    source_watermark_at,
    observed_at,
    rows_seen,
    is_unknown_source
  FROM registered_base
),
unregistered AS (
  SELECT
    st.source_id,
    st.source_id AS source_name,
    'unknown'::text AS domain,
    'export'::text AS source_kind,
    concat_ws(':', 'observed', st.source_id) AS source_ref,
    43200::int AS cadence_seconds,
    46800::int AS green_within_seconds,
    54000::int AS yellow_within_seconds,
    'noncritical'::text AS criticality,
    true AS enabled,
    false AS known_degraded,
    'Observed status for a source not registered in legacy_sync_sources.'::text AS notes,
    CASE WHEN st.freshness_status = 'green' THEN 'unknown' ELSE st.freshness_status END AS freshness_status,
    COALESCE(NULLIF(st.status_reason, ''), 'unregistered_source') AS status_reason,
    st.source_watermark_at,
    st.observed_at,
    st.rows_seen,
    true AS is_unknown_source
  FROM legacy_sync_source_status st
  LEFT JOIN legacy_sync_sources s ON s.source_id = st.source_id
  WHERE st.tenant_id = $1::uuid
    AND s.source_id IS NULL
    AND ($2::text = '' OR $2::text = 'unknown')
)
SELECT *
FROM (
  SELECT * FROM registered
  UNION ALL
  SELECT * FROM unregistered
) source_rows
ORDER BY
  CASE criticality WHEN 'critical' THEN 0 ELSE 1 END,
  domain,
  source_id`, filter.TenantID, filter.Domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []domain.Source{}
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
}

func (r *Repository) OverallCounterStatus(ctx context.Context, tenantID string) (domain.CounterStatus, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := uuidParam(tenantID); err != nil {
		return domain.CounterStatus{}, err
	}
	var rebuildRequired bool
	var rebuildReason sql.NullString
	var updatedAt sql.NullTime
	err := r.pool.QueryRow(ctx, `
SELECT rebuild_required, rebuild_reason, updated_at
FROM goat_identity_counter_projection_state
WHERE tenant_id = $1::uuid`, tenantID).Scan(&rebuildRequired, &rebuildReason, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CounterStatus{
			FreshnessStatus: domain.FreshnessUnknown,
			StatusReason:    "counter projection has not been rebuilt",
			RebuildRequired: false,
		}, nil
	}
	if err != nil {
		return domain.CounterStatus{}, err
	}
	status := domain.FreshnessGreen
	reason := "counter projection is available"
	if rebuildRequired {
		status = domain.FreshnessRed
		reason = "counter projection requires rebuild"
		if rebuildReason.Valid && rebuildReason.String != "" {
			reason = rebuildReason.String
		}
	}
	return domain.CounterStatus{
		FreshnessStatus: status,
		StatusReason:    reason,
		UpdatedAt:       timePtr(updatedAt),
		RebuildRequired: rebuildRequired,
	}, nil
}

func (r *Repository) HasSuccessWatermark(ctx context.Context, tenantID, selectedDomain string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := uuidParam(tenantID); err != nil {
		return false, err
	}
	var exists bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM legacy_sync_source_watermarks w
  JOIN legacy_sync_sources s ON s.source_id = w.source_id
  WHERE w.tenant_id = $1::uuid
    AND w.last_success_at IS NOT NULL
    AND ($2::text = 'all' OR $2::text = '' OR s.domain = $2::text)
)::bool`, tenantID, selectedDomain).Scan(&exists)
	return exists, err
}

func (r *Repository) CreateRun(ctx context.Context, params ports.CreateRunParams) (*domain.Run, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := uuidParam(params.TenantID); err != nil {
		return nil, err
	}
	if _, err := uuidParam(params.ActorID); err != nil {
		return nil, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	run, err := insertRun(ctx, tx, params)
	if err != nil {
		return nil, err
	}
	for _, step := range params.Steps {
		details, err := json.Marshal(step.Details)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `
INSERT INTO legacy_sync_run_steps (
  sync_run_id,
  source_id,
  step_name,
  status,
  rows_read,
  rows_planned,
  rows_applied,
  rows_skipped,
  details,
  completed_at
) VALUES (
  $1::uuid,
  $2::text,
  $3,
  $4,
  $5,
  $6,
  $7,
  $8,
  $9::jsonb,
  CASE WHEN $10::bool THEN now() ELSE NULL END
)`, run.SyncRunID, step.SourceID, step.StepName, step.Status, step.RowsRead, step.RowsPlanned, step.RowsApplied, step.RowsSkipped, string(details), step.Completed)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return run, nil
}

func (r *Repository) ListRuns(ctx context.Context, filter ports.RunFilter) ([]domain.Run, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := uuidParam(filter.TenantID); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, runSelectSQL()+`
WHERE tenant_id = $1::uuid
ORDER BY started_at DESC, sync_run_id DESC
LIMIT $2`, filter.TenantID, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []domain.Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return runs, nil
}

func (r *Repository) GetRun(ctx context.Context, tenantID, runID string) (*domain.RunDetailResponse, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := uuidParam(tenantID); err != nil {
		return nil, err
	}
	if _, err := uuidParam(runID); err != nil {
		return nil, err
	}
	run, err := r.getRun(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	steps, err := r.listRunSteps(ctx, runID)
	if err != nil {
		return nil, err
	}
	sources, err := r.ListSources(ctx, ports.SourceFilter{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	logItems, err := r.listRunConflicts(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	return &domain.RunDetailResponse{
		Run:                 *run,
		Steps:               steps,
		Sources:             sources,
		SourceCorrectionLog: logItems,
	}, nil
}

func (r *Repository) CancelRun(ctx context.Context, tenantID, runID string) (*domain.Run, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := uuidParam(tenantID); err != nil {
		return nil, err
	}
	if _, err := uuidParam(runID); err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx, `
UPDATE legacy_sync_runs
SET
  status = CASE
    WHEN status IN ('completed', 'failed', 'blocked', 'canceled') THEN status
    ELSE 'canceled'
  END,
  cancel_requested_at = CASE
    WHEN status IN ('completed', 'failed', 'blocked', 'canceled') THEN cancel_requested_at
    ELSE COALESCE(cancel_requested_at, now())
  END,
  completed_at = CASE
    WHEN status IN ('completed', 'failed', 'blocked', 'canceled') THEN completed_at
    ELSE now()
  END,
  updated_at = CASE
    WHEN status IN ('completed', 'failed', 'blocked', 'canceled') THEN updated_at
    ELSE now()
  END
WHERE tenant_id = $1::uuid
  AND sync_run_id = $2::uuid
`+runReturningSQL(), tenantID, runID)
	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func insertRun(ctx context.Context, tx pgx.Tx, params ports.CreateRunParams) (*domain.Run, error) {
	row := tx.QueryRow(ctx, `
INSERT INTO legacy_sync_runs (
  tenant_id,
  requested_by,
  mode,
  domain,
  status,
  cold_start,
  source_window_start,
  source_window_end,
  eta_seconds,
  rows_read,
  rows_planned,
  rows_applied,
  rows_skipped,
  goats_created,
  goats_updated,
  conflicts_opened,
  conflicts_refreshed,
  counters_rebuilt,
  counter_check_status,
  freshness_status,
  blocked_reason,
  completed_at,
  trace_id
) VALUES (
  $1::uuid,
  $2::uuid,
  $3,
  $4,
  $5,
  $6,
  $7,
  $8,
  $9,
  $10,
  $11,
  $12,
  $13,
  $14,
  $15,
  $16,
  $17,
  $18,
  $19,
  $20,
  $21,
  CASE WHEN $5 IN ('completed', 'failed', 'blocked', 'canceled') THEN now() ELSE NULL END,
  $22
)
`+runReturningSQL(),
		params.TenantID,
		params.ActorID,
		params.Mode,
		params.Domain,
		params.Status,
		params.ColdStart,
		params.SourceWindowStart,
		params.SourceWindowEnd,
		params.ETASeconds,
		params.Summary.RowsRead,
		params.Summary.RowsPlanned,
		params.Summary.RowsApplied,
		params.Summary.RowsSkipped,
		params.Summary.GoatsCreated,
		params.Summary.GoatsUpdated,
		params.Summary.ConflictsOpened,
		params.Summary.ConflictsRefreshed,
		params.CountersRebuilt,
		params.CounterCheckStatus,
		params.FreshnessStatus,
		params.BlockedReason,
		params.TraceID,
	)
	run, err := scanRun(row)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *Repository) getRun(ctx context.Context, tenantID, runID string) (*domain.Run, error) {
	row := r.pool.QueryRow(ctx, runSelectSQL()+`
WHERE tenant_id = $1::uuid
  AND sync_run_id = $2::uuid`, tenantID, runID)
	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *Repository) listRunSteps(ctx context.Context, runID string) ([]domain.RunStep, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
  sync_step_id::text,
  source_id,
  step_name,
  status,
  rows_read,
  rows_planned,
  rows_applied,
  rows_skipped,
  details,
  started_at,
  completed_at
FROM legacy_sync_run_steps
WHERE sync_run_id = $1::uuid
ORDER BY started_at ASC, sync_step_id ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.RunStep{}
	for rows.Next() {
		var item domain.RunStep
		var sourceID sql.NullString
		var detailsBytes []byte
		var completedAt sql.NullTime
		if err := rows.Scan(
			&item.SyncStepID,
			&sourceID,
			&item.StepName,
			&item.Status,
			&item.RowsRead,
			&item.RowsPlanned,
			&item.RowsApplied,
			&item.RowsSkipped,
			&detailsBytes,
			&item.StartedAt,
			&completedAt,
		); err != nil {
			return nil, err
		}
		item.SourceID = stringPtr(sourceID)
		item.CompletedAt = timePtr(completedAt)
		item.Details = map[string]any{}
		if len(detailsBytes) > 0 {
			if err := json.Unmarshal(detailsBytes, &item.Details); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) listRunConflicts(ctx context.Context, tenantID, runID string) ([]domain.SourceCorrectionLogItem, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
  sync_run_conflict_id::text,
  source_id,
  source_record_id,
  conflict_id::text,
  goat_id::text,
  evidence_reason,
  result,
  old_goatos_value,
  new_legacy_value,
  previous_decision_id::text,
  previous_decision_at,
  audit_id::text,
  created_at
FROM legacy_sync_run_conflicts
WHERE tenant_id = $1::uuid
  AND sync_run_id = $2::uuid
ORDER BY created_at DESC, sync_run_conflict_id DESC
LIMIT 200`, tenantID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.SourceCorrectionLogItem{}
	for rows.Next() {
		var item domain.SourceCorrectionLogItem
		var conflictID, goatID, oldValue, newValue, decisionID, auditID sql.NullString
		var previousDecisionAt sql.NullTime
		if err := rows.Scan(
			&item.SyncRunConflictID,
			&item.SourceID,
			&item.SourceRecordID,
			&conflictID,
			&goatID,
			&item.EvidenceReason,
			&item.Result,
			&oldValue,
			&newValue,
			&decisionID,
			&previousDecisionAt,
			&auditID,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.ConflictID = stringPtr(conflictID)
		item.GoatID = stringPtr(goatID)
		item.OldGoatOSValue = stringPtr(oldValue)
		item.NewLegacyValue = stringPtr(newValue)
		item.PreviousDecisionID = stringPtr(decisionID)
		item.PreviousDecisionAt = timePtr(previousDecisionAt)
		item.AuditID = stringPtr(auditID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSource(row rowScanner) (domain.Source, error) {
	var source domain.Source
	var sourceRef, notes sql.NullString
	var watermark, observed sql.NullTime
	err := row.Scan(
		&source.SourceID,
		&source.SourceName,
		&source.Domain,
		&source.SourceKind,
		&sourceRef,
		&source.CadenceSeconds,
		&source.GreenWithinSeconds,
		&source.YellowWithinSeconds,
		&source.Criticality,
		&source.Enabled,
		&source.KnownDegraded,
		&notes,
		&source.FreshnessStatus,
		&source.StatusReason,
		&watermark,
		&observed,
		&source.RowsSeen,
		&source.IsUnknownSource,
	)
	if err != nil {
		return domain.Source{}, err
	}
	source.SourceRef = stringPtr(sourceRef)
	source.Notes = stringPtr(notes)
	source.SourceWatermarkAt = timePtr(watermark)
	source.ObservedAt = timePtr(observed)
	return source, nil
}

func scanRun(row rowScanner) (domain.Run, error) {
	var run domain.Run
	var windowStart, windowEnd, completedAt, cancelRequestedAt sql.NullTime
	var eta sql.NullInt64
	var blockedReason sql.NullString
	err := row.Scan(
		&run.SyncRunID,
		&run.Mode,
		&run.Domain,
		&run.Status,
		&run.ColdStart,
		&windowStart,
		&windowEnd,
		&eta,
		&run.Summary.RowsRead,
		&run.Summary.RowsPlanned,
		&run.Summary.RowsApplied,
		&run.Summary.RowsSkipped,
		&run.Summary.GoatsCreated,
		&run.Summary.GoatsUpdated,
		&run.Summary.ConflictsOpened,
		&run.Summary.ConflictsRefreshed,
		&run.CountersRebuilt,
		&run.CounterCheckStatus,
		&run.FreshnessStatus,
		&blockedReason,
		&cancelRequestedAt,
		&run.StartedAt,
		&completedAt,
	)
	if err != nil {
		return domain.Run{}, err
	}
	run.SourceWindowStart = timePtr(windowStart)
	run.SourceWindowEnd = timePtr(windowEnd)
	run.ETASeconds = intPtr(eta)
	run.BlockedReason = stringPtr(blockedReason)
	run.CancelRequestedAt = timePtr(cancelRequestedAt)
	run.CompletedAt = timePtr(completedAt)
	return run, nil
}

func runSelectSQL() string {
	return `
SELECT
  sync_run_id::text,
  mode,
  domain,
  status,
  cold_start,
  source_window_start,
  source_window_end,
  eta_seconds,
  rows_read,
  rows_planned,
  rows_applied,
  rows_skipped,
  goats_created,
  goats_updated,
  conflicts_opened,
  conflicts_refreshed,
  counters_rebuilt,
  counter_check_status,
  freshness_status,
  blocked_reason,
  cancel_requested_at,
  started_at,
  completed_at
FROM legacy_sync_runs
`
}

func runReturningSQL() string {
	return `
RETURNING
  sync_run_id::text,
  mode,
  domain,
  status,
  cold_start,
  source_window_start,
  source_window_end,
  eta_seconds,
  rows_read,
  rows_planned,
  rows_applied,
  rows_skipped,
  goats_created,
  goats_updated,
  conflicts_opened,
  conflicts_refreshed,
  counters_rebuilt,
  counter_check_status,
  freshness_status,
  blocked_reason,
  cancel_requested_at,
  started_at,
  completed_at`
}

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.queryTimeout)
}

func uuidParam(value string) (pgtype.UUID, error) {
	var out pgtype.UUID
	if err := out.Scan(value); err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid uuid %q: %w", value, err)
	}
	return out, nil
}

func stringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func timePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}

func intPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	out := int(v.Int64)
	return &out
}

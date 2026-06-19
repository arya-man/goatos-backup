package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

const defaultQueryTimeout = 3 * time.Second

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

func (r *Repository) GetDashboard(ctx context.Context, params ports.DashboardParams, traceID string) (*domain.DashboardResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	state, hasState, err := r.loadState(ctx, params)
	if err != nil {
		return nil, err
	}
	if !hasState {
		return emptyDashboard(params.View, traceID), nil
	}
	rows, err := r.loadRows(ctx, params, state.SnapshotDate)
	if err != nil {
		return nil, err
	}
	sections := defaultSections()
	summary := make([]domain.ProjectionRow, 0)
	for _, row := range rows {
		if row.Section == "summary" {
			summary = append(summary, row)
			continue
		}
		sections[row.Section] = append(sections[row.Section], row)
	}
	return &domain.DashboardResponse{
		View:              params.View,
		SnapshotDate:      state.SnapshotDate,
		SummarySourceDate: state.SummarySourceDate,
		Freshness:         state.Freshness,
		Summary:           summary,
		Sections:          sections,
		TraceID:           traceID,
	}, nil
}

func (r *Repository) ListSourceRows(ctx context.Context, params ports.SourceRowsParams) ([]ports.SourceRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, `
SELECT
  counts_source_row_id::text,
  source_system,
  source_id,
  source_table,
  source_row_key,
  source_watermark_date::text,
  payload_json,
  payload_hash
FROM counts_source_rows
WHERE tenant_id = $1::uuid
  AND row_status = 'current'
  AND ($2 = '' OR source_watermark_date = $2::date OR payload_json->>'snapshot_date' = $2)
ORDER BY source_watermark_date NULLS LAST, source_id, source_row_key, counts_source_row_id
LIMIT 50000`, params.TenantID, strings.TrimSpace(params.SnapshotDate))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.SourceRow{}
	for rows.Next() {
		var item ports.SourceRow
		var watermark pgtype.Text
		if err := rows.Scan(
			&item.SourceRowID,
			&item.SourceSystem,
			&item.SourceID,
			&item.SourceTable,
			&item.SourceRowKey,
			&watermark,
			&item.PayloadJSON,
			&item.PayloadHash,
		); err != nil {
			return nil, err
		}
		item.SourceWatermarkDate = textPtr(watermark)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) RunSync(ctx context.Context, cmd ports.SyncCommand) (*ports.SyncMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackCountsUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginCountsIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, "counts_sync_run")
	if err != nil {
		return nil, err
	}
	if replayed {
		response, err := loadCountsSyncRun(ctx, tx, cmd.TenantID, replayID, cmd.ClientIdempotencyKey, true, &replayID, cmd.TraceID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		committed = true
		return &ports.SyncMutationResult{Response: *response, Replayed: true, FirstResultID: &replayID}, nil
	}

	status := "completed"
	freshnessStatus := "green"
	servingState := "fresh"
	if cmd.SourceUnavailable {
		status = "source_unavailable"
		freshnessStatus = "unknown"
		servingState = "source_unavailable"
	}
	unavailableSources := cmd.UnavailableSources
	if unavailableSources == nil {
		unavailableSources = []string{}
	}
	unavailableJSON, err := json.Marshal(unavailableSources)
	if err != nil {
		return nil, err
	}
	var syncRunID string
	if err := tx.QueryRow(ctx, `
INSERT INTO counts_sync_runs (
  tenant_id, requested_by, mode, status, snapshot_date, source_rows_read,
  projection_rows_written, rows_skipped, unresolved_location_labels,
  freshness_status, serving_state, source_watermark, unavailable_sources,
  trace_id, completed_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5::date, $6,
  $7, $8, $9,
  $10, $11, $12, $13::jsonb,
  $14, now()
)
RETURNING sync_run_id::text`,
		cmd.TenantID,
		cmd.ActorID,
		cmd.Mode,
		status,
		nullableCountsString(cmd.SnapshotDate),
		cmd.SourceRowsRead,
		len(cmd.ProjectionRows),
		cmd.RowsSkipped,
		cmd.UnresolvedLocationLabels,
		freshnessStatus,
		servingState,
		nullableCountsString(cmd.SourceWatermark),
		unavailableJSON,
		nullableCountsString(nonEmptyCountsStringPtr(cmd.TraceID)),
	).Scan(&syncRunID); err != nil {
		return nil, err
	}

	if cmd.SourceUnavailable {
		if err := markCountsSourceUnavailable(ctx, tx, cmd, unavailableJSON); err != nil {
			return nil, err
		}
	} else {
		projectionVersion, err := nextCountsProjectionVersion(ctx, tx, cmd.TenantID)
		if err != nil {
			return nil, err
		}
		for _, row := range cmd.SnapshotRows {
			if err := upsertCountsSnapshotRow(ctx, tx, row, cmd.TenantID); err != nil {
				return nil, err
			}
		}
		affected := map[string]map[string]int{}
		for _, row := range cmd.ProjectionRows {
			if affected[row.ViewID] == nil {
				affected[row.ViewID] = map[string]int{}
			}
			affected[row.ViewID][row.SnapshotDate]++
		}
		for viewID, dates := range affected {
			for snapshotDate := range dates {
				if _, err := tx.Exec(ctx, `
DELETE FROM counts_projection_rows
WHERE tenant_id = $1::uuid
  AND view_id = $2
  AND snapshot_date = $3::date`, cmd.TenantID, viewID, snapshotDate); err != nil {
					return nil, err
				}
			}
		}
		for _, row := range cmd.ProjectionRows {
			if err := insertCountsProjectionRow(ctx, tx, row, cmd.TenantID, projectionVersion); err != nil {
				return nil, err
			}
		}
		for viewID, dates := range affected {
			for snapshotDate, rowCount := range dates {
				if err := upsertCountsProjectionState(ctx, tx, cmd, viewID, snapshotDate, rowCount, projectionVersion); err != nil {
					return nil, err
				}
			}
		}
	}

	if err := completeCountsIdempotency(ctx, tx, cmd.StoredIdempotencyKey, "counts_sync_run", syncRunID); err != nil {
		return nil, err
	}
	response, err := loadCountsSyncRun(ctx, tx, cmd.TenantID, syncRunID, cmd.ClientIdempotencyKey, false, nil, cmd.TraceID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.SyncMutationResult{Response: *response}, nil
}

func (r *Repository) GetSyncRun(ctx context.Context, tenantID, syncRunID, traceID string) (*domain.CountsSyncRunResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	response, err := loadCountsSyncRun(ctx, r.pool, tenantID, syncRunID, "", false, nil, traceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrSyncRunNotFound
		}
		return nil, err
	}
	return response, nil
}

type stateRow struct {
	SnapshotDate      *string
	SummarySourceDate *string
	Freshness         domain.FreshnessEnvelope
}

func (r *Repository) loadState(ctx context.Context, params ports.DashboardParams) (stateRow, bool, error) {
	snapshotParam := ""
	if params.SnapshotDate != "latest" {
		snapshotParam = params.SnapshotDate
	}
	row := r.pool.QueryRow(ctx, `
SELECT
  snapshot_date::text,
  summary_source_date::text,
  last_success_at,
  freshness_status,
  serving_state,
  source_watermark,
  unavailable_sources,
  source_composition,
  conflict_count,
  projection_version,
  rebuild_required
FROM counts_projection_state
WHERE tenant_id = $1::uuid
  AND (view_id = $2 OR view_id IS NULL)
  AND ($3 = '' OR snapshot_date = $3::date)
ORDER BY CASE WHEN view_id = $2 THEN 0 ELSE 1 END, updated_at DESC
LIMIT 1`, params.TenantID, params.View, snapshotParam)

	var snapshotDate, summaryDate, sourceWatermark pgtype.Text
	var asOf pgtype.Timestamptz
	var freshnessStatus, servingState, sourceComposition string
	var unavailableBytes []byte
	var conflictCount int
	var projectionVersion int64
	var rebuildRequired bool
	if err := row.Scan(
		&snapshotDate,
		&summaryDate,
		&asOf,
		&freshnessStatus,
		&servingState,
		&sourceWatermark,
		&unavailableBytes,
		&sourceComposition,
		&conflictCount,
		&projectionVersion,
		&rebuildRequired,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return stateRow{}, false, nil
		}
		return stateRow{}, false, err
	}
	unavailable := []string{}
	if len(unavailableBytes) > 0 {
		_ = json.Unmarshal(unavailableBytes, &unavailable)
	}
	asOfText := timePtr(asOf)
	return stateRow{
		SnapshotDate:      textPtr(snapshotDate),
		SummarySourceDate: textPtr(summaryDate),
		Freshness: domain.FreshnessEnvelope{
			AsOf:               asOfText,
			FreshnessStatus:    freshnessStatus,
			ServingState:       servingState,
			Stale:              servingState == "stale" || servingState == "source_unavailable" || servingState == "failed",
			RebuildRequired:    rebuildRequired,
			SourceWatermark:    textPtr(sourceWatermark),
			UnavailableSources: unavailable,
			SourceComposition:  sourceComposition,
			ConflictCount:      conflictCount,
			ProjectionVersion:  projectionVersion,
		},
	}, true, nil
}

func (r *Repository) loadRows(ctx context.Context, params ports.DashboardParams, snapshotDate *string) ([]domain.ProjectionRow, error) {
	if snapshotDate == nil || *snapshotDate == "" {
		return []domain.ProjectionRow{}, nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT
  section,
  grain,
  dimension_key,
  dimension_label,
  secondary_dimension_key,
  secondary_dimension_label,
  metric_key,
  count_value,
  numeric_value::float8,
  unit,
  denominator::float8,
  source_composition
FROM counts_projection_rows
WHERE tenant_id = $1::uuid
  AND view_id = $2
  AND snapshot_date = $3::date
ORDER BY section, sort_order, dimension_label, metric_key, counts_projection_row_id
LIMIT 1000`, params.TenantID, params.View, *snapshotDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.ProjectionRow{}
	for rows.Next() {
		var item domain.ProjectionRow
		var secondaryKey, secondaryLabel pgtype.Text
		var countValue pgtype.Int8
		var numericValue, denominator pgtype.Float8
		if err := rows.Scan(
			&item.Section,
			&item.Grain,
			&item.DimensionKey,
			&item.DimensionLabel,
			&secondaryKey,
			&secondaryLabel,
			&item.MetricKey,
			&countValue,
			&numericValue,
			&item.Unit,
			&denominator,
			&item.SourceComposition,
		); err != nil {
			return nil, err
		}
		item.SecondaryDimensionKey = textPtr(secondaryKey)
		item.SecondaryDimensionLabel = textPtr(secondaryLabel)
		item.CountValue = int64Ptr(countValue)
		item.NumericValue = floatPtr(numericValue)
		item.Denominator = floatPtr(denominator)
		result = append(result, item)
	}
	return result, rows.Err()
}

func emptyDashboard(view, traceID string) *domain.DashboardResponse {
	return &domain.DashboardResponse{
		View:              view,
		SnapshotDate:      nil,
		SummarySourceDate: nil,
		Freshness: domain.FreshnessEnvelope{
			FreshnessStatus:    "unknown",
			ServingState:       "never_synced",
			Stale:              true,
			UnavailableSources: []string{},
			SourceComposition:  "legacy_only",
		},
		Summary:  []domain.ProjectionRow{},
		Sections: defaultSections(),
		TraceID:  traceID,
	}
}

func defaultSections() map[string][]domain.ProjectionRow {
	return map[string][]domain.ProjectionRow{
		"status":                 {},
		"breed":                  {},
		"status_breed":           {},
		"farm":                   {},
		"farm_distribution":      {},
		"age":                    {},
		"adults_gender":          {},
		"kids_gender":            {},
		"kids_stage_gender":      {},
		"fattening_gender":       {},
		"core_farm_gender_breed": {},
	}
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func timePtr(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	text := value.Time.UTC().Format(time.RFC3339)
	return &text
}

func int64Ptr(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func floatPtr(value pgtype.Float8) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

type countsRowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func loadCountsSyncRun(ctx context.Context, q countsRowQuerier, tenantID, syncRunID, clientKey string, replayed bool, firstResultID *string, traceID string) (*domain.CountsSyncRunResponse, error) {
	row := q.QueryRow(ctx, `
SELECT
  sync_run_id::text,
  mode,
  status,
  snapshot_date::text,
  source_rows_read,
  projection_rows_written,
  rows_skipped,
  unresolved_location_labels,
  completed_at,
  freshness_status,
  serving_state,
  source_watermark,
  unavailable_sources,
  COALESCE((
    SELECT max(projection_version)
    FROM counts_projection_state
    WHERE tenant_id = counts_sync_runs.tenant_id
  ), 0)
FROM counts_sync_runs
WHERE tenant_id = $1::uuid
  AND sync_run_id = $2::uuid`, tenantID, syncRunID)

	var response domain.CountsSyncRunResponse
	var snapshotDate, sourceWatermark pgtype.Text
	var completedAt pgtype.Timestamptz
	var freshnessStatus, servingState string
	var unavailableBytes []byte
	var projectionVersion int64
	if err := row.Scan(
		&response.SyncRunID,
		&response.Mode,
		&response.Status,
		&snapshotDate,
		&response.SourceRowsRead,
		&response.ProjectionRowsWritten,
		&response.RowsSkipped,
		&response.UnresolvedLocationLabels,
		&completedAt,
		&freshnessStatus,
		&servingState,
		&sourceWatermark,
		&unavailableBytes,
		&projectionVersion,
	); err != nil {
		return nil, err
	}
	unavailable := []string{}
	if len(unavailableBytes) > 0 {
		_ = json.Unmarshal(unavailableBytes, &unavailable)
	}
	response.SnapshotDate = textPtr(snapshotDate)
	response.Freshness = domain.FreshnessEnvelope{
		AsOf:               timePtr(completedAt),
		FreshnessStatus:    freshnessStatus,
		ServingState:       servingState,
		Stale:              servingState == "stale" || servingState == "source_unavailable" || servingState == "failed",
		RebuildRequired:    false,
		SourceWatermark:    textPtr(sourceWatermark),
		UnavailableSources: unavailable,
		SourceComposition:  "legacy_only",
		ConflictCount:      response.UnresolvedLocationLabels,
		ProjectionVersion:  projectionVersion,
	}
	response.Idempotency = domain.IdempotencyMeta{
		IdempotencyKey: clientKey,
		Replayed:       replayed,
		FirstResultID:  firstResultID,
	}
	response.TraceID = traceID
	return &response, nil
}

func nextCountsProjectionVersion(ctx context.Context, tx pgx.Tx, tenantID string) (int64, error) {
	var version int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(max(projection_version), 0) + 1
FROM counts_projection_state
WHERE tenant_id = $1::uuid`, tenantID).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func upsertCountsSnapshotRow(ctx context.Context, tx pgx.Tx, row ports.SnapshotInputRow, tenantID string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO counts_current_snapshot_rows (
  tenant_id, snapshot_date, source_mode, row_kind, tab_scope,
  farm_key, farm_label, farm_id, park_id, shed_key, shed_label, shed_id,
  resolved_location_id, resolved_location_type, status_key, status_label,
  breed_key, breed_label, age_class, sex, metric_name, count_value,
  source_row_id, logical_fact_key, projection_input_hash
) VALUES (
  $1::uuid, $2::date, $3, $4, $5,
  $6, $7, $8::uuid, $9::uuid, $10, $11, $12::uuid,
  $13::uuid, $14, $15, $16,
  $17, $18, $19, $20, $21, $22,
  $23::uuid, $24, $25
)
ON CONFLICT (tenant_id, snapshot_date, source_mode, logical_fact_key) DO UPDATE
SET row_kind = EXCLUDED.row_kind,
    tab_scope = EXCLUDED.tab_scope,
    farm_key = EXCLUDED.farm_key,
    farm_label = EXCLUDED.farm_label,
    farm_id = EXCLUDED.farm_id,
    park_id = EXCLUDED.park_id,
    shed_key = EXCLUDED.shed_key,
    shed_label = EXCLUDED.shed_label,
    shed_id = EXCLUDED.shed_id,
    resolved_location_id = EXCLUDED.resolved_location_id,
    resolved_location_type = EXCLUDED.resolved_location_type,
    status_key = EXCLUDED.status_key,
    status_label = EXCLUDED.status_label,
    breed_key = EXCLUDED.breed_key,
    breed_label = EXCLUDED.breed_label,
    age_class = EXCLUDED.age_class,
    sex = EXCLUDED.sex,
    metric_name = EXCLUDED.metric_name,
    count_value = EXCLUDED.count_value,
    source_row_id = EXCLUDED.source_row_id,
    projection_input_hash = EXCLUDED.projection_input_hash,
    updated_at = now()`,
		tenantID,
		row.SnapshotDate,
		row.SourceMode,
		row.RowKind,
		nullableCountsString(row.TabScope),
		nullableCountsString(row.FarmKey),
		nullableCountsString(row.FarmLabel),
		nullableCountsString(row.FarmID),
		nullableCountsString(row.ParkID),
		nullableCountsString(row.ShedKey),
		nullableCountsString(row.ShedLabel),
		nullableCountsString(row.ShedID),
		nullableCountsString(row.ResolvedLocationID),
		nullableCountsString(row.ResolvedLocationType),
		nullableCountsString(row.StatusKey),
		nullableCountsString(row.StatusLabel),
		nullableCountsString(row.BreedKey),
		nullableCountsString(row.BreedLabel),
		nullableCountsString(row.AgeClass),
		nullableCountsString(row.Sex),
		row.MetricName,
		nullableCountsInt64(row.CountValue),
		row.SourceRowID,
		row.LogicalFactKey,
		row.ProjectionInputHash,
	)
	return err
}

func insertCountsProjectionRow(ctx context.Context, tx pgx.Tx, row ports.ProjectionInputRow, tenantID string, projectionVersion int64) error {
	_, err := tx.Exec(ctx, `
INSERT INTO counts_projection_rows (
  tenant_id, view_id, snapshot_date, summary_source_date, section, grain,
  dimension_key, dimension_label, secondary_dimension_key, secondary_dimension_label,
  metric_key, count_value, numeric_value, unit, denominator, sort_order,
  projection_version, source_hash, source_composition
) VALUES (
  $1::uuid, $2, $3::date, $4::date, $5, $6,
  $7, $8, $9, $10,
  $11, $12, $13, $14, $15, $16,
  $17, $18, $19
)`,
		tenantID,
		row.ViewID,
		row.SnapshotDate,
		nullableCountsString(row.SummarySourceDate),
		row.Section,
		row.Grain,
		row.DimensionKey,
		row.DimensionLabel,
		nullableCountsString(row.SecondaryDimensionKey),
		nullableCountsString(row.SecondaryDimensionLabel),
		row.MetricKey,
		nullableCountsInt64(row.CountValue),
		nullableCountsFloat64(row.NumericValue),
		row.Unit,
		nullableCountsFloat64(row.Denominator),
		row.SortOrder,
		projectionVersion,
		row.SourceHash,
		row.SourceComposition,
	)
	return err
}

func upsertCountsProjectionState(ctx context.Context, tx pgx.Tx, cmd ports.SyncCommand, viewID, snapshotDate string, rowCount int, projectionVersion int64) error {
	unavailableJSON, _ := json.Marshal([]string{})
	tag, err := tx.Exec(ctx, `
UPDATE counts_projection_state
SET last_success_at = now(),
    snapshot_date = $3::date,
    summary_source_date = $4::date,
    source_watermark = $5,
    projection_version = $6,
    freshness_status = 'green',
    serving_state = 'fresh',
    row_count = $7,
    conflict_count = $8,
    unavailable_sources = $9::jsonb,
    source_composition = 'legacy_only',
    rebuild_required = false,
    last_error = NULL,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND view_id = $2`, cmd.TenantID, viewID, snapshotDate, nullableCountsString(cmd.SummarySourceDate), nullableCountsString(cmd.SourceWatermark), projectionVersion, rowCount, cmd.UnresolvedLocationLabels, unavailableJSON)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = tx.Exec(ctx, `
INSERT INTO counts_projection_state (
  tenant_id, view_id, last_success_at, snapshot_date, summary_source_date,
  source_watermark, projection_version, freshness_status, serving_state,
  row_count, conflict_count, unavailable_sources, source_composition,
  rebuild_required, updated_at
) VALUES (
  $1::uuid, $2, now(), $3::date, $4::date,
  $5, $6, 'green', 'fresh',
  $7, $8, $9::jsonb, 'legacy_only',
  false, now()
)`, cmd.TenantID, viewID, snapshotDate, nullableCountsString(cmd.SummarySourceDate), nullableCountsString(cmd.SourceWatermark), projectionVersion, rowCount, cmd.UnresolvedLocationLabels, unavailableJSON)
	return err
}

func markCountsSourceUnavailable(ctx context.Context, tx pgx.Tx, cmd ports.SyncCommand, unavailableJSON []byte) error {
	for _, viewID := range []string{"overall", "core-farms", "cbe", "cpt", "holdings"} {
		// A no-source sync must not wipe a view that previously served real data.
		// Views with a prior successful projection (last_success_at IS NOT NULL)
		// keep their projection rows, snapshot_date, version and row_count and are
		// only flagged stale/degraded; only never-synced views become
		// source_unavailable + rebuild_required.
		tag, err := tx.Exec(ctx, `
UPDATE counts_projection_state
SET freshness_status = CASE WHEN last_success_at IS NOT NULL THEN 'red' ELSE 'unknown' END,
    serving_state = CASE WHEN last_success_at IS NOT NULL THEN 'stale' ELSE 'source_unavailable' END,
    unavailable_sources = $3::jsonb,
    rebuild_required = CASE WHEN last_success_at IS NOT NULL THEN rebuild_required ELSE true END,
    last_error = 'counts source rows unavailable',
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND view_id = $2`, cmd.TenantID, viewID, unavailableJSON)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO counts_projection_state (
  tenant_id, view_id, freshness_status, serving_state, row_count,
  conflict_count, unavailable_sources, source_composition, rebuild_required,
  last_error, updated_at
) VALUES (
  $1::uuid, $2, 'unknown', 'source_unavailable', 0,
  0, $3::jsonb, 'legacy_only', true,
  'counts source rows unavailable', now()
)`, cmd.TenantID, viewID, unavailableJSON); err != nil {
			return err
		}
	}
	return nil
}

func beginCountsIdempotency(ctx context.Context, tx pgx.Tx, key, tenantID, scope, requestHash, resultType string) (string, bool, error) {
	var inserted string
	err := tx.QueryRow(ctx, `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status, expires_at)
VALUES ($1, $2::uuid, $3, $4, 'started', now() + interval '24 hours')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`, key, tenantID, scope, requestHash).Scan(&inserted)
	if err == nil {
		return "", false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	var existingHash, status string
	var existingResultType pgtype.Text
	var existingResultID pgtype.Text
	if err := tx.QueryRow(ctx, `
SELECT request_hash, status, result_type, result_id::text
FROM idempotency_keys
WHERE idempotency_key = $1`, key).Scan(&existingHash, &status, &existingResultType, &existingResultID); err != nil {
		return "", false, err
	}
	if existingHash != requestHash {
		return "", false, ports.ErrIdempotencyConflict
	}
	if status != "completed" || !existingResultType.Valid || existingResultType.String != resultType || !existingResultID.Valid || strings.TrimSpace(existingResultID.String) == "" {
		return "", false, ports.ErrIdempotencyPending
	}
	return existingResultID.String, true, nil
}

func completeCountsIdempotency(ctx context.Context, tx pgx.Tx, key, resultType, resultID string) error {
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed',
    result_type = $1,
    result_id = $2::uuid,
    completed_at = now()
WHERE idempotency_key = $3`, resultType, resultID, key)
	return err
}

func rollbackCountsUnlessCommitted(ctx context.Context, tx pgx.Tx, committed *bool) {
	if !*committed {
		_ = tx.Rollback(ctx)
	}
}

func nullableCountsString(value *string) any {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return *value
}

func nullableCountsInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableCountsFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nonEmptyCountsStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

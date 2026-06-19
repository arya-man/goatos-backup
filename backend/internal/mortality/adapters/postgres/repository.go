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

	"github.com/vgoats/goatos/backend/internal/mortality/domain"
	"github.com/vgoats/goatos/backend/internal/mortality/ports"
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

	freshness, hasState, err := r.loadFreshness(ctx, params)
	if err != nil {
		return nil, err
	}
	if !hasState {
		return emptyDashboard(params.Period, traceID), nil
	}
	rows, err := r.loadRows(ctx, params)
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
		Period:    params.Period,
		Freshness: freshness,
		Summary:   summary,
		Sections:  sections,
		TraceID:   traceID,
	}, nil
}

func (r *Repository) ListSourceRows(ctx context.Context, params ports.SourceRowsParams) ([]ports.SourceRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, `
SELECT
  mortality_source_row_id::text,
  source_system,
  source_table,
  source_row_key,
  source_watermark,
  payload_json,
  payload_hash
FROM mortality_source_rows
WHERE tenant_id = $1::uuid
  AND row_status = 'current'
ORDER BY source_observed_at NULLS LAST, source_table, source_row_key, mortality_source_row_id
LIMIT 50000`, params.TenantID)
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
			&item.SourceTable,
			&item.SourceRowKey,
			&watermark,
			&item.PayloadJSON,
			&item.PayloadHash,
		); err != nil {
			return nil, err
		}
		item.SourceWatermark = textPtr(watermark)
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
	defer rollbackMortalityUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginMortalityIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, "mortality_sync_run")
	if err != nil {
		return nil, err
	}
	if replayed {
		response, err := loadMortalitySyncRun(ctx, tx, cmd.TenantID, replayID, cmd.ClientIdempotencyKey, true, &replayID, cmd.TraceID)
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
INSERT INTO mortality_sync_runs (
  tenant_id, requested_by, mode, status, source_rows_read, events_upserted,
  projection_rows_written, rows_skipped, dedup_candidate_events,
  unresolved_location_labels, freshness_status, serving_state, source_watermark,
  unavailable_sources, trace_id, completed_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6,
  $7, $8, $9,
  $10, $11, $12, $13,
  $14::jsonb, $15, now()
)
RETURNING sync_run_id::text`,
		cmd.TenantID,
		cmd.ActorID,
		cmd.Mode,
		status,
		cmd.SourceRowsRead,
		len(cmd.Events),
		len(cmd.ProjectionRows),
		cmd.RowsSkipped,
		cmd.DedupCandidateEvents,
		cmd.UnresolvedLocationLabels,
		freshnessStatus,
		servingState,
		nullableMortalityString(cmd.SourceWatermark),
		unavailableJSON,
		nullableMortalityString(nonEmptyMortalityStringPtr(cmd.TraceID)),
	).Scan(&syncRunID); err != nil {
		return nil, err
	}
	if cmd.SourceUnavailable {
		if err := markMortalitySourceUnavailable(ctx, tx, cmd, unavailableJSON); err != nil {
			return nil, err
		}
	} else {
		projectionVersion, err := nextMortalityProjectionVersion(ctx, tx, cmd.TenantID)
		if err != nil {
			return nil, err
		}
		for _, event := range cmd.Events {
			if err := upsertMortalityEvent(ctx, tx, event, cmd.TenantID); err != nil {
				return nil, err
			}
		}
		affected := map[string]int{}
		for _, row := range cmd.ProjectionRows {
			affected[row.Period]++
		}
		for period := range affected {
			if _, err := tx.Exec(ctx, `
DELETE FROM mortality_projection_rows
WHERE tenant_id = $1::uuid
  AND period = $2`, cmd.TenantID, period); err != nil {
				return nil, err
			}
		}
		for _, row := range cmd.ProjectionRows {
			if err := insertMortalityProjectionRow(ctx, tx, row, cmd.TenantID, projectionVersion); err != nil {
				return nil, err
			}
		}
		for period, rowCount := range affected {
			if err := upsertMortalityProjectionState(ctx, tx, cmd, period, rowCount, projectionVersion); err != nil {
				return nil, err
			}
		}
	}
	if err := completeMortalityIdempotency(ctx, tx, cmd.StoredIdempotencyKey, "mortality_sync_run", syncRunID); err != nil {
		return nil, err
	}
	response, err := loadMortalitySyncRun(ctx, tx, cmd.TenantID, syncRunID, cmd.ClientIdempotencyKey, false, nil, cmd.TraceID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.SyncMutationResult{Response: *response}, nil
}

func (r *Repository) GetSyncRun(ctx context.Context, tenantID, syncRunID, traceID string) (*domain.MortalitySyncRunResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return loadMortalitySyncRun(ctx, r.pool, tenantID, syncRunID, "", false, nil, traceID)
}

func (r *Repository) loadFreshness(ctx context.Context, params ports.DashboardParams) (domain.FreshnessEnvelope, bool, error) {
	row := r.pool.QueryRow(ctx, `
SELECT
  last_success_at,
  freshness_status,
  serving_state,
  source_watermark,
  unavailable_sources,
  conflict_count,
  projection_version,
  source_composition,
  rebuild_required
FROM mortality_projection_state
WHERE tenant_id = $1::uuid
  AND (period = $2 OR period IS NULL)
ORDER BY CASE WHEN period = $2 THEN 0 ELSE 1 END, updated_at DESC
LIMIT 1`, params.TenantID, params.Period)

	var asOf pgtype.Timestamptz
	var freshnessStatus, servingState, sourceComposition string
	var sourceWatermark pgtype.Text
	var unavailableBytes []byte
	var conflictCount int
	var projectionVersion int64
	var rebuildRequired bool
	if err := row.Scan(
		&asOf,
		&freshnessStatus,
		&servingState,
		&sourceWatermark,
		&unavailableBytes,
		&conflictCount,
		&projectionVersion,
		&sourceComposition,
		&rebuildRequired,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.FreshnessEnvelope{}, false, nil
		}
		return domain.FreshnessEnvelope{}, false, err
	}
	unavailable := []string{}
	if len(unavailableBytes) > 0 {
		_ = json.Unmarshal(unavailableBytes, &unavailable)
	}
	return domain.FreshnessEnvelope{
		AsOf:               timePtr(asOf),
		FreshnessStatus:    freshnessStatus,
		ServingState:       servingState,
		Stale:              servingState == "stale" || servingState == "source_unavailable" || servingState == "failed",
		RebuildRequired:    rebuildRequired,
		SourceWatermark:    textPtr(sourceWatermark),
		UnavailableSources: unavailable,
		ConflictCount:      conflictCount,
		ProjectionVersion:  projectionVersion,
		SourceComposition:  sourceComposition,
	}, true, nil
}

func (r *Repository) loadRows(ctx context.Context, params ports.DashboardParams) ([]domain.ProjectionRow, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
  section,
  grain,
  dimension_key,
  dimension_label,
  metric_key,
  numerator::float8,
  denominator::float8,
  denominator_source_module,
  denominator_projection_version,
  denominator_source_watermark,
  numerator_source_composition,
  denominator_source_composition,
  mixed_composition_exception_id::text,
  value::float8,
  unit,
  source_composition
FROM mortality_projection_rows
WHERE tenant_id = $1::uuid
  AND period = $2
ORDER BY section, grain, sort_order, dimension_label, metric_key, mortality_projection_row_id
LIMIT 1000`, params.TenantID, params.Period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.ProjectionRow{}
	for rows.Next() {
		var item domain.ProjectionRow
		var numerator, denominator pgtype.Float8
		var denominatorModule, denominatorWatermark, numeratorComposition, denominatorComposition, exceptionID pgtype.Text
		var denominatorVersion pgtype.Int8
		if err := rows.Scan(
			&item.Section,
			&item.Grain,
			&item.DimensionKey,
			&item.DimensionLabel,
			&item.MetricKey,
			&numerator,
			&denominator,
			&denominatorModule,
			&denominatorVersion,
			&denominatorWatermark,
			&numeratorComposition,
			&denominatorComposition,
			&exceptionID,
			&item.Value,
			&item.Unit,
			&item.SourceComposition,
		); err != nil {
			return nil, err
		}
		item.Numerator = floatPtr(numerator)
		item.Denominator = floatPtr(denominator)
		item.DenominatorSourceModule = textPtr(denominatorModule)
		item.DenominatorProjectionVersion = int64Ptr(denominatorVersion)
		item.DenominatorSourceWatermark = textPtr(denominatorWatermark)
		item.NumeratorSourceComposition = textPtr(numeratorComposition)
		item.DenominatorSourceComposition = textPtr(denominatorComposition)
		item.MixedCompositionExceptionID = textPtr(exceptionID)
		result = append(result, item)
	}
	return result, rows.Err()
}

func emptyDashboard(period, traceID string) *domain.DashboardResponse {
	return &domain.DashboardResponse{
		Period: period,
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
		"breed":    {},
		"farm":     {},
		"load":     {},
		"delivery": {},
		"gender":   {},
		"status":   {},
		"housing":  {},
		"trends":   {},
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

func floatPtr(value pgtype.Float8) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func int64Ptr(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

type mortalityRowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func loadMortalitySyncRun(ctx context.Context, q mortalityRowQuerier, tenantID, syncRunID, clientKey string, replayed bool, firstResultID *string, traceID string) (*domain.MortalitySyncRunResponse, error) {
	row := q.QueryRow(ctx, `
SELECT
  sync_run_id::text,
  mode,
  status,
  source_rows_read,
  events_upserted,
  projection_rows_written,
  rows_skipped,
  dedup_candidate_events,
  unresolved_location_labels,
  completed_at,
  freshness_status,
  serving_state,
  source_watermark,
  unavailable_sources,
  COALESCE((
    SELECT max(projection_version)
    FROM mortality_projection_state
    WHERE tenant_id = mortality_sync_runs.tenant_id
  ), 0)
FROM mortality_sync_runs
WHERE tenant_id = $1::uuid
  AND sync_run_id = $2::uuid`, tenantID, syncRunID)

	var response domain.MortalitySyncRunResponse
	var completedAt pgtype.Timestamptz
	var freshnessStatus, servingState string
	var sourceWatermark pgtype.Text
	var unavailableBytes []byte
	var projectionVersion int64
	if err := row.Scan(
		&response.SyncRunID,
		&response.Mode,
		&response.Status,
		&response.SourceRowsRead,
		&response.EventsUpserted,
		&response.ProjectionRowsWritten,
		&response.RowsSkipped,
		&response.DedupCandidateEvents,
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
	response.Freshness = domain.FreshnessEnvelope{
		AsOf:               timePtr(completedAt),
		FreshnessStatus:    freshnessStatus,
		ServingState:       servingState,
		Stale:              servingState == "stale" || servingState == "source_unavailable" || servingState == "failed",
		RebuildRequired:    false,
		SourceWatermark:    textPtr(sourceWatermark),
		UnavailableSources: unavailable,
		ConflictCount:      response.UnresolvedLocationLabels + response.DedupCandidateEvents,
		ProjectionVersion:  projectionVersion,
		SourceComposition:  "legacy_only",
	}
	response.Idempotency = domain.IdempotencyMeta{
		IdempotencyKey: clientKey,
		Replayed:       replayed,
		FirstResultID:  firstResultID,
	}
	response.TraceID = traceID
	return &response, nil
}

func nextMortalityProjectionVersion(ctx context.Context, tx pgx.Tx, tenantID string) (int64, error) {
	var version int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(max(projection_version), 0) + 1
FROM mortality_projection_state
WHERE tenant_id = $1::uuid`, tenantID).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func upsertMortalityEvent(ctx context.Context, tx pgx.Tx, event ports.EventInputRow, tenantID string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO mortality_events (
  tenant_id, logical_event_key, dedup_candidate_key, unresolved_event_ordinal,
  dedup_confidence, event_type, event_date, goat_id, source_goat_identifier,
  source_identifier_kind, age_class, breed_key, breed_label, farm_key, farm_label,
  canonical_farm_location_id, canonical_park_location_id, canonical_shed_location_id,
  canonical_housing_location_id, load_key, load_label, delivery_key, delivery_label,
  sex, source_row_id, event_hash, review_status, idempotency_key
) VALUES (
  $1::uuid, $2, $3, $4,
  $5, $6, $7::date, $8::uuid, $9,
  $10, $11, $12, $13, $14, $15,
  $16::uuid, $17::uuid, $18::uuid,
  $19::uuid, $20, $21, $22, $23,
  $24, $25::uuid, $26, $27, $28
)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET logical_event_key = EXCLUDED.logical_event_key,
    dedup_candidate_key = EXCLUDED.dedup_candidate_key,
    unresolved_event_ordinal = EXCLUDED.unresolved_event_ordinal,
    dedup_confidence = EXCLUDED.dedup_confidence,
    event_type = EXCLUDED.event_type,
    event_date = EXCLUDED.event_date,
    goat_id = EXCLUDED.goat_id,
    source_goat_identifier = EXCLUDED.source_goat_identifier,
    source_identifier_kind = EXCLUDED.source_identifier_kind,
    age_class = EXCLUDED.age_class,
    breed_key = EXCLUDED.breed_key,
    breed_label = EXCLUDED.breed_label,
    farm_key = EXCLUDED.farm_key,
    farm_label = EXCLUDED.farm_label,
    canonical_farm_location_id = EXCLUDED.canonical_farm_location_id,
    canonical_park_location_id = EXCLUDED.canonical_park_location_id,
    canonical_shed_location_id = EXCLUDED.canonical_shed_location_id,
    canonical_housing_location_id = EXCLUDED.canonical_housing_location_id,
    load_key = EXCLUDED.load_key,
    load_label = EXCLUDED.load_label,
    delivery_key = EXCLUDED.delivery_key,
    delivery_label = EXCLUDED.delivery_label,
    sex = EXCLUDED.sex,
    source_row_id = EXCLUDED.source_row_id,
    event_hash = EXCLUDED.event_hash,
    review_status = EXCLUDED.review_status,
    updated_at = now()`,
		tenantID,
		nullableMortalityString(event.LogicalEventKey),
		nullableMortalityString(event.DedupCandidateKey),
		nullableMortalityInt(event.UnresolvedEventOrdinal),
		event.DedupConfidence,
		event.EventType,
		event.EventDate,
		nullableMortalityString(event.GoatID),
		nullableMortalityString(event.SourceGoatIdentifier),
		nullableMortalityString(event.SourceIdentifierKind),
		event.AgeClass,
		nullableMortalityString(event.BreedKey),
		nullableMortalityString(event.BreedLabel),
		nullableMortalityString(event.FarmKey),
		nullableMortalityString(event.FarmLabel),
		nullableMortalityString(event.CanonicalFarmLocationID),
		nullableMortalityString(event.CanonicalParkLocationID),
		nullableMortalityString(event.CanonicalShedLocationID),
		nullableMortalityString(event.CanonicalHousingLocationID),
		nullableMortalityString(event.LoadKey),
		nullableMortalityString(event.LoadLabel),
		nullableMortalityString(event.DeliveryKey),
		nullableMortalityString(event.DeliveryLabel),
		nullableMortalityString(event.Sex),
		event.SourceRowID,
		event.EventHash,
		event.ReviewStatus,
		event.IdempotencyKey,
	)
	return err
}

func insertMortalityProjectionRow(ctx context.Context, tx pgx.Tx, row ports.ProjectionInputRow, tenantID string, projectionVersion int64) error {
	_, err := tx.Exec(ctx, `
INSERT INTO mortality_projection_rows (
  tenant_id, period, period_start, period_end, section, grain,
  dimension_key, dimension_label, metric_key, numerator, denominator,
  denominator_source_module, denominator_projection_version, denominator_source_watermark,
  numerator_source_composition, denominator_source_composition, value, unit,
  sort_order, projection_version, source_hash, source_composition
) VALUES (
  $1::uuid, $2, $3::date, $4::date, $5, $6,
  $7, $8, $9, $10, $11,
  $12, $13, $14,
  $15, $16, $17, $18,
  $19, $20, $21, $22
)`,
		tenantID,
		row.Period,
		nullableMortalityString(row.PeriodStart),
		nullableMortalityString(row.PeriodEnd),
		row.Section,
		row.Grain,
		row.DimensionKey,
		row.DimensionLabel,
		row.MetricKey,
		nullableMortalityFloat64(row.Numerator),
		nullableMortalityFloat64(row.Denominator),
		nullableMortalityString(row.DenominatorSourceModule),
		nullableMortalityInt64(row.DenominatorProjectionVersion),
		nullableMortalityString(row.DenominatorSourceWatermark),
		nullableMortalityString(row.NumeratorSourceComposition),
		nullableMortalityString(row.DenominatorSourceComposition),
		row.Value,
		row.Unit,
		row.SortOrder,
		projectionVersion,
		row.SourceHash,
		row.SourceComposition,
	)
	return err
}

func upsertMortalityProjectionState(ctx context.Context, tx pgx.Tx, cmd ports.SyncCommand, period string, rowCount int, projectionVersion int64) error {
	unavailableJSON, _ := json.Marshal([]string{})
	conflictCount := cmd.UnresolvedLocationLabels + cmd.DedupCandidateEvents
	tag, err := tx.Exec(ctx, `
UPDATE mortality_projection_state
SET last_success_at = now(),
    source_watermark = $3,
    projection_version = $4,
    freshness_status = 'green',
    serving_state = 'fresh',
    source_composition = 'legacy_only',
    row_count = $5,
    conflict_count = $6,
    unavailable_sources = $7::jsonb,
    rebuild_required = false,
    last_error = NULL,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND period = $2`, cmd.TenantID, period, nullableMortalityString(cmd.SourceWatermark), projectionVersion, rowCount, conflictCount, unavailableJSON)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = tx.Exec(ctx, `
INSERT INTO mortality_projection_state (
  tenant_id, period, last_success_at, source_watermark, projection_version,
  freshness_status, serving_state, source_composition, row_count, conflict_count,
  unavailable_sources, rebuild_required, updated_at
) VALUES (
  $1::uuid, $2, now(), $3, $4,
  'green', 'fresh', 'legacy_only', $5, $6,
  $7::jsonb, false, now()
)`, cmd.TenantID, period, nullableMortalityString(cmd.SourceWatermark), projectionVersion, rowCount, conflictCount, unavailableJSON)
	return err
}

func markMortalitySourceUnavailable(ctx context.Context, tx pgx.Tx, cmd ports.SyncCommand, unavailableJSON []byte) error {
	for _, period := range []string{"overall", "this-month", "month-wise"} {
		tag, err := tx.Exec(ctx, `
UPDATE mortality_projection_state
SET freshness_status = 'unknown',
    serving_state = 'source_unavailable',
    unavailable_sources = $3::jsonb,
    rebuild_required = true,
    last_error = 'mortality source rows unavailable',
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND period = $2`, cmd.TenantID, period, unavailableJSON)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO mortality_projection_state (
  tenant_id, period, freshness_status, serving_state, source_composition,
  row_count, conflict_count, unavailable_sources, rebuild_required,
  last_error, updated_at
) VALUES (
  $1::uuid, $2, 'unknown', 'source_unavailable', 'legacy_only',
  0, 0, $3::jsonb, true,
  'mortality source rows unavailable', now()
)`, cmd.TenantID, period, unavailableJSON); err != nil {
			return err
		}
	}
	return nil
}

func beginMortalityIdempotency(ctx context.Context, tx pgx.Tx, key, tenantID, scope, requestHash, resultType string) (string, bool, error) {
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

func completeMortalityIdempotency(ctx context.Context, tx pgx.Tx, key, resultType, resultID string) error {
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed',
    result_type = $1,
    result_id = $2::uuid,
    completed_at = now()
WHERE idempotency_key = $3`, resultType, resultID, key)
	return err
}

func rollbackMortalityUnlessCommitted(ctx context.Context, tx pgx.Tx, committed *bool) {
	if !*committed {
		_ = tx.Rollback(ctx)
	}
}

func nullableMortalityString(value *string) any {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return *value
}

func nullableMortalityInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableMortalityInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableMortalityFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nonEmptyMortalityStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

package postgres

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	reportingdb "github.com/vgoats/goatos/backend/internal/reporting/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/reporting/domain"
	"github.com/vgoats/goatos/backend/internal/reporting/ports"
)

const defaultQueryTimeout = 3 * time.Second
const identityCountsCursorVersion = 1

type Repository struct {
	pool      *pgxpool.Pool
	queries   *reportingdb.Queries
	timeout   time.Duration
	afterLock func(context.Context) error
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{
		pool:    pool,
		queries: reportingdb.New(pool),
		timeout: queryTimeout,
	}
}

func (r *Repository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.pool.Ping(ctx)
}

func (r *Repository) ListIdentityCounts(ctx context.Context, params ports.CountParams) (*ports.CountPage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam("tenant_id", params.TenantID)
	if err != nil {
		return nil, err
	}
	custodianPartyID, err := nullableUUIDParam("custodian_party_id", params.CustodianPartyID)
	if err != nil {
		return nil, err
	}
	farmID, err := nullableUUIDParam("farm_id", params.FarmID)
	if err != nil {
		return nil, err
	}
	parkID, err := nullableUUIDParam("park_id", params.ParkID)
	if err != nil {
		return nil, err
	}
	shedID, err := nullableUUIDParam("shed_id", params.ShedID)
	if err != nil {
		return nil, err
	}
	cohortID, err := nullableUUIDParam("cohort_id", params.CohortID)
	if err != nil {
		return nil, err
	}
	breedID, err := nullableUUIDParam("breed_id", params.BreedID)
	if err != nil {
		return nil, err
	}
	cursorCountValue, cursorCounterID, err := decodeCountCursor(params.Cursor)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListIdentityCounts(ctx, reportingdb.ListIdentityCountsParams{
		CounterGrain:       params.Grain,
		TenantID:           tenantUUID,
		CustodianPartyID:   custodianPartyID,
		FarmID:             farmID,
		ParkID:             parkID,
		ShedID:             shedID,
		CohortID:           cohortID,
		LifecycleStatus:    nullableText(params.LifecycleStatus),
		ReproductiveStatus: nullableText(params.ReproductiveStatus),
		GrowthCohortTag:    nullableText(params.GrowthCohortTag),
		ManagementStage:    nullableText(params.ManagementStage),
		HealthStatus:       nullableText(params.HealthStatus),
		IdentityState:      nullableText(params.IdentityState),
		BreedID:            breedID,
		Sex:                nullableText(params.Sex),
		CursorCountValue:   cursorCountValue,
		CursorCounterID:    cursorCounterID,
		LimitCount:         int32(params.Limit + 1),
	})
	if err != nil {
		return nil, err
	}

	hasMore := len(rows) > params.Limit
	if hasMore {
		rows = rows[:params.Limit]
	}
	var nextCursor *string
	if hasMore && len(rows) > 0 {
		cursor, err := encodeCountCursor(rows[len(rows)-1])
		if err != nil {
			return nil, err
		}
		nextCursor = &cursor
	}

	items := make([]domain.IdentityCount, 0, len(rows))
	var freshness domain.Freshness
	for _, row := range rows {
		item := identityCountFromSQLC(row)
		items = append(items, item)
		if freshness.AsOfRecordedAt == nil && item.AsOfRecordedAt != nil {
			freshness.AsOfRecordedAt = item.AsOfRecordedAt
		}
		if freshness.SourceImportRunID == nil && item.SourceImportRunID != nil {
			freshness.SourceImportRunID = item.SourceImportRunID
		}
		freshness.IsRebuilding = freshness.IsRebuilding || item.IsRebuilding
	}
	return &ports.CountPage{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Freshness:  freshness,
	}, nil
}

func (r *Repository) RebuildIdentityCounters(ctx context.Context, params ports.RebuildIdentityCountersParams) (*domain.IdentityCounterRebuildResult, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := r.rebuildIdentityCountersOnce(ctx, params)
		if err == nil || !isSerializationRetryable(err) {
			return result, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (r *Repository) rebuildIdentityCountersOnce(ctx context.Context, params ports.RebuildIdentityCountersParams) (*domain.IdentityCounterRebuildResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam("tenant_id", params.TenantID)
	if err != nil {
		return nil, err
	}
	sourceRunUUID, err := nullableUUIDParam("source_import_run_id", params.SourceImportRunID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	qtx := r.queries.WithTx(tx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, params.TenantID); err != nil {
		return nil, err
	}
	if r.afterLock != nil {
		if err := r.afterLock(ctx); err != nil {
			return nil, err
		}
	}

	exists, err := qtx.TenantExists(ctx, tenantUUID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ports.ErrNotFound
	}
	if params.SourceImportRunID != nil {
		sourceExists, err := qtx.SourceImportRunBelongsToTenant(ctx, reportingdb.SourceImportRunBelongsToTenantParams{
			TenantID:    tenantUUID,
			ImportRunID: sourceRunUUID,
		})
		if err != nil {
			return nil, err
		}
		if !sourceExists {
			return nil, ports.ErrNotFound
		}
	}

	watermark, err := qtx.MaxGoatIdentityEventRecordedAtForTenant(ctx, tenantUUID)
	if err != nil {
		return nil, err
	}

	result := &domain.IdentityCounterRebuildResult{
		TenantID:          params.TenantID,
		SourceImportRunID: params.SourceImportRunID,
		AsOfRecordedAt:    pgTimePtr(watermark),
		Grains:            make([]domain.GrainRebuildResult, 0, len(params.Grains)),
	}
	for _, grain := range params.Grains {
		deleted, err := qtx.DeleteIdentityCountersByGrain(ctx, reportingdb.DeleteIdentityCountersByGrainParams{
			TenantID:     tenantUUID,
			CounterGrain: grain,
		})
		if err != nil {
			return nil, err
		}
		inserted, err := r.insertGrain(ctx, qtx, grain, tenantUUID, watermark, sourceRunUUID)
		if err != nil {
			return nil, err
		}
		result.Grains = append(result.Grains, domain.GrainRebuildResult{
			Grain:        grain,
			DeletedRows:  deleted,
			InsertedRows: inserted,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

func (r *Repository) insertGrain(ctx context.Context, qtx *reportingdb.Queries, grain string, tenantUUID pgtype.UUID, watermark pgtype.Timestamptz, sourceRunUUID pgtype.UUID) (int64, error) {
	params := grainParams{
		TenantID:          tenantUUID,
		AsOfRecordedAt:    watermark,
		SourceImportRunID: sourceRunUUID,
	}
	switch grain {
	case domain.GrainTenantLifecycle:
		return qtx.InsertTenantLifecycleCounters(ctx, reportingdb.InsertTenantLifecycleCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	case domain.GrainCustodianLife:
		return qtx.InsertCustodianLifecycleCounters(ctx, reportingdb.InsertCustodianLifecycleCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	case domain.GrainCustodianIdent:
		return qtx.InsertCustodianIdentityCounters(ctx, reportingdb.InsertCustodianIdentityCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	case domain.GrainParkLifecycle:
		return qtx.InsertParkLifecycleCounters(ctx, reportingdb.InsertParkLifecycleCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	case domain.GrainShedLifecycle:
		return qtx.InsertShedLifecycleCounters(ctx, reportingdb.InsertShedLifecycleCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	case domain.GrainBreedSexLife:
		return qtx.InsertBreedSexLifecycleCounters(ctx, reportingdb.InsertBreedSexLifecycleCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	case domain.GrainHealthStatus:
		return qtx.InsertHealthStatusCounters(ctx, reportingdb.InsertHealthStatusCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	case domain.GrainGrowthCohort:
		return qtx.InsertGrowthCohortCounters(ctx, reportingdb.InsertGrowthCohortCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	case domain.GrainManagementStage:
		return qtx.InsertManagementStageCounters(ctx, reportingdb.InsertManagementStageCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	case domain.GrainReproductiveStat:
		return qtx.InsertReproductiveStatusCounters(ctx, reportingdb.InsertReproductiveStatusCountersParams{
			AsOfRecordedAt:    params.AsOfRecordedAt,
			SourceImportRunID: params.SourceImportRunID,
			TenantID:          params.TenantID,
		})
	default:
		return 0, fmt.Errorf("unsupported reporting grain %q", grain)
	}
}

type grainParams struct {
	TenantID          pgtype.UUID
	AsOfRecordedAt    pgtype.Timestamptz
	SourceImportRunID pgtype.UUID
}

func identityCountFromSQLC(row reportingdb.ListIdentityCountsRow) domain.IdentityCount {
	item := domain.IdentityCount{
		CounterGrain: row.CounterGrain,
		Dimensions: domain.CountDimensions{
			TenantID:           row.TenantID,
			CustodianPartyID:   emptyStringPtr(row.CustodianPartyID),
			FarmID:             emptyStringPtr(row.FarmID),
			ParkID:             emptyStringPtr(row.ParkID),
			ShedID:             emptyStringPtr(row.ShedID),
			CohortID:           emptyStringPtr(row.CohortID),
			LifecycleStatus:    pgTextPtr(row.LifecycleStatus),
			ReproductiveStatus: pgTextPtr(row.ReproductiveStatus),
			GrowthCohortTag:    pgTextPtr(row.GrowthCohortTag),
			ManagementStage:    pgTextPtr(row.ManagementStage),
			HealthStatus:       pgTextPtr(row.HealthStatus),
			IdentityState:      pgTextPtr(row.IdentityState),
			BreedID:            emptyStringPtr(row.BreedID),
			Sex:                pgTextPtr(row.Sex),
		},
		CountValue:        row.CountValue,
		AsOfRecordedAt:    pgTimePtr(row.AsOfRecordedAt),
		SourceImportRunID: emptyStringPtr(row.SourceImportRunID),
		IsRebuilding:      row.IsRebuilding,
		UpdatedAt:         row.UpdatedAt.Time,
	}
	return item
}

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}

func uuidParam(field, value string) (pgtype.UUID, error) {
	var out pgtype.UUID
	if err := out.Scan(value); err != nil {
		return pgtype.UUID{}, fmt.Errorf("%w: %s must be a uuid", ports.ErrInvalidFilter, field)
	}
	return out, nil
}

func nullableUUIDParam(field string, value *string) (pgtype.UUID, error) {
	if value == nil {
		return pgtype.UUID{}, nil
	}
	return uuidParam(field, *value)
}

func nullableText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func pgTextPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func pgTimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func emptyStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

type countCursorPayload struct {
	Version    *int    `json:"v"`
	CountValue *int64  `json:"count_value"`
	CounterID  *string `json:"counter_id"`
}

func encodeCountCursor(row reportingdb.ListIdentityCountsRow) (string, error) {
	version := identityCountsCursorVersion
	countValue := row.CountValue
	counterID := row.CounterID
	payload, err := json.Marshal(countCursorPayload{
		Version:    &version,
		CountValue: &countValue,
		CounterID:  &counterID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCountCursor(value *string) (pgtype.Int8, pgtype.UUID, error) {
	if value == nil {
		return pgtype.Int8{}, pgtype.UUID{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*value)
	if err != nil {
		return pgtype.Int8{}, pgtype.UUID{}, fmt.Errorf("%w: cursor must be base64url JSON", ports.ErrInvalidCursor)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload countCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return pgtype.Int8{}, pgtype.UUID{}, fmt.Errorf("%w: cursor JSON is invalid", ports.ErrInvalidCursor)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return pgtype.Int8{}, pgtype.UUID{}, fmt.Errorf("%w: cursor JSON has trailing data", ports.ErrInvalidCursor)
	}
	if payload.Version == nil || *payload.Version != identityCountsCursorVersion {
		return pgtype.Int8{}, pgtype.UUID{}, fmt.Errorf("%w: unsupported cursor version", ports.ErrInvalidCursor)
	}
	if payload.CountValue == nil || *payload.CountValue < 0 {
		return pgtype.Int8{}, pgtype.UUID{}, fmt.Errorf("%w: cursor count_value is invalid", ports.ErrInvalidCursor)
	}
	if payload.CounterID == nil {
		return pgtype.Int8{}, pgtype.UUID{}, fmt.Errorf("%w: cursor counter_id is required", ports.ErrInvalidCursor)
	}
	var counterID pgtype.UUID
	if err := counterID.Scan(*payload.CounterID); err != nil {
		return pgtype.Int8{}, pgtype.UUID{}, fmt.Errorf("%w: cursor counter_id must be a uuid", ports.ErrInvalidCursor)
	}
	return pgtype.Int8{Int64: *payload.CountValue, Valid: true}, counterID, nil
}

func isSerializationRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40001" || pgErr.Code == "40P01"
}

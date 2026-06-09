package postgres

import (
	"context"
	"errors"
	"fmt"
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

func (r *Repository) ListIdentityCounts(ctx context.Context, params ports.CountParams) ([]domain.IdentityCount, domain.Freshness, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(params.TenantID)
	if err != nil {
		return nil, domain.Freshness{}, err
	}
	rows, err := r.queries.ListIdentityCounts(ctx, reportingdb.ListIdentityCountsParams{
		CounterGrain:       params.Grain,
		TenantID:           tenantUUID,
		CustodianPartyID:   nullableUUID(params.CustodianPartyID),
		FarmID:             nullableUUID(params.FarmID),
		ParkID:             nullableUUID(params.ParkID),
		ShedID:             nullableUUID(params.ShedID),
		CohortID:           nullableUUID(params.CohortID),
		LifecycleStatus:    nullableText(params.LifecycleStatus),
		ReproductiveStatus: nullableText(params.ReproductiveStatus),
		GrowthCohortTag:    nullableText(params.GrowthCohortTag),
		ManagementStage:    nullableText(params.ManagementStage),
		HealthStatus:       nullableText(params.HealthStatus),
		IdentityState:      nullableText(params.IdentityState),
		BreedID:            nullableUUID(params.BreedID),
		Sex:                nullableText(params.Sex),
	})
	if err != nil {
		return nil, domain.Freshness{}, err
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
	return items, freshness, nil
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

	tenantUUID, err := uuidParam(params.TenantID)
	if err != nil {
		return nil, err
	}
	sourceRunUUID, err := nullableUUIDParam(params.SourceImportRunID)
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

func uuidParam(value string) (pgtype.UUID, error) {
	var out pgtype.UUID
	if err := out.Scan(value); err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid uuid %q: %w", value, err)
	}
	return out, nil
}

func nullableUUIDParam(value *string) (pgtype.UUID, error) {
	if value == nil {
		return pgtype.UUID{}, nil
	}
	return uuidParam(*value)
}

func nullableUUID(value *string) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	uuid, err := uuidParam(*value)
	if err != nil {
		return pgtype.UUID{}
	}
	return uuid
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

func isSerializationRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40001" || pgErr.Code == "40P01"
}

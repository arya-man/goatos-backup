package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/legacy_import"
	importdb "github.com/vgoats/goatos/backend/internal/legacy_import/adapters/postgres/sqlc"
)

type Repository struct {
	pool           *pgxpool.Pool
	queries        *importdb.Queries
	queryTimeout   time.Duration
	afterAuditHook func(context.Context) error
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = 3 * time.Second
	}
	return &Repository{pool: pool, queries: importdb.New(pool), queryTimeout: queryTimeout}
}

func (r *Repository) LoadApprovedPolicy(ctx context.Context, policyVersion string) (legacy_import.Policy, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	row, err := r.queries.GetApprovedLegacyImportPolicy(ctx, policyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return legacy_import.Policy{}, fmt.Errorf("approved legacy import policy not found")
	}
	if err != nil {
		return legacy_import.Policy{}, err
	}

	var sourceKey legacy_import.SourceKeyRecipe
	if err := json.Unmarshal(row.SourceKeyRecipe, &sourceKey); err != nil {
		return legacy_import.Policy{}, fmt.Errorf("decode source key recipe: %w", err)
	}
	var hashRecipe legacy_import.HashRecipe
	if err := json.Unmarshal(row.HashRecipe, &hashRecipe); err != nil {
		return legacy_import.Policy{}, fmt.Errorf("decode hash recipe: %w", err)
	}
	return legacy_import.Policy{
		PolicyVersion:           row.PolicyVersion,
		SourceSystem:            row.SourceSystem,
		SourceDataset:           row.SourceDataset,
		IdentifierPolicyVersion: row.IdentifierPolicyVersion,
		SourceKeyRecipe:         sourceKey,
		SourceKeyRecipeVersion:  row.SourceKeyRecipeVersion,
		HashRecipe:              hashRecipe,
		HashRecipeVersion:       row.HashRecipeVersion,
		NormalizerVersion:       row.NormalizerVersion,
		RawSourceKeyRecipe:      append([]byte(nil), row.SourceKeyRecipe...),
		RawHashRecipe:           append([]byte(nil), row.HashRecipe...),
	}, nil
}

func (r *Repository) CreateImportRun(ctx context.Context, params legacy_import.CreateRunParams) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantID, err := uuidParam(params.TenantID)
	if err != nil {
		return "", err
	}
	startedBy, err := nullableUUID(params.StartedBy)
	if err != nil {
		return "", err
	}
	row, err := r.queries.CreateLegacyImportRun(ctx, importdb.CreateLegacyImportRunParams{
		TenantID:       tenantID,
		SourceName:     params.SourceName,
		SourceSystem:   params.SourceSystem,
		SourceDataset:  params.SourceDataset,
		SourceFileRef:  nullableText(params.SourceFileRef),
		SourceFileHash: nullableTextValue(params.SourceFileHash),
		PolicyVersion:  params.PolicyVersion,
		DryRun:         params.DryRun,
		Status:         params.Status,
		RowCount:       int32(params.RowCount),
		ErrorCount:     int32(params.ErrorCount),
		StartedBy:      startedBy,
	})
	if err != nil {
		return "", err
	}
	return row, nil
}

func (r *Repository) CompleteImportRun(ctx context.Context, params legacy_import.CompleteRunParams) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantID, runID, err := tenantRunUUIDs(params.TenantID, params.ImportRunID)
	if err != nil {
		return err
	}
	return r.queries.CompleteLegacyImportRun(ctx, importdb.CompleteLegacyImportRunParams{
		TenantID:    tenantID,
		ImportRunID: runID,
		RowCount:    int32(params.RowCount),
		ErrorCount:  int32(params.ErrorCount),
	})
}

func (r *Repository) FailImportRun(ctx context.Context, params legacy_import.CompleteRunParams) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantID, runID, err := tenantRunUUIDs(params.TenantID, params.ImportRunID)
	if err != nil {
		return err
	}
	return r.queries.FailLegacyImportRun(ctx, importdb.FailLegacyImportRunParams{
		TenantID:    tenantID,
		ImportRunID: runID,
		RowCount:    int32(params.RowCount),
		ErrorCount:  int32(params.ErrorCount),
	})
}

func (r *Repository) HasDifferentSourceRowVersion(ctx context.Context, params legacy_import.SourceRowChangeParams) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantID, err := uuidParam(params.TenantID)
	if err != nil {
		return false, err
	}
	return r.queries.HasLegacyImportRowWithDifferentHash(ctx, importdb.HasLegacyImportRowWithDifferentHashParams{
		TenantID:             tenantID,
		SourceSystem:         params.SourceSystem,
		SourceDataset:        params.SourceDataset,
		SourceRowKey:         params.SourceRowKey,
		SourceRowVersionHash: params.SourceRowVersionHash,
	})
}

func (r *Repository) InsertRows(ctx context.Context, rows []legacy_import.StagedRow, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = legacy_import.DefaultBatchSize
	}
	inserted := 0
	for start := 0; start < len(rows); start += batchSize {
		end := min(start+batchSize, len(rows))
		n, err := r.insertRowBatch(ctx, rows[start:end])
		if err != nil {
			return inserted, err
		}
		inserted += n
	}
	return inserted, nil
}

func (r *Repository) insertRowBatch(ctx context.Context, rows []legacy_import.StagedRow) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)

	inserted := 0
	for _, row := range rows {
		params, err := insertRowParams(row)
		if err != nil {
			return inserted, err
		}
		_, err = qtx.InsertLegacyImportRow(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return inserted, err
		}
		inserted++
	}
	if err := tx.Commit(ctx); err != nil {
		return inserted, err
	}
	committed = true
	return inserted, nil
}

func insertRowParams(row legacy_import.StagedRow) (importdb.InsertLegacyImportRowParams, error) {
	tenantID, err := uuidParam(row.TenantID)
	if err != nil {
		return importdb.InsertLegacyImportRowParams{}, err
	}
	runID, err := uuidParam(row.ImportRunID)
	if err != nil {
		return importdb.InsertLegacyImportRowParams{}, err
	}
	return importdb.InsertLegacyImportRowParams{
		ImportRunID:            runID,
		TenantID:               tenantID,
		SourceSystem:           row.SourceSystem,
		SourceDataset:          row.SourceDataset,
		SourceRecordID:         nullableText(row.SourceRecordID),
		SourceRowKey:           row.SourceRowKey,
		SourceKeyRecipeVersion: row.SourceKeyRecipeVer,
		SourceRowVersionHash:   row.SourceRowVersionHash,
		HashRecipeVersion:      row.HashRecipeVersion,
		RowNumber:              int32(row.RowNumber),
		RawPayload:             row.RawPayload,
		NormalizedPayload:      row.NormalizedPayload,
		ProcessingState:        row.ProcessingState,
		ErrorReason:            nullableText(row.ErrorReason),
	}, nil
}

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.queryTimeout <= 0 {
		r.queryTimeout = 3 * time.Second
	}
	return context.WithTimeout(ctx, r.queryTimeout)
}

func tenantRunUUIDs(tenant, run string) (pgtype.UUID, pgtype.UUID, error) {
	tenantID, err := uuidParam(tenant)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	runID, err := uuidParam(run)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	return tenantID, runID, nil
}

func uuidParam(value string) (pgtype.UUID, error) {
	var uuid pgtype.UUID
	if err := uuid.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return uuid, nil
}

func nullableUUID(value *string) (pgtype.UUID, error) {
	if value == nil || *value == "" {
		return pgtype.UUID{}, nil
	}
	return uuidParam(*value)
}

func nullableText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func nullableTextValue(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

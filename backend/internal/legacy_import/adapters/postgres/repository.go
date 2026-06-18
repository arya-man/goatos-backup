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

func (r *Repository) ListAnomalyReportRows(ctx context.Context, tenantIDValue, importRunIDValue string) ([]legacy_import.AnomalyReportInputRow, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantID, runID, err := tenantRunUUIDs(tenantIDValue, importRunIDValue)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
SELECT
  row_number,
  source_system,
  source_dataset,
  source_row_key,
  processing_state,
  error_reason,
  raw_payload,
  normalized_payload
FROM legacy_import_rows
WHERE tenant_id = $1
  AND import_run_id = $2
  AND processing_state IN ('needs_review', 'error')
ORDER BY row_number, legacy_row_id`, tenantID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []legacy_import.AnomalyReportInputRow{}
	for rows.Next() {
		var row legacy_import.AnomalyReportInputRow
		var errorReason pgtype.Text
		if err := rows.Scan(
			&row.RowNumber,
			&row.SourceSystem,
			&row.SourceDataset,
			&row.SourceRowKey,
			&row.ProcessingState,
			&errorReason,
			&row.RawPayload,
			&row.NormalizedPayload,
		); err != nil {
			return nil, err
		}
		if errorReason.Valid {
			row.ErrorReason = &errorReason.String
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
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
			requeued, requeueErr := r.requeueStaleDuplicateReviewRow(ctx, tx, row, params)
			if requeueErr != nil {
				return inserted, requeueErr
			}
			if requeued {
				inserted++
			}
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

func (r *Repository) requeueStaleDuplicateReviewRow(ctx context.Context, tx pgx.Tx, row legacy_import.StagedRow, params importdb.InsertLegacyImportRowParams) (bool, error) {
	if row.ProcessingState != legacy_import.StatePending || row.ErrorReason != nil || normalizedPayloadHasReasons(row.NormalizedPayload) {
		return false, nil
	}
	var legacyRowID string
	err := tx.QueryRow(ctx, `
UPDATE legacy_import_rows
SET
  import_run_id = $1,
  source_record_id = $2,
  row_number = $3,
  raw_payload = $4,
  normalized_payload = $5,
  processing_state = 'pending',
  error_reason = NULL,
  matched_goat_id = NULL
WHERE tenant_id = $6
  AND source_system = $7
  AND source_dataset = $8
  AND source_row_key = $9
  AND source_row_version_hash = $10
  AND processing_state = 'needs_review'
  AND matched_goat_id IS NULL
  AND error_reason IS NULL
  AND normalized_payload @> '{"processing_reasons":["duplicate_old_tag_same_scope"]}'::jsonb
  AND jsonb_array_length(normalized_payload->'processing_reasons') = 1
RETURNING legacy_row_id::text`,
		params.ImportRunID,
		params.SourceRecordID,
		params.RowNumber,
		params.RawPayload,
		params.NormalizedPayload,
		params.TenantID,
		params.SourceSystem,
		params.SourceDataset,
		params.SourceRowKey,
		params.SourceRowVersionHash,
	).Scan(&legacyRowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("requeue stale duplicate review row: %w", err)
	}
	return true, nil
}

func normalizedPayloadHasReasons(payload []byte) bool {
	var normalized map[string]any
	if err := json.Unmarshal(payload, &normalized); err != nil {
		return true
	}
	return len(stringSlice(normalized["processing_reasons"])) > 0
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

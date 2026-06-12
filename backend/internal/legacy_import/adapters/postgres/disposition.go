package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/legacy_import"
	importdb "github.com/vgoats/goatos/backend/internal/legacy_import/adapters/postgres/sqlc"
)

const confirmedNonGoatDispositionReason = "confirmed_non_goat_species"

func (r *Repository) RejectConfirmedNonGoatRows(ctx context.Context, cmd legacy_import.RejectConfirmedNonGoatCommand) (*legacy_import.RejectConfirmedNonGoatResult, error) {
	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	runUUID, err := uuidParam(cmd.ImportRunID)
	if err != nil {
		return nil, err
	}
	if cmd.BatchSize <= 0 {
		cmd.BatchSize = legacy_import.DefaultBatchSize
	}
	run, err := r.queries.GetLegacyImportRunForApply(ctx, importdb.GetLegacyImportRunForApplyParams{
		TenantID:    tenantUUID,
		ImportRunID: runUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("legacy import run not found")
	}
	if err != nil {
		return nil, err
	}
	if run.Status != "completed" {
		return nil, fmt.Errorf("legacy import run must be completed")
	}
	if run.DryRun {
		return nil, fmt.Errorf("dry-run import run cannot be terminalized")
	}
	actorUUID, err := nullableUUID(cmd.ActorID)
	if err != nil {
		return nil, err
	}

	result := &legacy_import.RejectConfirmedNonGoatResult{
		ImportRunID: cmd.ImportRunID,
		TenantID:    cmd.TenantID,
		DryRun:      cmd.DryRun,
	}
	if cmd.DryRun {
		count, err := r.queries.CountConfirmedNonGoatRowsForDisposition(ctx, importdb.CountConfirmedNonGoatRowsForDispositionParams{
			TenantID:    tenantUUID,
			ImportRunID: runUUID,
		})
		if err != nil {
			return nil, err
		}
		result.ScannedCount = int(count)
		return result, nil
	}

	for {
		rejected, err := r.rejectConfirmedNonGoatBatch(ctx, tenantUUID, runUUID, actorUUID, cmd)
		if err != nil {
			return nil, err
		}
		if rejected == 0 {
			break
		}
		result.ScannedCount += rejected
		result.RejectedCount += rejected
	}
	return result, nil
}

func (r *Repository) rejectConfirmedNonGoatBatch(ctx context.Context, tenantUUID, runUUID, actorUUID pgtype.UUID, cmd legacy_import.RejectConfirmedNonGoatCommand) (int, error) {
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
	rows, err := qtx.ListConfirmedNonGoatRowsForDisposition(ctx, importdb.ListConfirmedNonGoatRowsForDispositionParams{
		TenantID:    tenantUUID,
		ImportRunID: runUUID,
		LimitCount:  int32(cmd.BatchSize),
	})
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	rejected := 0
	reason := strings.TrimSpace(cmd.Reason)
	if reason == "" {
		reason = "Confirmed non-goat source breed; removed from actionable Goat Passport review queue."
	}
	for _, row := range rows {
		if !legacy_import.IsConfirmedNonGoatBreedLabel(row.SourceBreed) {
			continue
		}
		rowUUID, err := uuidParam(row.LegacyRowID)
		if err != nil {
			return 0, err
		}
		affected, err := qtx.RejectLegacyImportRowAsConfirmedNonGoat(ctx, importdb.RejectLegacyImportRowAsConfirmedNonGoatParams{
			TenantID:    tenantUUID,
			LegacyRowID: rowUUID,
		})
		if err != nil {
			return 0, err
		}
		if affected != 1 {
			continue
		}
		beforeState, afterState, metadata, err := nonGoatDispositionAuditPayloads(row, reason)
		if err != nil {
			return 0, err
		}
		if err := qtx.InsertAuditLogForImportRowDisposition(ctx, importdb.InsertAuditLogForImportRowDispositionParams{
			TenantID:    tenantUUID,
			ActorID:     actorUUID,
			Action:      "legacy_import_row.rejected",
			ResourceID:  rowUUID,
			BeforeState: beforeState,
			AfterState:  afterState,
			Metadata:    metadata,
			TraceID:     textParam("legacy-import-non-goat-disposition:" + cmd.ImportRunID),
		}); err != nil {
			return 0, err
		}
		rejected++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	committed = true
	return rejected, nil
}

func nonGoatDispositionAuditPayloads(row importdb.ListConfirmedNonGoatRowsForDispositionRow, reason string) ([]byte, []byte, []byte, error) {
	before := map[string]any{
		"processing_state": legacy_import.StateNeedsReview,
		"error_reason":     row.ErrorReason,
		"review_reasons":   row.ReviewReasons,
	}
	after := map[string]any{
		"processing_state": legacy_import.StateRejected,
		"error_reason":     confirmedNonGoatDispositionReason,
	}
	metadata := map[string]any{
		"reason":         reason,
		"source_breed":   row.SourceBreed,
		"row_number":     row.RowNumber,
		"source_row_key": row.SourceRowKey,
	}
	beforeBytes, err := json.Marshal(before)
	if err != nil {
		return nil, nil, nil, err
	}
	afterBytes, err := json.Marshal(after)
	if err != nil {
		return nil, nil, nil, err
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, nil, nil, err
	}
	return beforeBytes, afterBytes, metadataBytes, nil
}

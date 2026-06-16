package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	importRowResultType = "legacy_import_row"
	importRowAggregate  = "legacy_import_row"
)

// ReviewImportRow applies a safe staging-row review action in one transaction.
// It never mints a goat: reject is terminal, fix patches whitelisted normalized
// sex/breed, and reapply only requeues an eligible needs_review row to pending so
// the approved (operator-trusted, localtarget-guarded) RFID apply path mints the
// goat on its next run. The audited audit_log row is the review decision record;
// staging-row review is not an identity-graph mutation, so it writes no
// identity_decisions / goat_identity_events / outbox rows.
func (r *Repository) ReviewImportRow(ctx context.Context, cmd ports.ReviewImportRowCommand) (*ports.ReviewImportRowResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return nil, err
	}
	runUUID, err := uuidParam(cmd.ImportRunID)
	if err != nil {
		return nil, err
	}
	rowUUID, err := uuidParam(cmd.ImportRowID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
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

	_, err = qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          cmd.IdempotencyScope,
		RequestHash:    cmd.RequestHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return replayReviewedImportRow(ctx, qtx, tenantUUID, runUUID, rowUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	}
	if err != nil {
		return nil, err
	}

	var updated domain.ImportRunRow
	switch cmd.Action {
	case "reject":
		row, uErr := qtx.RejectImportRunRow(ctx, identitydb.RejectImportRunRowParams{
			TenantID:    tenantUUID,
			ImportRunID: runUUID,
			LegacyRowID: rowUUID,
			RowVersion:  int32(cmd.RowVersion),
		})
		if errors.Is(uErr, pgx.ErrNoRows) {
			return nil, importRowGuardError(ctx, qtx, tenantUUID, runUUID, rowUUID)
		}
		if uErr != nil {
			return nil, uErr
		}
		updated = importRowFromReviewFields(row.ImportRowID, row.SourceRecordID, row.RowNumber, row.RowState, row.RowVersion, row.SourceRowKey, row.MatchedGoatID, row.ErrorReason, row.Rfid, row.OldTag, row.Breed, row.Gender, row.Farm, row.Shed, row.Partition)
	case "fix":
		row, uErr := qtx.FixImportRunRowPayload(ctx, identitydb.FixImportRunRowPayloadParams{
			Sex:         nullableText(cmd.FixSex),
			Breed:       nullableText(cmd.FixBreed),
			TenantID:    tenantUUID,
			ImportRunID: runUUID,
			LegacyRowID: rowUUID,
			RowVersion:  int32(cmd.RowVersion),
		})
		if errors.Is(uErr, pgx.ErrNoRows) {
			return nil, importRowGuardError(ctx, qtx, tenantUUID, runUUID, rowUUID)
		}
		if uErr != nil {
			return nil, uErr
		}
		updated = importRowFromReviewFields(row.ImportRowID, row.SourceRecordID, row.RowNumber, row.RowState, row.RowVersion, row.SourceRowKey, row.MatchedGoatID, row.ErrorReason, row.Rfid, row.OldTag, row.Breed, row.Gender, row.Farm, row.Shed, row.Partition)
	case "reapply":
		row, uErr := qtx.ReapplyImportRunRow(ctx, identitydb.ReapplyImportRunRowParams{
			TenantID:    tenantUUID,
			ImportRunID: runUUID,
			LegacyRowID: rowUUID,
			RowVersion:  int32(cmd.RowVersion),
		})
		if errors.Is(uErr, pgx.ErrNoRows) {
			return nil, importRowGuardError(ctx, qtx, tenantUUID, runUUID, rowUUID)
		}
		if uErr != nil {
			return nil, uErr
		}
		updated = importRowFromReviewFields(row.ImportRowID, row.SourceRecordID, row.RowNumber, row.RowState, row.RowVersion, row.SourceRowKey, row.MatchedGoatID, row.ErrorReason, row.Rfid, row.OldTag, row.Breed, row.Gender, row.Farm, row.Shed, row.Partition)
	default:
		return nil, ports.ErrWriteConflict
	}

	afterState, err := json.Marshal(map[string]any{"row": updated})
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(map[string]any{
		"actor_id":               cmd.ActorID,
		"idempotency_key":        cmd.StoredIdempotencyKey,
		"client_idempotency_key": cmd.ClientIdempotencyKey,
		"idempotency_scope":      cmd.IdempotencyScope,
		"trace_id":               cmd.TraceID,
		"import_run_id":          cmd.ImportRunID,
		"action":                 cmd.Action,
		"reason":                 cmd.Reason,
		"evidence_refs":          cmd.EvidenceRefs,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertAuditLog(ctx, identitydb.InsertAuditLogParams{
		TenantID:     tenantUUID,
		ActorID:      actorUUID,
		Action:       "identity.import_row." + cmd.Action,
		ResourceType: importRowAggregate,
		ResourceID:   rowUUID,
		ScopeType:    pgtype.Text{},
		ScopeID:      pgtype.UUID{},
		AfterState:   afterState,
		Metadata:     metadata,
		TraceID:      nullableText(nonEmptyStringPtr(cmd.TraceID)),
	}); err != nil {
		return nil, err
	}
	if r.afterAuditHook != nil {
		if err := r.afterAuditHook(ctx); err != nil {
			return nil, err
		}
	}
	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(importRowResultType),
		ResultID:       rowUUID,
		IdempotencyKey: cmd.StoredIdempotencyKey,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	return &ports.ReviewImportRowResult{Row: updated}, nil
}

func replayReviewedImportRow(ctx context.Context, qtx *identitydb.Queries, tenantUUID, runUUID, rowUUID pgtype.UUID, key, requestHash string) (*ports.ReviewImportRowResult, error) {
	idempotency, err := qtx.GetIdempotencyKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, err
	}
	if idempotency.RequestHash != requestHash {
		return nil, ports.ErrIdempotencyConflict
	}
	if idempotency.Status != "completed" || idempotency.ResultType != importRowResultType || strings.TrimSpace(idempotency.ResultID) == "" {
		return nil, ports.ErrIdempotencyPending
	}
	row, err := qtx.GetImportRunRowForReview(ctx, identitydb.GetImportRunRowForReviewParams{
		TenantID:    tenantUUID,
		ImportRunID: runUUID,
		LegacyRowID: rowUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	firstResultID := idempotency.ResultID
	return &ports.ReviewImportRowResult{
		Row:           importRowFromReviewFields(row.ImportRowID, row.SourceRecordID, row.RowNumber, row.RowState, row.RowVersion, row.SourceRowKey, row.MatchedGoatID, row.ErrorReason, row.Rfid, row.OldTag, row.Breed, row.Gender, row.Farm, row.Shed, row.Partition),
		Replayed:      true,
		FirstResultID: &firstResultID,
	}, nil
}

func importRowGuardError(ctx context.Context, qtx *identitydb.Queries, tenantUUID, runUUID, rowUUID pgtype.UUID) error {
	if _, err := qtx.GetImportRunRowForReview(ctx, identitydb.GetImportRunRowForReviewParams{
		TenantID:    tenantUUID,
		ImportRunID: runUUID,
		LegacyRowID: rowUUID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	} else if err != nil {
		return err
	}
	// Row exists but the guarded update matched nothing: stale row_version or the
	// row is not in an eligible state for this action.
	return ports.ErrWriteConflict
}

func importRowFromReviewFields(importRowID, sourceRecordID string, rowNumber int32, rowState string, rowVersion int32, sourceRowKey, matchedGoatID, errorReason, rfid, oldTag, breed, gender, farm, shed, partition string) domain.ImportRunRow {
	return domain.ImportRunRow{
		ImportRowID:     importRowID,
		SourceRecordID:  nonEmptyStringPtr(sourceRecordID),
		RowNumber:       int(rowNumber),
		RowState:        rowState,
		RowVersion:      int(rowVersion),
		ReviewReasons:   []string{},
		RFID:            nonEmptyStringPtr(rfid),
		OldTag:          nonEmptyStringPtr(oldTag),
		Breed:           nonEmptyStringPtr(breed),
		Gender:          nonEmptyStringPtr(gender),
		Farm:            nonEmptyStringPtr(farm),
		Shed:            nonEmptyStringPtr(shed),
		Partition:       nonEmptyStringPtr(partition),
		SourceRowKeyRef: nonEmptyStringPtr(sourceRowKeyRef(sourceRowKey)),
		MatchedGoatID:   nonEmptyStringPtr(matchedGoatID),
		ErrorReason:     nonEmptyStringPtr(errorReason),
	}
}

package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

type importRunRowsCursor struct {
	Version     int    `json:"version"`
	RowNumber   int    `json:"row_number"`
	ImportRowID string `json:"import_row_id"`
}

func (r *Repository) GetImportRun(ctx context.Context, tenantID, importRunID string) (*domain.ImportRun, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(tenantID)
	if err != nil {
		return nil, err
	}
	runUUID, err := uuidParam(importRunID)
	if err != nil {
		return nil, err
	}

	row, err := r.queries.GetImportRunByID(ctx, identitydb.GetImportRunByIDParams{
		TenantID:    tenantUUID,
		ImportRunID: runUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return &domain.ImportRun{
		ImportRunID:   row.ImportRunID,
		SourceSystem:  row.SourceSystem,
		SourceDataset: row.SourceDataset,
		PolicyVersion: row.PolicyVersion,
		Status:        row.Status,
		DryRun:        row.DryRun,
		Summary: domain.ImportRunSummary{
			RowsProcessed:         int(row.RowCount),
			GoatsCreated:          int(row.CreatedGoatCount),
			IdentifiersAdded:      nil,
			CleanMatches:          nil,
			DuplicatesFound:       nil,
			MissingRequiredFields: nil,
			ConflictsOpened:       int(row.ConflictCount),
			RowsNeedingReview:     int(row.RowsNeedingReview),
			RowsRejected:          int(row.RowsRejected),
			ErrorCount:            int(row.ErrorCount),
		},
		CreatedAt:   pgTime(row.StartedAt),
		CompletedAt: pgTimePtr(row.CompletedAt),
	}, nil
}

func (r *Repository) ListImportRunRows(ctx context.Context, params ports.ListImportRunRowsParams) ([]domain.ImportRunRow, *string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(params.TenantID)
	if err != nil {
		return nil, nil, err
	}
	runUUID, err := uuidParam(params.ImportRunID)
	if err != nil {
		return nil, nil, err
	}

	cursorRowNumber := pgtype.Int4{}
	cursorRowID := pgtype.UUID{}
	if params.Cursor != nil && strings.TrimSpace(*params.Cursor) != "" {
		cursorRowNumber, cursorRowID, err = decodeImportRunRowsCursor(*params.Cursor)
		if err != nil {
			return nil, nil, err
		}
	}

	rows, err := r.queries.ListImportRunRows(ctx, identitydb.ListImportRunRowsParams{
		TenantID:          tenantUUID,
		ImportRunID:       runUUID,
		ProcessingState:   nullableText(params.ProcessingState),
		ReasonCode:        nullableText(params.ReasonCode),
		CursorRowNumber:   cursorRowNumber,
		CursorLegacyRowID: cursorRowID,
		LimitCount:        int32(params.Limit + 1),
	})
	if err != nil {
		return nil, nil, err
	}

	items := make([]domain.ImportRunRow, 0, min(len(rows), params.Limit))
	for _, row := range rows {
		items = append(items, importRunRowFromSQLC(row))
	}

	var next *string
	if len(items) > params.Limit {
		cursor, err := encodeImportRunRowsCursor(items[params.Limit-1])
		if err != nil {
			return nil, nil, err
		}
		next = &cursor
		items = items[:params.Limit]
	}
	return items, next, nil
}

func importRunRowFromSQLC(row identitydb.ListImportRunRowsRow) domain.ImportRunRow {
	return domain.ImportRunRow{
		ImportRowID:     row.ImportRowID,
		SourceRecordID:  nonEmptyStringPtr(row.SourceRecordID),
		RowNumber:       int(row.RowNumber),
		RowState:        row.RowState,
		ReviewReasons:   ensureStringSlice(row.ReviewReasons),
		RFID:            nonEmptyStringPtr(row.Rfid),
		OldTag:          nonEmptyStringPtr(row.OldTag),
		Breed:           nonEmptyStringPtr(row.Breed),
		Gender:          nonEmptyStringPtr(row.Gender),
		Farm:            nonEmptyStringPtr(row.Farm),
		Shed:            nonEmptyStringPtr(row.Shed),
		Partition:       nonEmptyStringPtr(row.Partition),
		SourceRowKeyRef: nonEmptyStringPtr(sourceRowKeyRef(row.SourceRowKey)),
		MatchedGoatID:   nonEmptyStringPtr(row.MatchedGoatID),
		ErrorReason:     nonEmptyStringPtr(row.ErrorReason),
	}
}

func encodeImportRunRowsCursor(item domain.ImportRunRow) (string, error) {
	raw, err := json.Marshal(importRunRowsCursor{
		Version:     1,
		RowNumber:   item.RowNumber,
		ImportRowID: item.ImportRowID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeImportRunRowsCursor(value string) (pgtype.Int4, pgtype.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return pgtype.Int4{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cursor importRunRowsCursor
	if err := decoder.Decode(&cursor); err != nil {
		return pgtype.Int4{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return pgtype.Int4{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	if cursor.Version != 1 || cursor.RowNumber < 1 {
		return pgtype.Int4{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	rowID, err := uuidParam(cursor.ImportRowID)
	if err != nil {
		return pgtype.Int4{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	return pgtype.Int4{Int32: int32(cursor.RowNumber), Valid: true}, rowID, nil
}

func sourceRowKeyRef(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func ensureStringSlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

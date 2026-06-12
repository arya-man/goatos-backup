package postgres

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

type timelineCursor struct {
	Version  int       `json:"version"`
	Occurred time.Time `json:"occurred_at"`
	EventID  string    `json:"event_id"`
}

type correctionListCursor struct {
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	CorrectionID string    `json:"correction_request_id"`
}

func (r *Repository) GetGoatTimeline(ctx context.Context, params ports.GetGoatTimelineParams) ([]domain.GoatTimelineEvent, *string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(params.TenantID)
	if err != nil {
		return nil, nil, err
	}
	goatUUID, err := uuidParam(params.GoatID)
	if err != nil {
		return nil, nil, err
	}

	cursorOccurredAt := pgtype.Timestamptz{}
	cursorEventID := pgtype.UUID{}
	if params.Cursor != nil && strings.TrimSpace(*params.Cursor) != "" {
		cursorOccurredAt, cursorEventID, err = decodeTimelineCursor(*params.Cursor)
		if err != nil {
			return nil, nil, err
		}
	}

	rows, err := r.queries.ListGoatTimeline(ctx, identitydb.ListGoatTimelineParams{
		TenantID:         tenantUUID,
		GoatID:           goatUUID,
		CursorOccurredAt: cursorOccurredAt,
		CursorEventID:    cursorEventID,
		LimitCount:       int32(params.Limit + 1),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}

	items := make([]domain.GoatTimelineEvent, 0, min(len(rows), params.Limit))
	for _, row := range rows {
		item, err := timelineEventFromSQLC(row)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}

	var next *string
	if len(items) > params.Limit {
		cursor, err := encodeTimelineCursor(items[params.Limit-1])
		if err != nil {
			return nil, nil, err
		}
		next = &cursor
		items = items[:params.Limit]
	}
	return items, next, nil
}

func (r *Repository) ListCorrectionRequests(ctx context.Context, params ports.ListCorrectionRequestsParams) ([]domain.CorrectionRequest, *string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(params.TenantID)
	if err != nil {
		return nil, nil, err
	}
	createdByUUID := pgtype.UUID{}
	if params.CreatedBy != nil && strings.TrimSpace(*params.CreatedBy) != "" {
		createdByUUID, err = uuidParam(*params.CreatedBy)
		if err != nil {
			return nil, nil, err
		}
	}
	cursorCreatedAt := pgtype.Timestamptz{}
	cursorCorrectionID := pgtype.UUID{}
	if params.Cursor != nil && strings.TrimSpace(*params.Cursor) != "" {
		cursorCreatedAt, cursorCorrectionID, err = decodeCorrectionListCursor(*params.Cursor)
		if err != nil {
			return nil, nil, err
		}
	}

	rows, err := r.queries.ListCorrectionRequests(ctx, identitydb.ListCorrectionRequestsParams{
		TenantID:                  tenantUUID,
		CreatedBy:                 createdByUUID,
		State:                     nullableText(params.State),
		CursorCreatedAt:           cursorCreatedAt,
		CursorCorrectionRequestID: cursorCorrectionID,
		LimitCount:                int32(params.Limit + 1),
	})
	if err != nil {
		return nil, nil, err
	}

	items := make([]domain.CorrectionRequest, 0, min(len(rows), params.Limit))
	for _, row := range rows {
		item, err := correctionRequestFromListRow(row)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	var next *string
	if len(items) > params.Limit {
		cursor, err := encodeCorrectionListCursor(items[params.Limit-1])
		if err != nil {
			return nil, nil, err
		}
		next = &cursor
		items = items[:params.Limit]
	}
	return items, next, nil
}

func timelineEventFromSQLC(row identitydb.ListGoatTimelineRow) (domain.GoatTimelineEvent, error) {
	refs, err := evidenceRefsFromJSON(row.EvidenceRefs)
	if err != nil {
		return domain.GoatTimelineEvent{}, err
	}
	return domain.GoatTimelineEvent{
		EventID:      row.EventID,
		EventType:    row.EventType,
		OccurredAt:   row.OccurredAt.Time,
		RecordedAt:   row.RecordedAt.Time,
		ActorType:    row.ActorType,
		EvidenceRefs: refs,
		DecisionID:   nonEmptyStringPtr(row.DecisionID),
	}, nil
}

func correctionRequestFromListRow(row identitydb.ListCorrectionRequestsRow) (domain.CorrectionRequest, error) {
	rowVersion := int(row.RowVersion)
	return correctionRequestFromFields(
		row.CorrectionRequestID,
		row.RequestType,
		row.State,
		row.GoatID,
		row.IdentifierType,
		row.IdentifierValue,
		row.FarmID,
		row.ParkID,
		row.ShedID,
		row.CohortID,
		row.Description,
		row.Evidence,
		&rowVersion,
		row.CreatedAt,
		row.ResolvedAt,
	)
}

func evidenceRefsFromJSON(raw string) ([]domain.EvidenceRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return []domain.EvidenceRef{}, nil
	}
	var refs []domain.EvidenceRef
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return nil, err
	}
	if refs == nil {
		refs = []domain.EvidenceRef{}
	}
	return refs, nil
}

func encodeTimelineCursor(item domain.GoatTimelineEvent) (string, error) {
	raw, err := json.Marshal(timelineCursor{Version: 1, Occurred: item.OccurredAt, EventID: item.EventID})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeTimelineCursor(value string) (pgtype.Timestamptz, pgtype.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cursor timelineCursor
	if err := decoder.Decode(&cursor); err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	if cursor.Version != 1 || cursor.Occurred.IsZero() {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	eventID, err := uuidParam(cursor.EventID)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	return pgtype.Timestamptz{Time: cursor.Occurred, Valid: true}, eventID, nil
}

func encodeCorrectionListCursor(item domain.CorrectionRequest) (string, error) {
	raw, err := json.Marshal(correctionListCursor{Version: 1, CreatedAt: item.CreatedAt, CorrectionID: item.CorrectionRequestID})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCorrectionListCursor(value string) (pgtype.Timestamptz, pgtype.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cursor correctionListCursor
	if err := decoder.Decode(&cursor); err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	if cursor.Version != 1 || cursor.CreatedAt.IsZero() {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	correctionID, err := uuidParam(cursor.CorrectionID)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	return pgtype.Timestamptz{Time: cursor.CreatedAt, Valid: true}, correctionID, nil
}

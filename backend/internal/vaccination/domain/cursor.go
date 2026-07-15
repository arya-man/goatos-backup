package domain

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

type recordedCompletionCursorPayload struct {
	AdministeredAt string `json:"a"`
	CompletionID   string `json:"c"`
}

type stageReviewItemCursorPayload struct {
	CreatedAt    string `json:"ca"`
	ReviewItemID string `json:"ri"`
}

func EncodeRecordedCompletionCursor(cursor RecordedCompletionCursor) (string, error) {
	raw, err := json.Marshal(recordedCompletionCursorPayload{
		AdministeredAt: cursor.AdministeredAt.UTC().Format(time.RFC3339Nano),
		CompletionID:   cursor.CompletionID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeRecordedCompletionCursor(value string) (RecordedCompletionCursor, error) {
	if strings.TrimSpace(value) == "" {
		return RecordedCompletionCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return RecordedCompletionCursor{}, ErrInvalidCursor
	}
	var payload recordedCompletionCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return RecordedCompletionCursor{}, ErrInvalidCursor
	}
	administeredAt, err := time.Parse(time.RFC3339Nano, payload.AdministeredAt)
	if err != nil || administeredAt.IsZero() || !uuidutil.IsUUIDString(payload.CompletionID) {
		return RecordedCompletionCursor{}, ErrInvalidCursor
	}
	return RecordedCompletionCursor{
		AdministeredAt: administeredAt,
		CompletionID:   payload.CompletionID,
	}, nil
}

// EncodeStageReviewItemCursor encodes a stage review item cursor for use in API pagination (VACC-REV-10B).
func EncodeStageReviewItemCursor(cursor StageReviewItemCursor) (string, error) {
	raw, err := json.Marshal(stageReviewItemCursorPayload{
		CreatedAt:    cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ReviewItemID: cursor.ReviewItemID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeStageReviewItemCursor decodes an API cursor for stage review item pagination (VACC-REV-10B).
func DecodeStageReviewItemCursor(value string) (StageReviewItemCursor, error) {
	if strings.TrimSpace(value) == "" {
		return StageReviewItemCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return StageReviewItemCursor{}, ErrInvalidCursor
	}
	var payload stageReviewItemCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return StageReviewItemCursor{}, ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil || createdAt.IsZero() || !uuidutil.IsUUIDString(payload.ReviewItemID) {
		return StageReviewItemCursor{}, ErrInvalidCursor
	}
	return StageReviewItemCursor{
		CreatedAt:    createdAt,
		ReviewItemID: payload.ReviewItemID,
	}, nil
}

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

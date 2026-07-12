package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const maxCursorLength = 512

// Cursor keysets the verification queue by (captured_at, item_id) ascending — oldest-captured-first,
// so the verifier works the standing backlog in submission order (verifier-app-and-flow.md).
type Cursor struct {
	CapturedAt time.Time
	ItemID     string
}

type cursorPayload struct {
	Version    int    `json:"v"`
	CapturedAt string `json:"c"`
	ItemID     string `json:"i"`
}

func EncodeCursor(cursor Cursor) (string, error) {
	if cursor.CapturedAt.IsZero() || !uuidutil.IsUUIDString(cursor.ItemID) {
		return "", fmt.Errorf("invalid verification cursor")
	}
	raw, err := json.Marshal(cursorPayload{Version: 1, CapturedAt: cursor.CapturedAt.UTC().Format(time.RFC3339Nano), ItemID: cursor.ItemID})
	if err != nil {
		return "", fmt.Errorf("encode verification cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCursor(value string) (Cursor, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxCursorLength {
		return Cursor{}, fmt.Errorf("invalid verification cursor length")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode verification cursor: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return Cursor{}, fmt.Errorf("decode verification cursor payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Cursor{}, fmt.Errorf("decode verification cursor payload: trailing data")
	}
	capturedAt, err := time.Parse(time.RFC3339Nano, payload.CapturedAt)
	if payload.Version != 1 || err != nil || capturedAt.IsZero() || !uuidutil.IsUUIDString(payload.ItemID) {
		return Cursor{}, fmt.Errorf("invalid verification cursor payload")
	}
	return Cursor{CapturedAt: capturedAt.UTC(), ItemID: payload.ItemID}, nil
}

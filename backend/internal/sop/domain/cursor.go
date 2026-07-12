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

const maxSOPCursorLength = 512

type SOPCursor struct {
	UpdatedAt time.Time
	SOPID     string
}

type sopCursorPayload struct {
	Version   int    `json:"v"`
	UpdatedAt string `json:"u"`
	SOPID     string `json:"i"`
}

func EncodeSOPCursor(cursor SOPCursor) (string, error) {
	if cursor.UpdatedAt.IsZero() || !uuidutil.IsUUIDString(cursor.SOPID) {
		return "", fmt.Errorf("invalid SOP cursor")
	}
	raw, err := json.Marshal(sopCursorPayload{Version: 1, UpdatedAt: cursor.UpdatedAt.UTC().Format(time.RFC3339Nano), SOPID: cursor.SOPID})
	if err != nil {
		return "", fmt.Errorf("encode SOP cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeSOPCursor(value string) (SOPCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxSOPCursorLength {
		return SOPCursor{}, fmt.Errorf("invalid SOP cursor length")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return SOPCursor{}, fmt.Errorf("decode SOP cursor: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload sopCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return SOPCursor{}, fmt.Errorf("decode SOP cursor payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return SOPCursor{}, fmt.Errorf("decode SOP cursor payload: trailing data")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, payload.UpdatedAt)
	if payload.Version != 1 || err != nil || updatedAt.IsZero() || !uuidutil.IsUUIDString(payload.SOPID) {
		return SOPCursor{}, fmt.Errorf("invalid SOP cursor payload")
	}
	return SOPCursor{UpdatedAt: updatedAt.UTC(), SOPID: payload.SOPID}, nil
}

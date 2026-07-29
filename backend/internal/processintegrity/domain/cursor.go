package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	MaxCursorLength        = 512
	maxCursorPayloadLength = 384
	MaxRowIDLength         = 256
)

type cursorPayload struct {
	SortPriority int    `json:"s"`
	DueAt        string `json:"d"`
	RowID        string `json:"r"`
}

func EncodeCursor(c Cursor) (string, error) {
	payload := cursorPayload{
		SortPriority: c.SortPriority,
		DueAt:        c.DueAt.UTC().Format(time.RFC3339Nano),
		RowID:        c.RowID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	if len(s) > MaxCursorLength {
		return Cursor{}, fmt.Errorf("decode cursor: too large")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	if len(raw) > maxCursorPayloadLength {
		return Cursor{}, fmt.Errorf("decode cursor payload: too large")
	}
	var payload cursorPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return Cursor{}, fmt.Errorf("decode cursor payload: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Cursor{}, fmt.Errorf("decode cursor payload: trailing data")
	}
	if payload.SortPriority < 0 || payload.SortPriority > 100 {
		return Cursor{}, fmt.Errorf("decode cursor sort priority: out of range")
	}
	dueAt, err := time.Parse(time.RFC3339Nano, payload.DueAt)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor due_at: %w", err)
	}
	if dueAt.IsZero() {
		return Cursor{}, fmt.Errorf("decode cursor due_at: zero")
	}
	if err := ValidateRowID(payload.RowID); err != nil {
		return Cursor{}, fmt.Errorf("decode cursor row_id: %w", err)
	}
	return Cursor{SortPriority: payload.SortPriority, DueAt: dueAt, RowID: payload.RowID}, nil
}

func ValidateRowID(rowID string) error {
	if rowID != strings.TrimSpace(rowID) {
		return fmt.Errorf("invalid whitespace")
	}
	if rowID == "" {
		return fmt.Errorf("empty")
	}
	if len(rowID) > MaxRowIDLength {
		return fmt.Errorf("too large")
	}
	parts := strings.Split(rowID, ":")
	switch {
	case len(parts) == 2 && parts[0] == "obligation" && isUUIDString(parts[1]):
		return nil
	case len(parts) == 6 &&
		parts[0] == "batch" && isUUIDString(parts[1]) &&
		parts[2] == "rule" && isUUIDString(parts[3]) &&
		parts[4] == "shed" && isUUIDString(parts[5]):
		return nil
	case len(parts) == 12 &&
		parts[0] == "batch" && isUUIDString(parts[1]) &&
		parts[2] == "rule" && isUUIDString(parts[3]) &&
		parts[4] == "protocol_version" && isUUIDString(parts[5]) &&
		parts[6] == "shed" && isUUIDString(parts[7]) &&
		parts[8] == "partition" && isRowIDSegment(parts[9]) &&
		parts[10] == "date" && isBusinessDate(parts[11]):
		return nil
	case len(parts) == 2 && parts[0] == "feed_projection_exception" && isUUIDString(parts[1]):
		return nil
	default:
		return fmt.Errorf("invalid shape")
	}
}

func isRowIDSegment(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func isBusinessDate(value string) bool {
	if len(value) != len("2006-01-02") {
		return false
	}
	if value[4] != '-' || value[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func isUUIDString(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

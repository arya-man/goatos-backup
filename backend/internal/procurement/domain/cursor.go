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
	maxCursorLength        = 512
	maxCursorPayloadLength = 384
)

type cursorPayload struct {
	Kind      string `json:"k"`
	UpdatedAt string `json:"u"`
	ID        string `json:"i"`
}

func EncodeLoadCursor(c LoadCursor) (string, error) {
	return encodeCursor(cursorPayload{Kind: "load", UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339Nano), ID: c.LoadID})
}

func DecodeLoadCursor(value string) (LoadCursor, error) {
	payload, err := decodeCursor(value, "load")
	if err != nil {
		return LoadCursor{}, err
	}
	if !isUUIDString(payload.ID) {
		return LoadCursor{}, fmt.Errorf("invalid load cursor id")
	}
	return LoadCursor{UpdatedAt: payload.UpdatedAt, LoadID: payload.ID}, nil
}

func EncodeWorkCursor(c WorkCursor) (string, error) {
	return encodeCursor(cursorPayload{Kind: "work", UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339Nano), ID: c.RowID})
}

func DecodeWorkCursor(value string) (WorkCursor, error) {
	payload, err := decodeCursor(value, "work")
	if err != nil {
		return WorkCursor{}, err
	}
	if err := ValidateRowID(payload.ID); err != nil {
		return WorkCursor{}, err
	}
	return WorkCursor{UpdatedAt: payload.UpdatedAt, RowID: payload.ID}, nil
}

func ValidateRowID(rowID string) error {
	if rowID == "" || rowID != strings.TrimSpace(rowID) || len(rowID) > 128 {
		return fmt.Errorf("invalid row id")
	}
	parts := strings.Split(rowID, ":")
	switch {
	case len(parts) == 2 && parts[0] == "load" && isUUIDString(parts[1]):
		return nil
	case len(parts) == 2 && parts[0] == "load_goat" && isUUIDString(parts[1]):
		return nil
	default:
		return fmt.Errorf("invalid row id")
	}
}

func encodeCursor(payload cursorPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type decodedCursor struct {
	UpdatedAt time.Time
	ID        string
}

func decodeCursor(value, kind string) (decodedCursor, error) {
	if value == "" {
		return decodedCursor{}, nil
	}
	if len(value) > maxCursorLength {
		return decodedCursor{}, fmt.Errorf("cursor too large")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return decodedCursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	if len(raw) > maxCursorPayloadLength {
		return decodedCursor{}, fmt.Errorf("cursor payload too large")
	}
	var payload cursorPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return decodedCursor{}, fmt.Errorf("decode cursor payload: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return decodedCursor{}, fmt.Errorf("decode cursor payload: trailing data")
	}
	if payload.Kind != kind {
		return decodedCursor{}, fmt.Errorf("cursor kind mismatch")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, payload.UpdatedAt)
	if err != nil || updatedAt.IsZero() {
		return decodedCursor{}, fmt.Errorf("invalid cursor timestamp")
	}
	return decodedCursor{UpdatedAt: updatedAt, ID: payload.ID}, nil
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

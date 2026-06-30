package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	MaxProjectionExceptionCursorLength        = 512
	maxProjectionExceptionCursorPayloadLength = 384
)

type projectionExceptionCursorPayload struct {
	Kind      string `json:"k"`
	UpdatedAt string `json:"u"`
	ID        string `json:"i"`
}

func EncodeProjectionExceptionCursor(c ProjectionExceptionCursor) (string, error) {
	raw, err := json.Marshal(projectionExceptionCursorPayload{
		Kind:      "counts_projection_exception",
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ID:        c.ProjectionExceptionID,
	})
	if err != nil {
		return "", fmt.Errorf("encode projection exception cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeProjectionExceptionCursor(value string) (ProjectionExceptionCursor, error) {
	if value == "" {
		return ProjectionExceptionCursor{}, nil
	}
	if len(value) > MaxProjectionExceptionCursorLength {
		return ProjectionExceptionCursor{}, fmt.Errorf("decode projection exception cursor: too large")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return ProjectionExceptionCursor{}, fmt.Errorf("decode projection exception cursor: %w", err)
	}
	if len(raw) > maxProjectionExceptionCursorPayloadLength {
		return ProjectionExceptionCursor{}, fmt.Errorf("decode projection exception cursor payload: too large")
	}
	var payload projectionExceptionCursorPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return ProjectionExceptionCursor{}, fmt.Errorf("decode projection exception cursor payload: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return ProjectionExceptionCursor{}, fmt.Errorf("decode projection exception cursor payload: trailing data")
	}
	if payload.Kind != "counts_projection_exception" {
		return ProjectionExceptionCursor{}, fmt.Errorf("decode projection exception cursor: kind mismatch")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, payload.UpdatedAt)
	if err != nil || updatedAt.IsZero() {
		return ProjectionExceptionCursor{}, fmt.Errorf("decode projection exception cursor: invalid updated_at")
	}
	if !IsUUIDString(payload.ID) {
		return ProjectionExceptionCursor{}, fmt.Errorf("decode projection exception cursor: invalid id")
	}
	return ProjectionExceptionCursor{UpdatedAt: updatedAt, ProjectionExceptionID: payload.ID}, nil
}

func IsUUIDString(value string) bool {
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

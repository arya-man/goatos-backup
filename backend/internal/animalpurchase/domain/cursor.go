package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Cursor is a keyset position: (created_at, id) for loads and the review queue, (seq_no)
// for a load's animals. Opaque to clients.
type Cursor struct {
	Kind      string `json:"k"`
	CreatedAt string `json:"t,omitempty"`
	ID        string `json:"i,omitempty"`
	Seq       int    `json:"s,omitempty"`
}

const (
	CursorKindLoad      = "load"
	CursorKindCandidate = "candidate"
	CursorKindReview    = "review"
	maxCursorLength     = 512
)

func EncodeCursor(c Cursor) string {
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func DecodeCursor(value, kind string) (Cursor, error) {
	if value == "" {
		return Cursor{}, nil
	}
	if len(value) > maxCursorLength {
		return Cursor{}, fmt.Errorf("cursor too large")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	var c Cursor
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Cursor{}, fmt.Errorf("decode cursor payload: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Cursor{}, fmt.Errorf("decode cursor payload: trailing data")
	}
	if c.Kind != kind {
		return Cursor{}, fmt.Errorf("cursor kind mismatch")
	}
	if c.CreatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, c.CreatedAt); err != nil {
			return Cursor{}, fmt.Errorf("invalid cursor timestamp")
		}
	}
	return c, nil
}

// ClampLimit bounds a caller's page size to the screen page.
func ClampLimit(limit int) int {
	if limit <= 0 {
		return DefaultPageSize
	}
	if limit > MaxPageSize {
		return MaxPageSize
	}
	return limit
}

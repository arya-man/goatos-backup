package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const maxExecutionCursorLength = 512

// ExecutionCursor is the stable SQL ordering tuple for the execution board. The compact JSON shape
// is opaque to callers and versioned so future ordering changes fail closed instead of skipping rows.
type ExecutionCursor struct {
	SortRank      int    `json:"r"`
	SortDueMicros int64  `json:"d"`
	SortRowKey    string `json:"k"`
}

type executionCursorPayload struct {
	Version int    `json:"v"`
	Rank    int    `json:"r"`
	Due     int64  `json:"d"`
	Key     string `json:"k"`
}

func EncodeExecutionCursor(cursor ExecutionCursor) (string, error) {
	if err := validateExecutionCursor(cursor); err != nil {
		return "", err
	}
	raw, err := json.Marshal(executionCursorPayload{Version: 1, Rank: cursor.SortRank, Due: cursor.SortDueMicros, Key: cursor.SortRowKey})
	if err != nil {
		return "", fmt.Errorf("encode execution cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeExecutionCursor(value string) (ExecutionCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxExecutionCursorLength {
		return ExecutionCursor{}, fmt.Errorf("invalid execution cursor length")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return ExecutionCursor{}, fmt.Errorf("decode execution cursor: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var payload executionCursorPayload
	if err := dec.Decode(&payload); err != nil {
		return ExecutionCursor{}, fmt.Errorf("decode execution cursor payload: %w", err)
	}
	if dec.More() {
		return ExecutionCursor{}, fmt.Errorf("decode execution cursor payload: trailing data")
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return ExecutionCursor{}, fmt.Errorf("decode execution cursor payload: trailing data")
	}
	if payload.Version != 1 {
		return ExecutionCursor{}, fmt.Errorf("unsupported execution cursor version")
	}
	cursor := ExecutionCursor{SortRank: payload.Rank, SortDueMicros: payload.Due, SortRowKey: payload.Key}
	if err := validateExecutionCursor(cursor); err != nil {
		return ExecutionCursor{}, err
	}
	return cursor, nil
}

func validateExecutionCursor(cursor ExecutionCursor) error {
	if cursor.SortRank < 0 || cursor.SortRank > 11 {
		return fmt.Errorf("invalid execution cursor rank")
	}
	if len(cursor.SortRowKey) == 0 || len(cursor.SortRowKey) > 192 || strings.ContainsAny(cursor.SortRowKey, "\r\n\t") {
		return fmt.Errorf("invalid execution cursor row key")
	}
	return nil
}

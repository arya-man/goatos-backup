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

const maxTaskCursorLength = 512

type SOPCursor struct {
	UpdatedAt time.Time
	SOPID     string
}

type sopCursorPayload struct {
	Version   int    `json:"v"`
	UpdatedAt string `json:"u"`
	SOPID     string `json:"i"`
}

type TaskCursor struct {
	DueAt  *time.Time
	TaskID string
}

type taskCursorPayload struct {
	Version int    `json:"v"`
	DueAt   string `json:"d,omitempty"`
	TaskID  string `json:"i"`
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

func EncodeTaskCursor(cursor TaskCursor) (string, error) {
	if !uuidutil.IsUUIDString(cursor.TaskID) {
		return "", fmt.Errorf("invalid task cursor")
	}
	dueAt := ""
	if cursor.DueAt != nil {
		if cursor.DueAt.IsZero() {
			return "", fmt.Errorf("invalid task cursor")
		}
		dueAt = cursor.DueAt.UTC().Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(taskCursorPayload{Version: 1, DueAt: dueAt, TaskID: cursor.TaskID})
	if err != nil {
		return "", fmt.Errorf("encode task cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeTaskCursor(value string) (TaskCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxTaskCursorLength {
		return TaskCursor{}, fmt.Errorf("invalid task cursor length")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return TaskCursor{}, fmt.Errorf("decode task cursor: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload taskCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return TaskCursor{}, fmt.Errorf("decode task cursor payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return TaskCursor{}, fmt.Errorf("decode task cursor payload: trailing data")
	}
	if payload.Version != 1 || !uuidutil.IsUUIDString(payload.TaskID) {
		return TaskCursor{}, fmt.Errorf("invalid task cursor payload")
	}
	var dueAt *time.Time
	if payload.DueAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, payload.DueAt)
		if err != nil || parsed.IsZero() {
			return TaskCursor{}, fmt.Errorf("invalid task cursor payload")
		}
		parsed = parsed.UTC()
		dueAt = &parsed
	}
	return TaskCursor{DueAt: dueAt, TaskID: payload.TaskID}, nil
}

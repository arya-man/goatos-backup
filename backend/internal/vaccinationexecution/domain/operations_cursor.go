package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const maxOperationsCursorLength = 512

// OperationsCursor is the stable cohort key used by GET /vaccination/operations.
// The repository orders cohorts by this tuple before expanding each cohort into
// its protocol cells, so a page never splits a cohort across responses.
type OperationsCursor struct {
	ParkID string
	ShedID string
	Stage  string
}

type operationsCursorPayload struct {
	Kind   string `json:"k"`
	ParkID string `json:"p"`
	ShedID string `json:"s"`
	Stage  string `json:"g"`
}

func EncodeOperationsCursor(cursor OperationsCursor) (string, error) {
	if err := validateOperationsCursor(cursor); err != nil {
		return "", err
	}
	raw, err := json.Marshal(operationsCursorPayload{
		Kind: "vaccination_operations", ParkID: cursor.ParkID, ShedID: cursor.ShedID, Stage: cursor.Stage,
	})
	if err != nil {
		return "", fmt.Errorf("encode vaccination operations cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeOperationsCursor(value string) (OperationsCursor, error) {
	if value == "" || len(value) > maxOperationsCursorLength {
		return OperationsCursor{}, fmt.Errorf("invalid vaccination operations cursor")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) > maxOperationsCursorLength {
		return OperationsCursor{}, fmt.Errorf("invalid vaccination operations cursor")
	}
	var payload operationsCursorPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return OperationsCursor{}, fmt.Errorf("invalid vaccination operations cursor: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return OperationsCursor{}, fmt.Errorf("invalid vaccination operations cursor")
	}
	if payload.Kind != "vaccination_operations" {
		return OperationsCursor{}, fmt.Errorf("invalid vaccination operations cursor kind")
	}
	cursor := OperationsCursor{ParkID: payload.ParkID, ShedID: payload.ShedID, Stage: payload.Stage}
	if err := validateOperationsCursor(cursor); err != nil {
		return OperationsCursor{}, err
	}
	return cursor, nil
}

func validateOperationsCursor(cursor OperationsCursor) error {
	if !operationsCursorUUID(cursor.ParkID) || !operationsCursorUUID(cursor.ShedID) {
		return fmt.Errorf("invalid vaccination operations cursor id")
	}
	if cursor.Stage == "" || cursor.Stage != strings.TrimSpace(cursor.Stage) || len(cursor.Stage) > 128 {
		return fmt.Errorf("invalid vaccination operations cursor stage")
	}
	return nil
}

func operationsCursorUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
				return false
			}
		}
	}
	return true
}

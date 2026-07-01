package domain

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

func EncodeDriveTargetCursor(cursor DriveTargetCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeDriveTargetCursor(value string) (DriveTargetCursor, error) {
	if strings.TrimSpace(value) == "" {
		return DriveTargetCursor{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return DriveTargetCursor{}, ErrInvalidCursor
	}
	var cursor DriveTargetCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.ObligationID == "" {
		return DriveTargetCursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

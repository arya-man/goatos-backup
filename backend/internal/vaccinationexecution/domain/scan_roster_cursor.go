package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// maxScanRosterCursorBytes caps the decoded cursor payload so a hostile client cannot force an
// oversized JSON parse; a legitimate cursor is two UUIDs and a few bytes of JSON framing.
const maxScanRosterCursorBytes = 256

func EncodeScanRosterCursor(cursor ScanRosterCursor) (string, error) {
	if cursor.GoatID == "" || cursor.ObligationID == "" {
		return "", errors.New("scan roster cursor requires goat_id and obligation_id")
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeScanRosterCursor(raw string) (ScanRosterCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return ScanRosterCursor{}, err
	}
	if len(decoded) > maxScanRosterCursorBytes {
		return ScanRosterCursor{}, errors.New("invalid scan roster cursor: payload too large")
	}
	var cursor ScanRosterCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return ScanRosterCursor{}, err
	}
	// Both keys are cast to ::uuid in the keyset SQL — validate here so a malformed cursor is a
	// 400 (invalid_cursor) rather than a DB 500 (invalid input syntax for type uuid).
	if !uuidutil.IsUUIDString(cursor.GoatID) || !uuidutil.IsUUIDString(cursor.ObligationID) {
		return ScanRosterCursor{}, errors.New("invalid scan roster cursor")
	}
	return cursor, nil
}

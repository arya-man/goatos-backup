package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

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
	var cursor ScanRosterCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return ScanRosterCursor{}, err
	}
	if cursor.GoatID == "" || cursor.ObligationID == "" {
		return ScanRosterCursor{}, errors.New("invalid scan roster cursor")
	}
	return cursor, nil
}

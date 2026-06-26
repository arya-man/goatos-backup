package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

var (
	ErrInvalidCursor  = errors.New("calendar: invalid cursor")
	ErrInvalidEventID = errors.New("calendar: invalid event id")

	historyIDPattern = regexp.MustCompile(`^[A-Za-z0-9:_-]+$`)
)

func EncodeCalendarCursor(cursor CalendarCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCalendarCursor(value string) (CalendarCursor, error) {
	if strings.TrimSpace(value) == "" {
		return CalendarCursor{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return CalendarCursor{}, ErrInvalidCursor
	}
	var cursor CalendarCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.DueAt.IsZero() || cursor.EventID == "" {
		return CalendarCursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

func EncodeHistoryCursor(cursor HistoryCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeHistoryCursor(value string) (HistoryCursor, error) {
	if strings.TrimSpace(value) == "" {
		return HistoryCursor{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return HistoryCursor{}, ErrInvalidCursor
	}
	var cursor HistoryCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.OccurredAt.IsZero() || cursor.HistoryID == "" {
		return HistoryCursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

// ValidateEventID accepts source-backed Calendar IDs used by this slice:
// obligation:<uuid>, batch:<uuid>:rule:<uuid>:shed:<uuid>, and calendar:<uuid>.
func ValidateEventID(eventID string) error {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || len(eventID) > 256 {
		return ErrInvalidEventID
	}
	if strings.HasPrefix(eventID, "obligation:") {
		if uuidutil.IsUUIDString(strings.TrimPrefix(eventID, "obligation:")) {
			return nil
		}
		return ErrInvalidEventID
	}
	if strings.HasPrefix(eventID, "calendar:") {
		if uuidutil.IsUUIDString(strings.TrimPrefix(eventID, "calendar:")) {
			return nil
		}
		return ErrInvalidEventID
	}
	parts := strings.Split(eventID, ":")
	if len(parts) == 6 && parts[0] == "batch" && parts[2] == "rule" && parts[4] == "shed" &&
		uuidutil.IsUUIDString(parts[1]) && uuidutil.IsUUIDString(parts[3]) && uuidutil.IsUUIDString(parts[5]) {
		return nil
	}
	return ErrInvalidEventID
}

func ValidateHistoryID(historyID string) bool {
	return historyID != "" && len(historyID) <= 160 && historyIDPattern.MatchString(historyID)
}

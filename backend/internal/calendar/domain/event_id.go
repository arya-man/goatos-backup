package domain

import (
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// DriveTargetQuery identifies goats to list for a vaccination drive calendar event.
type DriveTargetQuery struct {
	TenantID string
	EventID  string
	Scope    ScopeFilter
	Cursor   *DriveTargetCursor
	Limit    int
}

type DriveTargetCursor struct {
	ObligationID string `json:"obligation_id"`
}

// ParsedDriveEvent describes a calendar drive event_id (real batch or synthetic catch-up).
type ParsedDriveEvent struct {
	BatchID  string
	RuleID   string
	ShedID   string
	Catchup  bool
	DueDay   string // YYYY-MM-DD in the IST business-date bucket
	TenantID string // for tenant-wide catch-up when shed is absent
}

// ParseDriveEventID accepts batch and catch-up drive calendar event IDs.
func ParseDriveEventID(eventID string) (ParsedDriveEvent, error) {
	eventID = strings.TrimSpace(eventID)
	parts := strings.Split(eventID, ":")
	if len(parts) == 6 && parts[0] == "batch" && parts[2] == "rule" && parts[4] == "shed" &&
		uuidutil.IsUUIDString(parts[1]) && uuidutil.IsUUIDString(parts[3]) && uuidutil.IsUUIDString(parts[5]) {
		return ParsedDriveEvent{BatchID: parts[1], RuleID: parts[3], ShedID: parts[5]}, nil
	}
	if len(parts) == 7 && parts[0] == "catchup" && parts[1] == "shed" && parts[3] == "rule" && parts[5] == "due" &&
		uuidutil.IsUUIDString(parts[2]) && uuidutil.IsUUIDString(parts[4]) && isDueDay(parts[6]) {
		return ParsedDriveEvent{Catchup: true, ShedID: parts[2], RuleID: parts[4], DueDay: parts[6]}, nil
	}
	if len(parts) == 7 && parts[0] == "catchup" && parts[1] == "tenant" && parts[3] == "rule" && parts[5] == "due" &&
		uuidutil.IsUUIDString(parts[2]) && uuidutil.IsUUIDString(parts[4]) && isDueDay(parts[6]) {
		return ParsedDriveEvent{Catchup: true, TenantID: parts[2], RuleID: parts[4], DueDay: parts[6]}, nil
	}
	return ParsedDriveEvent{}, ErrInvalidEventID
}

func isDueDay(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

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
	Search   string
}

type DriveTargetCursor struct {
	ObligationID string `json:"obligation_id"`
}

// ParsedDriveEvent describes a calendar drive event_id: a real batch, a synthetic catch-up, or a
// stable park/day aggregate ("park-drive"). CR-002/CR-003 (calendar-canonical-5k50k review): the
// park-drive identity is the STABLE, park+business-date-keyed identity a vaccination drive carries
// from the moment it exists, regardless of how many batches/catch-up sources later aggregate into
// it -- batch:/catchup: remain valid MEMBERSHIP identities (still parsed here, still resolvable
// individually) but are no longer the identity a solo drive is first assigned; see
// canonical_read.go's park_drive_events CTE, which now emits the ParkDrive form for every drive
// (drive_count == 1 or > 1) instead of collapsing a lone source's own batch:/catchup: id.
type ParsedDriveEvent struct {
	BatchID   string
	RuleID    string
	ParkID    string
	ShedID    string
	Catchup   bool
	ParkDrive bool   // true for the stable "parkdrive:park:<uuid>:date:<day>" / "parkdrive:tenant:<uuid>:date:<day>" identity
	DueDay    string // YYYY-MM-DD in the IST business-date bucket
	TenantID  string // for tenant-wide catch-up/park-drive when park/shed is absent
}

// FormatParkDriveEventID builds the stable park/day drive identity: "parkdrive:park:<uuid>:date:<day>"
// when parkID is known, otherwise "parkdrive:tenant:<uuid>:date:<day>" for a tenant-wide drive with no
// resolvable park (mirrors the SQL CASE in canonical_read.go's park_drive_events CTE exactly, so Go
// callers building/round-tripping this id -- tests, notification producers -- never drift from what
// the canonical read actually emits).
func FormatParkDriveEventID(parkID, tenantID, dueDay string) string {
	if parkID != "" {
		return "parkdrive:park:" + parkID + ":date:" + dueDay
	}
	return "parkdrive:tenant:" + tenantID + ":date:" + dueDay
}

type ParsedHistoryEvent struct {
	Day    string
	ParkID string
	ShedID string
	RuleID string
}

// ParseDriveEventID accepts batch and catch-up drive calendar event IDs.
func ParseDriveEventID(eventID string) (ParsedDriveEvent, error) {
	eventID = strings.TrimSpace(eventID)
	parts := strings.Split(eventID, ":")
	if len(parts) == 2 && parts[0] == "batch" && uuidutil.IsUUIDString(parts[1]) {
		return ParsedDriveEvent{BatchID: parts[1]}, nil
	}
	if len(parts) == 6 && parts[0] == "batch" && parts[2] == "rule" && parts[4] == "shed" &&
		uuidutil.IsUUIDString(parts[1]) && uuidutil.IsUUIDString(parts[3]) && uuidutil.IsUUIDString(parts[5]) {
		return ParsedDriveEvent{BatchID: parts[1], RuleID: parts[3], ShedID: parts[5]}, nil
	}
	if len(parts) == 5 && parts[0] == "catchup" && parts[1] == "park" && parts[3] == "due" &&
		uuidutil.IsUUIDString(parts[2]) && isDueDay(parts[4]) {
		return ParsedDriveEvent{Catchup: true, ParkID: parts[2], DueDay: parts[4]}, nil
	}
	if len(parts) == 5 && parts[0] == "catchup" && parts[1] == "tenant" && parts[3] == "due" &&
		uuidutil.IsUUIDString(parts[2]) && isDueDay(parts[4]) {
		return ParsedDriveEvent{Catchup: true, TenantID: parts[2], DueDay: parts[4]}, nil
	}
	if len(parts) == 7 && parts[0] == "catchup" && parts[1] == "shed" && parts[3] == "rule" && parts[5] == "due" &&
		uuidutil.IsUUIDString(parts[2]) && uuidutil.IsUUIDString(parts[4]) && isDueDay(parts[6]) {
		return ParsedDriveEvent{Catchup: true, ShedID: parts[2], RuleID: parts[4], DueDay: parts[6]}, nil
	}
	if len(parts) == 7 && parts[0] == "catchup" && parts[1] == "tenant" && parts[3] == "rule" && parts[5] == "due" &&
		uuidutil.IsUUIDString(parts[2]) && uuidutil.IsUUIDString(parts[4]) && isDueDay(parts[6]) {
		return ParsedDriveEvent{Catchup: true, TenantID: parts[2], RuleID: parts[4], DueDay: parts[6]}, nil
	}
	// Stable park/day drive identity (CR-002/CR-003): "parkdrive:park:<uuid>:date:<day>" or
	// "parkdrive:tenant:<uuid>:date:<day>" -- the identity every vaccination drive (solo or
	// aggregated) now carries from the start, per canonical_read.go's park_drive_events CTE.
	if len(parts) == 5 && parts[0] == "parkdrive" && parts[1] == "park" && parts[3] == "date" &&
		uuidutil.IsUUIDString(parts[2]) && isDueDay(parts[4]) {
		return ParsedDriveEvent{ParkDrive: true, ParkID: parts[2], DueDay: parts[4]}, nil
	}
	if len(parts) == 5 && parts[0] == "parkdrive" && parts[1] == "tenant" && parts[3] == "date" &&
		uuidutil.IsUUIDString(parts[2]) && isDueDay(parts[4]) {
		return ParsedDriveEvent{ParkDrive: true, TenantID: parts[2], DueDay: parts[4]}, nil
	}
	return ParsedDriveEvent{}, ErrInvalidEventID
}

func ParseHistoryEventID(eventID string) (ParsedHistoryEvent, error) {
	eventID = strings.TrimSpace(eventID)
	parts := strings.Split(eventID, ":")
	if len(parts) != 5 || parts[0] != "history" || !isDueDay(parts[1]) {
		return ParsedHistoryEvent{}, ErrInvalidEventID
	}
	if !isHistoryScopeID(parts[2]) || !isHistoryScopeID(parts[3]) || !uuidutil.IsUUIDString(parts[4]) {
		return ParsedHistoryEvent{}, ErrInvalidEventID
	}
	return ParsedHistoryEvent{
		Day:    parts[1],
		ParkID: parts[2],
		ShedID: parts[3],
		RuleID: parts[4],
	}, nil
}

func isHistoryScopeID(value string) bool {
	return value == "none" || uuidutil.IsUUIDString(value)
}

func isDueDay(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

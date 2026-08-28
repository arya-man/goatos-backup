package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// ErrMockLocationDetected refuses a punch whose payload admits a mock-provided
// fix or an installed mock-provider app. Surfaced as 422
// mock_location_detected with farm-worded copy naming what to remove.
var ErrMockLocationDetected = errors.New("mock location detected")

// ErrAlreadyClockedIn refuses a second clock-in on a business day that already
// has one (one pair per day, maintainer decision D4).
var ErrAlreadyClockedIn = errors.New("already clocked in for this business day")

// ErrNotClockedIn refuses a clock-out on a business day with no open clock-in.
var ErrNotClockedIn = errors.New("not clocked in for this business day")

// ErrAlreadyClockedOut refuses a second clock-out on an already-closed day.
var ErrAlreadyClockedOut = errors.New("already clocked out for this business day")

// ClockPunchCommand is the fully-resolved punch write. The app service has
// already run the integrity gate, resolved the member, derived the effective
// instant and business date, and composed the device snapshot from headers.
type ClockPunchCommand struct {
	TenantID          string
	WorkforceMemberID string
	UserID            string
	// EventType: clock_in | clock_out.
	EventType      string
	IdempotencyKey string
	BusinessDate   string
	// EffectiveAt is the punch instant the entry pairs on: server now for an
	// online punch, device captured_at for an offline-queued one.
	EffectiveAt time.Time
	CapturedAt  time.Time
	ClockSkewMs int64

	Location  domain.ClockLocation
	Integrity domain.ClockIntegrity

	DeviceID       string
	AppInstallID   string
	AppVersion     string
	AppVersionCode string
	BuildType      string
	OSVersion      string
	SDKVersion     string
	DeviceModel    string
	// NetworkType: online | offline_queued.
	NetworkType string
	BatteryPct  *int
}

// ClockPunchRecord is what the repository hands back: the RAW pairing row.
// Label composition is the app service's job.
type ClockPunchRecord struct {
	Entry ClockEntryRow
	// Replayed is true when the idempotency key matched a completed punch and
	// the stored snapshot was returned without side effects.
	Replayed bool
}

// ClockStatusParams reads one member's day + recent days.
type ClockStatusParams struct {
	TenantID          string
	WorkforceMemberID string
	BusinessDate      string
	RecentLimit       int
}

// ClockPresenceParams is the whole-roster day read behind the presence board
// and the admin list. Grain: one row per ACTIVE workforce member (LEFT JOIN to
// the day's entry), so never-punched people appear under not_clocked_in.
type ClockPresenceParams struct {
	TenantID     string
	BusinessDate string
	ParkID       string
	// RoleHint filters on workforce_members.primary_role_hint (the
	// "designation" filter both surfaces expose).
	RoleHint string
	// Bucket narrows to working | clocked_out | not_clocked_in | flagged.
	Bucket string
	Search string
	Limit  int
	Cursor string
}

// ClockPresencePage is the repository's raw page: rows plus the whole-filter
// summary computed in SQL (never from the page).
type ClockPresencePage struct {
	Summary    domain.ClockPresenceSummary
	Rows       []ClockPresenceRawRow
	NextCursor string
}

// ClockPresenceRawRow is one member joined to their (possibly absent) entry.
type ClockPresenceRawRow struct {
	WorkforceMemberID string
	PersonName        string
	RoleHint          string
	DesignationGrade  string
	ParkID            string
	ParkLabel         string
	DepartmentLabel   string
	Entry             *ClockEntryRow
}

// ClockEntryRow is the raw pairing row before label composition.
type ClockEntryRow struct {
	ClockEntryID      string
	WorkforceMemberID string
	BusinessDate      string
	Status            string
	ClockInAt         time.Time
	ClockOutAt        *time.Time
	WorkedMinutes     *int
	OfflinePunch      bool
	LocationMissing   bool
}

// ClockEventRow is the raw event row for detail reads.
type ClockEventRow struct {
	ClockEventID            string
	EventType               string
	BusinessDate            string
	CapturedAt              time.Time
	RecordedAt              time.Time
	ClockSkewMs             *int64
	LocationStatus          string
	Latitude                *float64
	Longitude               *float64
	GpsAccuracyM            *float64
	Address                 string
	MockLocation            bool
	DeveloperOptionsEnabled *bool
	DeviceModel             string
	AppVersion              string
	OSVersion               string
	NetworkType             string
	BatteryPct              *int
}

// ClockPersonDay is one person's day detail plus recent days.
type ClockPersonDay struct {
	Person     ClockPresenceRawRow
	Entry      *ClockEntryRow
	Events     []ClockEventRow
	RecentDays []ClockEntryRow
}

// ClockRepository is the attendance port, implemented by the same postgres
// Repository as the operator/people ports.
type ClockRepository interface {
	// RecordClockPunch runs ONE transaction: idempotency reservation, event
	// insert, entry insert/close, auto-close of this member's stale open
	// entries from earlier days, idempotency completion. An exact replay
	// returns the original entry snapshot without side effects.
	RecordClockPunch(ctx context.Context, cmd ClockPunchCommand) (ClockPunchRecord, error)
	// ClockDayForMember reads one member's entry for a business date plus
	// recent entries (newest first).
	ClockDayForMember(ctx context.Context, params ClockStatusParams) (*ClockEntryRow, []ClockEntryRow, error)
	// ListClockPresence is the whole-roster day page (presence board + admin
	// list share it — cross-surface parity by construction).
	ListClockPresence(ctx context.Context, params ClockPresenceParams) (ClockPresencePage, error)
	// ClockPersonDayDetail is one member's day in full: entry, events, recent.
	ClockPersonDayDetail(ctx context.Context, tenantID, workforceMemberID, businessDate string) (ClockPersonDay, error)
	// ClockEntryDetail resolves an entry id to its day detail (admin drawer).
	ClockEntryDetail(ctx context.Context, tenantID, clockEntryID string) (ClockPersonDay, error)
}

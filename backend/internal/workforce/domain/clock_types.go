package domain

// Clock In / Clock Out (docs/features/clock-in-out/plan.md, maintainer
// decisions 2026-08-27/28). Every person with an app login clocks in at day
// start and out at day end; the backend owns the one number the feature exists
// for — worked minutes per IST business day. Punches capture location, device
// identity, and a mock-location verdict; a mock-provided punch is refused on
// the client AND here (422 mock_location_detected), while a punch with merely
// missing GPS is accepted and flagged.

// ClockLocation is the location capture of one punch. Status mirrors the
// proof-capture provider's vocabulary (AppProofLocationProvider on Android).
type ClockLocation struct {
	// Status: captured | permission_missing | unavailable.
	Status    string   `json:"status"`
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	AccuracyM *float64 `json:"gps_accuracy_m,omitempty"`
	Address   string   `json:"address,omitempty"`
}

// ClockIntegrity is the device-honesty verdict gathered at punch time. A punch
// arriving with MockLocation=true or any MockProviderPackages is REFUSED
// server-side regardless of what the client decided — a tampered client must
// not be able to skip the gate.
type ClockIntegrity struct {
	MockLocation            bool     `json:"mock_location"`
	MockProviderPackages    []string `json:"mock_provider_packages,omitempty"`
	DeveloperOptionsEnabled *bool    `json:"developer_options_enabled,omitempty"`
}

// ClockPunchRequest is the body of POST /app/clock/in and /app/clock/out.
// Device identity (model, versions, device id) is NOT in the body — it comes
// from the X-GoatOS-* request headers every Android call already carries, so a
// client cannot claim a different device than the one that spoke.
type ClockPunchRequest struct {
	// IdempotencyKey may also arrive via the Idempotency-Key header; body wins
	// when both are present so offline-queued outbox items stay self-contained.
	IdempotencyKey string `json:"idempotency_key"`
	// CapturedAt is the device clock at tap time (RFC3339). Always recorded;
	// it becomes the effective punch instant only for an offline-queued punch
	// (maintainer decision D1), where server arrival can be hours late.
	CapturedAt string `json:"captured_at"`
	// Offline marks a punch that was captured without network and drained
	// later through the outbox.
	Offline    bool           `json:"offline"`
	Location   ClockLocation  `json:"location"`
	Integrity  ClockIntegrity `json:"integrity"`
	BatteryPct *int           `json:"battery_pct,omitempty"`
	// NetworkKind is the transport at capture time when online (wifi |
	// cellular); informational.
	NetworkKind string `json:"network_kind,omitempty"`
}

// ClockFlag is one honesty chip rendered verbatim by clients.
type ClockFlag struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// ClockEventDetail is the full capture of one punch, for the admin drawer and
// the presence person-day detail.
type ClockEventDetail struct {
	ClockEventID            string   `json:"clock_event_id"`
	EventType               string   `json:"event_type"`
	BusinessDate            string   `json:"business_date"`
	CapturedAt              string   `json:"captured_at"`
	RecordedAt              string   `json:"recorded_at"`
	ClockSkewMs             *int64   `json:"clock_skew_ms,omitempty"`
	LocationStatus          string   `json:"location_status"`
	Latitude                *float64 `json:"latitude,omitempty"`
	Longitude               *float64 `json:"longitude,omitempty"`
	GpsAccuracyM            *float64 `json:"gps_accuracy_m,omitempty"`
	Address                 string   `json:"address,omitempty"`
	MockLocation            bool     `json:"mock_location"`
	DeveloperOptionsEnabled *bool    `json:"developer_options_enabled,omitempty"`
	DeviceModel             string   `json:"device_model,omitempty"`
	AppVersion              string   `json:"app_version,omitempty"`
	OSVersion               string   `json:"os_version,omitempty"`
	NetworkType             string   `json:"network_type"`
	BatteryPct              *int     `json:"battery_pct,omitempty"`
}

// ClockEntry is one person's one business day — the pairing row both surfaces
// render. Times, the hours label, and flags are backend-composed; clients
// render them verbatim (cross-surface parity rule).
type ClockEntry struct {
	ClockEntryID      string  `json:"clock_entry_id"`
	WorkforceMemberID string  `json:"workforce_member_id"`
	PersonName        string  `json:"person_name"`
	RoleHint          string  `json:"role_hint,omitempty"`
	Designation       string  `json:"designation,omitempty"`
	ParkID            *string `json:"park_id,omitempty"`
	ParkLabel         *string `json:"park_label,omitempty"`
	DepartmentLabel   *string `json:"department_label,omitempty"`
	BusinessDate      string  `json:"business_date"`
	// Status: open | closed | auto_closed. An `open` entry whose business day
	// has already ended renders as not clocked out (the flags carry it); the
	// row itself stays honest.
	Status        string  `json:"status"`
	ClockInAt     string  `json:"clock_in_at"`
	ClockInLabel  string  `json:"clock_in_label"`
	ClockOutAt    *string `json:"clock_out_at,omitempty"`
	ClockOutLabel *string `json:"clock_out_label,omitempty"`
	// WorkedMinutes is the backend-owned working-hours truth. Nil while open
	// and nil forever on an auto-closed day (no invented end time).
	WorkedMinutes *int `json:"worked_minutes,omitempty"`
	// HoursLabel is the rendered form ("9h 29m"), or the in-progress form for
	// an open entry composed at read time ("3h 40m so far").
	HoursLabel string `json:"hours_label,omitempty"`
	// LocationLabel is the clock-in punch's reverse-geocoded address; empty
	// when GPS was unavailable. DeviceLabel is "model · app version".
	LocationLabel string      `json:"location_label,omitempty"`
	DeviceLabel   string      `json:"device_label,omitempty"`
	Flags         []ClockFlag `json:"flags"`
}

// ClockStatusResponse is GET /app/clock/status — the My Clock screen state and
// the shell banner in one read.
type ClockStatusResponse struct {
	BusinessDate string `json:"business_date"`
	// State: not_clocked_in | clocked_in | clocked_out.
	State string      `json:"state"`
	Entry *ClockEntry `json:"entry,omitempty"`
	// BannerText is the backend-owned shell reminder; empty when no banner
	// should show.
	BannerText string `json:"banner_text,omitempty"`
	// PunchRefusedCopy is the farm-worded refusal template shown when the
	// CLIENT's own integrity check blocks the punch, so the phone never
	// composes that sentence itself. "%s" is replaced by the app label list.
	PunchRefusedCopy string            `json:"punch_refused_copy"`
	Copy             map[string]string `json:"copy"`
	// RecentEntries are this person's latest days, newest first (bounded).
	RecentEntries []ClockEntry `json:"recent_entries"`
	TraceID       string       `json:"trace_id"`
}

// ClockPunchResponse is the result of a punch write.
type ClockPunchResponse struct {
	Entry   ClockEntry `json:"entry"`
	TraceID string     `json:"trace_id"`
}

// ClockPresenceSummary is the whole-filter tile row of the presence board.
// Buckets are DISJOINT at person grain for the selected date: every active
// member lands in exactly one of working/clocked_out/not_clocked_in. Flagged
// OVERLAPS the first two (it counts flagged entries, not extra people) and is
// labeled as such on both surfaces.
type ClockPresenceSummary struct {
	Working      int `json:"working"`
	ClockedOut   int `json:"clocked_out"`
	NotClockedIn int `json:"not_clocked_in"`
	Flagged      int `json:"flagged"`
}

// ClockPresenceRow is one person on the presence board for the selected date.
type ClockPresenceRow struct {
	// ClockEntryID is empty for a not_clocked_in row (no entry exists).
	ClockEntryID      string  `json:"clock_entry_id,omitempty"`
	WorkforceMemberID string  `json:"workforce_member_id"`
	PersonName        string  `json:"person_name"`
	Designation       string  `json:"designation,omitempty"`
	ParkLabel         *string `json:"park_label,omitempty"`
	// Bucket: working | clocked_out | not_clocked_in.
	Bucket string `json:"bucket"`
	// TimeLabel is the backend-composed row line: "In 08:12 · 4h 05m so far",
	// "08:02 – 17:31 · 9h 29m", or "" for not clocked in.
	TimeLabel     string      `json:"time_label"`
	ClockInAt     *string     `json:"clock_in_at,omitempty"`
	ClockOutAt    *string     `json:"clock_out_at,omitempty"`
	WorkedMinutes *int        `json:"worked_minutes,omitempty"`
	Flags         []ClockFlag `json:"flags"`
}

// ClockPresenceResponse is GET /app/clock/presence (leadership Team page).
type ClockPresenceResponse struct {
	BusinessDate string               `json:"business_date"`
	IsToday      bool                 `json:"is_today"`
	Summary      ClockPresenceSummary `json:"summary"`
	Rows         []ClockPresenceRow   `json:"rows"`
	NextCursor   string               `json:"next_cursor"`
	Parks        []PeopleCatalogOption `json:"parks"`
	Designations []PeopleCatalogOption `json:"designations"`
	Copy         map[string]string    `json:"copy"`
	TraceID      string               `json:"trace_id"`
}

// ClockPersonDayResponse is the presence row drill-down: one person's selected
// day in full plus their recent days.
type ClockPersonDayResponse struct {
	PersonName   string             `json:"person_name"`
	Designation  string             `json:"designation,omitempty"`
	ParkLabel    *string            `json:"park_label,omitempty"`
	BusinessDate string             `json:"business_date"`
	Entry        *ClockEntry        `json:"entry,omitempty"`
	Events       []ClockEventDetail `json:"events"`
	RecentDays   []ClockEntry       `json:"recent_days"`
	Copy         map[string]string  `json:"copy"`
	TraceID      string             `json:"trace_id"`
}

// ClockEntriesListResponse is GET /admin/workforce/clock-entries — the
// admin-web People/HRMS Clock In / Out tab.
type ClockEntriesListResponse struct {
	Summary    ClockPresenceSummary  `json:"summary"`
	Items      []ClockEntry          `json:"items"`
	NextCursor string                `json:"next_cursor"`
	Parks      []PeopleCatalogOption `json:"parks"`
	Designations []PeopleCatalogOption `json:"designations"`
	TraceID    string                `json:"trace_id"`
}

// ClockEntryDetailResponse is the admin drawer: the paired entry plus every
// event's full capture, including refused-attempt context recorded in
// metadata.
type ClockEntryDetailResponse struct {
	Entry   ClockEntry         `json:"entry"`
	Events  []ClockEventDetail `json:"events"`
	TraceID string             `json:"trace_id"`
}

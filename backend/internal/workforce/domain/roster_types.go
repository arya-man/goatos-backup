package domain

// Position is a fixed operational/timetable seat (design doc
// docs/hr/roster-rbac-design.md S4.2): who holds it, at which center, and its
// recurring week-off day. It is the ONLY new roster table -- leave, coverage,
// and temporary execution permission all reuse existing tables (see
// roster_service.go doc comment). Distinct from HR designation grade
// (workforce_members.hr_designation_grade, informational only) and from
// department ownership (workforce_members.department_id, unchanged).
type Position struct {
	PositionID        string  `json:"position_id"`
	WorkforceMemberID string  `json:"workforce_member_id"`
	ScopeType         string  `json:"scope_type"`
	ScopeID           string  `json:"scope_id"`
	PositionCode      string  `json:"position_code"`
	PositionTier      string  `json:"position_tier"`
	IsBackupSlot      bool    `json:"is_backup_slot"`
	BackupGroupCode   *string `json:"backup_group_code"`
	WeekOffWeekday    *string `json:"week_off_weekday"`
	// VaccinationDailyAnimalCap is an optional HRMS-authored animal/day cap for vaccination
	// execution seats. Nil means use the tenant default from vaccination_capacity_config.
	VaccinationDailyAnimalCap *int    `json:"vaccination_daily_animal_cap,omitempty"`
	Status                    string  `json:"status"`
	ValidFrom                 string  `json:"valid_from"`
	ValidTo                   *string `json:"valid_to"`
	RowVersion                int     `json:"row_version"`
	CreatedAt                 string  `json:"created_at"`
	UpdatedAt                 string  `json:"updated_at"`

	// Enriched fields (read from related tables at list time)
	PersonDisplayName  *string        `json:"person_display_name,omitempty"`
	HrDesignationGrade *string        `json:"hr_designation_grade,omitempty"`
	PositionTitle      *string        `json:"position_title,omitempty"`
	CenterLabel        *string        `json:"center_label,omitempty"`
	Tier               *string        `json:"tier,omitempty"`
	WeekOff            *string        `json:"week_off,omitempty"`
	BackupGroup        *string        `json:"backup_group,omitempty"`
	Duties             []PositionDuty `json:"duties,omitempty"`
}

// PositionDuty is one row of the normalized "what work does this position do"
// model (position_module_duties, migration 000157): a module a position
// executes/manages, and the execution capability a temporary backup grant for
// it confers (capability_code, null when the module is not built yet). Read at
// enrichment time so the API can expose a position's duties alongside it.
type PositionDuty struct {
	ModuleCode     string  `json:"module_code"`
	DutyType       string  `json:"duty_type"`
	CapabilityCode *string `json:"capability_code,omitempty"`
}

// NotificationRecipient is one active, reachable device for a resolved notification recipient
// (module-duty holder, an explicit member, or a position holder). FCMToken is always non-empty here --
// resolution queries filter to devices carrying a live token (migration 000171), so a member with no
// registered FCM token simply produces no row (never a nil-token notification).
type NotificationRecipient struct {
	WorkforceMemberID string
	DeviceID          string
	FCMToken          string
}

// ShedOwnershipScope is the batch input for shed-wise vaccination owner cells. CenterID is the park/center
// scope used for the center backup fallback.
type ShedOwnershipScope struct {
	ShedID   string
	CenterID string
}

// ShedOwner is the small owner cell needed outside the workforce module.
type ShedOwner struct {
	WorkforceMemberID string
	DisplayName       string
}

// ShedOwnership is the manager/backup pair for a shed. Nil values mean the seat is unassigned.
type ShedOwnership struct {
	Manager *ShedOwner
	Backup  *ShedOwner
}

type PositionListResponse struct {
	Items   []Position `json:"items"`
	TraceID string     `json:"trace_id"`
}

type PositionResponse struct {
	Position Position `json:"position"`
	TraceID  string   `json:"trace_id"`
}

// CreatePositionRequest assigns a member to a fixed named seat at a scope.
// is_backup_slot + backup_group_code together express the generalized
// two-tier backup shape (design doc S4.3): a manager-tier covered position
// and its single center-wide Backup Manager share one backup_group_code
// (e.g. "manager_backup"); each assistant-tier parallel group (e.g. Feeding
// AM1/2/3 + Cleaning AM1/2 -> Backup AM1) shares its OWN backup_group_code.
// idempotency_key enables request-level dedup: exact replay returns the original
// result without re-running side effects (repo mandatory rule, see AGENTS.md).
type CreatePositionRequest struct {
	WorkforceMemberID string  `json:"workforce_member_id"`
	ScopeType         string  `json:"scope_type"`
	ScopeID           string  `json:"scope_id"`
	PositionCode      string  `json:"position_code"`
	PositionTier      string  `json:"position_tier"`
	IsBackupSlot      bool    `json:"is_backup_slot"`
	BackupGroupCode   *string `json:"backup_group_code"`
	WeekOffWeekday    *string `json:"week_off_weekday"`
	ValidTo           *string `json:"valid_to"`
	IdempotencyKey    *string `json:"idempotency_key"`
}

// UpdatePositionRequest edits an existing seat's attributes IN PLACE (never its
// holder -- reassigning a holder is a replace via CreatePositionRequest).
// Optional fields use pointers so a nil pointer means "leave unchanged" while a
// non-nil pointer to "" clears a nullable column (backup_group_code,
// week_off_weekday, valid_to). RowVersion is the optimistic-lock guard;
// idempotency_key enables request-level dedup.
type UpdatePositionRequest struct {
	RowVersion                int     `json:"row_version"`
	PositionTier              *string `json:"position_tier"`
	BackupGroupCode           *string `json:"backup_group_code"`
	WeekOffWeekday            *string `json:"week_off_weekday"`
	VaccinationDailyAnimalCap *int    `json:"vaccination_daily_animal_cap"`
	ValidTo                   *string `json:"valid_to"`
	Status                    *string `json:"status"`
	IdempotencyKey            *string `json:"idempotency_key"`
}

// UpsertBackupConfigRequest assigns/replaces the holder of a backup-slot seat
// for a backup group at a scope. It is a thin, intent-revealing wrapper over a
// CreatePositionRequest with is_backup_slot forced true (design doc S4.3): the
// backup config surfaced by ListBackupConfig IS the set of is_backup_slot=true
// positions, so configuring one is assigning that seat's holder.
type UpsertBackupConfigRequest struct {
	WorkforceMemberID  string  `json:"workforce_member_id"`
	ScopeType          string  `json:"scope_type"`
	ScopeID            string  `json:"scope_id"`
	BackupGroupCode    string  `json:"backup_group_code"`
	BackupPositionCode string  `json:"backup_position_code"`
	PositionTier       string  `json:"position_tier"`
	WeekOffWeekday     *string `json:"week_off_weekday"`
	IdempotencyKey     *string `json:"idempotency_key"`
}

// ImportPositionsRequest bulk-creates/replaces seats from a parsed timetable
// import. Each row is a normal CreatePositionRequest applied with the same
// validation + replace semantics; per-row outcomes are returned so a
// partially-valid import still lands its valid rows.
type ImportPositionsRequest struct {
	Rows           []CreatePositionRequest `json:"rows"`
	IdempotencyKey *string                 `json:"idempotency_key"`
}

// ImportPositionResult is the outcome of a single import row. Status is
// "created" (position_id set) or "error" (error_code/error_message set).
type ImportPositionResult struct {
	Index        int     `json:"index"`
	Status       string  `json:"status"`
	PositionID   *string `json:"position_id,omitempty"`
	ErrorCode    *string `json:"error_code,omitempty"`
	ErrorMessage *string `json:"error_message,omitempty"`
}

type ImportPositionsResponse struct {
	Imported int                    `json:"imported"`
	Failed   int                    `json:"failed"`
	Results  []ImportPositionResult `json:"results"`
	TraceID  string                 `json:"trace_id"`
}

// PositionProfile is the person/position drawer read model: the enriched seat
// plus its holder's currently-active coverage window (nil when the holder is
// present). No reports_to pointer is included -- the roster schema has no
// per-position reporting hierarchy column, so it is honestly omitted rather
// than invented.
type PositionProfile struct {
	Position       Position  `json:"position"`
	ActiveCoverage *Coverage `json:"active_coverage,omitempty"`
}

type PositionProfileResponse struct {
	Profile PositionProfile `json:"profile"`
	TraceID string          `json:"trace_id"`
}

// StaffLeave is the HR roster leave/absence concept -- backed entirely by the
// existing workforce_absences table (migration 000050), not a new one.
// ReplacementMemberID is that table's existing column, filled automatically
// by the approval service from effective_backup (S4.5), never
// operator-chosen. Status 'escalation_required' means no backup could be
// resolved (or the backup was itself unavailable) -- see S4.7.
type StaffLeave struct {
	AbsenceID              string  `json:"absence_id"`
	WorkforceMemberID      string  `json:"workforce_member_id"`
	ScopeType              string  `json:"scope_type"`
	ScopeID                string  `json:"scope_id"`
	ReasonCode             string  `json:"reason_code"`
	Status                 string  `json:"status"`
	StartsAt               string  `json:"starts_at"`
	EndsAt                 string  `json:"ends_at"`
	ReplacementMemberID    *string `json:"replacement_member_id"`
	CoverageOverrideReason *string `json:"coverage_override_reason"`
	CreatedBy              *string `json:"created_by"`
	ApprovedBy             *string `json:"approved_by"`
	RowVersion             int     `json:"row_version"`
	CreatedAt              string  `json:"created_at"`
	UpdatedAt              string  `json:"updated_at"`
}

type StaffLeaveListResponse struct {
	Items   []StaffLeave `json:"items"`
	TraceID string       `json:"trace_id"`
}

type StaffLeaveResponse struct {
	Leave   StaffLeave `json:"leave"`
	TraceID string     `json:"trace_id"`
}

type ApplyStaffLeaveRequest struct {
	WorkforceMemberID string  `json:"workforce_member_id"`
	ScopeType         string  `json:"scope_type"`
	ScopeID           string  `json:"scope_id"`
	ReasonCode        string  `json:"reason_code"`
	StartsOn          string  `json:"starts_on"`
	EndsOn            string  `json:"ends_on"`
	IdempotencyKey    *string `json:"idempotency_key"`
}

type ApproveStaffLeaveRequest struct {
	RowVersion     int     `json:"row_version"`
	IdempotencyKey *string `json:"idempotency_key"`
}

// ResolveLeaveCoverageRequest re-runs (or CEO-overrides) the effective_backup
// resolution for an already-approved/escalated absence. ReplacementMemberID
// nil re-runs auto-resolution; a non-nil value is a CEO override and MUST
// equal the position's current effective_backup holder (design doc S5.4: the
// picker "never a free list of all managers/assistants") -- the service
// rejects any other value. OverrideReason is recorded on
// coverage_override_reason for audit. idempotency_key enables request-level dedup.
type ResolveLeaveCoverageRequest struct {
	ReplacementMemberID *string `json:"replacement_member_id"`
	OverrideReason      *string `json:"override_reason"`
	IdempotencyKey      *string `json:"idempotency_key"`
}

// VaccinationOwner answers "who owns vaccination work for scope X on date D"
// (design doc S4.8's effective_owner worked example): the permanent position
// holder, the resolved ad-hoc-leave replacement, or the recurring week-off
// backup. EscalationParkHeadMemberID is set only when Reason is non-nil, so a
// caller always knows who to escalate to.
type VaccinationOwner struct {
	ScopeType                  string  `json:"scope_type"`
	ScopeID                    string  `json:"scope_id"`
	Date                       string  `json:"date"`
	PositionID                 *string `json:"position_id"`
	OwnerWorkforceMemberID     *string `json:"owner_workforce_member_id"`
	OwnerSource                string  `json:"owner_source"`
	Reason                     *string `json:"reason"`
	EscalationParkHeadMemberID *string `json:"escalation_park_head_member_id"`
}

type VaccinationOwnerResponse struct {
	Owner   VaccinationOwner `json:"owner"`
	TraceID string           `json:"trace_id"`
}

// Owner source / reason vocabulary (design doc S4.8).
const (
	OwnerSourcePositionHolder = "position_holder"
	OwnerSourceReplacement    = "replacement"
	OwnerSourceWeekOffBackup  = "week_off_backup"
	OwnerSourceNone           = "none"

	OwnerReasonNoHolder           = "no_holder_assigned"
	OwnerReasonEscalated          = "escalation_required"
	OwnerReasonNoBackupConfigured = "no_backup_configured"
)

// Leave status vocabulary. reported/approved/rejected/canceled already exist
// (migration 000050); escalation_required is the one new value (000151).
const (
	LeaveStatusReported           = "reported"
	LeaveStatusApproved           = "approved"
	LeaveStatusRejected           = "rejected"
	LeaveStatusCanceled           = "canceled"
	LeaveStatusEscalationRequired = "escalation_required"
)

// BackupConfig represents the backup configuration for a position group at a center.
type BackupConfig struct {
	CenterID             string  `json:"center_id"`
	CenterLabel          *string `json:"center_label,omitempty"`
	BackupGroupCode      string  `json:"backup_group_code"`
	BackupPositionCode   string  `json:"backup_position_code"`
	BackupPositionTitle  *string `json:"backup_position_title,omitempty"`
	ConfiguredHolderID   *string `json:"configured_holder_id,omitempty"`
	ConfiguredHolderName *string `json:"configured_holder_name,omitempty"`
	Status               string  `json:"status"`
}

type BackupConfigListResponse struct {
	Items   []BackupConfig `json:"items"`
	TraceID string         `json:"trace_id"`
}

// Coverage represents an active or historical coverage (leave, week-off, or escalation).
type Coverage struct {
	PositionID           string  `json:"position_id"`
	CoveredPositionCode  string  `json:"covered_position_code"`
	CoveredPositionTitle *string `json:"covered_position_title,omitempty"`
	CoveringMemberID     *string `json:"covering_member_id,omitempty"`
	CoveringMemberName   *string `json:"covering_member_name,omitempty"`
	StartDate            string  `json:"start_date"`
	EndDate              string  `json:"end_date"`
	Source               string  `json:"source"` // "leave", "week_off", "escalation"
	EscalationState      *string `json:"escalation_state,omitempty"`
	Status               string  `json:"status"`
}

type CoverageListResponse struct {
	Items   []Coverage `json:"items"`
	TraceID string     `json:"trace_id"`
}

// MyCoverage represents the current coverage state for the authenticated operator.
type MyCoverage struct {
	HasCoverage           bool    `json:"has_coverage"`
	PositionID            *string `json:"position_id,omitempty"`
	CoveringPersonName    *string `json:"covering_person_name,omitempty"`
	CoveringPositionTitle *string `json:"covering_position_title,omitempty"`
	WindowStart           *string `json:"window_start,omitempty"` // RFC3339
	WindowEnd             *string `json:"window_end,omitempty"`   // RFC3339
	BannerText            *string `json:"banner_text,omitempty"`
	Timezone              string  `json:"timezone"` // Always "Asia/Kolkata"
}

type MyCoverageResponse struct {
	Coverage MyCoverage `json:"coverage"`
	TraceID  string     `json:"trace_id"`
}

// Position tiers (design doc S4.2).
const (
	PositionTierAssistant = "assistant"
	PositionTierManager   = "manager"
	PositionTierHead      = "head"
	PositionTierDirector  = "director"
	PositionTierCXO       = "cxo"
)

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
	Status            string  `json:"status"`
	ValidFrom         string  `json:"valid_from"`
	ValidTo           *string `json:"valid_to"`
	RowVersion        int     `json:"row_version"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
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

// Position tiers (design doc S4.2).
const (
	PositionTierAssistant = "assistant"
	PositionTierManager   = "manager"
	PositionTierHead      = "head"
	PositionTierDirector  = "director"
	PositionTierCXO       = "cxo"
)

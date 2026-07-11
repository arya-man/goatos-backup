// Package domain holds the reusable process-integrity read model.
package domain

import "time"

const (
	CategoryVaccination   = "vaccination"
	CategoryFeedDirection = "feed_direction"
	SourceAPI             = "api"
)

type WorkState string

const (
	WorkStateScheduled           WorkState = "scheduled"
	WorkStateDue                 WorkState = "due"
	WorkStateOverdue             WorkState = "overdue"
	WorkStateInProgress          WorkState = "in_progress"
	WorkStateProofPending        WorkState = "proof_pending"
	WorkStateVerificationPending WorkState = "verification_pending"
	WorkStateRejected            WorkState = "rejected"
	WorkStateDeferred            WorkState = "deferred"
	WorkStateMissed              WorkState = "missed"
	WorkStateBlocked             WorkState = "blocked"
	WorkStateCompleted           WorkState = "completed"
)

type Severity string

const (
	SeverityOK     Severity = "ok"
	SeverityWatch  Severity = "watch"
	SeverityAtRisk Severity = "at_risk"
	SeverityBroken Severity = "broken"
)

type OwnerState string

const (
	OwnerStateAssigned OwnerState = "assigned"
	OwnerStateMissing  OwnerState = "missing"
)

type SOPState string

const (
	SOPStateNotStarted SOPState = "not_started"
	SOPStateInProgress SOPState = "in_progress"
	SOPStateSubmitted  SOPState = "submitted"
	SOPStateAccepted   SOPState = "accepted"
	SOPStateRework     SOPState = "rework"
)

type ProofState string

const (
	ProofStateNotRequired ProofState = "not_required"
	ProofStateMissing     ProofState = "missing"
	ProofStateUploaded    ProofState = "uploaded"
	ProofStateAccepted    ProofState = "accepted"
	ProofStateRejected    ProofState = "rejected"
)

type VerificationState string

const (
	VerificationStateNotReady VerificationState = "not_ready"
	VerificationStatePending  VerificationState = "pending"
	VerificationStateAccepted VerificationState = "accepted"
	VerificationStateRejected VerificationState = "rejected"
)

type Owner struct {
	OperatorID          *string `json:"operator_id,omitempty"`
	OperatorName        *string `json:"operator_name,omitempty"`
	ParkHeadID          *string `json:"park_head_id,omitempty"`
	ParkHeadName        *string `json:"park_head_name,omitempty"`
	VerifierID          *string `json:"verifier_id,omitempty"`
	VerifierName        *string `json:"verifier_name,omitempty"`
	EscalationOwnerID   *string `json:"escalation_owner_id,omitempty"`
	EscalationOwnerName *string `json:"escalation_owner_name,omitempty"`
}

type Evidence struct {
	ProofIDs              []string   `json:"proof_ids"`
	EvidenceCount         int        `json:"evidence_count"`
	LatestEvidenceAt      *time.Time `json:"latest_evidence_at,omitempty"`
	LatestRejectionReason *string    `json:"latest_rejection_reason,omitempty"`
	AuditRef              *string    `json:"audit_ref,omitempty"`
}

// Row is the generic process-integrity shape. The current API lens exposes only
// category=vaccination, but the fields are named for future module reuse.
type Row struct {
	TenantID string `json:"-"`

	Category        string  `json:"category"`
	RowID           string  `json:"row_id"`
	ProcessKey      string  `json:"process_key"`
	ObligationID    string  `json:"obligation_id"`
	BatchID         *string `json:"batch_id,omitempty"`
	SOPTaskID       *string `json:"sop_task_id,omitempty"`
	SOPTaskVersion  *int32  `json:"sop_task_row_version,omitempty"`
	SOPSubmissionID *string `json:"sop_submission_id,omitempty"`
	CompletionID    *string `json:"completion_id,omitempty"`

	ParkID      string  `json:"park_id"`
	ParkName    string  `json:"park_name"`
	ShedID      string  `json:"shed_id"`
	ShedName    string  `json:"shed_name"`
	CohortID    *string `json:"cohort_id,omitempty"`
	GoatID      *string `json:"goat_id,omitempty"`
	AnimalStage string  `json:"animal_stage"`

	ProtocolID        string  `json:"protocol_id"`
	ProtocolVersionID string  `json:"protocol_version_id"`
	RuleID            string  `json:"rule_id"`
	ProtocolName      string  `json:"protocol_name"`
	DoseCode          string  `json:"dose_code"`
	DriveName         *string `json:"drive_name,omitempty"`
	SOPVersionID      *string `json:"sop_version_id,omitempty"`
	ProofPolicy       string  `json:"proof_policy"`

	DueAt         time.Time  `json:"due_at"`
	WindowStart   *time.Time `json:"window_start,omitempty"`
	WindowEnd     *time.Time `json:"window_end,omitempty"`
	ExpectedCount int        `json:"expected_count"`

	ObligationStatus  string            `json:"obligation_status"`
	BatchStatus       *string           `json:"batch_status,omitempty"`
	SOPTaskState      SOPState          `json:"sop_task_state"`
	SubmissionState   *string           `json:"submission_state,omitempty"`
	ProofState        ProofState        `json:"proof_state"`
	VerificationState VerificationState `json:"verification_state"`
	CompletionState   *string           `json:"completion_state,omitempty"`
	CompletedCount    int               `json:"completed_count"`
	ProofCount        int               `json:"proof_count"`
	RejectedCount     int               `json:"rejected_count"`
	DeferredCount     int               `json:"deferred_count"`

	WorkState     WorkState  `json:"work_state"`
	GapType       string     `json:"gap_type"`
	Severity      Severity   `json:"severity"`
	BlockerReason *string    `json:"blocker_reason,omitempty"`
	OwnerState    OwnerState `json:"owner_state"`
	NextAction    string     `json:"next_action"`
	ProcessIntact bool       `json:"process_intact"`
	Owner         Owner      `json:"owner"`
	Evidence      Evidence   `json:"evidence"`
}

type Query struct {
	TenantID           string
	Category           *string
	ParkID             *string
	ShedID             *string
	WorkState          *WorkState
	Severity           *Severity
	OwnerID            *string
	ProtocolVersionID  *string
	RowID              *string
	DueAfter           *time.Time
	DueBefore          time.Time
	AsOf               time.Time
	Limit              int
	Offset             int
	Cursor             *Cursor
	IncludeCompleted   bool
	OnlyBrokenOrAtRisk bool
	// IncludeAdherenceSummary asks the repository to compute Protocol Adherence
	// KPIs over the full filtered set, not only the current page.
	IncludeAdherenceSummary bool
}

type ProjectionRecomputeRequest struct {
	TenantID string
	AsOf     time.Time
}

type ProjectionRecomputeResult struct {
	TenantID           string
	ProjectionVersion  int64
	ProjectedAt        time.Time
	AsOf               time.Time
	Rows               int64
	CountsByWorkState  []CountByWorkState
	ProjectionFreshFor time.Duration
}

type Cursor struct {
	SortPriority int
	DueAt        time.Time
	RowID        string
}

type CountByWorkState struct {
	WorkState WorkState `json:"work_state"`
	Count     int64     `json:"count"`
}

type ListResult struct {
	Rows              []Row              `json:"rows"`
	CountsByWorkState []CountByWorkState `json:"counts_by_work_state"`
	TotalCount        int64              `json:"total_count"`
	AdherenceSummary  AdherenceSummary   `json:"-"`
	NextCursor        *string            `json:"next_cursor,omitempty"`
}

type ActionCenterResponse struct {
	Source            string             `json:"source"`
	Items             []Row              `json:"items"`
	CountsByWorkState []CountByWorkState `json:"counts_by_work_state"`
	TotalCount        int64              `json:"total_count"`
	NextCursor        *string            `json:"next_cursor,omitempty"`
}

type ActionCenterCountsResponse struct {
	Source            string             `json:"source"`
	CountsByWorkState []CountByWorkState `json:"counts_by_work_state"`
	TotalCount        int64              `json:"total_count"`
}

type AdherenceSummary struct {
	ExpectedCount      int     `json:"expected_count"`
	CompletedCount     int     `json:"completed_count"`
	OpenGapCount       int     `json:"open_gap_count"`
	DeferredCount      int     `json:"deferred_count"`
	ProcessIntactCount int     `json:"process_intact_count"`
	AdherencePercent   float64 `json:"adherence_percent"`
}

type AdherenceRow struct {
	RowID      string    `json:"row_id"`
	Expected   string    `json:"expected"`
	Actual     string    `json:"actual"`
	Gap        string    `json:"gap"`
	Severity   Severity  `json:"severity"`
	Owner      Owner     `json:"owner"`
	NextAction string    `json:"next_action"`
	Evidence   Evidence  `json:"evidence"`
	WorkState  WorkState `json:"work_state"`
}

type ProtocolAdherenceResponse struct {
	Source     string           `json:"source"`
	Summary    AdherenceSummary `json:"summary"`
	Rows       []AdherenceRow   `json:"rows"`
	TotalCount int64            `json:"total_count"`
	NextCursor *string          `json:"next_cursor,omitempty"`
}

type ControlTowerSummary struct {
	ProcessIntact       bool `json:"process_intact"`
	CriticalCount       int  `json:"critical_count"`
	WarningCount        int  `json:"warning_count"`
	OpenGapCount        int  `json:"open_gap_count"`
	VerificationBacklog int  `json:"verification_backlog"`
	ConfigOrSOPBlockers int  `json:"config_or_sop_blockers"`
}

type ControlTowerAlert struct {
	RowID        string    `json:"row_id"`
	Severity     Severity  `json:"severity"`
	WorkState    WorkState `json:"work_state"`
	Title        string    `json:"title"`
	Detail       string    `json:"detail"`
	ParkID       string    `json:"park_id"`
	ParkName     string    `json:"park_name"`
	ShedID       string    `json:"shed_id"`
	ShedName     string    `json:"shed_name"`
	DriveName    *string   `json:"drive_name,omitempty"`
	Owner        Owner     `json:"owner"`
	NextAction   string    `json:"next_action"`
	EvidenceLink string    `json:"evidence_link"`
	// ObligationID lets the mobile app target a real obligation for the "reschedule this obligation"
	// write path (POST /app/vaccination/obligations/{obligation_id}/reschedule). Propagated straight
	// from Row.ObligationID, which is always populated (selected as a non-nullable oi.obligation_id
	// column for every process-integrity category, not only vaccination).
	ObligationID string `json:"obligation_id"`
}

type ControlTowerResponse struct {
	Source     string              `json:"source"`
	Summary    ControlTowerSummary `json:"summary"`
	Alerts     []ControlTowerAlert `json:"alerts"`
	TotalCount int64               `json:"total_count"`
	NextCursor *string             `json:"next_cursor,omitempty"`
}

type WorkflowNode struct {
	Key       string     `json:"key"`
	Label     string     `json:"label"`
	State     string     `json:"state"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
	Actor     *string    `json:"actor,omitempty"`
	Owner     *string    `json:"owner,omitempty"`
	Evidence  *string    `json:"evidence,omitempty"`
	Blocker   *string    `json:"blocker,omitempty"`
}

type WorkflowDrilldownResponse struct {
	Source string         `json:"source"`
	Row    Row            `json:"row"`
	Nodes  []WorkflowNode `json:"nodes"`
}

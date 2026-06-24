// Package domain holds vaccination-execution read-model types.
package domain

import "time"

const SourceAPI = "api"

type WorkState string

const (
	WorkStateDue                 WorkState = "due"
	WorkStateOverdue             WorkState = "overdue"
	WorkStateScheduled           WorkState = "scheduled"
	WorkStateInProgress          WorkState = "in_progress"
	WorkStateProofPending        WorkState = "proof_pending"
	WorkStateVerificationPending WorkState = "verification_pending"
	WorkStateRejected            WorkState = "rejected"
	WorkStateDeferred            WorkState = "deferred"
	WorkStateBlocked             WorkState = "blocked"
	WorkStateOwnerMissing        WorkState = "owner_missing"
	WorkStateCompleted           WorkState = "completed"
)

type Severity string

const (
	SeverityOK     Severity = "ok"
	SeverityWatch  Severity = "watch"
	SeverityAtRisk Severity = "at_risk"
	SeverityBroken Severity = "broken"
)

type SOPStatus string

const (
	SOPStatusNotStarted SOPStatus = "not_started"
	SOPStatusInProgress SOPStatus = "in_progress"
	SOPStatusSubmitted  SOPStatus = "submitted"
	SOPStatusAccepted   SOPStatus = "accepted"
	SOPStatusRework     SOPStatus = "rework"
)

type ProofStatus string

const (
	ProofStatusNotRequired ProofStatus = "not_required"
	ProofStatusMissing     ProofStatus = "missing"
	ProofStatusUploaded    ProofStatus = "uploaded"
	ProofStatusRejected    ProofStatus = "rejected"
	ProofStatusAccepted    ProofStatus = "accepted"
)

type VerificationStatus string

const (
	VerificationStatusNotReady VerificationStatus = "not_ready"
	VerificationStatusPending  VerificationStatus = "pending"
	VerificationStatusVerified VerificationStatus = "verified"
	VerificationStatusRejected VerificationStatus = "rejected"
)

type Owner struct {
	OperatorName *string `json:"operatorName,omitempty"`
	ParkHeadName *string `json:"parkHeadName,omitempty"`
	VerifierName *string `json:"verifierName,omitempty"`
}

type ExecutionRow struct {
	ParkID             string             `json:"parkId"`
	ParkName           string             `json:"parkName"`
	ShedID             string             `json:"shedId"`
	ShedName           string             `json:"shedName"`
	AnimalStage        string             `json:"animalStage"`
	DriveID            *string            `json:"driveId,omitempty"`
	DriveName          *string            `json:"driveName,omitempty"`
	DueDate            *string            `json:"dueDate,omitempty"`
	WorkState          WorkState          `json:"workState"`
	Severity           Severity           `json:"severity"`
	Owner              *Owner             `json:"owner,omitempty"`
	BlockerReason      *string            `json:"blockerReason,omitempty"`
	SOPStatus          SOPStatus          `json:"sopStatus"`
	ProofStatus        ProofStatus        `json:"proofStatus"`
	VerificationStatus VerificationStatus `json:"verificationStatus"`
	NextAction         string             `json:"nextAction"`
}

type ExecutionResponse struct {
	Source string         `json:"source"`
	Rows   []ExecutionRow `json:"rows"`
}

type DriveSummary struct {
	DriveID   *string   `json:"driveId,omitempty"`
	DriveName *string   `json:"driveName,omitempty"`
	WorkState WorkState `json:"workState"`
	Severity  Severity  `json:"severity"`
}

type ShedDrilldownSummary struct {
	Total               int `json:"total"`
	Due                 int `json:"due"`
	Overdue             int `json:"overdue"`
	ProofPending        int `json:"proofPending"`
	VerificationPending int `json:"verificationPending"`
	Rejected            int `json:"rejected"`
	Deferred            int `json:"deferred"`
	Blocked             int `json:"blocked"`
	OwnerMissing        int `json:"ownerMissing"`
	Completed           int `json:"completed"`
}

type ShedDrilldown struct {
	ParkID       string               `json:"parkId"`
	ParkName     string               `json:"parkName"`
	ShedID       string               `json:"shedId"`
	ShedName     string               `json:"shedName"`
	AnimalStages []string             `json:"animalStages"`
	Drives       []DriveSummary       `json:"drives"`
	Rows         []ExecutionRow       `json:"rows"`
	Summary      ShedDrilldownSummary `json:"summary"`
}

type ExecutionQuery struct {
	TenantID  string
	ParkID    *string
	ShedID    *string
	WorkState *WorkState
	AsOf      time.Time
	DueBefore time.Time
	Limit     int
}

// ---- Vaccination operations read model (cohort × protocol matrix + per-cohort detail) ----
// Source-backed view for the /vaccination screen: protocols (vaccine columns), cohorts (rows), and a cell
// per cohort × protocol. last_dose is the latest ACCEPTED administered_at for that cohort × protocol
// (deterministic "last dose"); next_due is the earliest open obligation due date.

type OperationsProtocol struct {
	ProtocolID string `json:"protocolId"`
	Name       string `json:"name"`
}

// OperationsCounts breaks a cohort × protocol cell (or a cohort rollup) into its obligation/completion
// tallies so the UI can show proof / verification / rework counts honestly. proofPending = completions
// recorded and awaiting verification; rejected = rework (rejected completions); accepted = verified doses.
// All counts are computed as-of the OperationsQuery.AsOf instant (events after as_of do not count).
type OperationsCounts struct {
	Overdue      int `json:"overdue"`
	Due          int `json:"due"`
	InProgress   int `json:"inProgress"`
	Scheduled    int `json:"scheduled"`
	Deferred     int `json:"deferred"`
	Accepted     int `json:"accepted"`
	ProofPending int `json:"proofPending"`
	Rejected     int `json:"rejected"`
	Total        int `json:"total"`
}

type OperationsCell struct {
	ProtocolID string           `json:"protocolId"`
	WorkState  WorkState        `json:"workState"`
	LastDose   *time.Time       `json:"lastDose,omitempty"`
	NextDue    *time.Time       `json:"nextDue,omitempty"`
	Counts     OperationsCounts `json:"counts"`
}

type OperationsCohort struct {
	ParkID    string           `json:"parkId"`
	ParkName  string           `json:"parkName"`
	ShedID    string           `json:"shedId"`
	ShedName  string           `json:"shedName"`
	Stage     string           `json:"stage"`
	AgeBand   *string          `json:"ageBand,omitempty"`
	Animals   int              `json:"animals"`
	LastDose  *time.Time       `json:"lastDose,omitempty"`
	NextDue   *time.Time       `json:"nextDue,omitempty"`
	WorkState WorkState        `json:"workState"`
	Counts    OperationsCounts `json:"counts"`
	Cells     []OperationsCell `json:"cells"`
}

type OperationsResponse struct {
	Source    string               `json:"source"`
	Protocols []OperationsProtocol `json:"protocols"`
	Cohorts   []OperationsCohort   `json:"cohorts"`
}

// OperationsRow is one cohort × protocol group straight from SQL; the service rolls these up into cohorts.
type OperationsRow struct {
	ParkID            string
	ParkName          string
	ShedID            string
	ShedName          string
	Stage             string
	AgeBand           *string
	ProtocolID        string
	ProtocolName      string
	Animals           int
	NextDue           *time.Time
	LastDose          *time.Time
	OverdueCount      int
	DueCount          int
	InProgressCount   int
	ScheduledCount    int
	DeferredCount     int
	AcceptedCount     int
	ProofPendingCount int
	RejectedCount     int
	TotalCount        int
}

type OperationsQuery struct {
	TenantID  string
	ParkID    *string
	AsOf      time.Time
	DueBefore time.Time
	Limit     int
}

type ExecutionProjection struct {
	ParkID               string
	ParkName             string
	ShedID               string
	ShedName             string
	AnimalStage          string
	BatchID              *string
	ProtocolName         string
	DoseCode             string
	DueAt                *time.Time
	ObligationCount      int
	ScheduledCount       int
	DueCount             int
	InProgressCount      int
	CompletedCount       int
	MissedCount          int
	DeferredCount        int
	CanceledCount        int
	CompletionRecorded   int
	CompletionAccepted   int
	CompletionRejected   int
	CompletionReversed   int
	BatchStatus          *string
	TaskState            *string
	OperatorName         *string
	ParkHeadName         *string
	VerifierName         *string
	UsableForVaccination bool
	IsQuarantine         bool
	IsICU                bool
	HealthDeferredCount  int
}

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
	WorkStateMissed              WorkState = "missed"
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
	ObligationID       *string            `json:"obligationId,omitempty"`
	BatchID            *string            `json:"batchId,omitempty"`
	SOPTaskID          *string            `json:"sopTaskId,omitempty"`
	SOPVersionID       *string            `json:"sopVersionId,omitempty"`
	SOPTaskRowVersion  *int32             `json:"sopTaskRowVersion,omitempty"`
	CompletionID       *string            `json:"completionId,omitempty"`
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
	Missed              int `json:"missed"`
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
	Missed       int `json:"missed"`
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
	MissedCount       int
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

// ScanRosterRow represents a single per-animal vaccination obligation for mobile scan screen.
// primaryTag and secondaryTag are RFID identifiers; vaccineLabel is the vaccine name and schedule position.
type ScanRosterRow struct {
	PrimaryTag   string  `json:"primaryTag"`
	SecondaryTag *string `json:"secondaryTag,omitempty"`
	VaccineLabel string  `json:"vaccineLabel"`
	Status       string  `json:"status"`
	ObligationID string  `json:"obligationId"`
}

type ScanRosterQuery struct {
	TenantID string
	ShedID   string
	Limit    int
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
	ObligationID         *string
	SOPTaskID            *string
	SOPVersionID         *string
	SOPTaskRowVersion    *int32
	CompletionID         *string
}

// ---- Vaccination gaps read model (animals excluded from the coverage denominator because their
// identity data is incomplete: no date of birth / no breed on record). Backs the mobile "Data gaps"
// overlay (Overlays.kt DataGapsSheet TODO(backend): GET gaps?scope_token=<token>). Scoped by tenant +
// optional park (the same park-scope mechanism /vaccination/execution and /vaccination/operations
// already use), and paginated by a goat_id keyset cursor so a park with many gapped animals never
// forces an unbounded full-herd scan. ----

// GapReasonCode enumerates the real, DB-backed reasons a live goat is excluded. Both are derived
// directly from nullable goats columns (dob — the canonical birth date backfilled from approx_dob in
// migration 000070 and used by the actual vaccination generation age-eligibility query — and
// breed/breed_id) that the vaccination generation engine's eligibility selectors (age_band, breed)
// require to match a protocol rule; there is no "missing weight" reason here because this schema has
// no live per-goat weight column yet.
type GapReasonCode string

const (
	GapReasonNoDateOfBirth   GapReasonCode = "no_date_of_birth"
	GapReasonNoBreedOnRecord GapReasonCode = "no_breed_on_record"
)

// GapProjectionRow is one excluded animal straight from SQL, before the service layer attaches a
// human-readable reason label.
type GapProjectionRow struct {
	GoatID     string
	DisplayID  string
	ParkID     string
	ParkName   string
	ShedID     *string
	ShedName   *string
	ReasonCode GapReasonCode
}

// GapReasonCount is a cheap scoped aggregate (GROUP BY reason, at most two groups) — never a raw
// full-herd COUNT(*).
type GapReasonCount struct {
	ReasonCode GapReasonCode
	Count      int
}

type GapRow struct {
	GoatID      string        `json:"goatId"`
	DisplayID   string        `json:"displayId"`
	ParkID      string        `json:"parkId"`
	ParkName    string        `json:"parkName"`
	ShedID      *string       `json:"shedId,omitempty"`
	ShedName    *string       `json:"shedName,omitempty"`
	ReasonCode  GapReasonCode `json:"reasonCode"`
	ReasonLabel string        `json:"reasonLabel"`
}

type GapReasonSummary struct {
	ReasonCode  GapReasonCode `json:"reasonCode"`
	ReasonLabel string        `json:"reasonLabel"`
	Count       int           `json:"count"`
}

type GapsQuery struct {
	TenantID string
	ParkID   *string
	Cursor   *string // last goat_id seen (exclusive); nil/empty means start from the beginning.
	Limit    int
}

type GapsResponse struct {
	Source     string             `json:"source"`
	ParkID     *string            `json:"parkId,omitempty"`
	Reasons    []GapReasonSummary `json:"reasons"`
	Rows       []GapRow           `json:"rows"`
	NextCursor *string            `json:"nextCursor,omitempty"`
}

// ---- Vaccination coverage rollup (per-vaccine given-count + coverage % for a scope). Backs the mobile
// "Doses given" overlay (Overlays.kt DosesGivenSheet TODO(backend): per-vaccine given + coverage % from
// the scope-token rollup). Reuses the exact same indexed cohort×protocol rows VaccinationOperations
// already reads (ports.Repository.VaccinationOperations) and re-aggregates them by protocol only, so
// this introduces no new hot-table query. ----

type CoverageProtocol struct {
	ProtocolID      string `json:"protocolId"`
	Name            string `json:"name"`
	GivenCount      int    `json:"givenCount"`
	TotalCount      int    `json:"totalCount"`
	CoveragePercent int    `json:"coveragePercent"`
}

type CoverageResponse struct {
	Source    string             `json:"source"`
	ParkID    *string            `json:"parkId,omitempty"`
	Protocols []CoverageProtocol `json:"protocols"`
}

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

type ProjectionFreshness struct {
	ProjectionVersion int64     `json:"projectionVersion"`
	ProjectedAt       time.Time `json:"projectedAt"`
	AsOf              time.Time `json:"asOf"`
	Status            string    `json:"status"`
	LagSeconds        int64     `json:"lagSeconds"`
	ServingState      string    `json:"servingState,omitempty"`
	Stale             bool      `json:"stale,omitempty"`
	RebuildRequired   bool      `json:"rebuildRequired,omitempty"`
	RowCount          int       `json:"rowCount,omitempty"`
}

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
	PhysicalShed       string             `json:"physicalShed,omitempty"`
	Partition          string             `json:"partition,omitempty"`
	AnimalStage        string             `json:"animalStage"`
	TargetCount        int                `json:"targetCount"`
	OpenCount          int                `json:"openCount"`
	DoneCount          int                `json:"doneCount"`
	AcceptedCount      int                `json:"acceptedCount"`
	ReviewCount        int                `json:"reviewCount"`
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
	PrimaryActionKey   string             `json:"primaryActionKey"`
	ObligationID       *string            `json:"obligationId,omitempty"`
	BatchID            *string            `json:"batchId,omitempty"`
	SOPTaskID          *string            `json:"sopTaskId,omitempty"`
	SOPVersionID       *string            `json:"sopVersionId,omitempty"`
	SOPTaskRowVersion  *int32             `json:"sopTaskRowVersion,omitempty"`
	CompletionID       *string            `json:"completionId,omitempty"`
}

type ExecutionResponse struct {
	Source     string         `json:"source"`
	Rows       []ExecutionRow `json:"rows"`
	TotalCount int64          `json:"totalCount"`
	NextCursor *string        `json:"nextCursor,omitempty"`
	// ViewerReadOnly marks this as a leadership OVERSIGHT read (park-scoped, all sheds):
	// the caller is not an assigned operator, so the client shows the shed list but must
	// NOT let them open a shed into the operator scan/execute loop. Operators get false.
	ViewerReadOnly bool                 `json:"viewerReadOnly"`
	Freshness      *ProjectionFreshness `json:"freshness,omitempty"`
	CarrySummary   *CarrySummary        `json:"carrySummary,omitempty"`
	FilterOptions  *ExecutionFilters    `json:"filterOptions,omitempty"`
}

type ExecutionFilters struct {
	Parks []ParkOption `json:"parks,omitempty"`
}

// VaccineCarryLine is internal aggregation from repo layer (date + vaccine + counts).
type VaccineCarryLine struct {
	Date           string // ISO date YYYY-MM-DD
	VaccineLabel   string
	RemainingDoses int64 // count(DISTINCT goat_id) WHERE status IN (scheduled,due,in_progress)
	TotalDoses     int64 // count(DISTINCT goat_id)
}

// VaccineCarrySummary is per-vaccine breakdown in one day's response.
type VaccineCarrySummary struct {
	VaccineLabel   string `json:"vaccineLabel"`
	RemainingDoses int64  `json:"remainingDoses"`
	TotalDoses     int64  `json:"totalDoses"`
}

// CarryDay is one business day's carry summary (date + per-vaccine breakdown + totals).
type CarryDay struct {
	Date             string                `json:"date"` // ISO date YYYY-MM-DD
	VaccineBreakdown []VaccineCarrySummary `json:"vaccineBreakdown"`
	TotalRemaining   int64                 `json:"totalRemaining"` // total remaining for day
}

// CarrySummary is page-independent daily carry aggregation (full date range, not paginated).
// projection-review: membership=all obligations for (tenant, operator, date) scope;
// grain=eff_date + protocol_name; parity=sum distinct goats with status IN (scheduled,due,in_progress)
type CarrySummary struct {
	CarryByDay []CarryDay `json:"carryByDay"` // ordered by date
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
	TenantID string
	ParkID   *string
	ShedID   *string
	// OperatorScopeActorID is set only for app/mobile execution reads. It is the
	// authenticated actor id and the repository resolves it to the matching
	// workforce member before returning assigned operator work. Admin reads leave
	// it empty and keep the broader park/tenant visibility.
	OperatorScopeActorID string
	AuthorizedParkIDs    []string
	WorkState            *WorkState
	Severity             *Severity
	Cursor               *ExecutionCursor
	AsOf                 time.Time
	DueBefore            time.Time
	HistoricalAsOf       bool
	OpenOnly             bool
	IncludeFilterOptions bool
	Limit                int
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
	ProtocolID   string           `json:"protocolId"`
	WorkState    WorkState        `json:"workState"`
	LastDose     *time.Time       `json:"lastDose,omitempty"`
	NextDue      *time.Time       `json:"nextDue,omitempty"`
	VaccineNames []string         `json:"vaccineNames"`
	Counts       OperationsCounts `json:"counts"`
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
	Source     string               `json:"source"`
	Protocols  []OperationsProtocol `json:"protocols"`
	Cohorts    []OperationsCohort   `json:"cohorts"`
	NextCursor *string              `json:"next_cursor,omitempty"`
	Freshness  *ProjectionFreshness `json:"freshness,omitempty"`
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
	VaccineNames      []string
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
	Freshness         *ProjectionFreshness
}

type OperationsQuery struct {
	TenantID string
	ParkID   *string
	// ShedID optionally narrows the cohort×protocol rollup to a single shed (used by the shed-detail
	// vaccine breakdown, which re-aggregates the shed's cohort cells per protocol). Empty = all sheds.
	ShedID         *string
	AsOf           time.Time
	DueBefore      time.Time
	HistoricalAsOf bool
	Limit          int
	Cursor         *OperationsCursor
}

// ScheduleQuery reads the Full Schedule canonical month/window.
// MonthStart is the first local business day of the requested month, stored as
// midnight in the Goat OS business timezone.
type ScheduleQuery struct {
	TenantID   string
	ParkID     *string
	MonthStart time.Time
	Limit      int
	Cursor     *OperationsCursor
}

type DriveAssignmentQuery struct {
	TenantID   string
	ParkID     *string
	MonthStart time.Time
	Limit      int
}

type DriveAssignmentRow struct {
	PlannedDate          string            `json:"plannedDate"`
	OriginalPlannedDate  string            `json:"originalPlannedDate"`
	OperatorID           string            `json:"operatorId"`
	OperatorName         string            `json:"operatorName"`
	ParkID               string            `json:"parkId"`
	ParkName             string            `json:"parkName"`
	ShedID               *string           `json:"shedId,omitempty"`
	PhysicalShed         string            `json:"physicalShed"`
	PartitionLabel       string            `json:"partitionLabel"`
	Animals              int               `json:"animals"`
	DueAnimals           int               `json:"dueAnimals"`
	DoneAnimals          int               `json:"doneAnimals"`
	DeferredAnimals      int               `json:"deferredAnimals"`
	OverdueAnimals       int               `json:"overdueAnimals"`
	VaccineNames         []string          `json:"vaccineNames"`
	VaccineCodes         []string          `json:"vaccineCodes"`
	VaccineOriginalDates map[string]string `json:"vaccineOriginalDates"`
	TotalDoses           int               `json:"totalDoses"`
	Capacity             CapacityStatus    `json:"capacity"`
}

type DriveAssignmentResponse struct {
	Source string               `json:"source"`
	Rows   []DriveAssignmentRow `json:"rows"`
}

// ScanRosterRow represents a single per-animal vaccination obligation for mobile scan screen.
// primaryTag and secondaryTag are RFID identifiers; vaccineLabel is the vaccine name and schedule position.
type ScanRosterRow struct {
	GoatID         string  `json:"goatId"`
	PrimaryTag     string  `json:"primaryTag"`
	SecondaryTag   *string `json:"secondaryTag,omitempty"`
	VaccineLabel   string  `json:"vaccineLabel"`
	Status         string  `json:"status"`
	ScannedAt      *string `json:"scannedAt,omitempty"`
	ObligationID   string  `json:"obligationId"`
	BatchID        string  `json:"batchId"`
	TaskID         string  `json:"taskId"`
	SOPVersionID   string  `json:"sopVersionId"`
	TaskRowVersion int32   `json:"taskRowVersion"`
}

type ScanRosterQuery struct {
	TenantID             string
	ShedID               string
	TaskID               string
	OperatorScopeActorID string
	Cursor               *ScanRosterCursor
	Limit                int
}

type ScanRosterCursor struct {
	GoatID       string `json:"goat_id"`
	ObligationID string `json:"obligation_id"`
}

type ScanRosterResult struct {
	Rows       []ScanRosterRow
	NextCursor *ScanRosterCursor
}

type TaskOptionValue struct {
	Value             string     `json:"value"`
	Label             string     `json:"label"`
	Disabled          bool       `json:"disabled"`
	DisabledReason    *string    `json:"disabled_reason,omitempty"`
	AvailableQuantity *string    `json:"available_quantity,omitempty"`
	QuantityUnit      *string    `json:"quantity_unit,omitempty"`
	ExpiryDate        *time.Time `json:"expiry_date,omitempty"`
	FEFORank          *int       `json:"fefo_rank,omitempty"`
}

type TaskOptionSource struct {
	Source         string            `json:"source"`
	DisabledReason *string           `json:"disabled_reason,omitempty"`
	Options        []TaskOptionValue `json:"options"`
}

type TaskOptionValuesResponse struct {
	TaskID         string             `json:"task_id"`
	BatchID        string             `json:"batch_id"`
	SOPVersionID   string             `json:"sop_version_id"`
	TaskRowVersion int32              `json:"task_row_version"`
	Sources        []TaskOptionSource `json:"sources"`
}

type ExecutionProjection struct {
	ParkID               string
	ParkName             string
	ShedID               string
	ShedName             string
	PhysicalShed         string
	Partition            string
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
	ScannedCount         int
	ProofSubmittedCount  int
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
	WorkState            WorkState
	SortRank             int
	SortDueMicros        int64
	SortRowKey           string
}

// ExecutionProjectionPage is the repository result for one stable keyset page. TotalCount is
// computed over the server-filtered set before the cursor is applied; NextCursor identifies the
// last returned row when another row exists.
type ExecutionProjectionPage struct {
	Rows       []ExecutionProjection
	TotalCount int64
	NextCursor *ExecutionCursor
	Freshness  *ProjectionFreshness
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
	GoatID            string
	DisplayID         string
	AnimalIdentifier1 *string
	AnimalIdentifier2 *string
	ParkID            string
	ParkName          string
	ShedID            *string
	ShedName          *string
	ReasonCode        GapReasonCode
}

type GapRow struct {
	GoatID string `json:"goatId"`
	// DisplayID is the Goat OS passport id (G-XXXXXX). AnimalIdentifier1/2 are the two
	// physical tags ("Tag 1"/"Tag 2") from goat_identifiers, nil when no active tag of that
	// type is attached — the mobile data-gaps card shows all three so a field operator can
	// physically locate the animal that needs its data fixed.
	DisplayID         string        `json:"displayId"`
	AnimalIdentifier1 *string       `json:"animalIdentifier1,omitempty"`
	AnimalIdentifier2 *string       `json:"animalIdentifier2,omitempty"`
	ParkID            string        `json:"parkId"`
	ParkName          string        `json:"parkName"`
	ShedID            *string       `json:"shedId,omitempty"`
	ShedName          *string       `json:"shedName,omitempty"`
	ReasonCode        GapReasonCode `json:"reasonCode"`
	ReasonLabel       string        `json:"reasonLabel"`
}

type GapsQuery struct {
	TenantID string
	ParkID   *string
	Cursor   *string // last goat_id seen (exclusive); nil/empty means start from the beginning.
	Limit    int
}

type GapsResponse struct {
	Source     string   `json:"source"`
	ParkID     *string  `json:"parkId,omitempty"`
	Rows       []GapRow `json:"rows"`
	NextCursor *string  `json:"nextCursor,omitempty"`
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

// ---- Shed-wise vaccination summary read model (shed-level rollup for the /vaccination screen) ----
//
// One row per shed. Counts are ANIMAL-LEVEL, never per-obligation: a goat needing three vaccines is ONE
// due animal at the shed level, not three (per-vaccine obligation counts appear ONLY in the shed detail
// vaccine breakdown, ShedVaccineRow.Counts). This is the CEO/shed view — a confusing "3 due" when one
// animal needs three shots is explicitly rejected.
//
//   Animals = alive goats physically in the shed (goats.lifecycle_status='alive').
//   Due     = distinct alive goats with >=1 obligation that is actionable-and-unfinished as-of
//             (see the shed-due predicate below).
//   Done    = Animals - Due (identity always holds; Due + Done == Animals).
//
// Shed-due predicate (the ONE place the animal-level "still needs work" rule is defined; implemented
// directly in shedSummaryCanonicalReadSQL -- see repository.go. The prior projector's replay of this
// logic, vaccinationShedProjectionInsertSQL, was removed with the dropped projection tables).
// An animal is DUE if, reconstructed as-of, it has at least one vaccination obligation
// whose effective status is one of {overdue, due, in_progress} OR whose completion sub-state is one of
// {recorded (proof pending), rejected (rework)}. NOT due: future 'scheduled', 'completed'+accepted,
// health-held 'deferred'/'waived'. (Open business question flagged to maintainer: whether 'missed'
// should also count as due — currently surfaced separately via ShedStatus 'blocked' but NOT added to
// the Due animal count, so it does not silently inflate Done; change one predicate to flip this.)

// ShedStatus is the CEO-friendly merged headline shown in the shed row Status column. It folds the
// capacity state and the vaccination state into ONE label by priority (highest first):
//
//	overdue > needs_review (capacity breach with no late animal) > split (safely split across days) > due > scheduled > on_track
//
// This is the INTERNAL machine vocabulary; the CEO UI renders the backend-provided label (never the raw
// token, never the word "state"). "within_cap" never appears here — a shed that fits in one day falls
// through to its vaccination status. Derived directly in shedSummaryCanonicalReadSQL (see
// repository.go), not stored; the prior projector's replay of this logic was removed with the
// dropped projection tables.
type ShedStatus string

const (
	ShedStatusOverdue     ShedStatus = "overdue"      // >=1 overdue animal
	ShedStatusNeedsReview ShedStatus = "needs_review" // capacity breach with no late animal
	ShedStatusSplit       ShedStatus = "split"        // safely split across multiple days (over cap), none overdue
	ShedStatusDue         ShedStatus = "due"          // >=1 due animal, none overdue
	ShedStatusScheduled   ShedStatus = "scheduled"    // only future scheduled work, nothing due
	ShedStatusOnTrack     ShedStatus = "on_track"     // no open vaccination work
)

// ShedOwner is a resolved workforce person for a shed's Manager or Backup slot, read cross-module from
// the workforce roster (Manager = shed-scoped Position holder; Backup = the center Backup Manager slot).
// A nil Manager/Backup on a row means the assignment is MISSING — a seed/config gap surfaced in staging
// preflight, NEVER a normal business state and never invented.
type ShedOwner struct {
	WorkforceMemberID string `json:"workforceMemberId"`
	DisplayName       string `json:"displayName"`
}

// ShedSummaryRow is one shed's rollup line for the shed-wise vaccination table. Sessions = planned
// vaccination visits/days for the shed (usually 1; >1 when the daily cap forces a split). Capacity is
// the machine capacity state (CEO label rendered by the UI). Status is the merged CEO headline.
type ShedSummaryRow struct {
	ParkID   string     `json:"parkId"`
	ParkName string     `json:"parkName"`
	ShedID   string     `json:"shedId"`
	ShedName string     `json:"shedName"`
	Animals  int        `json:"animals"`
	Due      int        `json:"due"`
	Done     int        `json:"done"`
	Sessions int        `json:"sessions"`
	LastDone *string    `json:"lastDone,omitempty"` // Asia/Kolkata business date of latest accepted dose
	NextDue  *string    `json:"nextDue,omitempty"`  // Asia/Kolkata business date of earliest open obligation
	Manager  *ShedOwner `json:"manager,omitempty"`
	Backup   *ShedOwner `json:"backup,omitempty"`
	// DriveOperatorNames are the actual vaccination operators assigned by the operator-cap planner.
	// This is the ownership field for vaccination drives; Manager/Backup remain legacy shed-owner context.
	DriveOperatorNames []string       `json:"driveOperatorNames,omitempty"`
	Capacity           CapacityStatus `json:"capacity"`
	Status             ShedStatus     `json:"status"`
}

// ShedOwnershipScope is one shed row whose owner cells need enrichment. ParkID is the center/park scope
// used for center-level backup fallback.
type ShedOwnershipScope struct {
	ShedID string
	ParkID string
}

// ShedOwnership is the workforce-owned manager/backup pair displayed on a shed row. Nil values are
// honest seed/config gaps.
type ShedOwnership struct {
	Manager *ShedOwner
	Backup  *ShedOwner
}

// PageInfo carries offset-pagination metadata. Shed rows are bounded (a tenant has at most a few hundred
// sheds), so offset+total is scale-safe here; the large, unbounded axis is the per-shed ANIMAL list,
// which uses goat_id keyset pagination (ShedAnimalPage), never offset.
type PageInfo struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type ShedSummaryResponse struct {
	Source    string               `json:"source"`
	Rows      []ShedSummaryRow     `json:"rows"`
	Page      PageInfo             `json:"page"`
	Freshness *ProjectionFreshness `json:"freshness,omitempty"`
}

// ShedSummarySort is the whitelisted sort vocabulary (the ORDER BY fragment is chosen in Go from this
// closed set — never interpolated from raw client input).
type ShedSummarySort string

const (
	ShedSortStatus     ShedSummarySort = "status"       // DEFAULT: merged status priority (overdue > needs_review > split > due > scheduled > on_track), then park, then shed
	ShedSortParkShed   ShedSummarySort = "park_shed"    // park name, then shed name (alphabetical)
	ShedSortDueDesc    ShedSummarySort = "due_desc"     // most due animals first
	ShedSortAnimalDesc ShedSummarySort = "animals_desc" // largest sheds first
	ShedSortNextDue    ShedSummarySort = "next_due"     // soonest due first
)

type ShedSummaryQuery struct {
	TenantID       string
	ParkID         *string
	ShedID         *string
	Status         *ShedStatus
	Capacity       *CapacityStatus
	Search         *string
	AsOf           time.Time
	DueBefore      time.Time
	HistoricalAsOf bool
	Sort           ShedSummarySort
	Limit          int
	Offset         int
}

// ShedSummaryProjection is one aggregated shed straight from SQL, before the service attaches the
// resolved Manager/Backup (cross-module). Sessions/Capacity/Status are legacy shed-summary fields
// computed IN SQL so older filters can page correctly. New vaccination drive scheduling must use
// operator/date assignments from the operator-drive planner, where capacity is unique animals per
// available operator per business date.
type ShedSummaryProjection struct {
	ParkID             string
	ParkName           string
	ShedID             string
	ShedName           string
	Animals            int
	DueAnimals         int
	OpenCells          int            // legacy open obligation rows; not the new operator capacity unit
	Sessions           int            // legacy session count
	Capacity           CapacityStatus // from Sessions vs (buffer+1)
	Status             ShedStatus     // merged CEO headline
	DriveOperatorNames []string
	LastDone           *time.Time
	NextDue            *time.Time
	TotalCount         int // window COUNT(*) OVER() of the filtered set, for PageInfo.Total
	Freshness          *ProjectionFreshness
}

// ---- Shed detail read model (per-vaccine breakdown + keyset-paginated animal list) ----

// ShedVaccineRow is one vaccine's obligation breakdown inside a shed. This is the ONLY place per-vaccine
// obligation counts are exposed (the shed row stays animal-level).
type ShedVaccineRow struct {
	ProtocolID string           `json:"protocolId"`
	Name       string           `json:"name"`
	WorkState  WorkState        `json:"workState"`
	LastDose   *string          `json:"lastDose,omitempty"`
	NextDue    *string          `json:"nextDue,omitempty"`
	Counts     OperationsCounts `json:"counts"`
}

// ShedAnimalRow is one animal in the shed-detail roster: the three identities the UI shows. Tag1/Tag2
// are nil when the animal has no such identifier on record — the UI renders "-", never a "missing id"
// badge. LastDose is the latest accepted vaccination timestamp for the animal. goatId is the opaque
// keyset cursor.
type ShedAnimalRow struct {
	GoatID    string  `json:"goatId"`
	DisplayID string  `json:"displayId"`
	Tag1      *string `json:"tag1,omitempty"`
	Tag2      *string `json:"tag2,omitempty"`
	Breed     *string `json:"breed,omitempty"`
	Sex       string  `json:"sex"`
	Age       *string `json:"age,omitempty"`
	Lifecycle string  `json:"lifecycleStatus"`
	Health    *string `json:"healthStatus,omitempty"`
	LastDose  *string `json:"lastDose,omitempty"`
	NextDue   *string `json:"nextDue,omitempty"`
	Status    string  `json:"status"`
}

type ShedAnimalPage struct {
	Rows       []ShedAnimalRow `json:"rows"`
	NextCursor *string         `json:"nextCursor,omitempty"`
}

type ShedDetailResponse struct {
	Source          string           `json:"source"`
	ParkID          string           `json:"parkId"`
	ParkName        string           `json:"parkName"`
	ShedID          string           `json:"shedId"`
	ShedName        string           `json:"shedName"`
	Animals         int              `json:"animals"`
	Due             int              `json:"due"`
	Done            int              `json:"done"`
	Sessions        int              `json:"sessions"`
	Manager         *ShedOwner       `json:"manager,omitempty"`
	Backup          *ShedOwner       `json:"backup,omitempty"`
	Capacity        CapacityStatus   `json:"capacity"`
	Status          ShedStatus       `json:"status"`
	PlannedSessions []PlannedSession `json:"plannedSessions"`
	Vaccines        []ShedVaccineRow `json:"vaccines"`
}

// ShedAnimalQuery is the keyset-paginated per-shed animal list query (separate endpoint so the large
// per-shed animal axis never rides on the shed-detail header/vaccine payload).
type ShedAnimalQuery struct {
	TenantID     string
	ShedID       string
	AsOf         time.Time
	DriveDueDate *time.Time
	Cursor       *string // last goat_id seen (exclusive)
	Limit        int
}

// ---- Vaccination command board read model (CEO closure view) ----
// projection-review: membership=tenant + optional drive_batch_id scoped obligations & completions;
// grain=kpi (all), cohort (stage×sex), shed×dose (rule grain), week (ISO week of administered_at),
// verification queue (shed×dose); join_cardinality=goat lookups 1:1, completion status pre-aggregated;
// pagination=verification_queue keyset-bound, others unbounded per envelope (324-goat, ~640-obligation basis).

// CommandBoardKPI is the board's headline row at ANIMAL grain: Targets is the number of
// distinct animals in scope, and the five counts below it are a disjoint, exhaustive partition
// of Targets, so the tiles always add up to the total they sit under.
//
// ClosedWithoutDose is the fifth tile. Without it the row did not reconcile: an animal whose
// every obligation was closed with no completion against it (canceled/waived/superseded) counted
// in Targets but matched none of the other four predicates, so a 100-animal drive with 3
// withdrawn animals showed "Total 100" over tiles summing to 97 and left the reader unable to
// tell a bug from real outstanding work. It names that residual rather than removing those
// animals from Targets, so Targets stays the roster the operator was handed and the withdrawal
// stays visible instead of being quietly deducted.
type CommandBoardKPI struct {
	Targets              int `json:"targets"`
	DosesVerified        int `json:"dosesVerified"`
	AwaitingVerification int `json:"awaitingVerification"`
	OverdueNotGiven      int `json:"overdueNotGiven"`
	ScheduledAhead       int `json:"scheduledAhead"`
	ClosedWithoutDose    int `json:"closedWithoutDose"`
}

type CommandBoardCohort struct {
	// ParkID/ParkName carry the farm this cohort sits on. The matrix is read farmwise, so the
	// same cohort on two farms stays two cells.
	ParkID          string `json:"parkId"`
	ParkName        string `json:"parkName"`
	ManagementStage string `json:"managementStage"`
	Sex             string `json:"sex"`
	AnimalCount     int    `json:"animalCount"`
}

// CommandBoardCohortCell is one farm × cohort × dose-qualified-vaccine cell of the CEO board's
// cohort matrix, at OBLIGATION grain.
//
// The three counts are a DISJOINT partition of the cell's obligations along the operational
// question "who owes the next move":
//
//	PendingCount   -- the OPERATOR owes field work: an open obligation with NO completion recorded
//	SubmittedCount -- the VERIFIER owes review: a completion is recorded but not yet accepted
//	VerifiedCount  -- nobody owes anything: the completion is verifier-accepted
//
// PendingCount previously fused the first two states, because obligation status advances only on
// VERIFICATION, never on submission. A park whose every animal had been vaccinated and submitted
// therefore rendered byte-identically to a park nobody had touched, and the same 40 animals were
// reported as "40 awaiting verification" in the KPI row and "40 pending" in the matrix directly
// below it, with no column reconciling the two. SubmittedCount is that missing column; it
// reconciles with CommandBoardKPI.AwaitingVerification (see the grain note below).
//
// GRAIN NOTE: these counts are DISTINCT obligation_id; CommandBoardKPI counts DISTINCT animal.
// The two agree exactly at one-obligation-per-animal-per-vaccine, the grain every live vaccination
// drive uses. An animal carrying several vaccines the same day contributes one obligation to each
// of its vaccine cells and one animal to the KPI row.
type CommandBoardCohortCell struct {
	Cohort       CommandBoardCohort `json:"cohort"`
	VaccineLabel string             `json:"vaccineLabel"`
	PendingCount int                `json:"pendingCount"`
	// SubmittedCount is field work DONE and awaiting a verifier. Disjoint from PendingCount and
	// VerifiedCount.
	SubmittedCount int `json:"submittedCount"`
	// VerifiedCount is DISJOINT from PendingCount and SubmittedCount: an accepted obligation is
	// neither still-open-unrecorded nor recorded-but-unverified, so the three can be shown side by
	// side without double counting.
	VerifiedCount int `json:"verifiedCount"`
	// MinAdministeredDate/MaxAdministeredDate are the real medical dates of the VERIFIED doses in
	// this cell (never the drive's planned date), so a clubbed adult drive still reports when the
	// dose actually went in.
	MinAdministeredDate *time.Time `json:"minAdministeredDate,omitempty"`
	MaxAdministeredDate *time.Time `json:"maxAdministeredDate,omitempty"`
}

// ShedDoseMatrixCell represents state of a shed × dose rule combination.
type ShedDoseMatrixCell struct {
	ShedID              string     `json:"shedId"`
	ShedName            string     `json:"shedName"`
	DoseRule            string     `json:"doseRule"` // e.g. "et_tt_adult_w1", vaccine + position label
	State               string     `json:"state"`    // "verified", "awaiting", "overdue", "scheduled"
	AnimalCount         int        `json:"animalCount"`
	MinAdministeredDate *time.Time `json:"minAdministeredDate,omitempty"` // for completed
	MaxAdministeredDate *time.Time `json:"maxAdministeredDate,omitempty"` // for completed
	MinDueDate          *time.Time `json:"minDueDate,omitempty"`          // for scheduled
	MaxDueDate          *time.Time `json:"maxDueDate,omitempty"`          // for scheduled
}

// WeeklyGivenRow is one ISO week × vaccine × status aggregation.
type WeeklyGivenRow struct {
	ISOYear           int       `json:"isoYear"`
	ISOWeek           int       `json:"isoWeek"`
	VaccineLabel      string    `json:"vaccineLabel"`
	CompletionStatus  string    `json:"completionStatus"` // "accepted" or "recorded"
	Count             int       `json:"count"`
	MinAdministeredAt time.Time `json:"minAdministeredAt"`
	MaxAdministeredAt time.Time `json:"maxAdministeredAt"`
}

// VerificationQueueRow is one shed × dose awaiting-verification row.
type VerificationQueueRow struct {
	ShedID          string     `json:"shedId"`
	ShedName        string     `json:"shedName"`
	DoseRule        string     `json:"doseRule"`
	AwaitingCount   int        `json:"awaitingCount"`
	TotalCount      int        `json:"totalCount"`
	LastGivenOnDate *time.Time `json:"lastGivenOnDate,omitempty"` // max administered_at
	DaysInQueue     *int       `json:"daysInQueue,omitempty"`     // business days since min administered_at
}

// CommandBoardResponse is the CEO closure view aggregating KPIs, cohort vaccine matrix,
// shed dose matrix, weekly given chart, and verification queue.
// CommandBoardDriveOption identifies one drive the board can be narrowed to. A drive is a
// batch with a window, so the label carries the vaccine, the window dates, and the status —
// rule ID alone is not a drive selector when rules recur across dates and parks.
type CommandBoardDriveOption struct {
	DriveBatchID string `json:"driveBatchId"`
	// ParkID/ParkName carry the park the drive's work is in. Without them the option list
	// was park-BLIND while the board it feeds is not: leadership viewing all parks
	// (park_id omitted) got one row per batch with no way to tell CBE's drive from CPT's,
	// so two same-vaccine, same-window drives in different parks presented as a single
	// selector entry and their counts read as one drive's. The row grain is therefore
	// (batch, park), not batch alone -- a batch whose obligations span parks is genuinely
	// two operator days in two places and must be offered as two choices.
	ParkID      string     `json:"parkId,omitempty"`
	ParkName    string     `json:"parkName,omitempty"`
	DriveName   string     `json:"driveName"`
	Label       string     `json:"label"`
	Status      string     `json:"status"`
	PlannedDate *time.Time `json:"plannedDate,omitempty"`
	WindowStart *time.Time `json:"windowStart,omitempty"`
	WindowEnd   *time.Time `json:"windowEnd,omitempty"`
	TargetCount int        `json:"targetCount"`
	DoseCount   int        `json:"doseCount"`
	ShedNames   []string   `json:"shedNames"`
}

type CommandBoardResponse struct {
	Source       string                    `json:"source"`
	KPIs         CommandBoardKPI           `json:"kpis"`
	DriveOptions []CommandBoardDriveOption `json:"driveOptions"`
	// DriveOptionsTruncated says the bound was hit and drives were left out. The list has always
	// been bounded, but it used to stop silently, so a scheduled drive that fell past the bound
	// was indistinguishable from a drive that was never planned -- the reader goes looking, finds
	// nothing, and concludes the work does not exist. Surfacing the overflow lets the UI say
	// "more drives exist, narrow by park" instead of lying by omission.
	DriveOptionsTruncated bool                     `json:"driveOptionsTruncated"`
	CohortMatrix          []CommandBoardCohortCell `json:"cohortMatrix"`
	ShedDoseMatrix        []ShedDoseMatrixCell     `json:"shedDoseMatrix"`
	WeeklyGiven           []WeeklyGivenRow         `json:"weeklyGiven"`
	VerificationQueue     []VerificationQueueRow   `json:"verificationQueue"`
	Freshness             *ProjectionFreshness     `json:"freshness,omitempty"`
}

type CommandBoardQuery struct {
	TenantID       string
	DriveBatchID   *string   // optional: narrow KPIs to this drive batch
	ParkID         *string   // optional: narrow KPIs to this park
	AsOf           time.Time // defaults to now in business timezone
	HistoricalAsOf bool
}

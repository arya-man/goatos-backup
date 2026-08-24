// Package domain holds the vaccination module domain types (dose-administered records).
package domain

import (
	"errors"
	"time"
)

var (
	ErrStockGateBlocked      = errors.New("vaccination: stock gate blocked")
	ErrStockMovementConflict = errors.New("vaccination: stock movement idempotency conflict")
	ErrCompletionNotOpen     = errors.New("vaccination: completion obligation is not open")
	ErrFutureManualCampaign  = errors.New("vaccination: manual campaign as_of is in the future")
	ErrInvalidCursor         = errors.New("vaccination: invalid cursor")
)

// NewCompletion is the input to record one administered dose against an obligation.
type NewCompletion struct {
	TenantID                 string
	ObligationID             string
	BatchID                  *string
	GoatID                   string
	SopSubmissionItemID      *string
	VaccineInventoryLotID    *string
	Doses                    *int32
	DoseMlGiven              string
	RouteSite                string
	AdverseReaction          bool
	AdverseReactionProblemID *string
	ColdChainVerified        bool
	AdministeredAt           time.Time
	Status                   string // defaults to "recorded"
	WithdrawalUntilDate      *time.Time
	RecordedBy               *string
	IdempotencyKey           string
}

// CompletionHistoryItem is one row of a goat's vaccination history.
type CompletionHistoryItem struct {
	CompletionID        string
	ObligationID        string
	BatchID             string
	AdministeredAt      time.Time
	Status              string
	Doses               int32
	RouteSite           string
	DoseCode            string
	VaccineLabel        string
	AdverseReaction     bool
	WithdrawalUntilDate *time.Time
}

// AcceptedCompletion is the verification context returned when a recorded completion is accepted:
// enough to complete the obligation (SM-5) and consume the reserved dose. BatchID/LotID are "" when
// the completion was not part of a drive (no stock to consume).
type AcceptedCompletion struct {
	CompletionID   string
	Status         string
	ObligationID   string
	GoatID         string
	BatchID        string
	LotID          string
	Doses          int32
	AdministeredAt time.Time
}

// AcceptCompletionAtomicInput carries the verification metadata for accepting an existing recorded
// completion inside the database-owned SM-5 transaction.
type AcceptCompletionAtomicInput struct {
	TenantID        string
	CompletionID    string
	VerifiedBy      *string
	WithdrawalUntil *time.Time
}

// AcceptCompletionAtomicResult reports the durable side effects applied by the SM-5 transaction.
type AcceptCompletionAtomicResult struct {
	Completion     AcceptedCompletion
	Applied        bool
	Accepted       bool
	Completed      bool
	Consumed       bool
	StatusEventID  string
	OutboxInserted bool
}

// RecordedCompletion is one completion awaiting review (Verification queue).
type RecordedCompletion struct {
	CompletionID   string
	ObligationID   string
	GoatID         string
	BatchID        string
	SOPTaskID      string
	SOPTaskVersion int32
	AdministeredAt time.Time
	Doses          int32
	RouteSite      string
}

// SubmissionCompletion is the durable per-goat completion context materialized from one
// vaccination SOP submission. A submission may contain more than one vaccine obligation for the
// same goat; verification groups these rows by GoatID so one handling clip can cover every vaccine
// administered to that goat in that handling. ObligationID is the specific obligation this
// completion row closes -- sopbridge's PEND-1 start trigger (markObligationsInProgress) calls
// obligation.MarkInProgress per row using this id.
type SubmissionCompletion struct {
	CompletionID string
	SubmissionID string
	ObligationID string
	GoatID       string
	GoatLabel    string
	ShedID       string
	// PartitionLabel is the raw partition ('1', 'Part 3'), empty for a non-partitioned shed.
	// Raw on purpose: the display is composed at the wire boundary via oploc.Display().
	PartitionLabel string
	ShedLabel      string
	ParkID         string
	ProofRefIDs    []string
	AdministeredAt time.Time
	// VaccineLabel is the HUMAN dose label for this completion ("ET+TT", "PPR · Booster"),
	// already run through DoseDisplayLabel by the adapter. It is what the verifier is shown, so
	// it must never be the raw dose_code ("et_tt_adult_w2") -- that is a config token and is
	// banned from user-facing copy (AGENTS.md, make ui-vaccine-labels-guard). Empty only when the
	// completion's obligation/rule/protocol chain does not resolve.
	VaccineLabel string
}

type RecordedCompletionCursor struct {
	AdministeredAt time.Time
	CompletionID   string
}

type RecordedCompletionPage struct {
	Items      []RecordedCompletion
	TotalCount int64
	NextCursor *string
}

// LastAccepted is the most recent accepted administration for a goat (next-due / SM-7 basis).
type LastAccepted struct {
	CompletionID   string
	ObligationID   string
	AdministeredAt time.Time
}

// ImpactFilter is the eligibility predicate for impact preview. Empty text dims mean "any";
// ParkID nil means tenant-wide.
type ImpactFilter struct {
	TenantID                string
	ProtocolVersionID       string
	AsOf                    time.Time
	WarmupNoVaccinationDays int32
	Species                 string
	Stage                   string
	Sex                     string
	Breed                   string
	Health                  string
	ParkID                  *string
}

// ImpactRequest drives an aggregate config impact preview for a vaccination rule/version. Preview
// numbers come from the vaccination_eligibility_rollups read model, never a live goats scan.
type ImpactRequest struct {
	Filter        ImpactFilter
	VaccineItemID *string
	LocationID    *string
	DoseRows      int32 // number of selected dose/schedule rows (vaccination cells per eligible animal)
	// DailyCap is the DRAFT animals/operator/day cap authored in the rule editor, used to compute
	// estimated_days before publish. 0 = fall back to the published/operational cap (CapacityMaxPerDay).
	DailyCap int64
	// MaxBufferDays is the DRAFT safe-window buffer authored in the rule editor. nil = fall back to the
	// business default. It drives the preview's within_cap / split / needs_review classification.
	MaxBufferDays *int64
	HorizonDays   int
	AsOf          time.Time
}

// EligibleGoat is one row from the generation listing (in-care cohort). The row intentionally
// carries all eligibility dimensions that are not enough to trust to a coarse SQL prefilter:
// health/reproductive state and location ICU/quarantine flags decide defer/hold behavior.
type EligibleGoat struct {
	GoatID               string
	DOB                  *time.Time
	EntryDate            *time.Time
	WarmingEntryAt       *time.Time
	BreedingDate         *time.Time
	LastDeliveryDate     *time.Time
	Species              string
	OriginType           string
	LifecycleStatus      string
	HealthStatus         string
	ReproductiveStatus   string
	ShedID               string
	ParkID               string
	PartitionLabel       string
	Sex                  string
	Breed                string
	Stage                string
	AgeBand              string
	LocationIsQuarantine bool
	LocationIsICU        bool
}

// TrustedCompletionCandidate is one generation-time suppression check. DueAt is part of the
// identity so repeat/campaign cycles for the same goat/rule do not collapse into each other.
type TrustedCompletionCandidate struct {
	GoatID   string
	RuleID   string
	DoseCode string
	DueAt    time.Time
	Repeat   string
}

func (c TrustedCompletionCandidate) Key() string {
	return c.GoatID + "|" + c.RuleID + "|" + c.DoseCode + "|" + c.DueAt.UTC().Format(time.RFC3339Nano)
}

// RecentVaccineAdministration is the latest accepted Goat OS dose used for cross-vaccine gap checks.
type RecentVaccineAdministration struct {
	AdministeredAt    time.Time
	VaccineCode       string
	VaccineType       string
	PathogenClass     string
	DoseCode          string
	Sequence          int32
	ProtocolVersionID string
	ProtocolID        string
}

// GenerateResult summarises an SM-1 generation run.
type GenerateResult struct {
	Generated int
	Deferred  int
	Reopened  int
	// Reconciled counts work the animal ALREADY owed under this rule identity, moved onto the
	// current version and due date instead of being re-minted beside itself. It is deliberately
	// separate from Generated: nothing new was created, and an operator reading the run should
	// see that the plan moved without their list churning.
	Reconciled                 int
	FailedGoats                int
	SkippedNoDueDate           int
	SuppressedByTrustedHistory int
}

// GenerationRun is the durable operator-visible status row for an existing-cohort
// vaccination generation pass. The row lets Config/Data Ops see publish-triggered
// generation without reading worker logs.
type GenerationRun struct {
	RunID                      string
	TenantID                   string
	ProtocolVersionID          string
	TriggerType                string
	TriggerRef                 string
	Status                     string
	StartedAt                  time.Time
	CompletedAt                *time.Time
	Generated                  int
	Deferred                   int
	Reopened                   int
	FailedGoats                int
	SkippedNoDueDate           int
	SuppressedByTrustedHistory int
	CursorGoatID               string
	LastError                  string
	IdempotencyKey             string
	RequestHash                string
}

// GenerationRunInput starts one durable generation run.
type GenerationRunInput struct {
	TenantID          string
	ProtocolVersionID string
	TriggerType       string
	TriggerRef        string
	StartedAt         time.Time
	IdempotencyKey    string
	RequestHash       string
}

// ImpactPreview is the aggregate-only config impact preview. Every number is computed from the
// vaccination_eligibility_rollups read model (plus a cheap optional stock lookup) — the UI request
// path never scans goats. Per-animal/workflow/session/manager detail is intentionally NOT here; that
// belongs after publish/planner execution.
type ImpactPreview struct {
	EligibleAnimals  int64 // SUM(animal_count) WHERE usable_for_vaccination
	VaccinationCells int64 // eligible_animals × selected dose rows
	AffectedSheds    int64 // distinct sheds with usable animals
	EstimatedDays    int64 // ceil(eligible_animals / daily_cap)
	DailyCap         int64 // configured animals/operator/day used for estimated_days
	// CapacityStatus classifies the draft under the operator animal cap + buffer window, mirroring the planner:
	// within_cap (fits one day), over_cap (fits the safe window = buffer + 1 days), capacity_breach
	// (beyond the window -> needs review). Empty when there are no eligible animals to plan.
	CapacityStatus  string
	PlannedSessions []ImpactPlannedSession
	// Optional stock check — populated only when a vaccine item is set. Kept because the lookup is a
	// single cheap indexed aggregate on inventory_stock, not a goats join.
	DosesAvailable string
	EarliestExpiry *time.Time
	// Read-model freshness: source_revision is the recompute stamp behind these numbers (0 when the
	// rollup has no rows for the scope yet), recomputed_at is when that grain was last rebuilt.
	SourceRevision int64
	RecomputedAt   *time.Time
	Warnings       []string
}

// ImpactPlannedSession mirrors the operational shed planner's day rows for a pre-publish, aggregate
// config preview. It is bounded by the service for very large herds; EstimatedDays remains the full
// source of truth for total duration.
type ImpactPlannedSession struct {
	Date         string `json:"date"`         // Asia/Kolkata business date, YYYY-MM-DD
	Vaccinations int64  `json:"vaccinations"` // eligible animals planned that day
	DailyLimit   int64  `json:"dailyLimit"`   // operator animal cap used for the preview
	Capacity     string `json:"capacity"`     // within_cap | capacity_breach
}

// EligibilityRollupAggregate is the aggregate read from vaccination_eligibility_rollups for a preview:
// total usable animals and the number of distinct sheds holding them, with the read model's freshness.
type EligibilityRollupAggregate struct {
	EligibleAnimals int64
	AffectedSheds   int64
	SourceRevision  int64
	RecomputedAt    *time.Time
}

// RollupRecomputeResult summarises one full recompute of the eligibility rollup for a tenant.
type RollupRecomputeResult struct {
	TenantID        string
	Grains          int64 // rollup rows written
	EligibleAnimals int64 // total usable animals across all grains
	SourceRevision  int64
	RecomputedAt    time.Time
}

// VaccineBreakdownItem is one display-name/count pair in a ShedCompletionSummary.
type VaccineBreakdownItem struct {
	Vaccine string
	Count   int64
}

// ShedCompletionSummary is the FROZEN read-only contract for the vaccination shed-completion /
// submit screen. Shed completion is an ACKNOWLEDGEMENT, not a manual form: the operator already
// did the real work at animal level (scan + one camera proof clip per goat row); this summary
// reports whether every expected animal in the shed has been scanned and proofed, so Submit can
// be enabled or blocked with a human reason. shed_name / drive_name / vaccine names are always
// human display strings, never raw UUIDs.
type ShedCompletionSummary struct {
	TaskID           string
	ShedName         string
	DriveName        string
	ExpectedCount    int64
	HandledCount     int64
	ProofReadyCount  int64
	ProofMode        string
	VaccineBreakdown []VaccineBreakdownItem
	SubmitEnabled    bool
	BlockingReason   *string
	SubmitState      string // draft | submitted | verified | closed
	// RoundSubmitted is true only when a live, shed-scoped submission trail exists for THIS
	// shed's CURRENT round of eligible (non-terminal) obligations: either a still-open
	// verification item for this shed, an unaccepted vaccination_completions row covering the
	// currently eligible obligations, or (when nothing is currently eligible) an accepted
	// completion history proving the round was submitted and verified. It is false whenever the
	// shed has open, unsubmitted obligations for this round -- including immediately after a
	// verifier rejection reopens an obligation, even if a STALE prior-round submission/verdict
	// still exists for this shed. SubmitState is a coarse, sometimes-stale word derived across
	// rounds; RoundSubmitted is the unambiguous per-round boolean clients must gate on instead of
	// inferring round identity from SubmitState alone.
	RoundSubmitted bool
	// RoundID is a deterministic fingerprint of this shed's current obligation-round state: a
	// hash over every obligation in this shed's batch paired with its own row_version. Postgres
	// already bumps obligation_instances.row_version on every completion/reopen transition
	// (MarkObligationCompleted, ReopenObligation), so RoundID changes value the instant any
	// obligation in the shed moves through submit or verifier-rejection reopen -- no new column
	// or client/timestamp-derived proxy needed. RoundSubmitted is computed from these SAME
	// per-obligation facts, so the two fields can never disagree.
	RoundID string
}

// --- BUG-017: pre-arrival accepted-history channel -------------------------------------------
//
// Procurement forwards a supplier-attested pre-arrival vaccination card in the `goat.created`
// payload key `trusted_vaccination_history`. Those claims have no proof artifact and no
// holding-farm warm-up window, so they can never satisfy the proof-backed
// `procurement_hf_vaccination_evidence` trust gate. They land in their own reviewed channel
// (`vaccination_prearrival_history_entries`) where each claim is validated against the published
// protocol rules and the animal's INDEPENDENTLY classified schedule path before it may become
// accepted history.

// PreArrivalHistoryReviewAccepted / PreArrivalHistoryReviewRejected are the two terminal review
// states of a pre-arrival claim. Only accepted entries feed vaccination generation.
const (
	PreArrivalHistoryReviewAccepted = "accepted"
	PreArrivalHistoryReviewRejected = "rejected"
)

// PreArrivalHistorySourceProcurementHandoff is the only source system that writes this channel today.
const PreArrivalHistorySourceProcurementHandoff = "procurement_pc_handoff"

// PreArrivalHistoryEntry is one reviewed supplier claim. RuleID/ProtocolVersionID are empty when
// the claim could not be resolved to a published rule; such an entry is always rejected.
type PreArrivalHistoryEntry struct {
	VaccineCode        string
	DoseCode           string
	Sequence           int32
	AdministeredAt     time.Time
	SchedulePath       string
	ProtocolVersionID  string
	RuleID             string
	ReviewStatus       string
	RejectionReason    string
	Claim              []byte
	IdempotencyKey     string
	RequestFingerprint string
}

// PreArrivalHistoryIngest is one goat's reviewed pre-arrival claim set, persisted atomically.
type PreArrivalHistoryIngest struct {
	TenantID      string
	GoatID        string
	SourceSystem  string
	SourceEventID string
	ReviewedBy    *string
	ReviewedAt    time.Time
	Entries       []PreArrivalHistoryEntry
}

// PreArrivalHistoryIngestResult reports what the write actually did. Replayed counts entries that
// already existed with an identical fingerprint (exact replay: no new side effects).
type PreArrivalHistoryIngestResult struct {
	Accepted int
	Rejected int
	Inserted int
	Replayed int
}

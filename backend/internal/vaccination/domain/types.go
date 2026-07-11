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
	// DailyCap is the DRAFT daily vaccination cap authored in the rule editor, used to compute
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
	Generated                  int
	Deferred                   int
	Reopened                   int
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
	EstimatedDays    int64 // ceil(vaccination_cells / daily_cap)
	DailyCap         int64 // configured vaccinations/day used for estimated_days
	// CapacityStatus classifies the draft under the daily cap + buffer window, mirroring the planner:
	// within_cap (fits one day), over_cap (fits the safe window = buffer + 1 days), capacity_breach
	// (beyond the window → needs review). Empty when there are no cells to plan.
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
	Vaccinations int64  `json:"vaccinations"` // cells planned that day
	DailyLimit   int64  `json:"dailyLimit"`   // daily cap used for the preview
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

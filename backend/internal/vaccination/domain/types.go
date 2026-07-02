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
	TenantID string
	Stage    string
	Sex      string
	Breed    string
	Health   string
	ParkID   *string
}

// ImpactRequest drives a live impact preview for a vaccination rule/version.
type ImpactRequest struct {
	Filter        ImpactFilter
	VaccineItemID *string
	LocationID    *string
	DosesPerGoat  int32
	DoseRows      int32 // number of schedule rows (obligations per eligible goat per cycle)
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
	ProtocolVersionID string
}

// GenerateResult summarises an SM-1 generation run.
type GenerateResult struct {
	Generated                  int
	Deferred                   int
	Reopened                   int
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

// ImpactPreview is the computed live impact (eligible goats, catch-up, obligations, batches,
// doses required vs available, warnings). Mock math is replaced by these real counts.
type ImpactPreview struct {
	EligibleGoats  int64
	CatchupGoats   int64
	Obligations    int64
	Batches        int64
	DosesRequired  int64
	DosesAvailable string
	EarliestExpiry *time.Time
	Warnings       []string
}

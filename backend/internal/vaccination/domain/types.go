// Package domain holds the vaccination module domain types (dose-administered records).
package domain

import "time"

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
	ObligationID   string
	GoatID         string
	BatchID        string
	LotID          string
	Doses          int32
	AdministeredAt time.Time
}

// RecordedCompletion is one completion awaiting review (Verification queue).
type RecordedCompletion struct {
	CompletionID   string
	ObligationID   string
	GoatID         string
	BatchID        string
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

// EligibleGoat is one row from the chunked generation listing (in-care cohort). Sex/Breed/Stage
// are populated only on the single-goat path (GetGoatForGeneration) for Go-side eligibility match;
// the chunked path pre-filters in SQL so they are left empty there.
type EligibleGoat struct {
	GoatID          string
	DOB             *time.Time
	EntryDate       *time.Time
	LifecycleStatus string
	ShedID          string
	ParkID          string
	Sex             string
	Breed           string
	Stage           string
}

// GenerateResult summarises an SM-1 generation run.
type GenerateResult struct {
	Generated        int
	Deferred         int
	SkippedNoDueDate int
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

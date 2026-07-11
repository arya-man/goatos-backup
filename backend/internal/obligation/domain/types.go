// Package domain holds the obligation (due-state) domain types.
package domain

import "time"

// NewObligation is the input to generate one obligation instance. IdempotencyKey is the
// deterministic key that makes generation a no-op on replay.
type NewObligation struct {
	TenantID             string
	ProtocolVersionID    string
	RuleID               string
	BatchID              *string
	TargetType           string
	TargetID             string
	ScopeType            string
	ScopeID              string
	DueAt                time.Time
	WindowStart          *time.Time
	WindowEnd            *time.Time
	Status               string
	IdempotencyKey       string
	GeneratedByTriggerID *string
	Sequence             int32
}

// RecoveryReschedule replans a health-deferred obligation on recovery: align to a nearby planned
// drive within the policy window, or due immediately for a micro-drive.
type RecoveryReschedule struct {
	DueAt       time.Time
	WindowStart time.Time
	WindowEnd   *time.Time
	AlignReason string
}

// ObligationRef is a minimal stored-obligation lookup result.
type ObligationRef struct {
	ObligationID string
	Status       string
	DueAt        time.Time
	RowVersion   int32
}

// DueObligation is a row from the due-window scan.
type DueObligation struct {
	ObligationID      string
	ProtocolVersionID string
	RuleID            string
	TargetType        string
	TargetID          string
	ScopeType         string
	ScopeID           string
	DueAt             time.Time
	Status            string
}

// OpenObligation is a goat's still-open obligation (Goat Passport next-due), earliest due first.
type OpenObligation struct {
	ObligationID      string
	ProtocolVersionID string
	RuleID            string
	BatchID           string
	ScopeType         string
	ScopeID           string
	DueAt             time.Time
	Status            string
	Sequence          int32
}

// NewBatch is the input to create a work-unit batch (a drive / feed session).
type NewBatch struct {
	TenantID              string
	ProtocolVersionID     string
	ScopeType             string
	ScopeID               string
	Session               string
	PlannedDate           *time.Time
	WindowStart           *time.Time
	WindowEnd             *time.Time
	Status                string
	EstimatedTargets      int32
	PlannedQuantity       string
	QuantityUnit          string
	PrimaryInventoryLotID *string
	SopTaskID             *string
	ConductedBy           *string
}

// UnbatchedDue is an unbatched scheduled/due obligation (SM-4 sweep input).
type UnbatchedDue struct {
	ObligationID             string
	RuleID                   string
	ScopeType                string
	ScopeID                  string
	TargetID                 string
	TargetSpecies            string
	TargetAnimalStage        string
	TargetReproductiveStatus string
	DueAt                    time.Time
	WindowStart              *time.Time
	WindowEnd                *time.Time
	BatchingHoldCount        int32
	FirstBatchingHoldUntil   *time.Time
}

// ParkConsolidationCandidate is a shed-scoped unbatched obligation eligible for park-level
// drive consolidation after the shed sweep pass.
type ParkConsolidationCandidate struct {
	ObligationID             string
	RuleID                   string
	ShedID                   string
	ParkID                   string
	TargetID                 string
	TargetSpecies            string
	TargetAnimalStage        string
	TargetReproductiveStatus string
	DueAt                    time.Time
	WindowStart              *time.Time
	WindowEnd                *time.Time
	BatchingHoldCount        int32
	FirstBatchingHoldUntil   *time.Time
}

// ParkConsolidationCursor is the keyset cursor for the park-consolidation candidate query. Its
// fields must stay in the same order as the repository ORDER BY.
type ParkConsolidationCursor struct {
	ParkID            string
	RuleID            string
	TargetSpecies     string
	TargetAnimalStage string
	DueAt             time.Time
	ObligationID      string
}

// ComboDriveBatch is a planned shed/park batch participating in combo-session alignment.
type ComboDriveBatch struct {
	BatchID           string
	ProtocolVersionID string
	ScopeType         string
	ScopeID           string
	Session           string
	PlannedDate       *time.Time
}

// ParkConsolidationSettings controls the second-pass park drive planner (after shed batching).
type ParkConsolidationSettings struct {
	Enabled             bool
	MinShedDriveTargets int32 // layer 1 defers shed groups smaller than this to the park pass
	MinParkMergeTargets int32 // cross-shed park batch needs at least this many goats
	MinParkMergeSheds   int32 // cross-shed park batch needs goats from at least this many sheds
}

// DrivePlannerSettings tunes Phase 3 smart drive date selection and batch sizing.
// Zero values use DefaultDrivePlannerSettings().
type DrivePlannerSettings struct {
	Enabled                   bool
	MaxGoatsPerDrive          int32 // 0 = no limit
	VaccinePriority           int32 // lower = higher disease priority (ET+TT=1, PPR=2, ...)
	ComboAlignWindowDays      int32 // cross-version combo batches align within this many days
	MaxBatchingHoldDays       int32 // one-time due-group hold window before forcing a micro-drive
	MaxBatchingHoldCount      int32 // 1 = hold a dose cycle once, never rolling postponement
	SpeciesGroupingPolicy     string
	MaxShotsPerAnimalPerDrive int32
}

// DefaultDrivePlannerSettings returns conservative Phase 3 defaults when rule_dsl omits drive_policy.
// Default drive-planner safety limits. Single source of truth for both enforcement
// (DefaultDrivePlannerSettings, below) and the read-only "Automatic safety rules" copy
// shown in the vaccination Config UI (safety_batch / safety_max_shots). The guard test
// TestSafetyRuleCopyMatchesEnforcedDefaults locks that UI copy to these constants so the
// displayed number can never silently drift from the enforced number.
const (
	DefaultMaxBatchingHoldDays       int32 = 7
	DefaultMaxBatchingHoldCount      int32 = 1
	DefaultMaxShotsPerAnimalPerDrive int32 = 2
)

func DefaultDrivePlannerSettings() DrivePlannerSettings {
	return DrivePlannerSettings{
		Enabled:                   true,
		MaxGoatsPerDrive:          0,
		VaccinePriority:           50,
		ComboAlignWindowDays:      7,
		MaxBatchingHoldDays:       DefaultMaxBatchingHoldDays,
		MaxBatchingHoldCount:      DefaultMaxBatchingHoldCount,
		SpeciesGroupingPolicy:     "kid_mixed",
		MaxShotsPerAnimalPerDrive: DefaultMaxShotsPerAnimalPerDrive,
	}
}

// DefaultParkConsolidationSettings returns the standard park consolidation thresholds.
func DefaultParkConsolidationSettings() ParkConsolidationSettings {
	return ParkConsolidationSettings{
		Enabled:             true,
		MinShedDriveTargets: 2,
		MinParkMergeTargets: 2,
		MinParkMergeSheds:   2,
	}
}

// RuleAttachmentCount is the number of obligations attached to a batch for one protocol rule.
type RuleAttachmentCount struct {
	RuleID string
	Count  int64
}

// BatchStockBlock is the stock-block context recorded for one failed batch reservation.
type BatchStockBlock struct {
	BatchID     string
	ItemID      string
	RequiredQty int64
	Reason      string
}

// PlannedBatchFinalization is a planned batch that already owns obligations but still needs
// replayable side-effect finalization (SOP task link and/or stock reservation).
type PlannedBatchFinalization struct {
	BatchID             string
	RuleID              string
	ScopeType           string
	ScopeID             string
	CreatedAt           time.Time
	PlannedDate         *time.Time
	EstimatedTargets    int32
	AttachedObligations int64
	HasSOPTask          bool
	HasStockReservation bool
	StockBlocked        bool
}

// PlannedBatchFinalizationCursor advances through planned batches in repository
// order so config-unactionable rows cannot pin a sweeper to the first page.
type PlannedBatchFinalizationCursor struct {
	CreatedAt time.Time
	BatchID   string
}

// SweepResult summarises an SM-4 sweep (batches created, obligations attached).
type SweepResult struct {
	Batches         int
	Obligations     int
	ParkBatches     int
	ParkObligations int
}

// NewStatusEvent is the input to append an obligation status event. Scope/RequestHash drive the
// shared idempotency_keys reserve-before-insert guard (cross-partition dedup).
type NewStatusEvent struct {
	TenantID       string
	ObligationID   string
	EventType      string
	OccurredAt     time.Time
	ActorID        *string
	Payload        []byte
	IdempotencyKey string
	Scope          string
	RequestHash    string
}

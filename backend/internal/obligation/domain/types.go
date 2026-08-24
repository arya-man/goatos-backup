// Package domain holds the obligation (due-state) domain types.
package domain

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrOperatorAssignmentConfigPresentButEmpty signals that a park HAS a vaccination
// operator-assignment config, but resolving it yielded zero executable operators for
// the drive day (all off/leave with no cover, missing shift config, or the resolved
// operator is not in the executable candidate set). Callers MUST fail closed
// (defer/zero-capacity, no base-cap batch, no unassigned conducted_by) — they must NOT
// treat this like "no config present" and fall back to base-capacity planning. This is
// distinct from a genuinely absent config, which returns the operators unchanged with
// a nil error. See docs/decisions/scale-anti-patterns.md "config-present must fail closed".
var ErrOperatorAssignmentConfigPresentButEmpty = errors.New("obligation: operator assignment config present but no executable operator for drive day")

// NewObligation is the input to generate one obligation instance. IdempotencyKey is the
// deterministic key that makes generation a no-op on replay.
type NewObligation struct {
	TenantID          string
	ProtocolVersionID string
	RuleID            string
	BatchID           *string
	TargetType        string
	TargetID          string
	ScopeType         string
	ScopeID           string
	DueAt             time.Time
	WindowStart       *time.Time
	WindowEnd         *time.Time
	Status            string
	IdempotencyKey    string
	// RuleIdentityKey names the RULE this obligation serves in business terms --
	// vaccine|dose|sequence -- rather than the version UUID that happened to mint it. Publishing
	// rewrites every rule row, so the version pointer changes while the animal's work does not;
	// the identity is what stays still. At most one open obligation may exist per
	// (target, identity, sequence), enforced by obligation_open_rule_identity_unique_idx.
	RuleIdentityKey      string
	GeneratedByTriggerID *string
	Sequence             int32

	// RepeatCycle records WHICH VACCINATION CAUSED this obligation, for a repeat dose.
	//
	// A repeat is anchored to when the previous dose was actually given, so its due date
	// legitimately moves -- which is why the due date cannot be part of its identity. The
	// cause can: one completed dose mints exactly one open successor. Nil for everything
	// else, and the partial unique indexes apply only where it is set, so non-repeat work
	// is untouched.
	RepeatCycle *RepeatCycleSource
}

// RepeatCycleSource identifies the administration a repeat obligation descends from.
//
// Every writer uses ONE vocabulary: Source is always RepeatCycleSourceTrustedHistory and
// SourceRef is always RepeatCycleRef(vaccine, administered-at, dose), whether the causing
// dose was given inside Goat OS or arrived as accepted history. Do not "correct" a writer to
// name the cause some other way, however natural it looks at that call site: the insert guard
// compares Source and SourceRef literally and the source index keys on them, so a second
// vocabulary means two open rows for one cycle, each invisible to the other. That was a real
// defect here, found in review, not a hypothetical.
//
// AnchorObligationID is recorded when the cause happens to be an obligation in this system --
// for the audit trail and for the stricter per-anchor index -- but it is never the identity.
// History-driven cycles have no obligation row to point at and are identified by SourceRef
// alone, which is why the two indexes are not interchangeable.
type RepeatCycleSource struct {
	Source             string
	SourceRef          string
	AnchorObligationID *string
	AnchorAt           *time.Time
	DueAt              *time.Time
}

// Repeat-cycle source kinds. Stored verbatim and half of the source-uniqueness key, so they
// are constants rather than literals retyped at each call site.
//
// Only RepeatCycleSourceTrustedHistory is written by production code today -- see the type's
// doc for why every writer shares it. The other two are the vocabulary this column would need
// if a cause ever genuinely could not be expressed as an administration.
// Valid reports whether the metadata identifies a cause. Source and SourceRef must BOTH
// be present: a half-populated value looks anchored while being invisible to the partial
// unique indexes (which key on source_ref, and treat a NULL source as distinct), so it
// would silently reintroduce the very duplicates this metadata exists to prevent.
func (r *RepeatCycleSource) Valid() bool {
	return r != nil &&
		strings.TrimSpace(r.Source) != "" &&
		strings.TrimSpace(r.SourceRef) != ""
}

// RepeatCycleRef names the administration that causes a repeat cycle, by its own immutable
// coordinates: which vaccine, given when, as which dose.
//
// Every writer of a repeat cycle must produce this same string for the same administration,
// or the writers do not share an identity at all: the insert guard compares source and ref
// literally, and the two partial unique indexes cannot collide across differing vocabularies.
// One writer naming the cause by completed-obligation id and another naming it by
// administration would leave two open rows for one cycle, each convinced it is the only one.
//
// The timestamp is truncated explicitly rather than relying on the layout: Go's RFC3339
// constant happens to omit fractional seconds, but RFC3339Nano does not, and the SQL that
// reconstructs this reference during repair formats whole seconds. Truncating here means a
// later switch of layout cannot silently split one cause into two.
func RepeatCycleRef(vaccineCode string, administeredAt time.Time, sequence int32) string {
	code := strings.ToLower(strings.TrimSpace(vaccineCode))
	if code == "" || administeredAt.IsZero() {
		return ""
	}
	return strings.Join([]string{
		code,
		administeredAt.UTC().Truncate(time.Second).Format(time.RFC3339),
		strconv.Itoa(int(sequence)),
	}, "|")
}

const (
	RepeatCycleSourceCompletedObligation = "completed_obligation"
	RepeatCycleSourceTrustedHistory      = "trusted_history"
	RepeatCycleSourceImportedHistory     = "imported_history"
)

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
	Reason       string
	DueAt        time.Time
	RowVersion   int32
	// IdempotencyKey is set only by lookups that find a row some OTHER way -- by its cause,
	// say. Generation's reconciliation is all keyed, so a row located by cause is unreachable
	// without carrying its key back.
	IdempotencyKey string
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
	ClinicalDueAt     time.Time
	ScheduledFor      *time.Time
	DoseCode          string
	VaccineLabel      string
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
	BatchingHoldUntil     *time.Time
	DriveAssignments      []DriveAssignment
}

type DriveAssignment struct {
	BatchID        string
	PlannedDate    time.Time
	OperatorID     *string
	ParkID         string
	ShedID         *string
	PhysicalShed   string
	PartitionLabel string
	AnimalCount    int32
	VaccineRuleIDs []string
	TotalDoses     int32
	CapacityStatus string
	Warnings       []string
}

type DriveOperatorCapacity struct {
	OperatorID    string
	Cap           int32
	ConfiguredCap int32
}

type VaccineDriveDateOverride struct {
	TenantID              string
	ParkID                string
	VaccineCode           string
	OriginalDriveDate     time.Time
	OverrideDate          time.Time
	RequestedOverrideDate time.Time
	AutoShifted           bool
	ShiftReason           string
	ConflictVaccineCode   string
	ConflictVaccineLabel  string
	ConflictDate          time.Time
	ConflictRule          string
	Reason                string
	CreatedBy             string
	CreatedAt             time.Time
}

// UnbatchedDue is an unbatched scheduled/due obligation (SM-4 sweep input).
type UnbatchedDue struct {
	ObligationID             string
	RuleID                   string
	ScopeType                string
	ScopeID                  string
	ParkID                   string
	ShedName                 string
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
	ShedName                 string
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

// UnbatchedDueCursor is the keyset cursor for the write-free preflight scan of unbatched due
// obligations (RV-02). Its fields MUST stay in the same order as the repository ORDER BY
// (scope_type, scope_id, rule_id, due_at, obligation_id) so keyset paging returns every candidate
// exactly once. UUID fields are stored as strings at the domain boundary but rebound as UUIDs by
// the Postgres adapter; ordering text renderings in SQL would not match the UUID-backed index.
type UnbatchedDueCursor struct {
	ScopeType    string
	ScopeID      string
	RuleID       string
	DueAt        time.Time
	ObligationID string
}

// ParkConsolidationCursor is the keyset cursor for the park-consolidation candidate query. The
// repository ORDER BY keeps the planner's real group key (park + species/stage) contiguous before
// due/rule row identity, so one park/species window is never split as unrelated rule pages.
type ParkConsolidationCursor struct {
	ParkID            string
	TargetSpecies     string
	TargetAnimalStage string
	DueAt             time.Time
	RuleID            string
	ObligationID      string
}

// ComboDriveBatch is a planned shed/park batch participating in combo-session alignment.
// TargetIDs is the distinct set of animal target IDs already attached to this batch, used to
// keep AlignComboDrives from pushing any one animal past MaxShotsPerAnimalPerDrive when it
// co-locates approved combo batches (FMD+HS, etc.) onto one shared drive date. SafeStart/SafeEnd
// are the intersection of the attached obligations' medical windows; HoldUntil is the intersection
// of their one-time batching hold caps. Alignment must satisfy both envelopes.
type ComboDriveBatch struct {
	BatchID           string
	ProtocolVersionID string
	ScopeType         string
	ScopeID           string
	ParkID            string
	Session           string
	PlannedDate       *time.Time
	SafeStart         *time.Time
	SafeEnd           *time.Time
	HoldUntil         *time.Time
	CellCount         int32
	TargetIDs         []string
}

// ComboBatchCursor is a keyset pagination cursor for ListPlannedComboBatchesKeyset.
// Matches the query's ORDER BY clause: (scope_type, scope_id, session, batch_id).
//
// R2-06 fix: planned_date was REMOVED from both the cursor and the ORDER BY. AlignComboDrives
// itself mutates planned_date (via UpdateBatchPlannedDate) mid-pagination, so keying the cursor on
// a column the same loop writes let an aligned row's sort position shift between page reads,
// letting a later page skip or re-read a row that crossed the cursor boundary. The cursor now keys
// ONLY on columns AlignComboDrives never mutates, so a row's position in keyset order is stable for
// the lifetime of the pagination loop.
type ComboBatchCursor struct {
	ScopeType string
	ScopeID   string
	Session   string
	BatchID   string
}

// ParkConsolidationSettings controls the second-pass park drive planner (after shed batching).
type ParkConsolidationSettings struct {
	Enabled             bool
	MinShedDriveTargets int32 // layer 1 defers shed groups at/below this to the park pass
	MinParkMergeTargets int32 // park batch needs at least this many distinct animals
	MinParkMergeSheds   int32 // deprecated/no-op: park vaccination drives optimize animals, not shed count
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
		MinParkMergeSheds:   1,
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
	StockBlockItemID    string
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

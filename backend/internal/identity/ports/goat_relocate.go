package ports

import "time"

// RelocateGoatsCommand is a BULK, set-based move of named animals into one destination shed.
//
// It exists for the Counts shifting-approval path. MoveGoat is the per-animal admin move: it owns a
// transaction, takes a row_version, and writes a decision record per goat. Approving a shifting
// event moves a whole group at once, so calling MoveGoat in a loop would be both non-atomic (one
// transaction per animal) and an N+1 fan-out. This command is applied as a single set-based
// statement inside the approval's transaction instead.
//
// The animals are ALWAYS named explicitly by GoatIDs. This module never infers which animals a
// movement refers to.
type RelocateGoatsCommand struct {
	TenantID string
	ActorID  string
	TraceID  string

	// GoatIDs is the explicit, caller-supplied set of animals to move. Bounded by
	// MaxRelocateGoatsPerCommand.
	GoatIDs []string

	// P1 follow-up #1: Expected source park and shed. If supplied, the relocate fails closed
	// if any animal's current location differs, preventing stale location overwrites.
	FromParkID         *string
	FromShedID         *string
	FromPartitionLabel *string

	ToParkID string
	ToShedID string

	// DestinationPartitionLabel is the raw partition label ('1', 'Part 3') within ToShedID this move
	// targets, or nil for a genuinely non-partitioned destination shed. OperationalLocation = park +
	// physical shed + optional partition (see backend/internal/platform/oploc); ToShedID always
	// stays the PARENT physical shed, never a partition-bearing alias, and this field carries the
	// partition half separately. Applied to every moved goat's goat_shed_partitions row in the SAME
	// transaction as the shed_id write, whether or not the shed itself also changed.
	DestinationPartitionLabel *string

	// DestinationShedName is the destination physical shed's display name, needed to compose the
	// operator-facing operational-location label (oploc.OperationalLocation.Display) stored as
	// goat_shed_partitions.source_shed_name. Required whenever a caller wants that column populated
	// with a real display string; an empty value falls back to the shed id so the write still
	// satisfies the non-blank source_shed_name constraint.
	DestinationShedName string

	// DestinationTag is the raise-time selected management_stage. Sheds may contain mixed stages;
	// residents and shed_profiles never override it. Empty means preserve each goat's current stage.
	// This field changes management_stage only; it never creates pregnancy or lactation facts.
	DestinationTag string

	// AllowClinicalDestinationTag lifts the clinical-state refusal on DestinationTag for exactly
	// one caller class: a HEALTH-type shifting (maintainer decision 2026-08-20, superseding the
	// 2026-08-15 clinical lock FOR THAT TYPE ONLY -- a health shifting IS the health team acting,
	// so moving an animal into the ICU pen also sets her clinical state, and her vaccinations
	// defer until the return leg makes her eligible again). Every other caller leaves this false
	// and keeps ErrClinicalDestinationTag. See docs/features/shifting/shifting-rewrite-tag-rules.md.
	AllowClinicalDestinationTag bool

	// Reason is recorded on every goat_location_history row written by this command.
	Reason string

	OccurredAt time.Time

	// OutboxIdempotencyPrefix seeds the per-goat outbox idempotency key
	// ("<prefix>:<goat_id>"), so replaying the same approval cannot enqueue a second
	// goat.location.changed for the same animal.
	OutboxIdempotencyPrefix string
}

// ConfigureAdoptedShedCohortCommand tags a destination pen with the tag an arriving typed
// shifting group carries -- "pen tags follow occupancy" (maintainer decisions 2026-08-20,
// docs/features/shifting/shifting-rewrite-tag-rules.md). Runs inside the shifting apply
// transaction via the counts IdentityTxWriter seam; the write re-validates that the pen is still
// unconfigured-or-matching and holds no disagreeing live animal, failing closed with
// ErrDestinationPenChanged otherwise.
type ConfigureAdoptedShedCohortCommand struct {
	TenantID string
	ShedID   string
	// PartitionLabel is the raw pen label within ShedID, nil for a genuinely non-partitioned shed
	// (the tag then lives on shed_profiles, exactly as the Counts Breakdown editor writes it).
	PartitionLabel *string
	// Stage is the tag to adopt, canonicalized against the live vocabulary at write time. Clinical
	// states are refused -- no movement type adopts a clinical tag onto a PEN.
	Stage string
}

// MaxRelocateGoatsPerCommand bounds one bulk relocate. A shed movement is a real-world group of
// animals walked between sheds, not a herd-wide operation; capping it keeps the statement's memory
// and lock footprint bounded and keeps a malformed request from locking the whole herd.
const MaxRelocateGoatsPerCommand = 500

// RelocateGoatsResult reports exactly which animals moved. The caller compares it against the
// requested set and FAILS CLOSED on any shortfall rather than silently relocating a subset.
type RelocateGoatsResult struct {
	MovedGoatIDs []string
}

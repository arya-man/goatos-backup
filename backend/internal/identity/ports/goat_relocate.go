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
	FromParkID *string
	FromShedID *string

	ToParkID string
	ToShedID string

	// DestinationTag is an optional caller assertion about the management_stage the moved animals
	// adopt at the destination shed. The effective value is resolved from the destination shed's
	// active shed_profiles row joined through animal_stage_lookup; if supplied, this tag must agree.
	DestinationTag string

	// ExpectedDestinationProfile* is the destination profile snapshot captured at authorization.
	// Completion fails closed if the profile identity, row_version, or resolved stage drifted before
	// the animals were physically moved.
	ExpectedDestinationProfileID         string
	ExpectedDestinationProfileRowVersion int32
	ExpectedDestinationStage             string

	// Reason is recorded on every goat_location_history row written by this command.
	Reason string

	OccurredAt time.Time

	// OutboxIdempotencyPrefix seeds the per-goat outbox idempotency key
	// ("<prefix>:<goat_id>"), so replaying the same approval cannot enqueue a second
	// goat.location.changed for the same animal.
	OutboxIdempotencyPrefix string
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

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

	// DestinationTag is the management_stage the moved animals ADOPT at the destination shed:
	// shifting a goat into a shed makes it JOIN that shed's operational cohort (a pregnant shed ⇒
	// pregnant; K0 → K1 ⇒ it grew up). The adapter resolves the EFFECTIVE tag as follows
	// (maintainer decision 2026-07-19, homogeneous sheds):
	//
	//   - OCCUPIED destination shed: the tag is DERIVED from the single distinct management_stage its
	//     existing live animals already carry. DestinationTag is then optional; if supplied it must
	//     AGREE with the derived tag (a disagreement is ErrDestinationTagConflict — a shed cannot
	//     hold two cohorts).
	//   - EMPTY destination shed: there is nothing to derive from, so DestinationTag is REQUIRED
	//     (absent ⇒ ErrDestinationTagRequired) and is validated against the tenant's active
	//     management-stage vocabulary.
	//
	// Never silently keeps the old tag, and never leaves management_stage stale. Empty string means
	// "not supplied".
	DestinationTag string

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

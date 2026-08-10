package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// Feed PACKING verification gate ports (maintainer decision, 2026-07-26, SUPERSEDING the "packing
// stays instant" rule). This is the module's THIRD write boundary, entirely separate from
// CompletionStore (the old instant feed_direction_session_completions path, now inert) and
// DistributionCompletionStore (feed_distribution_completions). It owns exactly one NEW table
// (feed_packing_completions) behind PackingCompletionStore, gated on a verifier-approved video before a
// packing session is completed. Unlike distribution (two proofs), packing needs ONE mandatory video.

var (
	// ErrPackingProofRequired is returned when a packing completion omits the MANDATORY packing VIDEO.
	// There is nothing for a verifier to approve without it, so the completion is rejected before any
	// state changes.
	ErrPackingProofRequired = errors.New("feeddirection: a packing video proof is required")
	// ErrPackingStoreUnavailable is returned when a packing completion is attempted but no
	// PackingCompletionStore is wired -- a deployment/wiring error, surfaced as a 500.
	ErrPackingStoreUnavailable = errors.New("feeddirection: packing completion store is not configured")
)

// CompletePackingParams is the persisted gated-completion write, at the PEN-DAY grain
// (tenant, park, shed, partition, target_date, workflow).
//
// session_no left this key on 2026-08-10 (maintainer decision): a packer packs a pen's whole day in
// one go and films it ONCE, so the day is the unit that is proved and verified. The pen did NOT
// leave the key and must not -- see PartitionLabel.
type CompletePackingParams struct {
	TenantID string
	ParkID   string
	ShedID   string
	// PartitionLabel is the pen this completion covers ("2", "Part 3"); empty for an undivided
	// shed. Part of the completion's IDENTITY -- see migration 000137 and app.completedKey.
	PartitionLabel string
	TargetDate     time.Time
	Workflow       string
	// PackingProofRef is the ONE MANDATORY packing VIDEO proof_id. It travels into the queued
	// verification item.
	PackingProofRef string
	// CompletedBy is the operator principal uuid when the caller carries one, else "".
	CompletedBy string
	// IdempotencyKey is the client-supplied request key, reserved in the same transaction as the write.
	IdempotencyKey string
	// ActorID/ActorType/TraceID feed the audit row written in the same transaction.
	ActorID   string
	ActorType string
	TraceID   string
}

// CompletePackingResult reports the outcome of a packing completion write.
type CompletePackingResult struct {
	CompletionID string
	// Status is the row's state after the write: 'pending_verification' on a fresh submit or a rework
	// re-submit, or 'completed' when the shed-session was already verified.
	Status string
	// RowVersion is the row's version after the write. It keys the verification item's idempotency so a
	// rework re-submit (row_version bumped) enqueues a fresh item rather than colliding with the old one.
	RowVersion int32
	// NewlyPending is true ONLY when the row entered pending_verification on THIS call (a fresh submit or
	// a rework re-submit). It is false on an idempotent replay, an already-pending no-op, or an
	// already-completed no-op -- so the enqueue fires exactly once per real pending transition.
	NewlyPending bool
	// ShedName and PartitionLabel are carried for verification enqueue label composition.
	ShedName, PartitionLabel string
}

// VerifiedPacking identifies one VERIFIED (status='completed') PEN-DAY for the packing
// serving-read overlay.
type VerifiedPacking struct {
	ShedID string
	// PartitionLabel is the pen. It was missing here while this type keyed on shed alone, which is
	// why ListVerifiedPacking could not key the overlay and the production path uses
	// ListPackingCompletionStatuses instead.
	PartitionLabel string
	Workflow       string
}

// PackingCompletionStatus is one PEN-DAY's packing completion row with its RAW status
// ('pending_verification' | 'rework' | 'completed'), for the serve-path status overlay + filter.
//
// Deliberately NOT the shared SessionCompletionStatus: distribution is still gated per shed-SESSION
// and keeps that type. Reusing it here would leave a SessionNo field that packing must always set to
// a meaningless zero, and the next author would key an overlay on it.
type PackingCompletionStatus struct {
	ShedID string
	// PartitionLabel is the pen this completion covers ("2", "Part 3"), empty for an undivided
	// shed. It is part of the completion's IDENTITY: without it every pen of a shed resolves to
	// one status, so a video shot in Castro - 1 marked Castro - 2 and Castro - 3 "in review" too
	// (reported on STG 2026-08-08). See migration 000137.
	PartitionLabel string
	Workflow       string
	Status         string
	// ReworkReason is the stored sentence explaining a 'rework' row, empty in every other state. It
	// travels with the status because the two things that put a pen in rework -- a verifier rejecting
	// the video, and the afternoon correction re-counting the pen -- are indistinguishable without it.
	ReworkReason string
}

// ApplyPackingParams flips a packing completion whose video a verifier APPROVED
// 'pending_verification' -> 'completed'. Issued by the verification.verdict.approved consumer.
type ApplyPackingParams struct {
	TenantID     string
	CompletionID string
	VerifiedBy   string
	TraceID      string
}

// BouncePackingParams flips a packing completion whose video a verifier REJECTED
// 'pending_verification' -> 'rework'. Issued by the verification.verdict.rework consumer.
type BouncePackingParams struct {
	TenantID     string
	CompletionID string
	Reason       string
	TraceID      string
}

// ReopenPackingParams reopens every already-submitted packing pen-day whose ANIMAL COUNT the
// afternoon correction moved, so the packer repacks the pen against the corrected sheet and films it
// again (maintainer decision 2026-08-10).
//
// It is a SET-BASED write over the pens the amend diff named, not one call per pen: the correction
// runs for a whole park at once and a per-pen call would be exactly the N+1 fan-out the scale rules
// ban.
type ReopenPackingParams struct {
	TenantID   string
	ParkID     string
	TargetDate time.Time
	// Workflow is always 'normal' in production. Experiment rations are authored as ABSOLUTE KG PER
	// PEN, so a head-count change does not move a single quantity there and reopening one would throw
	// away a good video for a sheet that did not change. It is a parameter rather than a constant only
	// so the store stays a faithful, testable write boundary.
	Workflow string
	// Pens are the operational locations to reopen, carrying the NORMALIZED partition key so they
	// match feed_packing_completions.partition_key ('whole' for an undivided shed).
	Pens []domain.PenKey
	// Reason is the operator-facing sentence stored on the row and shown on the reopened card. It
	// must say what happened in farm language ("animals moved in/out, quantities changed"), never
	// name a table, a job or a correction window.
	Reason  string
	ActorID string
	TraceID string
}

// ReopenPackingResult reports what the reopen actually moved.
type ReopenPackingResult struct {
	// ReopenedCompletionIDs are the rows moved to 'rework'. Empty is the ordinary case: most
	// corrections land before anyone has packed.
	ReopenedCompletionIDs []string
	// WithdrawnItemCount is the number of still-pending verification items retired with them.
	WithdrawnItemCount int
}

// PackingCompletionStore owns the feed_packing_completions table.
//
// It is an OPTIONAL service dependency, on the same terms as DistributionCompletionStore: a
// pure-generation unit test wires none. Production wires it so the gated packing flow works and the
// packing overlay reports verified sessions.
type PackingCompletionStore interface {
	// CompletePacking records the operator's mandatory video at 'pending_verification' (or moves a
	// 'rework' row back to it), idempotent on both the request key and the pen-day natural key. It
	// does NOT emit feed.packing.completed -- that fires only at verifier approval.
	CompletePacking(ctx context.Context, p CompletePackingParams) (CompletePackingResult, error)

	// ListVerifiedPacking returns every VERIFIED (status='completed') (shed, partition, workflow) for
	// one park-day in one bounded indexed read. Bounded by the park's pen catalog, never by herd size.
	ListVerifiedPacking(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]VerifiedPacking, error)

	// ListPackingCompletionStatuses returns EVERY (shed, partition, workflow) that has a
	// feed_packing_completions row for one park-day, each with its RAW status -- the packing serve
	// path's status overlay + filter source. Unlike ListVerifiedPacking (completed-only), this includes
	// 'pending_verification' and 'rework'. One bounded indexed read, bounded by the park's pen catalog,
	// never by herd size.
	ListPackingCompletionStatuses(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]PackingCompletionStatus, error)

	// ApplyVerifiedPacking flips 'pending_verification' -> 'completed', stamps verified_by/at, and emits
	// feed.packing.completed in one transaction. Returns applied=true only when it actually flipped a
	// pending row; already-completed or non-pending (stale) rows return false with no side effects.
	ApplyVerifiedPacking(ctx context.Context, p ApplyPackingParams) (bool, error)

	// BouncePackingForRework flips 'pending_verification' -> 'rework', stores the reason. Idempotent and
	// stale-guarded: a re-delivered verdict on a non-pending row is a no-op.
	BouncePackingForRework(ctx context.Context, p BouncePackingParams) (bool, error)

	// ReopenPackingForFeedChange moves every named pen's submitted packing back to 'rework' because
	// the afternoon correction changed how many animals it feeds, and retires the verification items
	// that were queued for the now-superseded videos.
	//
	// It reopens BOTH 'pending_verification' AND 'completed' rows (maintainer decision 2026-08-10): a
	// video a verifier already approved proves the packer packed the OLD quantity, which is now the
	// wrong quantity, so an approved clip is no more usable than an unapproved one. A row already in
	// 'rework' is left alone -- it is already back with the operator.
	//
	// Idempotent: running it twice for the same correction reopens nothing the second time, because
	// the rows it moved are no longer in a reopenable state.
	ReopenPackingForFeedChange(ctx context.Context, p ReopenPackingParams) (ReopenPackingResult, error)
}

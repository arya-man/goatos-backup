package ports

import (
	"context"
	"errors"
	"time"
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

// CompletePackingParams is the persisted gated-completion write, at the shed-session grain
// (tenant, park, shed, session_no, target_date, workflow).
type CompletePackingParams struct {
	TenantID   string
	ParkID     string
	ShedID     string
	SessionNo  int32
	TargetDate time.Time
	Workflow   string
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

// VerifiedPacking identifies one VERIFIED (status='completed') shed-session for the packing
// serving-read overlay.
type VerifiedPacking struct {
	ShedID    string
	SessionNo int32
	Workflow  string
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

// PackingCompletionStore owns the feed_packing_completions table.
//
// It is an OPTIONAL service dependency, on the same terms as DistributionCompletionStore: a
// pure-generation unit test wires none. Production wires it so the gated packing flow works and the
// packing overlay reports verified sessions.
type PackingCompletionStore interface {
	// CompletePacking records the operator's mandatory video at 'pending_verification' (or moves a
	// 'rework' row back to it), idempotent on both the request key and the shed-session natural key. It
	// does NOT emit feed.packing.completed -- that fires only at verifier approval.
	CompletePacking(ctx context.Context, p CompletePackingParams) (CompletePackingResult, error)

	// ListVerifiedPacking returns every VERIFIED (status='completed') (shed, session, workflow) for one
	// park-day in one bounded indexed read -- the packing serving-read overlay. Bounded by the park's
	// shed catalog x sessions, never by herd size.
	ListVerifiedPacking(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]VerifiedPacking, error)

	// ListPackingSessionStatuses returns EVERY (shed, session, workflow) that has a
	// feed_packing_completions row for one park-day, each with its RAW status -- the packing serve
	// path's status overlay + filter source. Unlike ListVerifiedPacking (completed-only), this includes
	// 'pending_verification' and 'rework'. One bounded indexed read, bounded by the park's shed catalog
	// x sessions, never by herd size.
	ListPackingSessionStatuses(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]SessionCompletionStatus, error)

	// ApplyVerifiedPacking flips 'pending_verification' -> 'completed', stamps verified_by/at, and emits
	// feed.packing.completed in one transaction. Returns applied=true only when it actually flipped a
	// pending row; already-completed or non-pending (stale) rows return false with no side effects.
	ApplyVerifiedPacking(ctx context.Context, p ApplyPackingParams) (bool, error)

	// BouncePackingForRework flips 'pending_verification' -> 'rework', stores the reason. Idempotent and
	// stale-guarded: a re-delivered verdict on a non-pending row is a no-op.
	BouncePackingForRework(ctx context.Context, p BouncePackingParams) (bool, error)
}

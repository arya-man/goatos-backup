package ports

import (
	"context"
	"errors"
	"time"
)

// Feed DISTRIBUTION verification gate ports (maintainer decision, 2026-07-26). This is the module's
// SECOND write boundary, entirely separate from CompletionStore (feed PACKING, the untouched
// feed_direction_session_completions path). It owns exactly one NEW table
// (feed_distribution_completions) behind DistributionCompletionStore, gated on a verifier-approved
// video before a session is completed. See docs/decisions/feed-distribution-verification.md.

var (
	// ErrDistributionProofRequired is returned when a distribution completion omits the MANDATORY
	// feed-distribution VIDEO. There is nothing for a verifier to approve without it, so the completion
	// is rejected before any state changes.
	ErrDistributionProofRequired = errors.New("feeddirection: a feed-distribution video proof is required")
	// ErrWaterProofRequired is returned when a distribution completion omits the MANDATORY
	// water-distribution proof (photo OR video).
	ErrWaterProofRequired = errors.New("feeddirection: a water-distribution proof is required")
	// ErrDistributionStoreUnavailable is returned when a distribution completion is attempted but no
	// DistributionCompletionStore is wired -- a deployment/wiring error, surfaced as a 500.
	ErrDistributionStoreUnavailable = errors.New("feeddirection: distribution completion store is not configured")
)

// CompleteDistributionParams is the persisted gated-completion write, at the shed-session grain
// (tenant, park, shed, session_no, target_date, workflow).
type CompleteDistributionParams struct {
	TenantID   string
	ParkID     string
	ShedID     string
	SessionNo  int32
	TargetDate time.Time
	Workflow   string
	// DistributionProofRef is the MANDATORY feed-distribution VIDEO proof_id. WaterProofRef is the
	// MANDATORY water proof_id (photo or video). Both travel into the queued verification item.
	DistributionProofRef string
	WaterProofRef        string
	// CompletedBy is the operator principal uuid when the caller carries one, else "".
	CompletedBy string
	// IdempotencyKey is the client-supplied request key, reserved in the same transaction as the write.
	IdempotencyKey string
	// ActorID/ActorType/TraceID feed the audit row written in the same transaction.
	ActorID   string
	ActorType string
	TraceID   string
}

// CompleteDistributionResult reports the outcome of a distribution completion write.
type CompleteDistributionResult struct {
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
}

// VerifiedDistribution identifies one VERIFIED (status='completed') shed-session for the direction
// serving-read overlay.
type VerifiedDistribution struct {
	ShedID    string
	SessionNo int32
	Workflow  string
}

// ApplyDistributionParams flips a distribution completion whose video a verifier APPROVED
// 'pending_verification' -> 'completed'. Issued by the verification.verdict.approved consumer.
type ApplyDistributionParams struct {
	TenantID     string
	CompletionID string
	VerifiedBy   string
	TraceID      string
}

// BounceDistributionParams flips a distribution completion whose video a verifier REJECTED
// 'pending_verification' -> 'rework'. Issued by the verification.verdict.rework consumer.
type BounceDistributionParams struct {
	TenantID     string
	CompletionID string
	Reason       string
	TraceID      string
}

// DistributionCompletionStore owns the feed_distribution_completions table.
//
// It is an OPTIONAL service dependency, on the same terms as CompletionStore: a pure-generation unit
// test wires none. Production wires it so the gated distribution flow works and the direction overlay
// reports verified sessions.
type DistributionCompletionStore interface {
	// CompleteDistribution records the operator's mandatory proofs at 'pending_verification' (or moves a
	// 'rework' row back to it), idempotent on both the request key and the shed-session natural key. It
	// does NOT emit feed.distribution.completed -- that fires only at verifier approval.
	CompleteDistribution(ctx context.Context, p CompleteDistributionParams) (CompleteDistributionResult, error)

	// ListVerifiedDistributions returns every VERIFIED (status='completed') (shed, session, workflow) for
	// one park-day in one bounded indexed read -- the direction serving-read overlay. Bounded by the
	// park's shed catalog x sessions, never by herd size.
	ListVerifiedDistributions(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]VerifiedDistribution, error)

	// ApplyVerifiedDistribution flips 'pending_verification' -> 'completed', stamps verified_by/at, and
	// emits feed.distribution.completed in one transaction. Returns applied=true only when it actually
	// flipped a pending row; already-completed or non-pending (stale) rows return false with no side
	// effects.
	ApplyVerifiedDistribution(ctx context.Context, p ApplyDistributionParams) (bool, error)

	// BounceDistributionForRework flips 'pending_verification' -> 'rework', stores the reason. Idempotent
	// and stale-guarded: a re-delivered verdict on a non-pending row is a no-op.
	BounceDistributionForRework(ctx context.Context, p BounceDistributionParams) (bool, error)
}

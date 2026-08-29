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
	// water-distribution VIDEO. Video-only since 2026-08-11: a photo of a full trough proves a trough
	// is full, not that this operator filled it today.
	ErrWaterProofRequired = errors.New("feeddirection: a water-distribution video proof is required")
	// ErrFeedWeightProofRequired is returned when a distribution completion omits the MANDATORY feed
	// WEIGHT PHOTO (maintainer decision 2026-08-11). The distribution video proves the feed reached
	// the animals but cannot prove how much did; the weight photo is the only capture a verifier can
	// check against the expected ration.
	ErrFeedWeightProofRequired = errors.New("feeddirection: a feed-weight photo proof is required")
	// ErrProofMediaKind is returned when a proof reference resolves to the wrong MEDIA KIND for its
	// step -- a video where the weight PHOTO belongs, or a photo where a VIDEO belongs. Distinct from
	// ErrInvalidProof (unknown/wrong-tenant/incomplete upload) so the handler can tell the operator
	// which capture to redo rather than rejecting the whole submission as unrecognised.
	ErrProofMediaKind = errors.New("feeddirection: proof is the wrong media kind for this step")
	// ErrDistributionStoreUnavailable is returned when a distribution completion is attempted but no
	// DistributionCompletionStore is wired -- a deployment/wiring error, surfaced as a 500.
	ErrDistributionStoreUnavailable = errors.New("feeddirection: distribution completion store is not configured")
	// ErrDistributionAlreadyRecorded is returned when the shed-session already holds a DIFFERENT
	// proof set while pending/completed. A same-proof replay is idempotent; a different proof set
	// would silently strand the operator's new media if accepted as a no-op.
	ErrDistributionAlreadyRecorded = errors.New("feeddirection: this feed-distribution session already has different proofs recorded")
)

// CompleteDistributionParams is the persisted gated-completion write, at the shed-session grain
// (tenant, park, shed, session_no, target_date, workflow).
type CompleteDistributionParams struct {
	TenantID string
	ParkID   string
	ShedID   string
	// PartitionLabel is the pen this completion covers ("2", "Part 3"); empty for an undivided
	// shed. Part of the completion's IDENTITY -- see migration 000137 and app.completedKey.
	PartitionLabel string
	SessionNo      int32
	TargetDate     time.Time
	Workflow       string
	// FeedWeightProofRef is the MANDATORY feed weight PHOTO proof_id. DistributionProofRef is the
	// MANDATORY feed-distribution VIDEO proof_id. WaterProofRef is the MANDATORY water-distribution
	// video proof_id. All three proofs travel into the queued verification item.
	FeedWeightProofRef   string
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
	// Proof refs are the canonical refs currently stored on the completion row. Repair enqueue paths use
	// these rather than the current request body, so a retry cannot queue media different from the row.
	FeedWeightProofRef   string
	DistributionProofRef string
	WaterProofRef        string
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

// SessionCompletionStatus is one shed-session's completion row with its RAW status
// ('pending_verification' | 'rework' | 'completed'), for the serve-path status overlay + filter.
// Shared by the distribution and packing status reads. A shed-session with NO completion row is
// simply absent from the list (the serve path treats absence as pending).
type SessionCompletionStatus struct {
	ShedID string
	// PartitionLabel is the pen this completion covers ("2", "Part 3"), empty for an undivided
	// shed. It is part of the completion's IDENTITY: without it every pen of a shed resolves to
	// one status, so a video shot in Castro - 1 marked Castro - 2 and Castro - 3 "in review" too
	// (reported on STG 2026-08-08). See migration 000137.
	PartitionLabel string
	SessionNo      int32
	Workflow       string
	Status         string
}

// PenSessionCaptureQuery identifies ONE pen-session's capture state. Every field is part of the
// identity: a shed alone would answer for the wrong pen, which is the recurring defect this module
// has paid for twice (migration 000137, and the 2026-08-13 partition_label contract gap).
type PenSessionCaptureQuery struct {
	TenantID       string
	ParkID         string
	ShedID         string
	PartitionLabel string
	SessionNo      int32
	TargetDate     time.Time
	Workflow       string
	// AuthorizedParkIDs is the caller's own park set, threaded through so the PROOF module can run
	// its own scope check rather than being told to skip it. A park-scoped operator must not be able
	// to read another park's proofs by naming its shed.
	AuthorizedParkIDs []string
}

// CapturedProofSlot is ONE already-uploaded proof for a pen-session, whoever recorded it.
//
// This exists because a pen-session's three proofs -- feed weight photo, feed-distribution video,
// water-distribution video -- may be recorded by THREE DIFFERENT operators, each on their own phone
// (maintainer decision 2026-08-14). Before this read, a proof was discoverable only on the device
// that shot it: the others could not see it had been done, and no single phone held all three
// references, so the pen could never be submitted at all.
//
// ProofID is the SERVER proof id, not a device-local outbox id, precisely so a phone that did not
// shoot this proof can still reference it when submitting.
type CapturedProofSlot struct {
	// FieldKey names the slot ("feed_distribution_feed_weight_photo", "feed_distribution_video",
	// "feed_distribution_water_video").
	FieldKey string
	ProofID  string
	// CapturedAt is when the upload completed.
	CapturedAt time.Time
	MimeType   string
	// CapturedByName is the display name of the operator who uploaded this proof. Populated from
	// workforce_members.display_name via uploaded_by user_id. May be empty if the uploader's
	// workforce record is not found.
	CapturedByName string
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

	// ListDistributionSessionStatuses returns EVERY (shed, session, workflow) that has a
	// feed_distribution_completions row for one park-day, each with its RAW status -- the serve path's
	// status overlay + filter source. Unlike ListVerifiedDistributions (completed-only), this includes
	// 'pending_verification' and 'rework'. One bounded indexed read, bounded by the park's shed catalog
	// x sessions, never by herd size.
	ListDistributionSessionStatuses(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]SessionCompletionStatus, error)

	// ApplyVerifiedDistribution flips 'pending_verification' -> 'completed', stamps verified_by/at, and
	// emits feed.distribution.completed in one transaction. Returns applied=true only when it actually
	// flipped a pending row; already-completed or non-pending (stale) rows return false with no side
	// effects.
	ApplyVerifiedDistribution(ctx context.Context, p ApplyDistributionParams) (bool, error)

	// BounceDistributionForRework flips 'pending_verification' -> 'rework', stores the reason. Idempotent
	// and stale-guarded: a re-delivered verdict on a non-pending row is a no-op.
	BounceDistributionForRework(ctx context.Context, p BounceDistributionParams) (bool, error)
}

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
// packing session is completed. Unlike distribution (three proofs), packing needs ONE mandatory video.

var (
	// ErrPackingProofRequired is returned when a packing completion omits the MANDATORY packing VIDEO.
	// There is nothing for a verifier to approve without it, so the completion is rejected before any
	// state changes.
	ErrPackingProofRequired = errors.New("feeddirection: a packing video proof is required")
	// ErrPackingStoreUnavailable is returned when a packing completion is attempted but no
	// PackingCompletionStore is wired -- a deployment/wiring error, surfaced as a 500.
	ErrPackingStoreUnavailable = errors.New("feeddirection: packing completion store is not configured")
	// ErrPackingAlreadyRecorded is returned when a shed-SESSION ALREADY holds a DIFFERENT packing
	// video -- it is awaiting verification, or already verified -- and a second, different one arrives.
	//
	// A packing line accepts exactly ONE video, so this is not a replay and must NOT be answered with
	// success. Returning success here is silent data loss: the operator is told their recording was
	// accepted while nothing records it and no verifier ever sees it.
	//
	// It is deliberately NOT triggered by a genuine retry. An identical request replays on its
	// idempotency key, and a re-send of the SAME proof under a new key still matches the stored
	// proof_ref and stays an idempotent no-op. Only a DIFFERENT video conflicts.
	//
	// It is kept from the 2026-08-10 pen-day work and is NOT specific to that grain. The morning and
	// evening submissions now key different rows again and cannot collide, but any second differing
	// video against one line -- a re-send after a rework the server never recorded, a duplicated
	// queue drain -- must still fail loudly rather than return 200 with the clip discarded.
	ErrPackingAlreadyRecorded = errors.New("feeddirection: this packing session already has a different packing video recorded")
)

// CompletePackingParams is the persisted gated-completion write, at the shed-SESSION grain
// (tenant, park, shed, partition, session_no, target_date, workflow).
//
// session_no briefly left this key on 2026-08-10 and was put BACK on 2026-08-11 (maintainer
// decision): a pen's morning and evening shares are two separate bags, each packed and each filmed
// on its own, so each is proved and verified on its own. The pen is in the key for a separate
// reason and must stay -- see PartitionLabel.
type CompletePackingParams struct {
	TenantID string
	ParkID   string
	ShedID   string
	// PartitionLabel is the pen this completion covers ("2", "Part 3"); empty for an undivided
	// shed. Part of the completion's IDENTITY -- see migration 000137 and app.completedKey.
	PartitionLabel string
	// SessionNo is the feeding session this completion covers (1-based, as authored). Part of the
	// completion's IDENTITY: without it one video closes out both of a pen's bags.
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
	ShedID, ShedName, PartitionLabel string
}

// VerifiedPacking identifies one VERIFIED (status='completed') shed-SESSION for the packing
// serving-read overlay.
type VerifiedPacking struct {
	ShedID string
	// PartitionLabel is the pen. It was missing here while this type keyed on shed alone, which is
	// why ListVerifiedPacking could not key the overlay and the production path uses
	// ListPackingCompletionStatuses instead.
	PartitionLabel string
	SessionNo      int32
	Workflow       string
}

// PackingCompletionStatus is one shed-SESSION's packing completion row with its RAW status
// ('pending_verification' | 'rework' | 'completed'), for the serve-path status overlay + filter.
//
// Deliberately NOT the shared SessionCompletionStatus even though both now carry a session: this one
// also carries ReworkReason, which distribution has no source for. Keeping them apart also keeps the
// two gates independently changeable -- they have diverged once already.
type PackingCompletionStatus struct {
	ShedID string
	// PartitionLabel is the pen this completion covers ("2", "Part 3"), empty for an undivided
	// shed. It is part of the completion's IDENTITY: without it every pen of a shed resolves to
	// one status, so a video shot in Castro - 1 marked Castro - 2 and Castro - 3 "in review" too
	// (reported on STG 2026-08-08). See migration 000137.
	PartitionLabel string
	// SessionNo is the feeding session this row covers. Part of the overlay key: without it the
	// morning's completion would mark the evening line packed too.
	SessionNo int32
	Workflow  string
	Status    string
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

// ReopenPackingParams reopens every already-submitted packing line whose ANIMAL COUNT the afternoon
// correction moved, so the packer repacks against the corrected sheet and films it again (maintainer
// decision 2026-08-10).
//
// THE UNIT NAMED IS THE PEN; THE ROWS MOVED ARE ALL OF THAT PEN'S SESSIONS. Head count is a pen
// fact, and it scales the morning and the evening ration alike, so both of a pen's videos now prove
// the wrong quantity and both must come back (maintainer decision 2026-08-11). There is deliberately
// no session in Pens and no session predicate in the write: a partial reopen would leave one bag
// packed to a head count the farm no longer has.
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
	Reason string
	// There is deliberately NO ActorID. The correction is a scheduled system transition with no human
	// behind it, and audit_log.actor_id is a UUID, so the only value a caller could reach for is the
	// generated_by provenance string ("goatos-api") -- which is not a shortened actor but
	// `invalid input syntax for type uuid`, aborting the audit INSERT and with it the whole reopen.
	// The audit row records ActorType "system" instead. Do not add the field back "for completeness":
	// a field nothing can legally fill is the declared-but-never-populated shape AGENTS.md bans.
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
	// 'rework' row back to it), idempotent on both the request key and the shed-session natural key.
	// It does NOT emit feed.packing.completed -- that fires only at verifier approval.
	CompletePacking(ctx context.Context, p CompletePackingParams) (CompletePackingResult, error)

	// ListVerifiedPacking returns every VERIFIED (status='completed') (shed, partition, session,
	// workflow) for one park-day in one bounded indexed read. Bounded by the park's pen catalog x
	// sessions, never by herd size.
	ListVerifiedPacking(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]VerifiedPacking, error)

	// ListPackingCompletionStatuses returns EVERY (shed, partition, session, workflow) that has a
	// feed_packing_completions row for one park-day, each with its RAW status -- the packing serve
	// path's status overlay + filter source. Unlike ListVerifiedPacking (completed-only), this includes
	// 'pending_verification' and 'rework'. One bounded indexed read, bounded by the park's pen catalog
	// x sessions, never by herd size.
	ListPackingCompletionStatuses(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]PackingCompletionStatus, error)

	// ApplyVerifiedPacking flips 'pending_verification' -> 'completed', stamps verified_by/at, and emits
	// feed.packing.completed in one transaction. Returns applied=true only when it actually flipped a
	// pending row; already-completed or non-pending (stale) rows return false with no side effects.
	ApplyVerifiedPacking(ctx context.Context, p ApplyPackingParams) (bool, error)

	// BouncePackingForRework flips 'pending_verification' -> 'rework', stores the reason. Idempotent and
	// stale-guarded: a re-delivered verdict on a non-pending row is a no-op.
	BouncePackingForRework(ctx context.Context, p BouncePackingParams) (bool, error)

	// ReopenPackingForFeedChange moves ALL SESSIONS of every named pen's submitted packing back to
	// 'rework' because the afternoon correction changed how many animals it feeds, and retires the
	// verification items that were queued for the now-superseded videos.
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

// Package ports defines Counts/Shifting repository boundaries.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

var (
	ErrIdempotencyConflict         = errors.New("counts: idempotency key reused with different payload")
	ErrIdempotencyInProgress       = errors.New("counts: idempotency key is still in progress")
	ErrLogicalKeyConflict          = errors.New("counts: logical shifting event key reused with different payload")
	ErrProjectionExceptionNotFound = errors.New("counts: projection exception not found")
	ErrProjectionExceptionClosed   = errors.New("counts: projection exception already closed")
	ErrMilkPreparationPending      = errors.New("counts: milk preparation is already pending verification")
	ErrMilkPreparationCompleted    = errors.New("counts: milk preparation is already completed")
	ErrMilkPreparationNotFound     = errors.New("counts: milk preparation completion not found")
	ErrMilkPreparationProofs       = errors.New("counts: every applicable milk preparation step requires its own video")
	ErrMilkPreparationInvalidProof = errors.New("counts: milk preparation proof must be a completed live-camera video for its step")
	ErrMilkFeedingPending          = errors.New("counts: milk feeding is already pending verification")
	ErrMilkFeedingCompleted        = errors.New("counts: milk feeding is already completed")
	ErrMilkFeedingNotFound         = errors.New("counts: milk feeding task not found")
	ErrMilkFeedingNotYetAvailable  = errors.New("counts: milk feeding session is not available before its scheduled time")
	ErrMilkFeedingInvalidProof     = errors.New("counts: milk feeding proof must be a completed live-camera video for its step")

	// ErrApprovalRequestNotFound is returned when the addressed approval request does not exist in
	// the caller's tenant.
	ErrApprovalRequestNotFound = errors.New("counts: approval request not found")
	// ErrApprovalAlreadyDecided is returned when a decision targets a request that already reached
	// a DIFFERENT terminal state (e.g. approving a rejected request). Repeating the SAME decision
	// is not an error -- it returns the original row with no new side effects.
	ErrApprovalAlreadyDecided = errors.New("counts: approval request already decided")
	// ErrApprovalEffectIncomplete is returned when an approval's side effect did not cover every
	// animal/row it was supposed to. The decision transaction is rolled back, so the request stays
	// pending rather than half-applying.
	ErrApprovalEffectIncomplete = errors.New("counts: approval effect did not apply completely")
	// ErrDeathEvidenceIncomplete keeps a death approval pending until both mandatory operator
	// videos are present in its staged workflow. Counts/lifecycle remain unchanged on this error.
	ErrDeathEvidenceIncomplete = errors.New("counts: death evidence is incomplete")

	// ErrShiftingEventNotFound is returned when the addressed shifting event does not exist in the
	// caller's tenant.
	ErrShiftingEventNotFound = errors.New("counts: shifting event not found")
	// ErrShiftingNotAuthorized is returned when a completion or cancellation addresses a movement
	// that is not in a state the transition may start from -- a pending (unapproved) movement, or
	// one already rejected/canceled. It is NOT returned for a replay of a transition that already
	// succeeded, which is a no-op success.
	ErrShiftingNotAuthorized = errors.New("counts: shifting event is not in an executable state")
	// ErrShiftingProofRequired is returned when an operator completes a shifting movement without the
	// mandatory video proof (maintainer decision, 2026-07-26). A shed move is applied only after a
	// verifier approves that video, so a completion with no video has nothing to verify and is
	// rejected before any state changes.
	ErrShiftingProofRequired = errors.New("counts: shifting completion requires a video proof")
	// ErrShiftingFeedProofsRequired is returned when a high-priority movement omits either embedded
	// feed-packing or feeding video.
	ErrShiftingFeedProofsRequired = errors.New("counts: high-priority shifting requires feed-packing and feeding video proofs")
	// ErrShiftingFeedConfigBlocked means active destination feed config cannot resolve an exact ration.
	ErrShiftingFeedConfigBlocked = errors.New("counts: high-priority shifting feed configuration is blocked")
	// ErrShiftingFeedConfigChanged means the config no longer matches what the operator saw.
	ErrShiftingFeedConfigChanged = errors.New("counts: high-priority shifting feed configuration changed")
	// ErrShiftingExecutionIncomplete is returned when a completion's relocation did not cover every
	// animal the movement named (one was exited, merged, or moved to another tenant). The
	// completion transaction is rolled back, so the movement stays authorized for a human rather
	// than half-applying.
	ErrShiftingExecutionIncomplete = errors.New("counts: shifting completion did not relocate every named animal")

	// ErrGoatNotFound is returned when a goat id named by a shifting request does not resolve to a
	// non-merged animal in the caller's tenant. It fails the write CLOSED rather than
	// deriving an impact for a subset: a movement whose animal cannot be read is a movement whose
	// destination shed and vaccination obligations would silently disagree with the reported count.
	ErrGoatNotFound = errors.New("counts: goat not found")
	// ErrGoatNotShiftable is returned when the goat exists in the tenant but is no longer a
	// current herd member (dead, sold, transferred, or otherwise exited). Unlike ErrGoatNotFound,
	// this is an actionable eligibility rejection rather than a missing resource.
	ErrGoatNotShiftable = errors.New("counts: goat is not eligible for shifting")

	// ErrPenReconciliationCardNotFound is returned when the addressed reconciliation card does
	// not exist in the caller's tenant.
	ErrPenReconciliationCardNotFound = errors.New("counts: pen reconciliation card not found")
	// ErrPenReconciliationNotActionable is returned when a completion addresses a card that is
	// not open/rework -- already submitted or already completed. A replay of the SAME completion
	// (same idempotency key) is not an error; it echoes the original result.
	ErrPenReconciliationNotActionable = errors.New("counts: pen reconciliation card is not in an actionable state")
	// ErrPenReconciliationProofRequired is returned when an operator submits a reconciliation
	// card without the mandatory return video. The verifier reviews that video; a submission
	// with no video has nothing to verify and is rejected before any state changes.
	ErrPenReconciliationProofRequired = errors.New("counts: pen reconciliation completion requires a video proof")
)

// PenReconciliationRepository owns the Reconcile card store. It is a separate interface from
// Repository so existing fakes keep compiling; the postgres Repository implements both.
type PenReconciliationRepository interface {
	// RaisePenReconciliationCards inserts one card per mismatched live animal scanned in the
	// named submitted weighing bucket, skipping animals that already carry a non-completed
	// card. It returns how many cards were inserted and is idempotent across duplicate event
	// deliveries.
	RaisePenReconciliationCards(ctx context.Context, in domain.PenReconciliationRaiseCommand) (int, error)

	// ListPenReconciliationCards returns one keyset page of the Reconcile queue plus
	// whole-filter status counts.
	ListPenReconciliationCards(ctx context.Context, q domain.PenReconciliationQuery) (domain.PenReconciliationPage, error)

	// CompletePenReconciliationCard stores the operator's mandatory return video and flips the
	// card open/rework -> pending_verification. The bool result reports an idempotent replay.
	CompletePenReconciliationCard(ctx context.Context, in domain.PenReconciliationCompletionCommand) (domain.PenReconciliationCompletionResult, bool, error)

	// MarkPenReconciliationVerificationEnqueued clears the durable retry marker after the
	// verifier item has been created or idempotently replayed. If this write fails, a later exact
	// completion replay sees the marker and retries the enqueue.
	MarkPenReconciliationVerificationEnqueued(ctx context.Context, tenantID, cardID string) error

	// ApplyVerifiedPenReconciliation flips a submitted card to completed on verifier approve.
	ApplyVerifiedPenReconciliation(ctx context.Context, in domain.PenReconciliationVerdictCommand) error

	// BouncePenReconciliationForRework flips a submitted card to rework on verifier reject,
	// recording the reason. The operator re-shoots and submits again.
	BouncePenReconciliationForRework(ctx context.Context, in domain.PenReconciliationVerdictCommand) error
}

type Repository interface {
	RecordBaseCountAnchor(ctx context.Context, in domain.BaseCountAnchor) (id string, replay bool, err error)
	RecordShiftingEvent(ctx context.Context, in domain.ShiftingEvent) (id string, replay bool, err error)
	ScanCountMismatches(ctx context.Context, req domain.CountMismatchScanRequest) (domain.CountMismatchScanResult, error)
	BeginProjectionRecomputeRun(ctx context.Context, req domain.ProjectionRecomputeRequest) (runID string, err error)
	FinishProjectionRecomputeRun(ctx context.Context, runID string, result domain.ProjectionRecomputeResult, recomputeErr error) error
	ProjectionInputs(ctx context.Context, req domain.ProjectionRecomputeRequest) (domain.ProjectionInputs, error)
	CreateProjectionSnapshot(ctx context.Context, in domain.ProjectionSnapshot) (id string, err error)
	CountAsOf(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error)
	ProjectedCountFor(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error)
	ListProjectionExceptions(ctx context.Context, req domain.ProjectionExceptionQuery) (domain.ProjectionExceptionList, error)
	ResolveProjectionException(ctx context.Context, in domain.ProjectionExceptionResolutionRequest) (domain.ProjectionExceptionResolution, error)
	Readiness(ctx context.Context, tenantID string) (domain.Readiness, error)
	GetHerdRegisterSummary(ctx context.Context, req domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error)
	GetCountsBreakdown(ctx context.Context, req domain.CountsBreakdownQuery) (domain.CountsBreakdown, error)
	GetMilkPreparation(ctx context.Context, req domain.MilkPreparationQuery) (domain.MilkPreparationPage, error)

	// ProjectedShedCountsForFeed returns what each shed grain will hold on a feed day, computed
	// from the LIVE herd plus the movements that are approved but not yet executed.
	//
	// Parallel to ProjectedCountFor/CountAsOf above, not a replacement for them: those replay
	// movements over a physically-counted anchor and stay in use by the counts-source import and
	// parity tooling. This one never reads count_base_anchors or count_projection_snapshots.
	ProjectedShedCountsForFeed(ctx context.Context, req domain.FeedProjectedCountQuery) (domain.FeedProjectedCounts, error)

	// CreateApprovalRequest persists a PENDING lifecycle request. It applies nothing: a submitted
	// birth creates no goats row and emits no goat.created. Idempotent -- an exact replay returns
	// the original request with replay=true, a same-key/different-payload replay is a conflict.
	CreateApprovalRequest(ctx context.Context, in domain.ApprovalRequestSubmission) (domain.ApprovalRequest, bool, error)

	// GetApprovalRequest reads one request so the caller can prepare the right side effect for its
	// type before deciding.
	GetApprovalRequest(ctx context.Context, tenantID, approvalRequestID string) (domain.ApprovalRequest, error)

	// ListApprovalRequests returns one keyset page of requests, restricted to the types the caller
	// may decide.
	ListApprovalRequests(ctx context.Context, q domain.ApprovalRequestQuery) (domain.ApprovalRequestPage, error)

	// ShiftingDestinationCatalog returns the active park -> shed option tree an operator picks a
	// shifting destination from, ordered for a stable dropdown. Bounded config catalog, not a feed.
	ShiftingDestinationCatalog(ctx context.Context, tenantID string) (domain.ShiftingDestinationCatalog, error)

	// ActiveBreeds lists the breeds present on the tenant's live herd (key=label=goats.breed, with a
	// head count, most-common first) for the operator birth form's breed picker. Same source/grain as
	// the Counts Breakdown breed facet, served on the operator surface. Bounded, not a feed.
	ActiveBreeds(ctx context.Context, tenantID string) ([]domain.CountsBreakdownSeriesPoint, error)

	// GoatShiftingFacts reads the narrow breed/stage/sex facts needed to derive a shifting impact
	// for the named animals. It is READ-ONLY: counts never writes goats. It must return exactly one
	// fact per requested id or the caller fails closed -- see ErrGoatNotFound.
	GoatShiftingFacts(ctx context.Context, tenantID string, goatIDs []string) ([]domain.GoatShiftingFact, error)

	// DecideApprovalRequest flips the request's status and applies the decision's effect in ONE
	// transaction. An approved request can therefore never be readable while its effect failed to
	// save. A reject applies no effect.
	//
	// For SHIFTING, approval stores the Park Head gate. If operator completion already exists, this
	// same transaction applies the movement; otherwise it moves no animals.
	DecideApprovalRequest(ctx context.Context, in domain.ApprovalDecision) (domain.ApprovalRequest, bool, error)

	// CompleteShiftingEvent stores mandatory video and the operator gate. If Park Head approval is
	// already stored, it applies the movement atomically; otherwise it waits without moving census.
	//
	// Idempotent: an exact replay of an already-submitted (or already-applied) movement returns the
	// original result; a same-key/different-payload replay is ErrIdempotencyConflict.
	CompleteShiftingEvent(ctx context.Context, in domain.ShiftingCompletionCommand) (domain.ShiftingExecutionResult, bool, error)

	// ApplyVerifiedShiftingEvent is the legacy-named evidence approval hook. New rows only change
	// verification_state; a pre-000049 authorized+completed row may be applied once for rollout.
	ApplyVerifiedShiftingEvent(ctx context.Context, in domain.ShiftingVerifiedApplyCommand) (domain.ShiftingExecutionResult, bool, error)

	// BounceShiftingEventForRework marks evidence rejected and reopens proof submission. It preserves
	// event_status, goat location, and count.
	BounceShiftingEventForRework(ctx context.Context, in domain.ShiftingReworkCommand) error

	// CancelShiftingEvent retires an authorized movement that will never be executed. It moves
	// NOTHING and records the required reason. Idempotent on the same terms as completion.
	CancelShiftingEvent(ctx context.Context, in domain.ShiftingCancellationCommand) (domain.ShiftingExecutionResult, bool, error)

	// ListShiftingEventsPendingExecution returns one keyset page of Actions. The work list carries
	// approved, evidence-rework and completed movements; an unapproved one is reachable only through
	// the read-only 'pending' bucket, and an approved one is held until its lead time elapses
	// (see counts/domain.ShiftingActionsDueFrom).
	ListShiftingEventsPendingExecution(ctx context.Context, q domain.ShiftingExecutionQuery) (domain.ShiftingExecutionPage, error)
}

// MilkPreparationCompletionStore owns the shed-day preparation verification state. Keeping this
// write slice separate from Repository means read-only Counts consumers do not gain a mutation
// dependency merely because milk preparation is visible in the Counts read model.
type MilkPreparationCompletionStore interface {
	SubmitMilkPreparation(ctx context.Context, in domain.MilkPreparationSubmission) (domain.MilkPreparationSubmissionResult, error)
	ApplyVerifiedMilkPreparation(ctx context.Context, in domain.MilkPreparationVerdictCommand) (bool, error)
	BounceMilkPreparationForRework(ctx context.Context, in domain.MilkPreparationVerdictCommand) (bool, error)
}

type MilkFeedingStore interface {
	MaterializeMilkFeedingTasks(ctx context.Context, in domain.MilkFeedingMaterializeRequest) (domain.MilkFeedingMaterializeResult, error)
	ListMilkFeedingTasks(ctx context.Context, in domain.MilkFeedingQuery) (domain.MilkFeedingPage, error)
	SubmitMilkFeeding(ctx context.Context, in domain.MilkFeedingSubmission) (domain.MilkFeedingSubmissionResult, error)
	ApplyVerifiedMilkFeeding(ctx context.Context, in domain.MilkFeedingVerdictCommand) (bool, error)
	BounceMilkFeedingForRework(ctx context.Context, in domain.MilkFeedingVerdictCommand) (bool, error)
}

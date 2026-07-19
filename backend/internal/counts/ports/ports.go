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

	// ErrShiftingEventNotFound is returned when the addressed shifting event does not exist in the
	// caller's tenant.
	ErrShiftingEventNotFound = errors.New("counts: shifting event not found")
	// ErrShiftingNotAuthorized is returned when a completion or cancellation addresses a movement
	// that is not in a state the transition may start from -- a pending (unapproved) movement, or
	// one already rejected/canceled. It is NOT returned for a replay of a transition that already
	// succeeded, which is a no-op success.
	ErrShiftingNotAuthorized = errors.New("counts: shifting event is not in an executable state")
	// ErrShiftingExecutionIncomplete is returned when a completion's relocation did not cover every
	// animal the movement named (one was exited, merged, or moved to another tenant). The
	// completion transaction is rolled back, so the movement stays authorized for a human rather
	// than half-applying.
	ErrShiftingExecutionIncomplete = errors.New("counts: shifting completion did not relocate every named animal")

	// ErrGoatNotFound is returned when a goat id named by a shifting request does not resolve to a
	// live, non-merged animal in the caller's tenant. It fails the write CLOSED rather than
	// deriving an impact for a subset: a movement whose animal cannot be read is a movement whose
	// destination shed and vaccination obligations would silently disagree with the reported count.
	ErrGoatNotFound = errors.New("counts: goat not found")
)

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

	// GoatShiftingFacts reads the narrow breed/stage/sex facts needed to derive a shifting impact
	// for the named animals. It is READ-ONLY: counts never writes goats. It must return exactly one
	// fact per requested id or the caller fails closed -- see ErrGoatNotFound.
	GoatShiftingFacts(ctx context.Context, tenantID string, goatIDs []string) ([]domain.GoatShiftingFact, error)

	// DecideApprovalRequest flips the request's status and applies the decision's effect in ONE
	// transaction. An approved request can therefore never be readable while its effect failed to
	// save. A reject applies no effect.
	//
	// For a SHIFTING request the effect is AUTHORIZATION ONLY -- it moves no animals. The
	// relocation happens later, in CompleteShiftingEvent.
	DecideApprovalRequest(ctx context.Context, in domain.ApprovalDecision) (domain.ApprovalRequest, bool, error)

	// CompleteShiftingEvent executes an AUTHORIZED movement: it relocates the animals the movement
	// named and flips event_status to 'applied', stamped with who confirmed it and when, in ONE
	// transaction. A relocation that cannot cover every named animal rolls the whole completion
	// back, so an 'applied' row with unmoved animals is unreachable.
	//
	// Idempotent: an exact replay returns the original result and relocates nobody a second time; a
	// same-key/different-payload replay is ErrIdempotencyConflict.
	CompleteShiftingEvent(ctx context.Context, in domain.ShiftingCompletionCommand) (domain.ShiftingExecutionResult, bool, error)

	// CancelShiftingEvent retires an authorized movement that will never be executed. It moves
	// NOTHING and records the required reason. Idempotent on the same terms as completion.
	CancelShiftingEvent(ctx context.Context, in domain.ShiftingCancellationCommand) (domain.ShiftingExecutionResult, bool, error)

	// ListShiftingEventsPendingExecution returns one keyset page of AUTHORIZED movements waiting to
	// be physically executed.
	ListShiftingEventsPendingExecution(ctx context.Context, q domain.ShiftingExecutionQuery) (domain.ShiftingExecutionPage, error)
}

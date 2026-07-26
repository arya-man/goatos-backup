package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

var (
	// ErrShiftingCancelReasonRequired is returned when a cancellation arrives without a reason.
	ErrShiftingCancelReasonRequired = errors.New("counts: a reason is required to cancel a shifting")
	// ErrVerificationEnqueuerNotWired is returned when a completion cannot enqueue its verification
	// item because the enqueue seam was never wired -- a composition bug, surfaced loudly rather than
	// silently stranding a pending_verification movement.
	ErrVerificationEnqueuerNotWired = errors.New("counts: shifting verification enqueuer is not wired")
	// ErrInvalidShiftingExecutionFilter is returned for a malformed cursor or park filter on the
	// pending-execution queue.
	ErrInvalidShiftingExecutionFilter = errors.New("counts: invalid pending-execution filter")
)

// ShiftingExecutionService owns the post-authorization half of a movement: complete, cancel, and
// the operator's "authorized, waiting to be walked" queue.
//
// It is deliberately SEPARATE from ApprovalService. Those are two different jobs done by two
// different people at two different times -- an approver authorizes from anywhere, an operator
// executes standing in a park -- and they are gated on different permissions
// (CountsApproveShifting vs CountsWrite). Folding them together is what produced the behaviour the
// 2026-07-19 decision retired, where pressing "approve" silently relocated a herd.
type ShiftingExecutionService struct {
	repo     ports.Repository
	now      func() time.Time
	enqueuer ShiftingVerificationEnqueuer
}

// NewShiftingExecutionService constructs the service. now may be nil (defaults to time.Now).
func NewShiftingExecutionService(repo ports.Repository, now func() time.Time) *ShiftingExecutionService {
	if now == nil {
		now = time.Now
	}
	return &ShiftingExecutionService{repo: repo, now: now}
}

// ShiftingVerificationEnqueuer enqueues the mandatory-video verification item for a submitted
// movement (maintainer decision, 2026-07-26). The composition layer adapts the verification module's
// CreateItem to this narrow port so counts never touches verification's tables directly.
type ShiftingVerificationEnqueuer interface {
	EnqueueShiftingMoveVerification(ctx context.Context, in ShiftingVerificationEnqueueRequest) error
}

// ShiftingVerificationEnqueueRequest is one shifting-move video handed to the verification queue.
type ShiftingVerificationEnqueueRequest struct {
	TenantID        string
	ShiftingEventID string
	OperatorID      string
	ParkID          string
	ShedID          string
	ProofRef        string
	SubjectLabel    string
	CapturedAt      time.Time
	IdempotencyKey  string
}

// WithVerificationEnqueuer wires the verification enqueue seam. Without it, Complete fails closed
// rather than flipping a movement to pending_verification with no verifier queue item.
func (s *ShiftingExecutionService) WithVerificationEnqueuer(enqueuer ShiftingVerificationEnqueuer) *ShiftingExecutionService {
	s.enqueuer = enqueuer
	return s
}

// CompleteInput is one "the animals actually moved" confirmation.
type CompleteShiftingInput struct {
	TenantID        string
	ShiftingEventID string

	// CompletedByUserID is ANY operator holding CountsWrite, not only the raiser (maintainer
	// decision, 2026-07-19). No same-actor check is made here, and that is intentional: the person
	// standing in the park when the animals walk is not reliably the person who typed the request,
	// and forcing the raiser to be present would push operators to complete movements they did not
	// witness just to clear the queue.
	CompletedByUserID string
	TraceID           string

	// ProofRef is the MANDATORY video the operator records to prove the move (maintainer decision,
	// 2026-07-26). A blank value is rejected with ports.ErrShiftingProofRequired; the move is applied
	// only after a verifier approves this video.
	ProofRef string

	// DestinationTag is the OPTIONAL destination management_stage (operational cohort) the moved
	// animals adopt. Required only when the destination shed is empty; derived server-side otherwise.
	DestinationTag string

	IdempotencyKey     string
	RequestFingerprint string
}

// Complete submits an authorized movement for verification: it records the operator's mandatory
// video and flips the movement to pending_verification. It relocates NOBODY -- the relocation runs
// on verifier approval (ApplyVerified).
func (s *ShiftingExecutionService) Complete(
	ctx context.Context, in CompleteShiftingInput,
) (domain.ShiftingExecutionResult, bool, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ShiftingEventID) == "" ||
		strings.TrimSpace(in.CompletedByUserID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.ShiftingExecutionResult{}, false, ErrMissingRequiredField
	}
	if strings.TrimSpace(in.ProofRef) == "" {
		return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingProofRequired
	}
	if s.enqueuer == nil {
		// Fail closed: without the verification queue seam a completion would flip a movement to
		// pending_verification with nothing for a verifier to act on, stranding the animals.
		return domain.ShiftingExecutionResult{}, false, ErrVerificationEnqueuerNotWired
	}
	result, replay, err := s.repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           in.TenantID,
		ShiftingEventID:    in.ShiftingEventID,
		CompletedByUserID:  in.CompletedByUserID,
		CompletedAt:        s.now().UTC(),
		TraceID:            in.TraceID,
		ProofRef:           strings.TrimSpace(in.ProofRef),
		DestinationTag:     strings.TrimSpace(in.DestinationTag),
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	})
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}

	// Enqueue the mandatory-video verification item. Only for a movement that is actually awaiting
	// verification (an already-applied replay has been verified and moved -- re-enqueuing would queue
	// a decided move). The enqueue is idempotent on the shifting event id, so a retry after a prior
	// enqueue failure heals rather than duplicates: the completion is not "done" for the operator
	// until the item is queued.
	if result.EventStatus == domain.ShiftingEventStatusPendingVerification {
		subject := "Shed move · " + strconv.Itoa(len(result.MovedGoatIDs)) + " animals"
		if enqErr := s.enqueuer.EnqueueShiftingMoveVerification(ctx, ShiftingVerificationEnqueueRequest{
			TenantID:        in.TenantID,
			ShiftingEventID: in.ShiftingEventID,
			OperatorID:      in.CompletedByUserID,
			ParkID:          result.DestinationParkID,
			ShedID:          result.DestinationShedID,
			ProofRef:        strings.TrimSpace(in.ProofRef),
			SubjectLabel:    subject,
			CapturedAt:      s.now().UTC(),
			// Keyed to the EVENT + its video so a retry collapses onto one queue item.
			IdempotencyKey: "counts-shifting-verification:" + in.ShiftingEventID + ":" + strings.TrimSpace(in.ProofRef),
		}); enqErr != nil {
			return domain.ShiftingExecutionResult{}, false, enqErr
		}
	}
	return result, replay, nil
}

// CancelShiftingInput retires an authorized movement that will never be executed.
type CancelShiftingInput struct {
	TenantID        string
	ShiftingEventID string

	CanceledByUserID string
	Reason           string

	IdempotencyKey     string
	RequestFingerprint string
}

// Cancel retires an authorized movement. It moves NOTHING.
func (s *ShiftingExecutionService) Cancel(
	ctx context.Context, in CancelShiftingInput,
) (domain.ShiftingExecutionResult, bool, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ShiftingEventID) == "" ||
		strings.TrimSpace(in.CanceledByUserID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.ShiftingExecutionResult{}, false, ErrMissingRequiredField
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return domain.ShiftingExecutionResult{}, false, ErrShiftingCancelReasonRequired
	}
	if len(reason) > domain.MaxShiftingCancelReasonLength {
		return domain.ShiftingExecutionResult{}, false, ErrInvalidJSON
	}
	return s.repo.CancelShiftingEvent(ctx, domain.ShiftingCancellationCommand{
		TenantID:           in.TenantID,
		ShiftingEventID:    in.ShiftingEventID,
		CanceledByUserID:   in.CanceledByUserID,
		CanceledAt:         s.now().UTC(),
		Reason:             reason,
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	})
}

// ListPendingExecution returns one keyset page of authorized movements waiting to be executed.
//
// OPERATOR -> PARK SCOPE: THE SWAP POINT.
//
// The owner asked for "all approved in their park". There is no per-operator park scope in the data
// today -- workforce_members.primary_location_id is NULL for every member, and
// workforce_positions / workforce_roster_assignments are both empty -- so deriving the filter from
// the caller would hand every operator in the tenant an empty queue and make the feature look
// broken rather than unscoped. The park is therefore an OPTIONAL client filter for now, defaulting
// to every park.
//
// When that roster data lands, the change is confined to THIS METHOD and its one caller:
//
//  1. give the service a roster/scope port (an interface with something like
//     ParkIDsForOperator(ctx, tenantID, userID) ([]string, error)) via the constructor;
//  2. here, resolve the caller's parks and treat sourceParkID as a NARROWING filter WITHIN that
//     set -- an empty sourceParkID means "all MY parks", and a sourceParkID outside the set is a
//     403 rather than a silent empty page;
//  3. widen domain.ShiftingExecutionQuery.SourceParkID to a SourceParkIDs slice and change the
//     adapter's `source_park_id = $2` to `source_park_id = ANY($2::uuid[])`
//     (shifting_events_pending_execution_park_idx already supports it);
//  4. add the caller's user id to the ListPendingExecution signature -- the HTTP handler already
//     has it as ActorIDFromContext.
//
// Nothing outside this method, the query struct, and that one SQL predicate has to change, because
// the filter never leaks into the transitions: completion and cancellation address a movement by
// id and are authorized by permission, not by park.
func (s *ShiftingExecutionService) ListPendingExecution(
	ctx context.Context, tenantID, sourceParkID, sourceShedID string, pageSize int, cursor string,
) (domain.ShiftingExecutionPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ShiftingExecutionPage{}, ErrMissingRequiredField
	}
	decoded, err := domain.DecodeShiftingExecutionCursor(cursor)
	if err != nil {
		return domain.ShiftingExecutionPage{}, ErrInvalidShiftingExecutionFilter
	}
	return s.repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID:     tenantID,
		SourceParkID: strings.TrimSpace(sourceParkID),
		SourceShedID: strings.TrimSpace(sourceShedID),
		PageSize:     pageSize,
		Cursor:       decoded,
	})
}

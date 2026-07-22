package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

var (
	// ErrShiftingCancelReasonRequired is returned when a cancellation arrives without a reason.
	ErrShiftingCancelReasonRequired = errors.New("counts: a reason is required to cancel a shifting")
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
	repo ports.Repository
	now  func() time.Time
}

// NewShiftingExecutionService constructs the service. now may be nil (defaults to time.Now).
func NewShiftingExecutionService(repo ports.Repository, now func() time.Time) *ShiftingExecutionService {
	if now == nil {
		now = time.Now
	}
	return &ShiftingExecutionService{repo: repo, now: now}
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

	// DestinationTag is the OPTIONAL destination management_stage (operational cohort) the moved
	// animals adopt. Required only when the destination shed is empty; derived server-side otherwise.
	DestinationTag string

	IdempotencyKey     string
	RequestFingerprint string
}

// Complete executes an authorized movement, relocating its animals atomically with the status flip.
func (s *ShiftingExecutionService) Complete(
	ctx context.Context, in CompleteShiftingInput,
) (domain.ShiftingExecutionResult, bool, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ShiftingEventID) == "" ||
		strings.TrimSpace(in.CompletedByUserID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.ShiftingExecutionResult{}, false, ErrMissingRequiredField
	}
	return s.repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           in.TenantID,
		ShiftingEventID:    in.ShiftingEventID,
		CompletedByUserID:  in.CompletedByUserID,
		CompletedAt:        s.now().UTC(),
		TraceID:            in.TraceID,
		DestinationTag:     strings.TrimSpace(in.DestinationTag),
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	})
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

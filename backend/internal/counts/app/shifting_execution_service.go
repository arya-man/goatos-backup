package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

var (
	// ErrShiftingCancelReasonRequired is returned when a cancellation arrives without a reason.
	ErrShiftingCancelReasonRequired = errors.New("counts: a reason is required to cancel a shifting")
	// ErrVerificationEnqueuerNotWired is returned when a completion cannot enqueue its verification
	// item because the enqueue seam was never wired -- a composition bug, surfaced loudly rather than
	// silently stranding a pending_verification movement.
	ErrVerificationEnqueuerNotWired = errors.New("counts: shifting verification enqueuer is not wired")
	// ErrInvalidShiftingExecutionFilter is returned for a malformed cursor, business date, or status.
	ErrInvalidShiftingExecutionFilter = errors.New("counts: invalid pending-execution filter")
)

const (
	ShiftingActionStatusAll        = "all"
	ShiftingActionStatusPending    = "pending"
	ShiftingActionStatusAuthorized = "authorized"
	ShiftingActionStatusRework     = "rework"
	ShiftingActionStatusCompleted  = "completed"
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

// WithVerificationEnqueuer wires the evidence-review enqueue seam. Without it, Complete fails
// closed rather than accepting operator evidence that can never reach the verifier queue.
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
	// 2026-07-26). A blank value is rejected with ports.ErrShiftingProofRequired; verification reviews
	// the video independently of the approval + completion apply gate.
	ProofRef string

	// DestinationTag is the OPTIONAL destination management_stage (operational cohort) the moved
	// animals adopt. Required only when the destination shed is empty; derived server-side otherwise.
	DestinationTag string

	IdempotencyKey     string
	RequestFingerprint string
}

// Complete records the operator gate and mandatory evidence. If Park Head approval already exists,
// the repository atomically applies the movement now; otherwise approval will apply it later.
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
		// Fail closed: without the verification queue seam the mandatory evidence would have no
		// review path. This does not make verification an apply gate: the approval + completion
		// transaction still owns relocation and the census change.
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
	if result.EventStatus == domain.ShiftingEventStatusPending ||
		result.EventStatus == domain.ShiftingEventStatusPendingVerification ||
		result.EventStatus == domain.ShiftingEventStatusApplied {
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

// ListPendingExecution returns one keyset page of date-scoped Shifting Actions history. Date is an
// Asia/Kolkata business day and status buckets are disjoint backend-owned workflow states. Farm and
// shed are deliberately not list filters: each row already identifies its source and destination.
func (s *ShiftingExecutionService) ListPendingExecution(
	ctx context.Context, tenantID, businessDate, status string, pageSize int, cursor string,
) (domain.ShiftingExecutionPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ShiftingExecutionPage{}, ErrMissingRequiredField
	}
	decoded, err := domain.DecodeShiftingExecutionCursor(cursor)
	if err != nil {
		return domain.ShiftingExecutionPage{}, ErrInvalidShiftingExecutionFilter
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = ShiftingActionStatusAll
	}
	if status != ShiftingActionStatusAll && status != ShiftingActionStatusPending &&
		status != ShiftingActionStatusAuthorized && status != ShiftingActionStatusRework &&
		status != ShiftingActionStatusCompleted {
		return domain.ShiftingExecutionPage{}, ErrInvalidShiftingExecutionFilter
	}
	var raisedFrom, raisedBefore *time.Time
	if strings.TrimSpace(businessDate) != "" {
		date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(businessDate), biztime.DefaultLocation())
		if err != nil {
			return domain.ShiftingExecutionPage{}, ErrInvalidShiftingExecutionFilter
		}
		from := date.UTC()
		before := date.AddDate(0, 0, 1).UTC()
		raisedFrom, raisedBefore = &from, &before
	}
	return s.repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID:     tenantID,
		RaisedFrom:   raisedFrom,
		RaisedBefore: raisedBefore,
		Status:       status,
		PageSize:     pageSize,
		Cursor:       decoded,
	})
}

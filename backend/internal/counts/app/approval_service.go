package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
)

var (
	// ErrApprovalForbiddenType is returned when a caller tries to decide a request type their role
	// does not own (e.g. a park_head deciding a birth).
	ErrApprovalForbiddenType = errors.New("counts: caller may not decide this request type")
	// ErrApprovalForbiddenScope is returned when a caller tries to decide a request outside their
	// scope (e.g. a park_head approving a movement in a park they don't manage).
	ErrApprovalForbiddenScope = errors.New("counts: caller scope does not include this request")
	// ErrApprovalReasonRequired is returned when a reject arrives without a reason.
	ErrApprovalReasonRequired = errors.New("counts: a reason is required to reject a request")
	// ErrApprovalInvalidStoredPayload is returned when a stored request payload can no longer be
	// replayed through its owning module (for example the animal it names has since been merged).
	ErrApprovalInvalidStoredPayload = errors.New("counts: stored approval payload is no longer applicable")
)

// GoatLifecyclePreparer is the slice of identity/app.Service the approval workflow needs to turn a
// stored payload back into a validated, ready-to-apply command WITHOUT applying it.
// *identityapp.Service satisfies it.
type GoatLifecyclePreparer interface {
	PrepareCreateAdminGoat(ctx context.Context, in identityapp.CreateAdminGoatInput) (identityports.CreateAdminGoatCommand, error)
	PrepareCriticalDeathExit(ctx context.Context, in identityapp.ExitGoatInput) (identityports.ExitGoatCommand, error)
}

// approvalSubjectParkReader is an optional narrow read seam implemented by the Postgres adapter.
// It lets a park-scoped manager decide a death only for a goat physically in that park.
type approvalSubjectParkReader interface {
	ApprovalSubjectPark(ctx context.Context, tenantID, goatID string) (string, error)
}

type birthApprovalSubmitter interface {
	CreateBirthApprovalRequest(
		ctx context.Context,
		in domain.ApprovalRequestSubmission,
		children []identityports.CreateAdminGoatCommand,
	) (domain.BirthSubmissionResult, error)
}

// ApprovalService owns the Counts lifecycle approval workflow: submit-as-pending, list, decide.
type ApprovalService struct {
	repo     ports.Repository
	preparer GoatLifecyclePreparer
	now      func() time.Time
}

// NewApprovalService constructs the workflow service. now may be nil (defaults to time.Now).
func NewApprovalService(repo ports.Repository, preparer GoatLifecyclePreparer, now func() time.Time) *ApprovalService {
	if now == nil {
		now = time.Now
	}
	return &ApprovalService{repo: repo, preparer: preparer, now: now}
}

// ---------------------------------------------------------------------------
// Submit
// ---------------------------------------------------------------------------

// SubmitRequest records a PENDING request. It applies nothing.
//
// The payload is validated by its owning module at SUBMIT time -- so an operator learns
// immediately that a dob is malformed or that a death is not a valid dead+died pairing -- but the
// resulting command is DISCARDED rather than executed. Preparing has no side effects, so validating
// here costs nothing but a read.
func (s *ApprovalService) SubmitRequest(ctx context.Context, in domain.ApprovalRequestSubmission) (domain.ApprovalRequest, bool, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.RaisedByUserID) == "" ||
		strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.ApprovalRequest{}, false, ErrMissingRequiredField
	}
	if !domain.ValidApprovalRequestType(in.RequestType) {
		return domain.ApprovalRequest{}, false, ErrMissingRequiredField
	}
	if in.RaisedAt.IsZero() {
		in.RaisedAt = s.now().UTC()
	}
	return s.repo.CreateApprovalRequest(ctx, in)
}

// SubmitBirthRequest atomically creates every canonical child and the independent web approval.
// The children are count-pending, but goat.created is emitted immediately for each child so their
// operational workflows start without waiting for the web queue.
func (s *ApprovalService) SubmitBirthRequest(
	ctx context.Context,
	in domain.ApprovalRequestSubmission,
	children []identityports.CreateAdminGoatCommand,
) (domain.BirthSubmissionResult, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.RaisedByUserID) == "" ||
		strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.RequestFingerprint) == "" ||
		in.RequestType != domain.ApprovalRequestTypeBirth || len(children) < 1 || len(children) > 3 {
		return domain.BirthSubmissionResult{}, ErrMissingRequiredField
	}
	if in.RaisedAt.IsZero() {
		in.RaisedAt = s.now().UTC()
	}
	repo, ok := s.repo.(birthApprovalSubmitter)
	if !ok {
		return domain.BirthSubmissionResult{}, fmt.Errorf("counts: birth submission repository is not wired")
	}
	return repo.CreateBirthApprovalRequest(ctx, in, children)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// ListPending returns one keyset page of requests, restricted to decidableTypes and scope.
//
// decidableTypes comes from the caller's permissions, not from the query string, so the pending
// list can only ever show work this approver is allowed to act on.
//
// P1: ListPending scope. Filter by the caller's park scope too, so a park_head sees only
// approvals in their managed park.
func (s *ApprovalService) ListPending(
	ctx context.Context, tenantID, status string, decidableTypes []string, callerParkID string, pageSize int, cursor string,
) (domain.ApprovalRequestPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ApprovalRequestPage{}, ErrMissingRequiredField
	}
	if status == "" {
		status = domain.ApprovalStatusPending
	}
	if !domain.ValidApprovalStatus(status) {
		return domain.ApprovalRequestPage{}, ErrInvalidExceptionFilter
	}
	decoded, err := domain.DecodeApprovalRequestCursor(cursor)
	if err != nil {
		return domain.ApprovalRequestPage{}, ErrInvalidExceptionFilter
	}
	return s.repo.ListApprovalRequests(ctx, domain.ApprovalRequestQuery{
		TenantID:     tenantID,
		Status:       status,
		RequestTypes: decidableTypes,
		CallerParkID: callerParkID,
		PageSize:     pageSize,
		Cursor:       decoded,
	})
}

// ---------------------------------------------------------------------------
// Decide
// ---------------------------------------------------------------------------

// DecisionInput is one approve/reject call.
type DecisionInput struct {
	TenantID          string
	ApprovalRequestID string
	Approve           bool
	Reason            string

	DecidedByUserID string
	TraceID         string

	IdempotencyKey     string
	RequestFingerprint string

	// DecidableTypes is the set of request types the CALLER may decide, derived from their
	// permissions. The request's own type is checked against it before anything is applied, so a
	// park_head cannot approve a birth even by addressing its id directly.
	DecidableTypes []string

	// P0-2: CallerParkID is the park scope the caller may decide requests in. Only requests
	// targeting this park are approvable. Empty string means no scope restriction (e.g. CEO/internal).
	CallerParkID string
}

// Decide approves or rejects a request.
//
// Authority is checked against the request's STORED TYPE, not against the route: the decision
// endpoints are one route pair, so the type-to-permission mapping has to be enforced here, after
// the row is read.
//
// For an approve, the stored payload is re-prepared through its owning module (producing a freshly
// validated command) and handed to the repository, which applies it in the SAME transaction as the
// status flip. A reject prepares and applies nothing.
func (s *ApprovalService) Decide(ctx context.Context, in DecisionInput) (domain.ApprovalRequest, bool, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ApprovalRequestID) == "" ||
		strings.TrimSpace(in.DecidedByUserID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" {
		return domain.ApprovalRequest{}, false, ErrMissingRequiredField
	}
	reason := strings.TrimSpace(in.Reason)
	if !in.Approve && reason == "" {
		return domain.ApprovalRequest{}, false, ErrApprovalReasonRequired
	}
	if len(reason) > domain.MaxApprovalDecisionReasonLength {
		return domain.ApprovalRequest{}, false, ErrInvalidJSON
	}

	req, err := s.repo.GetApprovalRequest(ctx, in.TenantID, in.ApprovalRequestID)
	if err != nil {
		return domain.ApprovalRequest{}, false, err
	}
	if !containsString(in.DecidableTypes, req.RequestType) {
		return domain.ApprovalRequest{}, false, ErrApprovalForbiddenType
	}

	// P0-2: Scope escalation prevention. Check that the caller's park scope includes the request's park.
	// Shifting approval: check destination_park_id.
	// Birth/Death: check the goat's park (which would need to be read from the payload or subject_goat_id).
	// For now, shifting has the park in the payload; for birth/death, we check against the subject goat.
	if in.CallerParkID != "" {
		switch req.RequestType {
		case domain.ApprovalRequestTypeShifting:
			// The destination_park_id lives in the payload and (by P0-1) equals the source park.
			// A scoped caller may decide the request only if that park is their scope. Fail CLOSED:
			// an empty or unparseable payload means we cannot prove scope, so we deny rather than
			// silently allow a park head to act on a request we could not attribute to their park.
			var shiftPayload shiftingApprovalPayload
			if len(req.Payload) == 0 || json.Unmarshal(req.Payload, &shiftPayload) != nil ||
				shiftPayload.DestinationParkID == "" || shiftPayload.DestinationParkID != in.CallerParkID {
				return domain.ApprovalRequest{}, false, ErrApprovalForbiddenScope
			}
		case domain.ApprovalRequestTypeBirth:
			// Births don't target a specific park in the payload; they're tenant-wide.
			// Only CEO/internal (no park scope) may decide births — a scoped caller is denied.
			return domain.ApprovalRequest{}, false, ErrApprovalForbiddenScope
		case domain.ApprovalRequestTypeDeath:
			if req.SubjectGoatID == nil || *req.SubjectGoatID == "" {
				return domain.ApprovalRequest{}, false, ErrApprovalForbiddenScope
			}
			reader, ok := s.repo.(approvalSubjectParkReader)
			if !ok {
				return domain.ApprovalRequest{}, false, ErrApprovalForbiddenScope
			}
			parkID, err := reader.ApprovalSubjectPark(ctx, req.TenantID, *req.SubjectGoatID)
			if err != nil || parkID == "" || parkID != in.CallerParkID {
				return domain.ApprovalRequest{}, false, ErrApprovalForbiddenScope
			}
		}
	}

	decision := domain.ApprovalDecision{
		TenantID:           in.TenantID,
		ApprovalRequestID:  in.ApprovalRequestID,
		DecidedByUserID:    in.DecidedByUserID,
		DecidedAt:          s.now().UTC(),
		Reason:             reason,
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	}
	if !in.Approve {
		decision.Status = domain.ApprovalStatusRejected
		return s.repo.DecideApprovalRequest(ctx, decision)
	}

	decision.Status = domain.ApprovalStatusApproved
	// Short-circuit: if the request is already decided there is nothing to prepare, and preparing
	// anyway would burn the idempotency key of a command that will never run.
	if req.Status != domain.ApprovalStatusPending {
		return s.repo.DecideApprovalRequest(ctx, decision)
	}
	effect, err := s.prepareEffect(ctx, req, in)
	if err != nil {
		return domain.ApprovalRequest{}, false, err
	}
	decision.Effect = effect
	return s.repo.DecideApprovalRequest(ctx, decision)
}

// prepareEffect turns a stored request back into a validated, ready-to-apply command.
func (s *ApprovalService) prepareEffect(
	ctx context.Context, req domain.ApprovalRequest, in DecisionInput,
) (*domain.ApprovalEffect, error) {
	switch req.RequestType {
	case domain.ApprovalRequestTypeBirth:
		return &domain.ApprovalEffect{BirthCounts: &domain.BirthCountsApprovalEffect{
			BirthEventID: req.ApprovalRequestID,
		}}, nil

	case domain.ApprovalRequestTypeDeath:
		if s.preparer == nil {
			return nil, fmt.Errorf("counts: approve death: goat lifecycle preparer is not wired")
		}
		goatID, body, err := splitDeathPayload(req)
		if err != nil {
			return nil, err
		}
		// PrepareCriticalDeathExit runs validateCriticalDeathExit, so the dead+died guardrail is
		// enforced on the approval path exactly as on the direct route.
		cmd, err := s.preparer.PrepareCriticalDeathExit(ctx, identityapp.ExitGoatInput{
			TenantID:       req.TenantID,
			ActorID:        in.DecidedByUserID,
			IdempotencyKey: approvalEffectIdempotencyKey(req),
			TraceID:        in.TraceID,
			GoatID:         goatID,
			RawBody:        body,
		})
		if err != nil {
			return nil, err
		}
		return &domain.ApprovalEffect{ExitGoat: cmd}, nil

	case domain.ApprovalRequestTypeShifting:
		if req.ShiftingEventID == nil || *req.ShiftingEventID == "" {
			return nil, ErrApprovalInvalidStoredPayload
		}
		effect, err := decodeShiftingApprovalPayload(req)
		if err != nil {
			return nil, err
		}
		return &domain.ApprovalEffect{Shifting: effect}, nil

	default:
		return nil, ErrApprovalInvalidStoredPayload
	}
}

// approvalEffectIdempotencyKey is the stable key under which an approved request's effect is
// written. It is a function of the request id alone, so every approve attempt for that request --
// however many times it is retried, and by whichever approver -- resolves to the same identity
// write and can never produce a second goat.
func approvalEffectIdempotencyKey(req domain.ApprovalRequest) string {
	return "counts-approval-" + req.RequestType + ":" + req.ApprovalRequestID
}

// shiftingApprovalPayload is the descriptor stored on a shifting approval request.
type shiftingApprovalPayload struct {
	ShiftingEventID   string   `json:"shifting_event_id"`
	DestinationParkID string   `json:"destination_park_id"`
	DestinationShedID string   `json:"destination_shed_id"`
	GoatIDs           []string `json:"goat_ids"`
}

func decodeShiftingApprovalPayload(req domain.ApprovalRequest) (*domain.ShiftingApprovalEffect, error) {
	var payload shiftingApprovalPayload
	if len(req.Payload) > 0 {
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return nil, ErrApprovalInvalidStoredPayload
		}
	}
	if payload.DestinationParkID == "" || payload.DestinationShedID == "" {
		return nil, ErrApprovalInvalidStoredPayload
	}
	// goat_ids is REQUIRED at submit (see normalizeShiftingEventRequest), so a stored shifting
	// payload that names nobody is corrupt, not a legal count-only movement. Enforced on BOTH
	// layers rather than trusting the writer: approving a request whose animal set went missing
	// would authorize the movement while relocating nobody, leaving the herd register and the
	// shed-scoped vaccination obligations disagreeing with the count that was just approved.
	// This blocks APPROVE only -- Decide short-circuits reject before prepareEffect, so a corrupt
	// request can still be rejected and never becomes undecidable.
	if len(payload.GoatIDs) == 0 {
		return nil, ErrApprovalInvalidStoredPayload
	}
	if len(payload.GoatIDs) > identityports.MaxRelocateGoatsPerCommand {
		return nil, ErrApprovalInvalidStoredPayload
	}
	return &domain.ShiftingApprovalEffect{
		ShiftingEventID:   *req.ShiftingEventID,
		DestinationParkID: payload.DestinationParkID,
		DestinationShedID: payload.DestinationShedID,
		GoatIDs:           payload.GoatIDs,
	}, nil
}

// splitDeathPayload separates the addressing field (goat_id) from the body identity's
// ExitGoatRequest decodes strictly. The stored payload is the operator's submitted body, which
// carries goat_id inline because the app route has no {goat_id} path segment.
func splitDeathPayload(req domain.ApprovalRequest) (string, []byte, error) {
	if req.SubjectGoatID == nil || *req.SubjectGoatID == "" {
		return "", nil, ErrApprovalInvalidStoredPayload
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(req.Payload, &fields); err != nil {
		return "", nil, ErrApprovalInvalidStoredPayload
	}
	delete(fields, "goat_id")
	body, err := json.Marshal(fields)
	if err != nil {
		// stored payload corruption: unable to re-marshal death request after field removal
		return "", nil, fmt.Errorf("split death payload: json marshal failed (stored data corruption): %w", err)
	}
	return *req.SubjectGoatID, body, nil
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

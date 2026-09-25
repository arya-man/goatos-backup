package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
	// ErrInvalidApprovalTypeFilter / ErrInvalidApprovalParkFilter refuse an unknown list filter
	// value; a filter is never silently widened to "everything" (2026-09-25).
	ErrInvalidApprovalTypeFilter = errors.New("counts: request_type filter must be birth, death or shifting")
	ErrInvalidApprovalParkFilter = errors.New("counts: park_id filter must be a park id")
	// ErrInvalidApprovalDateRange refuses a calendar filter that is not a YYYY-MM-DD date, or whose
	// start is after its end; it is never read as "no filter".
	ErrInvalidApprovalDateRange = errors.New("counts: raised_from / raised_to must be dates (YYYY-MM-DD), from on or before to")
	// ErrApprovalCursorFilterMismatch refuses a cursor minted under a different filter.
	ErrApprovalCursorFilterMismatch = errors.New("counts: cursor does not belong to this filter")
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
	ctx context.Context, tenantID, status string, decidableTypes []string, callerParkIDs []string, pageSize int, cursor string,
) (domain.ApprovalRequestPage, error) {
	return s.ListFiltered(ctx, tenantID, status, decidableTypes, callerParkIDs, domain.ApprovalListFilter{}, pageSize, cursor)
}

// ListFiltered is ListPending with the client's optional type/farm filter applied SERVER-SIDE
// (2026-09-25). The filter only ever NARROWS: a type the caller may not decide reads empty (never
// widened), a farm outside the caller's park scope reads empty (both predicates apply), and an
// unknown value is refused. The cursor is bound to the filter it was minted under.
func (s *ApprovalService) ListFiltered(
	ctx context.Context, tenantID, status string, decidableTypes []string, callerParkIDs []string,
	filter domain.ApprovalListFilter, pageSize int, cursor string,
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
	filter.RequestType = strings.TrimSpace(filter.RequestType)
	filter.ParkID = strings.ToLower(strings.TrimSpace(filter.ParkID))
	if filter.RequestType != "" && !domain.ValidApprovalRequestType(filter.RequestType) {
		return domain.ApprovalRequestPage{}, ErrInvalidApprovalTypeFilter
	}
	if filter.ParkID != "" && !uuidutil.IsUUIDString(filter.ParkID) {
		return domain.ApprovalRequestPage{}, ErrInvalidApprovalParkFilter
	}
	filter.RaisedFrom = strings.TrimSpace(filter.RaisedFrom)
	filter.RaisedTo = strings.TrimSpace(filter.RaisedTo)
	raisedFrom, raisedBefore, err := approvalRaisedRange(filter.RaisedFrom, filter.RaisedTo)
	if err != nil {
		return domain.ApprovalRequestPage{}, err
	}
	decoded, err := domain.DecodeApprovalRequestCursor(cursor)
	if err != nil {
		return domain.ApprovalRequestPage{}, ErrInvalidExceptionFilter
	}
	if decoded != nil && decoded.Filter != filter.Key() {
		return domain.ApprovalRequestPage{}, ErrApprovalCursorFilterMismatch
	}
	types := decidableTypes
	if filter.RequestType != "" {
		types = nil
		for _, t := range decidableTypes {
			if t == filter.RequestType {
				types = []string{t}
				break
			}
		}
	}
	return s.repo.ListApprovalRequests(ctx, domain.ApprovalRequestQuery{
		TenantID:      tenantID,
		Status:        status,
		RequestTypes:  types,
		CallerParkIDs: callerParkIDs,
		FilterParkID:  filter.ParkID,
		RaisedFrom:    raisedFrom,
		RaisedBefore:  raisedBefore,
		FilterKey:     filter.Key(),
		PageSize:      pageSize,
		Cursor:        decoded,
	})
}

// approvalRaisedRange turns the calendar filter's inclusive YYYY-MM-DD dates into a half-open
// instant range over INDIA business days: from the start of the first day to the start of the
// day after the last. UTC never defines the day (a 02:00 IST request is on its own date).
func approvalRaisedRange(from, to string) (*time.Time, *time.Time, error) {
	loc := biztime.DefaultLocation()
	parse := func(raw string) (*time.Time, error) {
		if raw == "" {
			return nil, nil
		}
		day, err := time.ParseInLocation("2006-01-02", raw, loc)
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", ErrInvalidApprovalDateRange, raw, err)
		}
		return &day, nil
	}
	start, err := parse(from)
	if err != nil {
		return nil, nil, err
	}
	end, err := parse(to)
	if err != nil {
		return nil, nil, err
	}
	if start != nil && end != nil && start.After(*end) {
		return nil, nil, ErrInvalidApprovalDateRange
	}
	var before *time.Time
	if end != nil {
		next := end.AddDate(0, 0, 1)
		before = &next
	}
	return start, before, nil
}

// requestInCallerScope is the ONE farm-scope rule for a single request, shared by Decide and
// GetForCaller so reading and deciding can never disagree. A tenant-scoped caller (no parks) sees
// every farm. A park-scoped caller sees a request only when its farm is one of THEIR parks: a pen
// move's destination_park_id (== source park by P0-1), a birth's park_id, a death's subject animal
// (goats.park_id, the indexed authority read). Fail CLOSED: a farm we cannot prove is out of scope.
//
// Live E2E 2026-09-11 found both named approvers (park_head in BOTH parks) refused on every
// decision: the scope was ONE park (the first grant), births were denied to any scoped caller on
// the wrong premise that a birth carries no park, and the refusal surfaced as a 500.
func (s *ApprovalService) requestInCallerScope(ctx context.Context, req domain.ApprovalRequest, callerParkIDs []string) bool {
	if len(callerParkIDs) == 0 {
		return true
	}
	requestPark := ""
	switch req.RequestType {
	case domain.ApprovalRequestTypeShifting:
		var shiftPayload shiftingApprovalPayload
		if len(req.Payload) > 0 && json.Unmarshal(req.Payload, &shiftPayload) == nil {
			requestPark = strings.TrimSpace(shiftPayload.DestinationParkID)
		}
	case domain.ApprovalRequestTypeBirth:
		var birthPayload struct {
			ParkID string `json:"park_id"`
		}
		if len(req.Payload) > 0 && json.Unmarshal(req.Payload, &birthPayload) == nil {
			requestPark = strings.TrimSpace(birthPayload.ParkID)
		}
	case domain.ApprovalRequestTypeDeath:
		if req.SubjectGoatID != nil && *req.SubjectGoatID != "" {
			if reader, ok := s.repo.(approvalSubjectParkReader); ok {
				if parkID, err := reader.ApprovalSubjectPark(ctx, req.TenantID, *req.SubjectGoatID); err == nil {
					requestPark = strings.TrimSpace(parkID)
				}
			}
		}
	}
	return requestPark != "" && containsString(callerParkIDs, requestPark)
}

// GetForCaller reads ONE request for a caller, whatever its status or place in the queue, so a
// link to it (a Work Board row, a bookmark) opens it even when it is older than the first page
// (maintainer 2026-09-25). It applies exactly the list's authority: a type the caller may not
// decide, or a farm outside their scope, reads as NOT FOUND -- the request's existence is not
// disclosed -- and a malformed id is refused.
func (s *ApprovalService) GetForCaller(ctx context.Context, tenantID, approvalRequestID string, decidableTypes, callerParkIDs []string) (domain.ApprovalRequestSummary, error) {
	approvalRequestID = strings.ToLower(strings.TrimSpace(approvalRequestID))
	if strings.TrimSpace(tenantID) == "" || !uuidutil.IsUUIDString(approvalRequestID) {
		return domain.ApprovalRequestSummary{}, ErrInvalidExceptionFilter
	}
	req, err := s.repo.GetApprovalRequest(ctx, tenantID, approvalRequestID)
	if err != nil {
		return domain.ApprovalRequestSummary{}, err
	}
	if !containsString(decidableTypes, req.RequestType) || !s.requestInCallerScope(ctx, req, callerParkIDs) {
		return domain.ApprovalRequestSummary{}, ports.ErrApprovalRequestNotFound
	}
	return domain.ApprovalRequestSummary{
		ApprovalRequestID: req.ApprovalRequestID, RequestType: req.RequestType, Status: req.Status,
		RaisedByUserID: req.RaisedByUserID, RaisedAt: req.RaisedAt,
		ShiftingEventID: req.ShiftingEventID, SubjectGoatID: req.SubjectGoatID, Summary: req.Payload,
		DecidedByUserID: req.DecidedByUserID, DecidedAt: req.DecidedAt, DecisionReason: req.DecisionReason,
		Capture: req.Capture, CaptureReviewStatus: req.CaptureReviewStatus, CaptureReviewReason: req.CaptureReviewReason,
	}, nil
}

// CountPending is the number of PENDING requests this caller may decide -- the same decidable
// types and park scope the list applies, over the whole queue (never a page). It answers the
// phone's Approvals badge, so the badge equals what the queue lists.
func (s *ApprovalService) CountPending(ctx context.Context, tenantID string, decidableTypes, callerParkIDs []string) (int, error) {
	if strings.TrimSpace(tenantID) == "" || len(decidableTypes) == 0 {
		return 0, nil
	}
	return s.repo.CountPendingApprovalRequests(ctx, domain.ApprovalRequestQuery{
		TenantID: tenantID, Status: domain.ApprovalStatusPending,
		RequestTypes: decidableTypes, CallerParkIDs: callerParkIDs,
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

	// P0-2: CallerParkIDs is the park scope the caller may decide requests in: every park the
	// caller holds a park grant in. Only requests targeting one of these parks are approvable.
	// Empty means no scope restriction (a tenant-scoped caller such as the CEO).
	CallerParkIDs []string
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

	// P0-2: Scope escalation prevention. A park-scoped caller may decide a request only when the
	// request's park is one of THEIR parks. Every kind names its park: a pen move's
	// destination_park_id (== source park by P0-1), a birth's park_id, a death's subject animal
	// (goats.park_id, the indexed authority read). Fail CLOSED: a park we cannot prove is denied.
	//
	// Live E2E 2026-09-11 found both named approvers (park_head in BOTH parks) refused on every
	// decision: the scope was ONE park (the first grant), births were denied to any scoped
	// caller on the wrong premise that a birth carries no park, and the refusal surfaced as a 500.
	if !s.requestInCallerScope(ctx, req, in.CallerParkIDs) {
		return domain.ApprovalRequest{}, false, ErrApprovalForbiddenScope
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

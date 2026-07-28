package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Counts lifecycle approval endpoints.
//
//	GET  /app/counts/approvals?status=pending   -- the approver's queue, keyset-paginated
//	POST /app/counts/approvals/{request_id}/approve
//	POST /app/counts/approvals/{request_id}/reject
//
// Authority is enforced against the request's STORED TYPE, not the route: a decision addresses a
// request by ID, so the middleware cannot tell a birth from a shifting. permissions.
// DecidableApprovalRequestTypes turns the caller's roles into the set of types they may decide, the
// list is filtered to that set, and a decision on a type outside it is 403.

const (
	appApprovalsRoute       = "/app/counts/approvals"
	appApprovalApproveRoute = "/app/counts/approvals/{request_id}/approve"
	appApprovalRejectRoute  = "/app/counts/approvals/{request_id}/reject"

	appApprovalApproveCommand = "counts.app.approval_approve"
	appApprovalRejectCommand  = "counts.app.approval_reject"

	// The admin-web Approvals page is served by the SAME approval service and handler logic; only
	// the route prefix and the caller's session differ (maintainer decision 2026-07-21, after
	// approvals were removed from mobile). Authority is still enforced against the request's stored
	// TYPE via permissions.DecidableApprovalRequestTypes, and the coarse route gate is still
	// CountsApproveAccess -- now held by the four org tiers + admin + ceo_internal, not park_head.
	adminWebApprovalsRoute       = "/admin-web/counts/approvals"
	adminWebApprovalApproveRoute = "/admin-web/counts/approvals/{request_id}/approve"
	adminWebApprovalRejectRoute  = "/admin-web/counts/approvals/{request_id}/reject"

	adminWebApprovalApproveCommand = "counts.adminweb.approval_approve"
	adminWebApprovalRejectCommand  = "counts.adminweb.approval_reject"
)

// ApprovalWorkflow is the slice of counts/app.ApprovalService this handler needs.
type ApprovalWorkflow interface {
	SubmitRequest(ctx context.Context, in domain.ApprovalRequestSubmission) (domain.ApprovalRequest, bool, error)
	SubmitBirthRequest(ctx context.Context, in domain.ApprovalRequestSubmission, children []identityports.CreateAdminGoatCommand) (domain.BirthSubmissionResult, error)
	ListPending(ctx context.Context, tenantID, status string, decidableTypes []string, callerParkID string, pageSize int, cursor string) (domain.ApprovalRequestPage, error)
	Decide(ctx context.Context, in countsapp.DecisionInput) (domain.ApprovalRequest, bool, error)
}

// RegisterApprovals wires the mobile/app approval decision surface.
func RegisterApprovals(mux *http.ServeMux, h *AppWriteHandler) {
	mux.HandleFunc("GET "+appApprovalsRoute, h.ListApprovals)
	mux.HandleFunc("POST "+appApprovalApproveRoute, h.ApproveRequest)
	mux.HandleFunc("POST "+appApprovalRejectRoute, h.RejectRequest)
}

func approvalPageSize(w http.ResponseWriter, r *http.Request, h *AppWriteHandler) (int, bool) {
	pageSize := domain.MaxApprovalPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_page_size", "page_size must be a positive integer", nil)
			return 0, false
		}
		if parsed < pageSize {
			pageSize = parsed
		}
	}
	return pageSize, true
}

// RegisterAdminWebApprovals wires the admin-web Approvals page onto the SAME approval service and
// list/decide logic under the /admin-web/* prefix. The handler reads tenant, roles, and park scope
// from the request context, so it is identical whether the caller arrived via the app or admin-web
// session middleware; only the telemetry route/command labels differ.
func RegisterAdminWebApprovals(mux *http.ServeMux, h *AppWriteHandler) {
	mux.HandleFunc("GET "+adminWebApprovalsRoute, h.ListApprovals)
	mux.HandleFunc("POST "+adminWebApprovalApproveRoute, h.ApproveRequestAdminWeb)
	mux.HandleFunc("POST "+adminWebApprovalRejectRoute, h.RejectRequestAdminWeb)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

type appApprovalListResponse struct {
	Items      []appApprovalListItem `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

type appApprovalListItem struct {
	ApprovalRequestID string          `json:"approval_request_id"`
	RequestType       string          `json:"request_type"`
	Status            string          `json:"status"`
	RaisedByUserID    string          `json:"raised_by_user_id"`
	RaisedAt          time.Time       `json:"raised_at"`
	ShiftingEventID   *string         `json:"shifting_event_id,omitempty"`
	SubjectGoatID     *string         `json:"subject_goat_id,omitempty"`
	Summary           json.RawMessage `json:"summary"`
	DecidedByUserID   *string         `json:"decided_by_user_id,omitempty"`
	DecidedAt         *time.Time      `json:"decided_at,omitempty"`
	DecisionReason    *string         `json:"decision_reason,omitempty"`
}

// ListApprovals returns one keyset page of requests the caller may decide.
func (h *AppWriteHandler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	if h.approvals == nil {
		h.writeError(w, r, http.StatusNotImplemented, "approvals_unavailable", "approval workflow is not configured", nil)
		return
	}

	decidable := permissions.DecidableApprovalRequestTypes(callerRoles(r))
	// P0-2: Extract caller's park scope for filtering.
	callerParkID := callerParkScope(r)

	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = domain.ApprovalStatusPending
	}

	// Page size is capped server-side at MaxApprovalPageSize: this queue is read from a phone, and
	// a client asking for 500 rows must get one screen of work, not the whole backlog.
	pageSize, ok := approvalPageSize(w, r, h)
	if !ok {
		return
	}

	page, err := h.approvals.ListPending(r.Context(), tenantID, status, decidable, callerParkID, pageSize, strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		h.writeApprovalError(w, r, err)
		return
	}

	items := make([]appApprovalListItem, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, appApprovalListItem{
			ApprovalRequestID: item.ApprovalRequestID,
			RequestType:       item.RequestType,
			Status:            item.Status,
			RaisedByUserID:    item.RaisedByUserID,
			RaisedAt:          item.RaisedAt,
			ShiftingEventID:   item.ShiftingEventID,
			SubjectGoatID:     item.SubjectGoatID,
			Summary:           item.Summary,
			DecidedByUserID:   item.DecidedByUserID,
			DecidedAt:         item.DecidedAt,
			DecisionReason:    item.DecisionReason,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, appApprovalListResponse{Items: items, NextCursor: page.NextCursor})
}

// ---------------------------------------------------------------------------
// Decide
// ---------------------------------------------------------------------------

type appApprovalDecisionRequest struct {
	Reason string `json:"reason,omitempty"`
}

type appApprovalDecisionResponse struct {
	ApprovalRequestID string     `json:"approval_request_id"`
	RequestType       string     `json:"request_type"`
	Status            string     `json:"status"`
	DecidedByUserID   *string    `json:"decided_by_user_id,omitempty"`
	DecidedAt         *time.Time `json:"decided_at,omitempty"`
	DecisionReason    *string    `json:"decision_reason,omitempty"`
	AppliedResultType *string    `json:"applied_result_type,omitempty"`
	AppliedResultID   *string    `json:"applied_result_id,omitempty"`
	IdempotentReplay  bool       `json:"idempotent_replay"`
}

// ApproveRequest approves a pending request, applying its effect atomically with the status flip.
func (h *AppWriteHandler) ApproveRequest(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, true, appApprovalApproveCommand, appApprovalApproveRoute)
}

// RejectRequest rejects a pending request. A reason is required, and NO effect is applied.
func (h *AppWriteHandler) RejectRequest(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, false, appApprovalRejectCommand, appApprovalRejectRoute)
}

// ApproveRequestAdminWeb is the admin-web counterpart of ApproveRequest: identical decision logic,
// distinct telemetry route/command labels so app and admin-web decisions are separable in traces.
func (h *AppWriteHandler) ApproveRequestAdminWeb(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, true, adminWebApprovalApproveCommand, adminWebApprovalApproveRoute)
}

// RejectRequestAdminWeb is the admin-web counterpart of RejectRequest.
func (h *AppWriteHandler) RejectRequestAdminWeb(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, false, adminWebApprovalRejectCommand, adminWebApprovalRejectRoute)
}

func (h *AppWriteHandler) decide(w http.ResponseWriter, r *http.Request, approve bool, command, route string) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	if h.approvals == nil {
		h.writeError(w, r, http.StatusNotImplemented, "approvals_unavailable", "approval workflow is not configured", nil)
		return
	}
	actorID := httpmiddleware.ActorIDFromContext(r.Context())
	if actorID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_actor", "missing actor context", nil)
		return
	}
	// Approve/reject are mutating writes and carry the full idempotency contract, exactly like the
	// submit routes.
	clientKey, err := appIdempotencyKey(r)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	requestID := strings.TrimSpace(r.PathValue("request_id"))
	if requestID == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_request_id", "request_id is required", nil)
		return
	}

	var body appApprovalDecisionRequest
	raw, ok := h.readBody(w, r)
	if !ok {
		return
	}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := decodeStrictJSON(raw, &body, "ApprovalDecisionRequest"); err != nil {
			h.writeAppError(w, r, err)
			return
		}
	}
	reason := strings.TrimSpace(body.Reason)
	if !approve && reason == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_reason", "reason is required to reject a request", nil)
		return
	}

	// The fingerprint covers the decision's meaning (which request, approve vs reject, what
	// reason), so replaying the same key with a different verdict or reason is a conflict rather
	// than a silent overwrite of someone else's decision.
	canonical, err := canonicalRequestBytes(tenantID, command, route, struct {
		RequestID string `json:"request_id"`
		Approve   bool   `json:"approve"`
		Reason    string `json:"reason"`
	}{RequestID: requestID, Approve: approve, Reason: reason})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}

	// P0-2: Thread the caller's park scope through to the decision.
	decided, replay, err := h.approvals.Decide(r.Context(), countsapp.DecisionInput{
		TenantID:           tenantID,
		ApprovalRequestID:  requestID,
		Approve:            approve,
		Reason:             reason,
		DecidedByUserID:    actorID,
		TraceID:            appTraceID(r),
		IdempotencyKey:     "counts-approval-decision:" + clientKey,
		RequestFingerprint: stableHash("counts-app-approval-decision", canonical),
		DecidableTypes:     permissions.DecidableApprovalRequestTypes(callerRoles(r)),
		CallerParkID:       callerParkScope(r),
	})
	if err != nil {
		h.writeApprovalError(w, r, err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, appApprovalDecisionResponse{
		ApprovalRequestID: decided.ApprovalRequestID,
		RequestType:       decided.RequestType,
		Status:            decided.Status,
		DecidedByUserID:   decided.DecidedByUserID,
		DecidedAt:         decided.DecidedAt,
		DecisionReason:    decided.DecisionReason,
		AppliedResultType: decided.AppliedResultType,
		AppliedResultID:   decided.AppliedResultID,
		IdempotentReplay:  replay,
	})
}

// callerRoles returns the roles the authenticated caller holds in the active tenant.
func callerRoles(r *http.Request) []string {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	roles := make([]string, 0, len(grants))
	seen := map[string]struct{}{}
	for _, grant := range grants {
		if grant.Role == "" {
			continue
		}
		if _, dup := seen[grant.Role]; dup {
			continue
		}
		seen[grant.Role] = struct{}{}
		roles = append(roles, grant.Role)
	}
	return roles
}

// callerParkScope returns the park scope (park_id) the caller is scoped to, if any.
// P0-2: Returns empty string if the caller has no scope restriction (e.g. CEO/internal).
func callerParkScope(r *http.Request) string {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	for _, grant := range grants {
		if grant.ScopeType == "park" && grant.ScopeID != "" {
			return grant.ScopeID
		}
	}
	return ""
}

func (h *AppWriteHandler) writeApprovalError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrApprovalRequestNotFound):
		h.writeError(w, r, http.StatusNotFound, "approval_request_not_found", "approval request not found", err)
	case errors.Is(err, countsapp.ErrApprovalForbiddenType):
		// The type-specific authority check: a park_head addressing a birth, or a ceo_internal
		// addressing a shifting without the shifting grant, lands here.
		h.writeError(w, r, http.StatusForbidden, "permission_denied",
			"caller may not decide this request type", err)
	case errors.Is(err, countsapp.ErrApprovalReasonRequired):
		h.writeError(w, r, http.StatusBadRequest, "missing_reason", "reason is required to reject a request", err)
	case errors.Is(err, ports.ErrApprovalAlreadyDecided):
		h.writeError(w, r, http.StatusConflict, "approval_already_decided",
			"approval request has already been decided", err)
	case errors.Is(err, ports.ErrApprovalEffectIncomplete):
		h.writeError(w, r, http.StatusConflict, "approval_effect_incomplete", err.Error(), err)
	case errors.Is(err, ports.ErrDeathEvidenceIncomplete):
		h.writeError(w, r, http.StatusConflict, "death_evidence_incomplete",
			"both death videos must be uploaded before approval", err)
	case errors.Is(err, countsapp.ErrApprovalInvalidStoredPayload):
		h.writeError(w, r, http.StatusConflict, "approval_payload_not_applicable",
			"the stored request can no longer be applied", err)
	case errors.Is(err, ports.ErrIdempotencyConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict",
			"Idempotency-Key was reused with a different payload", err)
	case errors.Is(err, countsapp.ErrMissingRequiredField),
		errors.Is(err, countsapp.ErrInvalidExceptionFilter),
		errors.Is(err, countsapp.ErrInvalidJSON):
		h.writeError(w, r, http.StatusBadRequest, "invalid_approval_request", err.Error(), err)
	default:
		// Identity's app errors (a malformed stored payload, a stale row_version on the animal, the
		// critical-death guardrail) surface with their own status/code taxonomy.
		var appErr *identityapp.Error
		if errors.As(err, &appErr) {
			h.writeError(w, r, appErr.HTTPStatus, appErr.Code, appErr.Message, err)
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
	}
}

package workforcehttp

import (
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// LeaveHandler serves the leave-request workflow (docs/features/leave-requests/
// plan.md): the requester's raise / withdraw / history on AppBootstrap, the
// approver queue and verbs on leave.approve (phone AND admin-web, one handler
// two prefixes), the People / HRMS list on leave.read, and the CEO-only
// routing config.
type LeaveHandler struct {
	service *app.LeaveService
	log     *slog.Logger
}

func NewLeaveHandler(service *app.LeaveService, log ...*slog.Logger) *LeaveHandler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &LeaveHandler{service: service, log: l}
}

func RegisterLeave(mux *http.ServeMux, h *LeaveHandler) {
	mux.HandleFunc("POST /app/leave/requests", h.Request)
	mux.HandleFunc("GET /app/leave/requests", h.MyRequests)
	mux.HandleFunc("POST /app/leave/requests/{leave_request_id}/withdraw", h.Withdraw)
	mux.HandleFunc("GET /app/leave/approvals", h.Queue)
	mux.HandleFunc("POST /app/leave/approvals/{leave_request_id}/approve", h.Approve)
	mux.HandleFunc("POST /app/leave/approvals/{leave_request_id}/reject", h.Reject)
	// Admin-web twins of the approver surface, so the web Approvals page and
	// the phone decide through ONE service and ONE idempotency scope.
	mux.HandleFunc("GET /admin-web/leave/approvals", h.Queue)
	mux.HandleFunc("POST /admin-web/leave/approvals/{leave_request_id}/approve", h.Approve)
	mux.HandleFunc("POST /admin-web/leave/approvals/{leave_request_id}/reject", h.Reject)
	mux.HandleFunc("GET /admin/leave/requests", h.AdminList)
	mux.HandleFunc("GET /admin/leave/approval-config", h.Config)
	mux.HandleFunc("PUT /admin/leave/approval-config", h.SetConfig)
}

func (h *LeaveHandler) Request(w http.ResponseWriter, r *http.Request) {
	var body domain.LeaveRequestCreate
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = idempotencyKeyHeader(r)
	}
	result, err := h.service.Request(r.Context(), tenantID(r), actorID(r), body,
		httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	if err != nil {
		h.log.WarnContext(r.Context(), "leave_request_failed",
			slog.String("trace_id", traceID(r)), slog.String("actor_id", actorID(r)), slog.String("error", err.Error()))
	}
	h.respond(w, r, result, err)
}

func (h *LeaveHandler) MyRequests(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.MyRequests(r.Context(), tenantID(r), actorID(r),
		httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *LeaveHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Withdraw(r.Context(), tenantID(r), actorID(r), r.PathValue("leave_request_id"),
		idempotencyKeyHeader(r), httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *LeaveHandler) Queue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.Queue(r.Context(), tenantID(r), actorID(r), leaveApprover(r),
		parseLimit(q.Get("limit")), q.Get("cursor"), httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *LeaveHandler) Approve(w http.ResponseWriter, r *http.Request) { h.decide(w, r, true) }
func (h *LeaveHandler) Reject(w http.ResponseWriter, r *http.Request)  { h.decide(w, r, false) }

func (h *LeaveHandler) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	var body domain.LeaveDecisionRequest
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &body) {
			return
		}
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = idempotencyKeyHeader(r)
	}
	result, err := h.service.Decide(r.Context(), tenantID(r), actorID(r), r.PathValue("leave_request_id"),
		approve, body, leaveApprover(r), httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	if err != nil {
		h.log.WarnContext(r.Context(), "leave_decision_failed",
			slog.String("trace_id", traceID(r)), slog.String("actor_id", actorID(r)),
			slog.Bool("approve", approve), slog.String("error", err.Error()))
	}
	h.respond(w, r, result, err)
}

func (h *LeaveHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.AdminList(r.Context(), tenantID(r), q.Get("status"), q.Get("park_id"),
		parseLimit(q.Get("limit")), q.Get("cursor"), httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *LeaveHandler) Config(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Config(r.Context(), tenantID(r), httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *LeaveHandler) SetConfig(w http.ResponseWriter, r *http.Request) {
	var body domain.LeaveApprovalConfigUpdate
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.SetConfig(r.Context(), tenantID(r), actorID(r), body,
		httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

// leaveApprover derives the caller's approving identity from their ACTIVE
// grants -- never from the body. A park head's parks are the park-scoped
// park_head grants; HR is the per-person `hr` role; the CEO floor decides any
// slot.
func leaveApprover(r *http.Request) app.LeaveApprover {
	var out app.LeaveApprover
	for _, grant := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		switch grant.Role {
		case permissions.RoleParkHead:
			if grant.ScopeType == "park" && grant.ScopeID != "" {
				out.ParkHeadParks = append(out.ParkHeadParks, grant.ScopeID)
			} else {
				// A tenant-scoped park_head grant (the shape the live roster actually carries)
				// heads the park the roster assigns them; the service resolves it from
				// workforce_members.primary_location_id.
				out.ParkHeadOfHomePark = true
			}
		case permissions.RoleHR:
			out.HR = true
		case permissions.RoleCEOInternal:
			out.Any = true
		}
	}
	return out
}

func (h *LeaveHandler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	writeServiceResponse(w, r, h.log, payload, err)
}

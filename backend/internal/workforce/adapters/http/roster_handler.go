package workforcehttp

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// RosterHandler exposes the HR roster surface approved in
// docs/hr/roster-rbac-design.md: fixed positions, leave/absence, coverage
// resolution, and vaccination-ownership resolution. Kept in its own file/type
// from Handler (operator/device/grant CRUD) for cohesion; both are registered
// on the same protected mux in bootstrap/api.go.
type RosterHandler struct {
	service *app.RosterService
	log     *slog.Logger
}

func NewRosterHandler(service *app.RosterService, log ...*slog.Logger) *RosterHandler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &RosterHandler{service: service, log: l}
}

func RegisterRoster(mux *http.ServeMux, h *RosterHandler) {
	mux.HandleFunc("GET /admin/roster/positions", h.ListPositions)
	mux.HandleFunc("POST /admin/roster/positions", h.CreatePosition)
	mux.HandleFunc("POST /admin/roster/leave", h.ApplyLeave)
	mux.HandleFunc("POST /admin/roster/leave/{absence_id}/approve", h.ApproveLeave)
	mux.HandleFunc("POST /admin/roster/leave/{absence_id}/resolve-coverage", h.ResolveLeaveCoverage)
	mux.HandleFunc("GET /admin/roster/leave/{absence_id}", h.GetLeave)
	mux.HandleFunc("GET /admin/roster/leave", h.ListLeave)
	mux.HandleFunc("GET /admin/roster/vaccination-owner", h.ResolveVaccinationOwner)
}

func (h *RosterHandler) ListPositions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.ListPositions(r.Context(), ports.ListPositionsParams{
		TenantID:          tenantID(r),
		WorkforceMemberID: q.Get("workforce_member_id"),
		ScopeType:         q.Get("scope_type"),
		ScopeID:           q.Get("scope_id"),
		PositionCode:      q.Get("position_code"),
		Status:            q.Get("status"),
		Limit:             parseLimit(q.Get("limit")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *RosterHandler) CreatePosition(w http.ResponseWriter, r *http.Request) {
	var body domain.CreatePositionRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if key := idempotencyKeyHeader(r); key != "" {
		body.IdempotencyKey = &key
	}
	result, err := h.service.CreatePosition(r.Context(), ports.CreatePositionCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		Body:     body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *RosterHandler) ApplyLeave(w http.ResponseWriter, r *http.Request) {
	var body domain.ApplyStaffLeaveRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if key := idempotencyKeyHeader(r); key != "" {
		body.IdempotencyKey = &key
	}
	result, err := h.service.ApplyLeave(r.Context(), tenantID(r), actorID(r), body, traceID(r))
	h.respond(w, r, result, err)
}

func (h *RosterHandler) ApproveLeave(w http.ResponseWriter, r *http.Request) {
	var body domain.ApproveStaffLeaveRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if key := idempotencyKeyHeader(r); key != "" {
		body.IdempotencyKey = &key
	}
	result, err := h.service.ApproveLeave(r.Context(), tenantID(r), actorID(r), r.PathValue("absence_id"), body, traceID(r))
	h.respond(w, r, result, err)
}

func (h *RosterHandler) ResolveLeaveCoverage(w http.ResponseWriter, r *http.Request) {
	var body domain.ResolveLeaveCoverageRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if key := idempotencyKeyHeader(r); key != "" {
		body.IdempotencyKey = &key
	}
	result, err := h.service.ResolveLeaveCoverage(r.Context(), tenantID(r), actorID(r), r.PathValue("absence_id"), body, traceID(r))
	h.respond(w, r, result, err)
}

func (h *RosterHandler) GetLeave(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetLeave(r.Context(), tenantID(r), r.PathValue("absence_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *RosterHandler) ListLeave(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.ListLeave(r.Context(), ports.ListLeaveParams{
		TenantID:          tenantID(r),
		WorkforceMemberID: q.Get("workforce_member_id"),
		ScopeType:         q.Get("scope_type"),
		ScopeID:           q.Get("scope_id"),
		Status:            q.Get("status"),
		Limit:             parseLimit(q.Get("limit")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *RosterHandler) ResolveVaccinationOwner(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.ResolveVaccinationOwner(r.Context(), tenantID(r), actorID(r), q.Get("scope_type"), q.Get("scope_id"), q.Get("date"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *RosterHandler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		envelope := domain.ErrorEnvelope{
			Code:        "internal_error",
			Message:     "internal server error",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   true,
		}
		var appErr *app.Error
		if errors.As(err, &appErr) {
			status = appErr.HTTPStatus
			envelope.Code = appErr.Code
			envelope.Message = appErr.Message
			envelope.Retryable = appErr.Retryable
		}
		httpresponse.WriteError(w, r, h.log, status, envelope, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, payload)
}

package workforcehttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// DisciplineHandler serves HRMS violations and enquiries (maintainer decisions 2026-09-30).
// The route table gates each route; the caller's PARK SCOPE is decided here from the same
// permission source the gate used: a holder of workforce.violations.read (HR, the CEO/CXO) sees
// every park, anyone else is a park head who sees only the parks they head.
type DisciplineHandler struct {
	service *app.DisciplineService
	log     *slog.Logger
}

func NewDisciplineHandler(service *app.DisciplineService, log *slog.Logger) *DisciplineHandler {
	if log == nil {
		log = slog.Default()
	}
	return &DisciplineHandler{service: service, log: log}
}

func RegisterDiscipline(mux *http.ServeMux, h *DisciplineHandler) {
	mux.HandleFunc("GET /admin/workforce/violations", h.Violations)
	mux.HandleFunc("POST /admin/workforce/violations", h.RecordViolation)
	mux.HandleFunc("POST /admin/workforce/violations/{violation_id}/withdraw", h.WithdrawViolation)
	mux.HandleFunc("GET /admin/workforce/enquiries", h.Enquiries)
	mux.HandleFunc("GET /admin/workforce/enquiries/{enquiry_id}", h.Enquiry)
	mux.HandleFunc("POST /admin/workforce/enquiries/{enquiry_id}/submit", h.SubmitEnquiry)
	// The phone twins: one service, one scope rule.
	mux.HandleFunc("GET /app/enquiries", h.Enquiries)
	mux.HandleFunc("GET /app/enquiries/{enquiry_id}", h.Enquiry)
	mux.HandleFunc("POST /app/enquiries/{enquiry_id}/submit", h.SubmitEnquiry)
}

func disciplineCaller(ctx context.Context) app.DisciplineCaller {
	return app.DisciplineCaller{All: holds(ctx, permissions.WorkforceViolationsRead), UserID: httpmiddleware.ActorIDFromContext(ctx)}
}

func holds(ctx context.Context, perm string) bool {
	if perms, ok := httpmiddleware.PersonPermissionsFromContext(ctx); ok {
		for _, p := range perms {
			if p == perm {
				return true
			}
		}
		return false
	}
	for _, grant := range httpmiddleware.AuthGrantsFromContext(ctx) {
		if permissions.RoleHasPermission(grant.Role, perm) {
			return true
		}
	}
	return false
}

func (h *DisciplineHandler) Violations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.Violations(r.Context(), tenantID(r), disciplineCaller(r.Context()), q.Get("park_id"), q.Get("month"),
		q.Get("status"), q.Get("cursor"), parseLimit(q.Get("limit")), traceID(r))
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *DisciplineHandler) RecordViolation(w http.ResponseWriter, r *http.Request) {
	var body domain.RecordViolationRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = idempotencyKeyHeader(r)
	}
	result, err := h.service.RecordViolation(r.Context(), tenantID(r), actorID(r), body, traceID(r))
	h.warn(r, "hrms_violation_record_failed", err)
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *DisciplineHandler) WithdrawViolation(w http.ResponseWriter, r *http.Request) {
	var body domain.WithdrawViolationRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.WithdrawViolation(r.Context(), tenantID(r), actorID(r), r.PathValue("violation_id"), body, traceID(r))
	h.warn(r, "hrms_violation_withdraw_failed", err)
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *DisciplineHandler) Enquiries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.Enquiries(r.Context(), tenantID(r), disciplineCaller(r.Context()), q.Get("park_id"), q.Get("status"),
		q.Get("cursor"), parseLimit(q.Get("limit")), traceID(r))
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *DisciplineHandler) Enquiry(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Enquiry(r.Context(), tenantID(r), disciplineCaller(r.Context()), r.PathValue("enquiry_id"), traceID(r))
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *DisciplineHandler) SubmitEnquiry(w http.ResponseWriter, r *http.Request) {
	var body domain.SubmitEnquiryRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.SubmitEnquiry(r.Context(), tenantID(r), disciplineCaller(r.Context()), actorID(r), r.PathValue("enquiry_id"), body, traceID(r))
	h.warn(r, "hrms_enquiry_submit_failed", err)
	writeServiceResponse(w, r, h.log, result, err)
}

func (h *DisciplineHandler) warn(r *http.Request, msg string, err error) {
	if err != nil {
		h.log.WarnContext(r.Context(), msg, slog.String("trace_id", traceID(r)), slog.String("actor_id", actorID(r)), slog.String("error", err.Error()))
	}
}

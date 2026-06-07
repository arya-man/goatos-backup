package identityhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type Handler struct {
	service *app.Service
}

func NewHandler(service *app.Service) *Handler {
	return &Handler{service: service}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /goats/search", h.SearchGoats)
	mux.HandleFunc("GET /goats/{goat_id}", h.GetGoatPassport)
	mux.HandleFunc("GET /goats/{goat_id}/timeline", h.NotImplemented("goat_timeline_deferred"))
	mux.HandleFunc("GET /identifiers/{type}/{value}/resolve", h.ResolveIdentifier)
	mux.HandleFunc("GET /identity/correction-requests", h.NotImplemented("correction_requests_deferred"))
	mux.HandleFunc("POST /identity/correction-requests", h.NotImplemented("correction_requests_deferred"))

	mux.HandleFunc("GET /admin/identity/conflicts", h.ListConflicts)
	mux.HandleFunc("GET /admin/identity/conflicts/{conflict_id}", h.GetConflict)
	mux.HandleFunc("POST /admin/identity/conflicts/{conflict_id}/resolve", h.NotImplemented("conflict_resolution_deferred"))
	mux.HandleFunc("GET /admin/import-runs/{import_run_id}", h.NotImplemented("legacy_import_deferred"))
	mux.HandleFunc("GET /admin/import-runs/{import_run_id}/rows", h.NotImplemented("legacy_import_deferred"))
	mux.HandleFunc("POST /admin/import-runs", h.NotImplemented("legacy_import_deferred"))
	mux.HandleFunc("GET /admin/identity/candidates", h.NotImplemented("identity_candidates_deferred"))
	mux.HandleFunc("POST /admin/identity/candidates/{candidate_id}/approve", h.NotImplemented("identity_candidates_deferred"))
	mux.HandleFunc("POST /admin/identity/candidates/{candidate_id}/reject", h.NotImplemented("identity_candidates_deferred"))
	mux.HandleFunc("POST /admin/goats", h.NotImplemented("admin_goat_writes_deferred"))
	mux.HandleFunc("PATCH /admin/goats/{goat_id}", h.NotImplemented("admin_goat_writes_deferred"))
	mux.HandleFunc("POST /admin/goats/{goat_id}/identifiers", h.NotImplemented("admin_identifier_writes_deferred"))
	mux.HandleFunc("POST /admin/goats/{goat_id}/identifiers/{identifier_id}/retire", h.NotImplemented("admin_identifier_writes_deferred"))
	mux.HandleFunc("POST /admin/identity/correction-requests/{correction_request_id}/resolve", h.NotImplemented("correction_resolution_deferred"))

	mux.HandleFunc("GET /analytics/identity/counts", h.GetIdentityCounts)
}

func (h *Handler) GetGoatPassport(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetGoatPassport(r.Context(), tenantID(r), r.PathValue("goat_id"), traceID(r))
	respond(w, r, result, err)
}

func (h *Handler) SearchGoats(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	params := ports.SearchGoatsParams{
		TenantID:       tenantID(r),
		Limit:          limit,
		Cursor:         optionalQuery(q.Get("cursor")),
		Query:          optionalQuery(q.Get("q")),
		IdentifierType: optionalQuery(q.Get("identifier_type")),
		ScopeKey:       optionalQuery(q.Get("scope_key")),
		FarmID:         optionalQuery(q.Get("farm_id")),
		ParkID:         optionalQuery(q.Get("park_id")),
		LocationID:     optionalQuery(q.Get("location_id")),
		Status:         optionalQuery(q.Get("status")),
	}
	result, err := h.service.SearchGoats(r.Context(), params, traceID(r))
	respond(w, r, result, err)
}

func (h *Handler) ResolveIdentifier(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := ports.ResolveIdentifierParams{
		TenantID:        tenantID(r),
		IdentifierType:  r.PathValue("type"),
		NormalizedValue: r.PathValue("value"),
		ScopeKey:        optionalQuery(q.Get("scope_key")),
		FarmID:          optionalQuery(q.Get("farm_id")),
		ParkID:          optionalQuery(q.Get("park_id")),
		LocationID:      optionalQuery(q.Get("location_id")),
	}
	result, err := h.service.ResolveIdentifier(r.Context(), params, traceID(r))
	respond(w, r, result, err)
}

func (h *Handler) ListConflicts(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	params := ports.ListConflictsParams{
		TenantID:     tenantID(r),
		Limit:        limit,
		Cursor:       optionalQuery(q.Get("cursor")),
		State:        optionalQuery(q.Get("state")),
		ConflictType: optionalQuery(q.Get("conflict_type")),
	}
	result, err := h.service.ListConflicts(r.Context(), params, traceID(r))
	respond(w, r, result, err)
}

func (h *Handler) GetConflict(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetConflict(r.Context(), tenantID(r), r.PathValue("conflict_id"), traceID(r))
	respond(w, r, result, err)
}

func (h *Handler) GetIdentityCounts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := ports.CountParams{
		Grain:              q.Get("grain"),
		TenantID:           q.Get("tenant_id"),
		CustodianPartyID:   optionalQuery(q.Get("custodian_party_id")),
		FarmID:             optionalQuery(q.Get("farm_id")),
		ParkID:             optionalQuery(q.Get("park_id")),
		ShedID:             optionalQuery(q.Get("shed_id")),
		CohortID:           optionalQuery(q.Get("cohort_id")),
		LifecycleStatus:    optionalQuery(q.Get("lifecycle_status")),
		ReproductiveStatus: optionalQuery(q.Get("reproductive_status")),
		GrowthCohortTag:    optionalQuery(q.Get("growth_cohort_tag")),
		ManagementStage:    optionalQuery(q.Get("management_stage")),
		HealthStatus:       optionalQuery(q.Get("health_status")),
		IdentityState:      optionalQuery(q.Get("identity_state")),
		BreedID:            optionalQuery(q.Get("breed_id")),
		Sex:                optionalQuery(q.Get("sex")),
	}
	result, err := h.service.GetIdentityCounts(r.Context(), params, traceID(r))
	respond(w, r, result, err)
}

func (h *Handler) NotImplemented(code string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, domain.ErrorEnvelope{
			Code:        code,
			Message:     "Endpoint is intentionally deferred in this Phase 1 backend foundation slice.",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
	}
}

func parseLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "missing_limit",
			Message:     "limit query parameter is required",
			FieldErrors: []domain.FieldError{{Field: "limit", Code: "required", Message: "limit is required"}},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_limit",
			Message:     "limit must be an integer between 1 and 100",
			FieldErrors: []domain.FieldError{{Field: "limit", Code: "invalid", Message: "limit must be between 1 and 100"}},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return 0, false
	}
	return limit, true
}

func respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
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
		writeError(w, status, envelope)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func writeError(w http.ResponseWriter, status int, envelope domain.ErrorEnvelope) {
	if envelope.FieldErrors == nil {
		envelope.FieldErrors = []domain.FieldError{}
	}
	writeJSON(w, status, envelope)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	traceID := httpmiddleware.TraceIDFromContext(r.Context())
	if traceID == "" {
		return "missing-trace"
	}
	return traceID
}

func optionalQuery(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

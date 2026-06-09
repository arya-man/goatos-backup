package reportinghttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/reporting/app"
	"github.com/vgoats/goatos/backend/internal/reporting/domain"
	"github.com/vgoats/goatos/backend/internal/reporting/ports"
)

type Handler struct {
	service *app.Service
}

func NewHandler(service *app.Service) *Handler {
	return &Handler{service: service}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /analytics/identity/counts", h.GetIdentityCounts)
}

func (h *Handler) GetIdentityCounts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if headerTenant := tenantID(r); headerTenant != "" && q.Get("tenant_id") != "" && headerTenant != q.Get("tenant_id") {
		writeError(w, http.StatusForbidden, domain.ErrorEnvelope{
			Code:        "tenant_scope_mismatch",
			Message:     "tenant_id query parameter must match the request tenant scope",
			FieldErrors: []domain.FieldError{{Field: "tenant_id", Code: "scope_mismatch", Message: "tenant_id does not match request tenant scope"}},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	result, err := h.service.GetIdentityCounts(r.Context(), ports.CountParams{
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
	}, traceID(r))
	respond(w, r, result, err)
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

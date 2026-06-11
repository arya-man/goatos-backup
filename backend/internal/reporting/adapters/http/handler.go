package reportinghttp

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/reporting/app"
	"github.com/vgoats/goatos/backend/internal/reporting/domain"
	"github.com/vgoats/goatos/backend/internal/reporting/ports"
)

type Handler struct {
	service *app.Service
	log     *slog.Logger
}

// NewHandler creates a Handler. If log is nil, slog.Default() is used.
func NewHandler(service *app.Service, log ...*slog.Logger) *Handler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Handler{service: service, log: l}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /analytics/identity/counts", h.GetIdentityCounts)
}

func (h *Handler) GetIdentityCounts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	requestTenant := tenantID(r)
	if requestTenant != "" && q.Get("tenant_id") != "" && requestTenant != q.Get("tenant_id") {
		writeError(w, http.StatusForbidden, domain.ErrorEnvelope{
			Code:        "tenant_scope_mismatch",
			Message:     "tenant_id query parameter must match the request tenant scope",
			FieldErrors: []domain.FieldError{{Field: "tenant_id", Code: "scope_mismatch", Message: "tenant_id does not match request tenant scope"}},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	limit, ok := parseIdentityCountsLimit(w, r, q.Get("limit"))
	if !ok {
		return
	}
	result, err := h.service.GetIdentityCounts(r.Context(), ports.CountParams{
		Grain:              q.Get("grain"),
		TenantID:           requestTenant,
		Limit:              limit,
		Cursor:             optionalQuery(q.Get("cursor")),
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
	h.respond(w, r, result, err)
}

func parseIdentityCountsLimit(w http.ResponseWriter, r *http.Request, value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "missing_limit",
			Message:     "limit query parameter is required",
			FieldErrors: []domain.FieldError{{Field: "limit", Code: "required", Message: "limit is required"}},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return 0, false
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > ports.MaxIdentityCountsLimit {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_limit",
			Message:     "limit must be an integer between 1 and 500",
			FieldErrors: []domain.FieldError{{Field: "limit", Code: "invalid", Message: "limit must be between 1 and 500"}},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return 0, false
	}
	return limit, true
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
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
		writeHandlerError(w, r, h.log, status, envelope, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func writeHandlerError(w http.ResponseWriter, r *http.Request, log *slog.Logger, status int, envelope domain.ErrorEnvelope, cause error) {
	if envelope.FieldErrors == nil {
		envelope.FieldErrors = []domain.FieldError{}
	}
	httpresponse.WriteError(w, r, log, status, envelope, cause)
}

func writeError(w http.ResponseWriter, status int, envelope domain.ErrorEnvelope) {
	if envelope.FieldErrors == nil {
		envelope.FieldErrors = []domain.FieldError{}
	}
	writeJSON(w, status, envelope)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	httpresponse.WriteJSON(w, status, payload)
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

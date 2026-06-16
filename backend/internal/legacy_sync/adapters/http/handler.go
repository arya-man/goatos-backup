package legacysynchttp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/legacy_sync/app"
	"github.com/vgoats/goatos/backend/internal/legacy_sync/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Handler struct {
	service *app.Service
	log     *slog.Logger
}

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
	mux.HandleFunc("GET /admin/legacy-sync/sources", h.ListSources)
	mux.HandleFunc("GET /admin/legacy-sync/status", h.OverallStatus)
	mux.HandleFunc("GET /admin/legacy-sync/runs", h.ListRuns)
	mux.HandleFunc("POST /admin/legacy-sync/runs", h.CreateRun)
	mux.HandleFunc("GET /admin/legacy-sync/runs/{sync_run_id}", h.GetRun)
	mux.HandleFunc("POST /admin/legacy-sync/runs/{sync_run_id}/cancel", h.CancelRun)
}

func (h *Handler) ListSources(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListSources(r.Context(), tenantID(r), r.URL.Query().Get("domain"), traceID(r))
	h.respond(w, r, result, err, http.StatusOK)
}

func (h *Handler) OverallStatus(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.OverallStatus(r.Context(), tenantID(r), traceID(r))
	h.respond(w, r, result, err, http.StatusOK)
}

func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r, 50)
	if !ok {
		return
	}
	result, err := h.service.ListRuns(r.Context(), tenantID(r), limit, traceID(r))
	h.respond(w, r, result, err, http.StatusOK)
}

func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetRun(r.Context(), tenantID(r), r.PathValue("sync_run_id"), traceID(r))
	h.respond(w, r, result, err, http.StatusOK)
}

func (h *Handler) CreateRun(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body is too large or unreadable",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	var req domain.CreateRunRequest
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_json",
			Message:     "request body must be valid JSON matching the contract",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return
	}
	result, err := h.service.CreateRun(r.Context(), tenantID(r), actorID(r), req, traceID(r))
	h.respond(w, r, result, err, http.StatusCreated)
}

func (h *Handler) CancelRun(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.CancelRun(r.Context(), tenantID(r), r.PathValue("sync_run_id"), traceID(r))
	h.respond(w, r, result, err, http.StatusOK)
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, payload any, err error, successStatus int) {
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
	httpresponse.WriteJSON(w, successStatus, payload)
}

func parseLimit(w http.ResponseWriter, r *http.Request, max int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		raw = "20"
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > max {
		writeError(w, http.StatusBadRequest, domain.ErrorEnvelope{
			Code:        "invalid_limit",
			Message:     "limit must be a positive integer within the route maximum",
			FieldErrors: []domain.FieldError{{Field: "limit", Code: "invalid", Message: "limit is outside the allowed range"}},
			TraceID:     traceID(r),
			Retryable:   false,
		})
		return 0, false
	}
	return limit, true
}

func writeError(w http.ResponseWriter, status int, envelope domain.ErrorEnvelope) {
	if envelope.FieldErrors == nil {
		envelope.FieldErrors = []domain.FieldError{}
	}
	httpresponse.WriteJSON(w, status, envelope)
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *http.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	traceID := httpmiddleware.TraceIDFromContext(r.Context())
	if traceID == "" {
		return "missing-trace"
	}
	return traceID
}

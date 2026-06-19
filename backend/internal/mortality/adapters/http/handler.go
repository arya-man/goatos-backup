package http

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/mortality/app"
	"github.com/vgoats/goatos/backend/internal/mortality/domain"
	"github.com/vgoats/goatos/backend/internal/mortality/ports"
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
	mux.HandleFunc("GET /analytics/mortality/dashboard", h.GetDashboard)
	mux.HandleFunc("POST /admin/mortality/sync-runs", h.CreateSyncRun)
	mux.HandleFunc("GET /admin/mortality/sync-runs/{sync_run_id}", h.GetSyncRun)
}

func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetDashboard(r.Context(), ports.DashboardParams{
		TenantID: tenantID(r),
		Period:   r.URL.Query().Get("period"),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateSyncRun(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.respond(w, r, nil, app.BadRequest("invalid_body", "request body could not be read"))
		return
	}
	result, err := h.service.Sync(r.Context(), app.SyncMortalityInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		RawBody:        body,
	})
	h.respond(w, r, result, err)
}

func (h *Handler) GetSyncRun(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetSyncRun(r.Context(), tenantID(r), r.PathValue("sync_run_id"), traceID(r))
	h.respond(w, r, result, err)
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
		httpresponse.WriteError(w, r, h.log, status, envelope, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, payload)
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

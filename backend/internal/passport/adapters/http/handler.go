// Package http exposes the read-only Goat Passport API.
package http

import (
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/passport/app"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// Handler serves the Goat Passport endpoints.
type Handler struct {
	service *app.Service
	log     *slog.Logger
}

// NewHandler constructs the handler with an optional logger.
func NewHandler(service *app.Service, log ...*slog.Logger) *Handler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Handler{service: service, log: l}
}

// Register mounts the passport routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /goats/{goat_id}/passport", h.GetPassport)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

// GetPassport returns a goat's vaccination passport (history, open obligations + next due, last dose).
func (h *Handler) GetPassport(w http.ResponseWriter, r *http.Request) {
	goatID := r.PathValue("goat_id")
	if !uuidutil.IsUUIDString(goatID) {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			errorEnvelope{Code: "invalid_goat_id", Message: "goat_id must be a UUID", TraceID: traceID(r)}, nil)
		return
	}
	passport, err := h.service.GetPassport(r.Context(), tenantID(r), goatID)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, passport)
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}

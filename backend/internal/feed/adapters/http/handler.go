// Package http exposes Feed Direction read APIs.
package http

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/feed/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// ReadinessReader is the Feed Direction readiness slice required by this handler.
type ReadinessReader interface {
	Readiness(ctx context.Context, tenantID string) (domain.Readiness, error)
}

// Handler serves Feed Direction endpoints.
type Handler struct {
	reader ReadinessReader
	log    *slog.Logger
}

// NewHandler constructs the Feed Direction handler.
func NewHandler(reader ReadinessReader, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{reader: reader, log: l}
}

// Register mounts the Feed Direction routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /feed-direction/readiness", h.GetReadiness)
}

// GetReadiness serves a fail-closed Feed Direction readiness contract.
func (h *Handler) GetReadiness(w http.ResponseWriter, r *http.Request) {
	readiness, err := h.reader.Readiness(r.Context(), tenantID(r))
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, readiness)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
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

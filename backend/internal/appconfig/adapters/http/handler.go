// Package http exposes the mobile remote-config ("live-config bundle") read endpoint.
package http

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/appconfig/app"
	"github.com/vgoats/goatos/backend/internal/appconfig/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// ConfigCompiler is the read-only compile slice this handler needs.
type ConfigCompiler interface {
	Compile(ctx context.Context, in app.Input) (domain.Response, error)
}

// Handler serves the mobile live-config bundle.
type Handler struct {
	service ConfigCompiler
	log     *slog.Logger
}

// NewHandler constructs the app-config handler.
func NewHandler(service ConfigCompiler, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

// Register mounts the app-config route.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /app/config", h.GetConfig)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

// GetConfig serves the mobile live-config bundle with conditional-GET support: a
// matching If-None-Match short-circuits to a bodyless 304 (cheap poll); otherwise the
// full bundle + its ETag/revision is returned. Side-effect-free — a pure read.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	resp, err := h.service.Compile(r.Context(), app.Input{
		TenantID:  httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:   httpmiddleware.ActorIDFromContext(r.Context()),
		LocaleTag: httpmiddleware.LocaleTagFromRequest(r),
	})
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, errorEnvelope{
			Code:    "internal_error",
			Message: "internal server error",
			TraceID: traceID(r),
		}, err)
		return
	}
	w.Header().Set("ETag", resp.CachePolicy.ETag)
	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == resp.CachePolicy.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}

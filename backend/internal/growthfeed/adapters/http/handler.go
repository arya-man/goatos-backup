package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/growthfeed/app"
	"github.com/vgoats/goatos/backend/internal/growthfeed/domain"
	"github.com/vgoats/goatos/backend/internal/growthfeed/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Service interface {
	GetPenGrowthFeed(ctx context.Context, actor app.Actor, parkID, fromBusinessDate, toBusinessDate string) (domain.PenGrowthFeed, error)
}

type Handler struct {
	service Service
	log     *slog.Logger
}

func NewHandler(service Service, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

// Register wires the one read this module serves. It is a leadership/admin-web
// path with no /app twin: the pen comparison is a desk decision made against a
// wide table, not field work done on a phone.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /growth-feed/pens", h.GetPenGrowthFeed)
}

// GetPenGrowthFeed serves the like-for-like pen comparison. Query parameters:
//   - park_id (optional): one park, which must be inside the caller's scope. When
//     omitted the response spans every park the caller may see — never widened to
//     the tenant on the strength of the flat role check alone.
//   - from, to (optional): INCLUSIVE Asia/Kolkata business dates (YYYY-MM-DD),
//     defaulting to the last 28 days, matching the Weights screen's own window.
func (h *Handler) GetPenGrowthFeed(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetPenGrowthFeed(
		r.Context(), actor(r),
		r.URL.Query().Get("park_id"),
		r.URL.Query().Get("from"),
		r.URL.Query().Get("to"),
	)
	h.respond(w, r, result, err)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id,omitempty"`
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, body any, err error) {
	if err == nil {
		httpresponse.WriteJSON(w, http.StatusOK, body)
		return
	}
	traceID := httpmiddleware.TraceIDFromContext(r.Context())
	switch {
	case errors.Is(err, ports.ErrForbidden):
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden,
			errorEnvelope{Code: "permission_denied", Message: "permission denied", TraceID: traceID}, nil)
	case errors.Is(err, ports.ErrInvalidArgument):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			errorEnvelope{Code: "invalid_request", Message: "request is invalid", TraceID: traceID}, nil)
	case errors.Is(err, ports.ErrNotFound):
		// Also the answer for a park outside the caller's scope: see the service's
		// resolveParkScope for why that is not a 403.
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "no park in scope", TraceID: traceID}, nil)
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "something went wrong", TraceID: traceID}, err)
	}
}

func actor(r *http.Request) app.Actor {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	roles := make([]string, 0, len(grants))
	for _, grant := range grants {
		roles = append(roles, grant.Role)
	}
	return app.Actor{TenantID: httpmiddleware.TenantIDFromContext(r.Context()), Roles: roles}
}

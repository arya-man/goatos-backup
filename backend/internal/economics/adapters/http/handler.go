// Package http exposes the Business Economics read over HTTP, mirroring the
// Growth Director module's handler/respond conventions.
package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/economics/domain"
	"github.com/vgoats/goatos/backend/internal/economics/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Service interface {
	GetBusinessEconomics(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate string) (domain.BusinessEconomics, error)
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

func Register(mux *http.ServeMux, h *Handler) {
	// Admin-web read only; no /app twin — the phone has no Economics screen.
	// Add the twin the day it does, with its own route entry.
	mux.HandleFunc("GET /economics/overview", h.GetBusinessEconomics)
}

// GetBusinessEconomics serves the Sales → Economics page. `park_id` is
// optional (omit for every park the caller may read); `from`/`to` are
// INCLUSIVE Asia/Kolkata business dates (YYYY-MM-DD) defaulting to the last 90
// days ending today.
func (h *Handler) GetBusinessEconomics(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetBusinessEconomics(
		r.Context(),
		actor(r),
		r.URL.Query().Get("park_id"),
		r.URL.Query().Get("from"),
		r.URL.Query().Get("to"),
	)
	h.respond(w, r, result, err)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, body any, err error) {
	if err == nil {
		httpresponse.WriteJSON(w, http.StatusOK, body)
		return
	}
	switch {
	case errors.Is(err, ports.ErrForbidden):
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden, errorEnvelope{Code: "permission_denied", Message: "permission denied", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrInvalidArgument):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, errorEnvelope{Code: "invalid_request", Message: "request is invalid", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound, errorEnvelope{Code: "not_found", Message: "economics resource was not found", TraceID: traceID(r)}, nil)
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, errorEnvelope{Code: "internal", Message: "internal error", TraceID: traceID(r)}, err)
	}
}

func actor(r *http.Request) domain.Actor {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	roles := make([]string, 0, len(grants))
	for _, grant := range grants {
		roles = append(roles, grant.Role)
	}
	return domain.Actor{
		TenantID: httpmiddleware.TenantIDFromContext(r.Context()),
		UserID:   httpmiddleware.ActorIDFromContext(r.Context()),
		Roles:    roles,
	}
}

func traceID(r *http.Request) string { return httpmiddleware.TraceIDFromContext(r.Context()) }

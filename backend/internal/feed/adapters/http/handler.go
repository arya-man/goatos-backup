// Package http exposes Feed Direction read APIs.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	feedapp "github.com/vgoats/goatos/backend/internal/feed/app"
	"github.com/vgoats/goatos/backend/internal/feed/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

const maxActionBodyBytes = 16 * 1024

// Service is the Feed Direction application surface required by this handler.
type Service interface {
	Readiness(ctx context.Context, tenantID string) (domain.Readiness, error)
	ResolveCountsProjectionException(ctx context.Context, in domain.CountsProjectionExceptionResolutionCommand) (domain.CountsProjectionExceptionResolution, error)
}

// Handler serves Feed Direction endpoints.
type Handler struct {
	service Service
	log     *slog.Logger
}

// NewHandler constructs the Feed Direction handler.
func NewHandler(service Service, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

// Register mounts the Feed Direction routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /feed-direction/readiness", h.GetReadiness)
	mux.HandleFunc("POST /feed-direction/counts-projection/exceptions/{exception_id}/resolve", h.ResolveCountsProjectionException)
	mux.HandleFunc("POST /feed-direction/counts-projection/exceptions/{exception_id}/dismiss", h.DismissCountsProjectionException)
}

// GetReadiness serves a fail-closed Feed Direction readiness contract.
func (h *Handler) GetReadiness(w http.ResponseWriter, r *http.Request) {
	readiness, err := h.service.Readiness(r.Context(), tenantID(r))
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, readiness)
}

func (h *Handler) ResolveCountsProjectionException(w http.ResponseWriter, r *http.Request) {
	h.closeCountsProjectionException(w, r, "resolve")
}

func (h *Handler) DismissCountsProjectionException(w http.ResponseWriter, r *http.Request) {
	h.closeCountsProjectionException(w, r, "dismiss")
}

type countsProjectionExceptionActionRequest struct {
	ResolutionReason string  `json:"resolution_reason"`
	ResolutionRef    *string `json:"resolution_ref,omitempty"`
}

type countsProjectionExceptionActionResponse struct {
	Resolution domain.CountsProjectionExceptionResolution `json:"resolution"`
	TraceID    string                                     `json:"trace_id"`
}

func (h *Handler) closeCountsProjectionException(w http.ResponseWriter, r *http.Request, action string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxActionBodyBytes)
	defer r.Body.Close()
	var req countsProjectionExceptionActionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			h.badRequest(w, r, "invalid_body", "request body is required")
			return
		}
		h.badRequest(w, r, "invalid_body", "request body must be JSON")
		return
	}
	resp, err := h.service.ResolveCountsProjectionException(r.Context(), domain.CountsProjectionExceptionResolutionCommand{
		TenantID:              tenantID(r),
		ProjectionExceptionID: r.PathValue("exception_id"),
		Action:                action,
		ActorID:               actorID(r),
		ResolutionReason:      req.ResolutionReason,
		ResolutionRef:         req.ResolutionRef,
		IdempotencyKey:        r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, countsProjectionExceptionActionResponse{
		Resolution: resp,
		TraceID:    traceID(r),
	})
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

func (h *Handler) writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "feed direction request failed"
	switch {
	case errors.Is(err, feedapp.ErrMissingRequiredField):
		status = http.StatusBadRequest
		code = "missing_required_field"
		message = "tenant, exception id, actor, resolution reason, and Idempotency-Key are required"
	case errors.Is(err, feedapp.ErrInvalidResolutionAction):
		status = http.StatusBadRequest
		code = "invalid_action"
		message = "action must be resolve or dismiss"
	case errors.Is(err, feedapp.ErrInvalidIdempotencyKey):
		status = http.StatusBadRequest
		code = "invalid_idempotency_key"
		message = "Idempotency-Key must be between 8 and 200 characters"
	case errors.Is(err, feedapp.ErrInvalidResolutionReason):
		status = http.StatusBadRequest
		code = "invalid_resolution_reason"
		message = "resolution_reason may not exceed 2000 characters"
	case errors.Is(err, feedapp.ErrInvalidResolutionRef):
		status = http.StatusBadRequest
		code = "invalid_resolution_ref"
		message = "resolution_ref may not exceed 500 characters"
	case errors.Is(err, feedapp.ErrProjectionExceptionNotFound):
		status = http.StatusNotFound
		code = "not_found"
		message = "counts projection exception was not found"
	case errors.Is(err, feedapp.ErrIdempotencyConflict):
		status = http.StatusConflict
		code = "idempotency_conflict"
		message = "Idempotency-Key was reused with different resolution data"
	case errors.Is(err, feedapp.ErrProjectionExceptionClosed):
		status = http.StatusConflict
		code = "exception_closed"
		message = "counts projection exception is already closed"
	case errors.Is(err, feedapp.ErrCountsResolverUnavailable):
		status = http.StatusInternalServerError
		code = "resolver_unavailable"
		message = "counts projection exception resolver is unavailable"
	}
	httpresponse.WriteError(w, r, h.log, status, errorEnvelope{Code: code, Message: message, TraceID: traceID(r)}, err)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, message string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, errorEnvelope{Code: code, Message: message, TraceID: traceID(r)}, nil)
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *http.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}

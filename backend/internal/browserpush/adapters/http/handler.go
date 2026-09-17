// Package browserpushhttp serves the browser web push registration endpoints.
package browserpushhttp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/browserpush"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// errorEnvelope mirrors the shared HTTP error envelope every other module serves. It is declared
// here rather than imported from workforce/domain so this package owns no dependency on another
// module just to name a response shape; the JSON tags are what the contract actually pins.
type errorEnvelope struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	FieldErrors []fieldError `json:"field_errors"`
	TraceID     string       `json:"trace_id"`
	Retryable   bool         `json:"retryable"`
}

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Handler serves the three registration routes.
type Handler struct {
	service *browserpush.Service
	log     *slog.Logger
}

// NewHandler wires the handler.
func NewHandler(service *browserpush.Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{service: service, log: log}
}

// Register attaches the routes. Patterns match permissions/routes.go byte for byte.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /admin/notifications/browser-registrations", h.ListRegistrations)
	mux.HandleFunc("POST /admin/notifications/browser-registrations", h.RegisterBrowser)
	mux.HandleFunc("POST /admin/notifications/browser-registrations/unregister", h.UnregisterBrowser)
}

// ListRegistrations returns the caller's own browser registrations.
func (h *Handler) ListRegistrations(w http.ResponseWriter, r *http.Request) {
	registrations, err := h.service.List(r.Context(), tenantID(r), actorID(r))
	if err != nil {
		h.fail(w, r, "browser_push_list_failed", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{"registrations": registrations})
}

// RegisterBrowser stores or refreshes the calling browser's push address.
func (h *Handler) RegisterBrowser(w http.ResponseWriter, r *http.Request) {
	var body browserpush.RegisterRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	// The browser is not asked for its user agent -- the header is the honest source and the
	// body's copy would be a second, forgeable one. It is diagnostics only either way.
	if strings.TrimSpace(body.UserAgent) == "" {
		body.UserAgent = r.UserAgent()
	}
	result, err := h.service.Register(r.Context(), browserpush.RegisterCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		Body:     body,
	})
	if err != nil {
		h.fail(w, r, "browser_push_register_failed", err)
		return
	}
	// The token is NEVER logged, not even fingerprinted here: it is a bearer push credential
	// (see the notification service's own note on fingerprintRecipientRef). The browser install
	// id is a client-generated opaque id and is safe.
	h.log.InfoContext(r.Context(), "browser_push_register_succeeded",
		slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
		slog.String("trace_id", traceID(r)),
		slog.String("tenant_id", tenantID(r)),
		slog.String("workforce_member_id", result.Registration.WorkforceMemberID),
		slog.String("browser_install_id", result.Registration.BrowserInstallID),
		slog.Bool("created", result.Created),
	)
	httpresponse.WriteJSON(w, http.StatusOK, result)
}

// UnregisterBrowser switches the calling browser off.
func (h *Handler) UnregisterBrowser(w http.ResponseWriter, r *http.Request) {
	var body browserpush.UnregisterRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.Unregister(r.Context(), browserpush.UnregisterCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		Body:     body,
	})
	if err != nil {
		h.fail(w, r, "browser_push_unregister_failed", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, result)
}

// fail maps a service error onto the shared error envelope. A validation error and a caller who
// is not an active member of the tenant are distinct 4xx answers; anything else is a 500, because
// guessing would hide a real fault behind a "your request was bad" the client cannot act on.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, event string, err error) {
	status := http.StatusInternalServerError
	envelope := errorEnvelope{
		Code:        "internal_error",
		Message:     "internal server error",
		FieldErrors: []fieldError{},
		TraceID:     traceID(r),
		Retryable:   true,
	}
	switch {
	case errors.Is(err, browserpush.ErrRegistrationNotFound):
		status = http.StatusForbidden
		envelope = errorEnvelope{
			Code:        "browser_push_member_not_found",
			Message:     "this account is not an active member of the tenant",
			FieldErrors: []fieldError{},
			TraceID:     traceID(r),
		}
	case isValidationError(err):
		status = http.StatusBadRequest
		envelope = errorEnvelope{
			Code:        "browser_push_invalid_request",
			Message:     err.Error(),
			FieldErrors: []fieldError{},
			TraceID:     traceID(r),
		}
	}
	h.log.WarnContext(r.Context(), event,
		slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
		slog.String("trace_id", traceID(r)),
		slog.String("tenant_id", tenantID(r)),
		slog.Int("status", status),
		slog.String("error", err.Error()),
	)
	httpresponse.WriteError(w, r, h.log, status, envelope, err)
}

// isValidationError recognises the service's own input refusals. They are plain errors rather
// than sentinels because each names exactly one field and the message IS the answer.
func isValidationError(err error) bool {
	text := err.Error()
	return strings.Contains(text, "is required") ||
		strings.Contains(text, "is too long") ||
		strings.Contains(text, "contains control characters")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		writeBadJSON(w, r, "request body is too large or unreadable")
		return false
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeBadJSON(w, r, "request body must be valid JSON")
		return false
	}
	return true
}

func writeBadJSON(w http.ResponseWriter, r *http.Request, message string) {
	httpresponse.WriteJSON(w, http.StatusBadRequest, errorEnvelope{
		Code:        "invalid_request_body",
		Message:     message,
		FieldErrors: []fieldError{},
		TraceID:     traceID(r),
	})
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

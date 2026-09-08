// Package http serves the notification-audience matrix on /admin/notifications/designations.
package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/notificationaudience/app"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// Handler serves the People / HRMS "Notifications" tab.
type Handler struct {
	service *app.ConfigService
	log     *slog.Logger
}

func NewHandler(service *app.ConfigService, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

// Register mounts the two routes. Their permissions live in permissions/routes.go: reading the
// matrix is OperatorsRead (the directory it sits beside), writing it is OperatorsManageCapability
// (deciding what every colleague is told is the same authority as deciding what they may do).
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /admin/notifications/designations", h.Matrix)
	mux.HandleFunc("PUT /admin/notifications/designations/{alert_key}", h.Save)
}

func (h *Handler) Matrix(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Matrix(r.Context(), httpmiddleware.TenantIDFromContext(r.Context()))
	h.respond(w, r, result, err)
}

func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	alertKey := strings.TrimSpace(r.PathValue("alert_key"))
	var req app.SaveAudienceRequest
	dec := json.NewDecoder(r.Body)
	// Reject an unknown field rather than dropping it: on an audience write, a misspelled key
	// that decodes to nothing is a tick the admin believes they made and the server never stored.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "That request could not be read. Reload the page and try again.")
		return
	}
	result, err := h.service.Save(r.Context(), httpmiddleware.TenantIDFromContext(r.Context()), httpmiddleware.ActorIDFromContext(r.Context()), alertKey, req)
	h.respond(w, r, result, err)
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode(payload); encodeErr != nil {
			h.log.ErrorContext(r.Context(), "notification audience: encode response", "error", encodeErr)
		}
		return
	}
	switch {
	case errors.Is(err, ports.ErrUnknownAlert):
		h.writeError(w, http.StatusNotFound, "unknown_alert", "That alert is no longer configurable. Reload the page.")
	case errors.Is(err, ports.ErrUnknownDesignation):
		h.writeError(w, http.StatusBadRequest, "unknown_designation", "One of the selected designations is no longer active. Reload and choose again.")
	case errors.Is(err, ports.ErrVersionConflict):
		h.writeError(w, http.StatusConflict, "audience_changed",
			"Someone else changed who receives this alert while you had it open. Reload to see the current settings, then make your changes again.")
	case errors.Is(err, app.ErrInvalidRequest):
		h.writeError(w, http.StatusBadRequest, "invalid_request", trimReason(err.Error()))
	default:
		h.log.ErrorContext(r.Context(), "notification audience: unhandled", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internal_error", "That could not be saved. Try again.")
	}
}

func trimReason(msg string) string {
	if _, after, found := strings.Cut(msg, ": "); found {
		return after
	}
	return msg
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

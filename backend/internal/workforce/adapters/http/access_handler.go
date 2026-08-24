package workforcehttp

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// AccessHandler serves the People/HRMS access editor.
type AccessHandler struct {
	service *app.AccessService
	log     *slog.Logger
}

func NewAccessHandler(service *app.AccessService, log ...*slog.Logger) *AccessHandler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &AccessHandler{service: service, log: l}
}

func RegisterAccess(mux *http.ServeMux, h *AccessHandler) {
	mux.HandleFunc("GET /admin/workforce/people/{person_id}/access", h.GetAccess)
	mux.HandleFunc("PUT /admin/workforce/people/{person_id}/access", h.SaveAccess)
	mux.HandleFunc("GET /admin/workforce/designations/{code}/defaults", h.DesignationDefaults)
}

func (h *AccessHandler) GetAccess(w http.ResponseWriter, r *http.Request) {
	personID := strings.TrimSpace(r.PathValue("person_id"))
	result, err := h.service.GetPersonAccess(r.Context(), tenantID(r), personID)
	h.respond(w, r, result, err)
}

func (h *AccessHandler) SaveAccess(w http.ResponseWriter, r *http.Request) {
	personID := strings.TrimSpace(r.PathValue("person_id"))

	var req domain.SavePersonAccessRequest
	dec := json.NewDecoder(r.Body)
	// Reject an unknown field rather than dropping it. On an access write, a
	// misspelled key that decodes to nothing is a tick the admin believes they
	// made and the server never stored.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "That request could not be read. Reload the page and try again.")
		return
	}

	result, err := h.service.SavePersonAccess(r.Context(), tenantID(r), actorID(r), personID, req)
	h.respond(w, r, result, err)
}

func (h *AccessHandler) DesignationDefaults(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.DesignationDefaults(r.Context(), strings.TrimSpace(r.PathValue("code")))
	h.respond(w, r, result, err)
}

func (h *AccessHandler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode(payload); encodeErr != nil {
			h.log.ErrorContext(r.Context(), "workforce access: encode response", "error", encodeErr)
		}
		return
	}

	switch {
	case errors.Is(err, ports.ErrPersonNotFound):
		h.writeError(w, http.StatusNotFound, "person_not_found", "That person is no longer on the roster.")
	case errors.Is(err, ports.ErrAccessVersionConflict):
		// 409, never a silent overwrite: someone else changed this person's access
		// while the editor was open, and the admin must see what it is now before
		// re-deciding.
		h.writeError(w, http.StatusConflict, "access_changed",
			"Someone else changed this person's access while you had it open. Reload to see the current settings, then make your changes again.")
	case errors.Is(err, ports.ErrUnknownPark):
		h.writeError(w, http.StatusBadRequest, "unknown_park", "One of the selected parks is no longer active. Reload and choose again.")
	case errors.Is(err, app.ErrInvalidAccessRequest):
		// The service's message names WHICH tick was refused and why, in farm words.
		h.writeError(w, http.StatusBadRequest, "invalid_access", trimReason(err.Error()))
	default:
		h.log.ErrorContext(r.Context(), "workforce access: unhandled", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internal_error", "That could not be saved. Try again.")
	}
}

// trimReason strips the sentinel prefix so the admin reads the reason, not the
// error chain: "invalid access request: Feed is not available on the phone"
// becomes "Feed is not available on the phone".
func trimReason(msg string) string {
	if _, after, found := strings.Cut(msg, ": "); found {
		return after
	}
	return msg
}

func (h *AccessHandler) writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

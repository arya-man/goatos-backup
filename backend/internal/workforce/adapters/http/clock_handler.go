package workforcehttp

import (
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// ClockHandler serves the Clock In / Clock Out module: the punch writes and
// status read for everyone, the presence board for leadership, and the
// admin-web People/HRMS clock tab.
type ClockHandler struct {
	service *app.ClockService
	log     *slog.Logger
}

func NewClockHandler(service *app.ClockService, log ...*slog.Logger) *ClockHandler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &ClockHandler{service: service, log: l}
}

func RegisterClock(mux *http.ServeMux, h *ClockHandler) {
	mux.HandleFunc("POST /app/clock/in", h.ClockIn)
	mux.HandleFunc("POST /app/clock/out", h.ClockOut)
	mux.HandleFunc("GET /app/clock/status", h.Status)
	mux.HandleFunc("GET /app/clock/presence", h.Presence)
	mux.HandleFunc("GET /app/clock/presence/{workforce_member_id}", h.PersonDay)
	mux.HandleFunc("GET /admin/workforce/clock-entries", h.AdminEntries)
	mux.HandleFunc("GET /admin/workforce/clock-entries/{clock_entry_id}", h.EntryDetail)
}

func (h *ClockHandler) punch(w http.ResponseWriter, r *http.Request, eventType string) {
	var body domain.ClockPunchRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = idempotencyKeyHeader(r)
	}
	result, err := h.service.Punch(r.Context(), tenantID(r), actorID(r), eventType, body,
		httpmiddleware.ClientInfoFromContext(r.Context()),
		httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	if err != nil {
		h.log.WarnContext(r.Context(), "clock_punch_failed",
			slog.String("trace_id", traceID(r)),
			slog.String("actor_id", actorID(r)),
			slog.String("event_type", eventType),
			slog.String("error", err.Error()),
		)
	}
	h.respond(w, r, result, err)
}

func (h *ClockHandler) ClockIn(w http.ResponseWriter, r *http.Request)  { h.punch(w, r, "clock_in") }
func (h *ClockHandler) ClockOut(w http.ResponseWriter, r *http.Request) { h.punch(w, r, "clock_out") }

func (h *ClockHandler) Status(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Status(r.Context(), tenantID(r), actorID(r),
		httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func presenceParams(r *http.Request) ports.ClockPresenceParams {
	q := r.URL.Query()
	return ports.ClockPresenceParams{
		BusinessDate: q.Get("date"),
		ParkID:       q.Get("park_id"),
		RoleHint:     q.Get("designation"),
		Bucket:       q.Get("bucket"),
		Search:       q.Get("q"),
		Limit:        parseLimit(q.Get("limit")),
		Cursor:       q.Get("cursor"),
	}
}

func (h *ClockHandler) Presence(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Presence(r.Context(), tenantID(r), presenceParams(r),
		httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *ClockHandler) PersonDay(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.PersonDay(r.Context(), tenantID(r),
		r.PathValue("workforce_member_id"), r.URL.Query().Get("date"),
		httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *ClockHandler) AdminEntries(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.AdminEntries(r.Context(), tenantID(r), presenceParams(r),
		httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *ClockHandler) EntryDetail(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.EntryDetail(r.Context(), tenantID(r),
		r.PathValue("clock_entry_id"),
		httpmiddleware.LocaleTagFromRequest(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *ClockHandler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	writeServiceResponse(w, r, h.log, payload, err)
}

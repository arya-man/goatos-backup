// Package http exposes generic Calendar APIs for the vaccination slice.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"strconv"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/app"
	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Service interface {
	ListEvents(ctx context.Context, q domain.Query) (domain.CalendarEventListResponse, error)
	GetEventDetail(ctx context.Context, tenantID, eventID string) (domain.CalendarEventDetail, error)
	History(ctx context.Context, q domain.HistoryQuery) (domain.CalendarHistoryResponse, error)
	SendNudge(ctx context.Context, in ports.SendNudge) (domain.CalendarActionResponse, error)
	Snooze(ctx context.Context, in ports.Snooze) (domain.CalendarActionResponse, error)
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

func Register(mux *stdhttp.ServeMux, h *Handler) {
	mux.HandleFunc("GET /calendar/vaccination/events", h.ListEvents)
	mux.HandleFunc("GET /calendar/vaccination/events/{event_id}", h.GetEventDetail)
	mux.HandleFunc("GET /calendar/vaccination/events/{event_id}/history", h.History)
	mux.HandleFunc("POST /calendar/vaccination/events/{event_id}/nudge", h.SendNudge)
	mux.HandleFunc("POST /calendar/vaccination/events/{event_id}/snooze", h.Snooze)
}

func (h *Handler) ListEvents(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	q, ok := h.listQuery(w, r)
	if !ok {
		return
	}
	resp, err := h.service.ListEvents(r.Context(), q)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) GetEventDetail(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	resp, err := h.service.GetEventDetail(r.Context(), tenantID(r), r.PathValue("event_id"))
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) History(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := r.URL.Query()
	q := domain.HistoryQuery{TenantID: tenantID(r), EventID: r.PathValue("event_id")}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := domain.DecodeHistoryCursor(raw)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor is invalid")
			return
		}
		q.Cursor = &cursor
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		q.Limit = limit
	}
	resp, err := h.service.History(r.Context(), q)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) SendNudge(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	defer r.Body.Close()
	var req domain.NudgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		h.badRequest(w, r, "invalid_body", "request body must be JSON")
		return
	}
	resp, err := h.service.SendNudge(r.Context(), ports.SendNudge{
		TenantID:       tenantID(r),
		EventID:        r.PathValue("event_id"),
		ActorID:        actorID(r),
		TraceID:        traceID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		Channel:        req.Channel,
		Message:        req.Message,
		Reason:         req.Reason,
	})
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) Snooze(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	defer r.Body.Close()
	var req domain.SnoozeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.badRequest(w, r, "invalid_body", "request body must be JSON")
		return
	}
	resp, err := h.service.Snooze(r.Context(), ports.Snooze{
		TenantID:        tenantID(r),
		EventID:         r.PathValue("event_id"),
		ActorID:         actorID(r),
		TraceID:         traceID(r),
		IdempotencyKey:  r.Header.Get("Idempotency-Key"),
		SnoozeUntil:     req.SnoozeUntil,
		Reason:          req.Reason,
		ReplaceExisting: req.ReplaceExisting,
	})
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) listQuery(w stdhttp.ResponseWriter, r *stdhttp.Request) (domain.Query, bool) {
	query := r.URL.Query()
	q := domain.Query{
		TenantID: tenantID(r),
		OwnerKey: query.Get("owner_key"),
	}
	if raw := query.Get("park_id"); raw != "" {
		q.ParkID = &raw
	}
	if raw := query.Get("shed_id"); raw != "" {
		q.ShedID = &raw
	}
	if raw := query.Get("status"); raw != "" {
		q.Status = &raw
	}
	if raw := query.Get("date_from"); raw != "" {
		date, ok := h.parseDate(w, r, "date_from", raw)
		if !ok {
			return domain.Query{}, false
		}
		q.DateFrom = date
	}
	if raw := query.Get("date_to"); raw != "" {
		date, ok := h.parseDate(w, r, "date_to", raw)
		if !ok {
			return domain.Query{}, false
		}
		q.DateTo = date
	}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := domain.DecodeCalendarCursor(raw)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor is invalid")
			return domain.Query{}, false
		}
		q.Cursor = &cursor
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return domain.Query{}, false
		}
		q.Limit = limit
	}
	return q, true
}

func (h *Handler) parseDate(w stdhttp.ResponseWriter, r *stdhttp.Request, field, raw string) (time.Time, bool) {
	loc, err := time.LoadLocation(domain.DefaultTimezone)
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		h.badRequest(w, r, "invalid_"+field, field+" must be YYYY-MM-DD")
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

func (h *Handler) writeAppError(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	var appErr app.Error
	if errors.As(err, &appErr) {
		status := stdhttp.StatusInternalServerError
		switch appErr.Code {
		case "not_found":
			status = stdhttp.StatusNotFound
		case "idempotency_conflict", "active_snooze_exists", "event_not_actionable":
			status = stdhttp.StatusConflict
		case "internal_error":
			status = stdhttp.StatusInternalServerError
		default:
			status = stdhttp.StatusBadRequest
		}
		httpresponse.WriteError(w, r, h.log, status, errorEnvelope{
			Code: appErr.Code, Message: appErr.Message, TraceID: traceID(r),
		}, nil)
		return
	}
	h.internal(w, r, err)
}

func (h *Handler) badRequest(w stdhttp.ResponseWriter, r *stdhttp.Request, code, message string) {
	httpresponse.WriteError(w, r, h.log, stdhttp.StatusBadRequest, errorEnvelope{Code: code, Message: message, TraceID: traceID(r)}, nil)
}

func (h *Handler) internal(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	httpresponse.WriteError(w, r, h.log, stdhttp.StatusInternalServerError, errorEnvelope{
		Code: "internal_error", Message: "calendar request failed", TraceID: traceID(r),
	}, err)
}

func tenantID(r *stdhttp.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *stdhttp.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

func traceID(r *stdhttp.Request) string {
	return httpmiddleware.TraceIDFromContext(r.Context())
}

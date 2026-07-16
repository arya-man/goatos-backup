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
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/app"
	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

const maxActionBodyBytes = 16 * 1024

type Service interface {
	ListEvents(ctx context.Context, q domain.Query) (domain.CalendarEventListResponse, error)
	GetEventDetail(ctx context.Context, q domain.EventQuery) (domain.CalendarEventDetail, error)
	ListDriveTargets(ctx context.Context, q domain.DriveTargetQuery) (domain.CalendarDriveTargetListResponse, error)
	History(ctx context.Context, q domain.HistoryQuery) (domain.CalendarHistoryResponse, error)
	SendNudge(ctx context.Context, in ports.SendNudge) (domain.CalendarActionResponse, error)
	Snooze(ctx context.Context, in ports.Snooze) (domain.CalendarActionResponse, error)
	AcknowledgeEscalation(ctx context.Context, in ports.AcknowledgeEscalation) (domain.CalendarActionResponse, error)
	ResolveEscalation(ctx context.Context, in ports.ResolveEscalation) (domain.CalendarActionResponse, error)
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
	mux.HandleFunc("GET /calendar/vaccination/events/{event_id}/targets", h.ListDriveTargets)
	mux.HandleFunc("GET /calendar/vaccination/events/{event_id}/history", h.History)
	mux.HandleFunc("POST /calendar/vaccination/events/{event_id}/nudge", h.SendNudge)
	mux.HandleFunc("POST /calendar/vaccination/events/{event_id}/snooze", h.Snooze)
	mux.HandleFunc("POST /calendar/vaccination/events/{event_id}/escalation/acknowledge", h.AcknowledgeEscalation)
	mux.HandleFunc("POST /calendar/vaccination/events/{event_id}/escalation/resolve", h.ResolveEscalation)
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
	resp, err := h.service.GetEventDetail(r.Context(), domain.EventQuery{
		TenantID: tenantID(r),
		EventID:  r.PathValue("event_id"),
		Scope:    calendarScope(r, permissions.CalendarRead),
	})
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) ListDriveTargets(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := r.URL.Query()
	q := domain.DriveTargetQuery{
		TenantID: tenantID(r),
		EventID:  r.PathValue("event_id"),
		Scope:    calendarScope(r, permissions.CalendarRead),
		Search:   strings.TrimSpace(query.Get("q")),
	}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := domain.DecodeDriveTargetCursor(raw)
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
	resp, err := h.service.ListDriveTargets(r.Context(), q)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) History(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := r.URL.Query()
	q := domain.HistoryQuery{TenantID: tenantID(r), EventID: r.PathValue("event_id"), Scope: calendarScope(r, permissions.CalendarRead)}
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
	r.Body = stdhttp.MaxBytesReader(w, r.Body, maxActionBodyBytes)
	defer r.Body.Close()
	var req domain.NudgeRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
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
		Scope:          calendarScope(r, permissions.CalendarAction),
	})
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) Snooze(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	r.Body = stdhttp.MaxBytesReader(w, r.Body, maxActionBodyBytes)
	defer r.Body.Close()
	var req domain.SnoozeRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
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
		Scope:           calendarScope(r, permissions.CalendarAction),
	})
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) AcknowledgeEscalation(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	r.Body = stdhttp.MaxBytesReader(w, r.Body, maxActionBodyBytes)
	defer r.Body.Close()
	var req domain.EscalationActionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		h.badRequest(w, r, "invalid_body", "request body must be JSON")
		return
	}
	resp, err := h.service.AcknowledgeEscalation(r.Context(), ports.AcknowledgeEscalation{
		TenantID:       tenantID(r),
		EventID:        r.PathValue("event_id"),
		ActorID:        actorID(r),
		TraceID:        traceID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		Reason:         req.Reason,
		Scope:          calendarScope(r, permissions.CalendarAction),
		ActorGrants:    calendarActorGrants(r, permissions.CalendarAction),
	})
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, stdhttp.StatusOK, resp)
}

func (h *Handler) ResolveEscalation(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	r.Body = stdhttp.MaxBytesReader(w, r.Body, maxActionBodyBytes)
	defer r.Body.Close()
	var req domain.EscalationActionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.badRequest(w, r, "invalid_body", "request body must be JSON")
		return
	}
	resp, err := h.service.ResolveEscalation(r.Context(), ports.ResolveEscalation{
		TenantID:       tenantID(r),
		EventID:        r.PathValue("event_id"),
		ActorID:        actorID(r),
		TraceID:        traceID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		Reason:         req.Reason,
		Scope:          calendarScope(r, permissions.CalendarAction),
		ActorGrants:    calendarActorGrants(r, permissions.CalendarAction),
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
		TenantID:            tenantID(r),
		OwnerKey:            query.Get("owner_key"),
		IncludeDateMarkers:  query.Get("include_date_markers") == "true",
		IncludeReminderRail: query.Get("include_reminder_rail") == "true",
		Scope:               calendarScope(r, permissions.CalendarRead),
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
	loc := biztime.Location(domain.DefaultTimezone)
	parsed, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		h.badRequest(w, r, "invalid_"+field, field+" must be YYYY-MM-DD")
		return time.Time{}, false
	}
	return parsed, true
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
		case "permission_denied":
			status = stdhttp.StatusForbidden
		case "idempotency_conflict", "active_snooze_exists", "event_not_actionable":
			status = stdhttp.StatusConflict
		case "idempotency_in_progress":
			status = stdhttp.StatusConflict
		case "internal_error":
			status = stdhttp.StatusInternalServerError
		case "projection_unavailable", "projection_stale":
			status = stdhttp.StatusServiceUnavailable
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

func calendarScope(r *stdhttp.Request, permission string) domain.ScopeFilter {
	tenant := tenantID(r)
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	filter := domain.ScopeFilter{}
	parkSeen := map[string]struct{}{}
	shedSeen := map[string]struct{}{}
	for _, grant := range grants {
		if !permissions.RoleHasPermission(grant.Role, permission) {
			continue
		}
		switch grant.ScopeType {
		case "tenant":
			if strings.EqualFold(grant.ScopeID, tenant) {
				filter.TenantWide = true
			}
		case "park":
			if grant.ScopeID != "" {
				if _, ok := parkSeen[grant.ScopeID]; !ok {
					parkSeen[grant.ScopeID] = struct{}{}
					filter.ParkIDs = append(filter.ParkIDs, grant.ScopeID)
				}
			}
		case "shed":
			if grant.ScopeID != "" {
				if _, ok := shedSeen[grant.ScopeID]; !ok {
					shedSeen[grant.ScopeID] = struct{}{}
					filter.ShedIDs = append(filter.ShedIDs, grant.ScopeID)
				}
			}
		}
	}
	return filter
}

func calendarActorGrants(r *stdhttp.Request, permission string) []ports.ActorGrant {
	tenant := tenantID(r)
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	out := make([]ports.ActorGrant, 0, len(grants))
	for _, grant := range grants {
		if !permissions.RoleHasPermission(grant.Role, permission) {
			continue
		}
		switch grant.ScopeType {
		case "tenant":
			if !strings.EqualFold(grant.ScopeID, tenant) {
				continue
			}
		case "park", "shed":
			if grant.ScopeID == "" {
				continue
			}
		default:
			continue
		}
		out = append(out, ports.ActorGrant{Role: grant.Role, ScopeType: grant.ScopeType, ScopeID: grant.ScopeID})
	}
	return out
}

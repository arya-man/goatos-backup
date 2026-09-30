// Package http is the Pen Routines transport: the assignee's own list, one task, the check-in
// and the submit (app routes), and the CEO's authoring routes (admin routes).
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/penroutines/app"
	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Service is the slice of the app service the assignee transport needs.
type Service interface {
	ListMine(ctx context.Context, tenantID, userID string, q app.ListQuery) (ports.Page, *domain.Tab, error)
	GetTask(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error)
	RecordPresence(ctx context.Context, p ports.PresenceParams) (domain.Task, error)
	Submit(ctx context.Context, p ports.SubmitParams) (domain.Task, error)
	Today() string
}

// Handler serves the app routes.
type Handler struct {
	service Service
	log     *slog.Logger
}

// NewHandler constructs the assignee transport.
func NewHandler(service Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{service: service, log: log}
}

// Register mounts the app routes.
//
// Patterns here must stay byte-identical to the entries in permissions/routes.go -- the
// permission table is matched by method + pattern, and a mismatch serves the route ungated.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /app/pen-routines", h.ListMine)
	mux.HandleFunc("GET /app/pen-routines/{task_id}", h.GetTask)
	mux.HandleFunc("POST /app/pen-routines/{task_id}/presence", h.RecordPresence)
	mux.HandleFunc("POST /app/pen-routines/{task_id}/submit", h.Submit)
}

// maxRequestBytes caps a write body: answers, a few proof ids and a row version.
const maxRequestBytes = 64 * 1024

// ListMine serves GET /app/pen-routines.
func (h *Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filterKey := domain.FilterKeyOrDefault(q.Get("filter"))
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErr(w, r, h.log, app.BadRequest("invalid_limit", "That page size is not valid."))
			return
		}
		limit = parsed
	}
	actor := actorFrom(r)
	shedID, partition, _ := strings.Cut(strings.TrimSpace(q.Get("pen")), "|")
	page, tab, err := h.service.ListMine(r.Context(), tenantID(r), actor.UserID, app.ListQuery{
		FilterKey:    filterKey,
		Limit:        limit,
		Cursor:       q.Get("cursor"),
		TabKey:       q.Get("tab"),
		DueFrom:      q.Get("due_from"),
		DueTo:        q.Get("due_to"),
		PenShedID:    shedID,
		PenPartition: partition,
	})
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	today := h.service.Today()
	rows := make([]stepPayload, 0, len(page.Rows))
	for _, t := range page.Rows {
		rows = append(rows, domain.StepFor(t, actor, today))
	}
	var next *string
	if page.NextCursor != "" {
		c := page.NextCursor
		next = &c
	}
	out := pagePayload{
		Title:      listTitle,
		Rows:       rows,
		NextCursor: next,
		Filters:    toFilterPayloads(filterKey, page.StateCounts),
		OpenCount:  domain.FilterCount(domain.FilterToDo, page.StateCounts),
		PenOptions: toPenOptionPayloads(page.PenOptions),
		TraceID:    traceID(r),
	}
	if tab != nil {
		out.Title = tab.Label
		out.Tab = &phoneTabPayload{Key: tab.Key, Label: tab.Label, Filters: tab.Filters}
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

// GetTask serves GET /app/pen-routines/{task_id}.
func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	task, err := h.service.GetTask(r.Context(), tenantID(r), actor, r.PathValue("task_id"))
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, detailPayload{Task: domain.StepFor(task, actor, h.service.Today()), TraceID: traceID(r)})
}

// RecordPresence serves POST /app/pen-routines/{task_id}/presence.
func (h *Handler) RecordPresence(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body presencePayload
	if !decode(w, r, h.log, &body) {
		return
	}
	capturedAt, err := parseInstant(body.CapturedAt)
	if err != nil {
		writeErr(w, r, h.log, app.BadRequest("invalid_body", "That request could not be read. Try again."))
		return
	}
	actor := actorFrom(r)
	task, err := h.service.RecordPresence(r.Context(), ports.PresenceParams{
		TenantID:       tenantID(r),
		Actor:          actor,
		TaskID:         r.PathValue("task_id"),
		EventType:      strings.TrimSpace(body.EventType),
		CapturedAt:     capturedAt,
		Location:       body.Location,
		Integrity:      body.Integrity,
		RowVersion:     body.RowVersion,
		IdempotencyKey: key,
		TraceID:        traceID(r),
	})
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, detailPayload{Task: domain.StepFor(task, actor, h.service.Today()), TraceID: traceID(r)})
}

// Submit serves POST /app/pen-routines/{task_id}/submit.
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body submitPayload
	if !decode(w, r, h.log, &body) {
		return
	}
	capturedAt, err := parseInstant(body.CapturedAt)
	if err != nil {
		writeErr(w, r, h.log, app.BadRequest("invalid_body", "That request could not be read. Try again."))
		return
	}
	actor := actorFrom(r)
	task, err := h.service.Submit(r.Context(), ports.SubmitParams{
		TenantID:       tenantID(r),
		Actor:          actor,
		TaskID:         r.PathValue("task_id"),
		Answers:        body.Answers,
		Proofs:         body.ProofRefs,
		RowVersion:     body.RowVersion,
		CapturedAt:     capturedAt,
		Location:       body.Location,
		Integrity:      body.Integrity,
		IdempotencyKey: key,
		TraceID:        traceID(r),
	})
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, detailPayload{Task: domain.StepFor(task, actor, h.service.Today()), TraceID: traceID(r)})
}

func parseInstant(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func idempotencyKey(w http.ResponseWriter, r *http.Request, log *slog.Logger) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeErr(w, r, log, app.BadRequest("missing_idempotency_key", "This could not be saved safely. Try again."))
		return "", false
	}
	return key, true
}

// decode reads a JSON write body, failing loud on unknown fields.
func decode(w http.ResponseWriter, r *http.Request, log *slog.Logger, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, r, log, app.BadRequest("invalid_body", "That request could not be read. Try again."))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeErr(w, r, log, app.BadRequest("invalid_body", "That request could not be read. Try again."))
		return false
	}
	return true
}

func toAppError(err error) *app.Error {
	var appErr *app.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return app.HTTPError(err)
}

func writeErr(w http.ResponseWriter, r *http.Request, log *slog.Logger, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}

func tenantID(r *http.Request) string {
	return strings.TrimSpace(httpmiddleware.TenantIDFromContext(r.Context()))
}

func traceID(r *http.Request) string {
	return httpmiddleware.TraceIDFromContext(r.Context())
}

// actorFrom resolves who is asking. Authority is the route table's (pen_routines.execute);
// WHO may work a task is decided per row by the routine's roles resolved against the caller's
// grants for the task's park (adapters/postgres/assignees.go), never by a role string here.
func actorFrom(r *http.Request) domain.Actor {
	return domain.Actor{UserID: strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context()))}
}

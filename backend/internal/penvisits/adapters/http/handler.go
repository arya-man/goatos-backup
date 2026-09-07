// Package http is the Pen Visit transport: the park head's own list, one visit, and the submit
// that carries the video.
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

	"github.com/vgoats/goatos/backend/internal/penvisits/app"
	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Service is the slice of the app service the transport needs.
type Service interface {
	ListMine(ctx context.Context, tenantID, userID, filterKey string, limit int, cursor string) (ports.Page, error)
	GetTask(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error)
	Submit(ctx context.Context, p ports.SubmitParams) (domain.Task, error)
	Today() string
}

// Handler serves the routes.
type Handler struct {
	service Service
	log     *slog.Logger
}

// NewHandler constructs the transport.
func NewHandler(service Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{service: service, log: log}
}

// Register mounts the routes.
//
// Patterns here must stay byte-identical to the entries in permissions/routes.go -- the
// permission table is matched by method + pattern, and a mismatch serves the route ungated.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /app/pen-visits", h.ListMine)
	mux.HandleFunc("GET /app/pen-visits/{task_id}", h.GetTask)
	mux.HandleFunc("POST /app/pen-visits/{task_id}/submit", h.Submit)
}

// maxRequestBytes caps a write body: a proof id and a row version.
const maxRequestBytes = 8 * 1024

// ListMine serves GET /app/pen-visits.
func (h *Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filterKey := domain.FilterKeyOrDefault(q.Get("filter"))
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_limit", "That page size is not valid."))
			return
		}
		limit = parsed
	}
	actor := actorFrom(r)
	page, err := h.service.ListMine(r.Context(), tenantID(r), actor.UserID, filterKey, limit, q.Get("cursor"))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	today := h.service.Today()
	rows := make([]visitPayload, 0, len(page.Rows))
	for _, t := range page.Rows {
		rows = append(rows, toVisitPayload(t, actor, today))
	}
	var next *string
	if page.NextCursor != "" {
		c := page.NextCursor
		next = &c
	}
	httpresponse.WriteJSON(w, http.StatusOK, visitPagePayload{
		Title:      listTitle,
		Rows:       rows,
		NextCursor: next,
		Filters:    toFilterPayloads(filterKey, page.StateCounts),
		OpenCount:  domain.FilterCount(domain.FilterToDo, page.StateCounts),
		TraceID:    traceID(r),
	})
}

// GetTask serves GET /app/pen-visits/{task_id}.
func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	task, err := h.service.GetTask(r.Context(), tenantID(r), actor, r.PathValue("task_id"))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, visitDetailPayload{Task: toVisitPayload(task, actor, h.service.Today()), TraceID: traceID(r)})
}

// Submit serves POST /app/pen-visits/{task_id}/submit.
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var body submitPayload
	if !h.decode(w, r, &body) {
		return
	}
	actor := actorFrom(r)
	task, err := h.service.Submit(r.Context(), ports.SubmitParams{
		TenantID:       tenantID(r),
		Actor:          actor,
		TaskID:         r.PathValue("task_id"),
		ProofRef:       strings.TrimSpace(body.ProofRef),
		RowVersion:     body.RowVersion,
		IdempotencyKey: key,
		TraceID:        traceID(r),
	})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, visitDetailPayload{Task: toVisitPayload(task, actor, h.service.Today()), TraceID: traceID(r)})
}

func (h *Handler) idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This could not be saved safely. Try again."))
		return "", false
	}
	return key, true
}

// decode reads a JSON write body, failing loud on unknown fields.
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That request could not be read. Try again."))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That request could not be read. Try again."))
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

func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
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

// actorFrom resolves who is asking. Authority is the route table's (pen_visits.execute); who
// OWNS a visit is decided against the stored row, never a role string the client sends.
func actorFrom(r *http.Request) domain.Actor {
	return domain.Actor{UserID: strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context()))}
}

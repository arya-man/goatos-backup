// Package http serves the Leadership Tasks routes on the phone: the caller's task list and
// detail, the raise/edit/status/seen writes, and the assignee picker.
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

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/app"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Service is the behaviour this transport depends on.
type Service interface {
	ListAssignees(ctx context.Context, tenantID string) ([]ports.Assignee, error)
	ListTasks(ctx context.Context, tenantID, userID, filterKey string, limit int, cursor string) (ports.Page, error)
	GetTask(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error)
	Raise(ctx context.Context, p ports.RaiseParams) (domain.Task, error)
	Edit(ctx context.Context, p ports.EditParams) (domain.Task, error)
	ChangeStatus(ctx context.Context, p ports.StatusParams) (domain.Task, error)
	SetComment(ctx context.Context, p ports.CommentParams) (domain.Task, error)
	MarkSeen(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error)
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
	mux.HandleFunc("GET /app/leadership-tasks", h.ListTasks)
	mux.HandleFunc("GET /app/leadership-tasks/assignees", h.ListAssignees)
	mux.HandleFunc("GET /app/leadership-tasks/{task_id}", h.GetTask)
	mux.HandleFunc("POST /app/leadership-tasks", h.Raise)
	mux.HandleFunc("POST /app/leadership-tasks/{task_id}/edit", h.Edit)
	mux.HandleFunc("POST /app/leadership-tasks/{task_id}/status", h.ChangeStatus)
	mux.HandleFunc("POST /app/leadership-tasks/{task_id}/comment", h.SetComment)
	mux.HandleFunc("POST /app/leadership-tasks/{task_id}/seen", h.MarkSeen)
}

// maxRequestBytes caps a write body: a 4000-rune brief plus 12 attachment refs is well
// under this.
const maxRequestBytes = 64 * 1024

// ListTasks serves GET /app/leadership-tasks.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
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
	page, err := h.service.ListTasks(r.Context(), tenantID(r), actor.UserID, filterKey, limit, q.Get("cursor"))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	rows := make([]taskPayload, 0, len(page.Rows))
	for _, t := range page.Rows {
		rows = append(rows, toTaskPayload(t, actor))
	}
	var next *string
	if page.NextCursor != "" {
		c := page.NextCursor
		next = &c
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskPagePayload{
		Title:       listTitle,
		Rows:        rows,
		NextCursor:  next,
		Filters:     toFilterPayloads(filterKey, page, actor.CanRaise),
		UnseenCount: page.UnseenCount,
		CanRaise:    actor.CanRaise,
		TraceID:     traceID(r),
	})
}

// ListAssignees serves GET /app/leadership-tasks/assignees -- the raise form's picker.
func (h *Handler) ListAssignees(w http.ResponseWriter, r *http.Request) {
	assignees, err := h.service.ListAssignees(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	out := make([]assigneePayload, 0, len(assignees))
	for _, a := range assignees {
		out = append(out, assigneePayload{UserID: a.UserID, Name: a.Name})
	}
	httpresponse.WriteJSON(w, http.StatusOK, assigneesPayload{Assignees: out, TraceID: traceID(r)})
}

// GetTask serves GET /app/leadership-tasks/{task_id}.
func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	task, err := h.service.GetTask(r.Context(), tenantID(r), actor, r.PathValue("task_id"))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor), TraceID: traceID(r)})
}

// Raise serves POST /app/leadership-tasks.
func (h *Handler) Raise(w http.ResponseWriter, r *http.Request) {
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var body raisePayload
	if !h.decode(w, r, &body) {
		return
	}
	actor := actorFrom(r)
	task, err := h.service.Raise(r.Context(), ports.RaiseParams{
		TenantID:       tenantID(r),
		ActorID:        actor.UserID,
		AssigneeUserID: body.AssigneeUserID,
		Title:          body.Title,
		Body:           body.Body,
		Refs:           toRefs(body.Attachments),
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, taskDetailPayload{Task: toTaskPayload(task, actor), TraceID: traceID(r)})
}

// Edit serves POST /app/leadership-tasks/{task_id}/edit.
func (h *Handler) Edit(w http.ResponseWriter, r *http.Request) {
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var body editPayload
	if !h.decode(w, r, &body) {
		return
	}
	actor := actorFrom(r)
	task, err := h.service.Edit(r.Context(), ports.EditParams{
		TenantID:       tenantID(r),
		ActorID:        actor.UserID,
		TaskID:         r.PathValue("task_id"),
		Title:          body.Title,
		Body:           body.Body,
		Refs:           toRefs(body.Attachments),
		RowVersion:     body.RowVersion,
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor), TraceID: traceID(r)})
}

// ChangeStatus serves POST /app/leadership-tasks/{task_id}/status.
func (h *Handler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var body statusPayload
	if !h.decode(w, r, &body) {
		return
	}
	actor := actorFrom(r)
	task, err := h.service.ChangeStatus(r.Context(), ports.StatusParams{
		TenantID:       tenantID(r),
		Actor:          actor,
		TaskID:         r.PathValue("task_id"),
		Status:         body.Status,
		RowVersion:     body.RowVersion,
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor), TraceID: traceID(r)})
}

// SetComment serves POST /app/leadership-tasks/{task_id}/comment -- the assignee's note.
func (h *Handler) SetComment(w http.ResponseWriter, r *http.Request) {
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var body commentPayload
	if !h.decode(w, r, &body) {
		return
	}
	actor := actorFrom(r)
	task, err := h.service.SetComment(r.Context(), ports.CommentParams{
		TenantID:       tenantID(r),
		Actor:          actor,
		TaskID:         r.PathValue("task_id"),
		Comment:        body.Comment,
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor), TraceID: traceID(r)})
}

// MarkSeen serves POST /app/leadership-tasks/{task_id}/seen. Naturally idempotent (a
// set-if-null), so it carries no key: the second tap returns the same task.
func (h *Handler) MarkSeen(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	task, err := h.service.MarkSeen(r.Context(), tenantID(r), actor, r.PathValue("task_id"))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor), TraceID: traceID(r)})
}

func toRefs(in []attachmentRefPayload) []domain.AttachmentRef {
	out := make([]domain.AttachmentRef, 0, len(in))
	for _, a := range in {
		out = append(out, domain.AttachmentRef{ProofID: strings.TrimSpace(a.ProofID), Kind: strings.TrimSpace(a.Kind), FileName: strings.TrimSpace(a.FileName)})
	}
	return out
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
		h.writeErr(w, r, app.BadRequest("invalid_body", "That form could not be read. Check the fields and try again."))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That form could not be read. Check the fields and try again."))
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

// actorFrom resolves who is asking and the two authorities the payload must answer for:
// raise (the "+" and edit/cancel controls) and act (the status buttons). Derived from the
// SAME grants the route table authorized against, never from a role string the client sends.
func actorFrom(r *http.Request) domain.Actor {
	actor := domain.Actor{UserID: strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context()))}
	for _, grant := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if permissions.RoleHasPermission(grant.Role, permissions.LeadershipTasksRaise) {
			actor.CanRaise = true
		}
		if permissions.RoleHasPermission(grant.Role, permissions.LeadershipTasksAct) {
			actor.CanAct = true
		}
	}
	return actor
}

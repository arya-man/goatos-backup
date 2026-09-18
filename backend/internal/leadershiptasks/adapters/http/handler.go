// Package http serves the Leadership Tasks routes on the phone: the caller's task list and
// detail, the raise/edit/status/seen writes, and the assignee picker.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/app"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
)

// Service is the behaviour this transport depends on.
type Service interface {
	ListAssignees(ctx context.Context, tenantID string) ([]ports.Assignee, error)
	ListMentionableUsers(ctx context.Context, tenantID string, actor domain.Actor, taskID string) ([]domain.MentionableUser, error)
	ListTasks(ctx context.Context, req app.ListRequest) (ports.Page, error)
	GetTask(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error)
	ActivityPage(ctx context.Context, tenantID string, actor domain.Actor, taskID, before string) (ports.ActivityPage, error)
	Raise(ctx context.Context, p ports.RaiseParams) (domain.Task, error)
	Edit(ctx context.Context, p ports.EditParams) (domain.Task, error)
	ChangeStatus(ctx context.Context, p ports.StatusParams) (domain.Task, error)
	SetComment(ctx context.Context, p ports.CommentParams) (domain.Task, error)
	MarkSeen(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error)
	AttachmentDownloadURL(ctx context.Context, tenantID string, actor domain.Actor, taskID, proofID string) (app.AttachmentDownload, error)
	// Now is the clock every day counter in a response is read against, so one page never
	// mixes two "todays".
	Now() time.Time
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
	mux.HandleFunc("GET /app/leadership-tasks/{task_id}/activity", h.GetTaskActivity)
	mux.HandleFunc("GET /app/leadership-tasks/{task_id}/mentionable-users", h.ListMentionableUsers)
	mux.HandleFunc("POST /app/leadership-tasks", h.Raise)
	mux.HandleFunc("POST /app/leadership-tasks/{task_id}/edit", h.Edit)
	mux.HandleFunc("POST /app/leadership-tasks/{task_id}/status", h.ChangeStatus)
	mux.HandleFunc("POST /app/leadership-tasks/{task_id}/comment", h.SetComment)
	mux.HandleFunc("POST /app/leadership-tasks/{task_id}/seen", h.MarkSeen)
	mux.HandleFunc("GET /app/leadership-tasks/{task_id}/attachments/{proof_id}/download", h.DownloadAttachment)
}

// maxRequestBytes caps a write body: a 4000-rune brief plus 12 attachment refs is well
// under this.
const maxRequestBytes = 64 * 1024

// ListTasks serves GET /app/leadership-tasks.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	// Unknown query params stay silently ignored on this READ path: the filter bar and the
	// Android client evolve separately, and a stale extra param must not blank the list.
	// (DisallowUnknownFields is for the write bodies only.)
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
	scopeKey := domain.ScopeKeyOrDefault(q.Get("scope"), actor)
	page, err := h.service.ListTasks(r.Context(), app.ListRequest{
		TenantID:       tenantID(r),
		UserID:         actor.UserID,
		ScopeKey:       scopeKey,
		FilterKey:      filterKey,
		Limit:          limit,
		Cursor:         q.Get("cursor"),
		Query:          q.Get("q"),
		AssigneeUserID: q.Get("assignee_user_id"),
		RaisedBy:       q.Get("raised_by"),
		DeadlineFrom:   q.Get("deadline_from"),
		DeadlineTo:     q.Get("deadline_to"),
		RaisedFrom:     q.Get("raised_from"),
		RaisedTo:       q.Get("raised_to"),
		Sort:           q.Get("sort"),
		Actor:          actor,
	})
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	now := h.service.Now()
	rows := make([]taskPayload, 0, len(page.Rows))
	for _, t := range page.Rows {
		rows = append(rows, toTaskPayload(t, actor, now))
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
		Scopes:      toScopePayloads(scopeKey, page, actor),
		UnseenCount: page.UnseenCount,
		CanRaise:    actor.CanRaise,
		TraceID:     traceID(r),
	})
}

// ListAssignees serves GET /app/leadership-tasks/assignees -- the raise form's picker.
func (h *Handler) ListAssignees(w http.ResponseWriter, r *http.Request) {
	assignees, err := h.service.ListAssignees(r.Context(), tenantID(r))
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	out := make([]assigneePayload, 0, len(assignees))
	for _, a := range assignees {
		out = append(out, assigneePayload{UserID: a.UserID, Name: a.Name, Title: a.Title})
	}
	httpresponse.WriteJSON(w, http.StatusOK, assigneesPayload{Assignees: out, TraceID: traceID(r)})
}

// ListMentionableUsers serves GET /app/leadership-tasks/{task_id}/mentionable-users -- the `@`
// autocomplete for ONE task.
//
// It is deliberately NOT /app/leadership-tasks/assignees: that picker is narrowed to people a
// task may be raised FOR, whereas this list answers who may be NAMED in a note on this task.
// The caller must be able to read the task itself, so the list cannot be used to enumerate the
// leadership roster from a task nobody showed them.
func (h *Handler) ListMentionableUsers(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	users, err := h.service.ListMentionableUsers(r.Context(), tenantID(r), actor, r.PathValue("task_id"))
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	out := make([]mentionableUserPayload, 0, len(users))
	for _, u := range users {
		out = append(out, mentionableUserPayload{UserID: u.UserID, Name: u.Name, Title: u.Title, Relation: u.Relation})
	}
	httpresponse.WriteJSON(w, http.StatusOK, mentionableUsersPayload{Users: out, TraceID: traceID(r)})
}

// GetTask serves GET /app/leadership-tasks/{task_id}.
func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	task, err := h.service.GetTask(r.Context(), tenantID(r), actor, r.PathValue("task_id"))
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor, h.service.Now()), TraceID: traceID(r)})
}

// GetTaskActivity serves GET /app/leadership-tasks/{task_id}/activity?before= -- one older
// window of the feed, for the drawer's "older" control.
func (h *Handler) GetTaskActivity(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	page, err := h.service.ActivityPage(r.Context(), tenantID(r), actor, r.PathValue("task_id"), r.URL.Query().Get("before"))
	if errors.Is(err, ports.ErrInvalidArgument) {
		h.writeErr(w, r, app.BadRequest("invalid_cursor", "That activity cursor is not valid."))
		return
	}
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, activityPagePayload{
		Activity:   activityPayloads(page.Activity),
		Notes:      notePayloads(page.Notes),
		HasMore:    page.HasMore,
		NextBefore: page.NextBefore,
		TraceID:    traceID(r),
	})
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
	deadline, err := parseDeadline(body.DeadlineAt)
	if err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_deadline", "That deadline is not a valid date and time."))
		return
	}
	task, err := h.service.Raise(r.Context(), ports.RaiseParams{
		TenantID:         tenantID(r),
		ActorID:          actor.UserID,
		ActorDesignation: actor.RaiseDesignation,
		AssigneeUserID:   body.AssigneeUserID,
		Title:            body.Title,
		Body:             body.Body,
		Refs:             toRefs(body.Attachments),
		IdempotencyKey:   key,
		DeadlineAt:       deadline,
	})
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, taskDetailPayload{Task: toTaskPayload(task, actor, h.service.Now()), TraceID: traceID(r)})
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
	deadline, err := parseDeadline(body.DeadlineAt)
	if err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_deadline", "That deadline is not a valid date and time."))
		return
	}
	task, err := h.service.Edit(r.Context(), ports.EditParams{
		TenantID:       tenantID(r),
		ActorID:        actor.UserID,
		Actor:          actor,
		TaskID:         r.PathValue("task_id"),
		Title:          body.Title,
		Body:           body.Body,
		Refs:           toRefs(body.Attachments),
		RowVersion:     body.RowVersion,
		IdempotencyKey: key,
		DeadlineAt:     deadline,
	})
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor, h.service.Now()), TraceID: traceID(r)})
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
		h.writeCause(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor, h.service.Now()), TraceID: traceID(r)})
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
		MentionUserIDs: toMentionUserIDs(body.Mentions),
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor, h.service.Now()), TraceID: traceID(r)})
}

// MarkSeen serves POST /app/leadership-tasks/{task_id}/seen. Naturally idempotent (a
// set-if-null), so it carries no key: the second tap returns the same task.
func (h *Handler) MarkSeen(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	task, err := h.service.MarkSeen(r.Context(), tenantID(r), actor, r.PathValue("task_id"))
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDetailPayload{Task: toTaskPayload(task, actor, h.service.Now()), TraceID: traceID(r)})
}

// DownloadAttachment serves GET /app/leadership-tasks/{task_id}/attachments/{proof_id}/download.
func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	download, err := h.service.AttachmentDownloadURL(
		r.Context(),
		tenantID(r),
		actor,
		r.PathValue("task_id"),
		r.PathValue("proof_id"),
	)
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	proof := download.Artifact
	h.log.Info(
		"proof_download_url_issued",
		"event", "proof_download_url_issued",
		"surface", "leadership_task_attachment",
		"route", "/app/leadership-tasks/{task_id}/attachments/{proof_id}/download",
		"result", "url_issued",
		"tenant_id", tenantID(r),
		"actor_id", actor.UserID,
		"task_id", r.PathValue("task_id"),
		"proof_id", r.PathValue("proof_id"),
		"client_app_version", clientHeader(r, "X-GoatOS-App-Version"),
		"client_app_version_code", clientHeader(r, "X-GoatOS-App-Version-Code"),
		"client_platform", clientHeader(r, "X-GoatOS-Platform"),
		"client_os_version", clientHeader(r, "X-GoatOS-OS-Version"),
		"device_id", httpmiddleware.DeviceIDFromContext(r.Context()),
		"client_device_model", clientHeader(r, "X-GoatOS-Device-Model"),
		"remote_ip", clientRemoteIP(r),
		"user_agent", r.UserAgent(),
		"storage_provider", nonEmpty(proof.StorageProvider, "gcs"),
		"object_key", proof.ObjectKey,
		"scope_type", nonEmpty(proof.ScopeType, "leadership_task"),
		"scope_id", nonEmpty(proof.ScopeID, r.PathValue("task_id")),
		"subject_type", nonEmpty(proof.SubjectType, "leadership_task_attachment"),
		"subject_id", proofSubjectID(proof, r.PathValue("proof_id")),
		"proof_type", nonEmpty(proof.ProofType, "leadership_task_attachment"),
		"mime_type", proof.MimeType,
		"size_bytes", proof.SizeBytes,
		"content_hash", proof.ContentHash,
		"uploaded_by", stringPtrValue(proof.UploadedBy),
		"request_id", httpmiddleware.RequestIDFromContext(r.Context()),
		"trace_id", traceID(r),
	)
	httpresponse.WriteJSON(w, http.StatusOK, downloadPayload{DownloadURL: download.URL, TraceID: traceID(r)})
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func proofSubjectID(proof proofdomain.Artifact, fallback string) string {
	if proof.SubjectID != nil && strings.TrimSpace(*proof.SubjectID) != "" {
		return strings.TrimSpace(*proof.SubjectID)
	}
	return fallback
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func clientHeader(r *http.Request, key string) string {
	return strings.TrimSpace(r.Header.Get(key))
}

func clientRemoteIP(r *http.Request) string {
	forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwardedFor == "" {
		return r.RemoteAddr
	}
	if comma := strings.Index(forwardedFor, ","); comma >= 0 {
		return strings.TrimSpace(forwardedFor[:comma])
	}
	return forwardedFor
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
	if len(key) < 8 || len(key) > 200 {
		h.writeErr(w, r, app.BadRequest("invalid_idempotency_key", "This could not be saved safely. Try again."))
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

// writeCause maps a module error onto the transport shape and logs the ORIGINAL error beside
// the code. Logging only the mapped code left a 500 on the phone's task list with no cause in
// the log (2026-09-14); a storage or timeout error must be readable from the request line.
func (h *Handler) writeCause(w http.ResponseWriter, r *http.Request, err error) {
	appErr := toAppError(err)
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, fmt.Errorf("%s: %w", appErr.Code, err))
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
// raise (the "+" and edit/cancel controls), act (the status buttons), and monitor
// (the CEO/COO Team progress scope -- the EXPLICIT leadership_tasks.monitor permission, never
// inferred from the other two). Derived from the SAME grants the route table authorized
// against, never from a role string the client sends.
func actorFrom(r *http.Request) domain.Actor {
	actor := domain.Actor{UserID: strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context()))}
	if held, ok := httpmiddleware.PersonPermissionsFromContext(r.Context()); ok {
		for _, perm := range held {
			switch perm {
			case permissions.LeadershipTasksRaise:
				actor.CanRaise = true
			case permissions.LeadershipTasksAct:
				actor.CanAct = true
			case permissions.LeadershipTasksMonitor:
				actor.CanMonitor = true
			}
		}
		return actor
	}
	for _, grant := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if permissions.RoleHasPermission(grant.Role, permissions.LeadershipTasksRaise) {
			actor.CanRaise = true
			if actor.RaiseDesignation == "" {
				actor.RaiseDesignation = grant.Role
			}
		}
		if permissions.RoleHasPermission(grant.Role, permissions.LeadershipTasksAct) {
			actor.CanAct = true
		}
		if permissions.RoleHasPermission(grant.Role, permissions.LeadershipTasksMonitor) {
			actor.CanMonitor = true
		}
	}
	return actor
}

// Package http serves the toxin module's routes: the tester's task list/detail and
// per-step completion on the phone, and the CEO/CXO-only review list + verdict on
// admin-web.
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

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/toxin/app"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// Service is the behaviour this transport depends on.
type Service interface {
	ListTasks(ctx context.Context, tenantID string, statuses []string, limit int, cursor string) (ports.TaskPage, error)
	GetTask(ctx context.Context, tenantID, taskID string) (ports.TaskRow, error)
	CompleteStep(ctx context.Context, p ports.CompleteStepParams) (ports.TaskRow, error)
	SubmitReading(ctx context.Context, p ports.SubmitParams) (ports.TaskRow, error)
	RecordVerdict(ctx context.Context, p ports.VerdictParams) (ports.TaskRow, error)
	// Now is the service clock the step states were gated against; payload composition
	// uses the same clock so the phone's countdowns agree with the server's refusals.
	Now() time.Time
}

// Handler serves the toxin routes.
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

// Register mounts the toxin routes.
//
// Patterns here must stay byte-identical to the entries in permissions/routes.go — the
// permission table is matched by method + pattern, and a mismatch serves the route
// ungated. The verdict route is CEO/CXO-only (toxin.verdict); see the permission's doc
// comment for the maintainer decision.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /app/toxin/tasks", h.ListTasks)
	mux.HandleFunc("GET /app/toxin/tasks/{task_id}", h.GetTask)
	mux.HandleFunc("POST /app/toxin/tasks/{task_id}/steps/{step_no}/complete", h.CompleteStep)
	mux.HandleFunc("POST /app/toxin/tasks/{task_id}/submit", h.SubmitReading)
	mux.HandleFunc("GET /toxin/review", h.ListReview)
	mux.HandleFunc("POST /toxin/tasks/{task_id}/verdict", h.RecordVerdict)
}

// maxToxinRequestBytes caps a write body; the largest legitimate payload is well under a
// kilobyte.
const maxToxinRequestBytes = 64 * 1024

// ListTasks serves GET /app/toxin/tasks — the tester's list.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, nil)
}

// ListReview serves GET /toxin/review — the CEO/CXO review tab. Same page shape as the
// tester's list; the route (and its toxin.verdict permission) is what narrows it to
// submitted work by default.
func (h *Handler) ListReview(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, []string{domain.StatusPendingReview})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, defaultStatuses []string) {
	q := r.URL.Query()
	statuses := defaultStatuses
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		statuses = strings.Split(raw, ",")
		for i := range statuses {
			statuses[i] = strings.TrimSpace(statuses[i])
		}
	}
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_limit", "That page size is not valid."))
			return
		}
		limit = parsed
	}
	page, err := h.service.ListTasks(r.Context(), tenantID(r), statuses, limit, q.Get("cursor"))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	now := h.service.Now()
	canExecute := callerCanExecute(r)
	items := make([]taskPayload, 0, len(page.Rows))
	for _, row := range page.Rows {
		items = append(items, toTaskPayload(row, now, canExecute))
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskPagePayload{
		Tasks:      items,
		NextCursor: page.NextCursor,
		// StatusCounts are whole-tenant aggregates, never page-local sums.
		StatusCounts: page.StatusCounts,
	})
}

// GetTask serves GET /app/toxin/tasks/{task_id} — the guided step flow with live states.
func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	row, err := h.service.GetTask(r.Context(), tenantID(r), r.PathValue("task_id"))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toTaskDetailPayload(row, h.service.Now(), callerCanExecute(r)))
}

// CompleteStep serves POST /app/toxin/tasks/{task_id}/steps/{step_no}/complete.
func (h *Handler) CompleteStep(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This step could not be recorded safely. Try again."))
		return
	}
	stepNo, err := strconv.Atoi(r.PathValue("step_no"))
	if err != nil {
		h.writeErr(w, r, app.BadRequest("unknown_step", "That step is not part of this test."))
		return
	}
	var body completeStepPayload
	if !h.decode(w, r, &body) {
		return
	}
	row, err := h.service.CompleteStep(r.Context(), ports.CompleteStepParams{
		TenantID:       tenantID(r),
		TaskID:         r.PathValue("task_id"),
		StepNo:         stepNo,
		ProofRef:       body.ProofRef,
		ActorID:        httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toTaskDetailPayload(row, h.service.Now(), callerCanExecute(r)))
}

// SubmitReading serves POST /app/toxin/tasks/{task_id}/submit — step 7.
func (h *Handler) SubmitReading(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This test could not be recorded safely. Try again."))
		return
	}
	var body submitPayload
	if !h.decode(w, r, &body) {
		return
	}
	row, err := h.service.SubmitReading(r.Context(), ports.SubmitParams{
		TenantID:       tenantID(r),
		TaskID:         r.PathValue("task_id"),
		Outcome:        body.Outcome,
		StripPhotoRef:  body.StripPhotoRef,
		ActorID:        httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toTaskDetailPayload(row, h.service.Now(), callerCanExecute(r)))
}

// RecordVerdict serves POST /toxin/tasks/{task_id}/verdict — CEO/CXO only.
func (h *Handler) RecordVerdict(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This review could not be recorded safely. Try again."))
		return
	}
	var body verdictPayload
	if !h.decode(w, r, &body) {
		return
	}
	row, err := h.service.RecordVerdict(r.Context(), ports.VerdictParams{
		TenantID:       tenantID(r),
		TaskID:         r.PathValue("task_id"),
		Decision:       body.Decision,
		Reason:         body.Reason,
		RowVersion:     body.RowVersion,
		ActorID:        httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toTaskDetailPayload(row, h.service.Now(), callerCanExecute(r)))
}

// decode reads a JSON write body, failing loud on unknown fields.
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxToxinRequestBytes))
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

// toAppError keeps already-shaped app errors and maps domain/port errors.
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

// callerCanExecute reports whether the principal on this request holds toxin.execute.
//
// The read routes (list, detail) are gated on toxin.read, which CEO/CXO holds and which
// says nothing about running a test — so the payload must answer the second question
// separately or the phone would offer a step the write route then refuses. It is derived
// from the SAME grants the route table authorizes against, never from a role string the
// client sends.
func callerCanExecute(r *http.Request) bool {
	for _, grant := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if permissions.RoleHasPermission(grant.Role, permissions.ToxinExecute) {
			return true
		}
	}
	return false
}

package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/sop/app"
	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

type Handler struct {
	service *app.Service
	log     *slog.Logger
}

func NewHandler(service *app.Service, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

func Register(mux *nethttp.ServeMux, h *Handler) {
	mux.HandleFunc("GET /admin/sops", h.ListSOPs)
	mux.HandleFunc("POST /admin/sops", h.CreateSOP)
	mux.HandleFunc("GET /admin/sops/{sop_id}", h.GetSOP)
	mux.HandleFunc("POST /admin/sops/{sop_id}/versions", h.CreateVersion)
	mux.HandleFunc("GET /admin/sops/{sop_id}/versions/{sop_version_id}", h.GetVersion)
	mux.HandleFunc("POST /admin/sops/{sop_id}/versions/{sop_version_id}/dry-run", h.DryRun)
	mux.HandleFunc("POST /admin/sops/{sop_id}/versions/{sop_version_id}/publish", h.PublishVersion)
	mux.HandleFunc("POST /admin/sops/{sop_id}/versions/{sop_version_id}/retire", h.RetireVersion)
	mux.HandleFunc("GET /admin/tasks", h.ListAdminTasks)
	mux.HandleFunc("POST /admin/tasks", h.CreateTask)
	mux.HandleFunc("GET /admin/tasks/{task_id}", h.GetTask)
	mux.HandleFunc("POST /admin/tasks/{task_id}/assign", h.AssignTask)
	mux.HandleFunc("POST /admin/tasks/{task_id}/verify", h.VerifyTask)
	mux.HandleFunc("POST /admin/tasks/{task_id}/rework", h.ReworkTask)

	mux.HandleFunc("GET /app/tasks", h.ListAppTasks)
	mux.HandleFunc("GET /app/tasks/{task_id}", h.GetTask)
	mux.HandleFunc("GET /app/sop-versions/{sop_version_id}", h.GetAppVersion)
	mux.HandleFunc("POST /app/tasks/{task_id}/submissions", h.SubmitTask)
}

func (h *Handler) ListSOPs(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	result, err := h.service.ListSOPs(r.Context(), ports.ListSOPsParams{
		TenantID: tenantID(r),
		Status:   q.Get("status"),
		Limit:    parseLimit(q.Get("limit")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateSOP(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body domain.CreateSOPRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.CreateSOP(r.Context(), ports.CreateSOPCommand{TenantID: tenantID(r), ActorID: actorID(r), Body: body}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) GetSOP(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := h.service.GetSOP(r.Context(), tenantID(r), r.PathValue("sop_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateVersion(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body domain.CreateSOPVersionRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.CreateVersion(r.Context(), ports.CreateVersionCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		SOPID:    r.PathValue("sop_id"),
		Body:     body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) GetVersion(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := h.service.GetVersion(r.Context(), tenantID(r), r.PathValue("sop_id"), r.PathValue("sop_version_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) GetAppVersion(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := h.service.GetVersionByID(r.Context(), tenantID(r), r.PathValue("sop_version_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) DryRun(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body domain.DryRunRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.DryRun(r.Context(), tenantID(r), r.PathValue("sop_id"), r.PathValue("sop_version_id"), body, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) PublishVersion(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.versionStatus(w, r, "publish")
}

func (h *Handler) RetireVersion(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.versionStatus(w, r, "retire")
}

func (h *Handler) versionStatus(w nethttp.ResponseWriter, r *nethttp.Request, action string) {
	var body struct {
		RowVersion int `json:"row_version"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	cmd := ports.VersionCommand{
		TenantID:     tenantID(r),
		ActorID:      actorID(r),
		SOPID:        r.PathValue("sop_id"),
		SOPVersionID: r.PathValue("sop_version_id"),
		RowVersion:   body.RowVersion,
	}
	var result *domain.SOPVersionResponse
	var err error
	if action == "publish" {
		result, err = h.service.PublishVersion(r.Context(), cmd, traceID(r))
	} else {
		result, err = h.service.RetireVersion(r.Context(), cmd, traceID(r))
	}
	h.respond(w, r, result, err)
}

func (h *Handler) ListAdminTasks(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	result, err := h.service.ListTasks(r.Context(), ports.ListTasksParams{
		TenantID:   tenantID(r),
		State:      q.Get("state"),
		AssignedTo: q.Get("assigned_to"),
		ScopeType:  q.Get("scope_type"),
		ScopeID:    q.Get("scope_id"),
		Limit:      parseLimit(q.Get("limit")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateTask(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body domain.CreateTaskRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.CreateTask(r.Context(), ports.CreateTaskCommand{TenantID: tenantID(r), ActorID: actorID(r), Body: body}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) GetTask(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := h.service.GetTask(r.Context(), tenantID(r), r.PathValue("task_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) AssignTask(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body domain.AssignTaskRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.AssignTask(r.Context(), ports.AssignTaskCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		TaskID:   r.PathValue("task_id"),
		Body:     body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) VerifyTask(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.reviewTask(w, r, "verify")
}

func (h *Handler) ReworkTask(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.reviewTask(w, r, "rework")
}

func (h *Handler) reviewTask(w nethttp.ResponseWriter, r *nethttp.Request, action string) {
	var body domain.ReviewTaskRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	cmd := ports.ReviewTaskCommand{TenantID: tenantID(r), ActorID: actorID(r), TaskID: r.PathValue("task_id"), Body: body}
	var result *domain.TaskResponse
	var err error
	if action == "verify" {
		result, err = h.service.VerifyTask(r.Context(), cmd, traceID(r))
	} else {
		result, err = h.service.ReworkTask(r.Context(), cmd, traceID(r))
	}
	h.respond(w, r, result, err)
}

func (h *Handler) ListAppTasks(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	result, err := h.service.ListTasks(r.Context(), ports.ListTasksParams{
		TenantID:   tenantID(r),
		ActorID:    actorID(r),
		State:      q.Get("state"),
		AssignedTo: actorID(r),
		Limit:      parseLimit(q.Get("limit")),
		AppView:    true,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) SubmitTask(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body domain.SubmitTaskRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.SubmitTask(r.Context(), ports.SubmitTaskCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		TaskID:   r.PathValue("task_id"),
		Body:     body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) respond(w nethttp.ResponseWriter, r *nethttp.Request, payload any, err error) {
	if err != nil {
		status := nethttp.StatusInternalServerError
		envelope := domain.ErrorEnvelope{
			Code:        "internal_error",
			Message:     "internal server error",
			FieldErrors: []domain.FieldError{},
			TraceID:     traceID(r),
			Retryable:   true,
		}
		var appErr *app.Error
		if errors.As(err, &appErr) {
			status = appErr.HTTPStatus
			envelope.Code = appErr.Code
			envelope.Message = appErr.Message
			envelope.Retryable = appErr.Retryable
		}
		httpresponse.WriteError(w, r, h.log, status, envelope, err)
		return
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, payload)
}

func decodeJSON(w nethttp.ResponseWriter, r *nethttp.Request, dst any) bool {
	body, err := io.ReadAll(nethttp.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeBadJSON(w, r, "request body is too large or unreadable")
		return false
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeBadJSON(w, r, "request body must be valid JSON")
		return false
	}
	return true
}

func writeBadJSON(w nethttp.ResponseWriter, r *nethttp.Request, message string) {
	httpresponse.WriteJSON(w, nethttp.StatusBadRequest, domain.ErrorEnvelope{
		Code:        "invalid_json",
		Message:     message,
		FieldErrors: []domain.FieldError{},
		TraceID:     traceID(r),
	})
}

func parseLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return limit
}

func tenantID(r *nethttp.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *nethttp.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

func traceID(r *nethttp.Request) string {
	traceID := httpmiddleware.TraceIDFromContext(r.Context())
	if traceID == "" {
		return "missing-trace"
	}
	return traceID
}

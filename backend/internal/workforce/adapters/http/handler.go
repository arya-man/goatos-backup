package workforcehttp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

type Handler struct {
	service *app.Service
	log     *slog.Logger
}

func NewHandler(service *app.Service, log ...*slog.Logger) *Handler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Handler{service: service, log: l}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /admin/operators", h.ListOperators)
	mux.HandleFunc("POST /admin/operators", h.CreateOperator)
	mux.HandleFunc("GET /admin/operators/{operator_id}", h.GetOperator)
	mux.HandleFunc("PATCH /admin/operators/{operator_id}", h.UpdateOperator)
	mux.HandleFunc("POST /admin/operators/{operator_id}/activate", h.ActivateOperator)
	mux.HandleFunc("POST /admin/operators/{operator_id}/deactivate", h.DeactivateOperator)
	mux.HandleFunc("GET /admin/operators/{operator_id}/grants", h.ListGrants)
	mux.HandleFunc("POST /admin/operators/{operator_id}/grants", h.CreateGrant)
	mux.HandleFunc("POST /admin/operators/{operator_id}/capabilities", h.AssignCapability)
	mux.HandleFunc("DELETE /admin/operators/{operator_id}/capabilities/{capability_id}", h.RemoveCapability)
	mux.HandleFunc("GET /admin/operators/{operator_id}/devices", h.ListDevices)
	mux.HandleFunc("POST /admin/operators/{operator_id}/devices/{device_id}/revoke", h.RevokeDevice)

	mux.HandleFunc("GET /app/me", h.AppMe)
	mux.HandleFunc("GET /app/bootstrap", h.Bootstrap)
	mux.HandleFunc("POST /app/devices/register", h.RegisterDevice)
	mux.HandleFunc("POST /app/devices/{device_id}/heartbeat", h.HeartbeatDevice)
	mux.HandleFunc("POST /app/devices/{device_id}/deregister", h.DeregisterDevice)
}

func (h *Handler) ListOperators(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.ListOperators(r.Context(), ports.ListOperatorsParams{
		TenantID:   tenantID(r),
		Status:     q.Get("status"),
		RoleHint:   q.Get("role_hint"),
		LocationID: q.Get("location_id"),
		Search:     q.Get("search"),
		Limit:      parseLimit(q.Get("limit")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateOperator(w http.ResponseWriter, r *http.Request) {
	var body domain.CreateOperatorRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.CreateOperator(r.Context(), ports.CreateOperatorCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		Body:     body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) GetOperator(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetOperator(r.Context(), tenantID(r), r.PathValue("operator_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) UpdateOperator(w http.ResponseWriter, r *http.Request) {
	var body domain.UpdateOperatorRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.UpdateOperator(r.Context(), ports.UpdateOperatorCommand{
		TenantID:   tenantID(r),
		ActorID:    actorID(r),
		OperatorID: r.PathValue("operator_id"),
		Body:       body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ActivateOperator(w http.ResponseWriter, r *http.Request) {
	h.statusChange(w, r, "active")
}

func (h *Handler) DeactivateOperator(w http.ResponseWriter, r *http.Request) {
	h.statusChange(w, r, "inactive")
}

func (h *Handler) statusChange(w http.ResponseWriter, r *http.Request, status string) {
	var body domain.StatusChangeRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.SetOperatorStatus(r.Context(), ports.StatusCommand{
		TenantID:   tenantID(r),
		ActorID:    actorID(r),
		OperatorID: r.PathValue("operator_id"),
		Reason:     body.Reason,
		RowVersion: body.RowVersion,
		Status:     status,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ListGrants(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListGrants(r.Context(), tenantID(r), r.PathValue("operator_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) CreateGrant(w http.ResponseWriter, r *http.Request) {
	var body domain.CreateGrantRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.CreateGrant(r.Context(), ports.CreateGrantCommand{
		TenantID:   tenantID(r),
		ActorID:    actorID(r),
		OperatorID: r.PathValue("operator_id"),
		Body:       body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) AssignCapability(w http.ResponseWriter, r *http.Request) {
	var body domain.CreateCapabilityRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.AssignCapability(r.Context(), ports.CapabilityCommand{
		TenantID:   tenantID(r),
		ActorID:    actorID(r),
		OperatorID: r.PathValue("operator_id"),
		Body:       body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) RemoveCapability(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.RemoveCapability(r.Context(), ports.RemoveCapabilityCommand{
		TenantID:     tenantID(r),
		ActorID:      actorID(r),
		OperatorID:   r.PathValue("operator_id"),
		CapabilityID: r.PathValue("capability_id"),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListDevices(r.Context(), tenantID(r), r.PathValue("operator_id"), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) RevokeDevice(w http.ResponseWriter, r *http.Request) {
	var body domain.RevokeDeviceRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.RevokeDevice(r.Context(), ports.RevokeDeviceCommand{
		TenantID:   tenantID(r),
		ActorID:    actorID(r),
		OperatorID: r.PathValue("operator_id"),
		DeviceID:   r.PathValue("device_id"),
		Reason:     body.Reason,
		RowVersion: body.RowVersion,
	}, traceID(r))
	h.respond(w, r, result, err)
}

// DeregisterDevice — app-facing logout decouple: the caller drops its OWN device's FCM push
// binding (actor-scoped, device_id from path, no body). Idempotent from the client's view.
func (h *Handler) DeregisterDevice(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.DeregisterDevice(r.Context(), ports.DeregisterDeviceCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		DeviceID: r.PathValue("device_id"),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) ListSourceCandidates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.ListSourceCandidates(r.Context(), ports.ListSourceCandidatesParams{
		TenantID:     tenantID(r),
		Status:       q.Get("status"),
		SourceSystem: q.Get("source_system"),
		Limit:        parseLimit(q.Get("limit")),
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) MapSourceCandidate(w http.ResponseWriter, r *http.Request) {
	var body domain.MapSourceCandidateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.MapSourceCandidate(r.Context(), ports.MapSourceCandidateCommand{
		TenantID:    tenantID(r),
		ActorID:     actorID(r),
		CandidateID: r.PathValue("candidate_id"),
		Body:        body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) RejectSourceCandidate(w http.ResponseWriter, r *http.Request) {
	var body domain.RejectSourceCandidateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.RejectSourceCandidate(r.Context(), ports.RejectSourceCandidateCommand{
		TenantID:    tenantID(r),
		ActorID:     actorID(r),
		CandidateID: r.PathValue("candidate_id"),
		Body:        body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) AppMe(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.AppMe(r.Context(), tenantID(r), actorID(r), traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	result, err := h.service.Bootstrap(
		r.Context(),
		tenantID(r),
		actorID(r),
		deviceID,
		httpmiddleware.LocaleTagFromRequest(r),
		traceID(r),
	)
	h.logBootstrapOutcome(r, result, err, deviceID)
	h.respond(w, r, result, err)
}

func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	var body domain.RegisterDeviceRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.RegisterDevice(r.Context(), ports.RegisterDeviceCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		Body:     body,
	}, traceID(r))
	if err != nil {
		h.log.WarnContext(r.Context(), "app_device_register_failed",
			slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
			slog.String("trace_id", traceID(r)),
			slog.String("actor_id", actorID(r)),
			slog.String("tenant_id", tenantID(r)),
			slog.String("app_install_id", body.AppInstallID),
			slog.String("app_version", body.AppVersion),
			slog.String("error", err.Error()),
		)
	} else if result != nil {
		h.log.InfoContext(r.Context(), "app_device_register_succeeded",
			slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
			slog.String("trace_id", traceID(r)),
			slog.String("actor_id", actorID(r)),
			slog.String("tenant_id", tenantID(r)),
			slog.String("operator_id", result.Device.OperatorID),
			slog.String("device_id", result.Device.DeviceID),
			slog.String("app_install_id", result.Device.AppInstallID),
			slog.String("device_status", result.Device.Status),
			slog.String("app_version", result.Device.AppVersion),
		)
	}
	h.respond(w, r, result, err)
}

func (h *Handler) logBootstrapOutcome(r *http.Request, result *domain.BootstrapResponse, err error, deviceID string) {
	args := []any{
		slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
		slog.String("trace_id", traceID(r)),
		slog.String("actor_id", actorID(r)),
		slog.String("tenant_id", tenantID(r)),
		slog.String("device_id", strings.TrimSpace(deviceID)),
	}
	if err != nil {
		code := "internal_error"
		status := http.StatusInternalServerError
		var appErr *app.Error
		if errors.As(err, &appErr) {
			code = appErr.Code
			status = appErr.HTTPStatus
		}
		args = append(args,
			slog.Int("status", status),
			slog.String("code", code),
			slog.String("error", err.Error()),
		)
		h.log.WarnContext(r.Context(), "app_bootstrap_failed", args...)
		return
	}
	if result != nil {
		args = append(args,
			slog.Int("status", http.StatusOK),
			slog.String("operator_id", result.OperatorProfile.OperatorID),
			slog.String("profile_status", result.OperatorProfile.Status),
			slog.String("primary_role_hint", result.OperatorProfile.PrimaryRoleHint),
			slog.Int("grant_count", len(result.RolesAndScopes)),
			slog.String("roles", strings.Join(bootstrapRoles(result.RolesAndScopes), ",")),
			slog.String("device_status", result.DeviceState.Status),
			slog.Int("nav_count", len(result.VisibleNavigation)),
			slog.Int("module_count", len(result.Modules)),
			slog.String("nav_chrome", result.NavChrome),
		)
	}
	h.log.InfoContext(r.Context(), "app_bootstrap_succeeded", args...)
}

func bootstrapRoles(grants []domain.GrantSummary) []string {
	roles := make([]string, 0, len(grants))
	seen := map[string]struct{}{}
	for _, grant := range grants {
		if _, ok := seen[grant.Role]; ok {
			continue
		}
		seen[grant.Role] = struct{}{}
		roles = append(roles, grant.Role)
	}
	sort.Strings(roles)
	return roles
}

func (h *Handler) HeartbeatDevice(w http.ResponseWriter, r *http.Request) {
	var body domain.HeartbeatDeviceRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.service.HeartbeatDevice(r.Context(), ports.HeartbeatDeviceCommand{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		DeviceID: r.PathValue("device_id"),
		Body:     body,
	}, traceID(r))
	h.respond(w, r, result, err)
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, payload any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
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
	httpresponse.WriteJSON(w, http.StatusOK, payload)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
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

func writeBadJSON(w http.ResponseWriter, r *http.Request, message string) {
	httpresponse.WriteJSON(w, http.StatusBadRequest, domain.ErrorEnvelope{
		Code:        "invalid_json",
		Message:     message,
		FieldErrors: []domain.FieldError{},
		TraceID:     traceID(r),
		Retryable:   false,
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

// idempotencyKeyHeader reads the client's request-level replay key from the
// standard Idempotency-Key header, or "" when absent. The header is the
// authoritative source (matching the procurement/obligation write paths); a
// same-name body field is only a fallback carrier.
func idempotencyKeyHeader(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("Idempotency-Key"))
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *http.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	traceID := httpmiddleware.TraceIDFromContext(r.Context())
	if traceID == "" {
		return "missing-trace"
	}
	return traceID
}

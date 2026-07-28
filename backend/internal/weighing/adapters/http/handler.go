package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

type Service interface {
	CreateCampaign(ctx context.Context, actor domain.Actor, cmd domain.CreateCampaign) (domain.Campaign, error)
	UpdateCampaign(ctx context.Context, actor domain.Actor, campaignID string, cmd domain.UpdateCampaign) (domain.Campaign, error)
	PublishCampaign(ctx context.Context, actor domain.Actor, campaignID, idempotencyKey string) (domain.Campaign, error)
	ListCampaigns(ctx context.Context, actor domain.Actor, cursor string, limit int) (domain.CampaignPage, error)
	PlannerCatalog(ctx context.Context, actor domain.Actor, periodStartDate string) (domain.PlannerCatalog, error)
	ListScopeRoster(ctx context.Context, actor domain.Actor, campaignID, campaignShedID string, cursor string, limit int) (domain.RosterPage, error)
	RecordAnimalObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordAnimalObservation) (domain.Observation, error)
	RecordShedObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordShedObservation) (domain.Observation, error)
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

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /weighing/campaigns", h.ListCampaigns)
	mux.HandleFunc("POST /weighing/campaigns", h.CreateCampaign)
	mux.HandleFunc("PUT /weighing/campaigns/{campaign_id}", h.UpdateCampaign)
	mux.HandleFunc("POST /weighing/campaigns/{campaign_id}/publish", h.PublishCampaign)
	mux.HandleFunc("GET /app/weighing/planner/catalog", h.PlannerCatalog)
	mux.HandleFunc("GET /app/weighing/campaigns", h.ListCampaigns)
	mux.HandleFunc("GET /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/roster", h.ListScopeRoster)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/animal-observations", h.RecordAnimalObservation)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/shed-observations", h.RecordShedObservation)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

type createCampaignRequest struct {
	ParkID            string                      `json:"park_id"`
	PeriodStartDate   string                      `json:"period_start_date"`
	PeriodEndDate     string                      `json:"period_end_date"`
	StartBusinessDate string                      `json:"start_business_date"`
	PlannedCapPerDay  int                         `json:"planned_cap_per_day"`
	OperatorUserID    string                      `json:"operator_user_id"`
	Sheds             []domain.CreateCampaignShed `json:"sheds"`
}

type animalObservationRequest struct {
	AnimalID         string  `json:"animal_id"`
	WeightKg         float64 `json:"weight_kg"`
	ProofArtifactID  string  `json:"proof_artifact_id"`
	ActualLocationID string  `json:"actual_location_id"`
}

type shedObservationRequest struct {
	CampaignShedID  string  `json:"campaign_shed_id"`
	WeightKg        float64 `json:"weight_kg"`
	ProofArtifactID string  `json:"proof_artifact_id"`
}

func (h *Handler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.queryLimit(w, r, 20)
	if !ok {
		return
	}
	page, err := h.service.ListCampaigns(r.Context(), actor(r), r.URL.Query().Get("cursor"), limit)
	h.respond(w, r, map[string]any{"items": page.Items, "next_cursor": page.NextCursor, "trace_id": traceID(r)}, err)
}

func (h *Handler) CreateCampaign(w http.ResponseWriter, r *http.Request) {
	var req createCampaignRequest
	if !h.decode(w, r, &req) {
		return
	}
	c, err := h.service.CreateCampaign(r.Context(), actor(r), domain.CreateCampaign{
		ParkID: req.ParkID, PeriodStartDate: req.PeriodStartDate, PeriodEndDate: req.PeriodEndDate, StartBusinessDate: req.StartBusinessDate,
		PlannedCapPerDay: req.PlannedCapPerDay, OperatorUserID: req.OperatorUserID, IdempotencyKey: r.Header.Get("Idempotency-Key"), Sheds: req.Sheds,
	})
	h.respond(w, r, map[string]any{"campaign": c, "trace_id": traceID(r)}, err)
}

func (h *Handler) UpdateCampaign(w http.ResponseWriter, r *http.Request) {
	var req createCampaignRequest
	if !h.decode(w, r, &req) {
		return
	}
	c, err := h.service.UpdateCampaign(r.Context(), actor(r), r.PathValue("campaign_id"), domain.UpdateCampaign{
		ParkID: req.ParkID, PeriodStartDate: req.PeriodStartDate, PeriodEndDate: req.PeriodEndDate, StartBusinessDate: req.StartBusinessDate,
		PlannedCapPerDay: req.PlannedCapPerDay, OperatorUserID: req.OperatorUserID, IdempotencyKey: r.Header.Get("Idempotency-Key"), Sheds: req.Sheds,
	})
	h.respond(w, r, map[string]any{"campaign": c, "trace_id": traceID(r)}, err)
}

func (h *Handler) PlannerCatalog(w http.ResponseWriter, r *http.Request) {
	catalog, err := h.service.PlannerCatalog(r.Context(), actor(r), r.URL.Query().Get("period_start_date"))
	h.respond(w, r, map[string]any{"parks": catalog.Parks, "operators": catalog.Operators, "trace_id": traceID(r)}, err)
}

func (h *Handler) PublishCampaign(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.PublishCampaign(r.Context(), actor(r), r.PathValue("campaign_id"), r.Header.Get("Idempotency-Key"))
	h.respond(w, r, map[string]any{"campaign": c, "trace_id": traceID(r)}, err)
}

func (h *Handler) ListScopeRoster(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.queryLimit(w, r, 50)
	if !ok {
		return
	}
	page, err := h.service.ListScopeRoster(r.Context(), actor(r), r.PathValue("campaign_id"), r.PathValue("campaign_shed_id"), r.URL.Query().Get("cursor"), limit)
	h.respond(w, r, map[string]any{"items": page.Items, "next_cursor": page.NextCursor, "trace_id": traceID(r)}, err)
}

func (h *Handler) queryLimit(w http.ResponseWriter, r *http.Request, fallback int) (int, bool) {
	limit := fallback
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil || parsed <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return 0, false
		}
		limit = parsed
	}
	return limit, true
}

func (h *Handler) RecordAnimalObservation(w http.ResponseWriter, r *http.Request) {
	var req animalObservationRequest
	if !h.decode(w, r, &req) {
		return
	}
	obs, err := h.service.RecordAnimalObservation(r.Context(), actor(r), domain.RecordAnimalObservation{
		CampaignID: r.PathValue("campaign_id"), AnimalID: req.AnimalID, WeightKg: req.WeightKg, ProofArtifactID: req.ProofArtifactID, ActualLocationID: req.ActualLocationID, IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	h.respond(w, r, map[string]any{"observation": obs, "trace_id": traceID(r)}, err)
}

func (h *Handler) RecordShedObservation(w http.ResponseWriter, r *http.Request) {
	var req shedObservationRequest
	if !h.decode(w, r, &req) {
		return
	}
	obs, err := h.service.RecordShedObservation(r.Context(), actor(r), domain.RecordShedObservation{
		CampaignID: r.PathValue("campaign_id"), CampaignShedID: req.CampaignShedID, WeightKg: req.WeightKg, ProofArtifactID: req.ProofArtifactID, IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	h.respond(w, r, map[string]any{"observation": obs, "trace_id": traceID(r)}, err)
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(body) == 0 {
		h.badRequest(w, r, "invalid_body", "request body is required")
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		h.badRequest(w, r, "invalid_json", "request body is not valid JSON")
		return false
	}
	return true
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, body any, err error) {
	if err == nil {
		httpresponse.WriteJSON(w, http.StatusOK, body)
		return
	}
	switch {
	case errors.Is(err, ports.ErrForbidden):
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden, errorEnvelope{Code: "permission_denied", Message: "permission denied", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrInvalidArgument):
		h.badRequest(w, r, "invalid_request", "request is invalid")
	case errors.Is(err, ports.ErrNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound, errorEnvelope{Code: "not_found", Message: "weighing resource was not found", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrImmutable):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "invalid_state", Message: "weighing resource is not editable in its current state", TraceID: traceID(r)}, nil)
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
	}
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, msg string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, errorEnvelope{Code: code, Message: msg, TraceID: traceID(r)}, nil)
}

func actor(r *http.Request) domain.Actor {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	roles := make([]string, 0, len(grants))
	for _, grant := range grants {
		roles = append(roles, grant.Role)
	}
	return domain.Actor{TenantID: tenantID(r), UserID: httpmiddleware.ActorIDFromContext(r.Context()), Roles: roles}
}

func tenantID(r *http.Request) string { return httpmiddleware.TenantIDFromContext(r.Context()) }
func traceID(r *http.Request) string  { return httpmiddleware.TraceIDFromContext(r.Context()) }

var _ = permissions.WeighingMonitor

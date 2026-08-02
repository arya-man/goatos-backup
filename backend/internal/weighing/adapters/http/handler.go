package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

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
	ListCampaigns(ctx context.Context, actor domain.Actor, scope domain.CampaignListScope, parkID, cursor string, limit int) (domain.CampaignPage, error)
	PlannerCatalog(ctx context.Context, actor domain.Actor, periodStartDate string) (domain.PlannerCatalog, error)
	PlannerParkBuckets(ctx context.Context, actor domain.Actor, parkID, periodStartDate, excludeCampaignID, cursor string, limit int) (domain.PlannerParkBuckets, error)
	ListScopeRoster(ctx context.Context, actor domain.Actor, campaignID, campaignShedID string, cursor string, observationsCursor string, limit int, includeRoster bool) (domain.RosterPage, error)
	ListCampaignSheds(ctx context.Context, actor domain.Actor, campaignID, cursor string, limit int) (domain.CampaignShedPage, error)
	GetLeadershipShedVideos(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, cursor string, limit int) (domain.LeadershipShedVideos, error)
	ListLeadershipSheds(ctx context.Context, actor domain.Actor, cursor string, limit int) (domain.LeadershipShedPage, error)
	RecordAnimalObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordAnimalObservation) (domain.Observation, error)
	RecordShedObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordShedObservation) (domain.Observation, error)
	SubmitIndividualScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey string, scannedIdentifiers []string) error
	ReopenScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey, reason string) error
	CloseScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey, reason string) (domain.CloseResult, error)
	AbandonScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey, reason string) (domain.CloseResult, error)
	CloseCampaign(ctx context.Context, actor domain.Actor, campaignID, idempotencyKey, reason string) (domain.CloseResult, error)
	WeighingProcessState(ctx context.Context, actor domain.Actor, campaignID, fromBusinessDate, toBusinessDate string) (domain.ProcessState, error)
}

type Handler struct {
	service Service
	log     *slog.Logger
	media   interface {
		DownloadURL(context.Context, string, string) (string, error)
	}
}

func NewHandler(service Service, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

func (h *Handler) WithMediaResolver(media interface {
	DownloadURL(context.Context, string, string) (string, error)
}) *Handler {
	h.media = media
	return h
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /weighing/campaigns", h.ListCampaigns)
	mux.HandleFunc("POST /weighing/campaigns", h.CreateCampaign)
	mux.HandleFunc("PUT /weighing/campaigns/{campaign_id}", h.UpdateCampaign)
	mux.HandleFunc("POST /weighing/campaigns/{campaign_id}/publish", h.PublishCampaign)
	mux.HandleFunc("GET /app/weighing/planner/catalog", h.PlannerCatalog)
	mux.HandleFunc("GET /app/weighing/planner/parks/{park_id}/buckets", h.PlannerParkBuckets)
	mux.HandleFunc("GET /app/weighing/campaigns", h.AppListCampaigns)
	mux.HandleFunc("GET /app/weighing/campaigns/{campaign_id}/sheds", h.ListCampaignSheds)
	mux.HandleFunc("GET /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/roster", h.ListScopeRoster)
	mux.HandleFunc("GET /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/videos", h.GetLeadershipShedVideos)
	mux.HandleFunc("GET /app/weighing/leadership/sheds", h.ListLeadershipSheds)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/animal-observations", h.RecordAnimalObservation)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/shed-observations", h.RecordShedObservation)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/submit", h.SubmitIndividualScope)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/reopen", h.ReopenScope)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/close", h.CloseScope)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/abandon", h.AbandonScope)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/close", h.CloseCampaign)
	// PHASE 2 Calendar / Control Tower binding. Backend-owned grain + disjoint
	// buckets + whole-filter summary; renderers never recompute totals.
	mux.HandleFunc("GET /weighing/process-state", h.WeighingProcessState)
}

// WeighingProcessState serves Calendar day markers and the Control Tower gap
// summary. `from`/`to` are INCLUSIVE Asia/Kolkata business dates (YYYY-MM-DD);
// anything finer than a business day is rejected.
func (h *Handler) WeighingProcessState(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.WeighingProcessState(
		r.Context(),
		actor(r),
		r.URL.Query().Get("campaign_id"),
		r.URL.Query().Get("from"),
		r.URL.Query().Get("to"),
	)
	h.respond(w, r, result, err)
}

// conflictFieldError names ONE blocked bucket inside the standard error envelope's
// field_errors array, so the conflicting shed names travel with the 409 without
// inventing envelope keys the contract does not declare.
type conflictFieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// shedScheduleConflictEnvelope is the standard envelope carrying one field error
// per already-scheduled bucket, so a blocked publish can be explained inline
// instead of costing the planner a second request.
type shedScheduleConflictEnvelope struct {
	Code        string               `json:"code"`
	Message     string               `json:"message"`
	FieldErrors []conflictFieldError `json:"field_errors"`
	TraceID     string               `json:"trace_id"`
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

// animalObservationRequest is the free-flow scan write request. It carries the
// raw scanned identifier only -- no animal_id field exists here, and none may
// be added: weighing never resolves a scan to herd identity (see
// repository_free_flow_no_herd_crosscheck_integration_test.go and the
// weighing-free-flow-guard machine gate).
type animalObservationRequest struct {
	CampaignShedID    string  `json:"campaign_shed_id"`
	ScannedIdentifier string  `json:"scanned_identifier"`
	WeightKg          float64 `json:"weight_kg"`
	ProofArtifactID   string  `json:"proof_artifact_id"`
	ActualLocationID  string  `json:"actual_location_id"`
}

type shedObservationRequest struct {
	CampaignShedID   string   `json:"campaign_shed_id"`
	WeightKg         float64  `json:"weight_kg"`
	AverageWeightKg  float64  `json:"average_weight_kg"`
	AnimalCount      int      `json:"animal_count"`
	ProofArtifactID  string   `json:"proof_artifact_id"`
	ProofArtifactIDs []string `json:"proof_artifact_ids"`
}

type submitIndividualScopeRequest struct {
	ScannedIdentifiers []string `json:"scanned_identifiers"`
}

type reopenScopeRequest struct {
	Reason string `json:"reason"`
}

// closeRequest carries the mandatory close reason. The idempotency key is read
// from the body (contract) or the Idempotency-Key header (mobile/admin default),
// with the body winning when both are present.
type closeRequest struct {
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

// ListCampaigns serves the admin listing, where an absent scope means the flat all-tasks list.
func (h *Handler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
	h.listCampaigns(w, r, domain.CampaignListScopeAll)
}

// AppListCampaigns serves the phone. An absent scope means the caller's OWN work, which is what
// an already-installed app that predates the scope parameter expects to receive.
func (h *Handler) AppListCampaigns(w http.ResponseWriter, r *http.Request) {
	h.listCampaigns(w, r, domain.CampaignListScopeMine)
}

func (h *Handler) listCampaigns(w http.ResponseWriter, r *http.Request, fallback domain.CampaignListScope) {
	limit, ok := h.queryLimit(w, r, 20)
	if !ok {
		return
	}
	scope, ok := domain.ParseCampaignListScope(r.URL.Query().Get("scope"), fallback)
	if !ok {
		h.respond(w, r, nil, ports.ErrInvalidArgument)
		return
	}
	// park_id filters the ROWS only. counts stays a whole-scope aggregate on purpose, so the
	// Active/Completed tab numbers do not move when the park chip changes or the user pages.
	caller := actor(r)
	page, err := h.service.ListCampaigns(r.Context(), caller, scope, r.URL.Query().Get("park_id"), r.URL.Query().Get("cursor"), limit)
	// Which task-level writes THIS caller may attempt. Publish and end are held by
	// DIFFERENT permissions (plan vs monitor), so a client that gates only on status
	// shows a live button that 403s -- a growth director holds monitor and not plan.
	capabilities := map[string]bool{
		"can_publish": permissions.RolesAuthorize(caller.Roles, []string{permissions.WeighingPlan}, false),
		"can_end":     permissions.RolesAuthorize(caller.Roles, []string{permissions.WeighingMonitor}, false),
		"can_reopen":  permissions.RolesAuthorize(caller.Roles, []string{permissions.WeighingMonitor}, false),
	}
	h.respond(w, r, map[string]any{"items": page.Items, "next_cursor": page.NextCursor, "counts": page.Counts, "capabilities": capabilities, "trace_id": traceID(r)}, err)
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

// PlannerCatalog is the PARK-grain read. It has no cursor and no limit: a park picker that
// paged could not offer the parks it had not reached yet, which is exactly the defect this
// split removes. The many-side page is PlannerParkBuckets.
func (h *Handler) PlannerCatalog(w http.ResponseWriter, r *http.Request) {
	catalog, err := h.service.PlannerCatalog(r.Context(), actor(r), r.URL.Query().Get("period_start_date"))
	h.respond(w, r, map[string]any{"parks": catalog.Parks, "operators": catalog.Operators, "trace_id": traceID(r)}, err)
}

func (h *Handler) PlannerParkBuckets(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.queryLimit(w, r, domain.PlannerBucketPageSize)
	if !ok {
		return
	}
	page, err := h.service.PlannerParkBuckets(
		r.Context(), actor(r),
		r.PathValue("park_id"),
		r.URL.Query().Get("period_start_date"),
		r.URL.Query().Get("exclude_campaign_id"),
		r.URL.Query().Get("cursor"),
		limit,
	)
	h.respond(w, r, map[string]any{"park_id": page.ParkID, "sheds": page.Sheds, "next_cursor": page.NextCursor, "trace_id": traceID(r)}, err)
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
	page, err := h.service.ListScopeRoster(
		r.Context(), actor(r), r.PathValue("campaign_id"), r.PathValue("campaign_shed_id"),
		r.URL.Query().Get("cursor"), r.URL.Query().Get("observations_cursor"), limit,
		r.URL.Query().Get("include_roster") != "false",
	)
	h.respond(w, r, map[string]any{
		"items":                    page.Items,
		"observations":             page.Observations,
		"next_cursor":              page.NextCursor,
		"next_observations_cursor": page.NextObservationsCursor,
		"trace_id":                 traceID(r),
	}, err)
}

func (h *Handler) ListCampaignSheds(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.queryLimit(w, r, domain.CampaignShedPageSize)
	if !ok {
		return
	}
	page, err := h.service.ListCampaignSheds(r.Context(), actor(r), r.PathValue("campaign_id"), r.URL.Query().Get("cursor"), limit)
	h.respond(w, r, map[string]any{
		"campaign_id": page.CampaignID,
		"items":       page.Items,
		"next_cursor": page.NextCursor,
		"total_count": page.TotalCount,
		"trace_id":    traceID(r),
	}, err)
}

func (h *Handler) GetLeadershipShedVideos(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.queryLimit(w, r, domain.LeadershipShedVideosPageSize)
	if !ok {
		return
	}
	result, err := h.service.GetLeadershipShedVideos(
		r.Context(),
		actor(r),
		r.PathValue("campaign_id"),
		r.PathValue("campaign_shed_id"),
		r.URL.Query().Get("cursor"),
		limit,
	)
	if err == nil {
		err = h.resolveLeadershipMedia(r.Context(), actor(r).TenantID, &result)
	}
	h.respond(w, r, map[string]any{"shed": result, "trace_id": traceID(r)}, err)
}

// ListLeadershipSheds serves the gallery a PAGE of buckets. The client used to
// build this page itself by calling the single-bucket read once per bucket.
func (h *Handler) ListLeadershipSheds(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.queryLimit(w, r, domain.LeadershipShedPageSize)
	if !ok {
		return
	}
	page, err := h.service.ListLeadershipSheds(r.Context(), actor(r), r.URL.Query().Get("cursor"), limit)
	if err == nil {
		for i := range page.Items {
			if err = h.resolveLeadershipMedia(r.Context(), actor(r).TenantID, &page.Items[i]); err != nil {
				break
			}
		}
	}
	h.respond(w, r, map[string]any{
		"items":       page.Items,
		"next_cursor": page.NextCursor,
		"trace_id":    traceID(r),
	}, err)
}

func (h *Handler) resolveLeadershipMedia(ctx context.Context, tenantID string, result *domain.LeadershipShedVideos) error {
	if h.media == nil {
		return errors.New("weighing media resolver is unavailable")
	}
	resolve := func(observation *domain.Observation) error {
		ids := observation.ProofArtifactIDs
		if len(ids) == 0 && observation.ProofArtifactID != "" {
			ids = []string{observation.ProofArtifactID}
		}
		observation.Media = make([]domain.ProofMedia, 0, len(ids))
		for _, proofID := range ids {
			url, err := h.media.DownloadURL(ctx, tenantID, proofID)
			if err != nil {
				return err
			}
			observation.Media = append(observation.Media, domain.ProofMedia{ProofID: proofID, DownloadURL: url})
		}
		return nil
	}
	for i := range result.Individual {
		if err := resolve(&result.Individual[i]); err != nil {
			return err
		}
	}
	if result.LumpSum != nil {
		return resolve(result.LumpSum)
	}
	return nil
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
		CampaignID: r.PathValue("campaign_id"), CampaignShedID: req.CampaignShedID, ScannedIdentifier: req.ScannedIdentifier, WeightKg: req.WeightKg, ProofArtifactID: req.ProofArtifactID, ActualLocationID: req.ActualLocationID, IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	h.respond(w, r, map[string]any{"observation": obs, "trace_id": traceID(r)}, err)
}

func (h *Handler) RecordShedObservation(w http.ResponseWriter, r *http.Request) {
	var req shedObservationRequest
	if !h.decode(w, r, &req) {
		return
	}
	obs, err := h.service.RecordShedObservation(r.Context(), actor(r), domain.RecordShedObservation{
		CampaignID: r.PathValue("campaign_id"), CampaignShedID: req.CampaignShedID, WeightKg: req.WeightKg, AverageWeightKg: req.AverageWeightKg, AnimalCount: req.AnimalCount,
		ProofArtifactID: req.ProofArtifactID, ProofArtifactIDs: req.ProofArtifactIDs, IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	h.respond(w, r, map[string]any{"observation": obs, "trace_id": traceID(r)}, err)
}

func (h *Handler) SubmitIndividualScope(w http.ResponseWriter, r *http.Request) {
	var req submitIndividualScopeRequest
	if !h.decode(w, r, &req) {
		return
	}
	err := h.service.SubmitIndividualScope(
		r.Context(),
		actor(r),
		r.PathValue("campaign_id"),
		r.PathValue("campaign_shed_id"),
		r.Header.Get("Idempotency-Key"),
		req.ScannedIdentifiers,
	)
	h.respond(w, r, map[string]any{"status": "completed", "trace_id": traceID(r)}, err)
}

func (h *Handler) ReopenScope(w http.ResponseWriter, r *http.Request) {
	var req reopenScopeRequest
	if !h.decode(w, r, &req) {
		return
	}
	err := h.service.ReopenScope(
		r.Context(),
		actor(r),
		r.PathValue("campaign_id"),
		r.PathValue("campaign_shed_id"),
		r.Header.Get("Idempotency-Key"),
		req.Reason,
	)
	h.respond(w, r, map[string]any{"status": "reopened", "trace_id": traceID(r)}, err)
}

func (h *Handler) CloseScope(w http.ResponseWriter, r *http.Request) {
	var req closeRequest
	if !h.decode(w, r, &req) {
		return
	}
	result, err := h.service.CloseScope(
		r.Context(),
		actor(r),
		r.PathValue("campaign_id"),
		r.PathValue("campaign_shed_id"),
		h.idempotencyKey(r, req.IdempotencyKey),
		req.Reason,
	)
	h.respond(w, r, map[string]any{"close": result, "trace_id": traceID(r)}, err)
}

// AbandonScope is the explicit force-close. It is a DIFFERENT endpoint from close so
// that ending unverified work is a deliberate act, never a fallback the UI can slip
// into when the normal close is refused.
func (h *Handler) AbandonScope(w http.ResponseWriter, r *http.Request) {
	var req closeRequest
	if !h.decode(w, r, &req) {
		return
	}
	result, err := h.service.AbandonScope(
		r.Context(),
		actor(r),
		r.PathValue("campaign_id"),
		r.PathValue("campaign_shed_id"),
		h.idempotencyKey(r, req.IdempotencyKey),
		req.Reason,
	)
	h.respond(w, r, map[string]any{"close": result, "trace_id": traceID(r)}, err)
}

func (h *Handler) CloseCampaign(w http.ResponseWriter, r *http.Request) {
	var req closeRequest
	if !h.decode(w, r, &req) {
		return
	}
	result, err := h.service.CloseCampaign(
		r.Context(),
		actor(r),
		r.PathValue("campaign_id"),
		h.idempotencyKey(r, req.IdempotencyKey),
		req.Reason,
	)
	h.respond(w, r, map[string]any{"close": result, "trace_id": traceID(r)}, err)
}

func (h *Handler) idempotencyKey(r *http.Request, fromBody string) string {
	if key := strings.TrimSpace(fromBody); key != "" {
		return key
	}
	return strings.TrimSpace(r.Header.Get("Idempotency-Key"))
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
	case errors.Is(err, ports.ErrVerificationPending):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "verification_pending", Message: "This shed still has videos waiting to be checked.", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrScopeIncomplete):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "scope_incomplete", Message: "submitted scan list omits already-captured observations for this shed", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrOperatorOutsidePark):
		// Farm language, not a rule name: the planner picked someone who does not work that park.
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "operator_outside_park", Message: "One of the people chosen does not work in this park. Pick someone from this park, or a director who covers both.", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrShedAlreadyScheduled):
		// The blocked bucket NAMES travel with the 409 so the planner can render the
		// reason inline instead of asking again.
		conflict := &ports.ShedScheduleConflict{}
		if !errors.As(err, &conflict) {
			conflict = &ports.ShedScheduleConflict{}
		}
		fieldErrors := make([]conflictFieldError, 0, len(conflict.Sheds))
		for _, shed := range conflict.Sheds {
			fieldErrors = append(fieldErrors, conflictFieldError{
				Field:   "sheds",
				Code:    "weighing_shed_already_scheduled",
				Message: shed + " is already scheduled on " + conflict.WeighDate + ".",
			})
		}
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, shedScheduleConflictEnvelope{
			Code:        "weighing_shed_already_scheduled",
			Message:     "Some of these sheds are already scheduled on this date.",
			FieldErrors: fieldErrors,
			TraceID:     traceID(r),
		}, nil)
	case errors.Is(err, ports.ErrIdempotencyConflict):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "idempotency_conflict", Message: "idempotency key was reused with a different request", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrDuplicateScan):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "duplicate_scan", Message: "this tag was already captured and submitted earlier today for this shed", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrWriteConflict):
		// Every ErrWriteConflict is retried internally inside
		// RecordAnimalObservation before it can ever reach this handler (see
		// recordAnimalObservationMaxSerializationRetries); a caller only sees
		// this after every retry ALSO lost a SERIALIZABLE race, which is a
		// transient contention signal, not a claim that anything is wrong
		// with the request itself. 503 (not the 409 duplicate_scan uses)
		// because this says "retry the exact same request", never "this
		// request cannot proceed as issued" -- collapsing it into
		// duplicate_scan would tell the operator they double-scanned an
		// animal they never scanned twice.
		httpresponse.WriteError(w, r, h.log, http.StatusServiceUnavailable, errorEnvelope{Code: "write_conflict", Message: "this capture is being retried due to a momentary conflict; please try again", TraceID: traceID(r)}, nil)
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

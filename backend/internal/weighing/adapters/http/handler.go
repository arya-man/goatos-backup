package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
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
	ListScopeRoster(ctx context.Context, actor domain.Actor, campaignID, campaignShedID string, observationsCursor string, limit int) (domain.RosterPage, error)
	ListCampaignSheds(ctx context.Context, actor domain.Actor, campaignID, cursor string, limit int) (domain.CampaignShedPage, error)
	GetCampaign(ctx context.Context, actor domain.Actor, campaignID string) (domain.Campaign, error)
	// CampaignCapabilities takes the RESOLVED campaign, not an id: the buttons are park-scoped
	// and must be answered for the park on the row that was just authorized and returned, never
	// for a park fetched again afterwards.
	CampaignCapabilities(ctx context.Context, actor domain.Actor, campaign domain.Campaign) domain.CampaignCapabilities
	ListParks(ctx context.Context, actor domain.Actor) ([]domain.WeighingPark, error)
	GetLeadershipShedVideos(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, cursor string, limit int) (domain.LeadershipShedVideos, error)
	ListLeadershipSheds(ctx context.Context, actor domain.Actor, cursor string, limit int) (domain.LeadershipShedPage, error)
	RecordAnimalObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordAnimalObservation) (domain.Observation, error)
	RecordShedObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordShedObservation) (domain.Observation, error)
	SubmitIndividualScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey string, scannedIdentifiers []string) error
	ReopenScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey, reason string) error
	CloseScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey, reason string) (domain.CloseResult, error)
	CloseCampaign(ctx context.Context, actor domain.Actor, campaignID, idempotencyKey, reason string) (domain.CloseResult, error)
	WeighingProcessState(ctx context.Context, actor domain.Actor, campaignID, fromBusinessDate, toBusinessDate string) (domain.ProcessState, error)
	ListAlerts(ctx context.Context, actor domain.Actor, cursor string, limit int) (domain.AlertPage, error)
	GetWeightHistory(ctx context.Context, actor domain.Actor, parkID, campaignShedID string) (domain.WeightHistory, error)
	GetLeadershipGrowthADG(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate string) (domain.GrowthADG, error)
	ExportCampaignCSV(ctx context.Context, actor domain.Actor, campaignID string, writer io.Writer) error
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
	mux.HandleFunc("GET /weighing/campaigns/{campaign_id}/export", h.ExportCampaignCSV)
	mux.HandleFunc("GET /app/weighing/planner/catalog", h.PlannerCatalog)
	mux.HandleFunc("GET /app/weighing/planner/parks/{park_id}/buckets", h.PlannerParkBuckets)
	mux.HandleFunc("GET /app/weighing/campaigns", h.AppListCampaigns)
	// Registered BEFORE the {campaign_id} pattern is irrelevant to net/http's precedence (it
	// picks the most specific pattern), but the two are listed together so the literal
	// "/app/weighing/parks" segment is visibly not a campaign id.
	mux.HandleFunc("GET /app/weighing/parks", h.ListParks)
	mux.HandleFunc("GET /app/weighing/campaigns/{campaign_id}", h.GetCampaign)
	mux.HandleFunc("GET /app/weighing/campaigns/{campaign_id}/sheds", h.ListCampaignSheds)
	mux.HandleFunc("GET /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/roster", h.ListScopeRoster)
	mux.HandleFunc("GET /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/videos", h.GetLeadershipShedVideos)
	mux.HandleFunc("GET /app/weighing/leadership/sheds", h.ListLeadershipSheds)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/animal-observations", h.RecordAnimalObservation)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/shed-observations", h.RecordShedObservation)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/submit", h.SubmitIndividualScope)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/reopen", h.ReopenScope)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/close", h.CloseScope)
	mux.HandleFunc("POST /app/weighing/campaigns/{campaign_id}/close", h.CloseCampaign)
	// PHASE 2 Calendar / Control Tower binding. Backend-owned grain + disjoint
	// buckets + whole-filter summary; renderers never recompute totals.
	mux.HandleFunc("GET /weighing/process-state", h.WeighingProcessState)
	// The weighing module's OWN lifecycle feed. Deliberately under /app/weighing/*
	// so the href carries the module scoping and the nav tab can stay labelled
	// just "Alerts" (maintainer ruling 2026-08-03).
	mux.HandleFunc("GET /app/weighing/alerts", h.ListAlerts)
	mux.HandleFunc("GET /app/weighing/weight-history", h.GetWeightHistory)
	mux.HandleFunc("GET /app/weighing/leadership/growth", h.GetLeadershipGrowthADG)
}

// ListAlerts serves the weighing alerts feed. Title and empty-state copy travel
// in the response because the BACKEND owns every visible label on this surface;
// the phone renders what it is given and hardcodes no weighing strings.
func (h *Handler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.queryLimit(w, r, domain.AlertPageSize)
	if !ok {
		return
	}
	page, err := h.service.ListAlerts(r.Context(), actor(r), r.URL.Query().Get("cursor"), limit)
	h.respond(w, r, map[string]any{
		"items":         page.Items,
		"next_cursor":   page.NextCursor,
		"title":         page.Title,
		"empty_message": page.EmptyMessage,
	}, err)
}

// GetWeightHistory serves CEO-tier weight history per RFID tag across weigh days.
// Query parameters:
//   - park_id (optional): filter to a specific park
//   - campaign_shed_id (optional): filter to a specific shed
func (h *Handler) GetWeightHistory(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetWeightHistory(
		r.Context(),
		actor(r),
		r.URL.Query().Get("park_id"),
		r.URL.Query().Get("campaign_shed_id"),
	)
	h.respond(w, r, result, err)
}

// GetLeadershipGrowthADG serves the CEO-tier ADG (Average Daily Gain) / growth aggregate for a
// park, or the herd-wide aggregate across every park the caller is authorized to monitor. Query
// parameters:
//   - park_id (optional): the park to report on. When omitted, the response aggregates across
//     the caller's own authorized-park scope (never widened) -- see domain.GrowthADG.ParkIDs.
//   - from, to (optional): INCLUSIVE Asia/Kolkata business dates (YYYY-MM-DD). Defaults to the
//     last 90 days ending today when omitted.
func (h *Handler) GetLeadershipGrowthADG(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetLeadershipGrowthADG(
		r.Context(),
		actor(r),
		r.URL.Query().Get("park_id"),
		r.URL.Query().Get("from"),
		r.URL.Query().Get("to"),
	)
	h.respond(w, r, result, err)
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
	// operator_summaries is the OPERATOR-grain roll-up the oversight surface renders.
	// Unlike counts it IS narrowed by park_id, because the park chip is that screen's
	// own filter: a summary naming people who hold no work in the selected park would
	// describe a different screen than the one on show.
	summaries := page.OperatorSummaries
	if summaries == nil {
		summaries = []domain.OperatorSummary{}
	}
	h.respond(w, r, map[string]any{"items": page.Items, "next_cursor": page.NextCursor, "counts": page.Counts, "operator_summaries": summaries, "capabilities": campaignCapabilities(caller), "trace_id": traceID(r)}, err)
}

// campaignCapabilities names which task-level writes THIS caller may attempt SOMEWHERE. Publish
// and end are held by DIFFERENT permissions (plan vs monitor), so a client that gates only on
// status shows a live button that 403s -- a growth director holds monitor and not plan.
//
// It is the LIST envelope's answer only, and it is deliberately park-blind: the envelope is one
// object over a page whose rows may span several parks, so it cannot carry a per-park answer.
// It is therefore an upper bound -- "you hold this permission somewhere on this surface" -- and
// a client must not treat it as per-row authority. The single-task read answers at row grain
// (Service.CampaignCapabilities) and is what a task screen gates its buttons on.
func campaignCapabilities(caller domain.Actor) map[string]bool {
	return map[string]bool{
		"can_publish": permissions.RolesAuthorize(caller.Roles, []string{permissions.WeighingPlan}, false),
		"can_end":     permissions.RolesAuthorize(caller.Roles, []string{permissions.WeighingMonitor}, false),
		"can_reopen":  permissions.RolesAuthorize(caller.Roles, []string{permissions.WeighingMonitor}, false),
	}
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

// GetCampaign resolves ONE task by id, which is what a notification deep link names. The task
// list is a keyset page with no id filter, so without this a cold tap on a task outside the
// first pages could only be answered by walking the keyset and giving up.
//
// It returns `capabilities` because a client that reached the task through a push never saw the
// list response, so gating its buttons on status alone showed a live Publish to a monitor
// (publish is WeighingPlan, ending is WeighingMonitor) that then 403'd.
//
// Those capabilities are computed for the RESOLVED campaign, not for the caller in the
// abstract. The park-blind version was the same defect one level down: end/reopen are
// park-scoped writes that answer ErrNotFound outside the caller's parks, so a monitor scoped
// elsewhere saw a live Close button that failed on tap. Passing the campaign the read
// just returned also means the park the buttons are computed against is the park that was
// authorized, with no extra lookup to disagree with.
func (h *Handler) GetCampaign(w http.ResponseWriter, r *http.Request) {
	caller := actor(r)
	campaign, err := h.service.GetCampaign(r.Context(), caller, r.PathValue("campaign_id"))
	h.respond(w, r, map[string]any{"campaign": campaign, "capabilities": h.service.CampaignCapabilities(r.Context(), caller, campaign), "trace_id": traceID(r)}, err)
}

// ListParks serves the oversight park chips. A separate read rather than a field on the task
// list envelope, because the list can refuse to answer at all until a park is NAMED
// (ErrParkSelectionRequired for a multi-park actor) -- so an envelope-carried vocabulary would
// be missing in precisely the case the client needs it to pick a park.
func (h *Handler) ListParks(w http.ResponseWriter, r *http.Request) {
	parks, err := h.service.ListParks(r.Context(), actor(r))
	h.respond(w, r, map[string]any{"parks": parks, "trace_id": traceID(r)}, err)
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
	// cursor / include_roster are no longer read: the roster half of this
	// response is always empty (weighing is free-flow, no expected roster
	// exists to page or toggle). Accepted-and-ignored so an older client that
	// still sends them keeps working unchanged.
	page, err := h.service.ListScopeRoster(
		r.Context(), actor(r), r.PathValue("campaign_id"), r.PathValue("campaign_shed_id"),
		r.URL.Query().Get("observations_cursor"), limit,
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
		DeviceID: httpmiddleware.DeviceIDFromContext(r.Context()),
	})
	h.logScanOutcome(r, "record_animal_observation", err)
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
		DeviceID: httpmiddleware.DeviceIDFromContext(r.Context()),
	})
	h.logScanOutcome(r, "record_shed_observation", err)
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
	h.logScanOutcome(r, "submit_individual_scope", err)
	h.respond(w, r, map[string]any{"status": "completed", "trace_id": traceID(r)}, err)
}

// logScanOutcome makes "I scanned and nothing happened" reconstructable: WriteError
// (httpresponse) only logs status >= 400, so a successful scan/submit otherwise leaves
// no trace at all. Failure paths are already logged with full context by
// h.respond/WriteError, so this only needs to cover the success case. It is
// deliberately scoped to the handful of scan/submit write routes -- NOT a blanket
// "log every 2xx" -- because farm-scale traffic through this service makes an
// unconditional success log a volume problem.
func (h *Handler) logScanOutcome(r *http.Request, route string, err error) {
	if err != nil {
		return
	}
	h.log.InfoContext(r.Context(), "weighing_scan_attempt",
		slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
		slog.String("trace_id", traceID(r)),
		slog.String("tenant_id", tenantID(r)),
		slog.String("actor_id", httpmiddleware.ActorIDFromContext(r.Context())),
		slog.String("device_id", httpmiddleware.DeviceIDFromContext(r.Context())),
		slog.String("route", route),
		slog.Int("status", http.StatusOK),
	)
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
	case errors.Is(err, ports.ErrParkSelectionRequired):
		// Same shape the vaccination park scope decision returns, so both modules teach the
		// client one remedy. The park list is re-derived from the actor's own grants here
		// rather than threaded through the service signature.
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, struct {
			errorEnvelope
			AvailableParks []string `json:"available_parks"`
		}{
			errorEnvelope: errorEnvelope{
				Code:    "park_selection_required",
				Message: "choose a park to view",
				TraceID: traceID(r),
			},
			// The remedy must be derived from the SAME capability set the service used to
			// decide the actor is multi-park, not from a hardcoded one. Pinning it to
			// WeighingMonitor reintroduced two layers up exactly the coupling this branch
			// removes everywhere else: an actor who trips this on OverseeOperators or Plan
			// without also holding Monitor would receive "choose a park" alongside an EMPTY
			// park list -- an error whose own remedy is unreachable. Latent today only
			// because growth_director happens to carry both.
			AvailableParks: weighingParkSelectionOptions(r.Context()),
		}, nil)
	case errors.Is(err, ports.ErrInvalidArgument):
		h.badRequest(w, r, "invalid_request", "request is invalid")
	case errors.Is(err, ports.ErrNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound, errorEnvelope{Code: "not_found", Message: "weighing resource was not found", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrImmutable):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "invalid_state", Message: "weighing resource is not editable in its current state", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrVerificationPending):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "verification_pending", Message: "This shed still has videos waiting to be checked.", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrReworkNotRecaptured):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "rework_not_recaptured", Message: "A video was sent back. Re-record that animal before submitting this shed again.", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrScopeIncomplete):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "scope_incomplete", Message: "submitted scan list omits already-captured observations for this shed", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrCaptureIncomplete):
		// One animal = one (weight, video) pair. The app renders code/message and
		// walks field_errors to mark the rows to go back and fix, so this must name
		// the ANIMALS -- a status line the operator cannot act on is what this
		// replaced.
		incomplete := &ports.CaptureIncomplete{}
		if !errors.As(err, &incomplete) {
			incomplete = &ports.CaptureIncomplete{}
		}
		fieldErrors := make([]conflictFieldError, 0, len(incomplete.MissingWeight)+len(incomplete.MissingVideo))
		for _, identifier := range incomplete.MissingWeight {
			fieldErrors = append(fieldErrors, conflictFieldError{
				Field:   "scanned_identifiers",
				Code:    "weighing_weight_missing",
				Message: identifier + " has no weight yet. Enter its weight, then submit again.",
			})
		}
		for _, identifier := range incomplete.MissingVideo {
			fieldErrors = append(fieldErrors, conflictFieldError{
				Field:   "scanned_identifiers",
				Code:    "weighing_video_missing",
				Message: identifier + " has no finished video yet. Record or finish uploading its video, then submit again.",
			})
		}
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, shedScheduleConflictEnvelope{
			Code:        "weighing_capture_incomplete",
			Message:     "Some animals still need a weight and a video. Every animal needs both before this shed can be submitted.",
			FieldErrors: fieldErrors,
			TraceID:     traceID(r),
		}, nil)
	case errors.Is(err, ports.ErrProofNotReady):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "weighing_video_missing", Message: "This shed's video is not ready yet. Wait for the video to finish uploading, then submit again.", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrRejectedProofReuse):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "weighing_rejected_proof_reuse", Message: "This video was sent back. Record a new video for this shed, then submit again.", TraceID: traceID(r)}, nil)
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
	case errors.Is(err, ports.ErrFinishedShedBlocksReschedule):
		// Same shape as the already-scheduled conflict: the blocking bucket NAMES
		// travel with the 409 so the planner reads which sheds are the problem
		// without asking again. The copy says what happened in farm words -- the work
		// was already done on the task's own day, so the task cannot take that day
		// with it -- and never mentions statuses or columns.
		finished := &ports.FinishedShedConflict{}
		if !errors.As(err, &finished) {
			finished = &ports.FinishedShedConflict{}
		}
		fieldErrors := make([]conflictFieldError, 0, len(finished.Sheds))
		for _, shed := range finished.Sheds {
			fieldErrors = append(fieldErrors, conflictFieldError{
				Field:   "start_business_date",
				Code:    "weighing_shed_already_weighed",
				Message: shed + " was already weighed on " + finished.WeighDate + ".",
			})
		}
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, shedScheduleConflictEnvelope{
			Code:        "weighing_shed_already_weighed",
			Message:     "This task already has weighed sheds, so it cannot be moved to another date or park. Remove those sheds from it, or leave this task and plan the new date as its own.",
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

// ExportCampaignCSV exports weighing observations for a campaign as CSV.
// It requires WeighingMonitor permission and enforces park scoping.
func (h *Handler) ExportCampaignCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := actor(r)

	// Check authorization: WeighingMonitor only
	if !permissions.RolesAuthorize(a.Roles, []string{permissions.WeighingMonitor}, false) {
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden, errorEnvelope{
			Code:    "permission_denied",
			Message: "requires weighing monitor permission",
			TraceID: traceID(r),
		}, nil)
		return
	}

	campaignID := strings.TrimSpace(r.PathValue("campaign_id"))
	if campaignID == "" {
		h.badRequest(w, r, "invalid_argument", "campaign_id is required")
		return
	}

	// Export CSV via service
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="weighing-export-%s.csv"`, campaignID))

	// Counts bytes so a mid-stream failure is not "corrected" by appending a JSON error body to
	// a half-written CSV. Once a single row has gone out the status is already 200 and the file
	// is already downloading; writing an error envelope after that produces a CSV whose last
	// line is JSON, which opens in Excel as a corrupt sheet that LOOKS like data. A truncated
	// file that fails loudly in the client is honest; a silently corrupted one is not.
	counting := &countingResponseWriter{ResponseWriter: w}
	if err := h.service.ExportCampaignCSV(ctx, a, campaignID, counting); err != nil {
		if counting.written > 0 {
			// Already streaming: log it and cut the response off. The client sees a truncated
			// body, which its CSV parse will reject.
			h.log.Error("export campaign csv failed mid-stream",
				"campaign_id", campaignID, "bytes_written", counting.written, "error", err)
			return
		}
		if errors.Is(err, ports.ErrNotFound) {
			httpresponse.WriteError(w, r, h.log, http.StatusNotFound, errorEnvelope{
				Code:    "not_found",
				Message: "campaign not found",
				TraceID: traceID(r),
			}, nil)
		} else if errors.Is(err, ports.ErrForbidden) {
			httpresponse.WriteError(w, r, h.log, http.StatusForbidden, errorEnvelope{
				Code:    "permission_denied",
				Message: "not authorized for this campaign",
				TraceID: traceID(r),
			}, nil)
		} else {
			h.log.Error("export campaign csv failed", "campaign_id", campaignID, "error", err)
			httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, errorEnvelope{
				Code:    "internal_error",
				Message: "failed to export campaign data",
				TraceID: traceID(r),
			}, err)
		}
		return
	}
}

// countingResponseWriter records whether any body bytes reached the client, so the export
// handler can tell "failed before anything was sent" (safe to write a proper error status) from
// "failed halfway through a download" (must not append anything).
type countingResponseWriter struct {
	http.ResponseWriter
	written int
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.written += n
	return n, err
}

var _ = permissions.WeighingMonitor

// weighingParkSelectionOptions lists every park the actor could legitimately name when the
// service asks them to choose one. It unions the capabilities that admit the multi-park
// surfaces rather than assuming a single one, so the remedy can never come back empty for an
// actor the service just told to pick a park.
func weighingParkSelectionOptions(ctx context.Context) []string {
	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	seen := map[string]struct{}{}
	parks := []string{}
	for _, capability := range []string{
		permissions.WeighingMonitor,
		permissions.WeighingPlan,
		permissions.WeighingOverseeOperators,
	} {
		for _, parkID := range httpmiddleware.AuthorizedParkIDsForCapability(grants, capability) {
			if _, ok := seen[parkID]; ok {
				continue
			}
			seen[parkID] = struct{}{}
			parks = append(parks, parkID)
		}
	}
	sort.Strings(parks)
	return parks
}

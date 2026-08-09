// Package http exposes the authored feed-configuration read/write API that the /feed/config UI
// consumes.
//
// READS are bounded pages over authored config; WRITES are effective-dated edits under the
// mandatory idempotency contract.
//
// # THE IDEMPOTENCY CONTRACT, AS IMPLEMENTED HERE
//
// Every mutating route requires an Idempotency-Key header and derives a request fingerprint from
// the CANONICAL CLIENT REQUEST ONLY -- tenant, command, route, and the normalized body. Nothing
// server-generated (the business date, the actor, a timestamp) is folded in.
//
// That exclusion is load-bearing, not incidental. If the business date were part of the
// fingerprint, a retry that crossed midnight IST would hash differently and a legitimate retry
// would be rejected as a same-key/different-payload conflict. If the actor were included, the same
// edit retried from a second session would look like a different request. So: the fingerprint
// covers what the client asserted, and only that.
//
// # VALIDATE OR REJECT
//
// Authored numbers arrive as *json.Number and stay pointers all the way into the service. That is
// what keeps ABSENT distinguishable from an authored 0 -- the distinction migration 000003 exists
// to protect, because absent means "not configured, BLOCK" while 0 means "feed nothing, proceed".
// A present-but-out-of-range value fails with a field error; it is never rewritten to a default.
package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	feedconfigapp "github.com/vgoats/goatos/backend/internal/feedconfig/app"
	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

const (
	rationRatesRoute      = "/feed-config/ration-rates"
	rationGroupsRoute     = "/feed-config/ration-groups"
	shedTagsRoute         = "/feed-config/shed-tags"
	feedItemsRoute        = "/feed-config/feed-items"
	sessionTemplatesRoute = "/feed-config/session-templates"
	scheduleRoute         = "/feed-config/schedule"
	shedFactorsRoute      = "/feed-config/shed-factors"
	experimentRoute       = "/feed-config/experiment"
	// pensRoute is the enroller's candidate source: the park's operational locations. It is its own
	// route rather than a flag on the shed list because a pen, not a shed, is what an experiment is
	// authored against.
	pensRoute = "/feed-config/pens"
	// experimentBatchRoute authors EVERY feed item of one pen atomically. A separate route from
	// experimentRoute because the guarantee differs: the single-cell write corrects one number, while
	// this one is all-or-nothing across a set precisely so a pen is never left half-authored (and so
	// silently underfed, since a pen's authored cells ARE the complete list of what it is fed).
	experimentBatchRoute = "/feed-config/experiment/batch"
	// experimentShedStatusRoute is a SEPARATE route from experimentRoute, not a status field on the
	// cell write. The two are different acts: one authors a quantity, the other changes WHICH
	// WORKFLOW feeds the shed. Keeping them apart means the workflow switch is an explicit request an
	// operator (and the audit trail) can see, rather than a side effect of saving a number.
	experimentShedStatusRoute = "/feed-config/experiment/shed-status"

	// Command names namespace the fingerprint hash so the same body posted to two different write
	// routes can never collide on one idempotency key.
	upsertRationRateCommand    = "feedconfig.ration_rate.upsert"
	createFeedItemCommand      = "feedconfig.feed_item.create"
	upsertShedFactorCommand    = "feedconfig.shed_factor.upsert"
	upsertScheduleCommand      = "feedconfig.schedule_config.upsert"
	upsertExperimentCommand    = "feedconfig.experiment_config.upsert"
	upsertExperimentBatchCmd   = "feedconfig.experiment_config.upsert_batch"
	setExperimentShedStatusCmd = "feedconfig.experiment_config.set_shed_status"

	maxBodyBytes = 1 << 20
)

// Service is the slice of feedconfig/app.Service this handler needs. *feedconfigapp.Service
// satisfies it.
type Service interface {
	ListRationRates(ctx context.Context, tenantID, parkID, rationGroup, shedTag, feedItem string, limit, offset *int32) (domain.RationRatePage, error)
	ListRationGroups(ctx context.Context, tenantID string, limit, offset *int32) (domain.RationGroupPage, error)
	ListShedTags(ctx context.Context, tenantID, appliesTo string, limit, offset *int32) (domain.ShedTagPage, error)
	ListFeedItems(ctx context.Context, tenantID string, limit, offset *int32) (domain.FeedItemPage, error)
	ListSessionTemplates(ctx context.Context, tenantID, parkID string, limit, offset *int32) (domain.SessionTemplatePage, error)
	ListScheduleConfig(ctx context.Context, tenantID, parkID, workflow string, limit, offset *int32) (domain.ScheduleConfigPage, error)
	ListShedFactors(ctx context.Context, tenantID, parkID, shedID, feedItem string, limit, offset *int32) (domain.ShedFactorPage, error)
	ListExperimentConfig(ctx context.Context, tenantID, parkID, shedID, status string, limit, offset *int32) (domain.ExperimentConfigPage, error)
	ListPens(ctx context.Context, tenantID, parkID string, limit, offset *int32) (domain.PenPage, error)

	UpsertRationRate(ctx context.Context, in feedconfigapp.UpsertRationRateInput) (domain.WriteResult, error)
	CreateFeedItem(ctx context.Context, in feedconfigapp.CreateFeedItemInput) (domain.WriteResult, error)
	UpsertShedFactor(ctx context.Context, in feedconfigapp.UpsertShedFactorInput) (domain.WriteResult, error)
	UpsertScheduleConfig(ctx context.Context, in feedconfigapp.UpsertScheduleConfigInput) (domain.WriteResult, error)
	UpsertExperimentConfig(ctx context.Context, in feedconfigapp.UpsertExperimentConfigInput) (domain.WriteResult, error)
	UpsertExperimentConfigBatch(ctx context.Context, in feedconfigapp.UpsertExperimentConfigBatchInput) (domain.WriteResult, error)
	SetExperimentShedStatus(ctx context.Context, in feedconfigapp.SetExperimentShedStatusInput) (domain.WriteResult, error)
}

type Handler struct {
	service Service
	log     *slog.Logger
}

func NewHandler(service Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{service: service, log: log}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET "+rationRatesRoute, h.ListRationRates)
	mux.HandleFunc("GET "+rationGroupsRoute, h.ListRationGroups)
	mux.HandleFunc("GET "+shedTagsRoute, h.ListShedTags)
	mux.HandleFunc("GET "+feedItemsRoute, h.ListFeedItems)
	mux.HandleFunc("GET "+sessionTemplatesRoute, h.ListSessionTemplates)
	mux.HandleFunc("GET "+scheduleRoute, h.ListScheduleConfig)
	mux.HandleFunc("GET "+shedFactorsRoute, h.ListShedFactors)
	mux.HandleFunc("GET "+experimentRoute, h.ListExperimentConfig)
	mux.HandleFunc("GET "+pensRoute, h.ListPens)

	mux.HandleFunc("POST "+rationRatesRoute, h.UpsertRationRate)
	mux.HandleFunc("POST "+feedItemsRoute, h.CreateFeedItem)
	mux.HandleFunc("POST "+shedFactorsRoute, h.UpsertShedFactor)
	mux.HandleFunc("POST "+scheduleRoute, h.UpsertScheduleConfig)
	mux.HandleFunc("POST "+experimentRoute, h.UpsertExperimentConfig)
	mux.HandleFunc("POST "+experimentBatchRoute, h.UpsertExperimentConfigBatch)
	mux.HandleFunc("POST "+experimentShedStatusRoute, h.SetExperimentShedStatus)
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

func (h *Handler) ListRationRates(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	limit, offset, err := pageParams(r.URL.Query())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_paging", err.Error(), nil)
		return
	}
	q := r.URL.Query()
	page, err := h.service.ListRationRates(r.Context(), tenantID, q.Get("park_id"),
		q.Get("ration_group"), q.Get("shed_tag"), q.Get("feed_item"), limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListRationGroups(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	limit, offset, err := pageParams(r.URL.Query())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_paging", err.Error(), nil)
		return
	}
	page, err := h.service.ListRationGroups(r.Context(), tenantID, limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListShedTags(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	limit, offset, err := pageParams(r.URL.Query())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_paging", err.Error(), nil)
		return
	}
	page, err := h.service.ListShedTags(r.Context(), tenantID, r.URL.Query().Get("applies_to"), limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListFeedItems(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	limit, offset, err := pageParams(r.URL.Query())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_paging", err.Error(), nil)
		return
	}
	page, err := h.service.ListFeedItems(r.Context(), tenantID, limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListSessionTemplates(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	limit, offset, err := pageParams(r.URL.Query())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_paging", err.Error(), nil)
		return
	}
	page, err := h.service.ListSessionTemplates(r.Context(), tenantID, r.URL.Query().Get("park_id"), limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListScheduleConfig(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	limit, offset, err := pageParams(r.URL.Query())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_paging", err.Error(), nil)
		return
	}
	q := r.URL.Query()
	page, err := h.service.ListScheduleConfig(r.Context(), tenantID, q.Get("park_id"), q.Get("workflow"), limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListShedFactors(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	limit, offset, err := pageParams(r.URL.Query())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_paging", err.Error(), nil)
		return
	}
	q := r.URL.Query()
	page, err := h.service.ListShedFactors(r.Context(), tenantID, q.Get("park_id"), q.Get("shed_id"), q.Get("feed_item"), limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListExperimentConfig(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	limit, offset, err := pageParams(r.URL.Query())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_paging", err.Error(), nil)
		return
	}
	q := r.URL.Query()
	page, err := h.service.ListExperimentConfig(r.Context(), tenantID, q.Get("park_id"), q.Get("shed_id"), q.Get("status"), limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

// ListPens serves the park's operational-location catalog: every active shed and every pen of a
// subdivided shed, each flagged with whether it already carries experiment configuration.
func (h *Handler) ListPens(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	limit, offset, err := pageParams(r.URL.Query())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_paging", err.Error(), nil)
		return
	}
	page, err := h.service.ListPens(r.Context(), tenantID, r.URL.Query().Get("park_id"), limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

// upsertRationRateRequest is the grid-edit body.
//
// GramsPerHead is a *json.Number, and BOTH halves of that type matter:
//
//	pointer      -- absent stays distinguishable from an authored 0. A cleared input is "not
//	                configured" (blocking); an explicit 0 is "feed nothing" (correct for milk-fed
//	                kids). Collapsing them is the starvation path migration 000003 is built around.
//	json.Number  -- the authored decimal survives as its exact text. Decoding into float64 would
//	                round-trip 149.995 into something else on a screen whose entire job is exact
//	                numbers.
type upsertRationRateRequest struct {
	ParkID       string       `json:"park_id"`
	RationGroup  string       `json:"ration_group"`
	ShedTag      string       `json:"shed_tag"`
	FeedItem     string       `json:"feed_item"`
	GramsPerHead *json.Number `json:"grams_per_head"`
}

func (h *Handler) UpsertRationRate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req upsertRationRateRequest
	if !h.decode(w, r, &req, "UpsertRationRateRequest") {
		return
	}
	// Normalize BEFORE hashing so two requests differing only in label whitespace are the same
	// request, and hash only the client's own assertion.
	req.ParkID = strings.TrimSpace(req.ParkID)
	req.RationGroup = strings.TrimSpace(req.RationGroup)
	req.ShedTag = strings.TrimSpace(req.ShedTag)
	req.FeedItem = strings.TrimSpace(req.FeedItem)

	fingerprint, err := requestFingerprint(tenantID, upsertRationRateCommand, rationRatesRoute, req)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	result, err := h.service.UpsertRationRate(r.Context(), feedconfigapp.UpsertRationRateInput{
		TenantID:           tenantID,
		ActorRef:           h.actor(r),
		ParkID:             req.ParkID,
		RationGroupLabel:   req.RationGroup,
		ShedTagLabel:       req.ShedTag,
		FeedItemLabel:      req.FeedItem,
		GramsPerHead:       numberPtr(req.GramsPerHead),
		IdempotencyKey:     key,
		RequestFingerprint: fingerprint,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, result)
}

// createFeedItemRequest adds one entry to the tenant's feed vocabulary.
//
// NO park_id: feed_item_catalog is keyed (tenant, feed_item_key) and is shared by every park.
//
// The three attributes are *json.Number, and here the POINTER means something DIFFERENT from what
// it means on grams_per_head. There, absent is rejected because a missing rate is a blocking state.
// Here, absent is ACCEPTED and stored as NULL -- the honest "nobody measured this" -- while an
// explicit 0 stays a measured zero. json.Number still keeps the authored decimal exact, so a
// dry-matter factor of 0.8500 does not become 0.8499999.
//
// DisplayOrder is a *int32 whose absence means "append to the end of the catalog", resolved
// server-side. That derivation is allowed only because display_order is a presentation position no
// feeding decision reads.
type createFeedItemRequest struct {
	FeedItem        string       `json:"feed_item"`
	EnergyKcalPerKg *json.Number `json:"energy_kcal_per_kg"`
	DryMatterFactor *json.Number `json:"dry_matter_factor"`
	WastageFactor   *json.Number `json:"wastage_factor"`
	DisplayOrder    *int32       `json:"display_order"`
}

func (h *Handler) CreateFeedItem(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req createFeedItemRequest
	if !h.decode(w, r, &req, "CreateFeedConfigFeedItemRequest") {
		return
	}
	// Normalized before hashing, like every other write here, so two submissions differing only in
	// the label's surrounding whitespace are recognised as the same request rather than as two.
	req.FeedItem = strings.TrimSpace(req.FeedItem)

	fingerprint, err := requestFingerprint(tenantID, createFeedItemCommand, feedItemsRoute, req)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	result, err := h.service.CreateFeedItem(r.Context(), feedconfigapp.CreateFeedItemInput{
		TenantID:           tenantID,
		ActorRef:           h.actor(r),
		FeedItemLabel:      req.FeedItem,
		EnergyKcalPerKg:    numberPtr(req.EnergyKcalPerKg),
		DryMatterFactor:    numberPtr(req.DryMatterFactor),
		WastageFactor:      numberPtr(req.WastageFactor),
		DisplayOrder:       req.DisplayOrder,
		IdempotencyKey:     key,
		RequestFingerprint: fingerprint,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, result)
}

type upsertShedFactorRequest struct {
	ParkID     string       `json:"park_id"`
	ShedID     string       `json:"shed_id"`
	FeedItem   string       `json:"feed_item"`
	Multiplier *json.Number `json:"multiplier"`
}

func (h *Handler) UpsertShedFactor(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req upsertShedFactorRequest
	if !h.decode(w, r, &req, "UpsertShedFactorRequest") {
		return
	}
	req.ParkID = strings.TrimSpace(req.ParkID)
	req.ShedID = strings.TrimSpace(req.ShedID)
	req.FeedItem = strings.TrimSpace(req.FeedItem)

	fingerprint, err := requestFingerprint(tenantID, upsertShedFactorCommand, shedFactorsRoute, req)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	result, err := h.service.UpsertShedFactor(r.Context(), feedconfigapp.UpsertShedFactorInput{
		TenantID:           tenantID,
		ActorRef:           h.actor(r),
		ParkID:             req.ParkID,
		ShedID:             req.ShedID,
		FeedItemLabel:      req.FeedItem,
		Multiplier:         numberPtr(req.Multiplier),
		IdempotencyKey:     key,
		RequestFingerprint: fingerprint,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, result)
}

// upsertScheduleRequest is the dispatch-clock body.
//
// The three times are LOCAL Asia/Kolkata wall-clock strings ("07:00" or "07:00:00") and carry no
// offset. TransportTime is a pointer because NULL is a real authored state -- "this park has not
// declared a transport cutoff" -- which must stay distinct from a value.
type upsertScheduleRequest struct {
	ParkID         string  `json:"park_id"`
	Workflow       string  `json:"workflow"`
	DirectionTime  string  `json:"direction_time"`
	CorrectionTime string  `json:"correction_time"`
	TransportTime  *string `json:"transport_time"`
}

func (h *Handler) UpsertScheduleConfig(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req upsertScheduleRequest
	if !h.decode(w, r, &req, "UpsertFeedScheduleConfigRequest") {
		return
	}
	req.ParkID = strings.TrimSpace(req.ParkID)
	req.Workflow = strings.ToLower(strings.TrimSpace(req.Workflow))
	req.DirectionTime = strings.TrimSpace(req.DirectionTime)
	req.CorrectionTime = strings.TrimSpace(req.CorrectionTime)
	if req.TransportTime != nil {
		trimmed := strings.TrimSpace(*req.TransportTime)
		req.TransportTime = &trimmed
	}

	fingerprint, err := requestFingerprint(tenantID, upsertScheduleCommand, scheduleRoute, req)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	result, err := h.service.UpsertScheduleConfig(r.Context(), feedconfigapp.UpsertScheduleConfigInput{
		TenantID:              tenantID,
		ActorRef:              h.actor(r),
		ParkID:                req.ParkID,
		Workflow:              req.Workflow,
		DirectionTime:         req.DirectionTime,
		CorrectionTime:        req.CorrectionTime,
		TransportTime:         req.TransportTime,
		TransportTimeProvided: req.TransportTime != nil,
		IdempotencyKey:        key,
		RequestFingerprint:    fingerprint,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, result)
}

// upsertExperimentBatchRequest authors every feed item of ONE pen in one atomic write.
//
// The pen's arm and head count sit at the TOP LEVEL, not on each item: they describe the pen, and
// per-item copies would let one pen carry two arms with the display picking whichever row sorted
// first. Each item carries only what varies -- the feed item and its absolute kg.
//
// AbsoluteKg stays a *json.Number per item for the same absent-vs-zero reason as the single-cell
// write. A field the author cleared must be OMITTED FROM items entirely; sending it with a null kg
// is a rejected request, never an instruction to feed nothing.
type upsertExperimentBatchRequest struct {
	ParkID             string                     `json:"park_id"`
	ShedID             string                     `json:"shed_id"`
	PartitionLabel     string                     `json:"partition_label"`
	ExperimentCategory string                     `json:"experiment_category"`
	HeadCount          *int32                     `json:"head_count"`
	Items              []upsertExperimentBatchRow `json:"items"`
}

type upsertExperimentBatchRow struct {
	FeedItem   string       `json:"feed_item"`
	AbsoluteKg *json.Number `json:"absolute_kg"`
}

func (h *Handler) UpsertExperimentConfigBatch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req upsertExperimentBatchRequest
	if !h.decode(w, r, &req, "UpsertFeedConfigExperimentBatchRequest") {
		return
	}
	req.ParkID = strings.TrimSpace(req.ParkID)
	req.ShedID = strings.TrimSpace(req.ShedID)
	req.PartitionLabel = strings.TrimSpace(req.PartitionLabel)
	req.ExperimentCategory = strings.TrimSpace(req.ExperimentCategory)
	for i := range req.Items {
		req.Items[i].FeedItem = strings.TrimSpace(req.Items[i].FeedItem)
	}

	// Fingerprinted AFTER trimming and over the whole body including the item slice, so a retry of
	// the same enrolment replays and a retry that changed ONE cell's kg is correctly a conflict
	// rather than a silent second authoring.
	fingerprint, err := requestFingerprint(tenantID, upsertExperimentBatchCmd, experimentBatchRoute, req)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}

	cells := make([]feedconfigapp.ExperimentBatchCellInput, 0, len(req.Items))
	for _, item := range req.Items {
		cell := feedconfigapp.ExperimentBatchCellInput{FeedItemLabel: item.FeedItem}
		if item.AbsoluteKg != nil {
			kg := item.AbsoluteKg.String()
			cell.AbsoluteKg = &kg
		}
		cells = append(cells, cell)
	}

	result, err := h.service.UpsertExperimentConfigBatch(r.Context(), feedconfigapp.UpsertExperimentConfigBatchInput{
		TenantID:           tenantID,
		ActorRef:           h.actor(r),
		ParkID:             req.ParkID,
		ShedID:             req.ShedID,
		PartitionLabel:     req.PartitionLabel,
		ExperimentCategory: req.ExperimentCategory,
		HeadCount:          req.HeadCount,
		Cells:              cells,
		IdempotencyKey:     key,
		RequestFingerprint: fingerprint,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, result)
}

// upsertExperimentRequest is the experiment-cell body.
//
// AbsoluteKg is a *json.Number for BOTH halves of the same reason grams_per_head is: the pointer
// keeps a cleared field distinguishable from an authored 0, and json.Number keeps the authored
// decimal exact. The failure mode differs from the ration grid and is arguably worse — a missing
// ration rate BLOCKS the shed loudly, while a missing experiment row silently drops the shed back
// onto the per-head grid and prints a complete-looking sheet with about twice the right quantity.
//
// HeadCount is a *int32 (not *json.Number) because it is a whole population count, and it is a
// pointer so "not recorded" stays distinct from an authored 0. It is INFORMATIONAL: nothing
// multiplies it into absolute_kg.
type upsertExperimentRequest struct {
	ParkID string `json:"park_id"`
	ShedID string `json:"shed_id"`
	// PartitionLabel identifies WHICH PEN of the shed is being authored. A partitioned shed holds
	// one cell per pen, so shed_id + feed_item does not identify a row -- the client echoes back
	// the partition_label it rendered. Absent means the undivided-shed row.
	PartitionLabel     string       `json:"partition_label"`
	FeedItem           string       `json:"feed_item"`
	AbsoluteKg         *json.Number `json:"absolute_kg"`
	HeadCount          *int32       `json:"head_count"`
	ExperimentCategory string       `json:"experiment_category"`
}

func (h *Handler) UpsertExperimentConfig(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req upsertExperimentRequest
	if !h.decode(w, r, &req, "UpsertFeedConfigExperimentRequest") {
		return
	}
	req.ParkID = strings.TrimSpace(req.ParkID)
	req.ShedID = strings.TrimSpace(req.ShedID)
	req.PartitionLabel = strings.TrimSpace(req.PartitionLabel)
	req.FeedItem = strings.TrimSpace(req.FeedItem)
	req.ExperimentCategory = strings.TrimSpace(req.ExperimentCategory)

	fingerprint, err := requestFingerprint(tenantID, upsertExperimentCommand, experimentRoute, req)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	result, err := h.service.UpsertExperimentConfig(r.Context(), feedconfigapp.UpsertExperimentConfigInput{
		TenantID:           tenantID,
		ActorRef:           h.actor(r),
		ParkID:             req.ParkID,
		ShedID:             req.ShedID,
		PartitionLabel:     req.PartitionLabel,
		FeedItemLabel:      req.FeedItem,
		AbsoluteKg:         numberPtr(req.AbsoluteKg),
		HeadCount:          req.HeadCount,
		ExperimentCategory: req.ExperimentCategory,
		IdempotencyKey:     key,
		RequestFingerprint: fingerprint,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, result)
}

// setExperimentShedStatusRequest switches one pen between the experiment workflow and the normal
// per-head ration grid. The legacy type and route names remain for compatibility.
//
// Status is a plain REQUIRED string with no default. It is the field that decides which planner
// feeds this pen, so an absent value cannot be filled in: 'active' would enrol the pen onto
// authored absolute kg and 'retired' would return it to head_count x grams_per_head, and both are
// changes to what its animals eat.
type setExperimentShedStatusRequest struct {
	ParkID string `json:"park_id"`
	ShedID string `json:"shed_id"`
	// PartitionLabel names the PEN being switched -- required for a subdivided shed, blank for an
	// undivided one. It is part of the request fingerprint, so two pens of one shed are two
	// different writes and cannot collapse onto each other as an idempotent replay.
	PartitionLabel string `json:"partition_label"`
	Status         string `json:"status"`
}

func (h *Handler) SetExperimentShedStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req setExperimentShedStatusRequest
	if !h.decode(w, r, &req, "SetFeedConfigExperimentShedStatusRequest") {
		return
	}
	req.ParkID = strings.TrimSpace(req.ParkID)
	req.ShedID = strings.TrimSpace(req.ShedID)
	req.PartitionLabel = strings.TrimSpace(req.PartitionLabel)
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))

	fingerprint, err := requestFingerprint(tenantID, setExperimentShedStatusCmd, experimentShedStatusRoute, req)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	result, err := h.service.SetExperimentShedStatus(r.Context(), feedconfigapp.SetExperimentShedStatusInput{
		TenantID:           tenantID,
		ActorRef:           h.actor(r),
		ParkID:             req.ParkID,
		ShedID:             req.ShedID,
		PartitionLabel:     req.PartitionLabel,
		Status:             req.Status,
		IdempotencyKey:     key,
		RequestFingerprint: fingerprint,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// plumbing
// ---------------------------------------------------------------------------

// errorEnvelope is the FLAT {code, message} shape declared by ErrorEnvelope in
// contracts/openapi/app-api.yaml, and the same shape the sibling feed modules emit.
//
// It was previously nested as {"error":{"code":…,"message":…}}, which no client could read: the
// admin-web parser requires top-level `code` and `message` strings and falls back to a generic
// "Backend service returned 409." when it does not find them. Every backend-authored message on
// this surface -- the field errors naming which authored value was refused, the duplicate feed-item
// name, the future-dated-row conflict -- was therefore discarded before it reached the operator,
// which is precisely what the backend-owns-the-copy rule exists to prevent. The nesting was the
// outlier, so the shape moved to the contract rather than the parser widening to accept both.
type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Field names the offending input when the failure is a field-level validation error, so the UI
	// can attach the message to the right control instead of showing a generic banner.
	Field string `json:"field,omitempty"`
}

func (h *Handler) tenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return "", false
	}
	return tenantID, true
}

// actor identifies who authored the edit, for the ledger's audit trail. It falls back to a
// non-empty marker rather than "" because feed_config_write_log.actor_ref is NOT NULL and blank-
// checked: an unattributable edit should be recorded as unattributable, not rejected outright.
func (h *Handler) actor(r *http.Request) string {
	if actor := httpmiddleware.ActorIDFromContext(r.Context()); actor != "" {
		return actor
	}
	return "unknown-actor"
}

func (h *Handler) idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", nil)
		return "", false
	}
	if len(key) < 8 || len(key) > 200 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters", nil)
		return "", false
	}
	return key, true
}

// decode reads a bounded body and rejects unknown fields. Strict decoding matters for an authoring
// API: a typo'd field name would otherwise be silently dropped and the author would believe they
// had set something they had not.
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dest any, schemaName string) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_body", "could not read request body", err)
		return false
	}
	if len(body) > maxBodyBytes {
		h.writeError(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "request body is too large", nil)
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is required", nil)
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must match "+schemaName, err)
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must contain a single JSON object", nil)
		return false
	}
	return true
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	httpresponse.WriteError(w, r, h.log, status, errorEnvelope{Code: code, Message: message}, cause)
}

// writeServiceError maps the module's errors onto status codes.
//
// The mapping that matters most: a same-key/different-payload replay is 409, NOT 400 and NOT a
// silent success. It tells the client "this key already means something else", which is the only
// answer that lets a retrying UI distinguish its own retry from a genuine collision.
func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var fieldErr *domain.FieldError
	switch {
	case errors.Is(err, ports.ErrIdempotencyConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict",
			"this Idempotency-Key was already used with a different request payload", nil)
	case errors.Is(err, ports.ErrFutureDatedRow):
		h.writeError(w, r, http.StatusConflict, "future_dated_config",
			"the currently-open configuration row takes effect on a later date; resolve it before editing", nil)
	case errors.Is(err, ports.ErrExperimentPenAlreadyConfigured):
		h.writeError(w, r, http.StatusConflict, "experiment_pen_already_configured",
			"this pen already has experiment configuration; refresh and edit its cells instead", nil)
	case errors.Is(err, ports.ErrFeedItemExists):
		// 409, not 400 and not a silent success. The author asked to ADD a name the vocabulary
		// already holds; reporting success would leave them believing there are now two entries when
		// feed_config_norm makes them one, and every rate keyed on that label resolves to the
		// original.
		h.writeError(w, r, http.StatusConflict, "feed_item_exists",
			"a feed item with this name already exists in this tenant", nil)
	case errors.Is(err, ports.ErrParkNotFound):
		h.writeError(w, r, http.StatusNotFound, "park_not_found", "park not found in this tenant", nil)
	case errors.Is(err, ports.ErrShedNotFound):
		h.writeError(w, r, http.StatusNotFound, "shed_not_found", "shed not found in this tenant", nil)
	case errors.Is(err, ports.ErrPartitionNotFound):
		h.writeError(w, r, http.StatusNotFound, "partition_not_found",
			"partition not found in this shed", nil)
	case errors.Is(err, ports.ErrPartitionRequired):
		h.writeError(w, r, http.StatusBadRequest, "partition_required",
			"this shed is divided into partitions, so the partition must be named", nil)
	case errors.Is(err, feedconfigapp.ErrMissingTenant):
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
	case errors.Is(err, feedconfigapp.ErrMissingIdempotencyKey):
		h.writeError(w, r, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required", nil)
	case errors.Is(err, feedconfigapp.ErrInvalidPaging),
		errors.Is(err, feedconfigapp.ErrMissingPark),
		errors.Is(err, feedconfigapp.ErrMissingActor):
		h.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
	case errors.As(err, &fieldErr):
		// Field errors carry the offending input name so the UI can attach the message to it. This is
		// the "validate or reject" surface: the author sees WHICH value was refused and why, rather
		// than a value quietly rewritten to a default they never entered.
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, errorEnvelope{
			Code: "invalid_field", Message: fieldErr.Error(), Field: fieldErr.Field,
		}, nil)
	default:
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
	}
}

// pageParams parses the optional limit/offset. Absent means "use the service default"; present but
// non-numeric is a 400. Range checking belongs to the service, which owns the bounds.
func pageParams(query url.Values) (*int32, *int32, error) {
	limit, err := optionalInt32(query, "limit")
	if err != nil {
		return nil, nil, err
	}
	offset, err := optionalInt32(query, "offset")
	if err != nil {
		return nil, nil, err
	}
	return limit, offset, nil
}

func optionalInt32(query url.Values, name string) (*int32, error) {
	raw := strings.TrimSpace(query.Get(name))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	v := int32(parsed)
	return &v, nil
}

// numberPtr converts the decoded *json.Number into the *string the service expects, preserving the
// author's exact decimal text and the absent-vs-zero distinction.
func numberPtr(n *json.Number) *string {
	if n == nil {
		return nil
	}
	s := n.String()
	return &s
}

// requestFingerprint hashes the canonical CLIENT request. Server-derived values (business date,
// actor, timestamps) are deliberately excluded -- see the package comment.
func requestFingerprint(tenantID, command, route string, body any) (string, error) {
	canonical, err := json.Marshal(struct {
		TenantID string `json:"tenant_id"`
		Command  string `json:"command"`
		Route    string `json:"route"`
		Body     any    `json:"body"`
	}{TenantID: tenantID, Command: command, Route: route, Body: body})
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("feedconfig-write"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil)), nil
}

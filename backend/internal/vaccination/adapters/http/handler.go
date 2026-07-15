// Package http exposes the vaccination module's read/preview API.
package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	vaccports "github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

// Reads is the slice of the vaccination service this handler needs.
type Reads interface {
	ImpactPreview(ctx context.Context, req domain.ImpactRequest) (domain.ImpactPreview, error)
	VerificationQueue(ctx context.Context, tenantID, parkID string, cursor *domain.RecordedCompletionCursor, limit int32) (domain.RecordedCompletionPage, error)
	ListOpenStageReviewItems(ctx context.Context, tenantID string, cursor *domain.StageReviewItemCursor, limit int) (domain.StageReviewItemPage, error)
	ResolveStageReviewItem(ctx context.Context, tenantID, reviewItemID, resolvedBy, note string, resolvedAt time.Time) (bool, error)
}

// ManualCampaignGenerator materializes deliberate manual_campaign schedule rows for a published
// vaccination protocol version. It is separate from publish/backfill generation so campaigns cannot
// fire accidentally.
type ManualCampaignGenerator interface {
	GenerateManualCampaignForVersionWithHTTPRun(ctx context.Context, tenantID, versionID, campaignID string, asOf time.Time, idempotencyKey, requestHash string) (domain.GenerationRun, domain.GenerateResult, error)
}

// Handler serves vaccination endpoints.
type Handler struct {
	svc      Reads
	campaign ManualCampaignGenerator
	log      *slog.Logger
}

// NewHandler constructs the handler. The second parameter is retained for the existing bootstrap
// signature; public completion review is intentionally not mounted here. Review must go through SOP
// task verify/rework routes with row-version concurrency.
func NewHandler(svc Reads, _ any, log ...*slog.Logger) *Handler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Handler{svc: svc, log: l}
}

// WithManualCampaignGenerator enables the manual campaign command route.
func (h *Handler) WithManualCampaignGenerator(gen ManualCampaignGenerator) *Handler {
	h.campaign = gen
	return h
}

// Register mounts the vaccination routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /protocols/vaccination/impact-preview", h.ImpactPreview)
	mux.HandleFunc("POST /vaccination/manual-campaigns", h.RunManualCampaign)
	mux.HandleFunc("GET /vaccination/verification-queue", h.VerificationQueue)
	mux.HandleFunc("GET /admin/vaccination/stage-review-items", h.ListStageReviewItems)
	mux.HandleFunc("POST /admin/vaccination/stage-review-items/{review_item_id}/resolve", h.ResolveStageReviewItem)
}

const (
	defaultQueueLimit = 100
	maxQueueLimit     = 500
)

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

// impactPreviewRequest is the API body: the eligibility filter + the number of selected dose rows. All
// optional; empty filter dims mean "any". Preview numbers are aggregate-only and read from the
// eligibility rollup read model, so there is no per-goat dose math (no doses_per_goat) and no
// time-relative warmup window on this path.
type impactPreviewRequest struct {
	Species       string  `json:"species"`
	Stage         string  `json:"stage"`
	Sex           string  `json:"sex"`
	Breed         string  `json:"breed"`
	Health        string  `json:"health"`
	ParkID        *string `json:"park_id"`
	VaccineItemID *string `json:"vaccine_item_id"`
	LocationID    *string `json:"location_id"`
	DoseRows      int32   `json:"dose_rows"`
	DailyCap      int64   `json:"daily_cap"`
	MaxBufferDays *int64  `json:"max_buffer_days"`
	HorizonDays   int     `json:"horizon_days"`
}

type impactPreviewResponse struct {
	EligibleAnimals  int64                         `json:"eligible_animals"`
	VaccinationCells int64                         `json:"vaccination_cells"`
	AffectedSheds    int64                         `json:"affected_sheds"`
	EstimatedDays    int64                         `json:"estimated_days"`
	DailyCap         int64                         `json:"daily_cap"`
	CapacityStatus   string                        `json:"capacity_status,omitempty"`
	PlannedSessions  []domain.ImpactPlannedSession `json:"planned_sessions"`
	DosesAvailable   string                        `json:"doses_available,omitempty"`
	EarliestExpiry   *time.Time                    `json:"earliest_expiry,omitempty"`
	SourceRevision   int64                         `json:"source_revision"`
	RecomputedAt     *time.Time                    `json:"recomputed_at,omitempty"`
	Warnings         []string                      `json:"warnings"`
}

type manualCampaignRequest struct {
	ProtocolVersionID string     `json:"protocol_version_id"`
	CampaignID        string     `json:"campaign_id"`
	AsOf              *time.Time `json:"as_of,omitempty"`
}

type manualCampaignRunResponse struct {
	RunID                            string     `json:"run_id"`
	ProtocolVersionID                string     `json:"protocol_version_id"`
	TriggerType                      string     `json:"trigger_type"`
	TriggerRef                       string     `json:"trigger_ref"`
	Status                           string     `json:"status"`
	StartedAt                        time.Time  `json:"started_at"`
	CompletedAt                      *time.Time `json:"completed_at,omitempty"`
	Generated                        int        `json:"generated"`
	Deferred                         int        `json:"deferred"`
	Reopened                         int        `json:"reopened"`
	FailedGoats                      int        `json:"failed_goats"`
	SkippedNoDueDate                 int        `json:"skipped_no_due_date"`
	SuppressedByTrustedHistory       int        `json:"suppressed_by_trusted_history"`
	CursorGoatID                     string     `json:"cursor_goat_id,omitempty"`
	LastError                        string     `json:"last_error,omitempty"`
	ResultGenerated                  int        `json:"result_generated"`
	ResultDeferred                   int        `json:"result_deferred"`
	ResultReopened                   int        `json:"result_reopened"`
	ResultFailedGoats                int        `json:"result_failed_goats"`
	ResultSkippedNoDueDate           int        `json:"result_skipped_no_due_date"`
	ResultSuppressedByTrustedHistory int        `json:"result_suppressed_by_trusted_history"`
}

// RunManualCampaign deliberately fires manual_campaign rows for a published vaccination protocol
// version. The durable run key combines campaign_id and as_of, while the HTTP Idempotency-Key gives
// operators a retry-safe command boundary.
func (h *Handler) RunManualCampaign(w http.ResponseWriter, r *http.Request) {
	if h.campaign == nil {
		httpresponse.WriteError(w, r, h.log, http.StatusServiceUnavailable,
			errorEnvelope{Code: "manual_campaign_unavailable", Message: "manual campaign generation is not wired", TraceID: traceID(r)}, nil)
		return
	}
	idempotencyKey, ok := h.manualCampaignIdempotencyKey(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.badRequest(w, r, "invalid_body", "request body could not be read")
		return
	}
	var req manualCampaignRequest
	if len(body) == 0 {
		h.badRequest(w, r, "invalid_json", "request body is required")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, r, "invalid_json", "request body is not valid JSON")
		return
	}
	if !uuidutil.IsUUIDString(req.ProtocolVersionID) {
		h.badRequest(w, r, "invalid_protocol_version_id", "protocol_version_id must be a UUID")
		return
	}
	req.CampaignID = strings.TrimSpace(req.CampaignID)
	if !validCampaignID(req.CampaignID) {
		h.badRequest(w, r, "invalid_campaign_id", "campaign_id must be 3-128 characters using letters, numbers, dash, underscore, colon, or dot")
		return
	}
	now := time.Now().In(biztime.DefaultLocation())
	asOf := now
	asOfProvided := req.AsOf != nil
	if req.AsOf != nil {
		asOf = req.AsOf.In(biztime.DefaultLocation())
		if asOf.After(now) {
			h.badRequest(w, r, "future_as_of", "as_of cannot be in the future for a mutating manual campaign")
			return
		}
	}
	requestHash := manualCampaignRequestHash(req, asOfProvided)
	run, result, err := h.campaign.GenerateManualCampaignForVersionWithHTTPRun(r.Context(), tenantID(r), req.ProtocolVersionID, req.CampaignID, asOf, idempotencyKey, requestHash)
	if err != nil {
		if errors.Is(err, domain.ErrFutureManualCampaign) {
			h.badRequest(w, r, "future_as_of", "as_of cannot be in the future for a mutating manual campaign")
			return
		}
		if errors.Is(err, vaccports.ErrIdempotencyConflict) {
			h.conflict(w, r, "idempotency_conflict", "Idempotency-Key was reused with a different manual campaign request")
			return
		}
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusAccepted, manualCampaignRunResponse{
		RunID:                            run.RunID,
		ProtocolVersionID:                run.ProtocolVersionID,
		TriggerType:                      run.TriggerType,
		TriggerRef:                       run.TriggerRef,
		Status:                           run.Status,
		StartedAt:                        run.StartedAt,
		CompletedAt:                      run.CompletedAt,
		Generated:                        run.Generated,
		Deferred:                         run.Deferred,
		Reopened:                         run.Reopened,
		FailedGoats:                      run.FailedGoats,
		SkippedNoDueDate:                 run.SkippedNoDueDate,
		SuppressedByTrustedHistory:       run.SuppressedByTrustedHistory,
		CursorGoatID:                     run.CursorGoatID,
		LastError:                        run.LastError,
		ResultGenerated:                  result.Generated,
		ResultDeferred:                   result.Deferred,
		ResultReopened:                   result.Reopened,
		ResultFailedGoats:                result.FailedGoats,
		ResultSkippedNoDueDate:           result.SkippedNoDueDate,
		ResultSuppressedByTrustedHistory: result.SuppressedByTrustedHistory,
	})
}

// ImpactPreview computes the AGGREGATE config impact preview for a vaccination rule/version: eligible
// animals, vaccination cells, affected sheds, and estimated days at the configured daily cap. Numbers
// come from the vaccination_eligibility_rollups read model — this request path never scans goats — so
// the "Preview impact" button stays cheap across the current 5,000-50,000-animal release envelope (up
// to the ~500k obligation-row upper bound; 1-5M is the future certification bar, not a present
// requirement — see docs/decisions/operational-kernel-5k-50k-scale-envelope.md). An optional cheap
// stock check is added only when a vaccine item is set.
func (h *Handler) ImpactPreview(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.badRequest(w, r, "invalid_body", "request body could not be read")
		return
	}
	var req impactPreviewRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			h.badRequest(w, r, "invalid_json", "request body is not valid JSON")
			return
		}
	}
	preview, err := h.svc.ImpactPreview(r.Context(), domain.ImpactRequest{
		Filter: domain.ImpactFilter{
			TenantID: tenantID(r),
			Species:  req.Species,
			Stage:    req.Stage,
			Sex:      req.Sex,
			Breed:    req.Breed,
			Health:   req.Health,
			ParkID:   req.ParkID,
		},
		VaccineItemID: req.VaccineItemID,
		LocationID:    req.LocationID,
		DoseRows:      req.DoseRows,
		DailyCap:      req.DailyCap,
		MaxBufferDays: req.MaxBufferDays,
		HorizonDays:   req.HorizonDays,
	})
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
		return
	}
	warnings := preview.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	plannedSessions := preview.PlannedSessions
	if plannedSessions == nil {
		plannedSessions = []domain.ImpactPlannedSession{}
	}
	httpresponse.WriteJSON(w, http.StatusOK, impactPreviewResponse{
		EligibleAnimals:  preview.EligibleAnimals,
		VaccinationCells: preview.VaccinationCells,
		AffectedSheds:    preview.AffectedSheds,
		EstimatedDays:    preview.EstimatedDays,
		DailyCap:         preview.DailyCap,
		CapacityStatus:   preview.CapacityStatus,
		PlannedSessions:  plannedSessions,
		DosesAvailable:   preview.DosesAvailable,
		EarliestExpiry:   preview.EarliestExpiry,
		SourceRevision:   preview.SourceRevision,
		RecomputedAt:     preview.RecomputedAt,
		Warnings:         warnings,
	})
}

type queueItem struct {
	CompletionID   string    `json:"completion_id"`
	ObligationID   string    `json:"obligation_id"`
	GoatID         string    `json:"goat_id"`
	BatchID        string    `json:"batch_id,omitempty"`
	SOPTaskID      string    `json:"sop_task_id,omitempty"`
	SOPTaskVersion int32     `json:"sop_task_row_version,omitempty"`
	AdministeredAt time.Time `json:"administered_at"`
	Doses          int32     `json:"doses"`
	RouteSite      string    `json:"route_site,omitempty"`
	WorkState      string    `json:"work_state"`
}

type queueResponse struct {
	Items      []queueItem `json:"items"`
	TotalCount int64       `json:"total_count"`
	NextCursor *string     `json:"next_cursor,omitempty"`
}

// VerificationQueue lists completions awaiting review (earliest administered first), bounded by limit
// (default 100, max 500).
func (h *Handler) VerificationQueue(w http.ResponseWriter, r *http.Request) {
	limit := int32(defaultQueueLimit)
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > maxQueueLimit {
			n = maxQueueLimit
		}
		limit = int32(n)
	}
	parkID := r.URL.Query().Get("park_id")
	if parkID != "" && !uuidutil.IsUUIDString(parkID) {
		h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
		return
	}
	var cursor *domain.RecordedCompletionCursor
	if value := strings.TrimSpace(r.URL.Query().Get("cursor")); value != "" {
		decoded, err := domain.DecodeRecordedCompletionCursor(value)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor must be an opaque queue cursor returned by the previous page")
			return
		}
		cursor = &decoded
	}
	page, err := h.svc.VerificationQueue(r.Context(), tenantID(r), parkID, cursor, limit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
		return
	}
	items := make([]queueItem, 0, len(page.Items))
	for _, c := range page.Items {
		items = append(items, queueItem{
			CompletionID:   c.CompletionID,
			ObligationID:   c.ObligationID,
			GoatID:         c.GoatID,
			BatchID:        c.BatchID,
			SOPTaskID:      c.SOPTaskID,
			SOPTaskVersion: c.SOPTaskVersion,
			AdministeredAt: c.AdministeredAt,
			Doses:          c.Doses,
			RouteSite:      c.RouteSite,
			WorkState:      "verification_pending",
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, queueResponse{Items: items, TotalCount: page.TotalCount, NextCursor: page.NextCursor})
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, msg string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		errorEnvelope{Code: code, Message: msg, TraceID: traceID(r)}, nil)
}

type stageReviewListResponse struct {
	Items      []domain.StageReviewItem `json:"items"`
	NextCursor *string                  `json:"nextCursor,omitempty"`
}

// ListStageReviewItems returns open vaccination stage/age review items for the tenant so operators
// can discover the animals whose stale K1/K2 tag needs reconciling (VACC-REV-10).
// Supports cursor pagination (VACC-REV-10B) for >200 items without capping access to older work.
func (h *Handler) ListStageReviewItems(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}
	var cursor *domain.StageReviewItemCursor
	if v := strings.TrimSpace(r.URL.Query().Get("cursor")); v != "" {
		decoded, err := domain.DecodeStageReviewItemCursor(v)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor is malformed or invalid")
			return
		}
		cursor = &decoded
	}
	page, err := h.svc.ListOpenStageReviewItems(r.Context(), tenantID(r), cursor, limit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
		return
	}
	if page.Items == nil {
		page.Items = []domain.StageReviewItem{}
	}
	httpresponse.WriteJSON(w, http.StatusOK, stageReviewListResponse{Items: page.Items, NextCursor: page.NextCursor})
}

type resolveStageReviewRequest struct {
	Note string `json:"note"`
}

// ResolveStageReviewItem marks an open stage/age review item resolved (VACC-REV-10). Idempotent: a
// replay on an already-resolved (or missing) item returns 404 without changing state.
func (h *Handler) ResolveStageReviewItem(w http.ResponseWriter, r *http.Request) {
	reviewItemID := r.PathValue("review_item_id")
	if !uuidutil.IsUUIDString(reviewItemID) {
		h.badRequest(w, r, "invalid_review_item_id", "review_item_id must be a UUID")
		return
	}
	var req resolveStageReviewRequest
	if r.Body != nil {
		dec := json.NewDecoder(io.LimitReader(r.Body, 8*1024))
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			h.badRequest(w, r, "invalid_body", "request body must be JSON")
			return
		}
	}
	resolved, err := h.svc.ResolveStageReviewItem(r.Context(), tenantID(r), reviewItemID,
		httpmiddleware.ActorIDFromContext(r.Context()), strings.TrimSpace(req.Note), time.Now().In(biztime.DefaultLocation()))
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
		return
	}
	if !resolved {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_open", Message: "review item not found or already resolved", TraceID: traceID(r)}, nil)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]string{"review_item_id": reviewItemID, "status": "resolved"})
}

func validCampaignID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '-', '_', ':', '.':
			continue
		default:
			return false
		}
	}
	return true
}

func manualCampaignRequestHash(req manualCampaignRequest, asOfProvided bool) string {
	asOf := ""
	if req.AsOf != nil {
		asOf = req.AsOf.UTC().Format(time.RFC3339Nano)
	}
	raw, _ := json.Marshal(struct {
		ProtocolVersionID string `json:"protocol_version_id"`
		CampaignID        string `json:"campaign_id"`
		AsOf              string `json:"as_of"`
		AsOfProvided      bool   `json:"as_of_provided"`
	}{
		ProtocolVersionID: req.ProtocolVersionID,
		CampaignID:        req.CampaignID,
		AsOf:              asOf,
		AsOfProvided:      asOfProvided,
	})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (h *Handler) manualCampaignIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.badRequest(w, r, "missing_idempotency_key", "Idempotency-Key header is required")
		return "", false
	}
	if len(key) < 8 || len(key) > 200 {
		h.badRequest(w, r, "invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
		return "", false
	}
	return key, true
}

func (h *Handler) conflict(w http.ResponseWriter, r *http.Request, code, message string) {
	httpresponse.WriteError(w, r, h.log, http.StatusConflict,
		errorEnvelope{Code: code, Message: message, TraceID: traceID(r)}, nil)
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}

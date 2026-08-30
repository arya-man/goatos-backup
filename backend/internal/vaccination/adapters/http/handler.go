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
	ShedCompletionSummary(ctx context.Context, tenantID, taskID, shedID string, partitionLabel ...string) (domain.ShedCompletionSummary, error)
}

// ManualCampaignGenerator materializes deliberate manual_campaign schedule rows for a published
// vaccination protocol version. It is separate from publish/backfill generation so campaigns cannot
// fire accidentally.
type ManualCampaignGenerator interface {
	GenerateManualCampaignForVersionWithHTTPRun(ctx context.Context, tenantID, versionID, campaignID string, asOf time.Time, idempotencyKey, requestHash string) (domain.GenerationRun, domain.GenerateResult, error)
}

type AnchorManager interface {
	PreviewAnchor(ctx context.Context, in domain.AnchorCommand) (domain.AnchorPreview, error)
	CreateAnchor(ctx context.Context, in domain.AnchorCommand) (domain.AnchorPreview, error)
}

// Handler serves vaccination endpoints.
type Handler struct {
	svc      Reads
	campaign ManualCampaignGenerator
	anchors  AnchorManager
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

func (h *Handler) WithAnchorManager(manager AnchorManager) *Handler {
	h.anchors = manager
	return h
}

// Register mounts the vaccination routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /protocols/vaccination/impact-preview", h.ImpactPreview)
	mux.HandleFunc("POST /vaccination/manual-campaigns", h.RunManualCampaign)
	mux.HandleFunc("POST /vaccination/anchors/preview", h.PreviewAnchor)
	mux.HandleFunc("POST /vaccination/anchors", h.CreateAnchor)
	mux.HandleFunc("GET /vaccination/verification-queue", h.VerificationQueue)
	mux.HandleFunc("GET /app/tasks/{task_id}/shed-completion-summary", h.ShedCompletionSummary)
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

type anchorRequest struct {
	VaccineCode           string          `json:"vaccine_code"`
	DoseCode              string          `json:"dose_code,omitempty"`
	AnchorDate            string          `json:"anchor_date"`
	ScopeType             string          `json:"scope_type"`
	ScopePayload          json.RawMessage `json:"scope_payload"`
	Reason                string          `json:"reason"`
	SourceRef             string          `json:"source_ref,omitempty"`
	SuppressBeforeAnchor  *bool           `json:"suppress_before_anchor,omitempty"`
	ChainFutureFromAnchor *bool           `json:"chain_future_from_anchor,omitempty"`
	EnforceAgeEligibility *bool           `json:"enforce_age_eligibility,omitempty"`
}

func (h *Handler) PreviewAnchor(w http.ResponseWriter, r *http.Request) {
	h.handleAnchor(w, r, false)
}

func (h *Handler) CreateAnchor(w http.ResponseWriter, r *http.Request) {
	h.handleAnchor(w, r, true)
}

func (h *Handler) handleAnchor(w http.ResponseWriter, r *http.Request, apply bool) {
	if h.anchors == nil {
		httpresponse.WriteError(w, r, h.log, http.StatusServiceUnavailable,
			errorEnvelope{Code: "anchor_unavailable", Message: "vaccination anchor creation is not wired", TraceID: traceID(r)}, nil)
		return
	}
	var idempotencyKey string
	if apply {
		var ok bool
		idempotencyKey, ok = h.manualCampaignIdempotencyKey(w, r)
		if !ok {
			return
		}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.badRequest(w, r, "invalid_body", "request body could not be read")
		return
	}
	if len(body) == 0 {
		h.badRequest(w, r, "invalid_json", "request body is required")
		return
	}
	var req anchorRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, r, "invalid_json", "request body is not valid JSON")
		return
	}
	anchorDate, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(req.AnchorDate), biztime.DefaultLocation())
	if err != nil {
		h.badRequest(w, r, "invalid_anchor_date", "anchor_date must be YYYY-MM-DD")
		return
	}
	if len(req.ScopePayload) == 0 {
		req.ScopePayload = json.RawMessage(`{}`)
	}
	suppress := boolDefault(req.SuppressBeforeAnchor, true)
	chain := boolDefault(req.ChainFutureFromAnchor, true)
	enforceAge := boolDefault(req.EnforceAgeEligibility, true)
	cmd := domain.AnchorCommand{
		TenantID:              tenantID(r),
		VaccineCode:           strings.TrimSpace(req.VaccineCode),
		DoseCode:              strings.TrimSpace(req.DoseCode),
		AnchorDate:            anchorDate,
		Scope:                 domain.AnchorScope{Type: strings.TrimSpace(req.ScopeType), Payload: req.ScopePayload},
		Reason:                strings.TrimSpace(req.Reason),
		SourceSystem:          "admin-web",
		SourceRef:             strings.TrimSpace(req.SourceRef),
		CreatedBy:             httpmiddleware.ActorIDFromContext(r.Context()),
		SuppressBeforeAnchor:  suppress,
		ChainFutureFromAnchor: chain,
		EnforceAgeEligibility: enforceAge,
		IdempotencyKey:        idempotencyKey,
		RequestHash:           anchorRequestHash(req),
	}
	var preview domain.AnchorPreview
	if apply {
		preview, err = h.anchors.CreateAnchor(r.Context(), cmd)
	} else {
		preview, err = h.anchors.PreviewAnchor(r.Context(), cmd)
	}
	if err != nil {
		if errors.Is(err, domain.ErrInvalidAnchor) {
			h.badRequest(w, r, "invalid_anchor", err.Error())
			return
		}
		if errors.Is(err, vaccports.ErrIdempotencyConflict) {
			h.conflict(w, r, "idempotency_conflict", "Idempotency-Key was reused with a different anchor request")
			return
		}
		h.internal(w, r, err)
		return
	}
	status := http.StatusOK
	if apply {
		status = http.StatusCreated
	}
	httpresponse.WriteJSON(w, status, preview)
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

type shedCompletionVaccineBreakdownItem struct {
	Vaccine string `json:"vaccine"`
	Count   int64  `json:"count"`
}

// shedCompletionSummaryResponse is the FROZEN wire shape for the vaccination shed-completion /
// submit read-only summary. Shed completion is an acknowledgement, not a manual form: field names
// below are exact per the frozen contract and must not change without updating every consumer
// (Android SubmitScreen, admin-web record/verify drawer, OpenAPI spec + generated api-client).
type shedCompletionSummaryResponse struct {
	TaskID           string                               `json:"task_id"`
	ShedName         string                               `json:"shed_name"`
	DriveName        string                               `json:"drive_name"`
	ExpectedCount    int64                                `json:"expected_count"`
	HandledCount     int64                                `json:"handled_count"`
	ProofReadyCount  int64                                `json:"proof_ready_count"`
	ProofMode        string                               `json:"proof_mode"`
	VaccineBreakdown []shedCompletionVaccineBreakdownItem `json:"vaccine_breakdown"`
	SubmitEnabled    bool                                 `json:"submit_enabled"`
	BlockingReason   *string                              `json:"blocking_reason"`
	SubmitState      string                               `json:"submit_state"`
	// RoundSubmitted is true only when a live/accepted submission trail exists for THIS shed's
	// CURRENT round of eligible obligations. See domain.ShedCompletionSummary.RoundSubmitted.
	RoundSubmitted bool `json:"round_submitted"`
	// RoundID is a deterministic fingerprint of this shed's current obligation-round state; it
	// changes value whenever an obligation in this shed submits or is reopened by a verifier
	// rejection. See domain.ShedCompletionSummary.RoundID. Opaque to clients -- not a UUID, not
	// stable across schema changes -- carried for round-identity comparisons only.
	RoundID string `json:"round_id"`
}

// ShedCompletionSummary serves GET /app/tasks/{task_id}/shed-completion-summary — the read-only
// acknowledgement summary the operator sees on the shed-completion/submit screen. It never reads
// submitted form answers; readiness comes exclusively from scan + proof + obligation state.
func (h *Handler) ShedCompletionSummary(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("task_id")
	if !uuidutil.IsUUIDString(taskID) {
		h.badRequest(w, r, "invalid_task_id", "task_id must be a UUID")
		return
	}
	shedID := strings.TrimSpace(r.URL.Query().Get("shed_id"))
	if shedID == "" {
		shedID = strings.TrimSpace(r.URL.Query().Get("shedId"))
	}
	if shedID != "" && !uuidutil.IsUUIDString(shedID) {
		h.badRequest(w, r, "invalid_shed_id", "shed_id must be a UUID")
		return
	}
	partitionLabel := strings.TrimSpace(r.URL.Query().Get("partition_label"))
	if partitionLabel == "" {
		partitionLabel = strings.TrimSpace(r.URL.Query().Get("partitionLabel"))
	}
	summary, err := h.svc.ShedCompletionSummary(r.Context(), tenantID(r), taskID, shedID, partitionLabel)
	if err != nil {
		if errors.Is(err, vaccports.ErrNotFound) {
			httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
				errorEnvelope{Code: "task_not_found", Message: "vaccination task not found", TraceID: traceID(r)}, err)
			return
		}
		h.internal(w, r, err)
		return
	}
	breakdown := make([]shedCompletionVaccineBreakdownItem, 0, len(summary.VaccineBreakdown))
	for _, item := range summary.VaccineBreakdown {
		breakdown = append(breakdown, shedCompletionVaccineBreakdownItem{Vaccine: item.Vaccine, Count: item.Count})
	}
	httpresponse.WriteJSON(w, http.StatusOK, shedCompletionSummaryResponse{
		TaskID:           summary.TaskID,
		ShedName:         summary.ShedName,
		DriveName:        summary.DriveName,
		ExpectedCount:    summary.ExpectedCount,
		HandledCount:     summary.HandledCount,
		ProofReadyCount:  summary.ProofReadyCount,
		ProofMode:        summary.ProofMode,
		VaccineBreakdown: breakdown,
		SubmitEnabled:    summary.SubmitEnabled,
		BlockingReason:   summary.BlockingReason,
		SubmitState:      summary.SubmitState,
		RoundSubmitted:   summary.RoundSubmitted,
		RoundID:          summary.RoundID,
	})
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
	// projection-review: the queue is park-scoped by the caller's GRANTS, not by an optional
	// client filter. It previously defaulted to tenant-wide, so a park-bound park head who omitted
	// park_id reviewed the other park's completions; park_id may now only narrow inside the grant.
	requestedPark := r.URL.Query().Get("park_id")
	if requestedPark != "" && !uuidutil.IsUUIDString(requestedPark) {
		h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
		return
	}
	parkID, ok := h.authorizedParkID(w, r, requestedPark)
	if !ok {
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

func anchorRequestHash(req anchorRequest) string {
	raw, _ := json.Marshal(struct {
		VaccineCode           string          `json:"vaccine_code"`
		DoseCode              string          `json:"dose_code"`
		AnchorDate            string          `json:"anchor_date"`
		ScopeType             string          `json:"scope_type"`
		ScopePayload          json.RawMessage `json:"scope_payload"`
		Reason                string          `json:"reason"`
		SourceRef             string          `json:"source_ref"`
		SuppressBeforeAnchor  bool            `json:"suppress_before_anchor"`
		ChainFutureFromAnchor bool            `json:"chain_future_from_anchor"`
		EnforceAgeEligibility bool            `json:"enforce_age_eligibility"`
	}{
		VaccineCode:           strings.TrimSpace(req.VaccineCode),
		DoseCode:              strings.TrimSpace(req.DoseCode),
		AnchorDate:            strings.TrimSpace(req.AnchorDate),
		ScopeType:             strings.TrimSpace(req.ScopeType),
		ScopePayload:          req.ScopePayload,
		Reason:                strings.TrimSpace(req.Reason),
		SourceRef:             strings.TrimSpace(req.SourceRef),
		SuppressBeforeAnchor:  boolDefault(req.SuppressBeforeAnchor, true),
		ChainFutureFromAnchor: boolDefault(req.ChainFutureFromAnchor, true),
		EnforceAgeEligibility: boolDefault(req.EnforceAgeEligibility, true),
	})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func boolDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
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

// authorizedParkID clamps a requested park to the caller's grant scope, mirroring the
// vaccination-execution handler: a tenant-wide (or grant-less internal) caller keeps the
// verbatim request, a park-scoped caller defaults to their own park and is refused 403 for any
// other. Writes the error response itself and reports ok=false when access is denied.
func (h *Handler) authorizedParkID(w http.ResponseWriter, r *http.Request, requested string) (string, bool) {
	decision := httpmiddleware.ResolveAuthorizedParkScope(r.Context(), tenantID(r), requested)
	if decision.Allowed {
		return decision.ParkID, true
	}
	httpresponse.WriteError(w, r, h.log, decision.Status,
		errorEnvelope{Code: decision.Code, Message: decision.Message, TraceID: traceID(r)}, nil)
	return "", false
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

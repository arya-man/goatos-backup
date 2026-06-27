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

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	app "github.com/vgoats/goatos/backend/internal/vaccination/app"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	vaccports "github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

// Reads is the slice of the vaccination service this handler needs.
type Reads interface {
	ImpactPreview(ctx context.Context, req domain.ImpactRequest) (domain.ImpactPreview, error)
	VerificationQueue(ctx context.Context, tenantID, parkID string, limit int32) ([]domain.RecordedCompletion, error)
}

// Verifier is the SM-5 verification slice (CompletionService): accept/reject a recorded completion.
type Verifier interface {
	AcceptExisting(ctx context.Context, in app.AcceptExistingInput) (app.AcceptResult, error)
	RejectExisting(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (app.RejectResult, error)
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
	verify   Verifier
	campaign ManualCampaignGenerator
	log      *slog.Logger
}

// NewHandler constructs the handler. verify may be nil (verification routes 503 until wired).
func NewHandler(svc Reads, verify Verifier, log ...*slog.Logger) *Handler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Handler{svc: svc, verify: verify, log: l}
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
	mux.HandleFunc("POST /vaccination/completions/{completion_id}/accept", h.AcceptCompletion)
	mux.HandleFunc("POST /vaccination/completions/{completion_id}/reject", h.RejectCompletion)
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

// impactPreviewRequest is the API body: the eligibility filter + drive math inputs. All optional;
// empty filter dims mean "any".
type impactPreviewRequest struct {
	Stage         string  `json:"stage"`
	Sex           string  `json:"sex"`
	Breed         string  `json:"breed"`
	ParkID        *string `json:"park_id"`
	VaccineItemID *string `json:"vaccine_item_id"`
	LocationID    *string `json:"location_id"`
	DosesPerGoat  int32   `json:"doses_per_goat"`
	DoseRows      int32   `json:"dose_rows"`
	HorizonDays   int     `json:"horizon_days"`
}

type impactPreviewResponse struct {
	EligibleGoats  int64      `json:"eligible_goats"`
	CatchupGoats   int64      `json:"catchup_goats"`
	Obligations    int64      `json:"obligations"`
	Batches        int64      `json:"batches"`
	DosesRequired  int64      `json:"doses_required"`
	DosesAvailable string     `json:"doses_available"`
	EarliestExpiry *time.Time `json:"earliest_expiry,omitempty"`
	Warnings       []string   `json:"warnings"`
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
	SkippedNoDueDate                 int        `json:"skipped_no_due_date"`
	SuppressedByTrustedHistory       int        `json:"suppressed_by_trusted_history"`
	CursorGoatID                     string     `json:"cursor_goat_id,omitempty"`
	LastError                        string     `json:"last_error,omitempty"`
	ResultGenerated                  int        `json:"result_generated"`
	ResultDeferred                   int        `json:"result_deferred"`
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
	asOf := time.Now().UTC()
	asOfProvided := req.AsOf != nil
	if req.AsOf != nil {
		asOf = req.AsOf.UTC()
	}
	requestHash := manualCampaignRequestHash(req, asOfProvided)
	run, result, err := h.campaign.GenerateManualCampaignForVersionWithHTTPRun(r.Context(), tenantID(r), req.ProtocolVersionID, req.CampaignID, asOf, idempotencyKey, requestHash)
	if err != nil {
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
		SkippedNoDueDate:                 run.SkippedNoDueDate,
		SuppressedByTrustedHistory:       run.SuppressedByTrustedHistory,
		CursorGoatID:                     run.CursorGoatID,
		LastError:                        run.LastError,
		ResultGenerated:                  result.Generated,
		ResultDeferred:                   result.Deferred,
		ResultSkippedNoDueDate:           result.SkippedNoDueDate,
		ResultSuppressedByTrustedHistory: result.SuppressedByTrustedHistory,
	})
}

// ImpactPreview computes a live impact preview for a vaccination rule/version (eligible goats,
// catch-up, obligations, drive batches, doses required vs available, stock warnings).
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
			Stage:    req.Stage,
			Sex:      req.Sex,
			Breed:    req.Breed,
			ParkID:   req.ParkID,
		},
		VaccineItemID: req.VaccineItemID,
		LocationID:    req.LocationID,
		DosesPerGoat:  req.DosesPerGoat,
		DoseRows:      req.DoseRows,
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
	httpresponse.WriteJSON(w, http.StatusOK, impactPreviewResponse{
		EligibleGoats:  preview.EligibleGoats,
		CatchupGoats:   preview.CatchupGoats,
		Obligations:    preview.Obligations,
		Batches:        preview.Batches,
		DosesRequired:  preview.DosesRequired,
		DosesAvailable: preview.DosesAvailable,
		EarliestExpiry: preview.EarliestExpiry,
		Warnings:       warnings,
	})
}

type queueItem struct {
	CompletionID   string    `json:"completion_id"`
	ObligationID   string    `json:"obligation_id"`
	GoatID         string    `json:"goat_id"`
	BatchID        string    `json:"batch_id,omitempty"`
	AdministeredAt time.Time `json:"administered_at"`
	Doses          int32     `json:"doses"`
	RouteSite      string    `json:"route_site,omitempty"`
	WorkState      string    `json:"work_state"`
}

type queueResponse struct {
	Items []queueItem `json:"items"`
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
	rows, err := h.svc.VerificationQueue(r.Context(), tenantID(r), parkID, limit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
		return
	}
	items := make([]queueItem, 0, len(rows))
	for _, c := range rows {
		items = append(items, queueItem{
			CompletionID:   c.CompletionID,
			ObligationID:   c.ObligationID,
			GoatID:         c.GoatID,
			BatchID:        c.BatchID,
			AdministeredAt: c.AdministeredAt,
			Doses:          c.Doses,
			RouteSite:      c.RouteSite,
			WorkState:      "verification_pending",
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, queueResponse{Items: items})
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

// AcceptCompletion verifies (accepts) a recorded completion: completes its obligation + consumes the
// reserved dose. Idempotent — a completion no longer 'recorded' is a no-op.
func (h *Handler) AcceptCompletion(w http.ResponseWriter, r *http.Request) {
	if h.verify == nil {
		h.unavailable(w, r)
		return
	}
	res, err := h.verify.AcceptExisting(r.Context(), app.AcceptExistingInput{
		TenantID:     tenantID(r),
		CompletionID: r.PathValue("completion_id"),
		VerifiedBy:   actorPtr(r),
	})
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]bool{"applied": res.Applied, "completed": res.Completed})
}

// RejectCompletion rejects (or requests rework on) a recorded completion: the obligation stays open.
// The reason distinguishes a plain reject from a rework request. Idempotent.
func (h *Handler) RejectCompletion(w http.ResponseWriter, r *http.Request) {
	if h.verify == nil {
		h.unavailable(w, r)
		return
	}
	var req rejectRequest
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			h.badRequest(w, r, "invalid_json", "request body is not valid JSON")
			return
		}
	}
	res, err := h.verify.RejectExisting(r.Context(), tenantID(r), r.PathValue("completion_id"), req.Reason, actorPtr(r))
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]bool{"applied": res.Applied})
}

func (h *Handler) unavailable(w http.ResponseWriter, r *http.Request) {
	httpresponse.WriteError(w, r, h.log, http.StatusServiceUnavailable,
		errorEnvelope{Code: "verification_unavailable", Message: "verification is not wired", TraceID: traceID(r)}, nil)
}

func actorPtr(r *http.Request) *string {
	if a := httpmiddleware.ActorIDFromContext(r.Context()); a != "" {
		return &a
	}
	return nil
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

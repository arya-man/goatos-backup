// Package http exposes the vaccination module's read/preview API.
package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	app "github.com/vgoats/goatos/backend/internal/vaccination/app"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// Reads is the slice of the vaccination service this handler needs.
type Reads interface {
	ImpactPreview(ctx context.Context, req domain.ImpactRequest) (domain.ImpactPreview, error)
	VerificationQueue(ctx context.Context, tenantID string, limit int32) ([]domain.RecordedCompletion, error)
}

// Verifier is the SM-5 verification slice (CompletionService): accept/reject a recorded completion.
type Verifier interface {
	AcceptExisting(ctx context.Context, in app.AcceptExistingInput) (app.AcceptResult, error)
	RejectExisting(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (app.RejectResult, error)
}

// Handler serves vaccination endpoints.
type Handler struct {
	svc    Reads
	verify Verifier
	log    *slog.Logger
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

// Register mounts the vaccination routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /protocols/vaccination/impact-preview", h.ImpactPreview)
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
	rows, err := h.svc.VerificationQueue(r.Context(), tenantID(r), limit)
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

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}

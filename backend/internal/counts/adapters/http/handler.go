// Package http exposes the herd register read model API.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type HerdRegisterService interface {
	GetSummary(ctx context.Context, req domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error)
	GetBreakdown(ctx context.Context, req domain.CountsBreakdownQuery) (domain.CountsBreakdown, error)
	GetMilkPreparation(ctx context.Context, req domain.MilkPreparationQuery) (domain.MilkPreparationPage, error)
	SubmitMilkPreparation(ctx context.Context, req domain.MilkPreparationSubmission) (domain.MilkPreparationSubmissionResult, error)
}

type Handler struct {
	service HerdRegisterService
	log     *slog.Logger
}

func NewHandler(service HerdRegisterService, log *slog.Logger) *Handler {
	return &Handler{service: service, log: log}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /herd-register/summary", h.GetSummary)
	mux.HandleFunc("GET /counts/breakdown", h.GetBreakdown)
	mux.HandleFunc("GET /counts/milk-preparation", h.GetMilkPreparation)
	mux.HandleFunc("POST /app/counts/milk-preparation/submit", h.SubmitMilkPreparation)
}

type submitMilkPreparationRequest struct {
	ParkID          string                       `json:"park_id"`
	PreparationDate string                       `json:"preparation_date"`
	GoatMilkUsed    bool                         `json:"goat_milk_used"`
	Proofs          domain.MilkPreparationProofs `json:"proofs"`
}

// SubmitMilkPreparation accepts one park-day attempt. It stores nothing as completed: success is
// 202 pending_verification until the generic verifier approves the complete step-video package.
func (h *Handler) SubmitMilkPreparation(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	actorID := httpmiddleware.ActorIDFromContext(r.Context())
	if tenantID == "" || actorID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, map[string]string{"code": "missing_context", "message": "tenant and actor context are required"}, nil)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 8 || len(key) > 200 {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_idempotency_key", "message": "Idempotency-Key must be between 8 and 200 characters"}, nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body submitMilkPreparationRequest
	if err := decoder.Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_json", "message": "request body must match the milk preparation submission schema"}, nil)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_json", "message": "request body must contain one JSON object"}, nil)
		return
	}
	preparationDate, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(body.PreparationDate), biztime.DefaultLocation())
	if err != nil || strings.TrimSpace(body.ParkID) == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"code": "invalid_scope", "message": "park_id and preparation_date (YYYY-MM-DD) are required"}, nil)
		return
	}
	result, err := h.service.SubmitMilkPreparation(r.Context(), domain.MilkPreparationSubmission{
		TenantID: tenantID, ParkID: strings.TrimSpace(body.ParkID), PreparationDate: preparationDate,
		GoatMilkUsed: body.GoatMilkUsed, Proofs: body.Proofs, SubmittedBy: actorID,
		SubmittedAt: time.Now().UTC(), IdempotencyKey: key,
		TraceID: httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		switch {
		case errors.Is(err, ports.ErrMilkPreparationProofs):
			httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, map[string]string{"code": "proof_required", "message": err.Error()}, nil)
		case errors.Is(err, ports.ErrMilkPreparationInvalidProof):
			httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, map[string]string{"code": "invalid_proof", "message": err.Error()}, nil)
		case errors.Is(err, ports.ErrMilkPreparationPending), errors.Is(err, ports.ErrMilkPreparationCompleted), errors.Is(err, ports.ErrIdempotencyConflict):
			httpresponse.WriteError(w, r, h.log, http.StatusConflict, map[string]string{"code": "state_conflict", "message": err.Error()}, nil)
		default:
			httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, map[string]string{"code": "milk_preparation_submit_failed", "message": "milk preparation submission failed"}, err)
		}
		return
	}
	httpresponse.WriteJSON(w, http.StatusAccepted, result)
}

// GetMilkPreparation serves today's live-herd K1/K2/K3 milk preparation direction.
func (h *Handler) GetMilkPreparation(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	query := r.URL.Query()
	limit, err := boundedIntParam(query, "limit", milkPreparationDefaultLimit, 1, milkPreparationMaxLimit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(query, "offset", 0, 0, milkPreparationMaxOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	page, err := h.service.GetMilkPreparation(r.Context(), domain.MilkPreparationQuery{
		TenantID: tenantID,
		ParkID:   nullableString(query.Get("park_id")),
		Limit:    limit,
		Offset:   offset,
		AsOf:     time.Now(),
	})
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "milk preparation direction", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

// GetSummary serves exact summary counts from the herd register summary projection.
func (h *Handler) GetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}

	// Parse query parameters.
	lifecycle := r.URL.Query().Get("lifecycle_status")
	park := r.URL.Query().Get("park_id")
	breed := r.URL.Query().Get("breed")
	sex := r.URL.Query().Get("sex")

	req := domain.HerdRegisterSummaryQuery{
		TenantID:        tenantID,
		LifecycleStatus: nullableString(lifecycle),
		ParkID:          nullableString(park),
		Breed:           nullableString(breed),
		Sex:             nullableString(sex),
	}

	summary, err := h.service.GetSummary(r.Context(), req)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "herd register summary", err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{
		"items": summary.Items,
	})
}

// GetBreakdown serves the Counts Breakdown census grouped by farm, stage, breed, sex and shed.
func (h *Handler) GetBreakdown(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}

	query := r.URL.Query()

	// A present-but-invalid paging value is rejected, never silently rewritten to a default the
	// caller never asked for. Absent values fall back to the declared defaults.
	limit, err := boundedIntParam(query, "limit", countsBreakdownDefaultLimit, 1, countsBreakdownMaxLimit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(query, "offset", 0, 0, countsBreakdownMaxOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	req := domain.CountsBreakdownQuery{
		TenantID:        tenantID,
		LifecycleStatus: nullableString(query.Get("lifecycle_status")),
		ParkID:          nullableString(query.Get("park_id")),
		ShedID:          nullableString(query.Get("shed_id")),
		ManagementStage: nullableString(query.Get("management_stage")),
		Breed:           nullableString(query.Get("breed")),
		Sex:             nullableString(query.Get("sex")),
		Limit:           limit,
		Offset:          offset,
	}

	breakdown, err := h.service.GetBreakdown(r.Context(), req)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "counts breakdown", err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, breakdown)
}

const (
	countsBreakdownDefaultLimit = 10
	countsBreakdownMaxLimit     = 100
	countsBreakdownMaxOffset    = 5000
	milkPreparationDefaultLimit = 10
	milkPreparationMaxLimit     = 50
	milkPreparationMaxOffset    = 5000
)

// boundedIntParam parses an optional integer query param. Absent or empty means the declared
// default; present but non-numeric or out of range is a 400, not a coerced value.
func boundedIntParam(query url.Values, name string, fallback, minValue, maxValue int32) (int32, error) {
	raw := strings.TrimSpace(query.Get(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if parsed < int64(minValue) || parsed > int64(maxValue) {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minValue, maxValue)
	}
	return int32(parsed), nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

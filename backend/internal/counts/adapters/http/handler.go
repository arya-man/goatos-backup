// Package http exposes the herd register read model API.
package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type HerdRegisterService interface {
	GetSummary(ctx context.Context, req domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error)
	GetBreakdown(ctx context.Context, req domain.CountsBreakdownQuery) (domain.CountsBreakdown, error)
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

// Package http exposes the feed-direction generation read API.
//
// Both routes are READ-ONLY. There is no write path here on purpose: this surface generates what
// SHOULD be fed, and recording what WAS fed (or packed, or proven on video) belongs to
// backend/internal/feed. Nothing on this surface needs an idempotency ledger because nothing on it
// has a side effect to replay.
package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Service is the generation boundary this handler renders.
type Service interface {
	Preview(ctx context.Context, q domain.PreviewQuery) (domain.PreviewPage, error)
	PackingWorklist(ctx context.Context, q domain.PackingQuery) (domain.PackingPage, error)
}

type Handler struct {
	service Service
	log     *slog.Logger
}

func NewHandler(service Service, log *slog.Logger) *Handler {
	return &Handler{service: service, log: log}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /feed-direction/preview", h.GetPreview)
	mux.HandleFunc("GET /feed-packing/worklist", h.GetPackingWorklist)
}

// GetPreview serves the generated feed direction for one park and one feed day.
func (h *Handler) GetPreview(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	query := r.URL.Query()

	targetDate, err := requiredBusinessDate(query, "target_date")
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	limit, err := boundedIntParam(query, "limit", app.DefaultShedPageLimit, 1, app.MaxShedPageLimit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(query, "offset", 0, 0, app.MaxShedPageOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	// Session 0 means "every session". A present-but-invalid session is rejected rather than
	// widened to all sessions, which would silently hand back three times the requested sheet.
	sessionNo, err := boundedIntParam(query, "session", 0, 0, 99)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	page, err := h.service.Preview(r.Context(), domain.PreviewQuery{
		TenantID:   tenantID,
		ParkID:     strings.TrimSpace(query.Get("park_id")),
		TargetDate: targetDate,
		ShedID:     strings.TrimSpace(query.Get("shed_id")),
		SessionNo:  sessionNo,
		Workflow:   strings.TrimSpace(query.Get("workflow")),
		Draft:      parseDraft(query),
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		h.writeServiceError(w, r, "feed direction preview", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

// GetPackingWorklist serves the per-shed packing view for one park and one feed day.
func (h *Handler) GetPackingWorklist(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	query := r.URL.Query()

	targetDate, err := requiredBusinessDate(query, "target_date")
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	limit, err := boundedIntParam(query, "limit", app.DefaultShedPageLimit, 1, app.MaxShedPageLimit)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	offset, err := boundedIntParam(query, "offset", 0, 0, app.MaxShedPageOffset)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	// Session 0 means "every session"; a present-but-invalid session is rejected, not widened --
	// same contract as the preview.
	sessionNo, err := boundedIntParam(query, "session", 0, 0, 99)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	page, err := h.service.PackingWorklist(r.Context(), domain.PackingQuery{
		TenantID:   tenantID,
		ParkID:     strings.TrimSpace(query.Get("park_id")),
		TargetDate: targetDate,
		SessionNo:  sessionNo,
		Workflow:   strings.TrimSpace(query.Get("workflow")),
		Draft:      parseDraft(query),
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		h.writeServiceError(w, r, "feed packing worklist", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

// writeServiceError maps the module's sentinel errors onto status codes. A caller error (bad park,
// bad paging) is a 400/404 rather than a 500, so a mistyped park id is not reported as an outage.
func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, op string, err error) {
	switch {
	case errors.Is(err, ports.ErrParkNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound, "park not found", nil)
	case errors.Is(err, ports.ErrParkRequired),
		errors.Is(err, ports.ErrInvalidTargetDate),
		errors.Is(err, ports.ErrInvalidWorkflow),
		errors.Is(err, ports.ErrInvalidPaging):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, op, err)
	}
}

// requiredBusinessDate parses the mandatory feed day.
//
// It accepts YYYY-MM-DD ONLY, and resolves it in Asia/Kolkata. An instant is rejected rather than
// truncated: accepting one would reintroduce exactly the UTC-vs-IST day-boundary bug the date form
// exists to prevent, and a feed sheet generated for the wrong day is a shed fed the wrong ration.
func requiredBusinessDate(query url.Values, name string) (time.Time, error) {
	raw := strings.TrimSpace(query.Get(name))
	if raw == "" {
		return time.Time{}, fmt.Errorf("%s is required (YYYY-MM-DD)", name)
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be a business date in YYYY-MM-DD form", name)
	}
	return biztime.BusinessDayStart(parsed), nil
}

// parseDraft reads the deliberate live-compute escape hatch. draft=true (or 1) live-computes a
// what-if sheet WITHOUT reading or writing any issue; anything else serves the frozen issued sheet.
// It is the only param that switches on live computation, and the response is stamped draft so a
// what-if can never be mistaken for an issued document.
func parseDraft(query url.Values) bool {
	raw := strings.ToLower(strings.TrimSpace(query.Get("draft")))
	return raw == "true" || raw == "1"
}

// boundedIntParam parses an optional integer query param. Absent or empty means the declared
// default; present but non-numeric or out of range is a 400, never a coerced value.
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

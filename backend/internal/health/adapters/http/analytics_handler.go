package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// The Health Analytics leadership read behind the /health/analytics screen.
//
// One GET, no writes, no idempotency: this handler cannot change a clinical
// fact, which is why it is a separate handler from the treatment-work one
// rather than another method on it.
//
// THE WINDOW IS VALIDATED HERE AND RESOLVED AGAIN IN THE SERVICE, through the
// SAME exported functions. That is deliberate duplication of the CALL, not of
// the rule: the handler owes a caller a 400 with a reason, and the service owes
// an internal caller a usable window; one shared implementation means the two
// can never disagree about what a legal window is.

const healthAnalyticsRoute = "/health/analytics"

// HealthAnalyticsService is the app-layer read this handler serves.
type HealthAnalyticsService interface {
	GetHealthAnalytics(context.Context, domain.HealthAnalyticsQuery) (domain.HealthAnalytics, error)
}

type AnalyticsHandler struct {
	svc HealthAnalyticsService
	log *slog.Logger
}

func NewAnalyticsHandler(svc HealthAnalyticsService, log *slog.Logger) *AnalyticsHandler {
	if log == nil {
		log = slog.Default()
	}
	return &AnalyticsHandler{svc: svc, log: log}
}

func RegisterAnalytics(mux *http.ServeMux, h *AnalyticsHandler) {
	mux.HandleFunc("GET "+healthAnalyticsRoute, h.GetHealthAnalytics)
}

// GetHealthAnalytics serves the whole page in one response.
//
// A present-but-invalid window is REJECTED rather than silently rewritten to the
// default: a director who names a window must see that window or an error, never
// a different window under the label they chose.
func (h *AnalyticsHandler) GetHealthAnalytics(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}

	fromDate, toDate, err := domain.ResolveHealthAnalyticsWindow(
		r.URL.Query().Get("from"),
		r.URL.Query().Get("to"),
		time.Now(),
	)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	req := domain.HealthAnalyticsQuery{
		TenantID: tenantID,
		FromDate: fromDate,
		ToDate:   toDate,
	}
	if park := strings.TrimSpace(r.URL.Query().Get("park_id")); park != "" {
		req.ParkID = &park
	}

	analytics, err := h.svc.GetHealthAnalytics(r.Context(), req)
	switch {
	case errors.Is(err, healthapp.ErrInvalidDate):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "from and to must be YYYY-MM-DD dates", nil)
		return
	case errors.Is(err, healthapp.ErrInvalidInput):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "invalid request", nil)
		return
	case err != nil:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "health analytics", err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, analytics)
}

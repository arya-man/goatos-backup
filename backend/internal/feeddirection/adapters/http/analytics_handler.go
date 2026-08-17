package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Feed Analytics read: the windowed DIRECTED rollup. Every figure is what the
// sheet directed, never measured consumption — the DTO field names and the page
// contract copy both carry that word so no client can honestly relabel it.

type analyticsDayDTO struct {
	FeedDay      string `json:"feed_day"`
	DirectedKg   string `json:"directed_kg"`
	HeadDays     int64  `json:"head_days"`
	PerHeadGrams string `json:"per_head_grams"`
}

type analyticsItemDTO struct {
	FeedDay       string `json:"feed_day"`
	FeedItemLabel string `json:"feed_item_label"`
	FeedItemKey   string `json:"feed_item_key"`
	DirectedKg    string `json:"directed_kg"`
	HeadDays      int64  `json:"head_days"`
	PerHeadGrams  string `json:"per_head_grams"`
}

type directedAnalyticsDTO struct {
	DateFrom string             `json:"date_from"`
	DateTo   string             `json:"date_to"`
	Days     []analyticsDayDTO  `json:"days"`
	Items    []analyticsItemDTO `json:"items"`
}

// GetDirectedAnalytics serves GET /feed-analytics/directed.
//
// date_from/date_to are optional business dates (inclusive). The default window
// is the 30 days ending YESTERDAY: today's sheet is still being executed, so
// every chart on the page excludes it, and the backend owns that rule rather
// than trusting each client to subtract a day.
func (h *Handler) GetDirectedAnalytics(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	query := r.URL.Query()

	yesterday := biztime.BusinessDayStart(time.Now().In(biztime.DefaultLocation())).AddDate(0, 0, -1)
	dateTo, err := optionalBusinessDate(query, "date_to", yesterday)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	dateFrom, err := optionalBusinessDate(query, "date_from", dateTo.AddDate(0, 0, -29))
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}

	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(),
		tenantID,
		strings.TrimSpace(query.Get("park_id")),
		permissions.FeedDirectionRead,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, parkScope.Message, nil)
		return
	}

	result, err := h.service.DirectedAnalytics(r.Context(), app.DirectedAnalyticsInput{
		TenantID:          tenantID,
		ParkID:            parkScope.ParkID,
		AuthorizedParkIDs: parkScope.ParkIDs,
		DateFrom:          dateFrom,
		DateTo:            dateTo,
	})
	if err != nil {
		h.writeServiceError(w, r, "feed analytics directed", err)
		return
	}

	from, to := domain.ClampAnalyticsWindow(dateFrom, dateTo)
	dto := directedAnalyticsDTO{
		DateFrom: from.Format("2006-01-02"),
		DateTo:   to.Format("2006-01-02"),
		Days:     make([]analyticsDayDTO, 0, len(result.Days)),
		Items:    make([]analyticsItemDTO, 0, len(result.Items)),
	}
	for _, d := range result.Days {
		dto.Days = append(dto.Days, analyticsDayDTO(d))
	}
	for _, it := range result.Items {
		dto.Items = append(dto.Items, analyticsItemDTO(it))
	}
	httpresponse.WriteJSON(w, http.StatusOK, dto)
}

// optionalBusinessDate parses an optional YYYY-MM-DD query param, applying the
// caller's default when absent. A PRESENT-but-malformed value is rejected, never
// silently replaced by the default the caller did not ask for.
func optionalBusinessDate(query map[string][]string, name string, fallback time.Time) (time.Time, error) {
	values := query[name]
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return fallback, nil
	}
	return businessDateFromString(strings.TrimSpace(values[0]))
}

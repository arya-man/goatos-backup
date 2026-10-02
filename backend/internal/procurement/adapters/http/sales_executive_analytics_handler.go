package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// SalesExecutiveAnalyticsService is the behaviour this transport depends on.
type SalesExecutiveAnalyticsService interface {
	SalesExecutiveAnalytics(ctx context.Context, tenantID string, days int, page domain.SalesExecutivePage) (domain.SalesExecutiveAnalytics, error)
}

// SalesExecutiveAnalyticsHandler serves the Sales > Sales executive analytics read.
type SalesExecutiveAnalyticsHandler struct {
	service SalesExecutiveAnalyticsService
	log     *slog.Logger
}

func NewSalesExecutiveAnalyticsHandler(service SalesExecutiveAnalyticsService, log ...*slog.Logger) *SalesExecutiveAnalyticsHandler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &SalesExecutiveAnalyticsHandler{service: service, log: l}
}

// RegisterSalesExecutiveAnalytics mounts the read. The pattern must stay byte-identical to its
// entry in permissions/routes.go -- the permission table is matched by method + pattern.
func RegisterSalesExecutiveAnalytics(mux *http.ServeMux, h *SalesExecutiveAnalyticsHandler) {
	mux.HandleFunc("GET /procurement/sales-executive-analytics", h.SalesExecutiveAnalytics)
}

type salesExecutiveCountsPayload struct {
	VendorsAdded      int     `json:"vendors_added"`
	VendorsEdited     int     `json:"vendors_edited"`
	MarketCalls       int     `json:"market_calls"`
	LeadCalls         int     `json:"lead_calls"`
	Calls             int     `json:"calls"`
	SalesRecorded     int     `json:"sales_recorded"`
	SalesValue        float64 `json:"sales_value"`
	PaymentsRecorded  int     `json:"payments_recorded"`
	PaymentsValue     float64 `json:"payments_value"`
	DealStatusChanges int     `json:"deal_status_changes"`
	Total             int     `json:"total"`
}

func countsPayload(c domain.SalesExecutiveCounts) salesExecutiveCountsPayload {
	return salesExecutiveCountsPayload{
		VendorsAdded:      c.VendorsAdded,
		VendorsEdited:     c.VendorsEdited,
		MarketCalls:       c.MarketCalls,
		LeadCalls:         c.LeadCalls,
		Calls:             c.Calls(),
		SalesRecorded:     c.SalesRecorded,
		SalesValue:        c.SalesValue,
		PaymentsRecorded:  c.PaymentsRecorded,
		PaymentsValue:     c.PaymentsValue,
		DealStatusChanges: c.DealStatusChanges,
		Total:             c.Total(),
	}
}

type salesExecutiveDayPayload struct {
	Date          string `json:"date"`
	DateTo        string `json:"date_to"`
	VendorsAdded  int    `json:"vendors_added"`
	VendorsEdited int    `json:"vendors_edited"`
	Calls         int    `json:"calls"`
	Sales         int    `json:"sales"`
}

type salesExecutivePersonPayload struct {
	ActorID string `json:"actor_id"`
	// Name is "" when the person cannot be named; the page renders its own "unknown" copy.
	Name             string                      `json:"name"`
	Counts           salesExecutiveCountsPayload `json:"counts"`
	ActiveDays       int                         `json:"active_days"`
	LastActiveAt     string                      `json:"last_active_at"`
	LastActivityKind string                      `json:"last_activity_kind"`
}

type salesExecutiveActivityPayload struct {
	Kind         string  `json:"kind"`
	ActorName    string  `json:"actor_name"`
	At           string  `json:"at"`
	BusinessDate string  `json:"business_date"`
	Subject      string  `json:"subject"`
	Category     string  `json:"category,omitempty"`
	Animals      float64 `json:"animals"`
	Amount       float64 `json:"amount"`
}

type salesExecutiveLatestVendorPayload struct {
	VendorID     string `json:"vendor_id"`
	BusinessName string `json:"business_name"`
	Category     string `json:"category"`
	Place        string `json:"place,omitempty"`
	AddedByName  string `json:"added_by_name"`
	AddedByKnown bool   `json:"added_by_known"`
	AddedAt      string `json:"added_at"`
}

type salesExecutiveAnalyticsPayload struct {
	Days            int                                 `json:"days"`
	DaysOptions     []int                               `json:"days_options"`
	TrendGrain      string                              `json:"trend_grain"`
	PeriodFrom      string                              `json:"period_from"`
	PeriodTo        string                              `json:"period_to"`
	Current         salesExecutiveCountsPayload         `json:"current"`
	Previous        salesExecutiveCountsPayload         `json:"previous"`
	ActivePeople    int                                 `json:"active_people"`
	RegisterVendors int                                 `json:"register_vendors"`
	ImportedVendors int                                 `json:"imported_vendors"`
	Daily           []salesExecutiveDayPayload          `json:"daily"`
	People          []salesExecutivePersonPayload       `json:"people"`
	Recent          []salesExecutiveActivityPayload     `json:"recent"`
	RecentTotal     int                                 `json:"recent_total"`
	ActivityOffset  int                                 `json:"activity_offset"`
	LatestVendors   []salesExecutiveLatestVendorPayload `json:"latest_vendors"`
	VendorOffset    int                                 `json:"vendor_offset"`
	PageSize        int                                 `json:"page_size"`
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// SalesExecutiveAnalytics serves GET /procurement/sales-executive-analytics?days=7|30|90, paged by
// activity_offset (the activity feed) and vendor_offset (the latest vendors).
func (h *SalesExecutiveAnalyticsHandler) SalesExecutiveAnalytics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	intParam := func(name, code, message string) (int, bool) {
		raw := strings.TrimSpace(q.Get(name))
		if raw == "" {
			return 0, true
		}
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest(code, message))
			return 0, false
		}
		return parsed, true
	}
	days, ok := intParam("days", "invalid_days", "Pick one of the offered periods.")
	if !ok {
		return
	}
	activityOffset, ok := intParam("activity_offset", "invalid_offset", "That page is not valid.")
	if !ok {
		return
	}
	vendorOffset, ok := intParam("vendor_offset", "invalid_offset", "That page is not valid.")
	if !ok {
		return
	}
	out, err := h.service.SalesExecutiveAnalytics(r.Context(), tenantID(r), days,
		domain.SalesExecutivePage{ActivityOffset: activityOffset, VendorOffset: vendorOffset})
	if err != nil {
		h.writeErr(w, r, app.SalesExecutiveAnalyticsHTTPError(err))
		return
	}

	daily := make([]salesExecutiveDayPayload, 0, len(out.Daily))
	for _, d := range out.Daily {
		daily = append(daily, salesExecutiveDayPayload{
			Date: d.Date, DateTo: d.DateTo, VendorsAdded: d.VendorsAdded, VendorsEdited: d.VendorsEdited,
			Calls: d.Calls, Sales: d.Sales,
		})
	}
	people := make([]salesExecutivePersonPayload, 0, len(out.People))
	for _, p := range out.People {
		people = append(people, salesExecutivePersonPayload{
			ActorID: p.ActorID, Name: p.Name, Counts: countsPayload(p.Counts),
			ActiveDays: p.ActiveDays, LastActiveAt: rfc3339(p.LastActiveAt),
			LastActivityKind: p.LastActivityKind,
		})
	}
	recent := make([]salesExecutiveActivityPayload, 0, len(out.Recent))
	for _, a := range out.Recent {
		recent = append(recent, salesExecutiveActivityPayload{
			Kind: a.Kind, ActorName: a.ActorName, At: rfc3339(a.At), BusinessDate: a.BusinessDate,
			Subject: a.Subject, Category: a.Category, Animals: a.Animals, Amount: a.Amount,
		})
	}
	latest := make([]salesExecutiveLatestVendorPayload, 0, len(out.LatestVendors))
	for _, v := range out.LatestVendors {
		latest = append(latest, salesExecutiveLatestVendorPayload{
			VendorID: v.VendorID, BusinessName: v.BusinessName, Category: v.Category, Place: v.Place,
			AddedByName: v.AddedByName, AddedByKnown: v.AddedByKnown, AddedAt: rfc3339(v.AddedAt),
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, salesExecutiveAnalyticsPayload{
		Days:            out.Days,
		DaysOptions:     append([]int(nil), domain.SalesExecutiveWindowDays...),
		TrendGrain:      out.TrendGrain,
		PeriodFrom:      out.PeriodFrom,
		PeriodTo:        out.PeriodTo,
		Current:         countsPayload(out.Current),
		Previous:        countsPayload(out.Previous),
		ActivePeople:    out.ActivePeople,
		RegisterVendors: out.RegisterVendors,
		ImportedVendors: out.ImportedVendors,
		Daily:           daily,
		People:          people,
		Recent:          recent,
		RecentTotal:     out.RecentTotal,
		ActivityOffset:  out.ActivityOffset,
		LatestVendors:   latest,
		VendorOffset:    out.VendorOffset,
		PageSize:        out.PageSize,
	})
}

func (h *SalesExecutiveAnalyticsHandler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}

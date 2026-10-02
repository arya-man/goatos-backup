package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// BuyerAnalyticsService is the behaviour this transport depends on.
type BuyerAnalyticsService interface {
	BuyerAnalyticsSorted(ctx context.Context, tenantID, farm, sortKey, dir string, limit, offset int) (domain.BuyerAnalytics, error)
}

// BuyerAnalyticsHandler serves the Sales > Buyer analytics read.
type BuyerAnalyticsHandler struct {
	service BuyerAnalyticsService
	log     *slog.Logger
}

func NewBuyerAnalyticsHandler(service BuyerAnalyticsService, log ...*slog.Logger) *BuyerAnalyticsHandler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &BuyerAnalyticsHandler{service: service, log: l}
}

// RegisterBuyerAnalytics mounts the read. The pattern must stay byte-identical to its entry in
// permissions/routes.go -- the permission table is matched by method + pattern, and a mismatch
// serves the route ungated.
func RegisterBuyerAnalytics(mux *http.ServeMux, h *BuyerAnalyticsHandler) {
	mux.HandleFunc("GET /procurement/buyer-analytics", h.BuyerAnalytics)
}

type buyerRowPayload struct {
	BuyerKey   string `json:"buyer_key"`
	VendorID   string `json:"vendor_id,omitempty"`
	InRegister bool   `json:"in_register"`
	BuyerName  string `json:"buyer_name"`
	// Phone is omitted for a buyer not in the register, and for EVERY row when the caller may
	// not read the vendor register (see phones_visible).
	Phone    string `json:"phone_number,omitempty"`
	Category string `json:"category,omitempty"`
	Place    string `json:"place,omitempty"`

	Purchases   int     `json:"purchases"`
	Animals     float64 `json:"animals"`
	Revenue     float64 `json:"revenue"`
	SharePct    float64 `json:"share_pct"`
	Outstanding float64 `json:"outstanding"`

	FirstSaleDate   string `json:"first_sale_date"`
	LastSaleDate    string `json:"last_sale_date"`
	Repeat          bool   `json:"repeat"`
	RepeatPurchases int    `json:"repeat_purchases"`
	AvgDaysBetween  *int   `json:"avg_days_between,omitempty"`
	DaysSinceLast   *int   `json:"days_since_last,omitempty"`

	ProductTypes []string `json:"product_types"`
}

type buyerSummaryPayload struct {
	Buyers           int     `json:"buyers"`
	RepeatBuyers     int     `json:"repeat_buyers"`
	OneTimeBuyers    int     `json:"one_time_buyers"`
	NotInRegister    int     `json:"not_in_register"`
	Purchases        int     `json:"purchases"`
	Animals          float64 `json:"animals"`
	Revenue          float64 `json:"revenue"`
	RepeatRevenue    float64 `json:"repeat_revenue"`
	RepeatRevenuePct float64 `json:"repeat_revenue_pct"`
	Outstanding      float64 `json:"outstanding"`
	PeriodFrom       string  `json:"period_from,omitempty"`
	PeriodTo         string  `json:"period_to,omitempty"`
}

type buyerAnalyticsPayload struct {
	Buyers      []buyerRowPayload   `json:"buyers"`
	TotalBuyers int                 `json:"total_buyers"`
	Summary     buyerSummaryPayload `json:"summary"`
	Limit       int                 `json:"limit"`
	Offset      int                 `json:"offset"`
	// PhonesVisible says whether phone numbers were included at all, so the client can render
	// the column's absence as a permission rather than as every buyer lacking a number.
	PhonesVisible bool `json:"phones_visible"`
}

// callerMaySeePhones resolves the vendor register permission from the SAME source the route table
// authorized against: the per-person permission set when THAT decided the request, else the grant
// roles -- never a query parameter. A buyer's phone number is the SALES half of the register (the
// /sales/vendors leaf is gated on VendorSalesRead for exactly this reason), so a sales reader who was never given the
// register gets the buyer rows without it.
func callerMaySeePhones(r *http.Request) bool {
	if perms, ok := httpmiddleware.PersonPermissionsFromContext(r.Context()); ok {
		for _, p := range perms {
			if p == permissions.VendorSalesRead {
				return true
			}
		}
		return false
	}
	for _, grant := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if permissions.RoleHasPermission(grant.Role, permissions.VendorSalesRead) {
			return true
		}
	}
	return false
}

// BuyerAnalytics serves GET /procurement/buyer-analytics. farm is the sales pages' toggle value
// (all/CBE/CPT); sort/dir order EVERY buyer by one column before limit and offset page the rows.
func (h *BuyerAnalyticsHandler) BuyerAnalytics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := 0, 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_limit", "That page size is not valid."))
			return
		}
		limit = parsed
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_offset", "That page is not valid."))
			return
		}
		offset = parsed
	}
	out, err := h.service.BuyerAnalyticsSorted(r.Context(), tenantID(r), q.Get("farm"), q.Get("sort"), q.Get("dir"), limit, offset)
	if err != nil {
		h.writeErr(w, r, app.BuyerAnalyticsHTTPError(err))
		return
	}
	phones := callerMaySeePhones(r)
	rows := make([]buyerRowPayload, 0, len(out.Buyers))
	for _, b := range out.Buyers {
		phone := ""
		if phones {
			phone = b.Phone
		}
		types := b.ProductTypes
		if types == nil {
			types = []string{}
		}
		rows = append(rows, buyerRowPayload{
			BuyerKey:        b.BuyerKey,
			VendorID:        b.VendorID,
			InRegister:      b.InRegister,
			BuyerName:       b.BuyerName,
			Phone:           phone,
			Category:        b.Category,
			Place:           b.Place,
			Purchases:       b.Purchases,
			Animals:         b.Animals,
			Revenue:         b.Revenue,
			SharePct:        b.SharePct,
			Outstanding:     b.Outstanding,
			FirstSaleDate:   b.FirstSaleDate,
			LastSaleDate:    b.LastSaleDate,
			Repeat:          b.Repeat,
			RepeatPurchases: b.RepeatPurchases,
			AvgDaysBetween:  b.AvgDaysBetween,
			DaysSinceLast:   b.DaysSinceLast,
			ProductTypes:    types,
		})
	}
	s := out.Summary
	httpresponse.WriteJSON(w, http.StatusOK, buyerAnalyticsPayload{
		Buyers:      rows,
		TotalBuyers: out.TotalBuyers,
		Summary: buyerSummaryPayload{
			Buyers:           s.Buyers,
			RepeatBuyers:     s.RepeatBuyers,
			OneTimeBuyers:    s.OneTimeBuyers,
			NotInRegister:    s.NotInRegister,
			Purchases:        s.Purchases,
			Animals:          s.Animals,
			Revenue:          s.Revenue,
			RepeatRevenue:    s.RepeatRevenue,
			RepeatRevenuePct: s.RepeatRevenuePct,
			Outstanding:      s.Outstanding,
			PeriodFrom:       s.PeriodFrom,
			PeriodTo:         s.PeriodTo,
		},
		Limit:         out.Limit,
		Offset:        out.Offset,
		PhonesVisible: phones,
	})
}

func (h *BuyerAnalyticsHandler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}

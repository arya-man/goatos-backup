package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

type stubBuyerAnalyticsService struct {
	out     domain.BuyerAnalytics
	farm    string
	sortKey string
	dir     string
	err     error
}

func (s *stubBuyerAnalyticsService) BuyerAnalyticsSorted(_ context.Context, _ string, farm, sortKey, dir string, limit, offset int) (domain.BuyerAnalytics, error) {
	s.farm, s.sortKey, s.dir = farm, sortKey, dir
	if s.err != nil {
		return domain.BuyerAnalytics{}, s.err
	}
	out := s.out
	out.Limit, out.Offset = limit, offset
	return out, nil
}

func buyerFixture() domain.BuyerAnalytics {
	gap := 140
	return domain.BuyerAnalytics{
		TotalBuyers: 2,
		Buyers: []domain.BuyerRow{
			{BuyerKey: "vendor:v1", VendorID: "v1", InRegister: true, BuyerName: "Mahendran", Phone: "9750078019", Category: "Agent", Place: "Pollachi", Purchases: 3, Animals: 75, Revenue: 599379, Repeat: true, RepeatPurchases: 2, AvgDaysBetween: &gap, FirstSaleDate: "2025-12-03", LastSaleDate: "2026-09-09", ProductTypes: []string{"Goat", "Sheep"}},
			{BuyerKey: "name:al madina", BuyerName: "Al Madina", Place: "Coimbatore, TN", Purchases: 1, Animals: 20, Revenue: 227160, FirstSaleDate: "2026-08-29", LastSaleDate: "2026-08-29"},
		},
		Summary: domain.BuyerSummary{Buyers: 2, RepeatBuyers: 1, OneTimeBuyers: 1, NotInRegister: 1, Purchases: 4, Revenue: 826539},
	}
}

func serveBuyerAnalytics(t *testing.T, h *BuyerAnalyticsHandler, path string, ctx func(context.Context) context.Context) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	c := httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001")
	if ctx != nil {
		c = ctx(c)
	}
	req = req.WithContext(c)
	rec := httptest.NewRecorder()
	h.BuyerAnalytics(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: decode %v: %s", path, err, rec.Body.String())
	}
	return rec.Code, body
}

func buyerRows(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["buyers"].([]any)
	if !ok {
		t.Fatalf("buyers missing: %v", body)
	}
	rows := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, r.(map[string]any))
	}
	return rows
}

// TestBuyerAnalyticsPhonesFollowVendorRead pins the ENDPOINT half of the phone gate: the number
// rides the row only for a caller holding the vendor register permission, resolved from the
// per-person set when that decided the request and from the grant roles otherwise -- never from
// the query. phones_visible tells the client which case it is in, so an absent column is a
// permission and not "no buyer has a number".
func TestBuyerAnalyticsPhonesFollowTheBuyersHalfOfTheRegister(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	h := NewBuyerAnalyticsHandler(&stubBuyerAnalyticsService{out: buyerFixture()})

	cases := []struct {
		name    string
		ctx     func(context.Context) context.Context
		visible bool
	}{
		{"role grant holding VendorRead", func(c context.Context) context.Context {
			return httpmiddleware.WithAuthGrants(c, []permissions.ActiveGrant{{Role: permissions.RoleProcurementManager, ScopeType: "tenant", ScopeID: tenant}})
		}, true},
		{"role grant without VendorRead", func(c context.Context) context.Context {
			return httpmiddleware.WithAuthGrants(c, []permissions.ActiveGrant{{Role: permissions.RoleVerifier, ScopeType: "tenant", ScopeID: tenant}})
		}, false},
		// A buyer's phone is the SALES half of the register (2026-10-02): VendorSalesRead.
		{"person ticks carrying the buyers half", func(c context.Context) context.Context {
			return httpmiddleware.WithPersonPermissions(c, []string{permissions.SalesRead, permissions.VendorSalesRead})
		}, true},
		{"person ticks carrying only the suppliers half", func(c context.Context) context.Context {
			return httpmiddleware.WithPersonPermissions(c, []string{permissions.SalesRead, permissions.VendorRead})
		}, false},
		// The person path DECIDES when present: a manager role grant beside a ticked set that
		// withholds the register must not leak the number through the role.
		{"person ticks without VendorRead beat the role", func(c context.Context) context.Context {
			c = httpmiddleware.WithAuthGrants(c, []permissions.ActiveGrant{{Role: permissions.RoleProcurementManager, ScopeType: "tenant", ScopeID: tenant}})
			return httpmiddleware.WithPersonPermissions(c, []string{permissions.SalesRead})
		}, false},
		{"no grants at all", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := serveBuyerAnalytics(t, h, "/procurement/buyer-analytics?phones=1", tc.ctx)
			if code != http.StatusOK {
				t.Fatalf("status %d: %v", code, body)
			}
			if body["phones_visible"] != tc.visible {
				t.Fatalf("phones_visible = %v, want %v", body["phones_visible"], tc.visible)
			}
			rows := buyerRows(t, body)
			if len(rows) != 2 {
				t.Fatalf("rows = %d", len(rows))
			}
			_, hasPhone := rows[0]["phone_number"]
			if hasPhone != tc.visible {
				t.Fatalf("register-backed row carries phone = %v, want %v: %v", hasPhone, tc.visible, rows[0])
			}
			if _, has := rows[1]["phone_number"]; has {
				t.Fatalf("a buyer not in the register must never carry a phone: %v", rows[1])
			}
			// Everything that is NOT register data is served regardless.
			if rows[0]["buyer_name"] != "Mahendran" || rows[0]["purchases"] != float64(3) || rows[0]["repeat"] != true || rows[0]["avg_days_between"] != float64(140) {
				t.Fatalf("row shape = %v", rows[0])
			}
			if rows[1]["in_register"] != false || rows[1]["repeat"] != false {
				t.Fatalf("name-only row shape = %v", rows[1])
			}
			if _, has := rows[1]["avg_days_between"]; has {
				t.Fatalf("a one-time buyer must carry no cadence: %v", rows[1])
			}
			summary := body["summary"].(map[string]any)
			if summary["buyers"] != float64(2) || summary["not_in_register"] != float64(1) || body["total_buyers"] != float64(2) {
				t.Fatalf("summary = %v total = %v", summary, body["total_buyers"])
			}
		})
	}
}

// TestBuyerAnalyticsPassesFilterAndRejectsBadPaging pins that the farm toggle value reaches the
// service untouched, paging is echoed, and a malformed page is refused rather than defaulted.
func TestBuyerAnalyticsPassesFilterAndRejectsBadPaging(t *testing.T) {
	svc := &stubBuyerAnalyticsService{out: buyerFixture()}
	h := NewBuyerAnalyticsHandler(svc)

	code, body := serveBuyerAnalytics(t, h, "/procurement/buyer-analytics?farm=CPT&limit=50&offset=25", nil)
	if code != http.StatusOK || svc.farm != "CPT" || body["limit"] != float64(50) || body["offset"] != float64(25) {
		t.Fatalf("status %d farm %q body %v", code, svc.farm, body)
	}
	for _, path := range []string{"?limit=abc", "?offset=x"} {
		code, body := serveBuyerAnalytics(t, h, "/procurement/buyer-analytics"+path, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("%s: status %d body %v", path, code, body)
		}
	}

	svc.err = app.ErrBuyerFarmInvalid
	code, body = serveBuyerAnalytics(t, h, "/procurement/buyer-analytics?farm=Pollachi", nil)
	if code != http.StatusBadRequest || body["error"] != "invalid_farm" {
		t.Fatalf("bad farm: status %d body %v", code, body)
	}
	svc.err = errors.New("boom")
	code, body = serveBuyerAnalytics(t, h, "/procurement/buyer-analytics", nil)
	if code != http.StatusInternalServerError || body["error"] == "boom" {
		t.Fatalf("internal error must not echo internals: status %d body %v", code, body)
	}
}

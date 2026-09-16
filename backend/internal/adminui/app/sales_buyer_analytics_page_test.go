package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestSalesBuyerAnalyticsPageContract pins the Buyer analytics page (maintainer request
// 2026-09-15): its own route under Sales, read-only by contract, one table served by the
// procurement buyer read with farm-worded column labels, the farm toggle vocabulary, and every
// copy key the renderer needs.
func TestSalesBuyerAnalyticsPageContract(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	page := pageByRouteID(t, resp.Pages, "sales-buyer-analytics")
	if page.Href != "/sales/buyer-analytics" || page.PathPattern != "/sales/buyer-analytics" || page.SurfaceKind != "module-surface" {
		t.Fatalf("page = %q/%q/%q", page.Href, page.PathPattern, page.SurfaceKind)
	}
	if len(page.Tables) != 1 || page.Tables[0].ID != "sales-buyer-analytics" {
		t.Fatalf("tables = %+v", page.Tables)
	}
	table := page.Tables[0]
	if table.DataSource != "/procurement/buyer-analytics" {
		t.Fatalf("data source = %q", table.DataSource)
	}
	if table.RowClick.Enabled {
		t.Fatalf("buyers table must not declare a row click: %+v", table.RowClick)
	}
	if len(table.PageSizeOptions) == 0 || table.PageSizeOptions[0] != 25 {
		t.Fatalf("page sizes = %v", table.PageSizeOptions)
	}
	labels := map[string]string{}
	for _, c := range table.Columns {
		labels[c.Key] = c.Label
	}
	// Every column but the phone sorts (maintainer request 2026-09-16); the phone is contact
	// detail, not a figure, and sorting it would order by digits nobody compares.
	sortableKeys := map[string]bool{}
	for _, c := range table.Columns {
		sortableKeys[c.Key] = c.Sortable
	}
	for key, want := range map[string]bool{
		"buyer_name": true, "phone_number": false, "purchases": true, "animals": true, "revenue": true,
		"repeat": true, "first_sale_date": true, "last_sale_date": true, "outstanding": true,
	} {
		got, ok := sortableKeys[key]
		if !ok || got != want {
			t.Fatalf("column %q sortable = %v (declared %v), want %v", key, got, ok, want)
		}
	}
	for key, want := range map[string]string{
		"buyer_name":   "Buyer",
		"phone_number": "Phone",
		"purchases":    "Purchases so far",
		"repeat":       "Comes back",
		"outstanding":  "Still owed",
	} {
		if labels[key] != want {
			t.Fatalf("column %q label = %q, want %q", key, labels[key], want)
		}
	}

	for _, key := range []string{
		"crumb", "filter.farm", "section.headline.aria",
		"kpi.buyers", "kpi.repeat_buyers", "kpi.repeat_revenue", "kpi.outstanding",
		"section.buyers.title", "section.buyers.subtitle",
		"chip.repeat", "chip.one_time",
		"value.every_days", "value.days_ago", "value.none", "value.settled", "hint.phone_hidden",
		"summary.buyers", "pager.page", "pager.of", "action.prev_page", "action.next_page",
		"empty.buyers", "error.load",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("buyer analytics copy missing %q", key)
		}
	}
	if page.Copy["crumb"] != "Sales" {
		t.Fatalf("crumb = %q", page.Copy["crumb"])
	}

	groups := map[string]int{}
	for _, g := range page.OptionGroups {
		groups[g.ID] = len(g.Options)
	}
	if groups["sales_farms"] != 3 {
		t.Fatalf("sales_farms options = %d, want 3", groups["sales_farms"])
	}

	found := false
	for _, rule := range resp.RouteLabels {
		if rule.Pattern == "/sales/buyer-analytics" && rule.Label == "Buyer analytics" && rule.Match == "exact" {
			found = true
		}
	}
	if !found {
		t.Fatal("route label for /sales/buyer-analytics missing")
	}
}

// TestBuyerPhoneColumnIsGatedOnVendorRead pins both halves of the capability lock for the phone
// column on the contract side: the control is ENABLED for a holder of VendorRead and otherwise
// present-but-disabled with a backend reason, a read control with no action. The verifier row
// is the negative case, exercised on the compile function directly because a role without
// SalesRead is never served the page at all. (Every role with SalesRead today also holds
// VendorRead, so no role fixture can distinguish keying this on SalesRead; the per-person path
// is what the gate exists for, and the endpoint half is pinned in procurement/adapters/http.)
func TestBuyerPhoneColumnIsGatedOnVendorRead(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	copy := pageSpecificCopy("sales-buyer-analytics")
	for _, tc := range []struct {
		role    string
		enabled bool
	}{
		{permissions.RoleCEOInternal, true},
		{permissions.RoleProcurementManager, true},
		{permissions.RoleVerifier, false},
		{permissions.RoleGrowthDirector, false},
	} {
		controls := compileBuyerAnalyticsControls(nil, BootstrapInput{
			TenantID: tenant,
			Grants:   []permissions.ActiveGrant{{Role: tc.role, ScopeType: "tenant", ScopeID: tenant}},
		}, copy)
		control := controlByID(t, controls, "buyer_phone_column")
		if control.Enabled != tc.enabled {
			t.Fatalf("buyer_phone_column enabled = %v for %s, want %v (%s)", control.Enabled, tc.role, tc.enabled, control.DisabledReason)
		}
		if control.Kind != "table_column" || control.Action != "" {
			t.Fatalf("the phone column is a read control: %+v", control)
		}
		if !tc.enabled && control.DisabledReason != copy["hint.phone_hidden"] {
			t.Fatalf("disabled reason = %q, want the backend's hint", control.DisabledReason)
		}
	}

	// And the served page carries it, enabled, for the CEO.
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: tenant,
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants:   []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: tenant}},
	})
	page := pageByRouteID(t, resp.Pages, "sales-buyer-analytics")
	if control := controlByID(t, page.Controls, "buyer_phone_column"); !control.Enabled {
		t.Fatalf("CEO phone column disabled: %+v", control)
	}
}

func TestBuyerPhoneColumnFollowsPersonVendorRead(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: tenant,
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	access := permissions.PageAccess{
		Pages: map[string]struct{}{
			"sales-buyer-analytics": {},
		},
		Modules: map[string]struct{}{
			"sales": {},
		},
	}

	withoutVendor := applyPersonPageLens(resp, access, []string{permissions.SalesRead}, true)
	page := pageByRouteID(t, withoutVendor.Pages, "sales-buyer-analytics")
	if control := controlByID(t, page.Controls, "buyer_phone_column"); control.Enabled {
		t.Fatalf("person without VendorRead must not get buyer phone column enabled: %+v", control)
	}
	if control := controlByID(t, page.Controls, "buyer_phone_column"); control.DisabledReason != page.Copy["hint.phone_hidden"] {
		t.Fatalf("disabled reason = %q, want %q", control.DisabledReason, page.Copy["hint.phone_hidden"])
	}

	withVendor := applyPersonPageLens(resp, access, []string{permissions.SalesRead, permissions.VendorRead}, true)
	page = pageByRouteID(t, withVendor.Pages, "sales-buyer-analytics")
	if control := controlByID(t, page.Controls, "buyer_phone_column"); !control.Enabled {
		t.Fatalf("person with VendorRead must keep buyer phone column even without Sales Vendors tick: %+v", control)
	}
}

// TestSalesBuyerAnalyticsNavLeafRidesSalesRead pins the leaf gate: SalesRead exactly, the
// TestSalesLoadsNavLeafRidesSalesRead shape -- never ProcurementRead, which operators and park
// heads hold for the intake screens they work, and never VendorRead, which gates only the
// phone column on it.
func TestSalesBuyerAnalyticsNavLeafRidesSalesRead(t *testing.T) {
	required := permissionsForNav("sales-buyer-analytics")
	if len(required) != 1 || required[0] != permissions.SalesRead {
		t.Fatalf("permissionsForNav(sales-buyer-analytics) = %v, want exactly SalesRead", required)
	}
}

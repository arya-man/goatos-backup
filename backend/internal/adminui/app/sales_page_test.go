package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestSalesPageContractAndNavigation pins the sales page's published contract: its OWN top-level
// nav group (Sales split out of Procurement, maintainer decision 2026-08-27), the breadcrumb, the
// deals table shape, and the backend-owned copy the client renders verbatim.
func TestSalesPageContractAndNavigation(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})

	page := pageByRouteID(t, resp.Pages, "sales")
	if page.Href != "/sales" || page.PathPattern != "/sales" {
		t.Fatalf("sales page href/pattern = %q/%q", page.Href, page.PathPattern)
	}
	if page.SurfaceKind != "module-surface" {
		t.Fatalf("sales must be a module surface, got %q", page.SurfaceKind)
	}

	if len(page.Tables) != 2 || page.Tables[0].ID != "sales-deals" || page.Tables[1].ID != "sales-buyers" {
		t.Fatalf("sales tables = %+v", page.Tables)
	}
	deals := page.Tables[0]
	// The buyer board is paged in the renderer off the overview response: the contract owns the
	// page size, and declares no row click because there is no buyer record to open.
	buyers := page.Tables[1]
	if len(buyers.PageSizeOptions) == 0 || buyers.PageSizeOptions[0] != 10 {
		t.Fatalf("buyers page sizes = %v", buyers.PageSizeOptions)
	}
	if buyers.RowClick.Enabled {
		t.Fatalf("buyers table must not declare a row click: %+v", buyers.RowClick)
	}
	if deals.DataSource != "/sales/deals" {
		t.Fatalf("deals table source = %q", deals.DataSource)
	}
	if deals.RowClick.Param != "deal_id" {
		t.Fatalf("deals row param = %q", deals.RowClick.Param)
	}

	// Backend-owned copy: every key the renderer needs must be published, farm language only.
	for _, key := range []string{
		"section.headline.title", "section.monthly.title", "section.price_bands.title",
		"section.market.title", "section.buyers.title", "section.pipeline.title",
		"section.evidence.title", "section.ledger.title",
		"kpi.revenue", "kpi.animals", "kpi.realized_price", "kpi.manure",
		"chart.monthly_revenue.title", "chart.monthly_animals.title",
		"chart.monthly_manure.title", "chart.price_bands.title",
		"chart.monthly_revenue.empty", "chart.price_bands.empty",
		"chart.monthly_animals.sub", "chart.monthly_manure.sub",
		"evidence.audit.within_0_3", "evidence.audit.within_1", "evidence.audit.over_1",
		"action.record_sale.label", "field.sale_date", "field.farm", "field.product_type",
		"field.breed", "field.buyer_name", "field.total_weight_kg", "field.sales_value",
		// The vendor select's copy: a REQUIRED field whose dead-end needs an exit, so the
		// placeholder, the "add them on Vendors" hint, the register-empty replacement and the
		// unreadable-register error are all backend-owned rather than composed in the client.
		"field.vendor", "select.vendor.placeholder", "hint.vendor", "hint.vendor_empty",
		"hint.vendor_truncated", "action.open_vendors", "hint.vendor_prefill", "error.vendors_unavailable",
		"action.sale_recorded", "action.sale_record_failed",
		"empty.deals", "error.load", "disabled.write",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("sales copy missing %q", key)
		}
	}
	if page.Copy["evidence.audit.within_0_3"] != "Matches the book (within 0.3 kg)" {
		t.Fatalf("weight-audit bucket copy drifted: %q", page.Copy["evidence.audit.within_0_3"])
	}

	// Option groups: farm scope, product types, and per-product breed groups.
	groups := map[string]int{}
	for _, g := range page.OptionGroups {
		groups[g.ID] = len(g.Options)
	}
	if groups["sales_farms"] != 3 {
		t.Fatalf("sales_farms options = %d", groups["sales_farms"])
	}
	if groups["sales_product_types"] != 3 {
		t.Fatalf("sales_product_types options = %d", groups["sales_product_types"])
	}
	for _, id := range []string{"sales_breeds_sheep", "sales_breeds_goat", "sales_breeds_manure"} {
		if groups[id] == 0 {
			t.Fatalf("option group %q missing", id)
		}
	}

	// Nav: Sales is its OWN top-level group, and Procurement no longer carries it. Both halves are
	// asserted -- a leaf left behind in Procurement would make the module appear twice, which is
	// the failure a "does the new group exist" check alone would pass.
	var salesGroup *domain.NavigationGroup
	for i, group := range resp.Navigation.Groups {
		if group.ID == "sales" {
			salesGroup = &resp.Navigation.Groups[i]
		}
		if group.ID != "procurement" {
			continue
		}
		for _, leaf := range group.Leaves {
			if leaf.Href == "/sales" || leaf.ID == "procurement-sales" {
				t.Fatalf("Sales must no longer be a Procurement leaf, found %+v", leaf)
			}
		}
	}
	if salesGroup == nil {
		t.Fatal("Sales nav group missing: Sales is its own vertical")
	}
	// The icon token must be one admin-web's iconByToken map knows, or the group silently renders
	// the Control Tower icon -- the defect `milk` and `wheat` each shipped with.
	if salesGroup.Icon != "banknote" {
		t.Fatalf("sales group icon = %q, want banknote", salesGroup.Icon)
	}
	if len(salesGroup.Leaves) != 1 || salesGroup.Leaves[0].Href != "/sales" {
		t.Fatalf("sales group leaves = %+v", salesGroup.Leaves)
	}

	// Breadcrumb rule, and the crumb copy: the page names itself, not the desk it used to sit
	// under.
	foundLabel := false
	for _, rule := range resp.RouteLabels {
		if rule.Pattern == "/sales" && rule.Label == "Sales" && rule.Match == "exact" {
			foundLabel = true
		}
		if rule.Pattern == "/procurement/sales" {
			t.Fatal("stale /procurement/sales route label survived the split")
		}
	}
	if !foundLabel {
		t.Fatal("route label for /sales missing")
	}
	if page.Copy["crumb"] != "Sales" {
		t.Fatalf("sales crumb = %q, want Sales", page.Copy["crumb"])
	}
}

// TestSalesRecordSaleControlIsCapabilityGated pins the write split: record_sale is ENABLED only
// for a holder of permissions.SalesWrite and is otherwise present-but-disabled with a backend
// reason. The verifier/growth-director rows are the mutation test: any change that enables the
// control from a broader permission (SalesRead, ProcurementRead, a role string) turns one red.
func TestSalesRecordSaleControlIsCapabilityGated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		enabled bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true},
		{"sales director", permissions.RoleKey(permissions.TierDirector, permissions.VerticalSales), true},
		{"sales assistant manager reads but cannot record", permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalSales), false},
		{"growth_director", permissions.RoleGrowthDirector, false},
		// The verifier is deliberately absent here: a review-without-act principal receives the
		// verifier LENS, which drops every non-verification page contract including sales.
		{"pc_director", permissions.RolePCDirector, false},
		{"operator holds procurement read but no sales write", permissions.RoleOperator, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			control := controlByID(t, pageByRouteID(t, resp.Pages, "sales").Controls, "record_sale")
			if control.Enabled != tc.enabled {
				t.Fatalf("%s record_sale.enabled = %v want %v (%#v)", tc.name, control.Enabled, tc.enabled, control)
			}
			if !tc.enabled && control.DisabledReason == "" {
				t.Fatalf("%s: disabled record_sale control must carry a backend disabled reason", tc.name)
			}
			if control.Action != "POST /sales/deals" {
				t.Fatalf("record_sale.action = %q want the record-sale write", control.Action)
			}
		})
	}
}

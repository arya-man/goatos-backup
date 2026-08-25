package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestSalesPageContractAndNavigation pins the sales page's published contract: the nav leaf under
// Procurement, the breadcrumb, the deals table shape, and the backend-owned copy the client
// renders verbatim.
func TestSalesPageContractAndNavigation(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})

	page := pageByRouteID(t, resp.Pages, "sales")
	if page.Href != "/procurement/sales" || page.PathPattern != "/procurement/sales" {
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

	// Nav: since the 2026-08-25 regrouping the Sales leaf lives in its OWN Sales
	// group (nav regrouping, not a route change — the href is unchanged), and it
	// must NOT reappear in Procurement.
	foundLeaf := false
	for _, group := range resp.Navigation.Groups {
		if group.ID == "procurement" {
			for _, leaf := range group.Leaves {
				if leaf.ID == "procurement-sales" {
					t.Fatal("procurement-sales leaf must not remain in the Procurement nav group after the Sales split")
				}
			}
		}
		if group.ID != "sales" {
			continue
		}
		for _, leaf := range group.Leaves {
			if leaf.ID == "procurement-sales" {
				foundLeaf = true
				if leaf.Href != "/procurement/sales" {
					t.Fatalf("sales leaf href = %q", leaf.Href)
				}
			}
		}
	}
	if !foundLeaf {
		t.Fatal("procurement-sales leaf missing from the Sales nav group")
	}

	// Breadcrumb rule.
	foundLabel := false
	for _, rule := range resp.RouteLabels {
		if rule.Pattern == "/procurement/sales" && rule.Label == "Sales" && rule.Match == "exact" {
			foundLabel = true
		}
	}
	if !foundLabel {
		t.Fatal("route label for /procurement/sales missing")
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

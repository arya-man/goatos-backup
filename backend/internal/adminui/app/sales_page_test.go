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
		"section.payments.title", "payments.received_so_far", "payments.balance", "payments.empty",
		"payments.column.received_on", "payments.column.amount", "payments.column.note",
		"field.received_on", "field.amount_rupees", "field.note", "hint.record_payment",
		"action.record_deal_payment.label", "action.payment_recorded", "action.payment_record_failed",
		"field.status", "hint.status", "action.update_deal_status.label",
		"action.deal_status_updated", "action.deal_status_update_failed",
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
	// Three leaves now: the board, Purchase & barn beside it (maintainer decision 2026-08-31),
	// and Sales Config last (maintainer decision 2026-09-01) -- the one place a sales fact is
	// entered or changed, which is why the two read leaves come first.
	if len(salesGroup.Leaves) != 3 ||
		salesGroup.Leaves[0].Href != "/sales" ||
		salesGroup.Leaves[1].Href != "/sales/loads" ||
		salesGroup.Leaves[1].Label != "Purchase & barn" ||
		salesGroup.Leaves[2].Href != "/sales/config" ||
		salesGroup.Leaves[2].Label != "Sales Config" {
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
// reason. It reads /sales/config, the ONE page carrying a sales write since 2026-09-01. The verifier/growth-director rows are the mutation test: any change that enables the
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
			pageControls := pageByRouteID(t, resp.Pages, "sales-config").Controls
			control := controlByID(t, pageControls, "record_sale")
			if control.Enabled != tc.enabled {
				t.Fatalf("%s record_sale.enabled = %v want %v (%#v)", tc.name, control.Enabled, tc.enabled, control)
			}
			if !tc.enabled && control.DisabledReason == "" {
				t.Fatalf("%s: disabled record_sale control must carry a backend disabled reason", tc.name)
			}
			if control.Action != "POST /sales/deals" {
				t.Fatalf("record_sale.action = %q want the record-sale write", control.Action)
			}
			// The buyer-receipt write rides the SAME permission split: money on the same ledger.
			payment := controlByID(t, pageControls, "record_sales_deal_payment")
			if payment.Enabled != tc.enabled {
				t.Fatalf("%s record_sales_deal_payment.enabled = %v want %v (%#v)", tc.name, payment.Enabled, tc.enabled, payment)
			}
			if !tc.enabled && payment.DisabledReason == "" {
				t.Fatalf("%s: disabled record_sales_deal_payment must carry a backend disabled reason", tc.name)
			}
			if payment.Action != "POST /sales/deals/{deal_id}/payments" {
				t.Fatalf("record_sales_deal_payment.action = %q want the receipt write", payment.Action)
			}
			status := controlByID(t, pageControls, "update_sales_deal_status")
			if status.Enabled != tc.enabled {
				t.Fatalf("%s update_sales_deal_status.enabled = %v want %v", tc.name, status.Enabled, tc.enabled)
			}
			if status.Action != "POST /sales/deals/{deal_id}/status" {
				t.Fatalf("update_sales_deal_status.action = %q want the status write", status.Action)
			}
		})
	}
}

// TestRecordLoadCostControlIsCapabilityGated pins the load-cost write split: record_load_cost is
// ENABLED only for a holder of permissions.LoadCostWrite and is otherwise present-but-disabled
// with a backend reason. It moved to /sales/config with every other sales entry (maintainer
// decision 2026-09-01) but kept its OWN permission -- sharing a page must not share authority,
// which is what the sales-director row below proves.
//
// The operator row is the mutation test that matters: an operator holds ProcurementWrite for the
// source-entry screens, so any change that enables this control from ProcurementWrite (or any
// broader procurement key) turns it red. The feed_director row covers the same leak from the feed
// oversight side, and growth_director covers a role with no procurement permission at all.
func TestRecordLoadCostControlIsCapabilityGated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		enabled bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true},
		{"procurement_director", permissions.RoleProcurementDirector, true},
		{"procurement_manager", permissions.RoleProcurementManager, true},
		// THE ROW THAT MATTERS since the write moved onto the shared config page: this director
		// holds SalesWrite, so every OTHER control on /sales/config is enabled for him. Costing a
		// load is the buying desk's money, not his, and any change that lets the page's own
		// permission carry this control turns this red.
		{"sales director holds sales write but no load cost write", permissions.RoleKey(permissions.TierDirector, permissions.VerticalSales), false},
		{"feed_director", permissions.RoleFeedDirector, false},
		{"growth_director", permissions.RoleGrowthDirector, false},
		{"operator holds procurement write but no load cost write", permissions.RoleOperator, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			control := controlByID(t, pageByRouteID(t, resp.Pages, "sales-config").Controls, "record_load_cost")
			if control.Enabled != tc.enabled {
				t.Fatalf("%s record_load_cost.enabled = %v want %v (%#v)", tc.name, control.Enabled, tc.enabled, control)
			}
			if !tc.enabled && control.DisabledReason == "" {
				t.Fatalf("%s: disabled control must carry a backend disabled reason", tc.name)
			}
			if control.Action != "PUT /procurement/loads/{load_id}/cost" {
				t.Fatalf("record_load_cost.action = %q want the load-cost write", control.Action)
			}
		})
	}
}

// TestSalesLoadsPageContract pins the Purchase & barn page: its own route under Sales, the
// load-wise table it serves, its two tabs with Purchased first, and the backend-owned copy the
// client renders verbatim.
//
// It is a SEPARATE page from the sales board (maintainer decision 2026-08-31): the board answers
// how sales are going, this answers how each batch of animals did.
func TestSalesLoadsPageContract(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})

	page := pageByRouteID(t, resp.Pages, "sales-loads")
	if page.Href != "/sales/loads" || page.SurfaceKind != "module-surface" {
		t.Fatalf("page href/kind = %q/%q", page.Href, page.SurfaceKind)
	}
	if page.Title != "Purchase & barn" {
		t.Fatalf("page title = %q, want the maintainer-chosen name", page.Title)
	}
	if len(page.Tables) != 1 || page.Tables[0].ID != "sales-loadwise" {
		t.Fatalf("tables = %+v", page.Tables)
	}
	if page.Tables[0].DataSource != "/procurement/loadwise-sales" {
		t.Fatalf("table source = %q", page.Tables[0].DataSource)
	}
	if page.Tables[0].RowClick.Param != "load_id" {
		t.Fatalf("row param = %q, want the load id the cost drawer opens on", page.Tables[0].RowClick.Param)
	}

	// The tabs, in order: Purchased is FIRST because the page selects it when the URL names none.
	var views []domain.Option
	for _, g := range page.OptionGroups {
		if g.ID == "sales_views" {
			views = g.Options
		}
	}
	if len(views) != 2 || views[0].Key != "purchased" || views[1].Key != "farm_born" {
		t.Fatalf("sales_views = %+v, want purchased then farm_born", views)
	}

	for _, key := range []string{
		"crumb", "value.none", "error.load",
		// Load-wise section (maintainer decision 2026-08-31): tabs, summary tiles, charts,
		// reconciliation columns and the load-cost drawer are all backend-owned copy.
		"tab.purchased", "tab.farm_born",
		"section.loadwise.title", "section.loadwise.subtitle", "empty.loadwise", "empty.farm_born",
		"loadwise.kpi.purchased", "loadwise.kpi.sold", "loadwise.kpi.mortality",
		"loadwise.kpi.remaining", "loadwise.kpi.purchase_value", "loadwise.kpi.sold_value",
		"loadwise.kpi.profit", "loadwise.kpi.profit.hint",
		"chart.loadwise_counts.title", "chart.loadwise_counts.empty",
		"chart.loadwise_value.title", "chart.loadwise_value.empty",
		"chart.series.purchased", "chart.series.sold_count", "chart.series.mortality",
		"chart.series.remaining", "chart.series.purchase_value", "chart.series.sold_value",
		"chart.series.profit_loss",
		"column.load", "column.purchased", "column.sold", "column.mortality",
		"column.other_exits", "column.remaining", "column.unaccounted",
		"column.purchase_value", "column.sold_value", "column.profit_loss",
		"column.remaining_value", "value.profit_unrealised", "value.profit_unavailable",
		"value.profit_incl_stock", "loadwise.stock_price_note", "loadwise.stock_price_each",
		"loadwise.stock_price_unknown",
		"value.cost_missing", "value.price_basis.load", "value.price_basis.overall",
		// All THREE bases must be published: the renderer resolves this key from the served
		// price_basis, so an unpublished value throws and takes the whole page down. "none" is
		// what a tenant with no sales yet returns, i.e. the very first state.
		"value.price_basis.none",
		"value.sold_unpriced",
		"loadwise.prior.title", "loadwise.prior.sold", "loadwise.prior.died",
		"drawer.load_cost.title", "field.animal_cost", "field.transport_cost", "field.other_cost",
		"hint.load_cost", "action.record_load_cost.label", "action.load_cost_recorded",
		"action.load_cost_record_failed", "disabled.load_cost",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("sales-loads copy missing %q", key)
		}
	}
}

// TestSalesLoadsNavLeafRidesSalesRead pins that the new leaf is gated on the SALES permission and
// not on ProcurementRead, which operators and park heads hold for the intake screens they work.
func TestSalesLoadsNavLeafRidesSalesRead(t *testing.T) {
	required := permissionsForNav("sales-loads")
	if len(required) != 1 || required[0] != permissions.SalesRead {
		t.Fatalf("permissionsForNav(sales-loads) = %v, want exactly SalesRead", required)
	}
}

// TestSalesReadPagesCarryNoWriteControl is the other half of the 2026-09-01 decision, and the half
// a "Sales Config exists" test would pass while the product was still wrong: /sales and
// /sales/loads must declare NO write control at all.
//
// A control is what the renderer gates its buttons and forms on, so a page that declares none
// renders none -- that is what makes those two pages read-only, and it is why the assertion is
// "the page has no control with this id" rather than "the control is disabled". A disabled control
// is a page that still offers the action and explains why you may not take it; these two pages
// must not offer it to anyone, including the CEO.
//
// Run with the CEO's grants deliberately: he holds SalesWrite and LoadCostWrite, so if the write
// were still compiled onto these pages for anybody it would be for him.
func TestSalesReadPagesCarryNoWriteControl(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})

	writes := []string{
		"record_sale",
		"record_pipeline",
		"record_sales_deal_payment",
		"update_sales_deal_status",
		"record_load_cost",
	}
	for _, pageID := range []string{"sales", "sales-loads"} {
		page := pageByRouteID(t, resp.Pages, pageID)
		for _, control := range page.Controls {
			for _, banned := range writes {
				if control.ID == banned {
					t.Fatalf("%s still declares the %q write; entry lives on /sales/config only", pageID, banned)
				}
			}
		}
	}

	// And the same five ARE on the config page, or the writes moved nowhere.
	config := pageByRouteID(t, resp.Pages, "sales-config")
	for _, id := range writes {
		control := controlByID(t, config.Controls, id)
		if !control.Enabled {
			t.Fatalf("sales-config control %q is disabled for the CEO, who holds every sales permission", id)
		}
	}
}

// TestSalesConfigPageContract pins the entry page: its route under Sales, the two tables whose
// rows open a form, and the backend-owned copy the client renders verbatim.
//
// The copy list is deliberately drawn from BOTH read pages plus the page's own headings, because
// this one page mounts every drawer they used to: the record-sale form, the tag-animals flow, the
// five pipeline panels and the load-cost drawer. A key missing here is a label the entry form
// cannot render, on the only screen that can record the fact.
func TestSalesConfigPageContract(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})

	page := pageByRouteID(t, resp.Pages, "sales-config")
	if page.Href != "/sales/config" || page.PathPattern != "/sales/config" {
		t.Fatalf("sales-config href/pattern = %q/%q", page.Href, page.PathPattern)
	}
	// A module surface, NOT an authority screen: the IA rule keeps Config as a top-level
	// authority lens, and this is Sales' own data-entry surface -- the /feed/config shape.
	if page.SurfaceKind != "module-surface" {
		t.Fatalf("sales-config must be a module surface, got %q", page.SurfaceKind)
	}
	if page.Title != "Sales Config" {
		t.Fatalf("page title = %q", page.Title)
	}

	// Both tables, and each must declare the param its row click opens a FORM on.
	if len(page.Tables) != 2 || page.Tables[0].ID != "sales-deals" || page.Tables[1].ID != "sales-loadwise" {
		t.Fatalf("sales-config tables = %+v", page.Tables)
	}
	if page.Tables[0].RowClick.Param != "deal_id" {
		t.Fatalf("deals row param = %q, want the id the deal form opens on", page.Tables[0].RowClick.Param)
	}
	if page.Tables[1].RowClick.Param != "load_id" {
		t.Fatalf("loadwise row param = %q, want the id the cost drawer opens on", page.Tables[1].RowClick.Param)
	}

	for _, key := range []string{
		"crumb", "value.none", "error.load",
		// Its own headings.
		"section.sales_entry.title", "section.sales_entry.subtitle", "section.sales_entry.row_hint",
		"section.pipeline_entry.title", "section.pipeline_entry.subtitle",
		"section.load_entry.title", "section.load_entry.subtitle", "section.load_entry.row_hint",
		"empty.loads", "hint.read_only", "link.sales_board", "link.sales_loads",
		// Inherited from the board: the record-sale form, the payments block, the status edit,
		// the vendor select and the tag-animals flow.
		"action.record_sale.label", "field.sale_date", "field.farm", "field.product_type",
		"field.breed", "field.buyer_name", "field.total_weight_kg", "field.sales_value",
		"field.vendor", "select.vendor.placeholder", "hint.vendor",
		"section.payments.title", "field.received_on", "field.amount_rupees",
		"action.record_deal_payment.label", "field.status", "action.update_deal_status.label",
		"action.tag_animals.label", "action.tag_animals.hint", "action.confirm_sold",
		"disabled.write",
		// Inherited from Purchase & barn: the load-cost drawer.
		"drawer.load_cost.title", "field.animal_cost", "field.transport_cost", "field.other_cost",
		"hint.load_cost", "action.record_load_cost.label", "disabled.load_cost",
		"column.load", "column.purchased", "column.sold", "column.remaining",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("sales-config copy missing %q", key)
		}
	}

	// The form vocabulary, shared with the board rather than copied: a form offering different
	// breeds from the page that reads them back is exactly the drift the merge avoids.
	groups := map[string]int{}
	for _, g := range page.OptionGroups {
		groups[g.ID] = len(g.Options)
	}
	for _, id := range []string{"sales_farms", "sales_product_types", "sales_deal_statuses", "sales_breeds_goat"} {
		if groups[id] == 0 {
			t.Fatalf("sales-config option group %q missing", id)
		}
	}
}

// TestSalesConfigNavLeafRidesSalesRead pins that the entry page is REACHED on SalesRead, not on
// SalesWrite. A reader who cannot record still opens it and sees each control disabled with its
// backend reason -- the health-config shape. Gating the leaf on the write would make the page
// vanish for them, which reads as a broken product rather than an answer.
func TestSalesConfigNavLeafRidesSalesRead(t *testing.T) {
	required := permissionsForNav("sales-config")
	if len(required) != 1 || required[0] != permissions.SalesRead {
		t.Fatalf("permissionsForNav(sales-config) = %v, want exactly SalesRead", required)
	}
}

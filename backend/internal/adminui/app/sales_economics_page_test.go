package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestSalesEconomicsPageContractAndNavigation pins the Economics page's published
// contract: the page shape, both renderer tables with backend-owned labels, the
// copy the client renders verbatim, the nav leaf under the Sales group, and the
// breadcrumb rule.
func TestSalesEconomicsPageContractAndNavigation(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})

	page := pageByRouteID(t, resp.Pages, "sales-economics")
	if page.Href != "/sales/economics" || page.PathPattern != "/sales/economics" {
		t.Fatalf("economics href/pattern = %q/%q", page.Href, page.PathPattern)
	}
	if page.SurfaceKind != "module-surface" {
		t.Fatalf("economics must be a module surface, got %q", page.SurfaceKind)
	}

	if len(page.Tables) != 2 || page.Tables[0].ID != "economics-animals" || page.Tables[1].ID != "economics-sold" {
		t.Fatalf("economics tables = %+v", page.Tables)
	}
	for _, tbl := range page.Tables {
		if tbl.DataSource != "/economics/overview" {
			t.Fatalf("table %q source = %q", tbl.ID, tbl.DataSource)
		}
		// There is no economics record to open; a declared row click the page
		// cannot honour would be a contract lie.
		if tbl.RowClick.Param != "" {
			t.Fatalf("table %q must not declare a row click, got %q", tbl.ID, tbl.RowClick.Param)
		}
		for _, column := range tbl.Columns {
			if column.Label == "" {
				t.Fatalf("table %q column %q has no backend-owned label", tbl.ID, column.Key)
			}
		}
	}

	// The column ORDER is part of the contract (see feed_purchase_page_test.go).
	wantAnimalColumns := []string{
		"tag", "animal", "shed", "latest_weight", "adg",
		"feed_cost_day", "cost_per_kg_gain", "value_per_day", "net_per_day", "signal",
	}
	if len(page.Tables[0].Columns) != len(wantAnimalColumns) {
		t.Fatalf("animal columns = %+v", page.Tables[0].Columns)
	}
	for i, want := range wantAnimalColumns {
		if page.Tables[0].Columns[i].Key != want {
			t.Fatalf("animal column %d = %q want %q", i, page.Tables[0].Columns[i].Key, want)
		}
	}

	// Backend-owned copy: every key the renderer needs must be published.
	for _, key := range []string{
		"crumb", "filter.park.all", "filter.window", "period.covering",
		"section.pulse.title", "section.pulse.aria",
		"kpi.feed_burn", "kpi.feed_burn.hint",
		"kpi.value_added", "kpi.value_added.hint",
		"kpi.realized_price", "kpi.realized_price.window", "kpi.realized_price.trailing_year", "kpi.realized_price.none",
		"kpi.cost_per_kg_gain", "kpi.cost_per_kg_gain.hint",
		"kpi.sold", "kpi.sold.detail", "kpi.deals", "kpi.animals",
		"trust.title", "trust.weighed", "trust.paired", "trust.costed",
		"trust.unpriced_items", "trust.estimate", "trust.deals_scope",
		"section.bands.title", "section.bands.subtitle",
		"bands.animals", "bands.gain", "bands.cost", "bands.value", "bands.net",
		"bands.sell", "bands.keep", "bands.unknown", "empty.bands",
		"section.animals.title", "section.animals.subtitle",
		"signal.earning", "signal.burning", "signal.watch",
		"value.none", "value.kg_suffix", "value.g_per_day_suffix", "value.per_day_suffix", "value.per_kg_suffix",
		"animals.capped", "empty.animals",
		"section.sold.title", "section.sold.subtitle", "empty.sold",
		"error.load",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("economics copy missing %q", key)
		}
	}

	// Nav: the leaf lives in the Sales group beside the Sales page.
	foundLeaf := false
	for _, group := range resp.Navigation.Groups {
		if group.ID != "sales" {
			continue
		}
		for _, leaf := range group.Leaves {
			if leaf.ID == "sales-economics" {
				foundLeaf = true
				if leaf.Href != "/sales/economics" {
					t.Fatalf("economics leaf href = %q", leaf.Href)
				}
			}
		}
	}
	if !foundLeaf {
		t.Fatal("sales-economics leaf missing from the Sales nav group")
	}

	// Breadcrumb rule.
	foundLabel := false
	for _, rule := range resp.RouteLabels {
		if rule.Pattern == "/sales/economics" && rule.Label == "Economics" && rule.Match == "exact" {
			foundLabel = true
		}
	}
	if !foundLabel {
		t.Fatal("route label for /sales/economics missing")
	}
}

// TestSalesEconomicsNavLeafIsLeadershipOnly pins that the Economics leaf's
// permission is its OWN, not SalesRead (maintainer decision 2026-08-25).
//
// The leak this guards: the sales director and procurement director both hold
// SalesRead for the deals board, and the growth director holds the weighing
// oversight the gain figures come from — gating the leaf on any of those would
// put feed spend, sale values and margins side by side in a director's sidebar.
// Those three roles are the mutation test: enabling the leaf from a broader
// permission turns one of them red.
func TestSalesEconomicsNavLeafIsLeadershipOnly(t *testing.T) {
	required := permissionsForNav("sales-economics")
	if len(required) != 1 || required[0] != permissions.SalesEconomicsRead {
		t.Fatalf("sales-economics nav permissions = %v want [%s]", required, permissions.SalesEconomicsRead)
	}
	salesDirector := permissions.RoleKey(permissions.TierDirector, permissions.VerticalSales)
	for _, role := range []string{permissions.RoleProcurementDirector, salesDirector} {
		if permissions.RolesAuthorize([]string{role}, required, false) {
			t.Fatalf("%s must not reach the economics leaf", role)
		}
		if !permissions.RolesAuthorize([]string{role}, []string{permissions.SalesRead}, false) {
			t.Fatalf("%s is expected to hold SalesRead -- otherwise this test proves nothing", role)
		}
	}
	if permissions.RolesAuthorize([]string{permissions.RoleGrowthDirector}, required, false) {
		t.Fatal("growth_director must not reach the economics leaf")
	}
	if !permissions.RolesAuthorize([]string{permissions.RoleCEOInternal}, required, false) {
		t.Fatal("ceo_internal must reach the economics leaf")
	}
}

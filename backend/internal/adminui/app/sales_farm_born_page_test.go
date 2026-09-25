package app

import (
	"context"
	"testing"
)

// TestSalesFarmBornPageContract pins the Farm born page (maintainer request 2026-09-18): its own
// route under Sales beside Load wise, read-only by contract, one sold-animal ledger served by the
// procurement farm-born read with farm-worded column labels, the Sex / Species vocabularies
// and NO origin control, and every copy key the renderer needs -- including the pen-not-shed rule.
func TestSalesFarmBornPageContract(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	page := pageByRouteID(t, resp.Pages, "sales-farm-born")
	if page.Href != "/sales/farm-born" || page.PathPattern != "/sales/farm-born" || page.SurfaceKind != "module-surface" {
		t.Fatalf("page = %q/%q/%q", page.Href, page.PathPattern, page.SurfaceKind)
	}
	// READ-ONLY by contract: no control at all, the /sales/sold shape.
	if len(page.Controls) != 0 {
		t.Fatalf("farm born page must declare no control; got %+v", page.Controls)
	}
	if len(page.Tables) != 1 || page.Tables[0].ID != "sales-farm-born-sold" {
		t.Fatalf("tables = %+v", page.Tables)
	}
	table := page.Tables[0]
	if table.DataSource != "/procurement/farm-born-sales" {
		t.Fatalf("data source = %q", table.DataSource)
	}
	if table.RowClick.Enabled {
		t.Fatalf("sold table must not declare a row click: %+v", table.RowClick)
	}
	if len(table.PageSizeOptions) == 0 || table.PageSizeOptions[0] != 25 {
		t.Fatalf("page sizes = %v", table.PageSizeOptions)
	}
	labels := map[string]string{}
	for _, c := range table.Columns {
		labels[c.Key] = c.Label
		if !c.Sortable {
			t.Fatalf("column %q must sort", c.Key)
		}
	}
	for key, want := range map[string]string{
		"tag": "Tag", "breed": "Breed", "sex": "Sex", "stage": "Stage", "pen": "Pen",
		"sale_date": "Sold on", "buyer_name": "Buyer", "sale_value": "Value",
	} {
		if labels[key] != want {
			t.Fatalf("column %q label = %q, want %q", key, labels[key], want)
		}
	}

	groups := map[string][]string{}
	for _, g := range page.OptionGroups {
		for _, o := range g.Options {
			groups[g.ID] = append(groups[g.ID], o.Key)
		}
	}
	// No origin control (maintainer instruction 2026-09-18): the page is farm born, full stop.
	if _, present := groups["farm_born_origins"]; present {
		t.Fatal("farm born page must serve no origin option group")
	}
	if got := groups["farm_born_sexes"]; len(got) != 2 || got[0] != "male" || got[1] != "female" {
		t.Fatalf("farm_born_sexes = %v", got)
	}
	if got := groups["farm_born_species"]; len(got) != 2 || got[0] != "goat" || got[1] != "sheep" {
		t.Fatalf("farm_born_species = %v", got)
	}

	for _, key := range []string{
		"crumb",
		"filter.period.label", "filter.period.field", "filter.period.today", "filter.period.single", "filter.period.range",
		"filter.period.aria", "filter.period.previous_month", "filter.period.next_month",
		"filter.period.range_start_hint", "filter.period.range_end_hint", "filter.period.range_separator",
		"filter.farm", "filter.pen.label", "filter.species.label", "filter.breed.label",
		"filter.sex.label", "filter.stage.label", "filter.all_option",
		"filter.bar_aria", "filter.apply", "filter.clear_all",
		"section.headline.aria", "kpi.on_farm", "kpi.on_farm.detail", "kpi.sold", "kpi.sold.detail",
		"kpi.revenue", "kpi.revenue.detail", "kpi.revenue.unpriced", "kpi.avg_price", "kpi.avg_price.detail",
		"section.breakdowns.title", "section.breakdowns.subtitle",
		"section.by_breed.title", "section.by_sex.title", "section.by_stage.title", "section.by_pen.title",
		"column.on_farm", "column.sold", "column.revenue", "column.share_pct", "empty.breakdown",
		"section.sold.title", "section.sold.subtitle", "section.sold.aria",
		"empty.sold", "value.no_deal", "value.not_recorded", "pager.noun", "pager.pens", "error.load",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("copy %q missing", key)
		}
	}

	found := false
	for _, rule := range resp.RouteLabels {
		if rule.Pattern == "/sales/farm-born" && rule.Label == "Farm born" && rule.Match == "exact" {
			found = true
		}
	}
	if !found {
		t.Fatal("route label for /sales/farm-born missing")
	}
}

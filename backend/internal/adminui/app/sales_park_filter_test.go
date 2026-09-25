package app

import (
	"context"
	"strings"
	"testing"
)

// TestEverySalesReadPageCarriesTheSameFarmChips pins ONE park filter across Sales (defect
// 2026-09-25): Summary, Farm value, Buyer analytics, Load wise and Farm born each render the same
// farm chips, which write the shell's `park` parameter so the sidebar carries the choice between
// them. A page missing the group or its label would render no chips (or an empty label) and fall
// back to a second, different control -- the disagreement this fixes.
func TestEverySalesReadPageCarriesTheSameFarmChips(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	for _, routeID := range []string{
		"sales-sold", "sales-farm-value", "sales-buyer-analytics", "sales-loads", "sales-farm-born",
	} {
		page := pageByRouteID(t, resp.Pages, routeID)
		var keys []string
		for _, group := range page.OptionGroups {
			if group.ID != "sales_farms" {
				continue
			}
			for _, option := range group.Options {
				keys = append(keys, option.Key)
			}
		}
		// All farms first, then CBE before CPT: the order of every All-parks surface.
		if got := strings.Join(keys, ","); got != "all,CBE,CPT" {
			t.Fatalf("%s sales_farms = %q, want all,CBE,CPT", routeID, got)
		}
		if page.Copy["filter.farm"] == "" {
			t.Fatalf("%s is missing the farm chips' label filter.farm", routeID)
		}
	}
}

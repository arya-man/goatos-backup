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
//
// The chips are the tenant's parks (a sale's farm is any park, migration 000438), compiled from the
// park catalog -- never a CBE/CPT list -- so the expectation is built from the same families.
func TestEverySalesReadPageCarriesTheSameFarmChips(t *testing.T) {
	families, err := fakeFamilies{}.LoadContractFamilies(context.Background(), "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"all"}
	for _, park := range families.Parks {
		if park.Code != "" {
			want = append(want, park.Code)
		}
	}
	if len(want) < 2 {
		t.Fatal("fixture carries no coded park; the chips would be only All farms")
	}
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
		// All farms first, then every coded park in park order: the same chips on every page.
		if got := strings.Join(keys, ","); got != strings.Join(want, ",") {
			t.Fatalf("%s sales_farms = %q, want %q", routeID, got, strings.Join(want, ","))
		}
		if page.Copy["filter.farm"] == "" {
			t.Fatalf("%s is missing the farm chips' label filter.farm", routeID)
		}
	}
}

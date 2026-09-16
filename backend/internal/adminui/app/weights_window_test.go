package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

type weightsWindowFamilies struct {
	fakeFamilies
	rules *weighingdomain.WeightsPagesRules
}

func (f weightsWindowFamilies) LoadContractFamilies(ctx context.Context, tenantID string) (ReferenceFamilies, error) {
	families, err := f.fakeFamilies.LoadContractFamilies(ctx, tenantID)
	families.WeighingWeightsPages = f.rules
	return families, err
}

func weightsPagesOf(t *testing.T, resp domain.BootstrapResponse) map[string]domain.PageContract {
	t.Helper()
	out := map[string]domain.PageContract{}
	for _, page := range resp.Pages {
		if page.RouteID == "weighing-weights" || page.RouteID == "weighing-analytics" {
			out[page.RouteID] = page
		}
	}
	if len(out) != 2 {
		t.Fatalf("want both Weights pages, got %v", out)
	}
	return out
}

// The Weights / ADG Analytics window is the PUBLISHED weighing SOP's weights_pages block
// (maintainer request 2026-09-16), served as copy on BOTH pages so they cannot open on
// different periods; with no published version the seed -- today's constants -- is served.
func TestWeightsPagesWindowComesFromThePublishedWeighingSOP(t *testing.T) {
	input := BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants:   []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"}},
	}
	seeded := weightsPagesOf(t, NewService(weightsWindowFamilies{}).Bootstrap(context.Background(), input))
	for id, page := range seeded {
		if page.Copy["weights.window.default_from_mode"] != "fixed_date" || page.Copy["weights.window.default_from_date"] != "2026-08-03" || page.Copy["weights.window.earliest_date"] != "2026-07-05" {
			t.Fatalf("%s without a published version: copy = %v, want the seeded window", id, page.Copy)
		}
	}
	published := weightsPagesOf(t, NewService(weightsWindowFamilies{rules: &weighingdomain.WeightsPagesRules{DefaultFromMode: "rolling_days", DefaultFromDays: 45, EarliestDate: "2026-08-10"}}).Bootstrap(context.Background(), input))
	for id, page := range published {
		if page.Copy["weights.window.default_from_mode"] != "rolling_days" || page.Copy["weights.window.default_from_days"] != "45" || page.Copy["weights.window.earliest_date"] != "2026-08-10" {
			t.Fatalf("%s: copy = %v, want the published rolling window", id, page.Copy)
		}
	}
}

func TestWeightsPagesRollingWeeksCopy(t *testing.T) {
	rules := &weighingdomain.WeightsPagesRules{DefaultFromMode: weighingdomain.WeightsFromRollingWeeks, DefaultFromWeeks: 6, EarliestDate: "2026-07-05"}
	copy := withWeightsWindowCopy(nil, rules)
	if copy["weights.window.default_from_mode"] != "rolling_weeks" || copy["weights.window.default_from_weeks"] != "6" {
		t.Fatalf("rolling weeks copy = %v", copy)
	}
}

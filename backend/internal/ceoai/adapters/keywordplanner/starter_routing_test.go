package keywordplanner

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

func TestStarterQuestionsRouteToAnswerableTools(t *testing.T) {
	cases := map[string]struct {
		tool  string
		route domain.Route
	}{
		"How many active goats and sheep do we have?":           {"active_animals", domain.RouteCube},
		"What vaccinations are overdue by park?":                {"vaccination_overdue", domain.RouteCube},
		"How many vaccinations are due today?":                  {"vaccination_due", domain.RouteCube},
		"Which operators are overloaded on vaccination drives?": {"operator_vaccination_utilization", domain.RouteCube},
		"What vaccines need pickup today?":                      {"mesha_vaccination_dose_pickup", domain.RouteToolbox},
		"What feed direction is pending today?":                 {"feed_direction_today", domain.RouteAPI},
		"Which shifting movements are pending?":                 {"mesha_shifting_summary", domain.RouteToolbox},
		"What procurement loads need attention?":                {"procurement_source_entry_loads", domain.RouteAPI},
		"What source-entry health issues exist?":                {"mesha_source_entry_health", domain.RouteToolbox},
		"What SOP execution is blocked?":                        {"mesha_sop_execution", domain.RouteToolbox},
		"What verification items are waiting?":                  {"verification_queue", domain.RouteAPI},
		"Where are sheds over capacity?":                        {"admin_location_usage", domain.RouteAPI},
		"What workforce coverage gaps exist?":                   {"admin_roster_coverage", domain.RouteAPI},
		"What operation exceptions are open?":                   {"operations_kernel_health", domain.RouteAPI},
		"What inventory stock needs reorder?":                   {"mesha_inventory_stock", domain.RouteToolbox},
		"Summarize the operations audit anomalies.":             {"operations_audit_summary", domain.RouteAPI},
		"Plot vaccination overdue by park.":                     {"vaccination_overdue", domain.RouteCube},
	}
	p := New()
	for q, want := range cases {
		pl, err := p.Plan(context.Background(), domain.Question{Text: q}, nil, nil)
		if err != nil {
			t.Fatalf("%q: plan err: %v", q, err)
		}
		if len(pl.SubQuestions) == 0 {
			t.Fatalf("%q: no sub-questions", q)
		}
		s := pl.SubQuestions[0]
		if s.Route != want.route {
			t.Errorf("%q: route=%v want %v", q, s.Route, want.route)
		}
		if s.ToolName != want.tool {
			t.Errorf("%q: tool=%s want %s", q, s.ToolName, want.tool)
		}
	}
}

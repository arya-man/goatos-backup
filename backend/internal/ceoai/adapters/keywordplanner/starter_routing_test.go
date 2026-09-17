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
		"How many animals were sold this month?":      {"sales_overview", domain.RouteAPI},
		"What is sales revenue this month?":           {"sales_overview", domain.RouteAPI},
		"Which customers bought the most this month?": {"sales_overview", domain.RouteAPI},
		"Who are the top buyers by sales revenue?":    {"sales_overview", domain.RouteAPI},
		"How many active goats and sheep do we have?": {"active_animals", domain.RouteCube},
		"What feed direction is pending today?":       {"feed_direction_today", domain.RouteAPI},
		"What procurement loads need attention?":      {"procurement_source_entry_loads", domain.RouteAPI},
		"What source-entry health issues exist?":      {"mesha_source_entry_health", domain.RouteToolbox},
		"What operation exceptions are open?":         {"operations_kernel_health", domain.RouteAPI},
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

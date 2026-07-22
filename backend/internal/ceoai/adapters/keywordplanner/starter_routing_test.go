package keywordplanner

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// TestStarterQuestionsRouteToCube locks the 5 leadership starter questions to the
// Cube tier (correct real data), guarding against the regression where
// "How many active goats and sheep" fell through to the broken counts_breakdown
// API route (sheep=0, no executor).
func TestStarterQuestionsRouteToCube(t *testing.T) {
	cases := map[string]string{
		"How many active goats and sheep do we have?": "active_animals",
		"What vaccinations are overdue by park?":      "vaccination_overdue",
		"How many vaccinations are due today?":        "vaccination_due",
		"Which sheds are behind on vaccination?":      "vaccination_overdue",
		"Plot vaccination overdue by park":            "vaccination_overdue",
	}
	p := New()
	for q, wantTool := range cases {
		pl, err := p.Plan(context.Background(), domain.Question{Text: q}, nil, nil)
		if err != nil {
			t.Fatalf("%q: plan err: %v", q, err)
		}
		if len(pl.SubQuestions) == 0 {
			t.Fatalf("%q: no sub-questions", q)
		}
		s := pl.SubQuestions[0]
		if s.Route != domain.RouteCube {
			t.Errorf("%q: route=%v want Cube (must not hit unwired API tools)", q, s.Route)
		}
		if s.ToolName != wantTool {
			t.Errorf("%q: tool=%s want %s", q, s.ToolName, wantTool)
		}
	}
}

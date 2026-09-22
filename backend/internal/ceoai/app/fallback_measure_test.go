package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// fallbackFit is the only thing between a model-free plan and the answer, and
// its measure check reads MeasureTerms -- which only natural-SQL templates set.
// A keyword-planned sub declared nothing, so the strictest gate in the fallback
// path was skipped for exactly the plans that have no other gate.
func TestAModelFreePlanDeclaresTheMeasureItWillAnswer(t *testing.T) {
	catalog := []ports.ToolSpec{{
		Name:        "weighing_progress",
		Route:       domain.RouteAPI,
		Description: "How far a weighing task has got",
	}}
	subs := []domain.SubQuestion{{ID: "s1", ToolName: "weighing_progress", Route: domain.RouteAPI}}

	declareFallbackMeasures(subs, catalog)
	if subs[0].Declared.IsZero() {
		t.Fatal("the sub still declares nothing, so fallbackFit's measure check cannot run")
	}

	asked := domain.Question{Text: "how is the weighing going"}
	if issues := fallbackFit(asked, RequestedShape{}, subs); len(issues) > 0 {
		t.Errorf("a question naming the measure was refused: %+v", issues)
	}

	unrelated := domain.Question{Text: "how much did we pay the vendor"}
	if issues := fallbackFit(unrelated, RequestedShape{}, subs); len(issues) == 0 {
		t.Error("a question sharing no word with the read that would run was answered anyway")
	}
}

// A spec the planner already declared is never overwritten: a natural-SQL
// template knows what it computes better than its tool name does.
func TestADeclaredSpecSurvivesTheFallbackDeclaration(t *testing.T) {
	subs := []domain.SubQuestion{{
		ID: "s1", ToolName: "weighing_progress",
		Declared: domain.AnswerSpec{Template: true, MeasureTerms: []string{"average weight"}},
	}}
	declareFallbackMeasures(subs, []ports.ToolSpec{{Name: "weighing_progress"}})
	if got := subs[0].Declared.MeasureTerms; len(got) != 1 || got[0] != "average weight" {
		t.Errorf("the template's own measure terms were overwritten: %v", got)
	}
}

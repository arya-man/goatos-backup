package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// A model-drafted SQL read writes its OWN row labels, so the label alone can
// claim anything: the live judge saw "Delayed tasks by category: 32" rendered
// from a query that had read the vaccination-obligations view. The block is
// titled with the view that actually ran — the one part of it the model did not
// choose — so the substitution is on the page instead of hidden.
func TestModelDraftedBlockIsTitledWithTheViewThatRan(t *testing.T) {
	r := domain.ToolResult{
		Route: domain.RouteSQL, ToolName: "sql_fallback", Surface: "Mesha operational data",
		SourceView: "vaccination_obligations_base",
		Facts:      []domain.Fact{{TenantID: probeTenant, Label: "Delayed tasks by category", Value: "32"}},
	}
	body := mustCompose(t, r)
	if !strings.Contains(body, "Vaccination obligations") {
		t.Errorf("block does not name the view it read: %q", body)
	}
	if !strings.Contains(body, "32") {
		t.Errorf("grounded figure dropped: %q", body)
	}

	// A curated route (Cube, a read API, the toolbox) does not let the model
	// name the metric, so nothing is prefixed and those answers read as before.
	curated := domain.ToolResult{
		Route: domain.RouteCube, Surface: "Cube · vaccination_overdue",
		Facts: []domain.Fact{{TenantID: probeTenant, Label: "Vaccinations overdue", Value: "7"}},
	}
	if body := mustCompose(t, curated); strings.Contains(body, "From ") {
		t.Errorf("a curated read must not be re-titled: %q", body)
	}
}

func TestViewTitleIsDerivedFromTheViewName(t *testing.T) {
	for view, want := range map[string]string{
		"vaccination_obligations_base": "Vaccination obligations",
		"workforce_tasks_base":         "Workforce tasks",
		"weighing_capture_activity":    "Weighing capture activity",
		"":                             "",
	} {
		if got := viewTitle(view); got != want {
			t.Errorf("viewTitle(%q) = %q, want %q", view, got, want)
		}
	}
}

// Grounding is PER SECTION. Pooling every result's numbers let a figure read by
// step A ground a sentence carrying step B's label — the answer passed review
// because the number existed somewhere in the evidence, which is exactly the
// "borrow a nearby number" shape the judge found.
func TestANumberMustBeGroundedByTheEvidenceItsOwnLineReports(t *testing.T) {
	pcCare := domain.ToolResult{Route: domain.RouteSQL, SourceView: "pc_care_tasks",
		Facts: []domain.Fact{{TenantID: probeTenant, Label: "Delayed tasks", Value: "0"}}}
	vaccination := domain.ToolResult{Route: domain.RouteSQL, SourceView: "vaccination_obligations_base",
		Facts: []domain.Fact{{TenantID: probeTenant, Label: "Obligations", Value: "32"}}}

	borrowed := []answerSection{
		{text: "Delayed tasks by category: 32.", result: pcCare},
		{text: "Obligations: 32.", result: vaccination},
	}
	verdict := reviewer{}.reviewSections(context.Background(), "Delayed tasks by category: 32.",
		borrowed, []domain.ToolResult{pcCare, vaccination}, 0)
	if verdict.Grounded {
		t.Fatalf("a figure from the vaccination read grounded a PC-care line: %+v", verdict.FailReasons)
	}

	honest := []answerSection{
		{text: "Delayed tasks: 0.", result: pcCare},
		{text: "Obligations: 32.", result: vaccination},
	}
	if v := (reviewer{}).reviewSections(context.Background(), "Delayed tasks: 0.\n\nObligations: 32.",
		honest, []domain.ToolResult{pcCare, vaccination}, 0); !v.Grounded {
		t.Errorf("each line grounded by its own read must pass: %+v", v.FailReasons)
	}
}

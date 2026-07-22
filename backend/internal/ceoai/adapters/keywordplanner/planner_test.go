package keywordplanner

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

func plan(t *testing.T, text string) domain.Plan {
	t.Helper()
	p := New()
	pl, err := p.Plan(context.Background(), domain.Question{Text: text}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pl
}

func TestKPIRoutesToCube(t *testing.T) {
	cases := map[string]struct{ tool string }{
		"how many goats do we have":  {"active_animals"},
		"which sheds are overdue":    {"vaccination_overdue"},
		"vaccination adherence week": {"vaccination_compliance"},
		"mortality this month":       {"mortality_rate"},
		"what is due today":          {"vaccination_due"},
	}
	for q, want := range cases {
		pl := plan(t, q)
		if len(pl.SubQuestions) == 0 {
			t.Fatalf("%q: no sub-questions", q)
		}
		s := pl.SubQuestions[0]
		if s.ToolName != want.tool {
			t.Errorf("%q: tool=%q want %q", q, s.ToolName, want.tool)
		}
		if s.Route != domain.RouteCube {
			t.Errorf("%q: route=%q want cube", q, s.Route)
		}
	}
}

func TestOperatorVaccinationRoutesToCube(t *testing.T) {
	cases := map[string]string{
		"which operators are behind on vaccination": "operator_vaccination_overdue",
		"which vaccinator is overloaded":            "operator_vaccination_utilization",
		"operator capacity today":                   "operator_vaccination_capacity",
		"operator drive assignments today":          "operator_vaccination_load",
		"how many animals is the operator assigned": "operator_vaccination_load",
	}
	for q, want := range cases {
		pl := plan(t, q)
		if len(pl.SubQuestions) == 0 {
			t.Fatalf("%q: no sub-questions", q)
		}
		s := pl.SubQuestions[0]
		if s.ToolName != want {
			t.Errorf("%q: tool=%q want %q", q, s.ToolName, want)
		}
		if s.Route != domain.RouteCube {
			t.Errorf("%q: route=%q want cube", q, s.Route)
		}
	}
}

// TestOperatorQuestionsCarryOperatorGroupBy proves the deterministic planner
// attaches group_by=operator_label to operator drive questions, so the Cube
// metric returns one row per NAMED operator instead of an ungrounded total.
func TestOperatorQuestionsCarryOperatorGroupBy(t *testing.T) {
	for _, q := range []string{
		"which operators are behind on vaccination",
		"which vaccinator is overloaded",
		"operator capacity today",
		"operator drive assignments today",
	} {
		pl := plan(t, q)
		if len(pl.SubQuestions) == 0 {
			t.Fatalf("%q: no sub-questions", q)
		}
		if gb, _ := pl.SubQuestions[0].Params["group_by"].(string); gb != "operator_label" {
			t.Errorf("%q: group_by=%q want operator_label", q, gb)
		}
	}
}

// TestWhyBehindDecomposesIntoContributors proves a "why are we behind" question
// decomposes into park + operator overdue breakdowns (grouped), not one number.
func TestWhyBehindDecomposesIntoContributors(t *testing.T) {
	pl := plan(t, "why are we behind on vaccination today")
	if len(pl.SubQuestions) != 2 {
		t.Fatalf("expected 2 contributor sub-questions, got %d: %+v", len(pl.SubQuestions), pl.SubQuestions)
	}
	byTool := map[string]string{}
	for _, s := range pl.SubQuestions {
		if s.Route != domain.RouteCube {
			t.Errorf("%s: route=%q want cube", s.ToolName, s.Route)
		}
		byTool[s.ToolName], _ = s.Params["group_by"].(string)
	}
	if byTool["vaccination_overdue"] != "park_label" {
		t.Errorf("vaccination_overdue group_by=%q want park_label", byTool["vaccination_overdue"])
	}
	if byTool["operator_vaccination_overdue"] != "operator_label" {
		t.Errorf("operator_vaccination_overdue group_by=%q want operator_label", byTool["operator_vaccination_overdue"])
	}
}

func TestOperationalRoutesToAPI(t *testing.T) {
	pl := plan(t, "what feed is needed today")
	if pl.SubQuestions[0].Route != domain.RouteAPI {
		t.Fatalf("feed must route api, got %q", pl.SubQuestions[0].Route)
	}
}

func TestWriteIntentRefused(t *testing.T) {
	pl := plan(t, "approve the procurement load")
	if pl.Refusal == "" {
		t.Fatal("write intent must be refused")
	}
}

func TestMultiIntentSplit(t *testing.T) {
	pl := plan(t, "how many goats and which sheds are overdue")
	if len(pl.SubQuestions) != 2 {
		t.Fatalf("expected 2 sub-questions, got %d (%+v)", len(pl.SubQuestions), pl.SubQuestions)
	}
}

func TestSpeciesParamExtracted(t *testing.T) {
	pl := plan(t, "how many sheep are active")
	if pl.SubQuestions[0].Params["species"] != "sheep" {
		t.Fatalf("expected species=sheep, got %v", pl.SubQuestions[0].Params["species"])
	}
}

func TestPlannedByModelFalse(t *testing.T) {
	if New().PlannedByModel() {
		t.Fatal("keyword planner must report PlannedByModel=false")
	}
}

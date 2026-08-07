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
		"how many goats do we have":       {"active_animals"},
		"which sheds are overdue":         {"vaccination_overdue"},
		"vaccination adherence week":      {"vaccination_compliance"},
		"mortality this month":            {"mortality_rate"},
		"what is due today":               {"vaccination_due"},
		"how many animals missed vaccine": {"vaccination_overdue"},
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

func TestVaccinationBreakdownsCarryRequestedGroupBy(t *testing.T) {
	cases := map[string]struct {
		tool    string
		groupBy string
	}{
		"what vaccinations are overdue by shed": {"vaccination_overdue", "shed_label"},
		"what vaccinations are overdue by park": {"vaccination_overdue", "park_label"},
		"chart vaccinations due today by shed":  {"vaccination_due", "shed_label"},
		"chart vaccinations due today by park":  {"vaccination_due", "park_label"},
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
		if gb, _ := s.Params["group_by"].(string); gb != want.groupBy {
			t.Errorf("%q: group_by=%q want %q", q, gb, want.groupBy)
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
// decomposes into shed + operator overdue breakdowns (grouped), not one number.
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
	if byTool["vaccination_overdue"] != "shed_label" {
		t.Errorf("vaccination_overdue group_by=%q want shed_label", byTool["vaccination_overdue"])
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

// TestFeedToolNameMatchesRegisteredExecutor is the P1-1 regression test: the
// planner's feed tool name must be the EXACT name the readtools executor is
// registered under (feed_direction_today), or registry.Execute's
// RouteAPI lookup (r.executors[sub.ToolName]) misses and every feed question
// dead-ends on "no read-service executor". This asserts the planner side of
// that contract directly against the string readtools.feedDirectionTodayExecutor
// advertises via Spec().Name, so a future rename on either side breaks the build.
func TestFeedToolNameMatchesRegisteredExecutor(t *testing.T) {
	const registeredFeedExecutorName = "feed_direction_today" // readtools.feedDirectionTodayExecutor.Spec().Name

	for _, q := range []string{
		"which feed directions are blocked",
		"feed ration today",
		"what feed is needed today",
	} {
		pl := plan(t, q)
		if len(pl.SubQuestions) == 0 {
			t.Fatalf("plan(%q): expected sub-questions, got none", q)
		}
		got := pl.SubQuestions[0]
		if got.Route != domain.RouteAPI {
			t.Fatalf("plan(%q): route=%q, want api", q, got.Route)
		}
		if got.ToolName != registeredFeedExecutorName {
			t.Fatalf("plan(%q): tool=%q, want %q (must match the registered readtools executor name or the registry lookup dead-ends)", q, got.ToolName, registeredFeedExecutorName)
		}
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

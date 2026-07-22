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

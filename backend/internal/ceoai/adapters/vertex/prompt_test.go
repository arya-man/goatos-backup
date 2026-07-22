package vertex

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

func TestParsePlanFencedJSON(t *testing.T) {
	raw := "```json\n{\"refusal\":\"\",\"sub_questions\":[{\"id\":\"0\",\"text\":\"t\",\"intent_class\":\"total_animal_census\",\"route\":\"cube\",\"tool\":\"active_animals\",\"params\":{\"park_label\":\"Castro 1\"}}]}\n```"
	pl, err := parsePlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.SubQuestions) != 1 {
		t.Fatalf("expected 1 sub-question, got %d", len(pl.SubQuestions))
	}
	s := pl.SubQuestions[0]
	if s.ToolName != "active_animals" || s.Route != domain.RouteCube {
		t.Fatalf("bad parse: %+v", s)
	}
	if s.Params["park_label"] != "Castro 1" {
		t.Fatalf("param not parsed: %+v", s.Params)
	}
}

func TestParsePlanRefusal(t *testing.T) {
	pl, err := parsePlan(`{"refusal":"read only","sub_questions":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Refusal == "" {
		t.Fatal("expected refusal preserved")
	}
}

func TestExtractJSONStripsProse(t *testing.T) {
	if got := extractJSON("Here you go: {\"a\":1} thanks"); got != `{"a":1}` {
		t.Fatalf("extractJSON=%q", got)
	}
}

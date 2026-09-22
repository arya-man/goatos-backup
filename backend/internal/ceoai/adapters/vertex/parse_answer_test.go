package vertex

import (
	"strings"
	"testing"
)

// The planner declares what each sub-question returns; the orchestrator's
// answer-fit check compares that declaration with the question. Template-only
// fields can never be set from model output.
func TestParsePlanReadsDeclaredAnswerShape(t *testing.T) {
	raw := `{"refusal":"","sub_questions":[
	 {"id":"0","route":"sql","tool":"sql_fallback","params":{"sql":"SELECT 1"},
	  "answer":{"measure":"directed feed kg","group_by":["park","day"],"window":"last 14 days"}},
	 {"id":"1","route":"cube","tool":"active_animals","params":{},
	  "answer":{"measure":"animals","group_by":"species, park"}},
	 {"id":"2","route":"api","tool":"x","params":{}}]}`
	plan, err := parsePlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	s0 := plan.SubQuestions[0].Declared
	if s0.Measure != "directed feed kg" || strings.Join(s0.Dimensions, ",") != "park,day" || s0.Window != "last 14 days" {
		t.Fatalf("declared shape not parsed: %+v", s0)
	}
	if s0.Template || len(s0.MeasureTerms) != 0 || s0.WindowFrom != "" {
		t.Fatalf("template-only fields must never come from model output: %+v", s0)
	}
	if got := strings.Join(plan.SubQuestions[1].Declared.Dimensions, ","); got != "species,park" {
		t.Fatalf("string group_by must be split, got %q", got)
	}
	if !plan.SubQuestions[2].Declared.IsZero() {
		t.Fatalf("absent answer must leave the declaration empty: %+v", plan.SubQuestions[2].Declared)
	}
}

func TestPlannerPromptAsksForAnswerFit(t *testing.T) {
	for _, want := range []string{"ANSWER FIT", `"answer"`, "concat_ws"} {
		if !strings.Contains(systemPlannerInstruction+sqlFallbackBlock("t1", ""), want) &&
			!strings.Contains(systemPlannerInstruction, want) {
			t.Fatalf("planner prompt missing %q", want)
		}
	}
}

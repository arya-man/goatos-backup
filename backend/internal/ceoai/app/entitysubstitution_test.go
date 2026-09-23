package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

const perAnimalQuestion = "What does MG-100001 weigh now and what did it weigh at the previous weighing?"

// scopeAverageResult is the answer the reviewer actually measured: the mean of
// every animal in scope, with the animal the reader named nowhere in it.
func scopeAverageResult() []domain.ToolResult {
	return []domain.ToolResult{{
		ToolName: "sql_fallback", SourceView: "weighing_capture_activity",
		Facts: []domain.Fact{{Label: "Average weight kg", Scope: "selected scope", Value: "29.67"}},
	}}
}

// TestTheDeterministicPlanForAPerAnimalWeightIsRefused runs the REAL
// deterministic planner for the reviewer's question, so the test cannot pass
// by testing a hand-written sub-question that happens to omit the tag. The
// plan that planner produces is the defect: a scope-wide avg() with nowhere to
// put an ear tag.
func TestTheDeterministicPlanForAPerAnimalWeightIsRefused(t *testing.T) {
	sub, ok := naturalSQLPlan(domain.Question{Actor: leadershipActor(), Text: perAnimalQuestion}, nil)
	if !ok {
		t.Fatal("the deterministic planner no longer plans the per-animal weight question")
	}
	sql, _ := sub.Params["sql"].(string)
	if sql == "" {
		t.Fatalf("the fallback plan carries no SQL: %+v", sub.Params)
	}
	if strings.Contains(strings.ToUpper(sql), "MG-100001") {
		t.Skip("the deterministic plan now selects the animal; this gate has nothing to refuse")
	}

	substituted, tag, view := namedEntitySubstitution(perAnimalQuestion,
		[]domain.SubQuestion{sub}, scopeAverageResult())
	if !substituted {
		t.Fatal("a question naming one animal was answered with a scope average and not refused")
	}
	if tag != "MG-100001" {
		t.Errorf("the refusal must name the animal the reader asked about, got %q", tag)
	}
	if view != "weighing_capture_activity" {
		t.Errorf("the refusal must name the read that ran, got %q", view)
	}

	refusal := substitutedEntityRefusal(tag, view)
	for _, want := range []string{"MG-100001", "weighing_capture_activity", "not for one animal"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("refusal %q is missing %q", refusal, want)
		}
	}
	if strings.Contains(refusal, "in Goat OS") {
		t.Errorf("refusal must not claim the product does not track it: %q", refusal)
	}
}

// TestAReadThatSelectedTheAnimalStillAnswers is the other half, and the one
// that keeps the gate shippable. A read FILTERED to the tag answers about that
// animal whether or not it echoes the tag back -- "Latest weight: 22.9" is a
// correct answer with the tag nowhere in it, and refusing it would be the
// opposite mistake.
func TestAReadThatSelectedTheAnimalStillAnswers(t *testing.T) {
	bare := []domain.ToolResult{{
		ToolName: "sql_fallback", SourceView: "weighing_latest_individual_weight",
		Facts: []domain.Fact{{Label: "Latest weight", Value: "22.9", Unit: "kg"}},
	}}
	cases := []struct {
		name    string
		subs    []domain.SubQuestion
		results []domain.ToolResult
	}{
		{
			name: "the SQL filtered on the tag",
			subs: []domain.SubQuestion{sqlSub(
				"SELECT 'Latest weight' AS label, weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight WHERE animal_key = 'mg-100001'")},
			results: bare,
		},
		{
			name: "the case-folded rewrite the identity filter produces",
			subs: []domain.SubQuestion{sqlSub(
				"SELECT weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight WHERE upper(trim(animal_key)) = upper(trim('MG-100001'))")},
			results: bare,
		},
		{
			name: "a read API taking the animal as a bound param",
			subs: []domain.SubQuestion{{
				Route: domain.RouteAPI, ToolName: "weighing_animal_history",
				Params: map[string]any{"animal_key": "mg-100001"},
			}},
			results: bare,
		},
		{
			name:    "the rows themselves name the animal",
			subs:    []domain.SubQuestion{{Route: domain.RouteAPI, ToolName: "weighing_animal_history"}},
			results: []domain.ToolResult{{ToolName: "weighing_animal_history", SourceView: "growth_adg_pairs", Facts: []domain.Fact{{Label: "Animal MG-100001", Scope: "2026-09-17", Value: "22.9"}}}},
		},
		{
			name:    "an empty read is honest, not a substitution",
			subs:    []domain.SubQuestion{sqlSub("SELECT 1 FROM ceo_ai.weighing_capture_activity")},
			results: []domain.ToolResult{{ToolName: "sql_fallback", SourceView: "weighing_capture_activity"}},
		},
		{
			name: "one read answers the animal, a second gives context",
			subs: []domain.SubQuestion{
				sqlSub("SELECT weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight WHERE animal_key = 'mg-100001'"),
				sqlSub("SELECT avg(shed_weight_avg_kg) AS value FROM ceo_ai.weighing_capture_activity"),
			},
			results: append(append([]domain.ToolResult{}, bare...), scopeAverageResult()...),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if substituted, tag, _ := namedEntitySubstitution(perAnimalQuestion, c.subs, c.results); substituted {
				t.Errorf("a legitimate answer about %s was refused", tag)
			}
		})
	}
}

// TestAQuestionNamingNoAnimalIsNeverTouched keeps the gate off every ordinary
// leadership question, including the ones whose text carries hyphens and
// numbers that are NOT ear tags.
func TestAQuestionNamingNoAnimalIsNeverTouched(t *testing.T) {
	for _, q := range []string{
		"What's the average animal weight per shed from the latest weighing?",
		"How many kg of feed did we actually give out yesterday, split by park?",
		"What did we weigh in Castro 1 - Part 2 on 2026-09-17?",
		"Revenue for 2026-09 by buyer",
		"How many animals in Godel 2 - Part 1?",
	} {
		if substituted, tag, _ := namedEntitySubstitution(q, nil, scopeAverageResult()); substituted {
			t.Errorf("question %q was refused over a false ear tag %q", q, tag)
		}
	}
}

func TestEarTagsInReadsTheReadersSpelling(t *testing.T) {
	cases := map[string][]string{
		"What does MG-100001 weigh now":          {"MG-100001"},
		"weight history for animal mg-100001":    {"mg-100001"},
		"compare MG-100001 and MG-100002":        {"MG-100001", "MG-100002"},
		"MG-100001 and mg-100001 are one animal": {"MG-100001"},
		"how heavy is Castro 1":                  nil,
		"weighed on 2026-09-17":                  nil,
		"Godel 2 - Part 1":                       nil,
	}
	for q, want := range cases {
		got := earTagsIn(q)
		if len(got) != len(want) {
			t.Errorf("earTagsIn(%q) = %v, want %v", q, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("earTagsIn(%q) = %v, want %v", q, got, want)
				break
			}
		}
	}
}

// TestTheRefusalNamesTheSubjectAsThePersonSaidIt is claim 4's remaining half:
// "a source for missed and treatment" is the right SET of terms in the wrong
// English. The question says them as one phrase, so the refusal does too.
func TestTheRefusalNamesTheSubjectAsThePersonSaidIt(t *testing.T) {
	cases := []struct {
		question string
		terms    []string
		want     string
	}{
		// hl-03, the sentence the reviewer would not ship.
		{"How many treatment sessions were missed yesterday?", []string{"missed", "treatment"}, "missed treatment sessions"},
		{"how many milk feeding sessions ran today", []string{"feeding", "milk"}, "milk feeding"},
		// Terms too far apart to be one phrase keep the list join: welding them
		// would invent a subject nobody named.
		{"how many litres of milk did we produce today", []string{"litres", "produce"}, "litres and produce"},
		// No question text, one term, or a term the question does not say.
		{"", []string{"missed", "treatment"}, "missed and treatment"},
		{"How many treatment sessions were missed yesterday?", []string{"milk"}, "milk"},
		{"How many treatment sessions were missed yesterday?", []string{"missed", "colostrum"}, "missed and colostrum"},
	}
	for _, c := range cases {
		if got := subjectPhrase(c.question, c.terms); got != c.want {
			t.Errorf("subjectPhrase(%q, %v) = %q, want %q", c.question, c.terms, got, c.want)
		}
	}

	refusal := unmodelledRefusalFor("How many treatment sessions were missed yesterday?", []string{"missed", "treatment"})
	if !strings.Contains(refusal, "source for missed treatment sessions") {
		t.Errorf("the refusal still hands the reader token soup: %q", refusal)
	}
	if strings.Contains(refusal, "missed and treatment") {
		t.Errorf("the refusal still joins the subject's own words with \"and\": %q", refusal)
	}
	if strings.Contains(refusal, "in Goat OS") {
		t.Errorf("refusal must not claim the product does not track it: %q", refusal)
	}
}

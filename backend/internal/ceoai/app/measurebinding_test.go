package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

func sqlSub(sql string) domain.SubQuestion {
	return domain.SubQuestion{ID: "s1", Route: domain.RouteSQL, ToolName: "sql_fallback", Params: map[string]any{"sql": sql}}
}

func oneFact() []domain.Fact {
	return []domain.Fact{{Label: "count", Value: "32"}}
}

// vax-03 and wf-03: the read ran over the RIGHT view and aggregated the WRONG
// column -- obligations DUE reported as obligations COMPLETED, workforce task
// rows reported as tasks VERIFIED. Both numbers are really in the evidence, so
// grounding cannot see it; the label is the model's, so the label cannot be
// trusted either. Only the columns the read touched can settle it.
func TestAMeasureMustBeBoundToAColumnTheReadActuallyRead(t *testing.T) {
	card, ok := reporting.CardByName("vaccination_obligations_base")
	if !ok {
		t.Skip("card not in the catalogue")
	}
	var dueCol, completedCol string
	for _, c := range card.Columns {
		low := strings.ToLower(c.Name)
		if strings.Contains(low, "complet") && completedCol == "" {
			completedCol = c.Name
		}
		if strings.Contains(low, "due") && dueCol == "" {
			dueCol = c.Name
		}
	}
	if dueCol == "" || completedCol == "" {
		t.Skip("this card does not carry both a due and a completed column")
	}

	asked := domain.Question{Text: "how many vaccination obligations were completed on 22 September 2026"}
	results := []domain.ToolResult{{ToolName: "sql_fallback", SourceView: card.Name, Facts: oneFact()}}

	wrong := []domain.SubQuestion{sqlSub("SELECT " + dueCol + ", count(*) FROM ceo_ai." + card.Name + " GROUP BY 1")}
	if issues := measureBindingIssues(asked, wrong, results, reporting.Cards(), nil); len(issues) == 0 {
		t.Error("a read that never touched the completed column answered a question about completions")
	}

	right := []domain.SubQuestion{sqlSub("SELECT " + completedCol + ", count(*) FROM ceo_ai." + card.Name + " GROUP BY 1")}
	if issues := measureBindingIssues(asked, right, results, reporting.Cards(), nil); len(issues) > 0 {
		t.Errorf("the read that DID report completions was flagged: %+v", issues)
	}
}

// A curated Cube or API tool reports whatever its own implementation reports;
// its name is not a column list. Guessing from it would flag correct reads, so
// a read whose view cannot be resolved is not judged at all.
func TestAToolWhoseColumnsAreUnknowableIsNotJudged(t *testing.T) {
	asked := domain.Question{Text: "how many vaccination obligations were completed yesterday"}
	subs := []domain.SubQuestion{{ID: "s1", Route: domain.RouteCube, ToolName: "vaccination_overdue"}}
	results := []domain.ToolResult{{ToolName: "vaccination_overdue", Facts: oneFact()}}
	if issues := measureBindingIssues(asked, subs, results, reporting.Cards(), nil); len(issues) > 0 {
		t.Errorf("a curated tool was judged on a column list nobody has: %+v", issues)
	}
}

// mk-02 and hl-03: a milk question answered "CBE 24, CPT 24" from the ANIMAL
// scope view, a missed-treatment question answered from workforce task rows.
// Nothing in the catalogue models milk or treatment sessions, so the honest
// answer is that the farm does not track it -- never the nearest number.
func TestASubjectNothingModelsIsRefusedRatherThanAnsweredFromANeighbour(t *testing.T) {
	catalog := []ports.ToolSpec{{
		Name: "counts_breakdown", Route: domain.RouteAPI,
		Description: "Live animal counts by park, pen, stage, breed and sex",
	}}
	unmodelled, terms := measureUnmodelled("how many litres of milk did we produce today", reporting.Cards(), catalog)
	if !unmodelled {
		t.Error("a milk question found a source in a catalogue that models no milk")
	}
	if len(terms) == 0 || !strings.Contains(strings.Join(terms, " "), "milk") {
		t.Errorf("the refusal must name what is not tracked, got %v", terms)
	}
	if !strings.Contains(unmodelledRefusal(terms), "don't track") {
		t.Error("the refusal must say the farm does not track it")
	}
}

// THE READ THAT ACTUALLY SHIPPED, reproduced verbatim. The gate above passed its
// own tests and stayed silent on this one live: asked "Kids on milk feeding per
// park today (head count)" the assistant counted rows in ceo_ai.animal_current_scope
// and answered "CBE 24, CPT 24".
//
// It stayed silent because a term was held against a read ONLY when the card that
// ran HAS a column for it, and the animal-scope view has no milk column, no
// feeding column and no head column -- so every term was skipped as unproven and
// the substitution case, the one the gate exists for, was the one case it could
// not see. A card that models NONE of the question is now the evidence.
func TestTheMilkQuestionAnsweredFromTheAnimalScopeViewIsFlagged(t *testing.T) {
	card, ok := reporting.CardByName("animal_current_scope")
	if !ok {
		t.Skip("card not in the catalogue")
	}
	asked := domain.Question{Text: "Kids on milk feeding per park today (head count)"}
	shipped := sqlSub("SELECT 'Kids on milk feeding' AS label, CAST(count(*) AS text) AS value, " +
		"park_label AS scope FROM ceo_ai." + card.Name + " WHERE lifecycle_status = 'alive' " +
		"AND management_stage = 'K2' GROUP BY park_label")
	results := []domain.ToolResult{{ToolName: "sql_fallback", SourceView: card.Name, Facts: oneFact()}}

	// The catalogue must offer something the question's words DO name, or this
	// is the untracked case rather than the substitution case.
	if len(modelledTerms(measureTerms(asked.Text), reporting.Cards(), nil)) == 0 {
		t.Skip("nothing in the catalogue models this question; that is the refusal path")
	}
	issues := measureBindingIssues(asked, []domain.SubQuestion{shipped}, results, reporting.Cards(), nil)
	if len(issues) == 0 {
		t.Fatal("an animal count from the animal-scope view was presented as the milk answer, unflagged")
	}
	if issues[0].Kind != "measure_binding" {
		t.Errorf("wrong issue kind %q", issues[0].Kind)
	}
}

// The other half, and the one that keeps the rule above from flagging ordinary
// correct work: a read whose own card carries the question's subject binds, and
// must pass even though plenty of the question's other words match nothing.
func TestAReadOverASourceThatCarriesTheSubjectIsNotFlagged(t *testing.T) {
	card, ok := reporting.CardByName("workforce_tasks_base")
	if !ok {
		t.Skip("card not in the catalogue")
	}
	asked := domain.Question{Text: "How many tasks did operators finish yesterday, by task type?"}
	bound := sqlSub("SELECT task_type, count(*) FROM ceo_ai." + card.Name + " GROUP BY task_type")
	results := []domain.ToolResult{{ToolName: "sql_fallback", SourceView: card.Name, Facts: oneFact()}}
	if issues := measureBindingIssues(asked, []domain.SubQuestion{bound}, results, reporting.Cards(), nil); len(issues) > 0 {
		t.Errorf("a read that named the task columns was flagged: %+v", issues)
	}
}

// hl-03: "How many treatment sessions were missed yesterday?" -- the farm's read
// models carry no treatment and no missed column, so this is the refusal case.
// It was answered anyway (from workforce tasks, then from vaccination obligations
// on a re-run) because the coverage matcher nominated a view on the strength of a
// single incidental `planned_sessions` column: questionWords supplies both
// "sessions" and its crude singular "session", and the "two INDEPENDENT words"
// rule counted the same word twice.
func TestASinglePluralColumnMatchDoesNotDisarmTheRefusal(t *testing.T) {
	catalog := []ports.ToolSpec{{
		Name: "counts_breakdown", Route: domain.RouteAPI,
		Description: "Live animal counts by park, pen, stage, breed and sex",
	}}
	const q = "How many treatment sessions were missed yesterday?"
	if got := coveringSources(q, reporting.Cards(), catalog); len(got) > 0 {
		t.Errorf("one repeated word nominated %v as covering a subject the farm does not model", got)
	}
	unmodelled, terms := measureUnmodelled(q, reporting.Cards(), catalog)
	if !unmodelled {
		t.Fatal("a treatment-session question found a source in a catalogue that models neither word")
	}
	if !strings.Contains(strings.Join(terms, " "), "treatment") {
		t.Errorf("the refusal must name what is not tracked, got %v", terms)
	}
}

// The stem fold must not make the matcher blind: two genuinely different words
// still nominate a view exactly as they did before.
func TestTwoDifferentWordsStillNominateTheirView(t *testing.T) {
	if _, ok := reporting.CardByName("vaccination_obligations_base"); !ok {
		t.Skip("card not in the catalogue")
	}
	got := coveringSources("which vaccination obligations are due", reporting.Cards(), nil)
	if len(got) == 0 {
		t.Error("two independent subject words stopped nominating the view that carries both")
	}
}

// The same check must not refuse a question the farm DOES answer. A subject the
// coverage matcher nominates is answerable even when the question's own words
// are not column names.
func TestAQuestionTheCatalogueCoversIsNeverRefusedAsUntracked(t *testing.T) {
	catalog := []ports.ToolSpec{{
		Name: "sales_overview", Route: domain.RouteAPI,
		Description: "Sales overview for closed deals: sold animals, revenue, deals, monthly sales",
	}}
	for _, q := range []string{
		"how many animals are in the herd right now",
		"what were our sales revenue and deals last month",
		"how much directed feed went out per park in the last 14 days",
	} {
		if unmodelled, terms := measureUnmodelled(q, reporting.Cards(), catalog); unmodelled {
			t.Errorf("%q was called untracked over %v", q, terms)
		}
	}
}

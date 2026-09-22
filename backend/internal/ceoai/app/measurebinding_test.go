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

package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// THE WINDOW LINE COUNTED AS AN ANSWER. The orchestrator appends a synthetic
// "Window: …" result to annotate the period, then hands that same slice to
// reviewSections with len(plan.SubQuestions) as the expected count. A 2-sub plan
// with ONE empty read therefore flipped Complete false -> true the moment the
// window line was appended — and the orchestrator now honours Complete, so the
// inflated verdict also skipped the failed-read retry that an incomplete answer
// is supposed to buy.
//
// Mutation: drop the `if r.Synthetic { continue }` guard in reviewSections and
// this goes red.
func TestTheSyntheticWindowLineDoesNotCompleteAnUnansweredSubQuestion(t *testing.T) {
	var rv reviewer
	answered := domain.ToolResult{ToolName: "sql_fallback", Facts: []domain.Fact{{Label: "Kids", Value: "24"}}}
	empty := domain.ToolResult{ToolName: "sql_fallback"}
	window := windowResult(domain.Actor{TenantID: "t1"},
		[]domain.SubQuestion{sqlSub("SELECT 1")},
		[]domain.ToolResult{{ToolName: "sql_fallback"}}, Window{})
	if window == nil {
		// windowResult declines when nothing bound a period; build the same
		// synthetic shape directly so the property under test still runs.
		window = &domain.ToolResult{ToolName: "window", Synthetic: true,
			Facts: []domain.Fact{{TenantID: "t1", Label: "Window", Value: "as of 18/09/2026"}}}
	}
	if !window.Synthetic {
		t.Fatal("the window annotation must declare itself synthetic")
	}

	sections := []answerSection{{text: "Kids: 24.", result: answered}}
	if v := rv.reviewSections(context.Background(), "Kids: 24.", sections,
		[]domain.ToolResult{answered, empty}, 2); v.Complete {
		t.Fatal("fixture is wrong: two sub-questions with one empty read must be incomplete")
	}
	if v := rv.reviewSections(context.Background(), "Kids: 24.", sections,
		[]domain.ToolResult{answered, empty, *window}, 2); v.Complete {
		t.Fatal("the synthetic window line was counted as the answer to the sub-question " +
			"that returned nothing, so the failed-read retry is skipped")
	}
}

// THE GLOBAL BAG. measureBindingIssues held ONE `unmet` set for the whole plan
// and `delete`d a term the moment any read satisfied it — so a SECOND
// sub-question over a DIFFERENT card cleared a flag the first one raised. That
// is the pooled-evidence defect review.go fixed for grounding, one file over: a
// read binds the measure for the question IT answered, and another card's read
// is no evidence about it.
//
// Mutation: restore the `delete(unmet, t)` branch and this goes red.
func TestOneSubQuestionsReadCannotClearAnothersMeasureFlag(t *testing.T) {
	card, ok := reporting.CardByName("workforce_tasks_base")
	if !ok {
		t.Skip("card not in the catalogue")
	}
	var verifiedCol, dueCol string
	for _, c := range card.Columns {
		switch c.Name {
		case "verified_at":
			verifiedCol = c.Name
		case "due_business_day":
			dueCol = c.Name
		}
	}
	if verifiedCol == "" || dueCol == "" {
		t.Skip("this card no longer carries both columns")
	}
	asked := domain.Question{Text: "How many tasks did operators get verified yesterday, by task type?"}

	// Sub 1 misses the measure: it groups the DUE day and never names verified.
	unbound := sqlSub("SELECT task_type, count(*) FROM ceo_ai." + card.Name +
		" WHERE " + dueCol + " >= '2026-09-22' GROUP BY task_type")
	// Sub 2 is a DIFFERENT question over the SAME card that does name the
	// column — the clearing read in the defect.
	bound := sqlSub("SELECT " + verifiedCol + ", count(*) FROM ceo_ai." + card.Name +
		" WHERE " + verifiedCol + " IS NOT NULL")

	subs := []domain.SubQuestion{unbound, bound}
	results := []domain.ToolResult{
		{ToolName: "sql_fallback", SourceView: card.Name, Facts: oneFact()},
		{ToolName: "sql_fallback", SourceView: card.Name, Facts: oneFact()},
	}
	issues := measureBindingIssues(asked, subs, results, reporting.Cards(), nil)
	if len(issues) == 0 {
		t.Fatal("a second read over another question cleared the first read's measure-binding flag")
	}
	var bindingFlagged bool
	for _, is := range issues {
		if is.Kind == "measure_binding" {
			bindingFlagged = true
		}
	}
	if !bindingFlagged {
		t.Errorf("expected the measure binding to stay flagged, got %+v", issues)
	}
}

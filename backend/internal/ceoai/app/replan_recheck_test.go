package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// THE FLAG THAT VANISHED. The first plan is judged by answerFit, which runs the
// shape checks, the period-capability check, the unsupported-param check and the
// measure binding. A misfit buys ONE re-plan -- and the re-plan used to be
// re-judged by planFitIssues ALONE, which knows only about shape.
//
// So the moment the second read returned any rows at all, a measure-binding
// issue the first plan had raised was recomputed as nothing and the answer went
// out as "planned" with no flag on it. Live, "How many tasks did operators get
// verified yesterday, by task type?" did exactly that: the gate fired, the
// re-plan grouped the due-day column again, and the reply read "planned".
//
// A second read is not evidence that it bound the measure. Both paths now call
// the SAME deterministic set, which is why it is one function.
func TestTheReplanIsJudgedByTheSameDeterministicChecksAsTheFirstPlan(t *testing.T) {
	card, ok := reporting.CardByName("workforce_tasks_base")
	if !ok {
		t.Skip("card not in the catalogue")
	}
	var verifiedCol, dueCol string
	for _, c := range card.Columns {
		if c.Name == "verified_at" {
			verifiedCol = c.Name
		}
		if c.Name == "due_business_day" {
			dueCol = c.Name
		}
	}
	if verifiedCol == "" || dueCol == "" {
		t.Skip("this card no longer carries both columns")
	}

	asked := domain.Question{Text: "How many tasks did operators get verified yesterday, by task type?"}
	// The re-plan that still does not bind: it groups by task_type (so the shape
	// check is satisfied and planFitIssues alone returns nothing) while filtering
	// on the DUE day and never naming the verified column.
	replan := []domain.SubQuestion{sqlSub(
		"SELECT task_type, count(*) FROM ceo_ai." + card.Name +
			" WHERE " + dueCol + " >= '2026-09-22' GROUP BY task_type")}
	results := []domain.ToolResult{{ToolName: "sql_fallback", SourceView: card.Name, Facts: oneFact()}}
	req := ParseRequestedShape(asked.Text, Window{})

	if shapeOnly := planFitIssues(req, replan, results); len(shapeOnly) > 0 {
		t.Skip("shape alone already rejects this re-plan; the gap under test needs a shape-clean one")
	}
	issues, reasons := deterministicFitIssues(asked, req, replan, results, nil)
	if len(issues) == 0 {
		t.Fatal("the re-plan reported the due day as the verified count and was recomputed as clean")
	}
	var bound bool
	for _, is := range issues {
		if is.Kind == "measure_binding" {
			bound = true
		}
	}
	if !bound {
		t.Errorf("the re-plan was re-judged without the measure binding: %+v", issues)
	}
	if len(reasons) != len(issues) {
		t.Errorf("every issue must carry its own re-plan feedback: %d issues, %d reasons", len(issues), len(reasons))
	}
}

// THE SAME GAP, THROUGH THE WHOLE ORCHESTRATOR, so the one-line wiring is pinned
// and not only the shared function. Both the first plan and the re-plan report
// the DUE day for a question about VERIFIED tasks. The re-plan returns rows, so
// under the shape-only recheck the answer shipped as "planned" with no flag on
// it -- a leader reading a due count labelled as a verified count.
func TestAReplanThatStillDoesNotBindComesBackPartial(t *testing.T) {
	if _, ok := reporting.CardByName("workforce_tasks_base"); !ok {
		t.Skip("card not in the catalogue")
	}
	const unbound = "SELECT task_type AS label, CAST(count(*) AS text) AS value, task_type AS scope " +
		"FROM ceo_ai.workforce_tasks_base WHERE tenant_id = 't1' " +
		"AND due_business_day >= '2026-09-17' AND due_business_day < '2026-09-18' GROUP BY task_type LIMIT 10"

	sqlFB := &fakeSQLFallback{result: domain.ToolResult{SourceView: "workforce_tasks_base", Facts: []domain.Fact{
		{TenantID: "t1", Label: "vaccination", Value: "2", Scope: "vaccination"},
	}}}
	reg := NewRegistry(nil, nil, sqlFB)
	spec := domain.AnswerSpec{Measure: "verified tasks", Dimensions: []string{"task_type"}}
	prov := &judgingProvider{
		fakeProvider: fakeProvider{byModel: true, plan: modelSQLPlan(unbound, spec)},
		fits:         true,
		replan:       modelSQLPlan(unbound, spec),
	}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(),
		Text: "How many tasks did operators get verified yesterday, by task type?",
		AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
	if err != nil {
		t.Fatal(err)
	}
	if prov.feedbackSeen == "" {
		t.Fatal("the first plan raised no issue, so the re-plan path under test never ran")
	}
	if sqlFB.calls != 2 {
		t.Fatalf("both the plan and the re-plan must have run, got %d reads", sqlFB.calls)
	}
	if ans.Mode == domain.ModePlanned {
		t.Fatalf("a read that never touched the verified column shipped as a planned answer: %q", ans.Answer)
	}
	if ans.Mode != domain.ModePartial {
		t.Fatalf("expected the answer to be downgraded to partial, got %q (%q)", ans.Mode, ans.Answer)
	}
	// Assert the flag names the MEASURE BINDING, not merely that something was
	// flagged: a shape-only recheck also downgrades, so a bare partial would
	// false-green the wiring this test exists to pin.
	if !strings.Contains(ans.Answer, "verified") {
		t.Fatalf("the flag must name the measure the read did not report, got %q", ans.Answer)
	}
}

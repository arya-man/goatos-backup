package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// THE GATE THAT NEVER RE-RAN. measureUnmodelled and subjectSubstitution are the
// two FAIL-CLOSED refusals, and they used to be judged on the FIRST plan's
// results only: when the fit re-plan replaced those results, only the shape
// checks were recomputed. So the exact failure the branch's last commit exists
// to refuse could still ship — "how many kids are on milk feeding, by park"
// misfits on shape against the feed read, re-plans onto animal_current_scope,
// and the 48-goat park split of a COUNT(*) comes back wearing the milk
// question's words, with hasUsableResult waving it through.
//
// Mutation: delete the postReadHonesty call in the re-plan branch of
// orchestrator.go and this test reports the substituted answer shipping.
func TestTheHonestyGatesAreReRunOnTheReplansResults(t *testing.T) {
	if _, ok := reporting.CardByName("animal_current_scope"); !ok {
		t.Skip("the animal-scope card is not in the catalogue")
	}
	// The re-plan's read: the animal-scope view, which reports neither milk nor
	// feeding, returning a real per-park head count.
	const scopeSQL = "SELECT park_label AS label, CAST(count(*) AS text) AS value, park_label AS scope " +
		"FROM ceo_ai.animal_current_scope WHERE tenant_id = 't1' AND management_stage = 'K2' " +
		"GROUP BY park_label LIMIT 10"
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{SourceView: "animal_current_scope", Facts: []domain.Fact{
		{TenantID: "t1", Label: "CBE", Value: "24", Scope: "CBE"},
		{TenantID: "t1", Label: "CPT", Value: "24", Scope: "CPT"},
	}}}
	// The FIRST read is a curated feed tool: it returns rows, but per pen rather
	// than per park, so the shape check buys the one re-plan. It names no schema
	// card, so the honesty gates cannot judge it — which is precisely why the
	// re-plan's results have to be judged instead of the first plan's.
	feed := &fakeExec{
		spec: ports.ToolSpec{Name: "feed_direction_today", Route: domain.RouteAPI,
			Description: "Feed direction for today by pen", Params: []string{"park_label"}},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{
			{TenantID: "t1", Label: "Pen 1", Value: "12", Scope: "Castro 1"},
		}},
	}
	reg := NewRegistry(nil, nil, sqlFB)
	reg.Register(feed)
	prov := &judgingProvider{
		fakeProvider: fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
			{ID: "0", Route: domain.RouteAPI, ToolName: "feed_direction_today"}}}},
		fits:   false,
		reason: "rows are per pen, not per park",
		replan: modelSQLPlan(scopeSQL, domain.AnswerSpec{Measure: "kids on milk feeding", Dimensions: []string{"park"}}),
	}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(),
		Text: "How many kids are on milk feeding, by park?",
		AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls == 0 {
		t.Fatal("the re-plan never ran, so the gap under test was not exercised")
	}
	if ans.Mode != domain.ModeRefused {
		t.Fatalf("the re-plan's animal-scope number shipped as %q instead of a refusal: %q", ans.Mode, ans.Answer)
	}
	if !strings.Contains(ans.Answer, "milk feeding") {
		t.Fatalf("the refusal must name the subject the farm does not record, got %q", ans.Answer)
	}
	if strings.Contains(ans.Answer, "24") {
		t.Fatalf("the substituted figure reached the leader anyway: %q", ans.Answer)
	}
}

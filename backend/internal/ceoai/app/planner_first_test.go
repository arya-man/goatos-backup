package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
)

// errVertexDown models the model planner being unavailable (the only
// condition under which the deterministic template/keyword path may run).
var errVertexDown = errors.New("vertex: status 503: model unavailable")

var istZone = time.FixedZone("IST", 5*60*60+30*60)

// liveProofQuestion is the question from the live defect report: it used to
// be answered with per-pen feed VARIANCE for today, labelled "Mode: fallback",
// while Vertex was up.
const liveProofQuestion = "Total directed feed kg per day by park for the last 14 days"

const modelFeedSQL = "SELECT 'Directed feed kg' AS label, CAST(sum(directed_kg) AS text) AS value, park_label AS scope, feed_day FROM ceo_ai.feed_adherence WHERE tenant_id = 't1' AND feed_day >= '2026-09-05' AND feed_day < '2026-09-19' GROUP BY park_label, feed_day ORDER BY feed_day LIMIT 100"

func modelSQLPlan(sql string, declared domain.AnswerSpec) domain.Plan {
	return domain.Plan{SubQuestions: []domain.SubQuestion{{
		ID: "0", Text: "q", IntentClass: "model_sql", Route: domain.RouteSQL, ToolName: "sql_fallback",
		Params: map[string]any{"sql": sql}, Declared: declared,
	}}}
}

func TestModelPlanWinsOverTopicTemplate(t *testing.T) {
	if err := sqlguard.Validate(modelFeedSQL); err != nil {
		t.Fatalf("fixture SQL must pass the real guard: %v", err)
	}
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{Facts: []domain.Fact{
		{TenantID: "t1", Label: "Directed feed kg", Value: "812.5", Scope: "Coimbatore"},
		{TenantID: "t1", Label: "Directed feed kg", Value: "640", Scope: "Channapatna"},
	}}}
	reg := NewRegistry(nil, nil, sqlFB)
	prov := &fakeProvider{byModel: true, plan: modelSQLPlan(modelFeedSQL, domain.AnswerSpec{
		Measure: "directed feed kg", Dimensions: []string{"park", "day"}, Window: "last 14 days",
	})}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})

	asOf := time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: liveProofQuestion, AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("the model must plan every question; planner calls=%d", prov.calls)
	}
	if sqlFB.calls != 1 || sqlFB.lastSQL != modelFeedSQL {
		t.Fatalf("the model's SQL must be what ran (never the per-pen variance template); calls=%d sql=%s", sqlFB.calls, sqlFB.lastSQL)
	}
	if strings.Contains(sqlFB.lastSQL, "variance_kg") {
		t.Fatalf("keyword template hijacked the planned question: %s", sqlFB.lastSQL)
	}
	if ans.Mode != domain.ModePlanned {
		t.Fatalf("model-planned answer must be labelled planned, got %q (%q)", ans.Mode, ans.Answer)
	}
	for _, c := range ans.Citations {
		if !c.PlannedByModel {
			t.Fatalf("citation must record the model planned it: %+v", c)
		}
	}
	if strings.Contains(ans.Answer, "Note: this answer may not match") {
		t.Fatalf("a correctly shaped answer must not be flagged: %q", ans.Answer)
	}
}

// Every topic area the natural-SQL templates or the keyword planner cover
// (feed, vaccination, weighing, health, counts, sales, procurement,
// workforce, milk, PC care, mortality, source entry, operators): with the
// model UP, the model's plan runs and no template ever does.
var topicQuestions = []string{
	liveProofQuestion,
	"Vaccinations completed per week by vaccine for the last 2 months",
	"Average weighed kg per animal by breed for last month",
	"Health cases opened per day by pen this week",
	"How many goats vs sheep were born per month this year",
	"Sales revenue in rupees by buyer for last quarter",
	"Procurement loads received per week by vendor for the last 30 days",
	"Workforce tasks completed per operator yesterday",
	"Milk fed per session by pen for the last 7 days",
	"PC care deworming tasks done by park last week",
	"Deaths per day by cause for the last 14 days",
	"Source entry health blockers by load for the last 30 days",
	"Operator utilization per day for the last 7 days",
	"Feed pending today for cpt",
}

func TestModelUpNeverRunsATemplateForAnyTopic(t *testing.T) {
	for _, q := range topicQuestions {
		t.Run(q, func(t *testing.T) {
			sqlFB := &fakeSQLFallback{}
			reg := NewRegistry(nil, nil, sqlFB)
			exec := &fakeExec{spec: ports.ToolSpec{Name: "model_choice", Route: domain.RouteAPI},
				result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "Model answer", Value: "7"}}}}
			reg.Register(exec)
			prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
				{ID: "0", Route: domain.RouteAPI, ToolName: "model_choice"},
			}}}
			a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
			ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: q,
				AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
			if err != nil {
				t.Fatal(err)
			}
			if exec.calls != 1 {
				t.Fatalf("model-chosen tool must run, calls=%d", exec.calls)
			}
			if sqlFB.calls != 0 || sqlFB.trustedCalls != 0 {
				t.Fatalf("a keyword template ran while the model was up: regular=%d trusted=%d sql=%s", sqlFB.calls, sqlFB.trustedCalls, sqlFB.lastSQL)
			}
			if ans.Mode == domain.ModeFallback {
				t.Fatalf("mode must never be fallback when the model planned: %q", ans.Answer)
			}
		})
	}
}

// With the model DOWN, a template matched on a topic word must NOT answer a
// question of a different shape: the reply is an honest "can't answer that
// precisely", no read runs, and the mode is fallback.
func TestFallbackRefusesATemplateThatDoesNotFitTheQuestion(t *testing.T) {
	for _, q := range topicQuestions[:len(topicQuestions)-1] {
		t.Run(q, func(t *testing.T) {
			sqlFB := &fakeSQLFallback{result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Feed variance kg", Value: "-42", Scope: "Godel 2"}}}}
			reg := NewRegistry(nil, nil, sqlFB)
			api := &fakeExec{spec: ports.ToolSpec{Name: "sales_overview", Route: domain.RouteAPI},
				result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "Sold", Value: "114"}}}}
			reg.Register(api)
			prov := &fakeProvider{byModel: true, err: errVertexDown}
			a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg, Fallback: fallbackKeyword{}})
			ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: q,
				AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
			if err != nil {
				t.Fatal(err)
			}
			if sqlFB.calls != 0 || sqlFB.trustedCalls != 0 || api.calls != 0 {
				t.Fatalf("an unfit template/keyword read ran in fallback: sql=%d trusted=%d api=%d (%s)", sqlFB.calls, sqlFB.trustedCalls, api.calls, sqlFB.lastSQL)
			}
			if ans.Mode != domain.ModeFallback {
				t.Fatalf("mode must be fallback when the model did not plan, got %q", ans.Mode)
			}
			if !strings.Contains(ans.Answer, "can't answer that precisely right now") {
				t.Fatalf("expected an honest can't-answer reply, got %q", ans.Answer)
			}
			for _, leaked := range []string{"-42", "114"} {
				if strings.Contains(ans.Answer, leaked) {
					t.Fatalf("a different metric leaked into the refusal: %q", ans.Answer)
				}
			}
		})
	}
}

// fallbackKeyword is a non-model planner that, like the real keyword planner,
// routes on a topic word with no grouping/period.
type fallbackKeyword struct{}

func (fallbackKeyword) Plan(_ context.Context, _ domain.Question, _ []domain.ResolvedEntities, _ []ports.ToolSpec) (domain.Plan, error) {
	return domain.Plan{SubQuestions: []domain.SubQuestion{{ID: "0", Route: domain.RouteAPI, ToolName: "sales_overview"}}}, nil
}
func (fallbackKeyword) PlannedByModel() bool { return false }

func TestModeLabelIsFallbackOnlyWhenTheModelDidNotPlan(t *testing.T) {
	facts := []domain.Fact{{TenantID: "t1", Label: "Feed variance kg", Value: "-42", Scope: "Godel 2"}}
	cases := []struct {
		name string
		prov ports.AIProvider
		want domain.Mode
	}{
		{"model planned", &fakeProvider{byModel: true, plan: modelSQLPlan(
			"SELECT 'Feed variance kg' AS label, CAST(variance_kg AS text) AS value, shed_label AS scope FROM ceo_ai.feed_adherence WHERE tenant_id = 't1' AND feed_day >= '2026-09-18' AND feed_day < '2026-09-19' AND variance_kg < 0 LIMIT 50",
			domain.AnswerSpec{})}, domain.ModePlanned},
		{"model errored -> template", &fakeProvider{byModel: true, err: errVertexDown}, domain.ModeFallback},
		{"no model configured -> template", &fakeProvider{}, domain.ModeFallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlFB := &fakeSQLFallback{result: domain.ToolResult{Facts: facts}}
			a := NewAssistant(Config{}, Deps{Provider: tc.prov, Registry: NewRegistry(nil, nil, sqlFB)})
			ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "feed pending today for cpt",
				AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
			if err != nil {
				t.Fatal(err)
			}
			if ans.Mode != tc.want {
				t.Fatalf("mode = %q, want %q (answer %q)", ans.Mode, tc.want, ans.Answer)
			}
			if !strings.Contains(ans.Answer, "-42") {
				t.Fatalf("expected the grounded figure, got %q", ans.Answer)
			}
		})
	}
}

func TestPlannedAnswerOfTheWrongShapeIsFlaggedNotShipped(t *testing.T) {
	// The model drafts a per-PEN read for a per-PARK, per-DAY question.
	wrong := "SELECT 'Directed feed kg' AS label, CAST(sum(directed_kg) AS text) AS value, shed_label AS scope FROM ceo_ai.feed_adherence WHERE tenant_id = 't1' AND feed_day >= '2026-09-05' AND feed_day < '2026-09-19' GROUP BY shed_label LIMIT 100"
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Directed feed kg", Value: "96", Scope: "Castro 1"}}}}
	cache := &setCountingCache{}
	a := NewAssistant(Config{}, Deps{Provider: &fakeProvider{byModel: true, plan: modelSQLPlan(wrong, domain.AnswerSpec{})},
		Registry: NewRegistry(nil, nil, sqlFB), Cache: cache})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: liveProofQuestion,
		AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModePartial {
		t.Fatalf("a mismatched answer must be downgraded to partial, got %q", ans.Mode)
	}
	for _, want := range []string{"may not match the question", "by park", "per day"} {
		if !strings.Contains(ans.Answer, want) {
			t.Fatalf("flag note missing %q: %q", want, ans.Answer)
		}
	}
	if cache.sets != 0 {
		t.Fatalf("a flagged answer must never be cached")
	}
}

func TestPlannedAnswerDeclaredShapeMismatchOnAPIRouteIsFlagged(t *testing.T) {
	exec := &fakeExec{spec: ports.ToolSpec{Name: "feed_direction_today", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "Directed kg", Value: "96", Unit: "kg"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{{
		ID: "0", Route: domain.RouteAPI, ToolName: "feed_direction_today",
		Declared: domain.AnswerSpec{Measure: "directed feed kg", Dimensions: []string{"shed_label"}},
	}}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "directed feed kg by park",
		AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModePartial || !strings.Contains(ans.Answer, "by park") {
		t.Fatalf("declared per-pen answer to a by-park question must be flagged, got mode=%q %q", ans.Mode, ans.Answer)
	}
}

func TestParseRequestedShape(t *testing.T) {
	cases := []struct {
		q     string
		dims  []string
		units []string
	}{
		{liveProofQuestion, []string{"day", "park"}, []string{"kg"}},
		{"average weight per animal by breed", []string{"breed"}, nil},
		{"average daily gain by sex", []string{"sex"}, nil},
		{"average kg per day for Castro 1", nil, []string{"kg"}},
		{"deaths month-wise in cbe", []string{"month"}, nil},
		{"vaccination compliance % per park weekly", []string{"park", "week"}, []string{"percent"}},
		{"feed by feed item for each pen", []string{"feed_item", "pen"}, nil},
		{"how many goats in cpt", nil, nil},
		{"sales revenue in rupees by buyer", []string{"buyer"}, []string{"money"}},
		{"which operators are behind", nil, nil},
	}
	for _, tc := range cases {
		got := ParseRequestedShape(tc.q, Window{})
		if strings.Join(got.Dimensions, ",") != strings.Join(tc.dims, ",") {
			t.Errorf("%q dims = %v, want %v", tc.q, got.Dimensions, tc.dims)
		}
		if strings.Join(got.Units, ",") != strings.Join(tc.units, ",") {
			t.Errorf("%q units = %v, want %v", tc.q, got.Units, tc.units)
		}
	}
}

func TestDimensionEvidenceReadsSelectAndGroupByOnly(t *testing.T) {
	// A day FILTER in WHERE is not a day GROUPING.
	sql := "SELECT 'x' AS label, CAST(sum(directed_kg) AS text) AS value, park_label AS scope FROM ceo_ai.feed_adherence WHERE tenant_id = 't1' AND feed_day >= '2026-09-05' GROUP BY park_label LIMIT 10"
	sub := domain.SubQuestion{Route: domain.RouteSQL, Params: map[string]any{"sql": sql}}
	issues := checkAnswerFit(ParseRequestedShape("feed kg per day by park", Window{}), sub, nil)
	if len(issues) != 1 || issues[0].Detail != "per day" {
		t.Fatalf("expected exactly the missing daily grouping, got %+v", issues)
	}
	// A month truncation is not a daily series.
	monthly := "SELECT 'x' AS label, CAST(sum(deaths) AS text) AS value, to_char(date_trunc('month', event_date), 'Mon YYYY') AS scope FROM ceo_ai.mortality_base WHERE tenant_id = 't1' GROUP BY date_trunc('month', event_date) LIMIT 10"
	sub = domain.SubQuestion{Route: domain.RouteSQL, Params: map[string]any{"sql": monthly}}
	if issues := checkAnswerFit(ParseRequestedShape("deaths per day", Window{}), sub, nil); len(issues) != 1 {
		t.Fatalf("monthly read must not satisfy a daily request, got %+v", issues)
	}
	if issues := checkAnswerFit(ParseRequestedShape("deaths by month", Window{}), sub, nil); len(issues) != 0 {
		t.Fatalf("monthly read satisfies a monthly request, got %+v", issues)
	}
}

// The row-cap marker the composer appends ("… N more not shown") is a count of
// hidden rows, not a business figure; it must not fail the grounding review.
func TestReviewDoesNotFlagTheComposersRowCapMarker(t *testing.T) {
	var facts []domain.Fact
	for i := 0; i < 56; i++ {
		facts = append(facts, domain.Fact{TenantID: "t1", Label: "Directed feed kg", Value: "120", Scope: "Coimbatore · day"})
	}
	res := []domain.ToolResult{{Facts: facts}}
	body := renderFacts(res[0])
	if !strings.Contains(body, "46 more not shown") {
		t.Fatalf("fixture must exercise the marker: %q", body)
	}
	var rv reviewer
	if v := rv.review(context.Background(), body, res, 1); !v.Grounded {
		t.Fatalf("row-cap marker flagged as ungrounded: %v", v.FailReasons)
	}
	if v := rv.review(context.Background(), body+"\nTotal 999.", res, 1); v.Grounded {
		t.Fatal("a genuinely ungrounded figure must still fail")
	}
}

// judgingProvider is a model planner that also judges answer fit and can
// re-plan with feedback (the Vertex planner's two optional capabilities).
type judgingProvider struct {
	fakeProvider
	fits         bool
	reason       string
	replan       domain.Plan
	judged       int
	feedbackSeen string
}

func (p *judgingProvider) JudgeFit(_ context.Context, _ string, _ []domain.Fact) (bool, string, TokenUsage, error) {
	p.judged++
	return p.fits, p.reason, TokenUsage{}, nil
}

func (p *judgingProvider) PlanWithFeedback(_ context.Context, q domain.Question, _ []domain.ResolvedEntities, _ []ports.ToolSpec, feedback string) (domain.Plan, TokenUsage, error) {
	p.feedbackSeen = feedback
	return p.replan, TokenUsage{}, nil
}

// A read-API tool that ignores the requested grouping is caught by the judge
// on the evidence, and the model re-plans ONCE with that feedback; the
// re-planned (fitting) read is what answers.
func TestJudgedMisfitIsReplannedOnceWithFeedback(t *testing.T) {
	counts := &fakeExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "Count", Value: "73", Scope: "CBE / Castro 2"}}}}
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{Facts: []domain.Fact{
		{TenantID: "t1", Label: "Live males", Value: "342", Scope: "Coimbatore"},
		{TenantID: "t1", Label: "Live males", Value: "242", Scope: "Channapatna"},
	}}}
	reg := NewRegistry(nil, nil, sqlFB)
	reg.Register(counts)
	fixed := "SELECT 'Live males' AS label, CAST(count(*) AS text) AS value, park_label AS scope FROM ceo_ai.animal_current_scope WHERE tenant_id = 't1' AND lifecycle_status = 'alive' AND sex = 'male' GROUP BY park_label LIMIT 10"
	prov := &judgingProvider{
		fakeProvider: fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{{ID: "0", Route: domain.RouteAPI, ToolName: "counts_breakdown"}}}},
		fits:         false, reason: "rows are per pen, not per park",
		replan: modelSQLPlan(fixed, domain.AnswerSpec{Measure: "live males", Dimensions: []string{"park"}}),
	}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "Split our live male animals by park.",
		AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
	if err != nil {
		t.Fatal(err)
	}
	if prov.judged != 1 {
		t.Fatalf("judge must run exactly once (bounded), ran %d", prov.judged)
	}
	if !strings.Contains(prov.feedbackSeen, "per pen, not per park") {
		t.Fatalf("re-plan must receive the judge's reason, got %q", prov.feedbackSeen)
	}
	if sqlFB.calls != 1 || sqlFB.lastSQL != fixed {
		t.Fatalf("the re-planned read must run: calls=%d sql=%s", sqlFB.calls, sqlFB.lastSQL)
	}
	if !strings.Contains(ans.Answer, "242") || strings.Contains(ans.Answer, "Castro 2") {
		t.Fatalf("answer must come from the fitting re-plan, got %q", ans.Answer)
	}
	if ans.Mode != domain.ModePlanned {
		t.Fatalf("a fitted re-plan is a planned answer, got %q", ans.Mode)
	}
}

// When the judge is satisfied nothing is re-planned.
func TestJudgedFitDoesNotReplan(t *testing.T) {
	exec := &fakeExec{spec: ports.ToolSpec{Name: "x", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "Live animals", Value: "713", Scope: "Channapatna"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &judgingProvider{fakeProvider: fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{{ID: "0", Route: domain.RouteAPI, ToolName: "x"}}}}, fits: true}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many live animals in cpt"})
	if err != nil {
		t.Fatal(err)
	}
	if prov.feedbackSeen != "" || exec.calls != 1 || ans.Mode != domain.ModePlanned {
		t.Fatalf("no re-plan expected: feedback=%q calls=%d mode=%q", prov.feedbackSeen, exec.calls, ans.Mode)
	}
}

// A catalog tool that advertises no period parameter cannot answer a question
// about a named period; without a re-plan the answer is flagged, never shipped
// as if it covered that period.
func TestPeriodQuestionOnAToolWithoutPeriodParamsIsFlagged(t *testing.T) {
	exec := &fakeExec{spec: ports.ToolSpec{Name: "snapshot_tool", Route: domain.RouteAPI, Params: []string{"park_label"}},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "Sold", Value: "682"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{{ID: "0", Route: domain.RouteAPI, ToolName: "snapshot_tool"}}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "How many animals were sold in the last 30 days?",
		AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModePartial || !strings.Contains(ans.Answer, "period you asked about") {
		t.Fatalf("period the tool cannot honour must be flagged, got mode=%q %q", ans.Mode, ans.Answer)
	}
	// A tool that does advertise a period parameter is not flagged.
	exec.spec.Params = []string{"from", "to"}
	ans, err = a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "How many animals were sold in the last 30 days?",
		AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModePlanned {
		t.Fatalf("period-capable tool must not be flagged, got %q %q", ans.Mode, ans.Answer)
	}
}

// scriptedSQL answers each statement from a function (probe vs draft vs repair).
type scriptedSQL struct {
	answer func(sql string) domain.ToolResult
	seen   []string
}

func (s *scriptedSQL) Execute(_ context.Context, _ domain.Actor, sql string, _ []any) (domain.ToolResult, error) {
	s.seen = append(s.seen, sql)
	r := s.answer(sql)
	r.Route, r.ToolName = domain.RouteSQL, "sql_fallback"
	return r, nil
}
func (s *scriptedSQL) ExecuteTrusted(ctx context.Context, a domain.Actor, sql string, args []any) (domain.ToolResult, error) {
	return s.Execute(ctx, a, sql, args)
}

type repairingProvider struct {
	fakeProvider
	reason string
	fixed  string
}

func (p *repairingProvider) RepairSQL(_ context.Context, _ domain.Question, _, reason, _, _ string) (string, TokenUsage, error) {
	p.reason = reason
	return p.fixed, TokenUsage{}, nil
}

// A model read that filters on a value the data does not use ("Female" when
// the column stores "female") is valid SQL that returns a confident 0. The
// filtered column's real values are probed and the model repairs ONCE with
// them; the repaired read answers.
func TestEmptyReadWithWrongFilterValueIsRepairedWithTheRealValues(t *testing.T) {
	draft := "SELECT 'Female animals' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.animal_current_scope WHERE tenant_id = 't1' AND lifecycle_status = 'alive' AND sex = 'Female' LIMIT 1"
	fixed := "SELECT 'Female animals' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.animal_current_scope WHERE tenant_id = 't1' AND lifecycle_status = 'alive' AND sex = 'female' LIMIT 1"
	sqlFB := &scriptedSQL{answer: func(sql string) domain.ToolResult {
		switch {
		case strings.Contains(sql, "GROUP BY sex"):
			return domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "value", Value: "female"}, {TenantID: "t1", Label: "value", Value: "male"}}}
		case strings.Contains(sql, "GROUP BY lifecycle_status"):
			return domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "value", Value: "alive"}, {TenantID: "t1", Label: "value", Value: "exited"}}}
		case strings.Contains(sql, "sex = 'female'"):
			return domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Female animals", Value: "985"}}}
		default:
			return domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Female animals", Value: "0"}}}
		}
	}}
	prov := &repairingProvider{fakeProvider: fakeProvider{byModel: true, plan: modelSQLPlan(draft, domain.AnswerSpec{})}, fixed: fixed}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB)})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "How many female animals are alive?"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prov.reason, `no value "Female"`) || !strings.Contains(prov.reason, "female, male") {
		t.Fatalf("repair reason must name the bad literal and the real values, got %q", prov.reason)
	}
	if strings.Contains(prov.reason, "lifecycle_status") {
		t.Fatalf("a literal that IS a stored value must not be reported: %q", prov.reason)
	}
	if !strings.Contains(ans.Answer, "985") {
		t.Fatalf("the repaired read must answer, got %q", ans.Answer)
	}
	for _, s := range sqlFB.seen {
		if err := sqlguard.Validate(s); err != nil {
			t.Fatalf("every probe/draft must pass the real guard: %v (%s)", err, s)
		}
	}
}

// A genuinely empty read whose literals are all real values is NOT repaired.
func TestEmptyReadWithRealFilterValuesIsLeftAlone(t *testing.T) {
	draft := "SELECT 'Deaths' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.animal_current_scope WHERE tenant_id = 't1' AND sex = 'male' LIMIT 1"
	sqlFB := &scriptedSQL{answer: func(sql string) domain.ToolResult {
		if strings.Contains(sql, "GROUP BY sex") {
			return domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "value", Value: "female"}, {TenantID: "t1", Label: "value", Value: "male"}}}
		}
		return domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Deaths", Value: "0"}}}
	}}
	prov := &repairingProvider{fakeProvider: fakeProvider{byModel: true, plan: modelSQLPlan(draft, domain.AnswerSpec{})}, fixed: "unused"}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB)})
	if _, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many male deaths"}); err != nil {
		t.Fatal(err)
	}
	if prov.reason != "" {
		t.Fatalf("no repair expected for a genuinely empty read, got %q", prov.reason)
	}
}

type setCountingCache struct{ sets int }

func (c *setCountingCache) Get(string) (domain.Answer, bool) { return domain.Answer{}, false }
func (c *setCountingCache) Set(string, domain.Answer)        { c.sets++ }

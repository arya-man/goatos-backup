package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// --- fakes ---

type fakeProvider struct {
	plan    domain.Plan
	err     error
	byModel bool
	calls   int
}

func (f *fakeProvider) Plan(_ context.Context, _ domain.Question, _ []domain.ResolvedEntities, _ []ports.ToolSpec) (domain.Plan, error) {
	f.calls++
	if f.err != nil {
		return domain.Plan{}, f.err
	}
	return f.plan, nil
}
func (f *fakeProvider) PlannedByModel() bool { return f.byModel }

type fakeMetrics struct {
	specs   []ports.MetricSpec
	lastReq ports.MetricQuery
	result  domain.ToolResult
}

func (f *fakeMetrics) Metrics(_ context.Context) ([]ports.MetricSpec, error) { return f.specs, nil }
func (f *fakeMetrics) Query(_ context.Context, _ domain.Actor, req ports.MetricQuery) (domain.ToolResult, error) {
	f.lastReq = req
	r := f.result
	r.Route = domain.RouteCube
	r.ToolName = req.Metric
	return r, nil
}

type fakeExec struct {
	spec   ports.ToolSpec
	result domain.ToolResult
	calls  int
	last   domain.SubQuestion
}

func (f *fakeExec) Spec() ports.ToolSpec { return f.spec }
func (f *fakeExec) Execute(_ context.Context, _ domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	f.calls++
	f.last = sub
	r := f.result
	r.Route = domain.RouteAPI
	r.ToolName = sub.ToolName
	return r, nil
}

type fakeSQLFallback struct {
	calls        int
	trustedCalls int
	lastSQL      string
	lastArgs     []any
	result       domain.ToolResult
}

func (f *fakeSQLFallback) Execute(_ context.Context, _ domain.Actor, sql string, _ []any) (domain.ToolResult, error) {
	f.calls++
	f.lastSQL = sql
	r := f.result
	r.Route = domain.RouteSQL
	r.ToolName = "sql_fallback"
	if r.Surface == "" {
		r.Surface = "Mesha operational data"
	}
	return r, nil
}

func (f *fakeSQLFallback) ExecuteTrusted(_ context.Context, _ domain.Actor, sql string, args []any) (domain.ToolResult, error) {
	f.trustedCalls++
	f.lastSQL = sql
	f.lastArgs = args
	r := f.result
	r.Route = domain.RouteSQL
	r.ToolName = "sql_fallback"
	if r.Surface == "" {
		r.Surface = "Mesha operational data"
	}
	return r, nil
}

type fakeAudit struct{ rec *ports.AuditRecord }

func (f *fakeAudit) Record(_ context.Context, rec ports.AuditRecord) error { f.rec = &rec; return nil }

type fakeMemory struct {
	remembered domain.ResolvedEntities
	recall     []domain.ResolvedEntities
}

func (f *fakeMemory) Recall(_ context.Context, _ domain.Actor, _ string) ([]domain.ResolvedEntities, error) {
	return f.recall, nil
}
func (f *fakeMemory) Remember(_ context.Context, _ domain.Actor, _ string, ent domain.ResolvedEntities) error {
	f.remembered = ent
	return nil
}

func leadershipActor() domain.Actor {
	return domain.Actor{TenantID: "t1", UserID: "u1", Role: permissions.RoleCEOInternal}
}

func newAsk(t *testing.T, d Deps) *Assistant {
	t.Helper()
	if d.Registry == nil {
		d.Registry = NewRegistry(nil, nil, nil)
	}
	if d.Parks == nil {
		d.Parks = testParks{}
	}
	return NewAssistant(Config{}, d)
}

// --- tests ---

func TestRoleGateRejectsNonLeadership(t *testing.T) {
	a := newAsk(t, Deps{Provider: &fakeProvider{}})
	_, err := a.Ask(context.Background(), domain.Question{
		Actor: domain.Actor{TenantID: "t1", UserID: "u1", Role: "operator"}, Text: "how many goats",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestCubeFirstRoutingForKPI(t *testing.T) {
	// Planner (mis)routes a KPI to SQL; enforcement must flip it to cube.
	metrics := &fakeMetrics{
		specs:  []ports.MetricSpec{{Name: "vaccination_overdue", Status: domain.MetricApproved}},
		result: domain.ToolResult{Surface: "Cube · vaccination_overdue", Facts: []domain.Fact{{TenantID: "t1", Label: "overdue", Value: "42"}}},
	}
	reg := NewRegistry(metrics, nil, nil)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "vaccination_overdue", Route: domain.RouteSQL},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "which sheds are overdue"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Answer, "42") {
		t.Fatalf("expected grounded 42, got %q", ans.Answer)
	}
	if len(ans.Citations) == 0 || ans.Citations[0].Route != domain.RouteCube {
		t.Fatalf("KPI must route through Cube, citations=%+v", ans.Citations)
	}
}

func TestOperationalQuestionRoutesToAPINotCube(t *testing.T) {
	metrics := &fakeMetrics{specs: []ports.MetricSpec{{Name: "vaccination_overdue"}}}
	exec := &fakeExec{
		spec:   ports.ToolSpec{Name: "feed_direction_preview", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "kg", Value: "310"}}},
	}
	reg := NewRegistry(metrics, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "feed_direction_preview", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "feed today"})
	if err != nil {
		t.Fatal(err)
	}
	if exec.calls != 1 {
		t.Fatalf("expected api executor called once, got %d", exec.calls)
	}
	if !strings.Contains(ans.Answer, "310") {
		t.Fatalf("expected 310, got %q", ans.Answer)
	}
}

func TestModelPlannedMissedVaccinationAPIReadBecomesAggregateMissedMetric(t *testing.T) {
	exec := &fakeExec{
		spec: ports.ToolSpec{Name: "vaccination_shed_summary", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{
			{TenantID: "t1", Label: "Vaccinations missed", Value: "0", Scope: "all parks"},
		}},
	}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "vaccination_shed_summary", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many animals missed vaccine"})
	if err != nil {
		t.Fatal(err)
	}
	if exec.last.Params["vaccination_intent"] != "missed" {
		t.Fatalf("vaccination_intent=%v want missed", exec.last.Params["vaccination_intent"])
	}
	if exec.last.Params["aggregate_total"] != "true" {
		t.Fatalf("aggregate_total=%v want true", exec.last.Params["aggregate_total"])
	}
	if strings.Contains(ans.Answer, "could not be retrieved") || !strings.Contains(ans.Answer, "Vaccinations missed") {
		t.Fatalf("expected clean missed-vaccination answer, got %q", ans.Answer)
	}
}

func TestAllParksVaccinationQuestionDropsInjectedPageScope(t *testing.T) {
	exec := &fakeExec{
		spec: ports.ToolSpec{Name: "vaccination_shed_summary", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{
			{TenantID: "t1", Label: "Vaccinations missed", Value: "0", Scope: "all parks"},
		}},
	}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "vaccination_shed_summary", Route: domain.RouteAPI, Params: map[string]any{
			"park_id": "00000000-0000-4000-8000-000000003001",
			"shed_id": "00000000-0000-4000-8000-000000004001",
		}},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})
	_, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many animals missed vaccination across all parks"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := exec.last.Params["park_id"]; ok {
		t.Fatalf("park_id should be dropped for all-parks question: %+v", exec.last.Params)
	}
	if _, ok := exec.last.Params["shed_id"]; ok {
		t.Fatalf("shed_id should be dropped for all-parks question: %+v", exec.last.Params)
	}
}

func TestModelPlannedVaccinationGraphAPIReadCarriesShedSeriesIntent(t *testing.T) {
	exec := &fakeExec{
		spec: ports.ToolSpec{Name: "vaccination_shed_summary", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{
			{TenantID: "t1", Label: "Vaccinations overdue", Value: "0", Scope: "CBE / Gandhi"},
			{TenantID: "t1", Label: "Vaccinations overdue", Value: "0", Scope: "CBE / Godel 1"},
		}},
	}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "vaccination_shed_summary", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "show vaccination overdue by shed as graph"})
	if err != nil {
		t.Fatal(err)
	}
	if exec.last.Params["vaccination_intent"] != "overdue" {
		t.Fatalf("vaccination_intent=%v want overdue", exec.last.Params["vaccination_intent"])
	}
	if exec.last.Params["group_by"] != "shed_label" {
		t.Fatalf("group_by=%v want shed_label", exec.last.Params["group_by"])
	}
	if ans.Chart == nil {
		t.Fatal("expected normalized API facts to produce a chart")
	}
}

func TestMultiToolDecompositionSynthesizesOneAnswer(t *testing.T) {
	metrics := &fakeMetrics{
		specs:  []ports.MetricSpec{{Name: "active_animals"}},
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "t1", Label: "goats", Value: "2567"}}},
	}
	exec := &fakeExec{
		spec:   ports.ToolSpec{Name: "admin_roster_coverage", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "gaps", Value: "3"}}},
	}
	reg := NewRegistry(metrics, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteCube},
		{ID: "1", ToolName: "admin_roster_coverage", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "animals and staffing gaps"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Answer, "2567") || !strings.Contains(ans.Answer, "3") {
		t.Fatalf("expected both facts, got %q", ans.Answer)
	}
	if len(ans.Citations) != 2 {
		t.Fatalf("expected 2 citations, got %d", len(ans.Citations))
	}
}

func TestNaturalActiveAnimalQuestionUsesLiveSQLFallbackForCPT(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Active animals", Value: "185", Scope: "goat"},
			{TenantID: "t1", Label: "Active animals", Value: "531", Scope: "sheep"},
		},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	api := &fakeExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "wrong", Value: "1"}}}}
	reg.Register(api)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many animals present in cpt"})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 1 {
		t.Fatalf("expected guarded SQL fallback to answer natural CPT count, got %d calls", sqlFB.calls)
	}
	if api.calls != 0 {
		t.Fatalf("natural CPT count must not fall through to API/MCP route, api calls=%d", api.calls)
	}
	for _, want := range []string{"185", "531"} {
		if !strings.Contains(ans.Answer, want) {
			t.Fatalf("answer missing %s: %q", want, ans.Answer)
		}
	}
	for _, wantSQL := range []string{"ceo_ai.animal_current_scope", "park_id IN ('00000000-0000-4000-8000-000000003002')", "lifecycle_status = 'alive'"} {
		if !strings.Contains(sqlFB.lastSQL, wantSQL) {
			t.Fatalf("SQL missing %q: %s", wantSQL, sqlFB.lastSQL)
		}
	}
	if err := sqlguard.Validate(sqlFB.lastSQL); err != nil {
		t.Fatalf("active-animal natural SQL must pass the real sqlguard validator: %v; sql=%s", err, sqlFB.lastSQL)
	}
	if len(ans.Citations) == 0 || ans.Citations[0].Route != domain.RouteSQL {
		t.Fatalf("expected SQL citation, got %+v", ans.Citations)
	}
}

func TestNaturalActiveAnimalOneToManyPageBoundaryDateShiftParkScopeSQLGuard(t *testing.T) {
	sql := activeAnimalsSQL(
		"t1",
		[]knownParkScope{{code: "CPT", label: "Channapatna", id: "00000000-0000-4000-8000-000000003002"}},
		"breed",
		"how many active animals in cpt by breed",
	)
	if err := sqlguard.Validate(sql); err != nil {
		t.Fatalf("active-animal SQL must pass the real sqlguard validator: %v; sql=%s", err, sql)
	}
	for _, want := range []string{
		"SELECT 'Active animals' AS label",
		"ceo_ai.animal_current_scope",
		"tenant_id = 't1'",
		"park_id IN ('00000000-0000-4000-8000-000000003002')",
		"lifecycle_status = 'alive'",
		"GROUP BY breed",
		"LIMIT 50",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("active-animal SQL missing %q: %s", want, sql)
		}
	}
	for _, forbidden := range []string{"/*", "*/", "--", ";"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("active-animal SQL must not contain %q: %s", forbidden, sql)
		}
	}
}

func TestNaturalFarmBornQuestionUsesOriginAndParkScope(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{{TenantID: "t1", Label: "Farm-born animals", Value: "42", Scope: "goat"}},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{byModel: true}, Registry: reg})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many animals are farm born in cbe"})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 1 {
		t.Fatalf("expected farm-born question to use SQL fallback, got %d calls", sqlFB.calls)
	}
	for _, want := range []string{
		"ceo_ai.animal_current_scope",
		"park_id IN ('00000000-0000-4000-8000-000000003001')",
		"origin_type = 'birth'",
		"lifecycle_status = 'alive'",
	} {
		if !strings.Contains(sqlFB.lastSQL, want) {
			t.Fatalf("farm-born SQL missing %q: %s", want, sqlFB.lastSQL)
		}
	}
	if !strings.Contains(ans.Answer, "42") || !strings.Contains(ans.Answer, "Farm-born") {
		t.Fatalf("expected farm-born answer, got %q", ans.Answer)
	}
}

func TestNaturalOwnFarmsQuestionDefaultsToKnownParks(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Farm-born animals", Value: "512", Scope: "Coimbatore"},
			{TenantID: "t1", Label: "Farm-born animals", Value: "345", Scope: "Channapatna"},
		},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{byModel: true}, Registry: reg})

	_, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many animals are from our own farms"})
	if err != nil {
		t.Fatal(err)
	}
	// "Our own farms" is EVERY active park, the third one included -- not a constant CBE/CPT pair.
	for _, want := range []string{
		"park_id IN ('00000000-0000-4000-8000-000000003002', '00000000-0000-4000-8000-000000003001', '00000000-0000-4000-8000-000000003099')",
		"origin_type = 'birth'",
		"park_label AS scope",
	} {
		if !strings.Contains(sqlFB.lastSQL, want) {
			t.Fatalf("own-farms SQL missing %q: %s", want, sqlFB.lastSQL)
		}
	}
}

func TestNaturalActiveAnimalQuestionToleratesTyposAndCBEAbbrev(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Active animals", Value: "805", Scope: "sheep"},
			{TenantID: "t1", Label: "Active animals", Value: "0", Scope: "goat"},
		},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "what abt cbe goats sheep split"})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 1 {
		t.Fatalf("expected typo/split question to use SQL fallback, got %d calls", sqlFB.calls)
	}
	if !strings.Contains(sqlFB.lastSQL, "park_id IN ('00000000-0000-4000-8000-000000003001')") {
		t.Fatalf("expected CBE scope in SQL, got %s", sqlFB.lastSQL)
	}
	if !strings.Contains(ans.Answer, "805") {
		t.Fatalf("answer missing SQL fact: %q", ans.Answer)
	}
}

func TestNaturalActiveAnimalFollowupUsesRememberedParkForBreed(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Active animals", Value: "531", Scope: "Anantapur Sheep"},
			{TenantID: "t1", Label: "Active animals", Value: "159", Scope: "Beetal"},
		},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI},
	}}}
	mem := &fakeMemory{recall: []domain.ResolvedEntities{{ParkLabel: "CPT"}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Memory: mem})

	ans, err := a.Ask(context.Background(), domain.Question{
		Actor:          leadershipActor(),
		ConversationID: "c1",
		Text:           "and their breed?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 1 {
		t.Fatalf("expected breed follow-up to use SQL fallback, got %d calls", sqlFB.calls)
	}
	if !strings.Contains(sqlFB.lastSQL, "breed AS scope") || !strings.Contains(sqlFB.lastSQL, "00000000-0000-4000-8000-000000003002") {
		t.Fatalf("expected remembered CPT breed SQL, got %s", sqlFB.lastSQL)
	}
	if !strings.Contains(ans.Answer, "Anantapur Sheep") || !strings.Contains(ans.Answer, "Beetal") {
		t.Fatalf("answer missing breed facts: %q", ans.Answer)
	}
}

func TestNaturalActiveAnimalGraphByPenUsesSQLAndReturnsChart(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Active animals", Value: "136", Scope: "Godel 2"},
			{TenantID: "t1", Label: "Active animals", Value: "121", Scope: "Mandela 1"},
		},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "mesha_count_by_scope", Route: domain.RouteToolbox},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "make graph of animals present in cpt by pen"})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 1 {
		t.Fatalf("expected SQL fallback call, got %d", sqlFB.calls)
	}
	if !strings.Contains(sqlFB.lastSQL, "shed_label AS scope") {
		t.Fatalf("expected pen/shed grouping SQL, got %s", sqlFB.lastSQL)
	}
	if ans.Chart == nil {
		t.Fatal("expected chart for graph-by-pen request")
	}
	if len(ans.Chart.X) != 2 || ans.Chart.X[0] != "Godel 2" || ans.Chart.Series[0].Data[0] != 136 {
		t.Fatalf("unexpected chart: %+v", ans.Chart)
	}
}

func TestNaturalWeighingQuestionToleratesAvgShorthand(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Average weight kg", Value: "21.4", Scope: "Castro 1"},
			{TenantID: "t1", Label: "Average weight kg", Value: "22.1", Scope: "Godel 2"},
		},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{Refusal: "wrong fallback"}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "which pens have lowest avg weight?"})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 1 {
		t.Fatalf("expected shorthand weighing question to use SQL fallback, got %d calls", sqlFB.calls)
	}
	if !strings.Contains(sqlFB.lastSQL, "ceo_ai.weighing_capture_activity") || !strings.Contains(sqlFB.lastSQL, "ORDER BY shed_weight_avg_kg ASC") {
		t.Fatalf("expected weighing pen SQL ordered lowest-first, got %s", sqlFB.lastSQL)
	}
	if !strings.Contains(ans.Answer, "Castro 1") || strings.Contains(ans.Answer, "wrong fallback") {
		t.Fatalf("expected grounded weighing answer, got %q", ans.Answer)
	}
}

func TestNaturalWeighingCountQuestionsUseDashboardDenominatorTerms(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{name: "lump sum", text: "how many animals weighed by lump sum in cbe", want: "weighing_category = 'per_shed_partition'"},
		{name: "per animal", text: "how many animals weighed per animal in cpt", want: "weighing_category = 'individual_animal'"},
		{name: "all weighed", text: "how many animals weighed in weight wise", want: "SELECT 'Animals weighed' AS label"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqlFB := &fakeSQLFallback{result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Animals weighed", Value: "1", Scope: "Coimbatore"}}}}
			reg := NewRegistry(nil, nil, sqlFB)
			a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{}, Registry: reg})
			_, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: tc.text})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sqlFB.lastSQL, "ceo_ai.weighing_capture_activity") || !strings.Contains(sqlFB.lastSQL, tc.want) {
				t.Fatalf("weighing SQL missing %q: %s", tc.want, sqlFB.lastSQL)
			}
			if tc.name != "all weighed" && strings.Contains(sqlFB.lastSQL, "shed_weight_avg_kg IS NOT NULL") {
				t.Fatalf("counting %s must not require shed averages: %s", tc.name, sqlFB.lastSQL)
			}
		})
	}
}

func TestNaturalFeedByWeightBandUsesDashboardReader(t *testing.T) {
	sub, ok := naturalSQLPlan(domain.Question{Actor: leadershipActor(), Text: "how many matched animals in feed by weight band"}, nil)
	if !ok {
		t.Fatal("feed-band matched animals should route to dedicated dashboard reader")
	}
	if sub.Route != domain.RouteAPI || sub.ToolName != "feed_weight_band_summary" {
		t.Fatalf("feed-band matched animals must use dashboard reader, got %s/%s: %+v", sub.Route, sub.ToolName, sub)
	}
}

func TestNaturalMortalityQuestionsUseMortalityBase(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{name: "kid deaths", text: "how many kid deaths this month", want: "sum(kid_deaths)"},
		{name: "adult deaths", text: "adult deaths in cbe", want: "sum(adult_deaths)"},
		{name: "cause established", text: "how many deaths had cause established", want: "sum(cause_established)"},
		{name: "rate", text: "mortality rate in cpt", want: "Mortality rate pct"},
		{name: "monthly", text: "show deaths by month", want: "date_trunc('month', event_date)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqlFB := &fakeSQLFallback{result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Deaths", Value: "1", Scope: "Coimbatore"}}}}
			reg := NewRegistry(nil, nil, sqlFB)
			a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{}, Registry: reg})
			_, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: tc.text, AsOf: time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sqlFB.lastSQL, "ceo_ai.mortality_base") || !strings.Contains(sqlFB.lastSQL, tc.want) {
				t.Fatalf("mortality SQL missing %q: %s", tc.want, sqlFB.lastSQL)
			}
			if err := sqlguard.Validate(sqlFB.lastSQL); err != nil {
				t.Fatalf("mortality SQL must pass sqlguard: %v; sql=%s", err, sqlFB.lastSQL)
			}
		})
	}
}

func TestNaturalFeedQuestionUsesAsOfDate(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{{TenantID: "t1", Label: "Feed variance kg", Value: "-42", Scope: "Godel 2"}},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{}, Registry: reg})

	asOf := time.Date(2026, 9, 18, 9, 0, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "feed pending today for cpt", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 1 {
		t.Fatalf("expected feed natural SQL fallback, got %d calls", sqlFB.calls)
	}
	for _, want := range []string{"ceo_ai.feed_adherence", "feed_day = '2026-09-18'", "park_label = 'Channapatna'"} {
		if !strings.Contains(sqlFB.lastSQL, want) {
			t.Fatalf("feed SQL missing %q: %s", want, sqlFB.lastSQL)
		}
	}
	if !strings.Contains(ans.Answer, "-42") {
		t.Fatalf("expected feed fact in answer, got %q", ans.Answer)
	}
}

func TestNaturalSQLQuestionRemembersScopedParkForFollowup(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{{TenantID: "t1", Label: "Feed variance kg", Value: "-42", Scope: "Godel 2"}},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	mem := &fakeMemory{}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{}, Registry: reg, Memory: mem})

	_, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), ConversationID: "c1", Text: "feed pending today for cpt"})
	if err != nil {
		t.Fatal(err)
	}
	if mem.remembered.ParkLabel != "Channapatna" {
		t.Fatalf("expected natural SQL turn to remember Channapatna, got %+v", mem.remembered)
	}
}

func TestNaturalHealthOneToManyPageBoundaryDateShiftParkScope(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{{TenantID: "t1", Label: "Vendor A", Value: "2", Scope: "Load 12345678"}},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{}, Registry: reg})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "source entry health issues in cpt"})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.trustedCalls != 1 || sqlFB.calls != 0 {
		t.Fatalf("expected health natural SQL to use trusted server-authored fallback, regular=%d trusted=%d", sqlFB.calls, sqlFB.trustedCalls)
	}
	// D0 trusted-SQL contract: the read goes through the governed ceo_ai view,
	// the tenant is ALWAYS $1 (bound by the executor from the session, never a
	// literal in the text) and the park scope rides as $2 in args.
	for _, want := range []string{"ceo_ai.source_entry_health_status", "tenant_id = $1", "park_location_id = $2"} {
		if !strings.Contains(sqlFB.lastSQL, want) {
			t.Fatalf("health SQL missing %q: %s", want, sqlFB.lastSQL)
		}
	}
	for _, forbidden := range []string{"tenant_id = '", "public.", "procurement_loads", "'t1'"} {
		if strings.Contains(sqlFB.lastSQL, forbidden) {
			t.Fatalf("trusted health SQL must not contain %q (tenant literal / raw public read): %s", forbidden, sqlFB.lastSQL)
		}
	}
	if len(sqlFB.lastArgs) != 1 || sqlFB.lastArgs[0] != "00000000-0000-4000-8000-000000003002" {
		t.Fatalf("expected the park location id as the single trusted arg ($2), got %v", sqlFB.lastArgs)
	}
	if err := sqlguard.ValidateTrusted(sqlFB.lastSQL); err != nil {
		t.Fatalf("trusted health SQL must satisfy the executor's trusted contract: %v", err)
	}
	trimmedSQL := strings.TrimSpace(sqlFB.lastSQL)
	if !strings.HasPrefix(strings.ToUpper(trimmedSQL), "SELECT") {
		t.Fatalf("trusted health SQL must begin with SELECT: %s", sqlFB.lastSQL)
	}
	for _, forbidden := range []string{"/*", "*/", "--", ";"} {
		if strings.Contains(trimmedSQL, forbidden) {
			t.Fatalf("trusted health SQL must not contain %q: %s", forbidden, sqlFB.lastSQL)
		}
	}
	if !strings.Contains(ans.Answer, "Vendor A") || !strings.Contains(ans.Answer, "2") {
		t.Fatalf("expected grounded health fact, got %q", ans.Answer)
	}
}

func TestModelCannotRequestTrustedSQLThroughParams(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{{TenantID: "t1", Label: "n", Value: "1"}},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{{
		ID:       "0",
		Route:    domain.RouteSQL,
		ToolName: "sql_fallback",
		Params: map[string]any{
			"sql":         "SELECT 'n' AS label, '1' AS value FROM ceo_ai.animal_current_scope WHERE tenant_id = 't1' LIMIT 1",
			"trusted_sql": "server_natural",
		},
	}}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})

	_, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "model drafted sql"})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 1 || sqlFB.trustedCalls != 0 {
		t.Fatalf("model-provided trusted_sql param must not reach trusted executor, regular=%d trusted=%d", sqlFB.calls, sqlFB.trustedCalls)
	}
}

func TestGenericHealthQuestionDoesNotUseSourceEntrySQL(t *testing.T) {
	sqlFB := &fakeSQLFallback{}
	health := &fakeExec{
		spec:   ports.ToolSpec{Name: "mesha_health_today", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "Health blockers", Value: "0"}}},
	}
	reg := NewRegistry(nil, nil, sqlFB)
	reg.Register(health)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{{
		ID:       "0",
		Route:    domain.RouteAPI,
		ToolName: "mesha_health_today",
	}}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})

	_, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "health blockers in cpt"})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 0 || sqlFB.trustedCalls != 0 {
		t.Fatalf("generic health question must not be captured by source-entry SQL, regular=%d trusted=%d", sqlFB.calls, sqlFB.trustedCalls)
	}
	if health.calls != 1 {
		t.Fatalf("expected planned health API route to run, got %d calls", health.calls)
	}
}

func TestNaturalAdultGoatCountDoesNotRouteToHealth(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{
		Facts: []domain.Fact{{TenantID: "t1", Label: "Active animals", Value: "185", Scope: "goat"}},
	}}
	reg := NewRegistry(nil, nil, sqlFB)
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{}, Registry: reg})

	_, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many adult goats in cpt"})
	if err != nil {
		t.Fatal(err)
	}
	if sqlFB.calls != 1 {
		t.Fatalf("expected adult goat count to use SQL fallback, got %d calls", sqlFB.calls)
	}
	for _, want := range []string{"ceo_ai.animal_current_scope", "species = 'goat'", "age_days >= 365"} {
		if !strings.Contains(sqlFB.lastSQL, want) {
			t.Fatalf("adult goat count SQL missing %q: %s", want, sqlFB.lastSQL)
		}
	}
	if strings.Contains(sqlFB.lastSQL, "source_entry_health_status") {
		t.Fatalf("adult goat count should not use health SQL, got %s", sqlFB.lastSQL)
	}
}

func TestNaturalSalesQuestionWinsOverAnimalCountWords(t *testing.T) {
	sales := &fakeExec{
		spec: ports.ToolSpec{Name: "sales_overview", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API · Sales overview", Facts: []domain.Fact{
			{TenantID: "t1", Label: "Sold animals this month in 2026-09", Value: "114"},
		}},
	}
	reg := NewRegistry(nil, nil, &fakeSQLFallback{})
	reg.Register(sales)
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{}, Registry: reg})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "sales this month how many animals sold?"})
	if err != nil {
		t.Fatal(err)
	}
	if sales.calls != 1 {
		t.Fatalf("expected sales overview executor, got %d calls", sales.calls)
	}
	if !strings.Contains(ans.Answer, "114") {
		t.Fatalf("expected sales fact, got %q", ans.Answer)
	}
}

func TestCacheKeyIncludesConversation(t *testing.T) {
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Registry: NewRegistry(nil, nil, nil)})
	asOf := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	q1 := domain.Question{Actor: leadershipActor(), ConversationID: "cpt-thread", Text: "and their breed?", AsOf: asOf}
	q2 := domain.Question{Actor: leadershipActor(), ConversationID: "cbe-thread", Text: "and their breed?", AsOf: asOf}
	if a.cacheKey(q1) == a.cacheKey(q2) {
		t.Fatalf("cache key must include conversation context for follow-up questions")
	}
}

func TestMaxStepsProducesPartial(t *testing.T) {
	exec := &fakeExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "n", Value: "10"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	subs := []domain.SubQuestion{}
	for i := 0; i < 5; i++ {
		subs = append(subs, domain.SubQuestion{ID: string(rune('0' + i)), ToolName: "counts_breakdown", Route: domain.RouteAPI})
	}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: subs}}
	a := NewAssistant(Config{MaxSteps: 2}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "big"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModePartial {
		t.Fatalf("expected partial mode, got %q", ans.Mode)
	}
	if exec.calls != 2 {
		t.Fatalf("max-steps=2 must cap executions, got %d", exec.calls)
	}
}

func TestPlannerFailureFallsBackToDeterministic(t *testing.T) {
	exec := &fakeExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "n", Value: "5"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	primary := &fakeProvider{err: errors.New("vertex down")}
	fb := &fakeProvider{byModel: false, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: primary, Fallback: fb, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "count by shed"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModeFallback {
		t.Fatalf("expected fallback mode, got %q", ans.Mode)
	}
}

func TestRuntimeReviewCatchesHallucinatedNumber(t *testing.T) {
	// Executor returns fact 42, but summary asserts an ungrounded 999.
	exec := &fakeExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Summary: "There are 999 animals.", Facts: []domain.Fact{{TenantID: "t1", Label: "count", Value: "42"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ans.Answer, "999") {
		t.Fatalf("hallucinated 999 must be stripped by review, got %q", ans.Answer)
	}
	if !strings.Contains(ans.Answer, "42") {
		t.Fatalf("grounded 42 must survive strict recompose, got %q", ans.Answer)
	}
}

func TestWriteRefusalFromPlan(t *testing.T) {
	prov := &fakeProvider{byModel: true, plan: domain.Plan{Refusal: "I'm read-only."}}
	a := newAsk(t, Deps{Provider: prov})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "approve load 5"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModeRefused {
		t.Fatalf("expected refused, got %q", ans.Mode)
	}
}

func TestScopeEscalationRefused(t *testing.T) {
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{{ID: "0", ToolName: "x", Route: domain.RouteAPI}}}}
	a := newAsk(t, Deps{Provider: prov})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "show me all tenants"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModeRefused {
		t.Fatalf("scope escalation must be refused, got %q / %q", ans.Mode, ans.Answer)
	}
	if prov.calls != 0 {
		t.Fatalf("planner must not run on scope escalation")
	}
}

func TestAuditRecordedWithRouteField(t *testing.T) {
	metrics := &fakeMetrics{specs: []ports.MetricSpec{{Name: "active_animals"}},
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "t1", Label: "n", Value: "2567"}}}}
	reg := NewRegistry(metrics, nil, nil)
	audit := &fakeAudit{}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteCube},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics, Audit: audit})
	if _, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "total animals"}); err != nil {
		t.Fatal(err)
	}
	if audit.rec == nil {
		t.Fatal("audit not recorded")
	}
	if len(audit.rec.Routes) == 0 || audit.rec.Routes[0] != domain.RouteCube {
		t.Fatalf("audit must carry route=cube, got %+v", audit.rec.Routes)
	}
	if audit.rec.QuestionHash == "" || audit.rec.RequestID == "" {
		t.Fatal("audit must carry request id + question hash")
	}
}

// failingAudit is an audit sink whose write always fails (a down Postgres).
type failingAudit struct{ err error }

func (f *failingAudit) Record(_ context.Context, _ ports.AuditRecord) error { return f.err }

// TestAuditSinkErrorIsLoggedNotSwallowed (PR #318 R2-5): a failed audit write
// never fails the answer, but it is logged at error level with the request
// id and the verdict's reasons — for a tenant-gate refusal that log line is
// the only remaining evidence of a D0 violation.
func TestAuditSinkErrorIsLoggedNotSwallowed(t *testing.T) {
	metrics := &fakeMetrics{specs: []ports.MetricSpec{{Name: "active_animals"}},
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "t1", Label: "n", Value: "2567"}}}}
	reg := NewRegistry(metrics, nil, nil)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteCube},
	}}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError}))
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics,
		Audit: &failingAudit{err: errors.New("audit insert: connection refused")}, Logger: logger})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "total animals"})
	if err != nil {
		t.Fatalf("a failed audit write must not fail the answer: %v", err)
	}
	if ans.Mode != domain.ModePlanned {
		t.Fatalf("answer must still be composed, got %s", ans.Mode)
	}
	out := logs.String()
	if !strings.Contains(out, "ceoai audit record failed") || !strings.Contains(out, "connection refused") || !strings.Contains(out, "request_id="+ans.RequestID) {
		t.Fatalf("audit sink error must be logged at error level with the request id, got %q", out)
	}

	// The tenant-gate refusal path goes through the same recordAudit and its
	// reasons reach the log line.
	logs.Reset()
	foreign := &fakeMetrics{specs: []ports.MetricSpec{{Name: "active_animals"}},
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "t-other", Label: "n", Value: "1"}}}}
	a = NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: NewRegistry(foreign, nil, nil), Metrics: foreign,
		Audit: &failingAudit{err: errors.New("audit insert: connection refused")}, Logger: logger})
	ans, err = a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "total animals"})
	if err != nil || ans.Mode != domain.ModeRefused {
		t.Fatalf("foreign facts must be refused: err=%v mode=%s", err, ans.Mode)
	}
	if out := logs.String(); !strings.Contains(out, "ceoai audit record failed") || !strings.Contains(out, "fail_reasons=tenant_gate:compose:foreign") {
		t.Fatalf("tenant-gate audit failure must be logged with its reason, got %q", out)
	}
}

func TestMemoryRememberedForFollowups(t *testing.T) {
	exec := &fakeExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{TenantID: "t1", Label: "n", Value: "5"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	mem := &fakeMemory{}
	convo := stubConvo{}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI, Params: map[string]any{"park_label": "Castro 1"}},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Memory: mem, Convo: convo})
	if _, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), ConversationID: "c1", Text: "count in Castro 1"}); err != nil {
		t.Fatal(err)
	}
	if mem.remembered.ParkLabel != "Castro 1" {
		t.Fatalf("expected remembered park Castro 1, got %q", mem.remembered.ParkLabel)
	}
}

func TestDraftMetricLabelled(t *testing.T) {
	metrics := &fakeMetrics{specs: []ports.MetricSpec{{Name: "feed_cost", Status: domain.MetricDraft}},
		result: domain.ToolResult{Surface: "Cube · feed_cost", MetricStatus: domain.MetricDraft, Facts: []domain.Fact{{TenantID: "t1", Label: "cost", Value: "1200"}}}}
	reg := NewRegistry(metrics, nil, nil)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "feed_cost", Route: domain.RouteCube},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "feed cost"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Answer, "draft metric") {
		t.Fatalf("draft metric must be labelled, got %q", ans.Answer)
	}
}

func TestCacheHitReturnsStoredAnswer(t *testing.T) {
	metrics := &fakeMetrics{specs: []ports.MetricSpec{{Name: "active_animals"}},
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "t1", Label: "n", Value: "2567"}}}}
	reg := NewRegistry(metrics, nil, nil)
	cache := &countingCache{m: map[string]domain.Answer{}}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteCube},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics, Cache: cache})
	q := domain.Question{Actor: leadershipActor(), Text: "total animals", AsOf: time.Now()}
	if _, err := a.Ask(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	prov.calls = 0
	if _, err := a.Ask(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 0 {
		t.Fatalf("cache hit must skip planner, planner ran %d times", prov.calls)
	}
}

// stub conversation store
type stubConvo struct{}

func (stubConvo) EnsureConversation(_ context.Context, _ domain.Actor, id, _ string) (string, string, error) {
	if id == "" {
		id = "new"
	}
	return id, "title", nil
}
func (stubConvo) AppendTurn(_ context.Context, _ domain.Actor, _ string, _ domain.Turn) error {
	return nil
}
func (stubConvo) RecentTurns(_ context.Context, _ domain.Actor, _ string, _ int) ([]domain.Turn, error) {
	return nil, nil
}

type countingCache struct {
	m map[string]domain.Answer
}

func (c *countingCache) Get(k string) (domain.Answer, bool) { a, ok := c.m[k]; return a, ok }
func (c *countingCache) Set(k string, a domain.Answer)      { c.m[k] = a }

// A park added on Configuration > Items & settings is understood by name and by code exactly like
// the first two: the planner matches against the tenant's live park list, not a constant.
func TestNaturalQuestionNamingANewParkIsScopedToIt(t *testing.T) {
	for _, text := range []string{"how many active animals in Hosur", "active animals at HSR"} {
		sqlFB := &fakeSQLFallback{result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Active animals", Value: "40", Scope: "Hosur"}}}}
		a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: &fakeProvider{byModel: true}, Registry: NewRegistry(nil, nil, sqlFB)})
		if _, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: text}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sqlFB.lastSQL, "'00000000-0000-4000-8000-000000003099'") {
			t.Fatalf("%q must be scoped to the new park, SQL: %s", text, sqlFB.lastSQL)
		}
		if strings.Contains(sqlFB.lastSQL, "3001'") || strings.Contains(sqlFB.lastSQL, "3002'") {
			t.Fatalf("%q must not include the other parks, SQL: %s", text, sqlFB.lastSQL)
		}
	}
}

package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
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
		result: domain.ToolResult{Surface: "Cube · vaccination_overdue", Facts: []domain.Fact{{Label: "overdue", Value: "42"}}},
	}
	reg := NewRegistry(metrics, nil, nil)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "vaccination_overdue", Route: domain.RouteSQL},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg, Metrics: metrics})
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
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{Label: "kg", Value: "310"}}},
	}
	reg := NewRegistry(metrics, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "feed_direction_preview", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg, Metrics: metrics})
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
			{Label: "Vaccinations missed", Value: "0", Scope: "all parks"},
		}},
	}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "vaccination_shed_summary", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
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

func TestModelPlannedVaccinationGraphAPIReadCarriesShedSeriesIntent(t *testing.T) {
	exec := &fakeExec{
		spec: ports.ToolSpec{Name: "vaccination_shed_summary", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{
			{Label: "Vaccinations overdue", Value: "0", Scope: "CBE / Gandhi"},
			{Label: "Vaccinations overdue", Value: "0", Scope: "CBE / Godel 1"},
		}},
	}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "vaccination_shed_summary", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
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
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{Label: "goats", Value: "2567"}}},
	}
	exec := &fakeExec{
		spec:   ports.ToolSpec{Name: "admin_roster_coverage", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{Label: "gaps", Value: "3"}}},
	}
	reg := NewRegistry(metrics, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteCube},
		{ID: "1", ToolName: "admin_roster_coverage", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg, Metrics: metrics})
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

func TestMaxStepsProducesPartial(t *testing.T) {
	exec := &fakeExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{Label: "n", Value: "10"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	subs := []domain.SubQuestion{}
	for i := 0; i < 5; i++ {
		subs = append(subs, domain.SubQuestion{ID: string(rune('0' + i)), ToolName: "counts_breakdown", Route: domain.RouteAPI})
	}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: subs}}
	a := NewAssistant(Config{MaxSteps: 2}, Deps{Provider: prov, Registry: reg})
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
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{Label: "n", Value: "5"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	primary := &fakeProvider{err: errors.New("vertex down")}
	fb := &fakeProvider{byModel: false, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: primary, Fallback: fb, Registry: reg})
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
		result: domain.ToolResult{Surface: "Mesha read API", Summary: "There are 999 animals.", Facts: []domain.Fact{{Label: "count", Value: "42"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})
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
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{Label: "n", Value: "2567"}}}}
	reg := NewRegistry(metrics, nil, nil)
	audit := &fakeAudit{}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteCube},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg, Metrics: metrics, Audit: audit})
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

func TestMemoryRememberedForFollowups(t *testing.T) {
	exec := &fakeExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "Mesha read API", Facts: []domain.Fact{{Label: "n", Value: "5"}}}}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(exec)
	mem := &fakeMemory{}
	convo := stubConvo{}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI, Params: map[string]any{"park_label": "Castro 1"}},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg, Memory: mem, Convo: convo})
	if _, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), ConversationID: "c1", Text: "count in Castro 1"}); err != nil {
		t.Fatal(err)
	}
	if mem.remembered.ParkLabel != "Castro 1" {
		t.Fatalf("expected remembered park Castro 1, got %q", mem.remembered.ParkLabel)
	}
}

func TestDraftMetricLabelled(t *testing.T) {
	metrics := &fakeMetrics{specs: []ports.MetricSpec{{Name: "feed_cost", Status: domain.MetricDraft}},
		result: domain.ToolResult{Surface: "Cube · feed_cost", MetricStatus: domain.MetricDraft, Facts: []domain.Fact{{Label: "cost", Value: "1200"}}}}
	reg := NewRegistry(metrics, nil, nil)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "feed_cost", Route: domain.RouteCube},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg, Metrics: metrics})
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
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{Label: "n", Value: "2567"}}}}
	reg := NewRegistry(metrics, nil, nil)
	cache := &countingCache{m: map[string]domain.Answer{}}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteCube},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg, Metrics: metrics, Cache: cache})
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

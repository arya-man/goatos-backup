package app

// latency_paths_test.go pins the three latency cuts that changed how an ask
// spends its model and database round trips: the plan's independent reads run
// concurrently, a repeat ask reuses the PLAN (never the data), and the fit
// judge returns its re-plan in the same call. Each test asserts the property
// that makes the cut SAFE, not merely that it is faster.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// slowExec sleeps before answering and records the maximum number of calls
// that were in flight at once.
type slowExec struct {
	spec    ports.ToolSpec
	delay   time.Duration
	inFlt   atomic.Int32
	maxFlt  atomic.Int32
	calls   atomic.Int32
	mu      sync.Mutex
	seenIDs []string
}

func (s *slowExec) Spec() ports.ToolSpec { return s.spec }

func (s *slowExec) Execute(_ context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	n := s.inFlt.Add(1)
	for {
		m := s.maxFlt.Load()
		if n <= m || s.maxFlt.CompareAndSwap(m, n) {
			break
		}
	}
	s.calls.Add(1)
	time.Sleep(s.delay)
	s.inFlt.Add(-1)
	s.mu.Lock()
	s.seenIDs = append(s.seenIDs, sub.ID)
	s.mu.Unlock()
	return domain.ToolResult{
		Route: domain.RouteAPI, ToolName: sub.ToolName,
		Facts: []domain.Fact{{TenantID: actor.TenantID, Label: sub.ToolName, Value: "1"}},
	}, nil
}

func threeToolPlan() domain.Plan {
	subs := make([]domain.SubQuestion, 0, 3)
	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		subs = append(subs, domain.SubQuestion{
			ID: name, Text: name, IntentClass: "x", Route: domain.RouteAPI, ToolName: name,
		})
	}
	return domain.Plan{SubQuestions: subs}
}

// TestIndependentReadsRunConcurrently is the concurrency contract: a
// three-read plan does NOT pay three read latencies end to end, and the
// results still come back in PLAN ORDER — everything downstream (compose, the
// fit checks, the audit) reads them positionally.
func TestIndependentReadsRunConcurrently(t *testing.T) {
	reg := NewRegistry(nil, nil, nil)
	execs := map[string]*slowExec{}
	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		e := &slowExec{spec: ports.ToolSpec{Name: name, Route: domain.RouteAPI, Description: name}, delay: 120 * time.Millisecond}
		execs[name] = e
		reg.Register(e)
	}
	a := newAsk(t, Deps{Provider: &fakeProvider{plan: threeToolPlan(), byModel: true}, Registry: reg})

	start := time.Now()
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "three reads please"})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	// Serial would be >= 360ms. Concurrent is one delay plus overhead; assert
	// well under the serial floor rather than on a tight timing window.
	if elapsed >= 300*time.Millisecond {
		t.Fatalf("three 120ms reads took %s; they did not run concurrently", elapsed)
	}
	for name, e := range execs {
		if e.calls.Load() != 1 {
			t.Fatalf("%s ran %d times, want exactly 1", name, e.calls.Load())
		}
	}
	if ans.Answer == "" || ans.Mode == domain.ModeRefused {
		t.Fatalf("expected an answer grounded in the three reads, got mode=%s answer=%q", ans.Mode, ans.Answer)
	}
}

// TestConcurrentReadsKeepPlanOrderAndTheStepBound proves the two invariants
// the serial loop provided for free: results/traces arrive in plan order, and
// MESHA_AI_MAX_STEPS still bounds the ask — the FIRST maxSteps sub-questions
// run and the rest are truncated, regardless of scheduling.
func TestConcurrentReadsKeepPlanOrderAndTheStepBound(t *testing.T) {
	var order []string
	budget := newAskBudget(2, time.Minute, time.Now)
	se := newStepExecutor(budget)
	subs := []domain.SubQuestion{
		{ID: "first", Route: domain.RouteAPI, ToolName: "a"},
		{ID: "second", Route: domain.RouteAPI, ToolName: "b"},
		{ID: "third", Route: domain.RouteAPI, ToolName: "c"},
	}
	// The slowest read is FIRST in the plan, so a results slice built in
	// completion order would come back reversed.
	delays := map[string]time.Duration{"first": 80 * time.Millisecond, "second": 30 * time.Millisecond, "third": 0}
	results, traces, truncated := se.run(context.Background(), leadershipActor(), subs,
		func(_ context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
			time.Sleep(delays[sub.ID])
			return domain.ToolResult{Facts: []domain.Fact{{TenantID: actor.TenantID, Label: sub.ID, Value: "1"}}}, nil
		})
	for _, r := range results {
		order = append(order, r.SubQuestionID)
	}
	if len(results) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("results must be in plan order and bounded by maxSteps, got %v", order)
	}
	if len(traces) != 2 || traces[0].SubQuestionID != "first" || traces[1].SubQuestionID != "second" {
		t.Fatalf("traces must be in plan order, got %+v", traces)
	}
	if !truncated {
		t.Fatal("spending the step allowance must still report truncated")
	}
}

// TestPlanCacheReusesTheRoutingAndNeverTheData is the whole point of the plan
// cache: the second ask does NOT call the planner again, and DOES read the
// database again — so a leader asking twice gets fresh numbers.
func TestPlanCacheReusesTheRoutingAndNeverTheData(t *testing.T) {
	reg := NewRegistry(nil, nil, nil)
	e := &fakeExec{spec: ports.ToolSpec{Name: "tool_a", Route: domain.RouteAPI, Description: "a"},
		result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Animals", Value: "12"}}}}
	reg.Register(e)
	prov := &fakeProvider{plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", Route: domain.RouteAPI, ToolName: "tool_a"},
	}}, byModel: true}
	a := newAsk(t, Deps{Provider: prov, Registry: reg})

	q := domain.Question{Actor: leadershipActor(), Text: "How many animals do we have?", ConversationID: "c1"}
	if _, err := a.Ask(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 || e.calls != 1 {
		t.Fatalf("first ask: planner=%d reads=%d, want 1/1", prov.calls, e.calls)
	}
	// A different request id each time keeps the ANSWER cache out of the way;
	// clear it so this test measures the plan cache alone.
	a.cache = nil
	if _, err := a.Ask(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("the repeat ask replanned (planner calls=%d); the plan cache did not hit", prov.calls)
	}
	if e.calls != 2 {
		t.Fatalf("the repeat ask must RE-READ the data, reads=%d want 2", e.calls)
	}
}

// TestPlanCacheIsKeyedByTenantAndUser is the replay guard. The conversation id
// is client-supplied, so a same-tenant colleague replaying another user's
// conversation id must not receive the plan resolved for that user, and
// another tenant must never see it at all.
func TestPlanCacheIsKeyedByTenantAndUser(t *testing.T) {
	a := newAsk(t, Deps{Provider: &fakeProvider{byModel: true}})
	owner := domain.Actor{TenantID: "t1", UserID: "u1", Role: leadershipActor().Role}
	colleague := domain.Actor{TenantID: "t1", UserID: "u2", Role: leadershipActor().Role}
	foreign := domain.Actor{TenantID: "t2", UserID: "u1", Role: leadershipActor().Role}

	q := domain.Question{Actor: owner, Text: "feed in Coimbatore", ConversationID: "shared-id"}
	key := a.planCacheKey(q)
	a.plans.set(key, owner, domain.Plan{SubQuestions: []domain.SubQuestion{{ID: "0", ToolName: "tool_a", Route: domain.RouteAPI}}})

	if _, ok := a.plans.get(key, owner); !ok {
		t.Fatal("the owner must get their own plan back")
	}
	colleagueQ := q
	colleagueQ.Actor = colleague
	if _, ok := a.plans.get(a.planCacheKey(colleagueQ), colleague); ok {
		t.Fatal("a same-tenant colleague replaying the conversation id must not hit another user's cached plan")
	}
	foreignQ := q
	foreignQ.Actor = foreign
	if _, ok := a.plans.get(a.planCacheKey(foreignQ), foreign); ok {
		t.Fatal("another tenant must never hit a cached plan")
	}
	// Even a key built wrong must not serve across tenants: the entry itself
	// re-checks the actor's tenant.
	if _, ok := a.plans.get(key, foreign); ok {
		t.Fatal("the cache entry must refuse an actor of another tenant")
	}
}

// TestCachedPlanShapeCarriesNoBoundWindow proves the cache stores the plan AS
// PLANNED. executePlan writes THIS ask's as-of day and resolved window into the
// sub-question params, and a stored copy carrying those literals would be a
// cached READ — tomorrow's ask would re-run yesterday's dates and report them
// as today's answer.
func TestCachedPlanShapeCarriesNoBoundWindow(t *testing.T) {
	reg := NewRegistry(nil, nil, nil)
	e := &fakeExec{spec: ports.ToolSpec{Name: "tool_a", Route: domain.RouteAPI, Description: "a", Params: []string{"from", "to"}},
		result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Feed", Value: "12"}}}}
	reg.Register(e)
	a := newAsk(t, Deps{Provider: &fakeProvider{byModel: true}, Registry: reg})

	plan := domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", Route: domain.RouteAPI, ToolName: "tool_a", Params: map[string]any{}},
	}}
	// The copy the cache would store is taken BEFORE execution, exactly as the
	// orchestrator takes it.
	shape := clonePlan(plan)

	q := domain.Question{Actor: leadershipActor(), Text: "feed fed in the last 7 days", AsOf: time.Now()}
	window, _ := ResolveWindow(q.Text, q.AsOf, nil)
	a.executePlan(context.Background(), q, plan.SubQuestions, window, newAskBudget(6, time.Minute, time.Now))

	if _, bound := plan.SubQuestions[0].Params[paramFrom]; !bound {
		t.Fatal("precondition: executePlan is expected to bind the window into the executed plan")
	}
	for _, s := range shape.SubQuestions {
		for _, k := range []string{paramFrom, paramTo, "as_of", paramWindowLabel} {
			if _, present := s.Params[k]; present {
				t.Fatalf("the cached shape carries a bound %q; it must store the plan as PLANNED, never this ask's dates", k)
			}
		}
	}
}

// foldedProvider implements the folded judge+replan capability and counts how
// many model calls the misfit path costs.
type foldedProvider struct {
	fakeProvider
	foldCalls     atomic.Int32
	feedbackCalls atomic.Int32
	replacement   domain.Plan
}

func (f *foldedProvider) JudgeFitAndReplan(_ context.Context, _ domain.Question, _ []domain.ResolvedEntities, _ []ports.ToolSpec, _ []domain.Fact) (bool, string, domain.Plan, TokenUsage, error) {
	f.foldCalls.Add(1)
	return false, "the rows are not broken down by park", f.replacement, TokenUsage{PromptTokens: 10, OutputTokens: 2}, nil
}

func (f *foldedProvider) PlanWithFeedback(_ context.Context, _ domain.Question, _ []domain.ResolvedEntities, _ []ports.ToolSpec, _ string) (domain.Plan, TokenUsage, error) {
	f.feedbackCalls.Add(1)
	return f.replacement, TokenUsage{}, nil
}

// TestFoldedJudgeReplanCostsOneRoundTrip: when the judge says the evidence
// does not answer, the replacement plan it returned in the SAME call is used,
// so the misfit path no longer pays a second serial planner round trip.
func TestFoldedJudgeReplanCostsOneRoundTrip(t *testing.T) {
	reg := NewRegistry(nil, nil, nil)
	first := &fakeExec{spec: ports.ToolSpec{Name: "tool_total", Route: domain.RouteAPI, Description: "total", Params: []string{"from", "to", "park_label"}},
		result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Feed", Value: "40"}}}}
	second := &fakeExec{spec: ports.ToolSpec{Name: "tool_by_park", Route: domain.RouteAPI, Description: "by park", Params: []string{"from", "to", "park_label", "group_by"}},
		result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Feed", Scope: "CBE", Value: "25"}, {TenantID: "t1", Label: "Feed", Scope: "CPT", Value: "15"}}}}
	reg.Register(first, second)

	prov := &foldedProvider{
		fakeProvider: fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
			{ID: "0", Route: domain.RouteAPI, ToolName: "tool_total", Params: map[string]any{}},
		}}},
		replacement: domain.Plan{SubQuestions: []domain.SubQuestion{
			{ID: "0", Route: domain.RouteAPI, ToolName: "tool_by_park", Params: map[string]any{"group_by": "park_label"}},
		}},
	}
	a := newAsk(t, Deps{Provider: prov, Registry: reg})

	if _, err := a.Ask(context.Background(), domain.Question{
		Actor: leadershipActor(), Text: "Total feed fed by park over the last 7 days",
	}); err != nil {
		t.Fatal(err)
	}
	if prov.foldCalls.Load() != 1 {
		t.Fatalf("expected exactly one folded judge+replan call, got %d", prov.foldCalls.Load())
	}
	if prov.feedbackCalls.Load() != 0 {
		t.Fatalf("the folded call already returned the plan; the separate re-plan must not run (%d calls)", prov.feedbackCalls.Load())
	}
	if second.calls == 0 {
		t.Fatal("the replacement plan the folded call returned was never executed")
	}
}

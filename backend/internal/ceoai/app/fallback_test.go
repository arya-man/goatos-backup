package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// fakeErroringExec simulates an in-process readtools executor whose backing
// reader is not wired: it returns a NIL Go error but a non-nil
// domain.ToolResult.Err (exactly what countsBreakdownExecutor/
// feedDirectionTodayExecutor do today when their reader closure is nil).
type fakeErroringExec struct {
	spec  ports.ToolSpec
	calls int
}

func (f *fakeErroringExec) Spec() ports.ToolSpec { return f.spec }
func (f *fakeErroringExec) Execute(_ context.Context, _ domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	f.calls++
	return domain.ToolResult{
		Surface: "Mesha read API", ToolName: sub.ToolName, Route: domain.RouteAPI,
		Facts: []domain.Fact{}, Err: errors.New(sub.ToolName + " reader not wired"),
	}, nil
}

// TestAPITierFailureFallsThroughToCube is the P1-3 regression test: the
// counts_breakdown API tool errors (unwired reader — a real, common failure
// mode per P1-2), and the runtime fallback must retry the SAME question
// through the aliased Cube metric (active_animals) and return REAL data,
// never the generic "isn't available yet" degrade.
func TestAPITierFailureFallsThroughToCube(t *testing.T) {
	exec := &fakeErroringExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI}}
	metrics := &fakeMetrics{
		specs: []ports.MetricSpec{{Name: "active_animals"}},
		result: domain.ToolResult{
			Surface: "Cube · active_animals",
			Facts:   []domain.Fact{{Label: "Goats", Value: "972"}, {Label: "Sheep", Value: "336"}},
		},
	}
	reg := NewRegistry(metrics, nil, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg, Metrics: metrics})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "count by breed"})
	if err != nil {
		t.Fatal(err)
	}
	if exec.calls != 1 {
		t.Fatalf("expected the API executor to be tried once before falling back, got %d calls", exec.calls)
	}
	if strings.Contains(ans.Answer, "isn't available") || strings.Contains(ans.Answer, "couldn't fully verify") {
		t.Fatalf("expected a real fallback answer, got a degrade message: %q", ans.Answer)
	}
	if !strings.Contains(ans.Answer, "972") {
		t.Fatalf("expected the Cube fallback fact (972) grounding the answer, got %q", ans.Answer)
	}
	if len(ans.Citations) == 0 || ans.Citations[0].Route != domain.RouteCube {
		t.Fatalf("expected the answer to cite the Cube fallback route, got %+v", ans.Citations)
	}
}

// TestAPITierFailureFallsThroughToToolbox proves the feed_direction_today
// path specifically: no Cube feed metric exists (per
// docs/ceo-ai/coverage-matrix.md), so the only real fallback is the MCP
// Toolbox mesha_feed_direction_summary tool. An errored/unwired feed API
// executor must fall through to the Toolbox and return real data.
type fakeToolbox struct {
	name   string
	result domain.ToolResult
	calls  int
}

func (f *fakeToolbox) Tools(_ context.Context) ([]ports.ToolSpec, error) {
	return []ports.ToolSpec{{Name: f.name, Route: domain.RouteToolbox}}, nil
}
func (f *fakeToolbox) Call(_ context.Context, _ domain.Actor, tool string, _ map[string]any) (domain.ToolResult, error) {
	f.calls++
	r := f.result
	r.Route = domain.RouteToolbox
	r.ToolName = tool
	return r, nil
}

func TestAPITierFailureFallsThroughToToolbox(t *testing.T) {
	exec := &fakeErroringExec{spec: ports.ToolSpec{Name: "feed_direction_today", Route: domain.RouteAPI}}
	toolbox := &fakeToolbox{
		name:   "mesha_feed_direction_summary",
		result: domain.ToolResult{Surface: "Mesha toolbox · feed", Facts: []domain.Fact{{Label: "Castro 1 · Feed A", Value: "310"}}},
	}
	reg := NewRegistry(nil, toolbox, nil)
	reg.Register(exec)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "feed_direction_today", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "which feed directions are blocked"})
	if err != nil {
		t.Fatal(err)
	}
	if exec.calls != 1 {
		t.Fatalf("expected the API executor to be tried once, got %d calls", exec.calls)
	}
	if toolbox.calls != 1 {
		t.Fatalf("expected the fallback to reach the toolbox exactly once, got %d calls", toolbox.calls)
	}
	if !strings.Contains(ans.Answer, "310") {
		t.Fatalf("expected the toolbox fallback fact (310) grounding the answer, got %q", ans.Answer)
	}
}

// TestIncompleteVerdictTriggersRetryBeforeDowngrade proves review.go's
// Complete=false verdict is honored (previously ignored at orchestrator.go's
// review-correction site): a multi-part question where one part's API tool
// fails must be retried through its fallback tier BEFORE the pipeline
// downgrades to the honest "couldn't fully verify" message.
func TestIncompleteVerdictTriggersRetryBeforeDowngrade(t *testing.T) {
	failing := &fakeErroringExec{spec: ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI}}
	metrics := &fakeMetrics{
		specs:  []ports.MetricSpec{{Name: "active_animals"}},
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{Label: "Goats", Value: "972"}}},
	}
	reg := NewRegistry(metrics, nil, nil)
	reg.Register(failing)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI},
	}}}
	a := NewAssistant(Config{ReviewEnabled: true}, Deps{Provider: prov, Registry: reg, Metrics: metrics})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "count by breed"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Answer, "972") {
		t.Fatalf("expected the fallback-recovered fact to ground the final answer, got %q", ans.Answer)
	}
}

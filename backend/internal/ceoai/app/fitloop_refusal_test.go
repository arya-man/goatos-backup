package app

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// refusingFoldedProvider returns a verdict of "does not answer" TOGETHER with a
// plan that carries a refusal. It deliberately does NOT implement
// FeedbackPlanner: with no separate re-plan capability wired, the ONLY way the
// replacement tool can run is the folded path adopting the refused plan, so the
// assertion below cannot pass for an unrelated reason.
type refusingFoldedProvider struct {
	fakeProvider
	folds       atomic.Int32
	refusal     string
	replacement domain.Plan
}

func (f *refusingFoldedProvider) JudgeFitAndReplan(_ context.Context, _ domain.Question, _ []domain.ResolvedEntities, _ []ports.ToolSpec, _ []domain.Fact) (bool, string, domain.Plan, TokenUsage, error) {
	f.folds.Add(1)
	alt := f.replacement
	alt.Refusal = f.refusal
	return false, "the rows are not broken down by park", alt, TokenUsage{PromptTokens: 10, OutputTokens: 2}, nil
}

// TestARefusedFoldedPlanIsNeverExecuted is the CONSUMER-side half of the
// folded-replan refusal guard, and it exists because the producer-side pin was
// the only one: deleting `alt.Refusal == ""` in answerFit left the entire
// ./internal/ceoai/... suite green, because the only test that could see it
// asserted the PLANNER still carries the refusal.
//
// The divergence that condition closes: the model answers "this does not fit,
// and here is why I will not answer it", with sub-questions attached. On the
// first-plan path that response REFUSES. If the folded path adopts the plan and
// runs it anyway, the same model response means two different things depending
// on which path read it — the exact two-paths-disagree defect the refusal
// carry-through was written for, left open on the side that acts on it.
//
// Table-driven on purpose: the refusing case and the identical NON-refusing
// case run through the same wiring, so the test cannot pass because the
// replacement was unrunnable.
func TestARefusedFoldedPlanIsNeverExecuted(t *testing.T) {
	for _, tc := range []struct {
		name       string
		refusal    string
		wantRuns   bool
		wantDetail string
	}{
		{
			name:       "a plan carrying a refusal is no plan",
			refusal:    "I cannot answer that from the herd register",
			wantRuns:   false,
			wantDetail: "the refused plan was executed on the folded path while the first-plan path refuses it",
		},
		{
			name:       "the same plan without a refusal still runs",
			refusal:    "",
			wantRuns:   true,
			wantDetail: "the folded replacement stopped running at all; the refusal check is over-firing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewRegistry(nil, nil, nil)
			first := &fakeExec{
				spec:   ports.ToolSpec{Name: "tool_total", Route: domain.RouteAPI, Description: "total", Params: []string{"from", "to", "park_label"}},
				result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Feed", Value: "40"}}},
			}
			second := &fakeExec{
				spec: ports.ToolSpec{Name: "tool_by_park", Route: domain.RouteAPI, Description: "by park", Params: []string{"from", "to", "park_label", "group_by"}},
				result: domain.ToolResult{Facts: []domain.Fact{
					{TenantID: "t1", Label: "Feed", Scope: "CBE", Value: "25"},
					{TenantID: "t1", Label: "Feed", Scope: "CPT", Value: "15"},
				}},
			}
			reg.Register(first, second)

			prov := &refusingFoldedProvider{
				fakeProvider: fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
					{ID: "0", Route: domain.RouteAPI, ToolName: "tool_total", Params: map[string]any{}},
				}}},
				refusal: tc.refusal,
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
			if prov.folds.Load() != 1 {
				t.Fatalf("precondition: expected exactly one folded judge+replan call, got %d", prov.folds.Load())
			}
			if ran := second.calls > 0; ran != tc.wantRuns {
				t.Fatalf("%s (replacement ran=%v, want %v)", tc.wantDetail, ran, tc.wantRuns)
			}
		})
	}
}

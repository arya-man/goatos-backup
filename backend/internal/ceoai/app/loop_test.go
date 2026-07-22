package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// TestStepExecutorRecordsToolResultErrInTrace is the P2-5 regression test.
// readtools executors (e.g. countsBreakdownExecutor when its reader is not
// wired) report failure via a non-nil domain.ToolResult.Err while returning a
// NIL Go error, so the pipeline can still compose an honest degraded answer
// instead of aborting the whole turn. Before the fix, stepExecutor.run only
// recorded the Go `err` into StepTrace.Err, so this exact — very common —
// failure mode never showed up in the internal admin trace: a failed tool
// step was indistinguishable from a successful empty one.
func TestStepExecutorRecordsToolResultErrInTrace(t *testing.T) {
	se := newStepExecutor(6, 0)
	sub := domain.SubQuestion{ID: "0", ToolName: "counts_breakdown", Route: domain.RouteAPI}

	exec := func(_ context.Context, _ domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
		// Mirrors readtools.countsBreakdownExecutor.Execute when unwired: nil
		// Go error, but ToolResult.Err set.
		return domain.ToolResult{
			SubQuestionID: sub.ID, Route: sub.Route, ToolName: sub.ToolName,
			Facts: []domain.Fact{}, Err: errors.New("counts data reader not wired"),
		}, nil
	}

	_, traces, truncated := se.run(context.Background(), domain.Actor{TenantID: "t1"}, []domain.SubQuestion{sub}, exec)
	if truncated {
		t.Fatalf("did not expect truncation")
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].Err == "" {
		t.Fatalf("expected StepTrace.Err to carry the ToolResult.Err failure, got empty string (failed step invisible in trace)")
	}
	if traces[0].Err != "counts data reader not wired" {
		t.Fatalf("StepTrace.Err = %q, want %q", traces[0].Err, "counts data reader not wired")
	}
}

// TestStepExecutorGoErrorStillRecorded proves the fix did not regress the
// existing behavior: a real Go error from exec (e.g. a cancelled sub-step)
// still lands in the trace.
func TestStepExecutorGoErrorStillRecorded(t *testing.T) {
	se := newStepExecutor(6, 0)
	sub := domain.SubQuestion{ID: "0", ToolName: "vaccination_overdue", Route: domain.RouteCube}
	wantErr := errors.New("cube metric service not wired")

	exec := func(_ context.Context, _ domain.Actor, _ domain.SubQuestion) (domain.ToolResult, error) {
		return domain.ToolResult{}, wantErr
	}

	_, traces, _ := se.run(context.Background(), domain.Actor{TenantID: "t1"}, []domain.SubQuestion{sub}, exec)
	if len(traces) != 1 || traces[0].Err != wantErr.Error() {
		t.Fatalf("expected trace to carry Go error %q, got %+v", wantErr, traces)
	}
}

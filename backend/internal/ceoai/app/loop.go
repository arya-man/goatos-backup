package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// bounded step executor: orchestrate MANY tools in one turn, capped by
// MESHA_AI_MAX_STEPS and a wall-clock budget, producing an honest partial when
// the bound is hit. Each step is one sub-question execution recorded as a
// StepTrace (internal-only).
type stepExecutor struct {
	budget *askBudget
	now    func() time.Time
}

// newStepExecutor binds the executor to the ASK's budget, so a second call to
// executePlan (the fit re-plan) continues spending the same allowance instead
// of starting a fresh one.
func newStepExecutor(budget *askBudget) stepExecutor {
	now := time.Now
	if budget != nil && budget.now != nil {
		now = budget.now
	}
	return stepExecutor{budget: budget, now: now}
}

// run executes each sub-question via exec, bounded by maxSteps and wallClock.
func (se stepExecutor) run(
	ctx context.Context,
	actor domain.Actor,
	subs []domain.SubQuestion,
	exec func(context.Context, domain.Actor, domain.SubQuestion) (domain.ToolResult, error),
) (results []domain.ToolResult, traces []domain.StepTrace, truncated bool) {
	for _, sub := range subs {
		if !se.budget.take() {
			truncated = true
			break
		}
		start := se.now()
		stepCtx, cancel := se.budget.withDeadline(ctx)
		res, err := exec(stepCtx, actor, sub)
		cancel()
		if err != nil {
			res = domain.ToolResult{SubQuestionID: sub.ID, Route: sub.Route, ToolName: sub.ToolName, Err: err}
		}
		res.SubQuestionID = sub.ID
		if res.Route == "" {
			res.Route = sub.Route
		}
		if res.ToolName == "" {
			res.ToolName = sub.ToolName
		}
		results = append(results, res)
		// Tool executors (readtools) report a failed step via ToolResult.Err
		// while returning a nil Go error (so the step loop doesn't abort the
		// whole turn) — e.g. "counts data reader not wired". Recording only
		// `err` here dropped that failure from the internal step trace/admin
		// audit entirely (P2-5): a failed tool step looked identical to a
		// successful empty one. Prefer the Go error, then fall back to the
		// ToolResult's own error so either failure mode is visible.
		traceErr := err
		if traceErr == nil {
			traceErr = res.Err
		}
		traces = append(traces, domain.StepTrace{
			SubQuestionID: sub.ID, Route: sub.Route, ToolName: sub.ToolName,
			StartedAt: start, DurationMS: se.now().Sub(start).Milliseconds(),
			RowCount: len(res.Facts), Err: errString(traceErr),
		})
	}
	return results, traces, truncated
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

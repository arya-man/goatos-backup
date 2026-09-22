package app

import (
	"context"
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// maxParallelReads bounds how many of a plan's sub-questions execute at the
// same time. A leadership plan is one to four reads, so this covers the whole
// plan in one wave without letting a pathological plan fan out over the
// read-only database pool or the Cube/Toolbox clients. The reads are
// INDEPENDENT by construction: a sub-question names one tool and its own
// params, and nothing in the executor passes one read's result to another.
const maxParallelReads = 4

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

// run executes the sub-questions via exec, bounded by maxSteps and wallClock.
//
// The reads run CONCURRENTLY, bounded by maxParallelReads, because a two-read
// plan otherwise pays both database/Cube latencies end to end while a leader
// waits. Three properties are preserved exactly as the serial loop had them:
//
//   - the ASK's single budget still decides which sub-questions run at all,
//     and it is still spent in plan order, so MESHA_AI_MAX_STEPS bounds the
//     question and the first N sub-questions are the ones that run when the
//     allowance runs short (truncated=true, as before);
//   - results and traces come back in PLAN ORDER regardless of which read
//     finished first — the composer, the answer-fit checks and the audit all
//     read them positionally;
//   - every step still gets its own deadline carved out of what is LEFT of
//     the ask's wall clock, so no read outlives the answer.
func (se stepExecutor) run(
	ctx context.Context,
	actor domain.Actor,
	subs []domain.SubQuestion,
	exec func(context.Context, domain.Actor, domain.SubQuestion) (domain.ToolResult, error),
) (results []domain.ToolResult, traces []domain.StepTrace, truncated bool) {
	// Spend the allowance in plan order BEFORE fanning out, so which
	// sub-questions run does not depend on goroutine scheduling.
	runnable := make([]domain.SubQuestion, 0, len(subs))
	for _, sub := range subs {
		if !se.budget.take() {
			truncated = true
			break
		}
		runnable = append(runnable, sub)
	}
	if len(runnable) == 0 {
		return nil, nil, truncated
	}

	gotResults := make([]domain.ToolResult, len(runnable))
	gotTraces := make([]domain.StepTrace, len(runnable))

	parallel := maxParallelReads
	if parallel > len(runnable) {
		parallel = len(runnable)
	}
	slots := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, sub := range runnable {
		wg.Add(1)
		// scale-guard:ignore: bounded fan-out over ONE plan's sub-questions (<= MESHA_AI_MAX_STEPS, at most maxParallelReads in flight), not a per-row loop.
		go func(i int, sub domain.SubQuestion) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			gotResults[i], gotTraces[i] = se.runOne(ctx, actor, sub, exec)
		}(i, sub)
	}
	wg.Wait()
	return gotResults, gotTraces, truncated
}

// runOne executes one sub-question and returns its result and step trace.
func (se stepExecutor) runOne(
	ctx context.Context,
	actor domain.Actor,
	sub domain.SubQuestion,
	exec func(context.Context, domain.Actor, domain.SubQuestion) (domain.ToolResult, error),
) (domain.ToolResult, domain.StepTrace) {
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
	return res, domain.StepTrace{
		SubQuestionID: sub.ID, Route: sub.Route, ToolName: sub.ToolName,
		StartedAt: start, DurationMS: se.now().Sub(start).Milliseconds(),
		RowCount: len(res.Facts), Err: errString(traceErr),
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

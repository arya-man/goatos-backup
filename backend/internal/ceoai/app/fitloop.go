package app

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// RelevanceJudge is the optional model capability that reads the evidence a
// plan actually returned and says whether it answers the question as asked —
// same measure, same breakdown, same period. It is the generic catch for a
// read the orchestrator cannot inspect (an in-process read API or Toolbox tool
// that ignores a requested grouping/period). It only JUDGES: it never picks a
// tool, sees credentials or changes scope.
type RelevanceJudge interface {
	JudgeFit(ctx context.Context, question string, facts []domain.Fact) (fits bool, reason string, usage TokenUsage, err error)
}

// FeedbackPlanner is the optional model capability to re-plan a question with
// server feedback about why the previous plan's read did not fit. The feedback
// is kept apart from the question text so it can never shift the
// server-resolved period or be mistaken for the user's words.
type FeedbackPlanner interface {
	PlanWithFeedback(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec, feedback string) (domain.Plan, TokenUsage, error)
}

// maxJudgedFacts bounds the evidence handed to the judge (prompt size).
const maxJudgedFacts = 25

// normalizePlan applies the deterministic, scope-preserving plan normalizers
// (Cube-first, living-herd census, utilization for overload questions,
// vaccination intent) to a plan — the first one and any re-plan alike.
func (a *Assistant) normalizePlan(ctx context.Context, q domain.Question, plan *domain.Plan) {
	// CUBE-FIRST enforcement: a sub-question that maps to a governed Cube metric
	// is forced to route=cube regardless of what the planner proposed.
	a.enforceCubeFirst(ctx, plan.SubQuestions)

	// Leadership "how many do we have" means the LIVING herd. Deterministically
	// prefer the active-animal census over the all-time total (which includes
	// exited/dead animals) for a plain headcount/species-split question, unless the
	// user explicitly asked for the all-time total.
	preferActiveCensus(q.Text, plan.SubQuestions)

	// "Who is overloaded / at capacity" is answered by the operator UTILIZATION
	// ratio (assigned ÷ daily capacity); guarantee it is queried per operator.
	plan.SubQuestions = ensureUtilizationForOverload(q.Text, plan.SubQuestions)

	normalizeVaccinationIntent(q.Text, plan.SubQuestions)
}

// executePlan threads the as-of day and the server-resolved period into every
// sub-question, runs them through the bounded step executor, repairs a
// rejected model-drafted SQL once, and retries failed/empty results down the
// Cube -> API -> Toolbox -> SQL tiers. It returns the model token usage of the
// repair calls.
func (a *Assistant) executePlan(ctx context.Context, q domain.Question, subs []domain.SubQuestion, window Window) ([]domain.ToolResult, []domain.StepTrace, bool, TokenUsage) {
	// Thread the resolved as-of business instant into every sub-question's
	// params (P1-4) so a scoped/as-of question reaches the reader that honors it.
	injectAsOf(subs, q.AsOf)

	// Thread the server-resolved period (plan v3 D1.2) the same way: from/to
	// ISO business dates on every sub-question. A model-drafted SQL read on a
	// current-state view (no date column) is converted to an explicit "as of
	// now" read here, so the guard never rejects it and the composer says so.
	injectWindow(subs, window)
	prepareSQLWindows(subs, window, q.AsOf)

	se := newStepExecutor(a.cfg.MaxSteps, a.cfg.WallClock)
	results, traces, truncated := se.run(ctx, q.Actor, subs, a.registry.Execute)
	if ctx.Err() != nil {
		return results, traces, truncated, TokenUsage{}
	}

	// One-shot SQL repair (plan v3 D1.3): a model-drafted sql_fallback that the
	// guard rejected or Postgres refused is re-prompted ONCE with the reason and
	// the view's schema card, then re-run through the same guard.
	usage := a.repairSQLResults(ctx, q, subs, results, &traces)

	// Runtime fallback (P1-3): before composing, retry any errored/empty
	// result at the next tier in Cube -> API -> Toolbox -> SQL order.
	a.retryFailedResults(ctx, q.Actor, subs, results)
	return results, traces, truncated, usage
}

// answerFit returns the ways the executed plan does not answer the question
// as asked, a feedback sentence for a re-plan, and the judge's token usage.
func (a *Assistant) answerFit(ctx context.Context, q domain.Question, req RequestedShape, subs []domain.SubQuestion, results []domain.ToolResult, catalog []ports.ToolSpec) ([]FitIssue, string, TokenUsage) {
	issues := planFitIssues(req, subs, results)
	var reasons []string
	for _, is := range issues {
		reasons = append(reasons, "the answer is not broken down "+is.Detail)
	}
	for _, is := range periodCapabilityIssues(q, req, subs, catalog) {
		issues = append(issues, is)
		reasons = append(reasons, is.Detail)
	}
	var usage TokenUsage
	if j, ok := a.provider.(RelevanceJudge); ok && a.provider.PlannedByModel() {
		var facts []domain.Fact
		for _, r := range results {
			if r.Err != nil {
				continue
			}
			for _, f := range r.Facts {
				if len(facts) >= maxJudgedFacts {
					break
				}
				facts = append(facts, f)
			}
		}
		if len(facts) > 0 {
			fits, reason, u, err := j.JudgeFit(ctx, q.Text, facts)
			usage = u
			if err == nil && !fits {
				issues = append(issues, FitIssue{Kind: "judge", Detail: "for the exact measure, breakdown or period you asked for"})
				if strings.TrimSpace(reason) != "" {
					reasons = append(reasons, strings.TrimSpace(reason))
				}
			}
		}
	}
	return dedupeIssues(issues), strings.Join(reasons, "; "), usage
}

// replanForFit asks the model planner ONCE for a different plan, telling it
// why the first read did not answer the question. It returns ok=false when no
// feedback-capable model planner is wired or the re-plan is unusable.
func (a *Assistant) replanForFit(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec, feedback string) (domain.Plan, TokenUsage, bool) {
	fp, ok := a.provider.(FeedbackPlanner)
	if !ok || !a.provider.PlannedByModel() {
		return domain.Plan{}, TokenUsage{}, false
	}
	plan, usage, err := fp.PlanWithFeedback(ctx, q, mem, catalog, feedback)
	if err != nil || plan.Refusal != "" || len(plan.SubQuestions) == 0 {
		return domain.Plan{}, usage, false
	}
	return plan, usage, true
}

// rangeParamHints are parameter names that let a tool read an arbitrary
// period; a tool advertising only a calendar-grain parameter ("month", "year")
// or a single day ("as_of", "date") can bind only that grain.
var rangeParamHints = []string{"from", "to", "time_range", "period", "window", "since", "start", "end", "range"}

func paramNamed(p, hint string) bool {
	p = strings.ToLower(strings.TrimSpace(p))
	return p == hint || strings.HasPrefix(p, hint+"_") || strings.HasSuffix(p, "_"+hint)
}

// toolCanBindWindow reports whether a catalog tool's advertised parameters can
// express the requested period.
func toolCanBindWindow(params []string, w Window) bool {
	for _, p := range params {
		for _, h := range rangeParamHints {
			if paramNamed(p, h) {
				return true
			}
		}
	}
	for _, p := range params {
		switch {
		case paramNamed(p, "month"):
			if w.From.Day() == 1 && w.To.Equal(endOfMonth(w.From)) {
				return true
			}
		case paramNamed(p, "year"):
			if w.From.Equal(startOfYear(w.From)) && (w.To.Equal(endOfYear(w.From)) || w.To.Year() == w.From.Year()) {
				return true
			}
		case paramNamed(p, "as_of"), paramNamed(p, "date"), paramNamed(p, "day"):
			if w.FromDate() == w.ToDate() {
				return true
			}
		}
	}
	return false
}

// periodCapabilityIssues flags a sub-question that cannot honour the period the
// question named: a catalog tool that advertises no period parameter, or a Cube
// read planned without a time_range. (Model-drafted SQL is held to the period
// by the window guard at execution; "today" is every read's as-of day.)
func periodCapabilityIssues(q domain.Question, req RequestedShape, subs []domain.SubQuestion, catalog []ports.ToolSpec) []FitIssue {
	if req.Window.IsZero() || isAsOfDay(req.Window, q) {
		return nil
	}
	specs := map[string]ports.ToolSpec{}
	for _, s := range catalog {
		specs[s.Name] = s
	}
	for _, sub := range subs {
		switch sub.Route {
		case domain.RouteCube:
			if tr, _ := sub.Params["time_range"].(string); strings.TrimSpace(tr) == "" {
				return []FitIssue{{Kind: "window", Detail: "for the period you asked about (the chosen metric was read without a period)"}}
			}
		case domain.RouteAPI, domain.RouteToolbox:
			spec, ok := specs[sub.ToolName]
			if !ok {
				continue
			}
			if !toolCanBindWindow(spec.Params, req.Window) {
				return []FitIssue{{Kind: "window", Detail: "for the period you asked about (tool " + sub.ToolName + " cannot be filtered by period)"}}
			}
		}
	}
	return nil
}

// hasUsableResult reports whether any sub-question returned grounding facts.
func hasUsableResult(results []domain.ToolResult) bool {
	for _, r := range results {
		if r.Err == nil && len(r.Facts) > 0 {
			return true
		}
	}
	return false
}

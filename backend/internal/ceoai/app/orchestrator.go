// Package app is the ceoai agentic runtime: plan -> decompose -> orchestrate
// many tools in one turn (Cube-first) -> synthesize ONE grounded answer, behind
// a bounded step loop with a runtime review pass. It depends only on ports;
// adapters (Vertex, Cube, Toolbox, sqlguard, persistence, safety, read
// services) are injected. The assistant is READ-ONLY and tenant/role scope
// comes only from the session Actor.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/ceoai/app/guard"
	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Config holds runtime-tunable orchestration knobs (validate-or-reject).
type Config struct {
	MaxSteps      int
	WallClock     time.Duration
	ReviewEnabled bool
	ModelVersion  string
	PromptVersion string
	MemoryTurns   int
}

func (c Config) withDefaults() Config {
	if c.MaxSteps <= 0 {
		c.MaxSteps = 6
	}
	if c.WallClock <= 0 {
		c.WallClock = 25 * time.Second
	}
	if c.MemoryTurns <= 0 {
		c.MemoryTurns = 6
	}
	if c.ModelVersion == "" {
		c.ModelVersion = "gemini-3.8-flash"
	}
	if c.PromptVersion == "" {
		c.PromptVersion = "v1"
	}
	return c
}

// Assistant is the leadership read-only agentic orchestrator.
type Assistant struct {
	cfg       Config
	provider  ports.AIProvider // primary planner (Vertex)
	fallback  ports.AIProvider // deterministic keyword planner (route.ts port)
	registry  *Registry
	metrics   ports.MetricService
	moderator ports.Moderator
	convo     ports.ConversationStore
	memory    ports.MemoryStore
	cache     ports.Cache
	limiter   ports.RateLimiter
	budget    ports.Budget
	audit     ports.AuditSink
	critic    ports.Reviewer
	telemetry ports.Telemetry
	sem       *Semaphore
	log       *slog.Logger
	now       func() time.Time
}

// Deps bundles the injected ports for NewAssistant.
type Deps struct {
	Provider  ports.AIProvider
	Fallback  ports.AIProvider
	Registry  *Registry
	Metrics   ports.MetricService
	Moderator ports.Moderator
	Convo     ports.ConversationStore
	Memory    ports.MemoryStore
	Cache     ports.Cache
	Limiter   ports.RateLimiter
	Budget    ports.Budget
	Audit     ports.AuditSink
	Critic    ports.Reviewer
	Telemetry ports.Telemetry
	Semaphore *Semaphore
	Logger    *slog.Logger
}

// NewAssistant constructs the orchestrator. Provider and Registry are required;
// other ports may be nil and degrade gracefully.
func NewAssistant(cfg Config, d Deps) *Assistant {
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	sem := d.Semaphore
	if sem == nil {
		sem = NewSemaphore(0)
	}
	// Session memory is what lets a follow-up ("and yesterday?", "why is that
	// one behind?") bind the park/shed the prior turn resolved. A nil MemoryStore
	// from the wiring (the default in bootstrap) would silently drop that scope
	// on every follow-up, so default to the bounded in-process store here rather
	// than leaving conversational context unwired.
	memory := d.Memory
	if memory == nil {
		memory = NewInMemoryMemory(0, 0)
	}
	return &Assistant{
		cfg: cfg.withDefaults(), provider: d.Provider, fallback: d.Fallback,
		registry: d.Registry, metrics: d.Metrics, moderator: d.Moderator,
		convo: d.Convo, memory: memory, cache: d.Cache, limiter: d.Limiter,
		budget: d.Budget, audit: d.Audit, critic: d.Critic, telemetry: d.Telemetry,
		sem: sem, log: log,
		now: time.Now,
	}
}

// progressFn receives coarse pipeline phase labels for progressive streaming.
// It is nil on the non-streaming path. phase is a stable enum ("planning",
// "querying", "synthesizing"); label is a coarse, user-safe route tag
// ("Consulting Cube · operator_vaccination_overdue") — NEVER chain-of-thought,
// reasoning, or internal step traces.
type progressFn func(phase, label string)

func (p progressFn) emit(phase, label string) {
	if p != nil {
		p(phase, label)
	}
}

// askOptions carries streaming-only knobs. The non-streaming Ask passes the zero
// value; AskStream supplies a progress callback and skips the optional Gemini
// critic (an extra Vertex round-trip) on the pre-first-frame path — the compose
// step is grounded-by-construction and the synchronous heuristic reviewer still
// runs, so groundedness is preserved without blocking the first frame.
type askOptions struct {
	progress        progressFn
	skipModelCritic bool
}

// Ask is the single entry point: one leadership turn -> one grounded Answer.
func (a *Assistant) Ask(ctx context.Context, q domain.Question) (domain.Answer, error) {
	return a.ask(ctx, q, askOptions{})
}

func (a *Assistant) ask(ctx context.Context, q domain.Question, opts askOptions) (ans domain.Answer, err error) {
	start := a.now()
	requestID := uuid.NewString()

	// Terminal metric fires on EVERY return path (auth refusal, rate/budget
	// rejection, cache hit, planner failure, grounded answer). tool is the
	// resolved read tier, updated once tools have run; status is the outcome
	// enum. Labels stay bounded (no tenant/actor/request/question text).
	resolvedTool := "none"
	defer func() {
		if a.telemetry != nil {
			a.telemetry.RecordRequest(ctx, resolvedTool, telemetryStatus(ans, err),
				float64(a.now().Sub(start).Milliseconds()))
		}
	}()

	if !isLeadership(q.Actor) || q.Actor.TenantID == "" {
		return domain.Answer{}, ErrForbidden
	}
	if strings.TrimSpace(q.Text) == "" {
		return domain.Answer{}, ErrEmptyQuestion
	}
	if q.AsOf.IsZero() {
		q.AsOf = biztime.BusinessDayStart(a.now())
	}
	if a.limiter != nil && !a.limiter.Allow(q.Actor.TenantID, q.Actor.UserID) {
		if a.telemetry != nil {
			a.telemetry.RateLimitTrip(ctx)
		}
		return a.plainAnswer(requestID, q.ConversationID, domain.ModeRefused,
			"You're sending requests too quickly. Please wait a moment and try again."), nil
	}
	if a.budget != nil {
		if ok, msg := a.budget.Reserve(ctx, q.Actor); !ok {
			if a.telemetry != nil {
				a.telemetry.BudgetRejection(ctx)
			}
			return a.plainAnswer(requestID, q.ConversationID, domain.ModeRefused, msg), nil
		}
	}

	// A question that NAMES another tenant/organisation (or carries an
	// identifier that is not this session's tenant) is refused, never silently
	// re-scoped: answering it with OUR number reads to the asker as the other
	// organisation's number, which is a disclosure they can act on even though
	// the tenant binding held and no foreign row was ever read.
	if foreign, why := guard.ForeignScopeReference(q.Text, q.Actor.TenantID); foreign {
		if a.telemetry != nil {
			a.telemetry.InjectionBlocked(ctx)
		}
		a.log.WarnContext(ctx, "ceoai refused a foreign-scope question",
			"reason", why, "request_id", requestID, "tenant_id", q.Actor.TenantID)
		return a.refusal(requestID, q.ConversationID,
			"I can only answer for your own organization, and I can't answer a question that names another tenant or an identifier from outside your session — even to say what your own figure is. Ask it without that scope and I'll answer for your organization."), nil
	}

	scan := guard.Scan(q.Text)
	if scan.ScopeEscalation {
		if a.telemetry != nil {
			a.telemetry.InjectionBlocked(ctx)
		}
		return a.refusal(requestID, q.ConversationID,
			"I can only answer for your own organization and role. Tenant and access scope come from your session and can't be changed by the question."), nil
	}
	if a.moderator != nil {
		if ok, refusal := a.moderator.CheckInbound(ctx, q.Text); !ok {
			return a.refusal(requestID, q.ConversationID, refusal), nil
		}
	}

	cacheKey := a.cacheKey(q)
	if a.cache != nil {
		hit, ok := a.cache.Get(cacheKey)
		if a.telemetry != nil {
			a.telemetry.CacheLookup(ctx, ok)
		}
		if ok {
			hit.RequestID = requestID
			return hit, nil
		}
	}

	if err := a.sem.Acquire(ctx); err != nil {
		return a.plainAnswer(requestID, q.ConversationID, domain.ModePartial,
			"The assistant is busy right now. Please try again in a few seconds."), nil
	}
	defer a.sem.Release()

	var mem []domain.ResolvedEntities
	if a.memory != nil && q.ConversationID != "" {
		mem, _ = a.memory.Recall(ctx, q.Actor, q.ConversationID)
	}

	catalog := a.registry.Catalog(ctx)

	// First meaningful frame: emit "planning" BEFORE the (slow) planner round-trip
	// so the UI shows progressive status within <1s instead of a frozen blank.
	opts.progress.emit("planning", "")

	// PLANNER-FIRST (fix/ceo-ai-planner-first-routing): the model plans every
	// question. The deterministic keyword/template path runs ONLY when no model
	// planner is configured or the model call failed, and then only answers a
	// question whose shape it actually covers (fallbackFit below). It used to
	// run BEFORE the model and win on any topic word ("feed", "weight",
	// "vaccination"…), answering a different hard-coded metric labelled
	// "fallback" while Vertex was up — which made the schema-card SQL path
	// unreachable for every common topic.
	//
	// Real token accounting (plan v3 D1.1): the planner's Vertex usageMetadata
	// plus any repair call, recorded to the budget instead of len/4 when reported.
	plan, planned, usage, err := a.planWithFallback(ctx, q, mem, catalog)
	if err != nil {
		// A cancelled/expired context is the client disconnecting (or the wall
		// clock firing), not a transient planner fault: propagate it so the
		// caller (streaming transport) stops upstream work instead of degrading
		// to a graceful answer for a request nobody is listening to.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.Answer{}, ctxErr
		}
		return a.plainAnswer(requestID, q.ConversationID, domain.ModePartial,
			"I couldn't process that request just now. Please try again."), nil
	}
	// Audit breadcrumbs for the plan-shaping decisions below.
	var fitAudit []string
	if plan.Refusal != "" {
		// A refusal that claims the farm does not RECORD something is checked
		// against the schema cards before a leader is told it: "no rows yet" and
		// "not modelled" are different answers, and only the second is a refusal.
		// One re-plan naming the covering views; if the planner still declines,
		// the reply says the read could not be built rather than asserting the
		// records do not exist.
		if covering := coveringSources(q.Text, reporting.Cards(), catalog); notTrackedRefusal(plan.Refusal) && len(covering) > 0 {
			fitAudit = append(fitAudit, "refusal_rechecked_against_schema_cards")
			if alt, altUsage, ok := a.replanForFit(ctx, q, mem, catalog, coverageFeedback(covering)); ok {
				usage = usage.add(altUsage)
				plan = alt
				fitAudit = append(fitAudit, "replanned_after_not_tracked_refusal")
			} else {
				return a.refusal(requestID, q.ConversationID,
					"I couldn't build a read for that question right now. The underlying records exist, so please rephrase it and I'll try again."), nil
			}
		} else {
			return a.refusal(requestID, q.ConversationID, plan.Refusal), nil
		}
	}

	// CUBE-FIRST enforcement: a sub-question that maps to a governed Cube metric
	// is forced to route=cube regardless of what the planner proposed.
	a.normalizePlan(ctx, q, &plan)

	// The server-resolved period (plan v3 D1.2) and the generic requested
	// answer shape (grouping, unit, period) the answer must correspond to.
	window, _ := ResolveWindow(q.Text, q.AsOf, nil)
	requested := ParseRequestedShape(q.Text, window)

	// Fallback relevance gate: without the model, a deterministic plan answers
	// only when its declared/visible shape covers the question. Otherwise the
	// honest reply is "can't answer that precisely right now", never a
	// different metric.
	if !planned && len(plan.SubQuestions) > 0 {
		if issues := fallbackFit(q, requested, plan.SubQuestions); len(issues) > 0 {
			reasons := []string{"fallback_unfit"}
			for _, is := range issues {
				reasons = append(reasons, "fallback_unfit:"+is.Kind)
			}
			a.recordAudit(ctx, q, requestID, q.ConversationID, domain.ModeFallback, nil, nil,
				domain.ReviewVerdict{ScopeSafe: true, FailReasons: reasons}, start)
			return a.plainAnswer(requestID, q.ConversationID, domain.ModeFallback,
				fallbackCannotAnswer(plan.SubQuestions, issues)), nil
		}
	}

	mode := domain.ModePlanned
	if !planned {
		mode = domain.ModeFallback
	}

	// "querying <route>" frame when tools run — a coarse route label, not reasoning.
	opts.progress.emit("querying", queryLabel(plan.SubQuestions))

	results, traces, truncated, execUsage := a.executePlan(ctx, q, plan.SubQuestions, window)
	usage = usage.add(execUsage)
	// If the client disconnected during tool execution, abort rather than
	// composing/reviewing/persisting an answer for a dead request.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return domain.Answer{}, ctxErr
	}

	// Answer fit (planned path): the measure, grouping, unit and period the
	// question asked for must be what the plan declared and what actually ran.
	// Checked generically (fit.go) and — when a model judge is wired — by the
	// model on the evidence itself, which also catches a read-API tool that
	// ignores a requested grouping/period. A mismatch gets ONE re-plan with the
	// reason as feedback; whatever still does not fit is flagged on the answer,
	// the mode is downgraded to partial (so it is never cached) and the audit
	// records why. Nothing here can widen scope: the re-plan goes through the
	// same guard, tenant binding and Cube-first normalization as the first.
	var fitIssues []FitIssue
	if planned {
		var feedback string
		var judgeUsage TokenUsage
		fitIssues, feedback, judgeUsage = a.answerFit(ctx, q, requested, plan.SubQuestions, results, catalog)
		usage = usage.add(judgeUsage)
		if len(fitIssues) > 0 {
			fitAudit = append(fitAudit, "answer_fit_first_plan:"+feedback)
			if alt, altUsage, ok := a.replanForFit(ctx, q, mem, catalog, feedback); ok {
				usage = usage.add(altUsage)
				a.normalizePlan(ctx, q, &alt)
				altResults, altTraces, altTruncated, altExecUsage := a.executePlan(ctx, q, alt.SubQuestions, window)
				usage = usage.add(altExecUsage)
				if ctxErr := ctx.Err(); ctxErr != nil {
					return domain.Answer{}, ctxErr
				}
				if hasUsableResult(altResults) {
					plan, results, traces, truncated = alt, altResults, append(traces, altTraces...), altTruncated
					fitIssues = planFitIssues(requested, plan.SubQuestions, results)
					fitAudit = append(fitAudit, "replanned_for_fit")
				}
			}
		}
	}
	// A read that ERRORED is not a read that found nothing. When every read
	// failed there is no evidence at all, so the answer must say that plainly
	// instead of composing a "partial" around failure lines — and the audit
	// records it, rather than the empty fit issue being dropped silently below.
	if allReadsFailed(results) {
		a.recordAudit(ctx, q, requestID, q.ConversationID, domain.ModePartial, results, traces,
			domain.ReviewVerdict{ScopeSafe: true, FailReasons: append([]string{"all_reads_failed"}, fitAudit...)}, start)
		return a.plainAnswer(requestID, q.ConversationID, domain.ModePartial,
			"I couldn't read the data this question needs right now, so I have no figure to give you. Please try again shortly, or check the source screen directly."), nil
	}
	// An empty read earns the re-plan above, but "nothing found" is itself an
	// honest answer, so it is never flagged as a shape mismatch.
	fitIssues = withoutKind(fitIssues, "empty")
	if truncated && mode == domain.ModePlanned {
		mode = domain.ModePartial
	}

	// The dominant read tier + rows grounding this answer are known now; label
	// the terminal metric and record the row histogram.
	resolvedTool = primaryTool(results)
	if a.telemetry != nil {
		a.telemetry.RecordToolRows(ctx, resolvedTool, totalRows(results))
	}

	// State the period once (D4): a "Window: …" line for a windowed or
	// as-of-now model-drafted SQL read, appended after every retry so it
	// describes what actually ran. It is a self-grounded fact stamped with the
	// actor's tenant and carries no new citation.
	if wr := windowResult(q.Actor, plan.SubQuestions, results, window); wr != nil {
		results = append(results, *wr)
	}

	// Optional partial-synthesis frame before the (grounded) compose + review.
	opts.progress.emit("synthesizing", "")

	var comp composer
	// Tenant gate (plan v3 D0 "Chart / facts"): a fact set that is not wholly
	// the actor's never reaches compose, chart or cache — it becomes a refusal.
	body, citations, sections, composeErr := comp.composeFor(q.Actor, results)
	if composeErr != nil {
		return a.tenantGateRefusal(ctx, q, requestID, start, "compose", composeErr, results, traces), nil
	}
	for i := range citations {
		citations[i].PlannedByModel = planned
	}

	var rvw reviewer
	// The optional Gemini critic adds a Vertex round-trip. Keep it on the
	// non-streaming path, but skip it while streaming (compose is
	// grounded-by-construction and the deterministic heuristic reviewer below
	// still runs) so the first answer frame is not blocked on it.
	if a.cfg.ReviewEnabled && !opts.skipModelCritic {
		rvw.critic = a.critic
	}
	verdict := rvw.reviewSections(ctx, body, sections, results, len(plan.SubQuestions))
	// Honor review.go's Complete verdict (previously ignored here — P1-3): an
	// answer that is grounded/scope-safe but INCOMPLETE (some sub-questions
	// never resolved) must not just fall straight to the generic
	// strictRecompose re-render of the same (still-incomplete) results. Give
	// the still-failed/empty results one more real fallback-tier retry first.
	if !verdict.Grounded || !verdict.ScopeSafe || !verdict.Complete {
		if a.telemetry != nil {
			a.telemetry.ReviewCorrection(ctx)
		}
		if a.retryFailedResults(ctx, q.Actor, plan.SubQuestions, results) {
			body, citations, sections, composeErr = comp.composeFor(q.Actor, results)
			if composeErr != nil {
				return a.tenantGateRefusal(ctx, q, requestID, start, "compose_retry", composeErr, results, traces), nil
			}
			for i := range citations {
				citations[i].PlannedByModel = planned
			}
			verdict = rvw.reviewSections(ctx, body, sections, results, len(plan.SubQuestions))
		}
	}
	if !verdict.Grounded || !verdict.ScopeSafe || !verdict.Complete {
		body, sections = a.strictRecomposeSections(results)
		// The strict recompose is grounded-by-CONSTRUCTION: every line is emitted
		// verbatim from a Fact (label/scope/value), so the deterministic heuristic
		// reviewer is authoritative here. Re-running the fuzzy Gemini critic on it is
		// what turned a legitimately grounded per-operator figure into "couldn't
		// verify" — the critic flagged incidental non-numeric prose (the draft-metric
		// disclaimer, a source label) as an "unsupported claim" even though every
		// NUMBER traced to a fact. Validate the fallback with the heuristic reviewer
		// only (no model critic) so a grounded answer can never degrade to the
		// generic refusal.
		var strictRvw reviewer
		verdict = strictRvw.reviewSections(ctx, body, sections, results, len(plan.SubQuestions))
		verdict.Downgraded = true
		if !verdict.Grounded || !verdict.ScopeSafe {
			body = "I could retrieve the underlying records but couldn't fully verify a figure for this answer. Please refine the question or check the source screens."
		}
	}

	if len(fitIssues) > 0 {
		body = strings.TrimSpace(body + "\n\n" + fitNote(fitIssues))
		if mode == domain.ModePlanned {
			mode = domain.ModePartial
		}
		for _, is := range fitIssues {
			verdict.FailReasons = append(verdict.FailReasons, "answer_fit:"+is.Kind+":"+is.Detail)
		}
	}
	verdict.FailReasons = append(verdict.FailReasons, fitAudit...)

	if a.moderator != nil {
		if ok, refusal := a.moderator.CheckOutbound(ctx, body); !ok {
			return a.refusal(requestID, q.ConversationID, refusal), nil
		}
	}

	source := sourceLabel(results)
	if source == "" {
		source = "Mesha read models"
	}

	convoID := q.ConversationID
	if a.convo != nil {
		if id, _, cerr := a.convo.EnsureConversation(ctx, q.Actor, convoID, q.Text); cerr == nil {
			convoID = id
			_ = a.convo.AppendTurn(ctx, q.Actor, convoID, domain.Turn{Role: "user", Content: q.Text, CreatedAt: start})
			_ = a.convo.AppendTurn(ctx, q.Actor, convoID, domain.Turn{Role: "assistant", Content: body, Source: source, Mode: mode, RequestID: requestID, CreatedAt: a.now()})
		}
	}
	if a.memory != nil && convoID != "" {
		_ = a.memory.Remember(ctx, q.Actor, convoID, resolveEntities(plan.SubQuestions))
	}

	// Chart is additive + optional: built from the SAME real facts that
	// grounded the answer, only when the question is plot-worthy or the
	// result is a dimensioned series. It never alters the answer fields. The
	// tenant gate already passed above; buildChart re-checks and a failure
	// here is a refusal, never a chart of someone else's rows.
	chart, chartErr := buildChart(q.Actor, q.Text, results)
	if chartErr != nil {
		return a.tenantGateRefusal(ctx, q, requestID, start, "chart", chartErr, results, traces), nil
	}
	answer := domain.Answer{
		Answer: body, Source: source, Mode: mode, RequestID: requestID,
		ConversationID: convoID, Citations: citations,
		Chart: chart,
	}

	a.recordAudit(ctx, q, requestID, convoID, mode, results, traces, verdict, start)

	if a.budget != nil {
		// Prefer the provider's billed usage (Vertex usageMetadata, plus any
		// repair call); fall back to the len/4 estimate when none was reported
		// (keyword planner, natural-SQL short-circuit, older adapters).
		in, out := len(q.Text)/4, len(body)/4
		if usage.reported() {
			in, out = usage.PromptTokens, usage.OutputTokens
		}
		a.budget.Record(ctx, q.Actor, in, out)
	}
	// Cache gate: the key is tenant-first (cacheKey) AND the cached facts must
	// be wholly the actor's — a mixed/foreign fact set is never stored.
	if a.cache != nil && mode == domain.ModePlanned && verdict.Grounded && validateFactTenants(q.Actor, results) == nil {
		a.cache.Set(cacheKey, answer)
	}
	return answer, nil
}

// planWithFallback is planner-first routing. When the primary provider is a
// real model (PlannedByModel), it plans every question and its plan is used
// as-is (planned=true). The deterministic path — natural-SQL templates, then a
// non-model primary provider, then the keyword fallback — runs ONLY when there
// is no model planner or the model call failed, and always reports
// planned=false, so an answer is labelled "fallback" exactly when the model
// did not plan. It returns the provider's real token usage when reported.
func (a *Assistant) planWithFallback(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec) (domain.Plan, bool, TokenUsage, error) {
	var usage TokenUsage
	var modelErr error
	if a.provider != nil && a.provider.PlannedByModel() {
		plan, u, err := a.planWithModel(ctx, q, mem, catalog)
		usage = usage.add(u)
		if err == nil {
			return plan, true, usage, nil
		}
		// Don't paper a client disconnect / deadline over with the deterministic
		// fallback: surface the context error so the pipeline aborts cleanly.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.Plan{}, false, usage, ctxErr
		}
		a.log.WarnContext(ctx, "ceoai planner failed, using deterministic fallback", "error", err)
		if a.telemetry != nil {
			a.telemetry.VertexFailover(ctx)
		}
		modelErr = err
	}
	plan, err := a.deterministicPlan(ctx, q, mem, catalog)
	if err != nil {
		if modelErr != nil {
			return domain.Plan{}, false, usage, modelErr
		}
		return domain.Plan{}, false, usage, err
	}
	return plan, false, usage, nil
}

// deterministicPlan is the model-free path: a natural-SQL template when one
// matches the topic (and the SQL fallback is wired), else a non-model primary
// provider, else the keyword fallback planner. Its plan is still subject to
// the fallback relevance gate (fallbackFit) before anything runs.
func (a *Assistant) deterministicPlan(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec) (domain.Plan, error) {
	if a.registry != nil && a.registry.HasSQLFallback() {
		if sub, ok := naturalSQLPlan(q, mem); ok {
			return domain.Plan{SubQuestions: []domain.SubQuestion{sub}}, nil
		}
	}
	var firstErr error
	var empty *domain.Plan
	for _, p := range []ports.AIProvider{a.provider, a.fallback} {
		if p == nil || p.PlannedByModel() {
			continue
		}
		plan, err := p.Plan(ctx, q, mem, catalog)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if plan.Refusal != "" || len(plan.SubQuestions) > 0 {
			return plan, nil
		}
		if empty == nil {
			empty = &plan
		}
	}
	if empty != nil {
		return *empty, nil
	}
	if firstErr != nil {
		return domain.Plan{}, firstErr
	}
	return domain.Plan{}, fmt.Errorf("ceoai: no planner available")
}

// planWithModel calls the model planner with bounded transient retries. It
// returns the provider's real token usage when reported (usagePlanner).
func (a *Assistant) planWithModel(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec) (domain.Plan, TokenUsage, error) {
	var plan domain.Plan
	var usage TokenUsage
	up, hasUsage := a.provider.(usagePlanner)
	err := retryTransient(ctx, 2, 150*time.Millisecond, func() error {
		if hasUsage {
			p, u, e := up.PlanWithUsage(ctx, q, mem, catalog)
			usage = usage.add(u) // a retried call is still billed
			if e != nil {
				return e
			}
			plan = p
			return nil
		}
		p, e := a.provider.Plan(ctx, q, mem, catalog)
		if e != nil {
			return e
		}
		plan = p
		return nil
	})
	return plan, usage, err
}

// enforceCubeFirst rewrites any sub-question whose resolved tool name matches a
// governed Cube metric to route=cube. This is the committed guarantee.
func (a *Assistant) enforceCubeFirst(ctx context.Context, subs []domain.SubQuestion) {
	if a.metrics == nil {
		return
	}
	specs, err := a.metrics.Metrics(ctx)
	if err != nil {
		return
	}
	metricByName := map[string]bool{}
	for _, m := range specs {
		metricByName[m.Name] = true
	}
	for i := range subs {
		if metricByName[subs[i].ToolName] && subs[i].Route != domain.RouteCube {
			subs[i].Route = domain.RouteCube
		}
	}
}

// preferActiveCensus rewrites a total_animals sub-question to active_animals for
// a plain headcount/species question. "How many goats vs sheep do we have" is a
// living-herd question; total_animals includes exited/dead animals and is only
// intended when the leader explicitly asks for the all-time total. It preserves
// every other param (group_by species, park filter, …).
func preferActiveCensus(questionText string, subs []domain.SubQuestion) {
	low := strings.ToLower(questionText)
	// Explicit all-time intent keeps total_animals: only then does the leader want
	// exited/dead animals folded into the census.
	for _, kw := range []string{"all-time", "all time", "including dead", "including exited", "ever ", "historical total", "total ever"} {
		if strings.Contains(low, kw) {
			return
		}
	}
	for _, kw := range []string{"sale", "sales", "sold", "selling"} {
		if strings.Contains(low, kw) {
			return
		}
	}
	for i := range subs {
		if subs[i].ToolName == "total_animals" {
			subs[i].ToolName = "active_animals"
		}
	}
}

// overloadIntent matches "who is overloaded / at capacity / over capacity /
// stretched / maxed out" leadership questions.
// No trailing \b: the stems are matched inside longer words ("overloaded",
// "over capacity", "overstretched") where a word-boundary after the stem would
// fail (e.g. "capacit|y").
var overloadIntent = regexp.MustCompile(`(?i)(overload|over.?capacit|at capacity|over.?stretch|stretch|maxed|too many animals|over.?work)`)

// ensureUtilizationForOverload appends an operator utilization sub-question (per
// operator) when the question is an overload/capacity question and no
// utilization sub-question is already planned. This makes the over-capacity
// framing deterministic regardless of which operator metrics the planner picked.
func ensureUtilizationForOverload(questionText string, subs []domain.SubQuestion) []domain.SubQuestion {
	if !overloadIntent.MatchString(questionText) {
		return subs
	}
	for _, s := range subs {
		if s.Route == domain.RouteSQL || s.ToolName == "sql_fallback" {
			return subs
		}
	}
	for _, s := range subs {
		if s.ToolName == "operator_vaccination_utilization" {
			return subs
		}
	}
	return append(subs, domain.SubQuestion{
		ID:          "util",
		Text:        "operator utilization by operator",
		IntentClass: "operator_vaccination_overloaded",
		Route:       domain.RouteCube,
		ToolName:    "operator_vaccination_utilization",
		Params:      map[string]any{"group_by": "operator_label"},
	})
}

var missedVaccinationIntent = regexp.MustCompile(`(?i)(missed|missing|not\s+done|not\s+vaccinated).{0,60}(vaccine|vaccination|vaccinat|shot|dose)|(vaccine|vaccination|vaccinat|shot|dose).{0,60}(missed|missing|not\s+done|not\s+vaccinated)`)

func normalizeVaccinationIntent(questionText string, subs []domain.SubQuestion) {
	low := strings.ToLower(questionText)
	wantsMissed := missedVaccinationIntent.MatchString(questionText)
	wantsOverdue := wantsMissed || strings.Contains(low, "overdue") || strings.Contains(low, "behind") || strings.Contains(low, "late")
	wantsGraph := plotRequested(questionText) || strings.Contains(low, "by shed") || strings.Contains(low, "per shed")
	wantsHowMany := strings.Contains(low, "how many") || strings.Contains(low, "count")
	wantsAllParks := asksAllParks(questionText)
	for i := range subs {
		if subs[i].ToolName != "vaccination_shed_summary" && subs[i].ToolName != "vaccination_due" &&
			subs[i].ToolName != "vaccination_due_today" && subs[i].ToolName != "vaccination_overdue" {
			continue
		}
		if subs[i].Params == nil {
			subs[i].Params = map[string]any{}
		}
		if wantsAllParks {
			delete(subs[i].Params, "park_id")
			delete(subs[i].Params, "shed_id")
			delete(subs[i].Params, "park_label")
			delete(subs[i].Params, "shed_label")
		}
		if wantsMissed {
			subs[i].Params["vaccination_intent"] = "missed"
			subs[i].Params["_fallback_from_tool"] = "vaccination_overdue"
		} else if wantsOverdue {
			subs[i].Params["vaccination_intent"] = "overdue"
			subs[i].Params["_fallback_from_tool"] = "vaccination_overdue"
		}
		if wantsGraph {
			subs[i].Params["group_by"] = "shed_label"
		} else if wantsHowMany && wantsOverdue {
			subs[i].Params["aggregate_total"] = "true"
		}
	}
}

func asksAllParks(questionText string) bool {
	low := strings.ToLower(questionText)
	for _, kw := range []string{"all parks", "across all parks", "company-wide", "company wide", "overall", "whole company", "tenant-wide", "tenant wide"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

func (a *Assistant) strictRecompose(results []domain.ToolResult) string {
	body, _ := a.strictRecomposeSections(results)
	return body
}

// strictRecomposeSections re-renders each result verbatim AND reports which
// result each line came from, so the reviewer can ground a line against its own
// evidence rather than against every result's numbers pooled together.
func (a *Assistant) strictRecomposeSections(results []domain.ToolResult) (string, []answerSection) {
	var lines []string
	var sections []answerSection
	for _, r := range results {
		line := ""
		switch {
		case r.Err != nil:
			line = readFailureLine(r)
		default:
			r = withGroundedFacts(r)
			if len(r.Facts) == 0 {
				line = emptyReadLine(r)
			} else {
				line = renderAnswerBlock(r)
			}
		}
		lines = append(lines, line)
		sections = append(sections, answerSection{text: line, result: r})
	}
	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n\n"), sections
}

func (a *Assistant) recordAudit(ctx context.Context, q domain.Question, requestID, convoID string, mode domain.Mode, results []domain.ToolResult, traces []domain.StepTrace, verdict domain.ReviewVerdict, start time.Time) {
	if a.audit == nil {
		return
	}
	var routes []domain.Route
	var tools []string
	rows := 0
	seen := map[domain.Route]bool{}
	for _, r := range results {
		if !seen[r.Route] {
			seen[r.Route] = true
			routes = append(routes, r.Route)
		}
		tools = append(tools, r.ToolName)
		rows += len(r.Facts)
	}
	err := a.audit.Record(ctx, ports.AuditRecord{
		RequestID: requestID, TenantID: q.Actor.TenantID, ActorID: q.Actor.UserID,
		ConversationID: convoID, QuestionHash: hashQuestion(q.Text), Mode: mode,
		Routes: routes, ToolsCalled: tools, RowCount: rows,
		LatencyMS: a.now().Sub(start).Milliseconds(), Steps: traces, Review: verdict,
		ModelVersion: a.cfg.ModelVersion, PromptVersion: a.cfg.PromptVersion,
	})
	if err != nil {
		// The answer is already composed; a failed audit write must not turn
		// it into an error, but it must never be silent either (PR #318 R2-5):
		// for a tenant-gate refusal this row is the only durable evidence of
		// a D0 violation, so the failure is logged at error level with the
		// request, mode and the verdict's reasons.
		a.log.ErrorContext(ctx, "ceoai audit record failed",
			"error", err, "request_id", requestID, "tenant_id", q.Actor.TenantID,
			"mode", string(mode), "fail_reasons", strings.Join(verdict.FailReasons, "|"))
	}
}

// cacheKey is tenant|user|conversation|day|text. The user component is
// load-bearing: the conversation id is client-supplied and is only proven to
// belong to the caller later (EnsureConversation), so without the user in the
// key a same-tenant colleague could replay another user's conversation id and
// receive their cached follow-up answer before ownership is checked.
func (a *Assistant) cacheKey(q domain.Question) string {
	day := q.AsOf.In(biztime.DefaultLocation()).Format("2006-01-02")
	norm := strings.ToLower(strings.Join(strings.Fields(q.Text), " "))
	conversation := strings.TrimSpace(q.ConversationID)
	if conversation == "" {
		conversation = "no-conversation"
	}
	user := strings.TrimSpace(q.Actor.UserID)
	if user == "" {
		user = "no-user"
	}
	return q.Actor.TenantID + "|" + user + "|" + conversation + "|" + day + "|" + norm
}

func (a *Assistant) plainAnswer(requestID, convoID string, mode domain.Mode, body string) domain.Answer {
	return domain.Answer{Answer: body, Source: "Mesha assistant", Mode: mode, RequestID: requestID, ConversationID: convoID}
}

// tenantGateTelemetry is the optional telemetry capability for the D0 tenant
// gate counter (adapters/observability.Metrics implements it).
type tenantGateTelemetry interface {
	TenantGateReject(ctx context.Context, reason string)
}

// tenantGateReason maps a validateFactTenants error to the bounded reason
// label of ceoai_tenant_gate_reject_total. Never a tenant id or a label.
func tenantGateReason(err error) string {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	switch {
	case strings.Contains(msg, "actor has no tenant"):
		return "no_actor_tenant"
	case strings.Contains(msg, "has no TenantID"):
		return "unstamped"
	case strings.Contains(msg, "another tenant"):
		return "foreign"
	case strings.Contains(msg, "distinct tenant ids"):
		return "mixed"
	default:
		return "other"
	}
}

// tenantGateRefusal is the ONLY way a D0 tenant-gate failure (a fact set
// that is unstamped, foreign or mixed at compose, compose-retry or chart
// time) becomes an answer. It is an invariant violation upstream, so before
// the generic refusal it is (1) logged at error level with the stage and the
// gate's reason, (2) counted on ceoai_tenant_gate_reject_total{reason}, and
// (3) audited as a ModeRefused request whose step trace carries a synthetic
// "tenant_gate" step with the error — so a cross-tenant fact set the gate
// caught is never indistinguishable from an ordinary refusal.
func (a *Assistant) tenantGateRefusal(ctx context.Context, q domain.Question, requestID string, start time.Time, stage string, gateErr error, results []domain.ToolResult, traces []domain.StepTrace) domain.Answer {
	reason := tenantGateReason(gateErr)
	a.log.ErrorContext(ctx, "ceoai tenant gate rejected fact set",
		"stage", stage, "reason", reason, "request_id", requestID, "tenant_id", q.Actor.TenantID, "error", gateErr)
	if tg, ok := a.telemetry.(tenantGateTelemetry); ok {
		tg.TenantGateReject(ctx, reason)
	}
	audited := append(append([]domain.StepTrace(nil), traces...), domain.StepTrace{
		SubQuestionID: "tenant_gate", ToolName: "tenant_gate:" + stage, StartedAt: a.now(),
		Err: gateErr.Error(),
	})
	a.recordAudit(ctx, q, requestID, q.ConversationID, domain.ModeRefused, results, audited,
		domain.ReviewVerdict{ScopeSafe: false, FailReasons: []string{"tenant_gate:" + stage + ":" + reason}}, start)
	return a.refusal(requestID, q.ConversationID, "I couldn't verify that every figure belongs to your organisation, so I won't show this answer.")
}

func (a *Assistant) refusal(requestID, convoID, body string) domain.Answer {
	if strings.TrimSpace(body) == "" {
		body = "That request is outside what this assistant can do. It answers read-only questions about your Mesha operations."
	}
	return a.plainAnswer(requestID, convoID, domain.ModeRefused, body)
}

func resolveEntities(subs []domain.SubQuestion) domain.ResolvedEntities {
	var ent domain.ResolvedEntities
	for _, s := range subs {
		if v, ok := s.Params["park_label"].(string); ok && v != "" {
			ent.ParkLabel = v
		}
		if v, ok := s.Params["shed_label"].(string); ok && v != "" {
			ent.ShedLabel = v
		}
		ent.Metric = s.ToolName
		ent.IntentClass = s.IntentClass
	}
	return ent
}

// telemetryStatus maps the terminal answer/error to the bounded status enum the
// assistant_requests_total metric is labelled by (ok|rejected|error|degraded).
func telemetryStatus(ans domain.Answer, err error) string {
	if err != nil {
		return "error"
	}
	switch ans.Mode {
	case domain.ModeRefused:
		return "rejected"
	case domain.ModePartial, domain.ModeFallback:
		return "degraded"
	default:
		return "ok"
	}
}

// queryLabel builds the coarse, user-safe "querying" progress label from the
// planned sub-questions: the primary route + tool (e.g. "Consulting Cube ·
// operator_vaccination_overdue"). It carries NO reasoning or chain-of-thought —
// only the route/tool tag the citation would already surface.
func queryLabel(subs []domain.SubQuestion) string {
	for _, s := range subs {
		if s.ToolName == "" {
			continue
		}
		verb := "Consulting"
		src := string(s.Route)
		switch s.Route {
		case domain.RouteCube:
			src = "Cube"
		case domain.RouteAPI:
			src = "Mesha read model"
		case domain.RouteToolbox:
			src = "Mesha toolbox"
		case domain.RouteSQL:
			src = "read-only SQL"
		}
		if src == "" {
			return verb + " Mesha read models"
		}
		return verb + " " + src + " · " + s.ToolName
	}
	return "Consulting Mesha read models"
}

// primaryTool is the dominant read route grounding the answer, used as the
// bounded tool label. It prefers the first successful result's route and falls
// back to "none" when nothing read.
func primaryTool(results []domain.ToolResult) string {
	for _, r := range results {
		if r.Err == nil && r.Route != "" {
			return string(r.Route)
		}
	}
	if len(results) > 0 && results[0].Route != "" {
		return string(results[0].Route)
	}
	return string(domain.RouteNone)
}

// totalRows sums the grounding facts across every tool result.
func totalRows(results []domain.ToolResult) int {
	n := 0
	for _, r := range results {
		n += len(r.Facts)
	}
	return n
}

func hashQuestion(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func isLeadership(a domain.Actor) bool {
	if a.Role == permissions.RoleCEOInternal {
		return true
	}
	for _, p := range a.Perms {
		if p == permissions.RoleCEOInternal {
			return true
		}
	}
	return false
}

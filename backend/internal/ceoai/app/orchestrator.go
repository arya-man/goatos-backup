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
		c.ModelVersion = "gemini-2.5-flash"
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

	plan, planned, err := a.planWithFallback(ctx, q, mem, catalog)
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
	if plan.Refusal != "" {
		return a.refusal(requestID, q.ConversationID, plan.Refusal), nil
	}

	// CUBE-FIRST enforcement: a sub-question that maps to a governed Cube metric
	// is forced to route=cube regardless of what the planner proposed.
	a.enforceCubeFirst(ctx, plan.SubQuestions)

	// Leadership "how many do we have" means the LIVING herd. Deterministically
	// prefer the active-animal census over the all-time total (which includes
	// exited/dead animals) for a plain headcount/species-split question, unless the
	// user explicitly asked for the all-time total. Without this, a planner that
	// picks total_animals answers "goats 975 / sheep 336" (3 dead included) where
	// leadership means "goats 972 / sheep 336" (active).
	preferActiveCensus(q.Text, plan.SubQuestions)

	// "Who is overloaded / at capacity" is answered by the operator UTILIZATION
	// ratio (assigned ÷ daily capacity). A planner that picks only load/capacity
	// leaves the answer as raw assigned-vs-capacity with no explicit over-capacity
	// framing. Deterministically guarantee the utilization metric is queried (per
	// operator) so the composer can state exactly which operators are OVER capacity
	// and by how much (e.g. "155% of capacity").
	plan.SubQuestions = ensureUtilizationForOverload(q.Text, plan.SubQuestions)

	mode := domain.ModePlanned
	if !planned {
		mode = domain.ModeFallback
	}

	// "querying <route>" frame when tools run — a coarse route label, not reasoning.
	opts.progress.emit("querying", queryLabel(plan.SubQuestions))

	se := newStepExecutor(a.cfg.MaxSteps, a.cfg.WallClock)
	results, traces, truncated := se.run(ctx, q.Actor, plan.SubQuestions, a.registry.Execute)
	// If the client disconnected during tool execution, abort rather than
	// composing/reviewing/persisting an answer for a dead request.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return domain.Answer{}, ctxErr
	}
	if truncated && mode == domain.ModePlanned {
		mode = domain.ModePartial
	}

	// The dominant read tier + rows grounding this answer are known now; label
	// the terminal metric and record the row histogram.
	resolvedTool = primaryTool(results)
	if a.telemetry != nil {
		a.telemetry.RecordToolRows(ctx, resolvedTool, totalRows(results))
	}

	// Optional partial-synthesis frame before the (grounded) compose + review.
	opts.progress.emit("synthesizing", "")

	var comp composer
	body, citations, _ := comp.compose(results)
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
	verdict := rvw.review(ctx, body, results, len(plan.SubQuestions))
	if !verdict.Grounded || !verdict.ScopeSafe {
		if a.telemetry != nil {
			a.telemetry.ReviewCorrection(ctx)
		}
		body = a.strictRecompose(results)
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
		verdict = strictRvw.review(ctx, body, results, len(plan.SubQuestions))
		verdict.Downgraded = true
		if !verdict.Grounded || !verdict.ScopeSafe {
			body = "I could retrieve the underlying records but couldn't fully verify a figure for this answer. Please refine the question or check the source screens."
		}
	}

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

	answer := domain.Answer{
		Answer: body, Source: source, Mode: mode, RequestID: requestID,
		ConversationID: convoID, Citations: citations,
		// Chart is additive + optional: built from the SAME real facts that
		// grounded the answer, only when the question is plot-worthy or the
		// result is a dimensioned series. It never alters the fields above.
		Chart: buildChart(q.Text, results),
	}

	a.recordAudit(ctx, q, requestID, convoID, mode, results, traces, verdict, start)

	if a.budget != nil {
		a.budget.Record(ctx, q.Actor, len(q.Text)/4, len(body)/4)
	}
	if a.cache != nil && mode == domain.ModePlanned && verdict.Grounded {
		a.cache.Set(cacheKey, answer)
	}
	return answer, nil
}

func (a *Assistant) planWithFallback(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec) (domain.Plan, bool, error) {
	var plan domain.Plan
	err := retryTransient(ctx, 2, 150*time.Millisecond, func() error {
		p, e := a.provider.Plan(ctx, q, mem, catalog)
		if e != nil {
			return e
		}
		plan = p
		return nil
	})
	if err == nil {
		return plan, a.provider.PlannedByModel(), nil
	}
	// Don't paper a client disconnect / deadline over with the deterministic
	// fallback: surface the context error so the pipeline aborts cleanly.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return domain.Plan{}, false, ctxErr
	}
	a.log.WarnContext(ctx, "ceoai planner failed, using deterministic fallback", "error", err)
	if a.telemetry != nil {
		a.telemetry.VertexFailover(ctx)
	}
	if a.fallback == nil {
		return domain.Plan{}, false, err
	}
	p, e := a.fallback.Plan(ctx, q, mem, catalog)
	if e != nil {
		return domain.Plan{}, false, e
	}
	return p, false, nil
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

func (a *Assistant) strictRecompose(results []domain.ToolResult) string {
	var lines []string
	for _, r := range results {
		if r.Err != nil {
			lines = append(lines, fmt.Sprintf("%s: could not be retrieved.", surfaceOrRoute(r)))
			continue
		}
		if len(r.Facts) == 0 {
			lines = append(lines, fmt.Sprintf("%s: no records found.", surfaceOrRoute(r)))
			continue
		}
		max := len(r.Facts)
		if max > 10 {
			max = 10
		}
		for _, f := range r.Facts[:max] {
			if f.Scope != "" {
				lines = append(lines, fmt.Sprintf("%s — %s (%s): %s", surfaceOrRoute(r), f.Label, f.Scope, f.Value))
			} else {
				lines = append(lines, fmt.Sprintf("%s — %s: %s", surfaceOrRoute(r), f.Label, f.Value))
			}
		}
	}
	return strings.Join(lines, "\n")
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
	_ = a.audit.Record(ctx, ports.AuditRecord{
		RequestID: requestID, TenantID: q.Actor.TenantID, ActorID: q.Actor.UserID,
		ConversationID: convoID, QuestionHash: hashQuestion(q.Text), Mode: mode,
		Routes: routes, ToolsCalled: tools, RowCount: rows,
		LatencyMS: a.now().Sub(start).Milliseconds(), Steps: traces, Review: verdict,
		ModelVersion: a.cfg.ModelVersion, PromptVersion: a.cfg.PromptVersion,
	})
}

func (a *Assistant) cacheKey(q domain.Question) string {
	day := q.AsOf.In(biztime.DefaultLocation()).Format("2006-01-02")
	norm := strings.ToLower(strings.Join(strings.Fields(q.Text), " "))
	return q.Actor.TenantID + "|" + day + "|" + norm
}

func (a *Assistant) plainAnswer(requestID, convoID string, mode domain.Mode, body string) domain.Answer {
	return domain.Answer{Answer: body, Source: "Mesha assistant", Mode: mode, RequestID: requestID, ConversationID: convoID}
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

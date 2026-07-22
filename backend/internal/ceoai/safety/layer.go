package safety

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Layer is the composed safety/abuse/cost facade the CEO-AI orchestrator calls.
// It sequences the guards in the correct order and returns, at each stage, a
// Verdict the orchestrator must honor. It never mutates tenant/role scope and
// never talks to business data.
//
// Guard order for a request:
//
//  1. Identity validity      (scope must come from the session)
//  2. Rate limit             (per-actor + per-tenant)
//  3. Concurrency/backpressure (bounded in-flight)
//  4. Input moderation        (abuse/unsafe/off-domain)
//  5. Prompt-injection screen  (override attempts in the question)
//  6. Budget pre-check         (per-request + per-actor/tenant day)
//     ... orchestrator runs the model under the breaker ...
//  7. Output moderation        (screen the composed answer)
//  8. Budget record            (persist actual usage)
type Layer struct {
	Injection *InjectionScanner
	Moderator Moderator
	Limiter   *Limiter
	Budgeter  *Budgeter
	Breaker   *CircuitBreaker
	Sem       *Semaphore
	Retention *RetentionCleaner
	log       *slog.Logger
}

// Config is the full safety-layer configuration.
type Config struct {
	Limiter   LimiterConfig
	Budget    BudgetConfig
	Breaker   BreakerConfig
	Semaphore SemaphoreConfig
	Retention RetentionConfig
	// InjectionExtraPatterns are optional additional regexes.
	InjectionExtraPatterns []string
}

// DefaultConfig returns the baseline safety configuration.
func DefaultConfig() Config {
	return Config{
		Limiter:   DefaultLimiterConfig(),
		Budget:    DefaultBudgetConfig(),
		Breaker:   DefaultBreakerConfig(),
		Semaphore: DefaultSemaphoreConfig(),
		Retention: DefaultRetentionConfig(),
	}
}

// NewLayer builds the composed safety layer. store (usage) and purger
// (retention) may be nil for local/CI (in-memory usage, no retention loop).
func NewLayer(cfg Config, store UsageStore, purger ConversationPurger, clock Clock, log *slog.Logger) (*Layer, error) {
	var retention *RetentionCleaner
	if purger != nil {
		retention = NewRetentionCleaner(purger, cfg.Retention, clock, log)
	}
	sem, err := NewSemaphore(cfg.Semaphore)
	if err != nil {
		return nil, fmt.Errorf("safety layer: %w", err)
	}
	return &Layer{
		Injection: NewInjectionScanner(cfg.InjectionExtraPatterns...),
		Moderator: ChainModerators(NewHeuristicModerator()),
		Limiter:   NewLimiter(cfg.Limiter),
		Budgeter:  NewBudgeter(cfg.Budget, store, clock),
		Breaker:   NewCircuitBreaker("vertex", cfg.Breaker, clock),
		Sem:       sem,
		Retention: retention,
		log:       log,
	}, nil
}

// AdmitResult carries the outcome of the pre-model admission gate plus a release
// func for the acquired concurrency slot. When Verdict is not Allowed, Release is
// a no-op and the orchestrator must return the verdict's UserMessage.
type AdmitResult struct {
	Verdict Verdict
	Release func()
}

// Admit runs stages 1-6 (everything before the model call). On an allow verdict
// the caller holds a concurrency slot and MUST call Release when done. On any
// non-allow verdict the slot is already released.
//
// estimate is the projected token usage for the request (from the planner or a
// heuristic on the question length); it feeds the budget pre-check.
func (l *Layer) Admit(ctx context.Context, id Identity, question string, estimate Usage) AdmitResult {
	noop := func() {}
	if !id.Valid() {
		return AdmitResult{Verdict: Verdict{
			Decision:    DecisionRefuse,
			Reason:      "identity:invalid",
			UserMessage: "Your session could not be verified. Please sign in again.",
		}, Release: noop}
	}

	// 2. Rate limit.
	if v := l.Limiter.Check(id); !v.Allowed() {
		l.logDeny(id, v)
		return AdmitResult{Verdict: v, Release: noop}
	}

	// 3. Concurrency / backpressure.
	release, err := l.Sem.Acquire(ctx)
	if err != nil {
		v := OverloadedVerdict()
		if err == context.Canceled || err == context.DeadlineExceeded {
			v = Verdict{Decision: DecisionThrottle, Reason: "backpressure:ctx_" + err.Error(), RetryAfter: time.Second, UserMessage: OverloadedVerdict().UserMessage}
		}
		l.logDeny(id, v)
		return AdmitResult{Verdict: v, Release: noop}
	}

	// From here, any non-allow path must release the slot.
	fail := func(v Verdict) AdmitResult {
		release()
		l.logDeny(id, v)
		return AdmitResult{Verdict: v, Release: noop}
	}

	// 4. Input moderation.
	if v := l.Moderator.Moderate(ctx, StageInput, question); !v.Allowed() {
		return fail(v)
	}

	// 5. Prompt-injection screen of the question.
	if v := l.Injection.ScreenQuestion(question); !v.Allowed() {
		return fail(v)
	}

	// 6. Budget pre-check.
	v, berr := l.Budgeter.CheckRequest(ctx, id, estimate)
	if berr != nil {
		// Never swallow: an accounting error degrades honestly rather than
		// silently allowing unbounded spend.
		return fail(Verdict{Decision: DecisionDegrade, Reason: "budget:store_error", UserMessage: "The assistant is temporarily unavailable. Please try again shortly."})
	}
	if !v.Allowed() {
		return fail(v)
	}

	return AdmitResult{Verdict: allow(), Release: release}
}

// GuardModelCall runs the model invocation under the circuit breaker. On an open
// breaker it returns a DecisionDegrade verdict without calling fn.
func (l *Layer) GuardModelCall(ctx context.Context, fn func(context.Context) error) (Verdict, error) {
	err := l.Breaker.Execute(ctx, fn)
	if err == ErrBreakerOpen {
		return DegradeVerdict("vertex"), nil
	}
	if err != nil {
		return Verdict{}, err
	}
	return allow(), nil
}

// ScreenAnswer runs output moderation on the composed answer (stage 7). A blocked
// answer must be replaced by the returned UserMessage, never surfaced.
func (l *Layer) ScreenAnswer(ctx context.Context, answer string) Verdict {
	return l.Moderator.Moderate(ctx, StageOutput, answer)
}

// RecordUsage persists actual usage (stage 8). The returned verdict reflects the
// post-record budget state for the NEXT request; it does not fail the current
// answer.
func (l *Layer) RecordUsage(ctx context.Context, id Identity, actual Usage) (Verdict, error) {
	return l.Budgeter.Record(ctx, id, actual)
}

func (l *Layer) logDeny(id Identity, v Verdict) {
	if l.log == nil {
		return
	}
	// Log once at the boundary. Tenant/actor are operational scope; no question
	// text, no secrets.
	l.log.Info("ceoai.safety.deny",
		slog.String("tenant_id", id.TenantID),
		slog.String("actor_id", id.ActorID),
		slog.String("decision", v.Decision.String()),
		slog.String("reason", v.Reason),
	)
}

// --- env-driven config ---

// ConfigFromEnv builds a Config from MESHA_AI_* environment variables, falling
// back to DefaultConfig values for anything unset or invalid (validate-or-reject:
// an invalid value is ignored with the default kept, never silently applied as
// zero). Recognized:
//
//	MESHA_AI_RATE_PER_ACTOR_MIN       int   (requests/actor/minute)
//	MESHA_AI_RATE_PER_TENANT_MIN      int   (requests/tenant/minute)
//	MESHA_AI_MAX_TOKENS_PER_REQUEST   int
//	MESHA_AI_MAX_TOKENS_ACTOR_DAY     int
//	MESHA_AI_MAX_TOKENS_TENANT_DAY    int
//	MESHA_AI_MAX_COST_ACTOR_DAY_UUSD  int64 (micro-USD)
//	MESHA_AI_MAX_COST_TENANT_DAY_UUSD int64
//	MESHA_AI_MAX_CONCURRENT           int
//	MESHA_AI_BREAKER_FAILS            int
//	MESHA_AI_RETENTION_DAYS           int   (informational; used by write path)
func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if v, ok := envPositiveInt("MESHA_AI_RATE_PER_ACTOR_MIN"); ok {
		cfg.Limiter.PerActorPerWindow = v
	}
	if v, ok := envPositiveInt("MESHA_AI_RATE_PER_TENANT_MIN"); ok {
		cfg.Limiter.PerTenantPerWindow = v
	}
	if v, ok := envPositiveInt("MESHA_AI_MAX_TOKENS_PER_REQUEST"); ok {
		cfg.Budget.MaxTokensPerRequest = v
	}
	if v, ok := envPositiveInt("MESHA_AI_MAX_TOKENS_ACTOR_DAY"); ok {
		cfg.Budget.MaxTokensPerActorDay = v
	}
	if v, ok := envPositiveInt("MESHA_AI_MAX_TOKENS_TENANT_DAY"); ok {
		cfg.Budget.MaxTokensPerTenantDay = v
	}
	if v, ok := envPositiveInt64("MESHA_AI_MAX_COST_ACTOR_DAY_UUSD"); ok {
		cfg.Budget.MaxCostMicroUSDPerActorDay = v
	}
	if v, ok := envPositiveInt64("MESHA_AI_MAX_COST_TENANT_DAY_UUSD"); ok {
		cfg.Budget.MaxCostMicroUSDPerTenantDay = v
	}
	if v, ok := envPositiveInt("MESHA_AI_MAX_CONCURRENT"); ok {
		cfg.Semaphore.MaxConcurrent = v
	}
	if v, ok := envPositiveInt("MESHA_AI_BREAKER_FAILS"); ok {
		cfg.Breaker.FailureThreshold = v
	}
	return cfg
}

// RetentionDaysFromEnv returns the configured retention window in days, or the
// provided default when unset/invalid. The write path stamps
// retention_expires_at using this; the cleaner enforces it.
func RetentionDaysFromEnv(def int) int {
	if v, ok := envPositiveInt("MESHA_AI_RETENTION_DAYS"); ok {
		return v
	}
	return def
}

func envPositiveInt(key string) (int, bool) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func envPositiveInt64(key string) (int64, bool) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

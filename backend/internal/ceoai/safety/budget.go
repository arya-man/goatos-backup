package safety

import (
	"context"
	"sync"
	"time"
)

// Usage is a single accounted LLM interaction: tokens consumed and the derived
// estimated cost in micro-USD (integer, to avoid float drift in accounting).
type Usage struct {
	InputTokens  int
	OutputTokens int
	CostMicroUSD int64
}

// TotalTokens returns input+output.
func (u Usage) TotalTokens() int { return u.InputTokens + u.OutputTokens }

// UsageStore persists per-actor/per-tenant daily usage so budgets survive
// process restarts. The default InMemoryUsageStore is used for local/CI; a
// Postgres-backed store (ceo_ai_usage table) implements the same interface in
// production. day is an IST business-day key ("2006-01-02").
type UsageStore interface {
	// Add atomically increments and returns the new running totals for the
	// (tenant, actor, day) tuple.
	Add(ctx context.Context, tenantID, actorID, day string, delta Usage) (actorDay Usage, tenantDay Usage, err error)
	// Get returns current running totals without incrementing.
	Get(ctx context.Context, tenantID, actorID, day string) (actorDay Usage, tenantDay Usage, err error)
}

// BudgetConfig defines the caps. A zero value in any field means "no cap on that
// dimension".
type BudgetConfig struct {
	// MaxTokensPerRequest bounds a single request's projected input+output
	// tokens (enforced before the model call using an estimate, and after using
	// actuals).
	MaxTokensPerRequest int
	// MaxTokensPerActorDay caps total tokens one actor may spend per IST day.
	MaxTokensPerActorDay int
	// MaxTokensPerTenantDay caps total tokens one tenant may spend per IST day.
	MaxTokensPerTenantDay int
	// MaxCostMicroUSDPerActorDay caps estimated spend per actor per IST day.
	MaxCostMicroUSDPerActorDay int64
	// MaxCostMicroUSDPerTenantDay caps estimated spend per tenant per IST day.
	MaxCostMicroUSDPerTenantDay int64
}

// DefaultBudgetConfig returns leadership-assistant defaults. These are
// intentionally generous per request but bounded per day. Values are chosen for
// gemini-3.5-flash-lite-class pricing and are overridable from MESHA_AI_* env.
func DefaultBudgetConfig() BudgetConfig {
	return BudgetConfig{
		MaxTokensPerRequest:         32_000,
		MaxTokensPerActorDay:        500_000,
		MaxTokensPerTenantDay:       3_000_000,
		MaxCostMicroUSDPerActorDay:  2_000_000,  // $2.00 / actor / day
		MaxCostMicroUSDPerTenantDay: 20_000_000, // $20.00 / tenant / day
	}
}

// Budgeter enforces token/cost caps and records usage. It is safe for
// concurrent use (the store must be too).
type Budgeter struct {
	cfg   BudgetConfig
	store UsageStore
	clock Clock
	// istOffset is the fixed Asia/Kolkata offset used to bucket days without a
	// tz database dependency.
	istOffset time.Duration
}

// NewBudgeter builds a Budgeter. If store is nil, an in-memory store is used.
func NewBudgeter(cfg BudgetConfig, store UsageStore, clock Clock) *Budgeter {
	if store == nil {
		store = NewInMemoryUsageStore()
	}
	return &Budgeter{
		cfg:       cfg,
		store:     store,
		clock:     clock,
		istOffset: 5*time.Hour + 30*time.Minute,
	}
}

// day returns the current IST business-day key.
func (b *Budgeter) day() string {
	return b.clock.now().UTC().Add(b.istOffset).Format("2006-01-02")
}

// CheckRequest is called BEFORE the model runs, with an estimate of the tokens
// the request will consume. It rejects when the single-request cap is exceeded
// or when the actor/tenant daily caps are already met.
func (b *Budgeter) CheckRequest(ctx context.Context, id Identity, estimate Usage) (Verdict, error) {
	if b == nil {
		return allow(), nil
	}
	if b.cfg.MaxTokensPerRequest > 0 && estimate.TotalTokens() > b.cfg.MaxTokensPerRequest {
		return b.overBudget("budget:request_tokens"), nil
	}
	actorDay, tenantDay, err := b.store.Get(ctx, id.TenantID, id.ActorID, b.day())
	if err != nil {
		return Verdict{}, err
	}
	if v, blocked := b.checkCaps(actorDay, tenantDay); blocked {
		return v, nil
	}
	// Also block if adding the estimate would clearly exceed a daily cap.
	if v, blocked := b.checkCaps(addUsage(actorDay, estimate), addUsage(tenantDay, estimate)); blocked {
		return v, nil
	}
	return allow(), nil
}

// Record is called AFTER the model runs with actual usage. It persists the spend
// and returns the post-record verdict (so a request that pushed the actor over
// the cap is reflected for the next call). Recording never fails the current
// answer; the caller uses the returned verdict only for the NEXT decision.
func (b *Budgeter) Record(ctx context.Context, id Identity, actual Usage) (Verdict, error) {
	if b == nil {
		return allow(), nil
	}
	actorDay, tenantDay, err := b.store.Add(ctx, id.TenantID, id.ActorID, b.day(), actual)
	if err != nil {
		return Verdict{}, err
	}
	if v, blocked := b.checkCaps(actorDay, tenantDay); blocked {
		return v, nil
	}
	return allow(), nil
}

func (b *Budgeter) checkCaps(actorDay, tenantDay Usage) (Verdict, bool) {
	c := b.cfg
	if c.MaxTokensPerActorDay > 0 && actorDay.TotalTokens() >= c.MaxTokensPerActorDay {
		return b.overBudget("budget:actor_daily_tokens"), true
	}
	if c.MaxTokensPerTenantDay > 0 && tenantDay.TotalTokens() >= c.MaxTokensPerTenantDay {
		return b.overBudget("budget:tenant_daily_tokens"), true
	}
	if c.MaxCostMicroUSDPerActorDay > 0 && actorDay.CostMicroUSD >= c.MaxCostMicroUSDPerActorDay {
		return b.overBudget("budget:actor_daily_cost"), true
	}
	if c.MaxCostMicroUSDPerTenantDay > 0 && tenantDay.CostMicroUSD >= c.MaxCostMicroUSDPerTenantDay {
		return b.overBudget("budget:tenant_daily_cost"), true
	}
	return Verdict{}, false
}

func (b *Budgeter) overBudget(reason string) Verdict {
	return Verdict{
		Decision:    DecisionThrottle,
		Reason:      reason,
		RetryAfter:  b.untilMidnightIST(),
		UserMessage: "The daily usage limit for the assistant has been reached for your account. Please try again tomorrow or contact your administrator.",
	}
}

func (b *Budgeter) untilMidnightIST() time.Duration {
	nowIST := b.clock.now().UTC().Add(b.istOffset)
	next := time.Date(nowIST.Year(), nowIST.Month(), nowIST.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
	d := next.Sub(nowIST)
	if d < 0 {
		return time.Hour
	}
	return d
}

func addUsage(a, b Usage) Usage {
	return Usage{
		InputTokens:  a.InputTokens + b.InputTokens,
		OutputTokens: a.OutputTokens + b.OutputTokens,
		CostMicroUSD: a.CostMicroUSD + b.CostMicroUSD,
	}
}

// --- in-memory usage store (default) ---

// InMemoryUsageStore is a process-local UsageStore for local dev and tests. It
// buckets per (tenant, actor, day) and per (tenant, day).
type InMemoryUsageStore struct {
	mu     sync.Mutex
	actor  map[string]Usage // key tenant|actor|day
	tenant map[string]Usage // key tenant|day
}

// NewInMemoryUsageStore constructs an empty store.
func NewInMemoryUsageStore() *InMemoryUsageStore {
	return &InMemoryUsageStore{
		actor:  map[string]Usage{},
		tenant: map[string]Usage{},
	}
}

func (s *InMemoryUsageStore) Add(_ context.Context, tenantID, actorID, day string, delta Usage) (Usage, Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ak := tenantID + "|" + actorID + "|" + day
	tk := tenantID + "|" + day
	s.actor[ak] = addUsage(s.actor[ak], delta)
	s.tenant[tk] = addUsage(s.tenant[tk], delta)
	return s.actor[ak], s.tenant[tk], nil
}

func (s *InMemoryUsageStore) Get(_ context.Context, tenantID, actorID, day string) (Usage, Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actor[tenantID+"|"+actorID+"|"+day], s.tenant[tenantID+"|"+day], nil
}

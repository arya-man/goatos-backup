package safety

import (
	"context"
	"testing"
	"time"
)

func fixedClock(ts time.Time) Clock { return func() time.Time { return ts } }

func TestBudgetRequestTokenCap(t *testing.T) {
	b := NewBudgeter(BudgetConfig{MaxTokensPerRequest: 1000}, nil, fixedClock(time.Now()))
	id := Identity{TenantID: "t1", ActorID: "a1"}
	v, err := b.CheckRequest(context.Background(), id, Usage{InputTokens: 800, OutputTokens: 500})
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision != DecisionThrottle || v.Reason != "budget:request_tokens" {
		t.Fatalf("expected per-request token block, got %s/%s", v.Decision, v.Reason)
	}
}

func TestBudgetActorDailyTokenTrip(t *testing.T) {
	ctx := context.Background()
	clk := fixedClock(time.Date(2026, 7, 22, 6, 0, 0, 0, time.UTC))
	b := NewBudgeter(BudgetConfig{MaxTokensPerActorDay: 1000}, nil, clk)
	id := Identity{TenantID: "t1", ActorID: "a1"}

	// Record usage up to the cap.
	if _, err := b.Record(ctx, id, Usage{InputTokens: 600, OutputTokens: 400}); err != nil {
		t.Fatal(err)
	}
	// Next request should be blocked because the daily cap is met.
	v, err := b.CheckRequest(ctx, id, Usage{InputTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision != DecisionThrottle || v.Reason != "budget:actor_daily_tokens" {
		t.Fatalf("expected actor daily token trip, got %s/%s", v.Decision, v.Reason)
	}
}

func TestBudgetTenantDailyCostTrip(t *testing.T) {
	ctx := context.Background()
	clk := fixedClock(time.Date(2026, 7, 22, 6, 0, 0, 0, time.UTC))
	b := NewBudgeter(BudgetConfig{MaxCostMicroUSDPerTenantDay: 5000}, nil, clk)
	a1 := Identity{TenantID: "t1", ActorID: "a1"}
	a2 := Identity{TenantID: "t1", ActorID: "a2"}

	if _, err := b.Record(ctx, a1, Usage{CostMicroUSD: 3000}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Record(ctx, a2, Usage{CostMicroUSD: 2500}); err != nil {
		t.Fatal(err)
	}
	// a1 now blocked because the shared tenant cost cap is exceeded.
	v, err := b.CheckRequest(ctx, a1, Usage{})
	if err != nil {
		t.Fatal(err)
	}
	if v.Reason != "budget:tenant_daily_cost" {
		t.Fatalf("expected tenant daily cost trip, got %s", v.Reason)
	}
}

func TestBudgetEstimateWouldExceed(t *testing.T) {
	ctx := context.Background()
	clk := fixedClock(time.Date(2026, 7, 22, 6, 0, 0, 0, time.UTC))
	b := NewBudgeter(BudgetConfig{MaxTokensPerActorDay: 1000}, nil, clk)
	id := Identity{TenantID: "t1", ActorID: "a1"}
	// Already spent 900; an estimate of 200 would exceed 1000.
	if _, err := b.Record(ctx, id, Usage{InputTokens: 900}); err != nil {
		t.Fatal(err)
	}
	v, err := b.CheckRequest(ctx, id, Usage{InputTokens: 200})
	if err != nil {
		t.Fatal(err)
	}
	if v.Allowed() {
		t.Fatal("estimate pushing over the daily cap should be blocked")
	}
}

func TestBudgetDayRollover(t *testing.T) {
	ctx := context.Background()
	day1 := time.Date(2026, 7, 22, 6, 0, 0, 0, time.UTC) // IST 11:30
	var now time.Time
	b := NewBudgeter(BudgetConfig{MaxTokensPerActorDay: 1000}, nil, func() time.Time { return now })
	id := Identity{TenantID: "t1", ActorID: "a1"}

	now = day1
	if _, err := b.Record(ctx, id, Usage{InputTokens: 1000}); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.CheckRequest(ctx, id, Usage{}); v.Allowed() {
		t.Fatal("should be blocked on day1")
	}
	// Move to next IST day.
	now = day1.Add(24 * time.Hour)
	if v, _ := b.CheckRequest(ctx, id, Usage{}); !v.Allowed() {
		t.Fatal("new day should reset the actor daily budget")
	}
}

func TestBudgetRecordPropagatesStoreError(t *testing.T) {
	b := NewBudgeter(DefaultBudgetConfig(), errStore{}, fixedClock(time.Now()))
	if _, err := b.Record(context.Background(), Identity{TenantID: "t", ActorID: "a"}, Usage{}); err == nil {
		t.Fatal("store error must not be swallowed")
	}
	if _, err := b.CheckRequest(context.Background(), Identity{TenantID: "t", ActorID: "a"}, Usage{}); err == nil {
		t.Fatal("store error on check must not be swallowed")
	}
}

type errStore struct{}

func (errStore) Add(context.Context, string, string, string, Usage) (Usage, Usage, error) {
	return Usage{}, Usage{}, context.DeadlineExceeded
}
func (errStore) Get(context.Context, string, string, string) (Usage, Usage, error) {
	return Usage{}, Usage{}, context.DeadlineExceeded
}

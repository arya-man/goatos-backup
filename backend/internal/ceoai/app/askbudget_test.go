package app

import (
	"context"
	"testing"
	"time"
)

// MESHA_AI_MAX_STEPS and the wall clock are meant to bound what ONE QUESTION
// may cost. They did not: a fresh executor was built per executePlan and the
// orchestrator calls it twice (the plan, then the fit re-plan), so an ask could
// spend twice the steps over twice the clock. One budget, shared.
func TestThePlanAndTheReplanSpendOneAllowance(t *testing.T) {
	budget := newAskBudget(3, time.Minute, nil)

	spent := 0
	for i := 0; i < 2; i++ {
		se := newStepExecutor(budget)
		for se.budget.take() {
			spent++
			if spent > 10 {
				t.Fatal("the budget never ran out: the two executors are not sharing it")
			}
		}
	}
	if spent != 3 {
		t.Errorf("two executors spent %d steps against an allowance of 3", spent)
	}
}

// The repair, filter-value probe and tier retry are model and database round
// trips that used to run on the raw request context, counted by nothing. An
// ask with nothing left skips them rather than starting work whose answer would
// arrive after the reply.
func TestAnExhaustedAskRefusesFurtherRoundTrips(t *testing.T) {
	budget := newAskBudget(1, time.Minute, nil)
	if !budget.take() {
		t.Fatal("the first step of an ask must be allowed")
	}
	if budget.take() {
		t.Error("a repair or probe was allowed past the ask's whole allowance")
	}
	if budget.refused != 1 {
		t.Errorf("refused = %d, want the skipped round trip counted", budget.refused)
	}
}

// The wall clock bounds the ask, not each call: a repair starting with two
// seconds left may take at most those two seconds, and one starting past the
// deadline is handed a cancelled context so the caller's own ctx.Err() check
// short-circuits exactly as it does on a timeout.
func TestWhatIsLeftOfTheAskBoundsEachRoundTrip(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	budget := newAskBudget(6, 10*time.Second, clock)

	ctx, cancel := budget.withDeadline(context.Background())
	deadline, ok := ctx.Deadline()
	cancel()
	// context.WithTimeout anchors on the real clock, so allow a moment of slack
	// over the pinned one; what matters is that it cannot outlive the ask.
	if !ok || deadline.After(now.Add(10*time.Second+time.Second)) {
		t.Errorf("round trip deadline %v outlives the ask", deadline)
	}

	now = now.Add(11 * time.Second)
	expired, cancelExpired := budget.withDeadline(context.Background())
	cancelExpired()
	if expired.Err() == nil {
		t.Error("a round trip started past the ask's wall clock was not cancelled")
	}
	if budget.take() {
		t.Error("a step was allowed after the ask ran out of wall clock")
	}
}

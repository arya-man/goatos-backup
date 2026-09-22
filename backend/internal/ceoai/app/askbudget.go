package app

import (
	"context"
	"sync"
	"time"
)

// askBudget is ONE step and wall-clock allowance for a whole ask.
//
// MESHA_AI_MAX_STEPS and the wall clock are a bound on what a single question
// may cost. They stopped being that: a fresh executor was built per executePlan
// and the orchestrator calls it twice (the plan and the re-plan), so the ask
// could spend 2x the steps over 2x the wall clock — and the repair, probe and
// retry round-trips ran outside the executor entirely, on the raw request
// context, counted by nothing. Every one of those paths now draws on this one
// budget, so the bound describes the ASK rather than one loop inside it.
//
// It is safe for concurrent use: the step executor now runs a plan's
// independent sub-questions at the same time, so several goroutines draw on
// this one allowance. The mutex is what keeps MaxSteps a real bound rather
// than an approximate one under fan-out.
type askBudget struct {
	mu        sync.Mutex
	remaining int
	deadline  time.Time
	now       func() time.Time
	// spentOutsideSteps counts the extra model/database round trips (repair,
	// filter-value probe, tier retry) that the budget refused, for the audit.
	refused int
}

func newAskBudget(maxSteps int, wallClock time.Duration, now func() time.Time) *askBudget {
	if maxSteps <= 0 {
		maxSteps = 6
	}
	if wallClock <= 0 {
		wallClock = 25 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &askBudget{remaining: maxSteps, deadline: now().Add(wallClock), now: now}
}

// take consumes one unit. It reports false when the ask has spent its steps or
// run past its wall clock — the caller then skips that round trip rather than
// starting work whose result would arrive after the answer.
func (b *askBudget) take() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.remaining <= 0 || b.expiredLocked() {
		b.refused++
		return false
	}
	b.remaining--
	return true
}

// expired reports that the ask's wall clock has run out.
func (b *askBudget) expired() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.expiredLocked()
}

// expiredLocked is expired() for a caller that already holds the mutex.
func (b *askBudget) expiredLocked() bool {
	return b.now().After(b.deadline)
}

// withDeadline bounds a call by whatever is LEFT of the ask's wall clock, so a
// repair or probe cannot outlive the answer it was meant to improve.
func (b *askBudget) withDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if b == nil {
		return ctx, func() {}
	}
	b.mu.Lock()
	d := b.deadline.Sub(b.now())
	b.mu.Unlock()
	if d <= 0 {
		// Already past: hand back a cancelled context so the caller's own
		// ctx.Err() check short-circuits exactly as it does on a timeout.
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		return cctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

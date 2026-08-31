package postgres

import (
	"context"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"
)

// TestCommandBoardConcurrencyBudgetIsSharedAcrossSections pins the invariant that the board and its
// lazy sections draw from ONE budget.
//
// Found by review. Splitting the board into three endpoints made admin-web fire them in PARALLEL on
// first paint, and each kept its own independent limit: 6 for the board, 3 for the cohort matrix, 1
// for the shed grid. That is TEN connections for a single reader against a GOATOS_PG_MAX_CONNS that
// defaults to TEN -- the endpoint was made fast and then handed the pool a way to starve, which is
// the original failure (a timeout) moved from the statement to the pool.
//
// This test would have failed against per-endpoint limits, because three separate semaphores admit
// commandBoardConcurrencyBudget each.
func TestCommandBoardConcurrencyBudgetIsSharedAcrossSections(t *testing.T) {
	r := &Repository{commandBoardSlots: semaphore.NewWeighted(commandBoardConcurrencyBudget)}

	held := make(chan struct{})
	release := make(chan struct{})
	ctx := context.Background()

	// Fill the whole budget with sections that do not return.
	for i := 0; i < commandBoardConcurrencyBudget; i++ {
		go func() {
			_ = r.commandBoardSection(ctx, func() error {
				held <- struct{}{}
				<-release
				return nil
			})()
		}()
	}
	for i := 0; i < commandBoardConcurrencyBudget; i++ {
		select {
		case <-held:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d sections acquired a slot", i, commandBoardConcurrencyBudget)
		}
	}

	// One more section -- as a lazy section on another endpoint would be -- must WAIT, not proceed.
	blocked := make(chan error, 1)
	go func() {
		blocked <- r.commandBoardSection(ctx, func() error { return nil })()
	}()
	select {
	case <-blocked:
		t.Fatal("a section ran while the shared budget was fully held; the budget is not shared, " +
			"so one reader's board plus its lazy sections can claim more connections than the pool has")
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("queued section returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued section never ran after the budget was released")
	}
}

// TestCommandBoardSectionStopsWaitingOnCancel: a cancelled request must stop queueing behind the
// budget rather than holding a goroutine until a slot frees.
func TestCommandBoardSectionStopsWaitingOnCancel(t *testing.T) {
	r := &Repository{commandBoardSlots: semaphore.NewWeighted(1)}
	ctx := context.Background()
	if err := r.commandBoardSlots.Acquire(ctx, 1); err != nil {
		t.Fatalf("seed acquire: %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- r.commandBoardSection(cancelCtx, func() error { return nil })() }()
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled section reported success without ever running")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled section kept waiting for a slot")
	}
}

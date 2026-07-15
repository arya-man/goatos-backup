package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// flakyContinuousStage fails its first failFirst runs, then blocks until ctx is
// done. It models a domain-event consumer that hits a few transient errors and
// then runs healthily for the rest of the process lifetime.
type flakyContinuousStage struct {
	name      string
	failFirst int

	mu       sync.Mutex
	runCount int
}

func (f *flakyContinuousStage) Name() string { return f.name }

func (f *flakyContinuousStage) Run(ctx context.Context) error {
	f.mu.Lock()
	f.runCount++
	n := f.runCount
	f.mu.Unlock()

	if n <= f.failFirst {
		return fmt.Errorf("simulated transient failure %d", n)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *flakyContinuousStage) runs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runCount
}

// TestContinuousStageErrorDoesNotKillKernel proves the kernel-robustness
// contract: a transient error from a continuous stage is isolated — it is
// retried with backoff instead of cancelling the errgroup, so the supervisor
// keeps running and the stage is restarted rather than silently dropped. The
// supervisor exits ONLY on ctx cancellation, and Run returns the context error,
// never the stage's transient error.
func TestContinuousStageErrorDoesNotKillKernel(t *testing.T) {
	t.Parallel()
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewSupervisor(logger, pool, 1*time.Second)

	stage := &flakyContinuousStage{name: "flaky-consumer", failFirst: 2}
	s.RegisterContinuous("flaky-consumer", stage)

	// Bounded run: the healthy stage blocks until this deadline; the two prior
	// failures each cost ~1s + 2s of backoff, so ~4s in, run #3 is healthy.
	runCtx, runCancel := context.WithTimeout(ctx, 6*time.Second)
	defer runCancel()

	err := s.Run(runCtx)

	// Run must end because of shutdown, NOT the stage's transient error.
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v; expected a context error (transient stage error must not kill the kernel)", err)
	}

	// The stage must have been restarted past its failures (proves error
	// isolation + restart, not silent drop).
	if got := stage.runs(); got < 3 {
		t.Fatalf("stage ran %d times; expected >=3 (2 failed restarts + 1 healthy run)", got)
	}
}

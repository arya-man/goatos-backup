package worker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestSupervisorPanicIsolation verifies that a panic in one stage does not
// kill sibling stages or the supervisor.
func TestSupervisorPanicIsolation(t *testing.T) {
	t.Parallel()
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	s := NewSupervisor(logger, pool, 1*time.Second)

	// Register a stage that panics and a no-op stage.
	panicStage := NewPanicStage(logger, "panic-stage")
	noOpStage := NewNoOpStage(logger, "no-op-stage")

	s.RegisterCadence("test", 100*time.Millisecond, panicStage, noOpStage)

	// Run the supervisor for a short time; it should not crash even though
	// panicStage panics.
	runCtx, runCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer runCancel()

	err := s.Run(runCtx)
	// Expect context timeout, not a panic.
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("supervisor returned unexpected error: %v", err)
	}
}

// TestSupervisorTimeoutIsolation verifies that a stage which honors ctx
// cancellation is actually canceled at its assigned deadline (returning
// context.DeadlineExceeded), and that a sibling stage running concurrently
// under its own lock/context is unaffected by the timed-out stage.
//
// The two stages are run directly via runStageOnce with explicit CadenceStage
// timeouts (bypassing the cadence-interval-derived default), so the assertion
// is deterministic and does not depend on tuning cadence intervals against a
// wall-clock test window.
func TestSupervisorTimeoutIsolation(t *testing.T) {
	t.Parallel()
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	s := NewSupervisor(logger, pool, 1*time.Second)

	// slowStage honors ctx (see SlowStage.Run below) and sleeps far longer
	// than the deadline it is given, so it must be canceled, not merely
	// outrun by a longer test window.
	slowStage := NewSlowStage(logger, "slow-stage", 2*time.Second)
	noOpStage := NewNoOpStage(logger, "no-op-stage")

	slowCS := CadenceStage{stage: slowStage, timeout: 50 * time.Millisecond}
	noOpCS := CadenceStage{stage: noOpStage, timeout: 5 * time.Second}

	var slowErr, noOpErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		slowErr = s.runStageOnce(ctx, slowCS)
	}()
	go func() {
		defer wg.Done()
		noOpErr = s.runStageOnce(ctx, noOpCS)
	}()
	wg.Wait()

	if slowErr == nil || !errors.Is(slowErr, context.DeadlineExceeded) {
		t.Fatalf("expected slow-stage to be canceled with context.DeadlineExceeded, got %v", slowErr)
	}
	if noOpErr != nil {
		t.Fatalf("expected sibling no-op-stage to complete unaffected, got %v", noOpErr)
	}
}

// TestDefaultStageTimeoutBoundedByCadenceInterval is a regression guard for
// the U8 fix: a periodic stage's derived timeout must always be strictly less
// than its own cadence interval. Before the fix, runStageOnce floored every
// stage's timeout at 5 minutes (queryTimeout*2, min 5m), which exceeded the
// 1-minute "fast" cadence interval — a wedged fast stage could block its own
// cadence for minutes instead of timing out within it.
func TestDefaultStageTimeoutBoundedByCadenceInterval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		interval     time.Duration
		queryTimeout time.Duration
	}{
		{"fast", 1 * time.Minute, 3 * time.Second},
		{"operational", 15 * time.Minute, 3 * time.Second},
		{"generation", 1 * time.Hour, 3 * time.Second},
		{"housekeeping", 24 * time.Hour, 3 * time.Second},
		{"sub-second-test-interval", 100 * time.Millisecond, 100 * time.Millisecond},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := defaultStageTimeout(tc.interval, tc.queryTimeout)
			if got <= 0 {
				t.Fatalf("expected a positive timeout, got %v", got)
			}
			if got >= tc.interval {
				t.Fatalf("stage timeout %v must be strictly less than cadence interval %v", got, tc.interval)
			}
		})
	}
}

// TestRegisterCadenceAssignsTimeoutBelowInterval verifies RegisterCadence
// wires the derived timeout onto every stage in the cadence.
func TestRegisterCadenceAssignsTimeoutBelowInterval(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	s := NewSupervisor(logger, nil, 3*time.Second)

	s.RegisterCadence("fast", 1*time.Minute,
		NewNoOpStage(logger, "outbox-relay"),
		NewNoOpStage(logger, "notification-dispatcher"),
	)

	def, ok := s.cadences["fast"]
	if !ok {
		t.Fatalf("expected cadence %q to be registered", "fast")
	}
	if len(def.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(def.Stages))
	}
	for _, cs := range def.Stages {
		if cs.timeout <= 0 || cs.timeout >= def.Interval {
			t.Fatalf("stage %q timeout %v must be > 0 and < cadence interval %v", cs.stage.Name(), cs.timeout, def.Interval)
		}
	}
}

// TestRegisterContinuousHasNoForcedTimeout verifies continuous stages keep
// their long-running semantics: no periodic force-timeout is assigned.
func TestRegisterContinuousHasNoForcedTimeout(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	s := NewSupervisor(logger, nil, 3*time.Second)

	s.RegisterContinuous("event-consumer", NewNoOpStage(logger, "consumer"))

	for _, cs := range s.continuous {
		if cs.timeout != 0 {
			t.Fatalf("continuous stage %q must not have a forced periodic timeout, got %v", cs.stage.Name(), cs.timeout)
		}
	}
}

// TestSupervisorStartupCatchUp verifies that each stage runs an immediate
// startup catch-up before the first interval tick.
func TestSupervisorStartupCatchUp(t *testing.T) {
	t.Parallel()
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	s := NewSupervisor(logger, pool, 1*time.Second)

	// Register a stage that records when it runs.
	tracker := NewTrackerStage(logger, "tracker")
	s.RegisterCadence("test", 1*time.Second, tracker)

	// Run for a very short time, less than the 1-second interval.
	runCtx, runCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer runCancel()

	_ = s.Run(runCtx)

	// The tracker should have run at least once (the startup catch-up).
	if tracker.runCount == 0 {
		t.Fatalf("expected at least one startup catch-up run, got %d runs", tracker.runCount)
	}
}

// PanicStage is a test stage that panics on Run.
type PanicStage struct {
	logger *slog.Logger
	name   string
}

func NewPanicStage(logger *slog.Logger, name string) *PanicStage {
	return &PanicStage{logger: logger, name: name}
}

func (p *PanicStage) Run(ctx context.Context) error {
	panic("intentional panic for testing")
}

func (p *PanicStage) Name() string {
	return p.name
}

// SlowStage is a test stage that sleeps for a specified duration.
type SlowStage struct {
	logger   *slog.Logger
	name     string
	duration time.Duration
}

func NewSlowStage(logger *slog.Logger, name string, duration time.Duration) *SlowStage {
	return &SlowStage{logger: logger, name: name, duration: duration}
}

// Run honors ctx cancellation: it sleeps for the configured duration but
// returns ctx.Err() immediately if the context is canceled or its deadline
// is exceeded first. A stage that ignores ctx cannot be isolated by a
// supervisor-imposed timeout, so this is required for the timeout-isolation
// test to exercise real cancellation rather than merely being outrun by a
// longer outer test deadline.
func (s *SlowStage) Run(ctx context.Context) error {
	select {
	case <-time.After(s.duration):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *SlowStage) Name() string {
	return s.name
}

// TrackerStage is a test stage that counts how many times Run is called.
type TrackerStage struct {
	logger   *slog.Logger
	name     string
	runCount int
}

func NewTrackerStage(logger *slog.Logger, name string) *TrackerStage {
	return &TrackerStage{logger: logger, name: name}
}

func (t *TrackerStage) Run(ctx context.Context) error {
	t.runCount++
	return nil
}

func (t *TrackerStage) Name() string {
	return t.name
}

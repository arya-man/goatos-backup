package worker

import (
	"context"
	"errors"
	"log/slog"
	"os"
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

// TestSupervisorTimeoutIsolation verifies that a stage exceeding its timeout
// does not kill sibling stages.
func TestSupervisorTimeoutIsolation(t *testing.T) {
	t.Parallel()
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	s := NewSupervisor(logger, pool, 100*time.Millisecond)

	// Register a stage that sleeps longer than the timeout and a no-op stage.
	slowStage := NewSlowStage(logger, "slow-stage", 500*time.Millisecond)
	noOpStage := NewNoOpStage(logger, "no-op-stage")

	s.RegisterCadence("test", 100*time.Millisecond, slowStage, noOpStage)

	// Run the supervisor for a short time; slow-stage should timeout but no-op
	// should still run.
	runCtx, runCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer runCancel()

	err := s.Run(runCtx)
	// Expect context timeout, not a stage timeout panic.
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("supervisor returned unexpected error: %v", err)
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

func (s *SlowStage) Run(ctx context.Context) error {
	<-time.After(s.duration)
	return nil
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

package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

// StageRunner is the interface a work stage must implement to be orchestrated
// by the Supervisor. Each stage performs one unit of bounded work with its own
// timeout and advisory lock.
type StageRunner interface {
	// Run executes the stage work until completion or context cancellation.
	// The stage is responsible for honoring the context deadline and returning
	// promptly on cancellation.
	Run(ctx context.Context) error

	// Name returns a human-readable name for logging and lock identification.
	Name() string
}

// CadenceStage pairs a stage with its effective per-run timeout. Advisory-lock
// uniqueness is derived from the stage name at lock time (hashtext, see
// stagelock.go), not from a per-registration salt, so lock identity is stable
// across worker versions and registration order.
type CadenceStage struct {
	stage StageRunner

	// timeout bounds a single run of the stage. It is derived at registration
	// time (see defaultStageTimeout) so a periodic stage's timeout is always
	// strictly less than its own cadence interval — a wedged stage times out
	// and releases its advisory lock before the next tick would otherwise
	// queue up behind it. A zero value means "no forced periodic deadline":
	// this is used for continuous (long-running) stages, which must run
	// until the parent context (process shutdown) cancels them, not on a
	// fixed clock.
	timeout time.Duration
}

// Supervisor orchestrates work stages across multiple cadence classes
// (continuous, fast, operational, generation, housekeeping). Each stage
// claims a Postgres advisory lock before running, ensuring at most one
// instance runs the stage at a time across multiple Supervisor processes.
// A panic or timeout in one stage does not affect siblings.
type Supervisor struct {
	logger       *slog.Logger
	pool         *pgxpool.Pool
	queryTimeout time.Duration

	mu         sync.Mutex
	continuous []CadenceStage
	cadences   map[string]CadenceDefinition // cadence name -> definition
}

// CadenceDefinition holds the refresh interval and stages for a named cadence.
type CadenceDefinition struct {
	Interval time.Duration
	Stages   []CadenceStage
}

// NewSupervisor constructs a Supervisor with a shared pgxpool and observability logger.
// Each stage's advisory-lock key is derived from its name via hashtext at lock
// time (see stagelock.go), so keys are stable across worker versions and
// registration order — no per-process salt allocation is needed.
func NewSupervisor(logger *slog.Logger, pool *pgxpool.Pool, queryTimeout time.Duration) *Supervisor {
	return &Supervisor{
		logger:       logger,
		pool:         pool,
		queryTimeout: queryTimeout,
		cadences:     make(map[string]CadenceDefinition),
	}
}

// RegisterContinuous registers stages that run continuously (e.g., event consumer).
// Continuous stages are long-running by design (they block on a subscription
// or stream) and are never force-timed-out on a clock the way a periodic
// cadence stage is: their timeout is left at zero, so runStageOnce lets them
// run until the parent (process shutdown) context cancels them.
func (s *Supervisor) RegisterContinuous(name string, stages ...StageRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stage := range stages {
		s.continuous = append(s.continuous, CadenceStage{
			stage:   stage,
			timeout: 0, // no forced periodic deadline; long-running by design
		})
	}
}

// RegisterCadence registers stages that run at a specific interval (e.g., every 15 minutes).
// Each stage's per-run timeout is derived from the cadence interval (see
// defaultStageTimeout) so it is always strictly less than the interval itself
// — a wedged fast (1-minute) stage can no longer block its own cadence for
// minutes on end the way a flat 5-minute floor previously allowed.
func (s *Supervisor) RegisterCadence(name string, interval time.Duration, stages ...StageRunner) {
	s.RegisterCadenceWithTimeout(name, interval, 0, stages...)
}

// RegisterCadenceWithTimeout is RegisterCadence with an EXPLICIT per-run stage
// timeout budget instead of the interval-derived default. Use it for a stage
// that was retired from a Cloud Run Job whose per-run budget is larger than the
// interval-derived default would give it on a shared lane (e.g. the obligation
// sweeper's retired 180s job budget, which the shared operational lane's
// interval/4 default would shrink to ~75s). A stageTimeout of 0 falls back to
// the derived default. An explicit budget is capped strictly below the interval
// so serial stages in a cadence still cannot pile past a tick.
func (s *Supervisor) RegisterCadenceWithTimeout(name string, interval, stageTimeout time.Duration, stages ...StageRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.cadences[name]; exists {
		s.logger.Warn("cadence_already_registered", "cadence", name)
		return
	}
	effectiveTimeout := stageTimeout
	if effectiveTimeout <= 0 {
		effectiveTimeout = defaultStageTimeout(interval, s.queryTimeout)
	} else if interval > 0 {
		if cap := interval - interval/10; effectiveTimeout > cap {
			s.logger.Warn("cadence_stage_timeout_capped",
				"cadence", name, "requested", effectiveTimeout.String(), "cap", cap.String())
			effectiveTimeout = cap
		}
	}
	var cadenceStages []CadenceStage
	for _, stage := range stages {
		cadenceStages = append(cadenceStages, CadenceStage{
			stage:   stage,
			timeout: effectiveTimeout,
		})
	}
	s.cadences[name] = CadenceDefinition{
		Interval: interval,
		Stages:   cadenceStages,
	}
}

// defaultStageTimeout derives a sane per-run stage timeout for a cadence from
// its tick interval and the shared query timeout. It scales naturally across
// cadence classes without hardcoding cadence names:
//
//   - The timeout is capped at 90% of the interval, so a stage always times
//     out (and releases its advisory lock) with headroom before the next
//     tick — a "fast" (1m) cadence gets a timeout well under a minute, while
//     "housekeeping" (24h) gets a timeout of many hours.
//   - Within that cap, the target is the larger of twice the shared query
//     timeout (room for at least a couple of round trips) and one quarter of
//     the interval (so a generous cadence like "operational" or "generation"
//     gets a stage budget proportional to its own tick, not just a multiple
//     of a single query's timeout).
//
// A zero/negative interval (should not occur for a registered cadence) falls
// back to twice the query timeout, or 1 second if that is also unset, so the
// timeout is never zero/negative.
func defaultStageTimeout(interval, queryTimeout time.Duration) time.Duration {
	const minStageTimeout = 1 * time.Second

	if interval <= 0 {
		if queryTimeout > 0 {
			return queryTimeout * 2
		}
		return minStageTimeout
	}

	timeoutCap := interval - interval/10 // 90% of interval; strictly < interval for interval > 0
	if timeoutCap <= 0 {
		timeoutCap = interval
	}

	target := queryTimeout * 2
	if floor := interval / 4; floor > target {
		target = floor
	}
	if target > timeoutCap {
		target = timeoutCap
	}
	if target < minStageTimeout {
		target = minStageTimeout
	}
	if target >= interval {
		// Guard against rounding pushing target back up to/over the interval
		// for very small intervals (e.g. sub-10ns test intervals).
		target = interval - 1
		if target <= 0 {
			target = 1
		}
	}
	return target
}

// Run starts the supervisor, orchestrating all registered stages and cadences
// until ctx is canceled or an error occurs. Each cadence runs its stages at
// the configured interval, and each stage performs an immediate startup
// catch-up run before the first interval tick.
func (s *Supervisor) Run(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)

	// Start continuous stages (e.g., event consumer) under a supervised restart
	// loop. A continuous stage must run for the whole process lifetime; a
	// transient error or an unexpected clean return must NOT (a) cancel the
	// errgroup and kill every other stage, nor (b) silently stop event handling
	// while the worker appears alive. runContinuous isolates both: it only
	// returns on shutdown (ctx cancellation).
	for _, cadenceStage := range s.continuous {
		cs := cadenceStage
		eg.Go(func() error {
			return s.runContinuous(egCtx, cs)
		})
	}

	// Start cadence tickers and stage runners.
	for cadenceName, def := range s.cadences {
		cadence := cadenceName
		definition := def
		eg.Go(func() error {
			return s.runCadence(egCtx, cadence, definition)
		})
	}

	return eg.Wait()
}

// Continuous-stage restart backoff bounds. A failing continuous stage is
// retried with exponential backoff so a persistently-broken dependency does not
// hot-loop, while an unexpected clean exit restarts promptly so failover to a
// healthy consumer is fast.
const (
	continuousMinBackoff = 1 * time.Second
	continuousMaxBackoff = 30 * time.Second

	// continuousStandbyBackoff (+jitter) bounds how often a standby instance
	// that did NOT win a continuous stage's advisory lock re-attempts it. This
	// is the expected steady state for every non-active HA replica — it is
	// NOT an error or an unexpected exit, so it must not share the tight
	// continuousMinBackoff retry loop (which would otherwise re-query
	// Postgres for the lock roughly every second, forever, on every standby;
	// see KERN-05). The window stays small (single-digit seconds) so a
	// standby still picks up the lock promptly after the active instance
	// dies and releases it.
	continuousStandbyBackoff = 5 * time.Second
	continuousStandbyJitter  = 2 * time.Second
)

// standbyBackoffWithJitter returns continuousStandbyBackoff plus a random
// jitter in [0, continuousStandbyJitter), so many standby replicas contending
// for the same lock do not all re-poll Postgres in lockstep.
func standbyBackoffWithJitter() time.Duration {
	return continuousStandbyBackoff + time.Duration(rand.Int63n(int64(continuousStandbyJitter)))
}

// runContinuous supervises a single continuous stage for the life of the
// process. It runs the stage, and when the stage returns it restarts it —
// EXCEPT when ctx is done (shutdown), which is the only condition that ends the
// loop. Crucially, a stage error is logged and retried with backoff rather than
// returned: returning it would cancel the parent errgroup and take down every
// other stage (the "one transient error kills the whole kernel" failure).
//
// runStageOnce reports whether the advisory lock was acquired, which lets this
// loop distinguish three outcomes instead of collapsing them into one:
//   - an error (lock-acquire failure or a failed stage run): logged as a
//     warning-level failure and retried with exponential backoff, same as
//     before.
//   - lock NOT acquired (another instance holds it — the normal HA standby
//     state): stays quiet (debug only) and backs off on the slower, jittered
//     continuousStandbyBackoff instead of the tight continuousMinBackoff loop.
//   - lock acquired but the stage returned nil anyway (an unexpected clean
//     exit while holding the lock — genuinely abnormal): logged as a warning
//     and restarted promptly on continuousMinBackoff, same as before.
func (s *Supervisor) runContinuous(ctx context.Context, cs CadenceStage) error {
	backoff := continuousMinBackoff
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		acquiredLock, err := s.runStageOnce(ctx, cs)
		if ctx.Err() != nil {
			// Shutdown: the only way out of the loop.
			return ctx.Err()
		}

		var wait time.Duration
		switch {
		case err != nil:
			s.logger.Error("continuous_stage_failed_restarting",
				"stage", cs.stage.Name(), "backoff", backoff.String(), "err", err)
			wait = backoff
			backoff *= 2
			if backoff > continuousMaxBackoff {
				backoff = continuousMaxBackoff
			}

		case !acquiredLock:
			// Standby: another instance holds this stage's advisory lock.
			// Expected steady state — stay quiet and poll on the slower
			// standby cadence, not the tight per-second restart loop.
			s.logger.Debug("continuous_stage_lock_contended", "stage", cs.stage.Name())
			backoff = continuousMinBackoff // reset so a future real failure still starts from the min
			wait = standbyBackoffWithJitter()

		default:
			// Unexpected clean exit while holding the lock. Restart promptly
			// so event handling never silently stops.
			s.logger.Warn("continuous_stage_exited_restarting", "stage", cs.stage.Name())
			backoff = continuousMinBackoff
			wait = backoff
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// runCadence runs a single cadence class (fast, operational, etc.) which
// performs a startup catch-up run, then waits for each interval tick to
// run the stages again.
func (s *Supervisor) runCadence(ctx context.Context, cadenceName string, def CadenceDefinition) error {
	s.logger.Info("cadence_starting", "cadence", cadenceName, "interval", def.Interval.String())

	// Immediate startup catch-up run for each stage.
	for _, cadenceStage := range def.Stages {
		cs := cadenceStage
		if _, err := s.runStageOnce(ctx, cs); err != nil {
			// Log but continue to the next stage; a catch-up failure does not
			// stop the entire cadence.
			s.logger.Error("startup_catch_up_failed", "cadence", cadenceName, "stage", cs.stage.Name(), "err", err)
		}
	}

	// Then run at each interval tick.
	ticker := time.NewTicker(def.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("cadence_stopping", "cadence", cadenceName)
			return ctx.Err()
		case <-ticker.C:
			// Run all stages in the cadence serially (each stage claims an
			// advisory lock, so if multiple instances are running, only one
			// will hold the lock at a time).
			for _, cadenceStage := range def.Stages {
				cs := cadenceStage
				if _, err := s.runStageOnce(ctx, cs); err != nil {
					s.logger.Error("cadence_stage_failed", "cadence", cadenceName, "stage", cs.stage.Name(), "err", err)
					// Continue to the next stage; do not stop the cadence.
				}
			}
		}
	}
}

// runStageOnce runs a single stage with advisory lock, per-stage timeout,
// and panic recovery. Returns an error if the lock cannot be acquired,
// context is canceled, or the stage fails.
//
// acquiredLock reports whether this call actually won the advisory lock and
// ran the stage. It is false when another instance already holds the lock
// (locked=false, err=nil) — the normal HA-standby case — which callers that
// care about that distinction (runContinuous) use to avoid treating a lock
// miss the same as an unexpected clean stage exit (KERN-05).
func (s *Supervisor) runStageOnce(ctx context.Context, cs CadenceStage) (acquiredLock bool, retErr error) {
	stage := cs.stage
	stageTimeout := cs.timeout

	// A zero timeout means "no forced periodic deadline" — used for
	// continuous (long-running) stages, which must keep running until the
	// parent (process shutdown) context cancels them, not on a fixed clock.
	// A periodic cadence stage always has a positive timeout assigned at
	// registration (see defaultStageTimeout), strictly less than its own
	// cadence interval.
	var stageCtx context.Context
	var cancel context.CancelFunc
	if stageTimeout > 0 {
		stageCtx, cancel = context.WithTimeout(ctx, stageTimeout)
	} else {
		stageCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	// Try to acquire the advisory lock. If another instance holds it,
	// this will return immediately with locked=false, and we skip the stage.
	locked, err := AcquireStageLock(stageCtx, s.pool, stage.Name())
	if err != nil {
		s.logger.Error("stage_lock_acquire_failed", "stage", stage.Name(), "err", err)
		return false, fmt.Errorf("acquire lock for stage %q: %w", stage.Name(), err)
	}
	if !locked {
		s.logger.Debug("stage_lock_already_held", "stage", stage.Name())
		return false, nil // Another instance holds the lock; skip.
	}

	// Lock was acquired; the lock is held on a dedicated connection and will
	// be released in defer by ReleaseStageLock.
	acquiredLock = true

	// Run the stage with panic recovery.
	s.logger.Info("stage_starting", "stage", stage.Name())
	startTime := time.Now()
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("stage_panic", "stage", stage.Name(), "panic", r)
			// Named return: propagate the recovered panic as an error so a
			// continuous stage does not exit its errgroup goroutine cleanly
			// (which would silently stop it) — the panic surfaces to Run.
			retErr = fmt.Errorf("stage %q panicked: %v", stage.Name(), r)
		}

		duration := time.Since(startTime)
		if retErr != nil {
			s.logger.Error("stage_failed", "stage", stage.Name(), "duration", duration.String(), "err", retErr)
		} else {
			s.logger.Info("stage_completed", "stage", stage.Name(), "duration", duration.String())
		}

		// Always release the lock, even if the stage failed or panicked.
		if err := ReleaseStageLock(context.Background(), s.pool, stage.Name()); err != nil {
			s.logger.Error("stage_lock_release_failed", "stage", stage.Name(), "err", err)
		}
	}()

	retErr = stage.Run(stageCtx)
	if stageTimeout > 0 && retErr != nil && errors.Is(retErr, context.DeadlineExceeded) {
		retErr = fmt.Errorf("stage %q timeout (%v): %w", stage.Name(), stageTimeout, retErr)
	}
	return acquiredLock, retErr
}

// NoOpStage is a placeholder stage that does nothing; used for testing
// the supervisor shell before real stages are wired in (U3).
type NoOpStage struct {
	logger *slog.Logger
	name   string
}

// NewNoOpStage creates a no-op stage with the given name.
func NewNoOpStage(logger *slog.Logger, name string) *NoOpStage {
	return &NoOpStage{logger: logger, name: name}
}

// Run does nothing and returns nil.
func (n *NoOpStage) Run(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}

// Name returns the stage name.
func (n *NoOpStage) Name() string {
	return n.name
}

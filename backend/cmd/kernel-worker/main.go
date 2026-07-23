package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vgoats/goatos/backend/internal/kernelstages"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/worker"
)

type cliConfig struct {
	Timeout time.Duration
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "kernel-worker"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

	logger := observability.New(observability.Config{Service: "kernel-worker"})

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	deps := kernelstages.Deps{Pool: pool, PgCfg: pgCfg, Logger: logger}

	// Tenant-scoped operational/generation/fast stages need a tenant ID.
	// CRITICAL: this must be set to an actual tenant ID, never a fake/placeholder UUID.
	// Fail fast at startup rather than logging a per-tick error forever.
	// Dev/test environments must explicitly set GOATOS_TENANT_ID to their target tenant.
	tenantID := strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID"))
	if tenantID == "" {
		return errors.New("GOATOS_TENANT_ID is required for the kernel worker (must be set to an actual tenant UUID, not empty or placeholder)")
	}

	// Shared domain-event envelope validator (outbox relay + domain consumer).
	validator, err := kernelstages.NewEnvelopeValidator()
	if err != nil {
		return err
	}

	// Durable (or non-durable dev) outbox publisher, built once and reused across
	// fast-lane ticks. The close func releases the Pub/Sub client on shutdown.
	publisher, closePublisher, err := kernelstages.BuildOutboxPublisher(ctx, deps)
	if err != nil {
		return err
	}
	if closePublisher != nil {
		defer func() { _ = closePublisher() }()
	}

	// Operational stage: obligation sweeper. SweeperConfigFromEnv enforces the
	// tenant + audited actor-id preconditions (a nil TaskCreator silently drops
	// SOP-task creation), so a misconfigured deployment fails to boot here.
	sweeperCfg, err := kernelstages.SweeperConfigFromEnv()
	if err != nil {
		return err
	}

	supervisor := worker.NewSupervisor(logger, pool, pgCfg.QueryTimeout)

	// KERN-01 SAFETY: worker stages gate. Phase 1 (flag=false): worker deploys but
	// stages are shadowed, running in health-only mode pending legacy job retirement.
	// Phase 2 (flag=true): legacy jobs removed and worker stages enabled.
	// Staging sets GOATOS_WORKER_STAGES_ENABLED=true unconditionally after the
	// legacy scheduled-job fleet was retired. Development still uses its cutover
	// gate until that environment converges separately.
	stagesEnabled := stageShadowEnabled()
	if !stagesEnabled {
		logger.Info("kernel_worker_stages_shadowed", "reason", "legacy jobs still active; run health server only")
	}

	// Continuous: Pub/Sub domain-event consumer. Registered only when Pub/Sub is
	// configured (staging/production); local/dev in-process dispatch runs through
	// the outbox eventbus publisher instead.
	consumerCfg, consumerEnabled, err := kernelstages.DomainConsumerConfigFromEnv()
	if err != nil {
		return err
	}
	if stagesEnabled && consumerEnabled {
		supervisor.RegisterContinuous("event-consumer",
			kernelstages.NewDomainConsumerStage(deps, validator, consumerCfg))
	} else if !stagesEnabled {
		logger.Debug("kernel_worker_domain_consumer_shadowed", "reason", "stages not enabled")
	} else {
		logger.Info("kernel_worker_domain_consumer_disabled", "reason", "pubsub not configured")
	}

	if stagesEnabled {
		// Fast lanes (every minute), on SEPARATE cadences so they run as independent
		// goroutines. Outbox relay and notification dispatch must not share one
		// serial lane: a full outbox drain can use most of the ~54s per-run budget
		// (derived from the 1-minute interval), and if the dispatcher ran after it on
		// the same lane a slow outbox would delay — or, if it overran the tick, skip
		// — notification delivery. On their own cadences each gets its own advisory
		// lock (no self-overlap across the HA pair or across ticks) and its own full
		// budget, and a slow outbox cannot starve the dispatcher (KERN-REV-05A).
		//
		// The ~54s outbox budget is sufficient: RunUntilDrained publishes a claimed
		// batch (limit GOATOS_OUTBOX_LIMIT=500) serially and loops until drained or
		// the budget expires; a 500-message batch drains well within 54s (see
		// TestOutboxRelayStageDrains500WithinFastLaneBudget). Unprocessed claimed
		// messages (when ctx cancels mid-batch) are released back to pending
		// immediately via RunOnce's deferred releaseUnprocessedMessages, making them
		// eligible for re-claim on the next tick (KERN-02 mitigation). At-least-once
		// with consumer-side dedup, no loss.
		supervisor.RegisterCadence("outbox", 1*time.Minute,
			kernelstages.NewOutboxRelayStage(deps, publisher, validator, kernelstages.OutboxRelayConfigFromEnv()),
		)
		supervisor.RegisterCadence("notify", 1*time.Minute,
			kernelstages.NewNotificationDispatcherStage(deps, tenantID),
		)
	}

	if stagesEnabled {
		// Obligation sweep (every 5 minutes, isolated on its own lane with an
		// explicit per-run budget). This is the heaviest operational stage at 5k-50k
		// scale — mark-missed, escalation, batch + SOP-task creation, reminders — so
		// it keeps the same headroom the retired goatos-stg obligation-sweeper Cloud
		// Run Job had (-timeout=180s). On the SHARED operational lane below it would
		// only get the interval/4 (~75s) default, since that default is sized so
		// several serial stages fit inside one tick. Isolating it preserves the
		// budget without shrinking the other stages. Configurable via
		// GOATOS_OBLIGATION_SWEEP_TIMEOUT.
		supervisor.RegisterCadenceWithTimeout("obligation-sweep", 5*time.Minute,
			durationEnv("GOATOS_OBLIGATION_SWEEP_TIMEOUT", 180*time.Second),
			kernelstages.NewObligationSweeperStage(deps, sweeperCfg),
		)

		// Operational (every 5 minutes): queue reminder cadence fires (T-7d, daily,
		// due-today), reconcile inventory batches, retry SOP review fanout. All light,
		// so the interval/4 default is ample.
		supervisor.RegisterCadence("operational", 5*time.Minute,
			kernelstages.NewReminderCadenceStage(deps, tenantID),
			kernelstages.NewInventoryBatchReconcilerStage(deps, tenantID),
			kernelstages.NewSopReviewFanoutRetryStage(deps, tenantID),
		)

		// Generation (hourly): idempotently generate/recheck effective vaccination
		// obligations. Event-triggered generation still runs via the domain consumer.
		supervisor.RegisterCadence("generation", 1*time.Hour,
			kernelstages.NewVaccinationGenerationStage(deps, tenantID),
		)

		// Housekeeping (hourly): processed-event retention + expired idempotency
		// keys. 1h initially to match the retired jobs; safe to relax to daily once
		// retention volume at scale is measured.
		supervisor.RegisterCadence("housekeeping", 1*time.Hour,
			kernelstages.NewProcessedEventSweeperStage(deps, tenantID),
			kernelstages.NewIdempotencyKeySweeperStage(deps, tenantID),
			kernelstages.NewCalendarReconcilerStage(deps, tenantID),
			// BUG-016: scheduled recovery for a LOST goat.created event — the sole
			// SM-1 vaccination-generation trigger. Alerts on any gap and repairs it
			// through the same path as the manual backfill-goat-created CLI, so a
			// lost event no longer waits for a human to notice.
			kernelstages.NewGoatCreatedRecoveryStage(deps, tenantID),
		)
	}

	// Cloud Run lifecycle/health listener on $PORT. Required so a Cloud Run
	// SERVICE revision (min=2 HA) becomes ready — the worker itself has no
	// request surface. The listener only answers /livez + /readyz; it never
	// triggers stage work. Skipped when GOATOS_HEALTH_ADDR is empty (e.g. a
	// one-shot local/CI run of the binary).
	if healthAddr := healthListenAddr(); healthAddr != "" {
		health := worker.NewHealthServer(healthAddr, pool, logger)
		health.Start()
		health.SetReady(true)
		defer func() {
			// Drain first (readyz 503) so Cloud Run stops counting this
			// instance before the process exits, then stop the listener.
			health.BeginDraining()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			health.Shutdown(shutdownCtx)
		}()
		logger.Info("kernel_worker_health_listening", "addr", healthAddr)
	}

	logger.Info("kernel_worker_starting", "tenant_id", tenantID, "domain_consumer_enabled", consumerEnabled, "stages_enabled", stagesEnabled)
	if !stagesEnabled {
		// Shadow mode (phase 1, legacy jobs still own every stage): no stages are
		// registered, so supervisor.Run would return immediately and the process
		// would exit the instant it starts — crash-looping a Cloud Run revision and
		// failing any "worker is up with zero restarts" check. Block on the health
		// server until shutdown so the worker stays a live health-only instance.
		<-ctx.Done()
		logger.Info("kernel_worker_shutdown", "mode", "shadow")
		return nil
	}
	if err := supervisor.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("kernel_worker_shutdown")
	return nil
}

// healthListenAddr resolves the health/lifecycle listen address. Cloud Run
// injects PORT; GOATOS_HEALTH_ADDR overrides (set it empty to disable the
// listener for a one-shot binary run).
func healthListenAddr() string {
	if addr, ok := os.LookupEnv("GOATOS_HEALTH_ADDR"); ok {
		return strings.TrimSpace(addr)
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		return ":" + port
	}
	return ":8080"
}

// durationEnv reads a duration from env, falling back to the default on empty or
// unparseable input.
func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func stageShadowEnabled() bool {
	// KERN-01 SAFETY: read GOATOS_WORKER_STAGES_ENABLED to gate stage registration.
	// When false (phase 1), the worker runs in shadow mode (health server only);
	// legacy jobs are still active on their schedules. When true (phase 2),
	// worker stages are enabled and legacy jobs are removed. The terraform
	// binding ensures the two can never both be active.
	enabled := strings.TrimSpace(os.Getenv("GOATOS_WORKER_STAGES_ENABLED"))
	return enabled == "true"
}

func parseFlags(args []string) (cliConfig, error) {
	fs := flag.NewFlagSet("kernel-worker", flag.ContinueOnError)
	var timeout time.Duration
	fs.DurationVar(&timeout, "timeout", 0, "timeout for the entire worker run (0 = no timeout)")
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, err
	}
	return cliConfig{Timeout: timeout}, nil
}

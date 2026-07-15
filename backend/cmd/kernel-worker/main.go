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

	// Continuous: Pub/Sub domain-event consumer. Registered only when Pub/Sub is
	// configured (staging/production); local/dev in-process dispatch runs through
	// the outbox eventbus publisher instead.
	consumerCfg, consumerEnabled, err := kernelstages.DomainConsumerConfigFromEnv()
	if err != nil {
		return err
	}
	if consumerEnabled {
		supervisor.RegisterContinuous("event-consumer",
			kernelstages.NewDomainConsumerStage(deps, validator, consumerCfg))
	} else {
		logger.Info("kernel_worker_domain_consumer_disabled", "reason", "pubsub not configured")
	}

	// Fast lane (every minute): drain outbox, dispatch due notifications.
	supervisor.RegisterCadence("fast", 1*time.Minute,
		kernelstages.NewOutboxRelayStage(deps, publisher, validator, kernelstages.OutboxRelayConfigFromEnv()),
		kernelstages.NewNotificationDispatcherStage(deps, tenantID),
	)

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
	)

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

	logger.Info("kernel_worker_starting", "tenant_id", tenantID, "domain_consumer_enabled", consumerEnabled)
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

func parseFlags(args []string) (cliConfig, error) {
	fs := flag.NewFlagSet("kernel-worker", flag.ContinueOnError)
	var timeout time.Duration
	fs.DurationVar(&timeout, "timeout", 0, "timeout for the entire worker run (0 = no timeout)")
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, err
	}
	return cliConfig{Timeout: timeout}, nil
}

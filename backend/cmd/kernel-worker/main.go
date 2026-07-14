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

	// Tenant-scoped operational/generation/fast stages need a tenant. Fail fast at
	// startup rather than logging a per-tick error forever.
	tenantID := strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID"))
	if tenantID == "" {
		return errors.New("GOATOS_TENANT_ID is required for the kernel worker (tenant-scoped stages)")
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

	// Operational (every 15 minutes): sweep due/missed obligations, create
	// batches + SOP tasks, reconcile inventory, queue reminders + escalations,
	// retry SOP review fanout.
	supervisor.RegisterCadence("operational", 15*time.Minute,
		kernelstages.NewObligationSweeperStage(deps, sweeperCfg),
		kernelstages.NewInventoryBatchReconcilerStage(deps, tenantID),
		kernelstages.NewSopReviewFanoutRetryStage(deps, tenantID),
	)

	// Generation (hourly): idempotently generate/recheck effective vaccination
	// obligations. Event-triggered generation still runs via the domain consumer.
	supervisor.RegisterCadence("generation", 1*time.Hour,
		kernelstages.NewVaccinationGenerationStage(deps, tenantID),
	)

	// Housekeeping (daily): processed-event retention + expired idempotency keys.
	supervisor.RegisterCadence("housekeeping", 24*time.Hour,
		kernelstages.NewProcessedEventSweeperStage(deps, tenantID),
		kernelstages.NewIdempotencyKeySweeperStage(deps, tenantID),
	)

	logger.Info("kernel_worker_starting", "tenant_id", tenantID, "domain_consumer_enabled", consumerEnabled)
	if err := supervisor.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("kernel_worker_shutdown")
	return nil
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

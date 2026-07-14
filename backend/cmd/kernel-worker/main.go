package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// Build the supervisor with reusable stage constructors.
	supervisor := worker.NewSupervisor(logger, pool, pgCfg.QueryTimeout)

	// Register cadence classes (stages will be added in U3 by real stage constructors).
	// For U2 scaffold, we register placeholder/no-op stages so the shell is testable.
	supervisor.RegisterContinuous("event-consumer",
		worker.NewNoOpStage(logger, "event-consumer"))

	supervisor.RegisterCadence("fast", 1*time.Minute,
		worker.NewNoOpStage(logger, "outbox-relay"),
		worker.NewNoOpStage(logger, "notification-dispatcher"))

	supervisor.RegisterCadence("operational", 15*time.Minute,
		worker.NewNoOpStage(logger, "obligation-sweep"))

	supervisor.RegisterCadence("generation", 1*time.Hour,
		worker.NewNoOpStage(logger, "vaccination-generation"))

	supervisor.RegisterCadence("housekeeping", 24*time.Hour,
		worker.NewNoOpStage(logger, "processed-event-retention"))

	logger.Info("kernel_worker_starting")
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

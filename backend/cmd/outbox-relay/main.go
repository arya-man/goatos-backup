package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	outboxlogging "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/logging"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

type cliConfig struct {
	Limit        int
	Timeout      time.Duration
	LeaseTimeout time.Duration
	MaxAttempts  int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	schemaPath, err := findDomainEventEnvelopeSchema()
	if err != nil {
		return err
	}
	validator, err := outboxapp.NewEnvelopeValidator(schemaPath)
	if err != nil {
		return err
	}

	logger := observability.New(observability.Config{Service: "outbox-relay"})
	repo := outboxpg.NewRepository(pool, pgCfg.QueryTimeout)
	publisher := outboxlogging.NewPublisher(logger)
	service := outboxapp.NewService(repo, publisher, validator, outboxapp.Config{
		Limit:        cfg.Limit,
		MaxAttempts:  cfg.MaxAttempts,
		LeaseTimeout: cfg.LeaseTimeout,
	}, logger)

	result, err := service.RunOnce(ctx)
	if err != nil {
		return err
	}
	logger.Info("outbox relay run complete",
		"reclaimed_stale", result.ReclaimedStaleCount,
		"claimed", result.ClaimedCount,
		"published", result.PublishedCount,
		"retry_scheduled", result.RetryScheduledCount,
		"failed", result.FailedCount,
		"dead_letter", result.DeadLetterCount,
	)
	return nil
}

func parseFlags(args []string) (cliConfig, error) {
	var cfg cliConfig
	fs := flag.NewFlagSet("outbox-relay", flag.ContinueOnError)
	fs.IntVar(&cfg.Limit, "limit", 50, "maximum outbox rows to claim in one run")
	fs.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "one-shot relay timeout")
	fs.DurationVar(&cfg.LeaseTimeout, "lease-timeout", 5*time.Minute, "stale publishing lease timeout")
	fs.IntVar(&cfg.MaxAttempts, "max-attempts", 5, "maximum publish attempts before dead-letter")
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, err
	}
	if cfg.Limit < 1 || cfg.Limit > 500 {
		return cliConfig{}, errors.New("limit must be between 1 and 500")
	}
	if cfg.Timeout <= 0 {
		return cliConfig{}, errors.New("timeout must be positive")
	}
	if cfg.LeaseTimeout <= 0 {
		return cliConfig{}, errors.New("lease-timeout must be positive")
	}
	if cfg.MaxAttempts < 1 {
		return cliConfig{}, errors.New("max-attempts must be positive")
	}
	return cfg, nil
}

func findDomainEventEnvelopeSchema() (string, error) {
	candidates := []string{
		filepath.Join("contracts", "jsonschema", "domain-event-envelope.schema.json"),
		filepath.Join("..", "contracts", "jsonschema", "domain-event-envelope.schema.json"),
		filepath.Join("..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"),
		filepath.Join("..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"),
	}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	return "", errors.New("contracts/jsonschema/domain-event-envelope.schema.json not found")
}

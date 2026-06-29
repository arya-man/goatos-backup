package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domainconsumerpg "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/postgres"
	domainconsumerpubsub "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/pubsub"
	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

type cliConfig struct {
	ProjectID      string
	SubscriptionID string
	Timeout        time.Duration
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
	logger := observability.New(observability.Config{Service: "domain-event-consumer"})
	bus := buildDomainBus(pool, pgCfg, logger)
	consumer := consumerapp.NewService(bus, validator, logger).
		WithProcessedEventStore(domainconsumerpg.NewProcessedEventStore(pool, pgCfg.QueryTimeout))
	subscriber, err := domainconsumerpubsub.NewGCPSubscriber(ctx, cfg.ProjectID)
	if err != nil {
		return err
	}
	defer subscriber.Close()
	logger.Info("domain_event_consumer_ready", "project_id", cfg.ProjectID, "subscription_id", cfg.SubscriptionID)
	if err := consumer.Run(ctx, subscriber, cfg.SubscriptionID); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Info("domain_event_consumer_stopped", "reason", err.Error())
			return nil
		}
		return err
	}
	return nil
}

func buildDomainBus(pool *pgxpool.Pool, pgCfg platformpg.Config, logger *slog.Logger) eventbus.Bus {
	bus := eventbus.NewInProcessBus()
	protocolRepo := protocolpg.NewRepository(pool, pgCfg.QueryTimeout)
	vaccinationRepo := vaccinationpg.NewRepository(pool, pgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, pgCfg.QueryTimeout)
	inventoryService := inventoryapp.NewService(inventorypg.NewRepository(pool, pgCfg.QueryTimeout))
	vaccinationService := vaccinationapp.NewService(vaccinationRepo)
	vaccinationCompletion := vaccinationapp.NewCompletionService(vaccinationService, obligationRepo, inventoryService)
	vaccinationBooster := vaccinationapp.NewBoosterService(protocolRepo, obligationRepo)
	vaccinationGeneration := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)
	obligationapp.NewGoatShiftedHandler(obligationRepo).Register(bus)
	obligationapp.NewGoatExitedHandler(obligationRepo).Register(bus)
	vaccinationapp.NewGoatCreatedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewGoatRecheckHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewProtocolPublishedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewManualCampaignHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).Register(bus)
	vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
	if logger != nil {
		logger.Info("domain_event_handlers_registered")
	}
	return bus
}

func parseFlags(args []string) (cliConfig, error) {
	cfg := cliConfig{
		ProjectID:      firstNonEmptyEnv("GOATOS_PUBSUB_PROJECT_ID", "GOOGLE_CLOUD_PROJECT"),
		SubscriptionID: firstNonEmptyEnv("GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID", "GOATOS_PUBSUB_SUBSCRIPTION_ID"),
	}
	fs := flag.NewFlagSet("domain-event-consumer", flag.ContinueOnError)
	fs.StringVar(&cfg.ProjectID, "project-id", cfg.ProjectID, "Google Cloud project id for Pub/Sub")
	fs.StringVar(&cfg.SubscriptionID, "subscription", cfg.SubscriptionID, "Pub/Sub subscription id for domain events")
	fs.DurationVar(&cfg.Timeout, "timeout", 0, "optional run timeout; 0 runs until signal/context cancellation")
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, err
	}
	cfg.ProjectID = strings.TrimSpace(cfg.ProjectID)
	cfg.SubscriptionID = strings.TrimSpace(cfg.SubscriptionID)
	if cfg.ProjectID == "" {
		return cliConfig{}, errors.New("project id is required via --project-id, GOATOS_PUBSUB_PROJECT_ID, or GOOGLE_CLOUD_PROJECT")
	}
	if cfg.SubscriptionID == "" {
		return cliConfig{}, errors.New("subscription is required via --subscription, GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID, or GOATOS_PUBSUB_SUBSCRIPTION_ID")
	}
	if cfg.Timeout < 0 {
		return cliConfig{}, errors.New("timeout must be non-negative")
	}
	return cfg, nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func findDomainEventEnvelopeSchema() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("GOATOS_DOMAIN_EVENT_SCHEMA_PATH")); configured != "" {
		abs, err := filepath.Abs(configured)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
		return "", fmt.Errorf("GOATOS_DOMAIN_EVENT_SCHEMA_PATH does not exist: %s", abs)
	}
	candidates := []string{
		filepath.Join("/app", "contracts", "jsonschema", "domain-event-envelope.schema.json"),
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

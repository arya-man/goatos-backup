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

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	domainconsumerpg "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/postgres"
	domainconsumerpubsub "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/pubsub"
	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
	eventwiring "github.com/vgoats/goatos/backend/internal/eventwiring"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	healthpg "github.com/vgoats/goatos/backend/internal/health/adapters/postgres"
	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	notificationbridge "github.com/vgoats/goatos/backend/internal/notificationbridge"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
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

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "domain-event-consumer"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

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
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, pgCfg.QueryTimeout))
	countsService := countsapp.NewService(countspg.NewRepository(pool, pgCfg.QueryTimeout))
	vaccinationRepo := vaccinationpg.NewRepository(pool, pgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, pgCfg.QueryTimeout)
	inventoryService := inventoryapp.NewService(inventorypg.NewRepository(pool, pgCfg.QueryTimeout))
	vaccinationService := vaccinationapp.NewService(vaccinationRepo)
	vaccinationCompletion := vaccinationapp.NewCompletionService(vaccinationService, obligationRepo, inventoryService)
	sopService := sopapp.NewService(soppg.NewRepository(pool, pgCfg.QueryTimeout))
	vaccinationBooster := vaccinationapp.NewBoosterService(protocolRepo, obligationRepo).WithGoatReader(vaccinationRepo).WithCrossVaccineGapReader(vaccinationRepo)
	vaccinationGeneration := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)
	workforceRepo := workforcepg.NewRepository(pool, pgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	// Counts approval + feed-direction repos so this durable-bus consumer can apply the shifting,
	// feed-distribution, and feed-packing verification verdicts. These handlers live on the API's
	// in-process bus too (bootstrap/api.go), but that bus never receives the async verdict events --
	// the verdict is delivered ONLY through the outbox -> this consumer, so a missing registration here
	// is a silent drop that strands every feed/shifting approval in pending_verification (as it did).
	// The shifting apply relocates animals and writes identity audit in one txn, so it needs the same
	// identity tx writer the API wires (approval_repository.go WithIdentityTxWriter).
	identityRepo := identitypg.NewRepository(pool, pgCfg.QueryTimeout)
	countsApprovalRepo := countspg.NewRepository(pool, pgCfg.QueryTimeout).WithIdentityTxWriter(identityRepo)
	countsMilkPreparationRepo := countspg.NewRepository(pool, pgCfg.QueryTimeout)
	feedDirectionRepo := feeddirectionpg.NewRepository(pool, pgCfg.QueryTimeout)
	healthRepo := healthpg.NewRepository(pool, pgCfg.QueryTimeout)
	weighingRepo := weighingpg.NewRepository(pool, pgCfg.QueryTimeout)
	workflowService := eventwiring.NewWorkflowConsumerService(pool, pgCfg.QueryTimeout, logger)
	obligationapp.NewGoatShiftedHandler(obligationRepo).Register(bus)
	obligationapp.NewGoatExitedHandler(obligationRepo).Register(bus)
	obligationapp.NewOperatorConfigReplanHandler(obligationRepo).Register(bus)
	vaccinationapp.NewGoatCreatedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewGoatRecheckHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewProtocolPublishedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).WithClosureProjector(sopService).Register(bus)
	vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
	notificationbridge.NewVerificationEventConsumer(rosterService, calendarService, logger).Register(bus)
	notificationbridge.NewWeighingSubmissionEventConsumer(rosterService, calendarService, logger).Register(bus)
	notificationbridge.NewWeighingLifecycleEventConsumer(rosterService, calendarService, logger).Register(bus)
	calendarapp.NewObligationMissedHandler(calendarService).Register(bus)
	countsapp.NewProjectionInputHandler(countsService).Register(bus)
	// Keep every durable handler explicit in this production bus builder. The cascade-event-wiring
	// guard compares this list with kernelstages.BuildDomainBus so a wrapper cannot hide bus drift.
	countsapp.NewShiftingVerificationHandler(countsApprovalRepo, nil).Register(bus)
	countsapp.NewMilkPreparationVerificationHandler(countsMilkPreparationRepo).Register(bus)
	countsapp.NewMilkFeedingVerificationHandler(countsMilkPreparationRepo).Register(bus)
	feeddirectionapp.NewFeedDistributionVerificationHandler(feedDirectionRepo, logger).Register(bus)
	feeddirectionapp.NewFeedPackingVerificationHandler(feedDirectionRepo, logger).Register(bus)
	feeddirectionapp.NewFeedTransportVerificationHandler(feedDirectionRepo, logger).Register(bus)
	// Weighing verdict applier: weighing enqueues a verification item for every
	// observation, so without this consumer every approve/reject is a silent drop.
	weighingapp.NewVerificationVerdictHandler(weighingRepo, logger).Register(bus)
	tasksapp.NewCountsDeathReportedHandler(workflowService).Register(bus)
	tasksapp.NewCountsDeathRejectedHandler(workflowService).Register(bus)
	tasksapp.NewGoatCreatedWorkflowHandler(workflowService).Register(bus)
	tasksapp.NewGoatExitedWorkflowHandler(workflowService).Register(bus)
	tasksapp.NewIdentifierAddedWorkflowHandler(workflowService).Register(bus)
	tasksapp.NewDeathVerificationHandler(workflowService, nil).Register(bus)
	tasksapp.NewBirthVerificationHandler(workflowService, nil).Register(bus)
	healthapp.NewDeathLifecycleHandler(healthRepo).Register(bus)

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

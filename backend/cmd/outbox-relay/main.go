package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	eventwiring "github.com/vgoats/goatos/backend/internal/eventwiring"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	notificationbridge "github.com/vgoats/goatos/backend/internal/notificationbridge"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	outboxpublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher"
	eventbuspublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/eventbus"
	pubsubpublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/pubsub"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	outboxports "github.com/vgoats/goatos/backend/internal/outbox/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
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

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "outbox-relay"})
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

	logger := observability.New(observability.Config{Service: "outbox-relay"})
	repo := outboxpg.NewRepository(pool, pgCfg.QueryTimeout)
	publisherKind := os.Getenv("GOATOS_OUTBOX_PUBLISHER")
	publisher, closePublisher, err := buildPublisher(ctx, publisherKind, pool, pgCfg, logger)
	if err != nil {
		return err
	}
	if closePublisher != nil {
		defer closePublisher()
	}
	service := outboxapp.NewService(repo, publisher, validator, outboxapp.Config{
		Limit:        cfg.Limit,
		MaxAttempts:  cfg.MaxAttempts,
		LeaseTimeout: cfg.LeaseTimeout,
	}, logger)

	result, err := service.RunUntilDrained(ctx)
	if err != nil {
		return err
	}
	logger.Info("outbox relay run complete",
		"batches_processed", result.BatchesProcessed,
		"reclaimed_stale", result.ReclaimedStaleCount,
		"claimed", result.ClaimedCount,
		"published", result.PublishedCount,
		"retry_scheduled", result.RetryScheduledCount,
		"failed", result.FailedCount,
		"dead_letter", result.DeadLetterCount,
	)
	return nil
}

func buildPublisher(ctx context.Context, kind string, pool *pgxpool.Pool, pgCfg platformpg.Config, logger *slog.Logger) (outboxports.Publisher, func() error, error) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" {
		return nil, nil, fmt.Errorf("GOATOS_OUTBOX_PUBLISHER must be set: use 'pubsub' for staging/production; 'eventbus' or 'logging' require GOATOS_OUTBOX_ALLOW_NONDURABLE=1 and are for local/dev only")
	}
	switch normalized {
	case "logging", "log":
		// Logging publisher marks outbox rows published with no fanout at all — a silent event-loss
		// footgun in production. Fail closed unless explicitly opted into for local/dev.
		if err := requireNonDurablePublisherAllowed(normalized); err != nil {
			return nil, nil, err
		}
		return outboxpublisher.Select(kind, logger, nil), nil, nil
	case "eventbus", "local", "inprocess":
		// In-process fanout delivers to handlers in THIS process only — not the durable Pub/Sub topic
		// other services consume. Allowed for the local business-chain rehearsal, never silently in prod.
		if err := requireNonDurablePublisherAllowed(normalized); err != nil {
			return nil, nil, err
		}
		bus := eventbus.NewInProcessBus()
		protocolRepo := protocolpg.NewRepository(pool, pgCfg.QueryTimeout)
		countsService := countsapp.NewService(countspg.NewRepository(pool, pgCfg.QueryTimeout))
		vaccinationRepo := vaccinationpg.NewRepository(pool, pgCfg.QueryTimeout)
		obligationRepo := obligationpg.NewRepository(pool, pgCfg.QueryTimeout)
		inventoryService := inventoryapp.NewService(inventorypg.NewRepository(pool, pgCfg.QueryTimeout))
		vaccinationService := vaccinationapp.NewService(vaccinationRepo)
		vaccinationCompletion := vaccinationapp.NewCompletionService(vaccinationService, obligationRepo, inventoryService)
		sopService := sopapp.NewService(soppg.NewRepository(pool, pgCfg.QueryTimeout))
		vaccinationBooster := vaccinationapp.NewBoosterService(protocolRepo, obligationRepo).WithGoatReader(vaccinationRepo).WithCrossVaccineGapReader(vaccinationRepo)
		generation := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)
		workforceRepo := workforcepg.NewRepository(pool, pgCfg.QueryTimeout)
		rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
		calendarRepo := calendarpg.NewRepository(pool, pgCfg.QueryTimeout)
		calendarService := calendarapp.NewService(calendarRepo)
		// Counts approval + feed-direction repos for the shifting/feed verification appliers below. The
		// shifting apply relocates animals and writes identity audit in one txn, so it needs the identity
		// tx writer (approval_repository.go WithIdentityTxWriter), same as bootstrap/api.go.
		identityRepo := identitypg.NewRepository(pool, pgCfg.QueryTimeout)
		countsApprovalRepo := countspg.NewRepository(pool, pgCfg.QueryTimeout).WithIdentityTxWriter(identityRepo)
		feedDirectionRepo := feeddirectionpg.NewRepository(pool, pgCfg.QueryTimeout)
		weighingRepo := weighingpg.NewRepository(pool, pgCfg.QueryTimeout)
		obligationapp.NewGoatShiftedHandler(obligationRepo).Register(bus)
		obligationapp.NewGoatExitedHandler(obligationRepo).Register(bus)
		obligationapp.NewOperatorConfigReplanHandler(obligationRepo).Register(bus)
		vaccinationapp.NewGoatCreatedHandler(generation).Register(bus)
		vaccinationapp.NewGoatRecheckHandler(generation).Register(bus)
		vaccinationapp.NewProtocolPublishedHandler(generation).Register(bus)
		vaccinationapp.NewVerificationHandler(vaccinationCompletion).WithClosureProjector(sopService).Register(bus)
		vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
		vaccineLabels := notificationbridge.NewVaccineLabelResolver(pool, logger)
		notificationbridge.NewVerificationEventConsumer(rosterService, calendarService, logger).WithVaccineLabels(vaccineLabels).Register(bus)
		notificationbridge.NewWeighingSubmissionEventConsumer(rosterService, calendarService, logger).Register(bus)
		notificationbridge.NewWeighingLifecycleEventConsumer(rosterService, calendarService, logger).Register(bus)
		countsapp.NewProjectionInputHandler(countsService).Register(bus)
		// Shifting + feed verification appliers: the ONE shared registration (see bootstrap/api.go and
		// cmd/domain-event-consumer). In local eventbus mode this in-process bus IS the delivery, so
		// without these a verifier approval never applies locally either.
		eventwiring.RegisterVerificationAppliers(bus, feedDirectionRepo, countsApprovalRepo, weighingRepo, logger)
		// Birth/death workflow consumers: in local eventbus mode this in-process bus IS the delivery,
		// so without these an approved birth/death opens no follow-up work locally.
		eventwiring.RegisterWorkflowConsumers(bus,
			eventwiring.NewWorkflowConsumerService(pool, pgCfg.QueryTimeout, logger), logger)
		logger.Info("outbox_relay_eventbus_dispatcher_ready")
		return eventbuspublisher.New(bus), nil, nil
	case outboxpublisher.KindPubSub:
		projectID := strings.TrimSpace(os.Getenv("GOATOS_PUBSUB_PROJECT_ID"))
		if projectID == "" {
			projectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
		}
		if projectID == "" {
			return nil, nil, fmt.Errorf("GOATOS_OUTBOX_PUBLISHER=pubsub requires GOATOS_PUBSUB_PROJECT_ID or GOOGLE_CLOUD_PROJECT")
		}
		topicID := firstNonEmptyEnv("GOATOS_OUTBOX_PUBSUB_TOPIC_ID", "GOATOS_PUBSUB_TOPIC_ID")
		if topicID == "" {
			return nil, nil, fmt.Errorf("GOATOS_OUTBOX_PUBLISHER=pubsub requires GOATOS_OUTBOX_PUBSUB_TOPIC_ID")
		}
		client, err := pubsubpublisher.NewGCPMessagePublisher(ctx, projectID)
		if err != nil {
			return nil, nil, err
		}
		logger.Info("outbox_relay_pubsub_publisher_ready", "project_id", projectID, "topic_id", topicID)
		return pubsubpublisher.NewPublisherWithConfig(client, pubsubpublisher.Config{TopicID: topicID}), client.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported GOATOS_OUTBOX_PUBLISHER %q", kind)
	}
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

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

// requireNonDurablePublisherAllowed fails closed for non-Pub/Sub publishers (logging, eventbus, ...):
// they mark outbox rows published without durable cross-service Pub/Sub egress, so staging/production
// must use GOATOS_OUTBOX_PUBLISHER=pubsub. Local/dev runs opt in explicitly via
// GOATOS_OUTBOX_ALLOW_NONDURABLE.
func requireNonDurablePublisherAllowed(kind string) error {
	if envTruthy("GOATOS_OUTBOX_ALLOW_NONDURABLE") {
		return nil
	}
	return fmt.Errorf("GOATOS_OUTBOX_PUBLISHER=%q is a non-durable publisher (no Pub/Sub egress); set GOATOS_OUTBOX_ALLOW_NONDURABLE=1 to permit it for local/dev, or use GOATOS_OUTBOX_PUBLISHER=pubsub for staging/production", kind)
}

func envTruthy(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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

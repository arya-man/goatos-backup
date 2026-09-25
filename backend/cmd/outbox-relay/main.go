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

	"github.com/vgoats/goatos/backend/internal/browserpush"
	browserpushpg "github.com/vgoats/goatos/backend/internal/browserpush/adapters/postgres"
	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	eventwiring "github.com/vgoats/goatos/backend/internal/eventwiring"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	healthpg "github.com/vgoats/goatos/backend/internal/health/adapters/postgres"
	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	notificationaudiencepg "github.com/vgoats/goatos/backend/internal/notificationaudience/adapters/postgres"
	notificationbridge "github.com/vgoats/goatos/backend/internal/notificationbridge"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	outboxpublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher"
	eventbuspublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/eventbus"
	pubsubpublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/pubsub"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	outboxports "github.com/vgoats/goatos/backend/internal/outbox/ports"
	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
	pccareverificationbridge "github.com/vgoats/goatos/backend/internal/pccare/adapters/verificationbridge"
	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	penroutinespg "github.com/vgoats/goatos/backend/internal/penroutines/adapters/postgres"
	penroutinesverificationbridge "github.com/vgoats/goatos/backend/internal/penroutines/adapters/verificationbridge"
	penroutinesapp "github.com/vgoats/goatos/backend/internal/penroutines/app"
	penvisitspg "github.com/vgoats/goatos/backend/internal/penvisits/adapters/postgres"
	penvisitsverificationbridge "github.com/vgoats/goatos/backend/internal/penvisits/adapters/verificationbridge"
	penvisitsapp "github.com/vgoats/goatos/backend/internal/penvisits/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	toxinpg "github.com/vgoats/goatos/backend/internal/toxin/adapters/postgres"
	toxinapp "github.com/vgoats/goatos/backend/internal/toxin/app"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	weighingverificationbridge "github.com/vgoats/goatos/backend/internal/weighing/adapters/verificationbridge"
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
	pgCfg.ApplicationName = platformpg.ServiceApplicationName("outbox-relay")
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
		// The same seam bootstrap/api.go uses: a person's subscribed browsers BESIDE their phones, so
		// a push delivered through the outbox reaches Chrome too. This process is where the async
		// task/mention/status pushes actually originate; wiring it in the API alone left them phone-only.
		notifyRecipients := notificationbridge.WithBrowserRecipients(rosterService, browserpush.NewService(browserpushpg.NewRepository(pool, pgCfg.QueryTimeout)), logger)
		calendarRepo := calendarpg.NewRepository(pool, pgCfg.QueryTimeout)
		calendarService := calendarapp.NewService(calendarRepo)
		// Counts approval + feed-direction repos for the shifting/feed verification appliers below. The
		// shifting apply relocates animals and writes identity audit in one txn, so it needs the identity
		// tx writer (approval_repository.go WithIdentityTxWriter), same as bootstrap/api.go.
		identityRepo := identitypg.NewRepository(pool, pgCfg.QueryTimeout)
		countsApprovalRepo := countspg.NewRepository(pool, pgCfg.QueryTimeout).WithIdentityTxWriter(identityRepo)
		countsMilkPreparationRepo := countspg.NewRepository(pool, pgCfg.QueryTimeout)
		feedDirectionRepo := feeddirectionpg.NewRepository(pool, pgCfg.QueryTimeout)
		healthRepo := healthpg.NewRepository(pool, pgCfg.QueryTimeout)
		weighingRepo := weighingpg.NewRepository(pool, pgCfg.QueryTimeout)
		// Weighing's apply-receipt seam. The relay is where the weighing verdict applier ACTUALLY
		// runs (the API's in-process bus does not receive verdict events at all), so this is the
		// process that must tell verification the verdict landed -- without it every applied
		// weighing verdict would stay stuck reading as "decided, not yet in effect".
		weighingVerificationBridge := weighingverificationbridge.New(
			verificationpg.NewRepository(pool, pgCfg.QueryTimeout),
		)
		verificationService := verificationapp.NewService(verificationpg.NewRepository(pool, pgCfg.QueryTimeout), nil)
		if err := pccareverificationbridge.RegisterCategories(verificationService); err != nil {
			return nil, nil, fmt.Errorf("register pc care verification categories: %w", err)
		}
		if err := verificationService.RegisterCategory(verificationcatalog.PenVisit); err != nil {
			return nil, nil, fmt.Errorf("register pen visit verification category: %w", err)
		}
		if err := verificationService.RegisterCategory(verificationcatalog.PenRoutine); err != nil {
			return nil, nil, fmt.Errorf("register pen routine verification category: %w", err)
		}
		pcCareVerificationBridge := pccareverificationbridge.New(verificationService)
		pcCareRepo := pccarepg.NewRepository(pool, pgCfg.QueryTimeout)
		penVisitsRepo := penvisitspg.NewRepository(pool, pgCfg.QueryTimeout)
		penRoutinesRepo := penroutinespg.NewRepository(pool, pgCfg.QueryTimeout)
		obligationapp.NewGoatShiftedHandler(obligationRepo).Register(bus)
		obligationapp.NewGoatExitedHandler(obligationRepo).Register(bus)
		obligationapp.NewOperatorConfigReplanHandler(obligationRepo).Register(bus)
		vaccinationapp.NewGoatCreatedHandler(generation).Register(bus)
		vaccinationapp.NewGoatRecheckHandler(generation).Register(bus)
		vaccinationapp.NewGoatReinstatedHandler(generation).Register(bus)
		vaccinationapp.NewProtocolPublishedHandler(generation).Register(bus)
		vaccinationapp.NewVerificationHandler(vaccinationCompletion).WithClosureProjector(sopService).Register(bus)
		vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
		vaccineLabels := notificationbridge.NewVaccineLabelResolver(pool, logger)
		// locationNames enriches rework/approval push copy with the park/shed's human name (see
		// kernelstages/bus.go C-defect-B): WithVaccineLabels alone was chained here but NOT
		// WithLocationNames, so every push this relay's eventbus dispatcher produced degraded to
		// the generic, unactionable "The proof is ready for operational closure" copy with no
		// park/shed named.
		locationNames := notificationbridge.NewLocationNameResolver(pool)
		// WHO hears each leadership push is per-designation config (maintainer decision 2026-09-08):
		// every upward-routing consumer below resolves its audience through the stored override,
		// falling back to the catalog default. Pinned by notificationbridge's audience wiring test.
		leadershipAudience := notificationbridge.NewStoredAudience(notifyRecipients, notificationaudiencepg.NewRepository(pool, pgCfg.QueryTimeout), logger)
		notificationbridge.NewVerificationEventConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).WithVaccineLabels(vaccineLabels).WithLocationNames(locationNames).Register(bus)
		notificationbridge.NewWeighingSubmissionEventConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).Register(bus)
		notificationbridge.NewWeighingLifecycleEventConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).Register(bus)
		// The afternoon feed correction's packing reopen: DOWNWARD push to the packer whose bag was
		// taken back, carrying the old-vs-new quantities (feed.packing.reopened; maintainer decision
		// 2026-08-29).
		notificationbridge.NewFeedPackingReopenNotifyConsumer(notifyRecipients, calendarService, logger).Register(bus)
		// Sale -> Feed Director notice (maintainer decision 2026-09-07): goat.sale_allocated, emitted once
		// per confirm with the pen-by-pen breakdown, pushes the pens and the feed day to reduce from.
		notificationbridge.NewSaleFeedReduceNotifier(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).WithFeedClocks(feeddirectionpg.NewRepository(pool, pgCfg.QueryTimeout)).Register(bus)
		notificationbridge.NewLeadershipTaskNotifyConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).Register(bus)
		notificationbridge.NewLeadershipTaskActivityNotifyConsumer(notifyRecipients, calendarService, logger).Register(bus)
		notificationbridge.NewLeaveRequestNotifyConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).Register(bus)
		notificationbridge.NewAnimalPurchaseNotifyConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).Register(bus)
		countsapp.NewProjectionInputHandler(countsService).Register(bus)
		// Shifting + feed verification appliers: the ONE shared registration (see bootstrap/api.go and
		// cmd/domain-event-consumer). In local eventbus mode this in-process bus IS the delivery, so
		// without these a verifier approval never applies locally either.
		eventwiring.RegisterVerificationAppliers(bus, feedDirectionRepo, countsApprovalRepo, countsMilkPreparationRepo, countsMilkPreparationRepo, weighingRepo, weighingVerificationBridge, pcCareRepo, healthRepo, penVisitsRepo, pcCareRepo, penRoutinesRepo, logger)
		countsapp.NewPenReconciliationRaiser(countsMilkPreparationRepo, logger, nil).Register(bus)
		countsapp.NewPenReconciliationVerificationHandler(countsMilkPreparationRepo, nil).Register(bus)
		pccareapp.NewPCCarePendingVerificationHandler(pcCareVerificationBridge, logger).Register(bus)
		// Pen visit submit -> verifier item (maintainer decision 2026-09-12), the PC Care shape.
		penvisitsapp.NewPendingVerificationHandler(penvisitsverificationbridge.New(verificationService), logger).Register(bus)
		// Pen routine submit -> verifier item (maintainer instruction 2026-09-16), the same shape.
		penroutinesapp.NewPendingVerificationHandler(penroutinesverificationbridge.New(verificationService), logger).WithTaskReader(penRoutinesRepo).Register(bus)
		// Birth/death workflow consumers: in local eventbus mode this in-process bus IS the delivery,
		// so without these an approved birth/death opens no follow-up work locally.
		relayWorkflowService := eventwiring.NewWorkflowConsumerService(pool, pgCfg.QueryTimeout, logger)
		eventwiring.RegisterWorkflowConsumers(bus, relayWorkflowService, logger)
		captureEnqueuer, captureReviews := eventwiring.NewCountsCaptureStores(pool, pgCfg.QueryTimeout)
		eventwiring.RegisterCountsCaptureConsumers(bus, captureEnqueuer, captureReviews, relayWorkflowService)
		eventwiring.RegisterSaleReleaseConsumers(bus, pool, pgCfg.QueryTimeout)
		healthapp.NewDeathLifecycleHandler(healthRepo).Register(bus)
		// Toxin task creation (maintainer decision 2026-08-25): in local eventbus mode this
		// in-process bus IS the delivery, so without this a recorded feed purchase never gets its
		// aflatoxin test task locally. The durable twin is cmd/domain-event-consumer.
		toxinapp.NewFeedPurchaseReachedHandler(toxinpg.NewRepository(pool, pgCfg.QueryTimeout), logger).Register(bus)
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

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

	"github.com/vgoats/goatos/backend/internal/browserpush"
	browserpushpg "github.com/vgoats/goatos/backend/internal/browserpush/adapters/postgres"
	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	domainconsumerpg "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/postgres"
	domainconsumerpubsub "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/pubsub"
	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
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
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
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
	// The same seam bootstrap/api.go uses: a person's subscribed browsers BESIDE their phones, so
	// a push delivered through the outbox reaches Chrome too. This process is where the async
	// task/mention/status pushes actually originate; wiring it in the API alone left them phone-only.
	notifyRecipients := notificationbridge.WithBrowserRecipients(rosterService, browserpush.NewService(browserpushpg.NewRepository(pool, pgCfg.QueryTimeout)), logger)
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
	vaccineLabels := notificationbridge.NewVaccineLabelResolver(pool, logger)
	// locationNames enriches rework/approval push copy with the park/shed's human name (see
	// kernelstages/bus.go C-defect-B): WithVaccineLabels alone was chained here but NOT
	// WithLocationNames, so every push this durable consumer produced -- the path that actually
	// delivers a rework/approved push in production, since the outbox only reaches this consumer,
	// never the API's in-process bus -- degraded straight to the generic, unactionable "The proof
	// is ready for operational closure" copy with no park/shed named.
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
	// A missed obligation must reach people, not just open an escalation row: DOWN to the assigned
	// operator, UP to the park head and the owning module's director. locationNames enriches the
	// push with the park's human name (confirmed maintainer defect: pushes were too abstract to
	// act on) -- a tiny, dependency-free lookup owned entirely by notificationbridge.
	calendarapp.NewObligationMissedHandler(calendarService).WithNotifier(notificationbridge.NewObligationMissedNotifier(calendarService, notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).WithLocationNames(locationNames)).Register(bus)
	countsapp.NewProjectionInputHandler(countsService).Register(bus)
	// Keep every durable verdict applier on the shared registration path so the
	// production consumer cannot drift from API/outbox-relay wiring. This path
	// includes milk-preparation -> UHT stock consumption forwarding.
	pcCareRepo := pccarepg.NewRepository(pool, pgCfg.QueryTimeout)
	penVisitsRepo := penvisitspg.NewRepository(pool, pgCfg.QueryTimeout)
	penRoutinesRepo := penroutinespg.NewRepository(pool, pgCfg.QueryTimeout)
	eventwiring.RegisterVerificationAppliers(bus, feedDirectionRepo, countsApprovalRepo, countsMilkPreparationRepo, countsMilkPreparationRepo, weighingRepo, weighingverificationbridge.New(verificationpg.NewRepository(pool, pgCfg.QueryTimeout)), pcCareRepo, healthRepo, penVisitsRepo, pcCareRepo, penRoutinesRepo, logger)
	countsapp.NewPenReconciliationRaiser(countsMilkPreparationRepo, logger, nil).Register(bus)
	countsapp.NewPenReconciliationVerificationHandler(countsMilkPreparationRepo, nil).Register(bus)
	// Toxin (maintainer decision 2026-08-25): procurement.feed_purchase.reached reaches THIS
	// durable consumer, never the API's in-process bus, so a missing registration here is a
	// silent drop that leaves every purchased load without its aflatoxin test task. Mirrors
	// kernelstages.BuildDomainBus.
	toxinapp.NewFeedPurchaseReachedHandler(toxinpg.NewRepository(pool, pgCfg.QueryTimeout), logger).Register(bus)
	verificationService := verificationapp.NewService(verificationpg.NewRepository(pool, pgCfg.QueryTimeout), nil)
	if err := pccareverificationbridge.RegisterCategories(verificationService); err != nil {
		panic(fmt.Sprintf("register pc care verification categories: %v", err))
	}
	if err := verificationService.RegisterCategory(verificationcatalog.PenVisit); err != nil {
		panic(fmt.Sprintf("register pen visit verification category: %v", err))
	}
	if err := verificationService.RegisterCategory(verificationcatalog.PenRoutine); err != nil {
		panic(fmt.Sprintf("register pen routine verification category: %v", err))
	}
	pccareapp.NewPCCarePendingVerificationHandler(pccareverificationbridge.New(verificationService), logger).Register(bus)
	// Pen visit submit -> verifier item (maintainer decision 2026-09-12), the PC Care shape.
	penvisitsapp.NewPendingVerificationHandler(penvisitsverificationbridge.New(verificationService), logger).Register(bus)
	// Pen routine submit -> verifier item (maintainer instruction 2026-09-16), the same shape.
	penroutinesapp.NewPendingVerificationHandler(penroutinesverificationbridge.New(verificationService), logger).WithTaskReader(penRoutinesRepo).Register(bus)
	// One shared list for every bus process (docs/decisions/sop-driven-herd-operations.md).
	eventwiring.RegisterWorkflowConsumers(bus, workflowService, logger)
	captureEnqueuer, captureReviews := eventwiring.NewCountsCaptureStores(pool, pgCfg.QueryTimeout)
	eventwiring.RegisterCountsCaptureConsumers(bus, captureEnqueuer, captureReviews, workflowService)
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

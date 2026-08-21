package kernelstages

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
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
	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
	pccareverificationbridge "github.com/vgoats/goatos/backend/internal/pccare/adapters/verificationbridge"
	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// BuildDomainBus wires the in-process domain event bus with every module
// handler, mirroring the domain-event-consumer command's buildDomainBus. It is
// shared by the continuous domain-consumer stage (dispatching Pub/Sub events)
// and the local/dev in-process outbox publisher path so the handler set stays in
// one place. It reuses module app-service constructors only; no SQL is
// reimplemented here.
func BuildDomainBus(pool *pgxpool.Pool, pgCfg platformpg.Config, logger *slog.Logger) eventbus.Bus {
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
	// C-defect-B (2026-08-04): this is the DURABLE bus (Pub/Sub domain-event-consumer / kernel
	// worker), the path that actually delivers a rework/approved push in production and in the
	// E2E stack -- unlike internal/bootstrap/api.go's in-process bus, which is dev/local-only.
	// Neither WithLocationNames nor WithVaccineLabels was ever chained here, so every push this
	// bus produced degraded straight to the generic "The proof is ready for operational closure"/
	// no-park copy, even after the enrichment itself was written (see verification_notify_consumer.go
	// enrichApprovedNotificationCopy and handleVerdictRework's park-name prefix). Confirmed live: a
	// real rework push carried no park name until this wiring was added.
	verificationVaccineLabels := notificationbridge.NewVaccineLabelResolver(pool, logger)
	verificationLocationNames := notificationbridge.NewLocationNameResolver(pool)
	notificationbridge.NewVerificationEventConsumer(rosterService, calendarService, logger).WithVaccineLabels(verificationVaccineLabels).WithLocationNames(verificationLocationNames).Register(bus)
	notificationbridge.NewWeighingSubmissionEventConsumer(rosterService, calendarService, logger).Register(bus)
	notificationbridge.NewWeighingLifecycleEventConsumer(rosterService, calendarService, logger).Register(bus)
	// A missed obligation must reach people, not just open an escalation row: DOWN to the assigned
	// operator, UP to the park head and the owning module's director.
	calendarapp.NewObligationMissedHandler(calendarService).WithNotifier(notificationbridge.NewObligationMissedNotifier(calendarService, rosterService, calendarService, logger)).Register(bus)
	countsapp.NewProjectionInputHandler(countsService).Register(bus)
	// Keep every durable handler explicit in this production bus builder. The cascade-event-wiring
	// guard compares this list with cmd/domain-event-consumer so a wrapper cannot hide bus drift.
	countsapp.NewShiftingVerificationHandler(countsApprovalRepo, nil).Register(bus)
	countsapp.NewMilkPreparationVerificationHandler(countsMilkPreparationRepo).Register(bus)
	countsapp.NewMilkFeedingVerificationHandler(countsMilkPreparationRepo).Register(bus)
	feeddirectionapp.NewFeedDistributionVerificationHandler(feedDirectionRepo, logger).Register(bus)
	feeddirectionapp.NewFeedPackingVerificationHandler(feedDirectionRepo, logger).Register(bus)
	feeddirectionapp.NewFeedTransportVerificationHandler(feedDirectionRepo, logger).Register(bus)
	feeddirectionapp.NewFeedWastageVerificationHandler(feedDirectionRepo, logger).Register(bus)
	// Weighing verdict applier: weighing enqueues a verification item for every
	// observation, so without this consumer every approve/reject is a silent drop.
	weighingapp.NewVerificationVerdictHandler(weighingRepo, logger).Register(bus)
	// PC Care verdict applier: pc_care enqueues a verification item per completed care
	// task, so without this consumer every approve/reject is a silent drop.
	pccareapp.NewPCCareVerificationHandler(pccarepg.NewRepository(pool, pgCfg.QueryTimeout), logger).Register(bus)
	verificationService := verificationapp.NewService(verificationpg.NewRepository(pool, pgCfg.QueryTimeout), nil)
	if err := pccareverificationbridge.RegisterCategories(verificationService); err != nil {
		panic(fmt.Sprintf("register pc care verification categories: %v", err))
	}
	pccareapp.NewPCCarePendingVerificationHandler(pccareverificationbridge.New(verificationService), logger).Register(bus)
	tasksapp.NewCountsDeathReportedHandler(workflowService).Register(bus)
	tasksapp.NewCountsDeathRejectedHandler(workflowService).Register(bus)
	tasksapp.NewGoatCreatedWorkflowHandler(workflowService).Register(bus)
	tasksapp.NewGoatExitedWorkflowHandler(workflowService).Register(bus)
	tasksapp.NewIdentifierAddedWorkflowHandler(workflowService).Register(bus)
	tasksapp.NewDeathVerificationHandler(workflowService, nil).Register(bus)
	tasksapp.NewBirthVerificationHandler(workflowService, nil).Register(bus)
	healthapp.NewDeathLifecycleHandler(healthRepo).Register(bus)

	if logger != nil {
		logger.Info("kernelstages_domain_event_handlers_registered")
	}
	return bus
}

// NewEnvelopeValidator locates the domain-event envelope JSON schema and builds
// the shared validator used by the outbox relay and the domain consumer.
func NewEnvelopeValidator() (*outboxapp.EnvelopeValidator, error) {
	schemaPath, err := findDomainEventEnvelopeSchema()
	if err != nil {
		return nil, err
	}
	return outboxapp.NewEnvelopeValidator(schemaPath)
}

func findDomainEventEnvelopeSchema() (string, error) {
	if configured := getenv("GOATOS_DOMAIN_EVENT_SCHEMA_PATH"); configured != "" {
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

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
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	notificationbridge "github.com/vgoats/goatos/backend/internal/notificationbridge"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
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
	vaccinationBooster := vaccinationapp.NewBoosterService(protocolRepo, obligationRepo).WithGoatReader(vaccinationRepo).WithCrossVaccineGapReader(vaccinationRepo)
	vaccinationGeneration := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)
	workforceRepo := workforcepg.NewRepository(pool, pgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	obligationapp.NewGoatShiftedHandler(obligationRepo).Register(bus)
	obligationapp.NewGoatExitedHandler(obligationRepo).Register(bus)
	vaccinationapp.NewGoatCreatedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewGoatRecheckHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewProtocolPublishedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewManualCampaignHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).Register(bus)
	vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
	notificationbridge.NewVerificationEventConsumer(rosterService, calendarService, logger).Register(bus)
	calendarapp.NewObligationMissedHandler(calendarService).Register(bus)
	countsapp.NewProjectionInputHandler(countsService).Register(bus)

	// Bounded incremental vaccination-shed projector: dirty the affected shed(s)
	// instead of waiting for the full-tenant scheduled rebuild.
	vaccExecRepo := vaccexecpg.NewRepository(pool, pgCfg.QueryTimeout)
	vaccexecapp.NewGoatShiftedDirtyShedHandler(vaccExecRepo).Register(bus)
	vaccexecapp.NewGoatExitedDirtyShedHandler(vaccExecRepo).Register(bus)
	vaccexecapp.NewVaccinationCompletedDirtyShedHandler(vaccExecRepo, vaccExecRepo).Register(bus)

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

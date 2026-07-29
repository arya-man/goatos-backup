package wiring

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	eventwiring "github.com/vgoats/goatos/backend/internal/eventwiring"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// BuildDomainBus builds the production in-process domain event bus and registers
// all handlers used by identity, vaccination, obligation, and projection writers.
func BuildDomainBus(pool *pgxpool.Pool, queryTimeout time.Duration, logger *slog.Logger) eventbus.Bus {
	if queryTimeout <= 0 {
		queryTimeout = 5 * time.Second
	}

	bus := eventbus.NewInProcessBus()
	protocolRepo := protocolpg.NewRepository(pool, queryTimeout)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, queryTimeout))
	countsService := countsapp.NewService(countspg.NewRepository(pool, queryTimeout))
	vaccinationRepo := vaccinationpg.NewRepository(pool, queryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, queryTimeout)
	inventoryService := inventoryapp.NewService(inventorypg.NewRepository(pool, queryTimeout))
	vaccinationService := vaccinationapp.NewService(vaccinationRepo)
	vaccinationCompletion := vaccinationapp.NewCompletionService(vaccinationService, obligationRepo, inventoryService)
	sopService := sopapp.NewService(soppg.NewRepository(pool, queryTimeout))
	vaccinationBooster := vaccinationapp.NewBoosterService(protocolRepo, obligationRepo).WithGoatReader(vaccinationRepo).WithCrossVaccineGapReader(vaccinationRepo)
	vaccinationGeneration := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)
	workforceRepo := workforcepg.NewRepository(pool, queryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
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
	calendarapp.NewObligationMissedHandler(calendarService).Register(bus)
	countsapp.NewProjectionInputHandler(countsService).Register(bus)
	// Birth/death workflow consumers: the ONE shared registration (internal/eventwiring), same set on
	// every bus so approved births/deaths always open their follow-up work.
	eventwiring.RegisterWorkflowConsumers(bus,
		eventwiring.NewWorkflowConsumerService(pool, queryTimeout, logger), logger)

	if logger != nil {
		logger.Info("domain_event_handlers_registered")
	}
	return bus
}

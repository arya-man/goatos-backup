package wiring

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
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
	obligationapp.NewGoatShiftedHandler(obligationRepo).Register(bus)
	obligationapp.NewGoatExitedHandler(obligationRepo).Register(bus)
	vaccinationapp.NewGoatCreatedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewGoatRecheckHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewProtocolPublishedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).WithClosureProjector(sopService).Register(bus)
	vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
	calendarapp.NewObligationMissedHandler(calendarService).Register(bus)
	countsapp.NewProjectionInputHandler(countsService).Register(bus)

	if logger != nil {
		logger.Info("domain_event_handlers_registered")
	}
	return bus
}

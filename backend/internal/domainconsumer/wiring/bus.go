package wiring

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/browserpush"
	browserpushpg "github.com/vgoats/goatos/backend/internal/browserpush/adapters/postgres"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
	penroutinespg "github.com/vgoats/goatos/backend/internal/penroutines/adapters/postgres"
	penvisitspg "github.com/vgoats/goatos/backend/internal/penvisits/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	toxinpg "github.com/vgoats/goatos/backend/internal/toxin/adapters/postgres"
	toxinapp "github.com/vgoats/goatos/backend/internal/toxin/app"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	weighingverificationbridge "github.com/vgoats/goatos/backend/internal/weighing/adapters/verificationbridge"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// BuildDomainBus builds the production in-process domain event bus and registers
// all handlers used by identity, vaccination, obligation, and projection writers.
func BuildDomainBus(pool *pgxpool.Pool, queryTimeout time.Duration, logger *slog.Logger) eventbus.Bus {
	return buildDomainBusOn(eventbus.NewInProcessBus(), pool, queryTimeout, logger, verificationStores{})
}

// verificationStores lets a test substitute the verdict-applier stores so the builder's real
// registration + dispatch path can be exercised without a database. Production passes the zero
// value, which builds the real Postgres repositories from `pool`.
type verificationStores struct {
	feed     eventwiring.FeedCompletionStore
	shifting eventwiring.ShiftingVerificationRepo
	// milkPreparation applies milk-preparation verdicts. It is registered on the SAME bus as
	// the other appliers: a consumer that applies feed/shifting/weighing verdicts but not this
	// one would silently drop every milk-preparation verdict it received.
	milkPreparation eventwiring.MilkVerdictStore
	// penReconciliation raises wrong-pen cards from weighing submits and applies their
	// verdicts (counts/pen_reconciliation_card). Same drift argument as every store here.
	penReconciliation eventwiring.PenReconciliationStore
	weighing          eventwiring.WeighingVerdictStore
	// weighingAck is the apply-RECEIPT seam. Injectable for the same reason the stores are:
	// the pool-less dispatch test must be able to exercise the real registration without a
	// database, and a Postgres repository built on a nil pool panics the moment it is used.
	weighingAck weighingapp.VerificationApplyAcker
	// pcCare applies PC Care task verdicts (pc_care/pc_care_task).
	pcCare eventwiring.PCCareVerdictStore
	// health applies treatment-session verdicts (health/health_treatment_session).
	health eventwiring.HealthVerdictStore
	// penVisits applies pen-visit verdicts (pen_visits/pen_visit_task); penVisitCloser closes
	// the PC Care tasks an approved visit was the last step of (maintainer decision 2026-09-12).
	penVisits      eventwiring.PenVisitVerdictStore
	penVisitCloser eventwiring.PenVisitParentCloser
	// penRoutines applies routine-check verdicts (pen_routines/pen_routine_task).
	penRoutines eventwiring.PenRoutineVerdictStore
}

// buildDomainBusOn is BuildDomainBus with the bus (and the verdict-applier stores) injected.
func buildDomainBusOn(bus eventbus.Bus, pool *pgxpool.Pool, queryTimeout time.Duration, logger *slog.Logger, stores verificationStores) eventbus.Bus {
	if queryTimeout <= 0 {
		queryTimeout = 5 * time.Second
	}

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
	// Browsers beside phones (bootstrap/api.go, kernelstages): the pushes this bus delivers are
	// exactly the async ones -- a comment, a mention, a status -- that must reach Chrome too.
	// The pool-less dispatch test hands a nil pool; a Postgres browser repository on a nil pool
	// panics when used, so that test keeps the bare roster.
	var browsers interface {
		ResolveBrowserRecipients(context.Context, string, string) ([]browserpush.Recipient, error)
		ResolveModuleDutyBrowserRecipients(context.Context, string, string, string, string, string, time.Time) ([]browserpush.Recipient, error)
		ResolvePositionBrowserRecipients(context.Context, string, string, string, string, time.Time) ([]browserpush.Recipient, error)
	} = notificationbridge.NoBrowsers{}
	if pool != nil {
		browsers = browserpush.NewService(browserpushpg.NewRepository(pool, queryTimeout))
	}
	notifyRecipients := notificationbridge.WithBrowserRecipients(rosterService, browsers, logger)
	healthRepo := healthpg.NewRepository(pool, queryTimeout)
	obligationapp.NewGoatShiftedHandler(obligationRepo).Register(bus)
	obligationapp.NewGoatExitedHandler(obligationRepo).Register(bus)
	obligationapp.NewOperatorConfigReplanHandler(obligationRepo).Register(bus)
	vaccinationapp.NewGoatCreatedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewGoatRecheckHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewProtocolPublishedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).WithClosureProjector(sopService).Register(bus)
	vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
	// C-defect-B (2026-08-04): mirrors the identical fix in internal/kernelstages/bus.go -- this
	// consumer was never given a location/vaccine-label resolver, so a rework/approved push
	// produced through cmd/domain-event-consumer's bus degraded to generic no-park copy.
	// WHO hears each leadership push is per-designation config (maintainer decision 2026-09-08).
	leadershipAudience := notificationbridge.NewStoredAudience(notifyRecipients, notificationaudiencepg.NewRepository(pool, queryTimeout), logger)
	notificationbridge.NewVerificationEventConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).WithVaccineLabels(notificationbridge.NewVaccineLabelResolver(pool, logger)).WithLocationNames(notificationbridge.NewLocationNameResolver(pool)).Register(bus)
	notificationbridge.NewWeighingSubmissionEventConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).Register(bus)
	notificationbridge.NewWeighingLifecycleEventConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).Register(bus)
	// The afternoon feed correction's packing reopen: DOWNWARD push to the packer whose bag was
	// taken back, carrying the old-vs-new quantities (feed.packing.reopened; maintainer decision
	// 2026-08-29). Registered here as well as in kernelstages/bus.go, cmd/domain-event-consumer and
	// cmd/outbox-relay -- this builder is also the bus the kernel E2E fixture relays through, so a
	// consumer missing here is invisible to the story suite.
	notificationbridge.NewFeedPackingReopenNotifyConsumer(notifyRecipients, calendarService, logger).Register(bus)
	// Leave requests (maintainer decision 2026-09-10): raise -> the park head + HR who must sign it;
	// final approve/reject -> the requester. Registered on every bus builder (cascade-event-wiring guard).
	notificationbridge.NewLeaveRequestNotifyConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).Register(bus)
	notificationbridge.NewAnimalPurchaseNotifyConsumer(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).Register(bus)
	// Sale -> Feed Director notice (maintainer decision 2026-09-07): goat.sale_allocated, emitted once
	// per confirm with the pen-by-pen breakdown, pushes the pens and the feed day to reduce from.
	notificationbridge.NewSaleFeedReduceNotifier(notifyRecipients, calendarService, logger).WithAudience(leadershipAudience).WithFeedClocks(feeddirectionpg.NewRepository(pool, queryTimeout)).Register(bus)
	// Toxin (maintainer decision 2026-08-25): a feed load REACHING the farm owes an aflatoxin
	// strip test, minted from procurement.feed_purchase.reached. Registered on the three bus
	// builders that carry it in production (kernelstages, cmd/domain-event-consumer,
	// cmd/outbox-relay) and MISSING here until now -- which, by this file's own rule above, made
	// the chain invisible to the story suite: no E2E could see a reached load fail to owe its
	// test, because the consumer that mints it was not on the bus the fixture relays through.
	toxinapp.NewFeedPurchaseReachedHandler(toxinpg.NewRepository(pool, queryTimeout), logger).Register(bus)
	// Verifier-verdict appliers: the ONE shared registration (internal/eventwiring), the same call
	// bootstrap/api.go and cmd/outbox-relay make. This builder previously hand-listed consumers and
	// carried ONLY the weighing applier, so every shifting / feed-distribution / feed-packing /
	// feed-transport approval routed through it was a silent no-op. A hand list that drifts is the
	// defect class, so this must stay a call to the shared helper (cascade-event-wiring guard rule 5).
	if stores.feed == nil {
		stores.feed = feeddirectionpg.NewRepository(pool, queryTimeout)
	}
	if stores.shifting == nil {
		stores.shifting = countspg.NewRepository(pool, queryTimeout).WithIdentityTxWriter(identitypg.NewRepository(pool, queryTimeout))
	}
	if stores.milkPreparation == nil {
		stores.milkPreparation = countspg.NewRepository(pool, queryTimeout)
	}
	if stores.penReconciliation == nil && pool != nil {
		stores.penReconciliation = countspg.NewRepository(pool, queryTimeout)
	}
	if stores.weighing == nil {
		stores.weighing = weighingpg.NewRepository(pool, queryTimeout)
	}
	// Weighing's apply-receipt seam: this consumer is one of the two processes where the weighing
	// verdict applier actually runs, so it must also tell verification the verdict landed --
	// otherwise every verdict it applies stays reading as "decided, not yet in effect" forever.
	if stores.weighingAck == nil && pool != nil {
		stores.weighingAck = weighingverificationbridge.New(verificationpg.NewRepository(pool, queryTimeout))
	}
	if stores.pcCare == nil {
		pcCareRepo := pccarepg.NewRepository(pool, queryTimeout)
		stores.pcCare = pcCareRepo
		if stores.penVisitCloser == nil {
			stores.penVisitCloser = pcCareRepo
		}
	}
	if stores.health == nil {
		stores.health = healthpg.NewRepository(pool, queryTimeout)
	}
	if stores.penVisits == nil {
		stores.penVisits = penvisitspg.NewRepository(pool, queryTimeout)
	}
	if stores.penRoutines == nil {
		stores.penRoutines = penroutinespg.NewRepository(pool, queryTimeout)
	}
	eventwiring.RegisterVerificationAppliers(bus, stores.feed, stores.shifting, stores.penReconciliation, stores.milkPreparation, stores.weighing, stores.weighingAck, stores.pcCare, stores.health, stores.penVisits, stores.penVisitCloser, stores.penRoutines, logger)
	if stores.penReconciliation != nil {
		countsapp.NewPenReconciliationRaiser(stores.penReconciliation, logger, nil).Register(bus)
		countsapp.NewPenReconciliationVerificationHandler(stores.penReconciliation, nil).Register(bus)
	}
	calendarapp.NewObligationMissedHandler(calendarService).Register(bus)
	countsapp.NewProjectionInputHandler(countsService).Register(bus)
	// Birth/death workflow consumers: the ONE shared registration (internal/eventwiring), same set on
	// every bus so approved births/deaths always open their follow-up work.
	consumerWorkflowService := eventwiring.NewWorkflowConsumerService(pool, queryTimeout, logger)
	eventwiring.RegisterWorkflowConsumers(bus, consumerWorkflowService, logger)
	captureEnqueuer, captureReviews := eventwiring.NewCountsCaptureStores(pool, queryTimeout)
	eventwiring.RegisterCountsCaptureConsumers(bus, captureEnqueuer, captureReviews, consumerWorkflowService)
	healthapp.NewDeathLifecycleHandler(healthRepo).Register(bus)

	if logger != nil {
		logger.Info("domain_event_handlers_registered")
	}
	return bus
}

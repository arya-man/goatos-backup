// Package eventwiring holds the SINGLE registration of the cross-module verifier-verdict appliers, so
// every process that owns a domain-event bus registers the exact same set and they cannot drift.
//
// Why this package exists: the shifting + feed-distribution + feed-packing verification handlers were
// registered on the API's in-process bus (bootstrap/api.go) but NOT on the two processes that actually
// consume the durable outbox — cmd/outbox-relay (local eventbus) and cmd/domain-event-consumer (prod
// Pub/Sub). A verifier's verdict is delivered ONLY through the outbox, so the API-bus registration
// never fired and every feed/shifting approval was silently stranded in pending_verification. Three
// hand-maintained registration lists drifted; this collapses them to one.
package eventwiring

import (
	"context"
	"log/slog"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	countsports "github.com/vgoats/goatos/backend/internal/counts/ports"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	pccareports "github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	weighingports "github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// ShiftingVerificationRepo is the counts slice the shifting applier drives. Exported so the shared
// wiring (and its test) can supply it; *countspg.Repository (with WithIdentityTxWriter) satisfies it.
type ShiftingVerificationRepo interface {
	ApplyVerifiedShiftingEvent(ctx context.Context, in countsdomain.ShiftingVerifiedApplyCommand) (countsdomain.ShiftingExecutionResult, bool, error)
	BounceShiftingEventForRework(ctx context.Context, in countsdomain.ShiftingReworkCommand) error
}

// PenReconciliationStore is the counts slice the pen-reconciliation consumers drive
// (maintainer decision 2026-09-02). Satisfied by *countspg.Repository. It carries BOTH halves
// of the flow — the raiser that turns a durable weighing.shed_submission.completed event into
// wrong-pen cards, and the verdict applier that completes/reworks a submitted card — because
// both are delivered ONLY through the durable outbox on deployed processes, so a registration
// missing on one bus would silently raise no cards or strand every card in
// pending_verification: the exact incident this package exists to prevent.
type PenReconciliationStore interface {
	RaisePenReconciliationCards(ctx context.Context, in countsdomain.PenReconciliationRaiseCommand) (int, error)
	ApplyVerifiedPenReconciliation(ctx context.Context, in countsdomain.PenReconciliationVerdictCommand) error
	BouncePenReconciliationForRework(ctx context.Context, in countsdomain.PenReconciliationVerdictCommand) error
}

// FeedCompletionStore is satisfied by *feeddirectionpg.Repository (it owns both the distribution and
// packing completion tables, so it is both stores at once).
type FeedCompletionStore interface {
	feeddirectionports.DistributionCompletionStore
	feeddirectionports.PackingCompletionStore
	feeddirectionports.TransportStore
	feeddirectionports.WastageCompletionStore
}

// WeighingVerdictStore is satisfied by *weighingpg.Repository. Weighing enqueued a verification item
// for every observation but had NO verdict consumer, so every approve/reject was a silent drop; the
// applier registered here is that missing half.
type WeighingVerdictStore interface {
	weighingports.VerificationVerdictStore
}

// PCCareVerdictStore is satisfied by *pccarepg.Repository — the pc_care_tasks verdict half
// (ApplyVerifiedTask / BounceTaskForRework).
type PCCareVerdictStore interface {
	ApplyVerifiedTask(ctx context.Context, p pccareports.ApplyVerifiedTaskParams) (bool, error)
	BounceTaskForRework(ctx context.Context, p pccareports.BounceTaskParams) (bool, error)
}

// HealthVerdictStore is satisfied by *healthpg.Repository — the treatment-session verdict half
// (approve stamps verified_by/verified_at, reject flips the session to rework).
type HealthVerdictStore = healthapp.TreatmentVerdictStore

// RegisterVerificationAppliers subscribes the shifting, feed-distribution, and feed-packing appliers to
// the generic verification verdict events on `bus`. Each handler filters strictly on
// source.module + source.ref_type (counts/shifting_event, feed/feed_distribution_completion,
// feed/feed_packing_completion), so cross-fire is impossible. This is the ONE place these three are
// registered; bootstrap/api.go, cmd/outbox-relay, and cmd/domain-event-consumer all call it.
func RegisterVerificationAppliers(
	bus eventbus.Bus,
	feed FeedCompletionStore,
	shifting ShiftingVerificationRepo,
	penReconciliation PenReconciliationStore,
	milkPreparation countsports.MilkPreparationCompletionStore,
	weighing WeighingVerdictStore,
	weighingAck weighingapp.VerificationApplyAcker,
	pcCare PCCareVerdictStore,
	health HealthVerdictStore,
	log *slog.Logger,
) {
	countsapp.NewShiftingVerificationHandler(shifting, nil).Register(bus)
	// Pen reconciliation (maintainer decision 2026-09-02). The RAISER consumes the durable
	// weighing.shed_submission.completed event and inserts one card per scanned animal whose
	// registered pen disagrees with the pen it was weighed in (set-based, idempotent SQL, so a
	// duplicate delivery inserts nothing). The APPLIER is filtered to
	// counts/pen_reconciliation_card: approve completes the card, reject sends it to rework;
	// the herd register is never touched by either. penReconciliation may be nil on a bus
	// built without a counts store; both consumers then no-op.
	if penReconciliation != nil {
		countsapp.NewPenReconciliationRaiser(penReconciliation, log, nil).Register(bus)
		countsapp.NewPenReconciliationVerificationHandler(penReconciliation, nil).Register(bus)
	}
	// Milk preparation carries NO feed-stock fan-out, and that absence is deliberate (maintainer
	// decision 2026-08-27, migration 000216): UHT stock depletes from the preparation itself, on
	// SUBMIT, through the feed_effective_external_consumption view, which reads
	// milk_preparation_completions directly. The retired 2026-08-22 seam wrote a SECOND row into
	// feed_external_consumption on APPROVE keyed at preparation_date while the view books the same
	// milk at feeding_date (= preparation_date + 1, DB CHECK), so the ledger row was never
	// suppressed by its own preparation and the litres were deducted twice on any day whose
	// PREVIOUS day carried no preparation. Do not reattach a recorder here; the workflow already
	// owns the fact, and a second writer can only disagree with it.
	countsapp.NewMilkPreparationVerificationHandler(milkPreparation).Register(bus)
	feeddirectionapp.NewFeedDistributionVerificationHandler(feed, log).Register(bus)
	feeddirectionapp.NewFeedPackingVerificationHandler(feed, log).Register(bus)
	feeddirectionapp.NewFeedTransportVerificationHandler(feed, log).Register(bus)
	// Feed WASTAGE (maintainer decision 2026-08-18): the fourth feed gate's applier, filtered to
	// feed/feed_wastage_completion. Registered HERE, in the one shared list, so the API bus, the
	// outbox relay, and the Pub/Sub consumer cannot drift apart — the exact incident this package
	// exists to prevent.
	feeddirectionapp.NewFeedWastageVerificationHandler(feed, log).Register(bus)
	// PC Care (maintainer decision 2026-08-21): the pc_care module's applier, filtered to
	// pc_care/pc_care_task. Registered HERE, in the one shared list, so the API bus, the outbox
	// relay, and the Pub/Sub consumer cannot drift apart.
	pccareapp.NewPCCareVerificationHandler(pcCare, log).Register(bus)
	// Health (2026-08-29): the treatment-session applier, filtered to
	// health/health_treatment_session. Post-task evidence review only — approve stamps
	// verified_by/verified_at, reject flips the session to 'rework'; nothing rolls back a
	// treatment already given. Registered HERE, in the one shared list, so the API bus, the
	// outbox relay, and the Pub/Sub consumer cannot drift apart.
	healthapp.NewHealthVerificationHandler(health, log).Register(bus)
	// weighingAck is the receipt weighing sends verification once a verdict has landed on the
	// observation, so a decided item stops reading as still-being-applied. It may be nil (a bus
	// built without a verification repo still applies verdicts exactly as before -- the ack is
	// visibility, never a correctness gate).
	weighingapp.NewVerificationVerdictHandler(weighing, log).WithApplyAcker(weighingAck).Register(bus)
}

package eventwiring

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/countsbridge"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	proofpg "github.com/vgoats/goatos/backend/internal/proof/adapters/postgres"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	tasksproofkinds "github.com/vgoats/goatos/backend/internal/tasks/adapters/proofkinds"
	tasksverificationbridge "github.com/vgoats/goatos/backend/internal/tasks/adapters/verificationbridge"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
)

// NewWorkflowConsumerService builds the durable-bus tasks service with a real verification
// enqueue seam. The bridge hardcodes the registered birth/death evidence contracts, so the trusted
// internal producer can call the verification repository directly; queue reads still use the
// verification app service/category registry in the API process.
func NewWorkflowConsumerService(pool *pgxpool.Pool, timeout time.Duration, log *slog.Logger) *tasksapp.Service {
	verificationRepo := verificationpg.NewRepository(pool, timeout)
	// Same identity seam bootstrap/api.go wires, so a Record shed step completed against a durable
	// consumer process places the kid exactly as one completed against the API does. A repository
	// missing it would accept the answer and silently leave the animal in the wrong pen.
	workflowRepo := taskspg.NewRepository(pool, timeout).
		WithIdentityTxWriter(identitypg.NewRepository(pool, timeout))
	return tasksapp.NewService(workflowRepo, log).
		WithVerificationEnqueuer(tasksverificationbridge.New(verificationRepo)).
		// A recorded birth report re-shoot swaps the proof on the approval row and re-queues it.
		WithCaptureReshootListener(countsapp.NewBirthCaptureReshootService(countspg.NewRepository(pool, timeout),
			countsbridge.NewBirthCaptureVerificationEnqueuer(verificationRepo))).
		WithProofKindResolver(tasksproofkinds.New(proofpg.NewRepository(pool, timeout)))
}

// RegisterWorkflowConsumers subscribes the birth/death workflow-engine consumers
// (docs/decisions/birth-death-workflows.md) to `bus`. Mirrors RegisterVerificationAppliers: this is
// the ONE place these handlers are registered; bootstrap/api.go, internal/domainconsumer/wiring,
// internal/kernelstages, cmd/outbox-relay, and cmd/domain-event-consumer all call it, so the
// durable-bus consumers can never drift from the API's in-process bus.
//
//	counts.death.reported                 -> open the staged death-video workflow
//	counts.death.rejected                 -> cancel staged work; goat remains alive
//	goat.created (origin_type=birth)      -> open birth_kid (+ shared birth_mother) workflows
//	goat.exited (exit_reason=died)        -> release approved death evidence to Verify
//	RFID promotion stays identity-owned; Tag the kid completes only with its task video
//	verification.verdict.approved/.rework  -> apply birth/death evidence verdicts
//
// The verdict handlers filter strictly on source.module=counts and distinct birth/death ref types,
// so they cannot cross-fire with the shifting applier that also uses module=counts.
func RegisterWorkflowConsumers(bus eventbus.Bus, svc *tasksapp.Service, log *slog.Logger) {
	_ = log
	tasksapp.NewCountsDeathReportedHandler(svc).Register(bus)
	tasksapp.NewCountsDeathRejectedHandler(svc).Register(bus)
	tasksapp.NewGoatCreatedWorkflowHandler(svc).Register(bus)
	tasksapp.NewGoatExitedWorkflowHandler(svc).Register(bus)
	tasksapp.NewIdentifierAddedWorkflowHandler(svc).Register(bus)
	tasksapp.NewDeathVerificationHandler(svc, nil).Register(bus)
	tasksapp.NewBirthVerificationHandler(svc, nil).Register(bus)
	// SOP questionnaires on a non-goat subject (reconcile card today, shifting event next): a
	// verifier rework reopens the workflow's proof steps (docs/decisions/sop-driven-herd-operations.md).
	tasksapp.NewSubjectWorkflowVerdictHandler(svc).Register(bus)
	// SALES SOP (2026-09-19): a recorded sale opens its workflow; the tagging confirm completes
	// the sale_tag_animals step (docs/decisions/sales-sop.md).
	tasksapp.NewSaleRecordedWorkflowHandler(svc).Register(bus)
	tasksapp.NewSaleAllocatedWorkflowHandler(svc).Register(bus)
}

// CaptureReviewStore is the counts repository slice the birth_capture verdict consumer drives.
type CaptureReviewStore interface {
	SetCaptureSlotReview(ctx context.Context, tenantID, approvalRequestID, slotKey, ref, status, reason string) (countsdomain.ApprovalRequest, bool, error)
}

// RegisterCountsCaptureConsumers subscribes the SOP capture card's birth-report consumers
// (docs/decisions/sop-driven-herd-operations.md → "Phase 2: capture forms"). The ONE place they
// are registered; bootstrap/api.go, cmd/outbox-relay, cmd/domain-event-consumer,
// internal/kernelstages and internal/domainconsumer/wiring all call it.
//
//	counts.birth.reported                  -> one birth_evidence item per form proof slot (ref_type birth_capture)
//	verification.verdict.approved/.rework  -> that slot's review (+ rollup) on the approval row
//
// engine (the tasks service) appends the re-shoot steps a rejection asks for (decision 5).
func RegisterCountsCaptureConsumers(bus eventbus.Bus, enqueuer countsapp.BirthCaptureVerificationEnqueuer, store CaptureReviewStore, engine countsapp.CaptureReshootEngine) {
	countsapp.NewBirthReportedVerificationHandler(enqueuer, nil).Register(bus)
	countsapp.NewBirthCaptureVerdictHandler(store, nil).WithReshootEngine(engine).Register(bus)
}

// NewCountsCaptureStores builds the durable-bus seams for RegisterCountsCaptureConsumers from a
// pool (the verification repository is the trusted internal producer, as for the workflow
// consumers above).
func NewCountsCaptureStores(pool *pgxpool.Pool, timeout time.Duration) (countsapp.BirthCaptureVerificationEnqueuer, CaptureReviewStore) {
	return countsbridge.NewBirthCaptureVerificationEnqueuer(verificationpg.NewRepository(pool, timeout)),
		countspg.NewRepository(pool, timeout)
}

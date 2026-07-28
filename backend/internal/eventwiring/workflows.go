package eventwiring

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
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
	return tasksapp.NewService(taskspg.NewRepository(pool, timeout), log).
		WithVerificationEnqueuer(tasksverificationbridge.New(verificationRepo))
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
}

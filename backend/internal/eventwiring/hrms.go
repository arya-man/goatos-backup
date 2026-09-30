package eventwiring

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	hrmssoppg "github.com/vgoats/goatos/backend/internal/hrmssop/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// RegisterHRMSConsumers subscribes the HRMS consumers (maintainer decisions 2026-09-30). The ONE
// place they are registered; bootstrap/api.go, cmd/outbox-relay, cmd/domain-event-consumer,
// internal/kernelstages and internal/domainconsumer/wiring all call it beside
// RegisterWorkflowConsumers.
//
//	goat.exited (exit_reason=died) -> open the death enquiry for that park's park head
func RegisterHRMSConsumers(bus eventbus.Bus, pool *pgxpool.Pool, timeout time.Duration, log *slog.Logger) {
	repo := workforcepg.NewRepository(pool, timeout)
	svc := workforceapp.NewDisciplineService(repo, repo, hrmssoppg.NewSource(pool, timeout), log)
	workforceapp.NewDeathEnquiryHandler(svc).Register(bus)
}

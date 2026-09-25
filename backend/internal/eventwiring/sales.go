package eventwiring

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// RegisterSaleReleaseConsumers subscribes the herd side of a FAILED SALE (maintainer decision
// 2026-09-25, docs/decisions/sales-sop.md -> "A failed sale"):
//
//	sales.deal.status_changed (Deal Failed) -> release every animal tagged to the deal back into
//	                                           the herd, in the pen it was sold from
//
// The ONE place it is registered; bootstrap/api.go, internal/kernelstages, internal/domainconsumer/
// wiring, cmd/outbox-relay and cmd/domain-event-consumer all call it, beside
// RegisterWorkflowConsumers (which cancels the same deal's workflow), so no bus process can drop it.
func RegisterSaleReleaseConsumers(bus eventbus.Bus, pool *pgxpool.Pool, timeout time.Duration) {
	identityapp.NewSaleFailedReleaseHandler(identitypg.NewRepository(pool, timeout)).Register(bus)
}

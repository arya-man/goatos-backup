package kernelstages

import (
	"context"
	"fmt"
	"log/slog"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// SaleFeedReduceReminderStage sends the Feed Director the feed-day reminder after a sale
// (maintainer decision 2026-09-07): "did feed reduce for these pens?".
//
// It rides the SHARED operational cadence rather than owning a schedule, which the task-kernel lock
// requires: no module may keep a private scheduler. "Once per batch, on its feed day, at a waking
// hour" comes from the notifier's gate plus its per-confirm idempotency key -- the first tick past
// 07:00 IST on the feed day writes, every later tick writes nothing -- so the property holds without
// state of its own and survives a worker restart, a redeploy, or the HA pair running both instances.
// Same shape as FeedLowStockStage. The confirm-time NOTICE is not here: it is the goat.sale_allocated
// consumer registered on the domain bus.
type SaleFeedReduceReminderStage struct {
	notifier *notificationbridge.SaleFeedReduceNotifier
	tenantID string
}

func NewSaleFeedReduceReminderStage(deps Deps, tenantID string, logger *slog.Logger) *SaleFeedReduceReminderStage {
	calendarService := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	workforceRepo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	return &SaleFeedReduceReminderStage{
		notifier: notificationbridge.NewSaleFeedReduceNotifier(rosterService, calendarService, logger).
			WithBatches(identitypg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)).
			WithFeedClocks(feeddirectionpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)),
		tenantID: tenantID,
	}
}

func (s *SaleFeedReduceReminderStage) Name() string { return "feed-sale-reduce-reminder" }

func (s *SaleFeedReduceReminderStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("sale feed reduce reminder: tenant id is required")
	}
	return s.notifier.RemindDue(ctx, s.tenantID)
}

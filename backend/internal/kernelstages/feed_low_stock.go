package kernelstages

import (
	"context"
	"fmt"
	"log/slog"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// FeedLowStockStage sends the daily low-stock alert.
//
// It rides the SHARED operational cadence rather than owning a schedule, which the task-kernel lock
// requires: no module may keep a private scheduler. "Once per day" comes from the notifier's
// business-date idempotency key instead -- the first tick of the day writes, every later tick
// writes nothing -- so the property holds without state of its own and survives a worker restart,
// a redeploy mid-day, or the HA pair running both instances.
type FeedLowStockStage struct {
	notifier *notificationbridge.FeedLowStockNotifier
	tenantID string
}

func NewFeedLowStockStage(deps Deps, tenantID string, logger *slog.Logger) *FeedLowStockStage {
	feedRepo := feeddirectionpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	workforceRepo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	return &FeedLowStockStage{
		notifier: notificationbridge.NewFeedLowStockNotifier(feedRepo, rosterService, calendarService, logger).
			WithLocationNames(notificationbridge.NewLocationNameResolver(deps.Pool)),
		tenantID: tenantID,
	}
}

func (s *FeedLowStockStage) Name() string { return "feed-low-stock-alert" }

func (s *FeedLowStockStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("feed low stock alert: tenant id is required")
	}
	return s.notifier.NotifyLowStock(ctx, s.tenantID)
}

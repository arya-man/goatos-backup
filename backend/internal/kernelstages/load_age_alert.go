package kernelstages

import (
	"context"
	"fmt"
	"log/slog"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	procurementpg "github.com/vgoats/goatos/backend/internal/procurement/adapters/postgres"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// LoadAgeAlertStage sends the daily overdue-load alert to the CXO.
//
// It rides the SHARED operational cadence rather than owning a schedule, which the task-kernel
// lock requires: no module may keep a private scheduler. "Once per day" comes from the notifier's
// business-date idempotency key instead -- the first tick of the day writes, every later tick
// writes nothing -- so the property holds without state of its own and survives a worker restart,
// a redeploy mid-day, or the HA pair running both instances. Same shape as FeedLowStockStage.
type LoadAgeAlertStage struct {
	notifier *notificationbridge.LoadAgeNotifier
	tenantID string
}

func NewLoadAgeAlertStage(deps Deps, tenantID string, logger *slog.Logger) *LoadAgeAlertStage {
	loadRepo := procurementpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	workforceRepo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	return &LoadAgeAlertStage{
		notifier: notificationbridge.NewLoadAgeNotifier(loadRepo, rosterService, calendarService, logger),
		tenantID: tenantID,
	}
}

func (s *LoadAgeAlertStage) Name() string { return "procurement-load-age-alert" }

func (s *LoadAgeAlertStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("load age alert: tenant id is required")
	}
	return s.notifier.NotifyOverdueLoads(ctx, s.tenantID)
}

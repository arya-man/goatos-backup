package kernelstages

import (
	"context"
	"fmt"
	"log/slog"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	marketpg "github.com/vgoats/goatos/backend/internal/market/adapters/postgres"
	marketapp "github.com/vgoats/goatos/backend/internal/market/app"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// MarketSurveyStage sends the morning market-calls reminder (maintainer decision 2026-09-14).
//
// It rides the SHARED operational cadence rather than owning a schedule, which the task-kernel
// lock requires. "Once per day, from 08:00 IST" comes from the notifier: it declines before the
// cutoff, and its idempotency key carries the business date, so the first tick after 08:00 writes
// and every later tick that day writes nothing -- surviving a worker restart or the HA pair.
type MarketSurveyStage struct {
	notifier *notificationbridge.MarketSurveyNotifier
	tenantID string
}

func NewMarketSurveyStage(deps Deps, tenantID string, logger *slog.Logger) *MarketSurveyStage {
	repo := marketpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	workforceRepo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	return &MarketSurveyStage{
		notifier: notificationbridge.NewMarketSurveyNotifier(marketapp.NewService(repo), repo, notifyRecipients(deps, rosterService, logger), calendarService, logger),
		tenantID: tenantID,
	}
}

func (s *MarketSurveyStage) Name() string { return "market-survey-reminder" }

func (s *MarketSurveyStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("market survey reminder: tenant id is required")
	}
	return s.notifier.NotifyDue(ctx, s.tenantID)
}

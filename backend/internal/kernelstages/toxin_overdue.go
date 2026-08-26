package kernelstages

import (
	"context"
	"fmt"
	"log/slog"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	toxinpg "github.com/vgoats/goatos/backend/internal/toxin/adapters/postgres"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// ToxinOverdueStage sends the 12-hour toxin start reminder.
//
// Like the low-stock alert, it rides the SHARED operational cadence rather than owning a schedule,
// which the task-kernel lock requires: no module may keep a private scheduler, a private overdue
// calculation or a private reminder ladder. The deadline itself lives in toxin/domain
// (StartDeadline), the same definition the card's Overdue chip reads, so the message and the
// screen can never disagree about what "late" means. "Once per day per task" comes from the
// business-date idempotency key inside the notifier, not from state here.
type ToxinOverdueStage struct {
	notifier *notificationbridge.ToxinOverdueNotifier
	tenantID string
}

func NewToxinOverdueStage(deps Deps, tenantID string, logger *slog.Logger) *ToxinOverdueStage {
	toxinRepo := toxinpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	workforceRepo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	return &ToxinOverdueStage{
		notifier: notificationbridge.NewToxinOverdueNotifier(
			toxinRepo, toxinRepo, rosterService, calendarService, logger),
		tenantID: tenantID,
	}
}

func (s *ToxinOverdueStage) Name() string { return "toxin-start-overdue-reminder" }

func (s *ToxinOverdueStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("toxin overdue reminder: tenant id is required")
	}
	return s.notifier.NotifyOverdue(ctx, s.tenantID)
}

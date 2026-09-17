package kernelstages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	penroutinespg "github.com/vgoats/goatos/backend/internal/penroutines/adapters/postgres"
	penroutineports "github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// PenRoutineKernelStage is the pen-routine cadence inside the consolidated kernel worker
// (maintainer instruction 2026-09-16, docs/decisions/pen-routines.md): the configurable
// recurring pen checks a park head owes.
//
// One tick does three bounded things, in order:
//
//  1. MATERIALIZE every active routine's tasks for each business date in a bounded look-back
//     window (today and the day before by default, so a worker that was down at the day
//     boundary still catches up), oldest first. The write is idempotent on
//     (routine, shed, pen, planned date), so the first tick after 00:00 IST inserts and every
//     later tick inserts nothing -- "once per day" from the natural key, not from a schedule,
//     which the task-kernel lock requires. A task born late is due the later of its planned
//     date and today.
//  2. PUSH one digest per routine whose notify time has passed, naming the routine, the park
//     and the pens still owed today; the business-date event key keeps it to one push a day.
//  3. ROLL FORWARD unfinished checks whose due date has passed to today as 'delayed', so the
//     assignee keeps seeing them with the date they were owed.
//
// Registered on the OPERATIONAL 5-minute lane so day-boundary surfacing lands within minutes
// of the Asia/Kolkata business-day boundary.
type PenRoutineKernelStage struct {
	store     *penroutinespg.Repository
	notifier  *notificationbridge.PenRoutineDueNotifier
	tenantID  string
	lookback  int
	chunkSize int
	maxChunks int
	logger    *slog.Logger
	now       func() time.Time
}

// NewPenRoutineKernelStage builds the stage.
func NewPenRoutineKernelStage(deps Deps, tenantID string, logger *slog.Logger) *PenRoutineKernelStage {
	chunk := intEnv("GOATOS_PEN_ROUTINE_KERNEL_CHUNK_SIZE", 200)
	if chunk < 1 || chunk > 5000 {
		chunk = 200
	}
	maxChunks := intEnv("GOATOS_PEN_ROUTINE_KERNEL_MAX_CHUNKS", 50)
	if maxChunks < 1 || maxChunks > 1000 {
		maxChunks = 50
	}
	lookback := intEnv("GOATOS_PEN_ROUTINE_LOOKBACK_DAYS", 2)
	if lookback < 1 || lookback > 7 {
		lookback = 2
	}
	calendarService := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	workforceRepo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	return &PenRoutineKernelStage{
		store:     penroutinespg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		notifier:  notificationbridge.NewPenRoutineDueNotifier(rosterService, calendarService, logger),
		tenantID:  strings.TrimSpace(tenantID),
		lookback:  lookback,
		chunkSize: chunk,
		maxChunks: maxChunks,
		logger:    logger,
		now:       time.Now,
	}
}

// Name implements worker.StageRunner.
func (s *PenRoutineKernelStage) Name() string { return "pen-routine-kernel" }

// Run performs one bounded tick.
func (s *PenRoutineKernelStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("pen routine kernel: tenant id is required")
	}
	now := s.now()
	today := biztime.BusinessDate(now)
	todayStart := biztime.BusinessDayStart(now)
	// Oldest first, so a catch-up after an outage raises the older day's checks before
	// today's; every day is its own idempotent pass.
	for back := s.lookback - 1; back >= 0; back-- {
		businessDate := todayStart.AddDate(0, 0, -back).Format("2006-01-02")
		// scale-guard:ignore: bounded lookback of at most 7 business days (GOATOS_PEN_ROUTINE_LOOKBACK_DAYS, default 2), one set-based materialize transaction per day; never per row.
		result, err := s.store.Materialize(ctx, s.tenantID, businessDate, today, now)
		if err != nil {
			return err
		}
		s.logResult(ctx, result)
	}
	digests, err := s.store.DueDigests(ctx, s.tenantID, today)
	if err != nil {
		return err
	}
	if len(digests) > 0 {
		if err := s.notifier.NotifyDue(ctx, s.tenantID, digests); err != nil {
			return err
		}
	}
	sweep, err := s.store.SweepRollForward(ctx, s.tenantID, now, s.chunkSize, s.maxChunks)
	if err != nil {
		return err
	}
	if s.logger != nil && (sweep.RolledForward > 0 || sweep.Truncated) {
		s.logger.Info("pen routine kernel tick", "rolled_forward", sweep.RolledForward, "truncated", sweep.Truncated)
	}
	return nil
}

func (s *PenRoutineKernelStage) logResult(ctx context.Context, r penroutineports.MaterializeResult) {
	if s.logger == nil {
		return
	}
	if len(r.RoutinesWithoutAssignee) > 0 {
		// Loud, and no fallback: a routine whose roles NOBODY holds in its park raises nothing
		// until someone is granted one of those roles there (or the routine names another).
		names := make([]string, 0, len(r.RoutinesWithoutAssignee))
		for _, ref := range r.RoutinesWithoutAssignee {
			names = append(names, ref.Name+" ("+ref.RoutineID+") roles="+strings.Join(ref.Roles, ","))
		}
		s.logger.WarnContext(ctx, "pen_routine_without_assignee",
			"tenant_id", s.tenantID, "business_date", r.BusinessDate,
			"routines", names, "pens_skipped", r.PensSkipped)
	}
	if r.Created > 0 || r.Widened > 0 {
		s.logger.InfoContext(ctx, "pen routine materialized",
			"business_date", r.BusinessDate, "created", r.Created, "widened", r.Widened)
	}
}

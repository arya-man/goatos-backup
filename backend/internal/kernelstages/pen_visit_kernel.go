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
	penvisitspg "github.com/vgoats/goatos/backend/internal/penvisits/adapters/postgres"
	penvisitports "github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// PenVisitKernelStage is the pen-visit cadence inside the consolidated kernel worker
// (maintainer decision 2026-09-07): the day after any vaccination or PC Care work is submitted
// in a pen, the park's head owes that pen a visit and one video.
//
// One tick does three bounded things, in order:
//
//  1. MATERIALIZE yesterday's pens (and the day before, so a worker that was down at the day
//     boundary still catches up) into visit tasks assigned to the park's configured head. The
//     write is idempotent on (park, shed, pen, source date), so the first tick after 00:00 IST
//     inserts and every later tick inserts nothing -- "once per day" from the natural key, not
//     from a schedule, which the task-kernel lock requires.
//  2. PUSH one digest per park for the visits THIS tick created, naming the pens and why.
//  3. ROLL FORWARD unvisited pens whose due date has passed to today as 'delayed' (the PC Care
//     kernel shape), so the park head keeps seeing them with the date they were owed.
//
// Registered on the OPERATIONAL 5-minute lane so day-boundary surfacing lands within minutes of
// the Asia/Kolkata business-day boundary.
type PenVisitKernelStage struct {
	store     *penvisitspg.Repository
	notifier  *notificationbridge.PenVisitDueNotifier
	tenantID  string
	lookback  int
	chunkSize int
	maxChunks int
	logger    *slog.Logger
	now       func() time.Time
}

// NewPenVisitKernelStage builds the stage.
func NewPenVisitKernelStage(deps Deps, tenantID string, logger *slog.Logger) *PenVisitKernelStage {
	chunk := intEnv("GOATOS_PEN_VISIT_KERNEL_CHUNK_SIZE", 200)
	if chunk < 1 || chunk > 5000 {
		chunk = 200
	}
	maxChunks := intEnv("GOATOS_PEN_VISIT_KERNEL_MAX_CHUNKS", 50)
	if maxChunks < 1 || maxChunks > 1000 {
		maxChunks = 50
	}
	lookback := intEnv("GOATOS_PEN_VISIT_LOOKBACK_DAYS", 2)
	if lookback < 1 || lookback > 7 {
		lookback = 2
	}
	calendarService := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	workforceRepo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	return &PenVisitKernelStage{
		store:     penvisitspg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		notifier:  notificationbridge.NewPenVisitDueNotifier(rosterService, calendarService, logger),
		tenantID:  strings.TrimSpace(tenantID),
		lookback:  lookback,
		chunkSize: chunk,
		maxChunks: maxChunks,
		logger:    logger,
		now:       time.Now,
	}
}

// Name implements worker.StageRunner.
func (s *PenVisitKernelStage) Name() string { return "pen-visit-kernel" }

// Run performs one bounded tick.
func (s *PenVisitKernelStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("pen visit kernel: tenant id is required")
	}
	now := s.now()
	today := biztime.BusinessDate(now)
	todayStart := biztime.BusinessDayStart(now)
	// Oldest first, so a catch-up after an outage announces the older day's pens before
	// today's; every day is its own idempotent pass.
	for back := s.lookback; back >= 1; back-- {
		sourceDate := todayStart.AddDate(0, 0, -back).Format("2006-01-02")
		// scale-guard:ignore: bounded lookback of at most 7 business days (GOATOS_PEN_VISIT_LOOKBACK_DAYS, default 2), one set-based materialize transaction per day; never per row.
		result, digests, err := s.store.Materialize(ctx, s.tenantID, sourceDate, today, now)
		if err != nil {
			return err
		}
		s.logResult(ctx, result)
		if len(digests) == 0 {
			digests, err = s.store.DueDigestsForSourceDate(ctx, s.tenantID, sourceDate)
			if err != nil {
				return err
			}
		}
		if len(digests) > 0 {
			if err := s.notifier.NotifyCreated(ctx, s.tenantID, digests); err != nil {
				return err
			}
		}
	}
	sweep, err := s.store.SweepRollForward(ctx, s.tenantID, now, s.chunkSize, s.maxChunks)
	if err != nil {
		return err
	}
	if s.logger != nil && (sweep.RolledForward > 0 || sweep.Truncated) {
		s.logger.Info("pen visit kernel tick", "rolled_forward", sweep.RolledForward, "truncated", sweep.Truncated)
	}
	return nil
}

func (s *PenVisitKernelStage) logResult(ctx context.Context, r penvisitports.MaterializeResult) {
	if s.logger == nil {
		return
	}
	if len(r.ParksWithoutAssignee) > 0 {
		// Loud, and no fallback: a park with pen work and no configured head gets no visit
		// tasks until pen_visit_park_assignees names someone (the vaccination operator rule).
		s.logger.WarnContext(ctx, "pen_visit_park_without_assignee",
			"tenant_id", s.tenantID, "source_business_date", r.SourceDate,
			"park_ids", r.ParksWithoutAssignee, "pens_skipped", r.PensSkipped)
	}
	if r.Created > 0 || r.Widened > 0 {
		s.logger.InfoContext(ctx, "pen visit materialized",
			"source_business_date", r.SourceDate, "created", r.Created, "widened", r.Widened)
	}
}

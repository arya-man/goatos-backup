package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
)

// CalendarReconcilerStage surfaces calendar_snoozes / notification_requests rows
// whose calendar_event_id no longer resolves to any canonical work (obligation /
// batch / park-day drive / SOP task / completion). Migration 000189 dropped the
// event-identity foreign keys in favour of app-enforced references, so this
// housekeeping (daily) sweep is the integrity enforcement: it PARSES each typed
// id and reports genuine orphans for ops follow-up. It is a DETECTOR — it never
// mutates or deletes rows (a dangling reference may be a transient recompute
// window, not corruption).
//
// FINDING 4 fix (migration 000192): stage processes bounded pages (pageSize=1000,
// perRunCap=10 pages = 10k orphans max per run) to avoid timeout/memory bloat at
// scale. Logs totals + sample (first 5 orphans), NOT one line per orphan.
//
// CAL-MAIN-03 fix (migrations 000202 + 000203): the stage uses a stable keyset
// cursor (source_table, record_id) instead of LIMIT/OFFSET, and PERSISTS that
// cursor across daily runs (calendar_reconciler_progress). A run resumes from
// where the previous run stopped and only wraps back to the beginning once it
// exhausts the current orphan set, so records ordered beyond a single run's
// per-run cap are eventually inspected instead of being permanently skipped.
type CalendarReconcilerStage struct {
	service  *calendarapp.Service
	tenantID string
	logger   *slog.Logger
}

const (
	reconcilerPageSize   = 1000 // FINDING 4: bounded page size
	reconcilerPerRunCap  = 10   // process up to 10 pages (10k orphans) per run
	reconcilerSampleSize = 5    // log sample of first 5 orphans
)

// NewCalendarReconcilerStage builds the calendar event-reference reconciler stage.
func NewCalendarReconcilerStage(deps Deps, tenantID string) *CalendarReconcilerStage {
	service := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	return &CalendarReconcilerStage{service: service, tenantID: tenantID, logger: deps.Logger}
}

// Name implements worker.StageRunner.
func (s *CalendarReconcilerStage) Name() string { return "calendar-event-reference-reconciler" }

// Run processes bounded pages of calendar event-reference orphans, logging totals
// and a small sample rather than one line per orphan (FINDING 4 fix).
func (s *CalendarReconcilerStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("calendar reconciler: tenant id is required")
	}

	// Resume from the cursor persisted by the previous run (CAL-MAIN-03). An
	// absent cursor ("", "") starts from the beginning of the orphan set.
	cursorSourceTable, cursorRecordID, err := s.service.LoadReconcilerCursor(ctx, s.tenantID)
	if err != nil {
		return fmt.Errorf("calendar reconciler: load cursor: %w", err)
	}

	// Process bounded pages up to the per-run cap, advancing the keyset cursor.
	var totalOrphans int
	var sampleOrphans []string // track the first N for logging
	exhausted := false

	for page := 0; page < reconcilerPerRunCap; page++ {
		// Bounded: <=reconcilerPerRunCap (10) iterations, each one keyset paged read of 1000 rows.
		// scale-guard:ignore: per-PAGE keyset read in a capped loop, not a per-row round trip.
		orphans, err := s.service.ReconcileEventReferencesPage(ctx, s.tenantID, cursorSourceTable, cursorRecordID, reconcilerPageSize)
		if err != nil {
			return fmt.Errorf("calendar event-reference reconcile page %d: %w", page, err)
		}

		if len(orphans) == 0 {
			// Reached the end of the current orphan set.
			exhausted = true
			break
		}

		totalOrphans += len(orphans)

		// Advance the keyset cursor to the last row of this page.
		last := orphans[len(orphans)-1]
		cursorSourceTable, cursorRecordID = last.SourceTable, last.RecordID

		// Collect the first reconcilerSampleSize orphans for logging.
		if len(sampleOrphans) < reconcilerSampleSize {
			for _, o := range orphans {
				if len(sampleOrphans) < reconcilerSampleSize {
					sampleOrphans = append(sampleOrphans, o.CalendarEventID)
				}
			}
		}

		// A short page means we reached the end of the current orphan set.
		if len(orphans) < reconcilerPageSize {
			exhausted = true
			break
		}
	}

	// Persist progress across runs. If we exhausted the orphan set, wrap back to
	// the beginning so newly-created orphans are re-inspected next run; otherwise
	// resume from where this run stopped so records beyond the per-run cap are
	// eventually inspected (CAL-MAIN-03). A save failure is non-fatal: the next
	// run simply re-processes from the previously stored cursor.
	if exhausted {
		cursorSourceTable, cursorRecordID = "", ""
	}
	if err := s.service.SaveReconcilerCursor(ctx, s.tenantID, cursorSourceTable, cursorRecordID, time.Now()); err != nil {
		if s.logger != nil {
			s.logger.Warn("calendar_reconciler_cursor_save_failed",
				"tenant_id", s.tenantID,
				"error", err.Error(),
			)
		}
	}

	// more_remain is true when we hit the per-run cap without exhausting the set.
	moreRemain := !exhausted

	if s.logger != nil {
		attrs := []any{
			"tenant_id", s.tenantID,
			"total_orphans", totalOrphans,
		}
		if moreRemain {
			attrs = append(attrs, "capped", "true", "more_remain", "true")
		}
		s.logger.Info("calendar_event_reference_reconcile_stage_complete", attrs...)

		// Log a sample of the orphans found, not one per orphan.
		if len(sampleOrphans) > 0 {
			s.logger.Warn("calendar_event_reference_orphans_sample",
				"tenant_id", s.tenantID,
				"sample_size", len(sampleOrphans),
				"sample_ids", sampleOrphans,
			)
		}
	}
	return nil
}

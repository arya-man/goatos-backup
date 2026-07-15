package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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

	// Process bounded pages up to the per-run cap, retaining progress across pages.
	var totalOrphans int
	var sampleOrphans []string // track the first N for logging
	var capped bool

	for page := 0; page < reconcilerPerRunCap; page++ {
		offset := page * reconcilerPageSize
		// Bounded: <=reconcilerPerRunCap (10) iterations, each one LIMIT/OFFSET paged read of 1000 rows.
		// scale-guard:ignore: per-PAGE paged read in a capped loop, not a per-row round trip.
		orphans, err := s.service.ReconcileEventReferencesPage(ctx, s.tenantID, reconcilerPageSize, offset)
		if err != nil {
			return fmt.Errorf("calendar event-reference reconcile page %d: %w", page, err)
		}

		if len(orphans) == 0 {
			// No more pages to process.
			break
		}

		totalOrphans += len(orphans)

		// Collect the first reconcilerSampleSize orphans for logging.
		if len(sampleOrphans) < reconcilerSampleSize {
			for _, o := range orphans {
				if len(sampleOrphans) < reconcilerSampleSize {
					sampleOrphans = append(sampleOrphans, o.CalendarEventID)
				}
			}
		}

		// If this page is full, there may be more to process next run.
		if len(orphans) == reconcilerPageSize {
			capped = true
		}
	}

	if s.logger != nil {
		attrs := []any{
			"tenant_id", s.tenantID,
			"total_orphans", totalOrphans,
		}
		if capped {
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

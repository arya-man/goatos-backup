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
// window, not corruption), so an orphan is logged at warn, not auto-repaired.
type CalendarReconcilerStage struct {
	service  *calendarapp.Service
	tenantID string
	logger   *slog.Logger
}

// NewCalendarReconcilerStage builds the calendar event-reference reconciler stage.
func NewCalendarReconcilerStage(deps Deps, tenantID string) *CalendarReconcilerStage {
	service := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	return &CalendarReconcilerStage{service: service, tenantID: tenantID, logger: deps.Logger}
}

// Name implements worker.StageRunner.
func (s *CalendarReconcilerStage) Name() string { return "calendar-event-reference-reconciler" }

// Run reports calendar event-reference orphans for the tenant.
func (s *CalendarReconcilerStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("calendar reconciler: tenant id is required")
	}
	orphans, err := s.service.ReconcileEventReferences(ctx, s.tenantID)
	if err != nil {
		return fmt.Errorf("calendar event-reference reconcile: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("calendar_event_reference_reconcile_stage_complete",
			"tenant_id", s.tenantID,
			"orphans", len(orphans),
		)
		for _, o := range orphans {
			s.logger.Warn("calendar_event_reference_orphan",
				"tenant_id", s.tenantID,
				"source_table", o.SourceTable,
				"record_id", o.RecordID,
				"calendar_event_id", o.CalendarEventID,
				"issue", o.Issue,
			)
		}
	}
	return nil
}

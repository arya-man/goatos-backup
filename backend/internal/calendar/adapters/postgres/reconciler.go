package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
)

// calendarReconcileEventReferencesSQL delegates to the Postgres-side integrity check introduced by
// migration 000189 and fixed by migration 000191 (CR-004, calendar-canonical-5k50k review):
// goatos_reconcile_calendar_event_references parses each notification_requests/calendar_snoozes
// calendar_event_id by its typed prefix (obligation:/batch:/parkdrive:.../catchup:.../completion:/
// calendar:) via goatos_calendar_event_reference_valid, instead of the original (buggy) raw-uuid
// text comparison that flagged every valid row as orphaned.
const calendarReconcileEventReferencesSQL = `
SELECT source_table, record_id::text, calendar_event_id, issue
FROM goatos_reconcile_calendar_event_references($1::uuid)
ORDER BY source_table, record_id`

// ReconcileEventReferences is the Go-callable seam for the calendar_event_id integrity check
// (ports.Repository.ReconcileEventReferences). It is deliberately NOT wired into any recurring
// schedule here -- this package (backend/internal/calendar) does not own cmd/kernel-worker or
// internal/kernelstages. Whichever agent owns kernel-worker's housekeeping stages should wrap this
// exactly the way internal/kernelstages/inventory_reconciler.go wraps
// inventoryapp.Service.ReleaseBatchReconcileRemainders:
//
//	type CalendarEventReferenceReconcilerStage struct {
//	    service  *calendarapp.Service
//	    tenantID string
//	    logger   *slog.Logger
//	}
//	func (s *CalendarEventReferenceReconcilerStage) Name() string { return "calendar-event-reference-reconciler" }
//	func (s *CalendarEventReferenceReconcilerStage) Run(ctx context.Context) error {
//	    orphans, err := s.service.ReconcileEventReferences(ctx, s.tenantID)
//	    // log/alert on len(orphans) > 0; this is a surfacing check, not an auto-repair -- an orphaned
//	    // calendar_event_id needs human review (was the source obligation/batch legitimately deleted,
//	    // or is this a real data-integrity bug?), same posture as goatos_reconcile_calendar_event_references's
//	    // original doc comment in migration 000189.
//	}
//
// then register it in cmd/kernel-worker/main.go's stage list alongside
// kernelstages.NewIdempotencyKeySweeperStage/NewProcessedEventSweeperStage on a daily housekeeping
// cadence (this is a read-only surfacing check, not latency-sensitive).
func (r *Repository) ReconcileEventReferences(ctx context.Context, tenantID string) ([]ports.OrphanedCalendarEventReference, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, calendarReconcileEventReferencesSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("calendar: reconcile event references: %w", err)
	}
	defer rows.Close()
	orphans := []ports.OrphanedCalendarEventReference{}
	for rows.Next() {
		var o ports.OrphanedCalendarEventReference
		if err := rows.Scan(&o.SourceTable, &o.RecordID, &o.CalendarEventID, &o.Issue); err != nil {
			return nil, fmt.Errorf("calendar: scan orphaned event reference: %w", err)
		}
		orphans = append(orphans, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return orphans, nil
}

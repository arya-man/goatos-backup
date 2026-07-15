package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
)

// calendarReconcileEventReferencesSQL delegates to the Postgres-side integrity check introduced by
// migration 000189, fixed by migration 000191 (CR-004 FINDING 5: safe casting), and migration 000192
// (CR-004 FINDING 4: scale + pagination):
// goatos_reconcile_calendar_event_references parses each notification_requests/calendar_snoozes
// calendar_event_id by its typed prefix (obligation:/batch:/parkdrive:.../catchup:.../completion:/
// calendar:) via goatos_calendar_event_reference_valid, with safe casting (EXCEPTION handlers for
// malformed IDs) and bounded pagination (LIMIT/OFFSET) support.
const calendarReconcileEventReferencesSQL = `
SELECT source_table, record_id::text, calendar_event_id, issue
FROM goatos_reconcile_calendar_event_references($1::uuid, $2::int, $3::int)
ORDER BY source_table, record_id`

// ReconcileEventReferencesPage is the bounded pagination version: fetches up to limit orphans
// starting at the given offset, ordered by (source_table, record_id) for stable keyset pagination.
// This enables processing large orphan sets without loading all into memory (FINDING 4 fix).
func (r *Repository) ReconcileEventReferencesPage(ctx context.Context, tenantID string, limit, offset int) ([]ports.OrphanedCalendarEventReference, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, calendarReconcileEventReferencesSQL, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("calendar: reconcile event references page: %w", err)
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

// ReconcileEventReferences is the legacy unbounded version, now a thin wrapper for backward
// compatibility. New code should use ReconcileEventReferencesPage for bounded processing.
// Kept for internal/kernelstages CalendarReconcilerStage compat during migration.
func (r *Repository) ReconcileEventReferences(ctx context.Context, tenantID string) ([]ports.OrphanedCalendarEventReference, error) {
	// Fetch a very large offset-bound to get all (for now), to unblock the stage.
	// The stage itself should switch to ReconcileEventReferencesPage.
	return r.ReconcileEventReferencesPage(ctx, tenantID, 999999, 0)
}

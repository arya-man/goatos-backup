package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
)

// calendarReconcileEventReferencesSQL delegates to the Postgres-side integrity check introduced by
// migration 000189, fixed by migration 000191 (CR-004 FINDING 5: safe casting), migration 000192
// (CR-004 FINDING 4: scale + pagination), and migration 000202 (CAL-MAIN-03: keyset cursor):
// goatos_reconcile_calendar_event_references parses each notification_requests/calendar_snoozes
// calendar_event_id by its typed prefix (obligation:/batch:/parkdrive:.../catchup:.../completion:/
// calendar:) via goatos_calendar_event_reference_valid, with safe casting (EXCEPTION handlers for
// malformed IDs) and stable keyset pagination (no LIMIT/OFFSET rescanning).
//
// CAL-MAIN-03 FIX: uses keyset pagination (WHERE (source_table, record_id) > cursor) instead of
// LIMIT/OFFSET. Cursor parameters ($2, $3) are the last_source_table and last_record_id from the
// previous run. Passing empty strings starts from the beginning.
const calendarReconcileEventReferencesSQL = `
SELECT source_table, record_id::text, calendar_event_id, issue
FROM goatos_reconcile_calendar_event_references($1::uuid, $2, $3, $4::int)
ORDER BY source_table, record_id`

// ReconcileEventReferencesPage is the bounded pagination version: fetches up to limit orphans
// using stable keyset pagination (WHERE (source_table, record_id) > cursor). The cursor is the
// last (source_table, record_id) pair from the previous page; passing (”, ”) starts from the
// beginning. This enables processing large orphan sets without LIMIT/OFFSET rescanning.
// FINDING 4 + CAL-MAIN-03 fix: keyset pagination instead of LIMIT/OFFSET.
func (r *Repository) ReconcileEventReferencesPage(ctx context.Context, tenantID string, cursorSourceTable, cursorRecordID string, limit int) ([]ports.OrphanedCalendarEventReference, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, calendarReconcileEventReferencesSQL, tenantID, cursorSourceTable, cursorRecordID, limit)
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
// Uses keyset pagination (cursor=”, ”) to fetch from the start with a large limit.
func (r *Repository) ReconcileEventReferences(ctx context.Context, tenantID string) ([]ports.OrphanedCalendarEventReference, error) {
	// Fetch a very large limit to get all rows from the start (for now), to unblock the stage.
	// The stage itself should switch to ReconcileEventReferencesPage and track the keyset cursor.
	return r.ReconcileEventReferencesPage(ctx, tenantID, "", "", 999999)
}

// calendarLoadReconcilerCursorSQL loads a tenant's persisted keyset cursor (CAL-MAIN-03).
const calendarLoadReconcilerCursorSQL = `
SELECT cursor_source_table, cursor_record_id
FROM calendar_reconciler_progress
WHERE tenant_id = $1::uuid`

// LoadReconcilerCursor returns the last (source_table, record_id) the reconciler
// stage processed for this tenant. An absent row means "start from the beginning"
// and returns ("", "", nil). See CAL-MAIN-03 / migration 000203.
func (r *Repository) LoadReconcilerCursor(ctx context.Context, tenantID string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var cursorSourceTable, cursorRecordID string
	err := r.pool.QueryRow(ctx, calendarLoadReconcilerCursorSQL, tenantID).Scan(&cursorSourceTable, &cursorRecordID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("calendar: load reconciler cursor: %w", err)
	}
	return cursorSourceTable, cursorRecordID, nil
}

// calendarSaveReconcilerCursorSQL upserts a tenant's keyset cursor (CAL-MAIN-03).
const calendarSaveReconcilerCursorSQL = `
INSERT INTO calendar_reconciler_progress (tenant_id, cursor_source_table, cursor_record_id, updated_at)
VALUES ($1::uuid, $2, $3, $4)
ON CONFLICT (tenant_id) DO UPDATE
SET cursor_source_table = EXCLUDED.cursor_source_table,
    cursor_record_id = EXCLUDED.cursor_record_id,
    updated_at = EXCLUDED.updated_at`

// SaveReconcilerCursor persists the reconciler stage's keyset cursor so the next
// daily run resumes from where this run stopped (or from the beginning when the
// stage passes ("", "") after exhausting the orphan set). See CAL-MAIN-03.
func (r *Repository) SaveReconcilerCursor(ctx context.Context, tenantID, cursorSourceTable, cursorRecordID string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.pool.Exec(ctx, calendarSaveReconcilerCursorSQL, tenantID, cursorSourceTable, cursorRecordID, now); err != nil {
		return fmt.Errorf("calendar: save reconciler cursor: %w", err)
	}
	return nil
}

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const calendarDriveTargetsSQL = `
SELECT
  oi.obligation_id::text,
  oi.target_id::text AS goat_id,
  rfid.identifier_value AS rfid,
  g.management_stage AS stage,
  oi.status,
  oi.due_at
FROM obligation_instances oi
JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND oi.target_type = 'goat'
LEFT JOIN goat_identifiers rfid
  ON rfid.tenant_id = g.tenant_id
 AND rfid.goat_id = g.goat_id
 AND rfid.identifier_type = 'rfid'
 AND rfid.status = 'active'
LEFT JOIN locations scope_loc
  ON scope_loc.tenant_id = oi.tenant_id
 AND scope_loc.location_id = oi.scope_id
 AND oi.scope_type IN ('park', 'shed', 'cohort')
LEFT JOIN locations scope_parent
  ON scope_parent.tenant_id = oi.tenant_id
 AND scope_parent.location_id = scope_loc.parent_location_id
LEFT JOIN locations scope_grand
  ON scope_grand.tenant_id = oi.tenant_id
 AND scope_grand.location_id = scope_parent.parent_location_id
LEFT JOIN LATERAL (
  SELECT
    CASE
      WHEN oi.scope_type = 'shed' THEN scope_loc.location_id
      WHEN oi.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.location_id
    END AS shed_id
) loc ON true
WHERE oi.tenant_id = $1::uuid
  AND oi.rule_id = $2::uuid
  AND oi.status NOT IN ('waived', 'canceled', 'superseded')
  AND (
    ($3::uuid IS NOT NULL AND oi.batch_id = $3::uuid)
    OR (
      $3::uuid IS NULL
      AND oi.batch_id IS NULL
      AND to_char((oi.due_at AT TIME ZONE COALESCE(scope_loc.timezone, 'Asia/Kolkata'))::date, 'YYYY-MM-DD') = $4::text
      AND (
        ($5::uuid IS NULL AND loc.shed_id IS NULL)
        OR loc.shed_id = $5::uuid
      )
    )
  )
  AND ($6::uuid IS NULL OR oi.obligation_id > $6::uuid)
  AND ($7::bool OR g.park_id::text = ANY($8::text[]) OR g.shed_id::text = ANY($9::text[]))
ORDER BY oi.obligation_id ASC
LIMIT $10`

func (r *Repository) ListDriveTargets(ctx context.Context, q domain.DriveTargetQuery) (domain.CalendarDriveTargetListResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	parsed, err := domain.ParseDriveEventID(q.EventID)
	if err != nil {
		return domain.CalendarDriveTargetListResponse{}, ports.ErrNotFound
	}
	if err := r.eventExists(ctx, q.TenantID, q.EventID, q.Scope); err != nil {
		return domain.CalendarDriveTargetListResponse{}, err
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	fetchLimit := limit + 1

	var batchID any
	var shedID any
	dueDay := parsed.DueDay
	if parsed.BatchID != "" {
		batchID = parsed.BatchID
		dueDay = ""
	} else {
		batchID = nil
		if parsed.ShedID != "" {
			shedID = parsed.ShedID
		}
	}

	var cursorID any
	if q.Cursor != nil {
		if !uuidutil.IsUUIDString(q.Cursor.ObligationID) {
			return domain.CalendarDriveTargetListResponse{}, fmt.Errorf("calendar: invalid target cursor")
		}
		cursorID = q.Cursor.ObligationID
	}

	tenantWide, parkIDs, shedIDs := scopeArgs(q.Scope)
	rows, err := r.pool.Query(ctx, calendarDriveTargetsSQL,
		q.TenantID, parsed.RuleID, batchID, dueDay, shedID, cursorID,
		tenantWide, parkIDs, shedIDs, fetchLimit)
	if err != nil {
		return domain.CalendarDriveTargetListResponse{}, fmt.Errorf("calendar: list drive targets: %w", err)
	}
	defer rows.Close()

	items := make([]domain.CalendarDriveTarget, 0, limit)
	for rows.Next() {
		var item domain.CalendarDriveTarget
		var rfid pgtype.Text
		var stage pgtype.Text
		if err := rows.Scan(&item.ObligationID, &item.GoatID, &rfid, &stage, &item.Status, &item.DueAt); err != nil {
			return domain.CalendarDriveTargetListResponse{}, fmt.Errorf("calendar: scan drive target: %w", err)
		}
		item.RFID = textPtr(rfid)
		item.Stage = textPtr(stage)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.CalendarDriveTargetListResponse{}, err
	}

	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		cursor, err := domain.EncodeDriveTargetCursor(domain.DriveTargetCursor{ObligationID: last.ObligationID})
		if err != nil {
			return domain.CalendarDriveTargetListResponse{}, err
		}
		next = &cursor
	}
	return domain.CalendarDriveTargetListResponse{
		Source:     domain.SourceAPI,
		EventID:    q.EventID,
		Items:      items,
		NextCursor: next,
	}, nil
}

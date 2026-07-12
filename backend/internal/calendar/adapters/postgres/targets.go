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
WITH matched_obligations AS (
SELECT
  oi.obligation_id,
  oi.target_id AS animal_id,
  g.display_id AS display_id,
  aid1.identifier_value AS animal_identifier_1,
  aid2.identifier_value AS animal_identifier_2,
  g.management_stage AS stage,
  oi.status,
  oi.due_at
FROM obligation_instances oi
JOIN protocol_versions pv
  ON pv.tenant_id = oi.tenant_id
 AND pv.protocol_version_id = oi.protocol_version_id
 AND pv.status = 'published'
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id
 AND pd.protocol_id = pv.protocol_id
 AND pd.category = 'vaccination'
JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND oi.target_type = 'goat'
LEFT JOIN goat_identifiers aid1
  ON aid1.tenant_id = g.tenant_id
 AND aid1.goat_id = g.goat_id
 AND aid1.identifier_type = 'animal_identifier_1'
 AND aid1.status = 'active'
LEFT JOIN goat_identifiers aid2
  ON aid2.tenant_id = g.tenant_id
 AND aid2.goat_id = g.goat_id
 AND aid2.identifier_type = 'animal_identifier_2'
 AND aid2.status = 'active'
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
      WHEN oi.scope_type = 'park' THEN scope_loc.location_id
      WHEN oi.scope_type = 'shed' AND scope_parent.location_type = 'park' THEN scope_parent.location_id
      WHEN oi.scope_type = 'cohort' AND scope_grand.location_type = 'park' THEN scope_grand.location_id
    END AS park_id,
    CASE
      WHEN oi.scope_type = 'shed' THEN scope_loc.location_id
      WHEN oi.scope_type = 'cohort' AND scope_parent.location_type = 'shed' THEN scope_parent.location_id
    END AS shed_id
) loc ON true
WHERE oi.tenant_id = $1::uuid
  AND (
    (
      $2::uuid IS NOT NULL
      AND oi.batch_id = $2::uuid
      AND oi.status NOT IN ('waived', 'canceled', 'superseded')
    )
    OR (
      $2::uuid IS NULL
      AND oi.batch_id IS NULL
      AND oi.status NOT IN ('waived', 'canceled', 'superseded', 'completed')
      AND to_char((oi.due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD') = $3::text
      AND (
        ($4::uuid IS NOT NULL AND loc.park_id = $4::uuid)
        OR ($5::uuid IS NOT NULL AND loc.shed_id = $5::uuid)
        OR ($6::uuid IS NOT NULL AND oi.tenant_id = $6::uuid AND loc.park_id IS NULL AND loc.shed_id IS NULL)
      )
      AND ($7::uuid IS NULL OR oi.rule_id = $7::uuid)
    )
  )
  AND ($9::bool OR g.park_id::text = ANY($10::text[]) OR g.shed_id::text = ANY($11::text[]))
),
animal_targets AS (
  SELECT DISTINCT ON (animal_id)
    obligation_id,
    animal_id,
    display_id,
    animal_identifier_1,
    animal_identifier_2,
    stage,
    status,
    due_at
  FROM matched_obligations
  ORDER BY animal_id, due_at ASC, obligation_id ASC
)
SELECT
  obligation_id::text,
  animal_id::text,
  display_id,
  animal_identifier_1,
  animal_identifier_2,
  stage,
  status,
  due_at
FROM animal_targets
WHERE ($8::uuid IS NULL OR obligation_id > $8::uuid)
ORDER BY obligation_id ASC
LIMIT $12`

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
	var parkID any
	var shedID any
	var tenantID any
	var ruleID any
	dueDay := parsed.DueDay
	if parsed.BatchID != "" {
		batchID = parsed.BatchID
		dueDay = ""
	} else {
		batchID = nil
		if parsed.ParkID != "" {
			parkID = parsed.ParkID
		}
		if parsed.ShedID != "" {
			shedID = parsed.ShedID
		}
		if parsed.TenantID != "" {
			tenantID = parsed.TenantID
		}
		if parsed.RuleID != "" {
			ruleID = parsed.RuleID
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
		q.TenantID, batchID, dueDay, parkID, shedID, tenantID, ruleID, cursorID,
		tenantWide, parkIDs, shedIDs, fetchLimit)
	if err != nil {
		return domain.CalendarDriveTargetListResponse{}, fmt.Errorf("calendar: list drive targets: %w", err)
	}
	defer rows.Close()

	items := make([]domain.CalendarDriveTarget, 0, limit)
	for rows.Next() {
		var item domain.CalendarDriveTarget
		var animalIdentifier1 pgtype.Text
		var animalIdentifier2 pgtype.Text
		var stage pgtype.Text
		if err := rows.Scan(&item.ObligationID, &item.AnimalID, &item.DisplayID, &animalIdentifier1, &animalIdentifier2, &stage, &item.Status, &item.DueAt); err != nil {
			return domain.CalendarDriveTargetListResponse{}, fmt.Errorf("calendar: scan drive target: %w", err)
		}
		item.AnimalIdentifier1 = textPtr(animalIdentifier1)
		item.AnimalIdentifier2 = textPtr(animalIdentifier2)
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

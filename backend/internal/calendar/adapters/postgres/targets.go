package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// calendarDriveTargetsSQL resolves the animal roster for a drive event_id, in three mutually
// exclusive branches selected by which parameters are bound (batchID / dueDay+park+shed+tenant+rule /
// isParkDrive):
//   - $2 (batchID) set: a single batch's own obligations (also used as the internal membership
//     lookup for one member batch of a park-drive aggregate -- see matched_batches below).
//   - $2 unset, $12 (isParkDrive) false: the legacy unbatched catch-up lookup (due day + park/shed/
//     tenant + optional rule), open work only (excludes 'completed').
//   - $12 true (CR-002): the STABLE park-drive roster for a park+business-date identity
//     (parkdrive:park:<uuid>:date:<day> / parkdrive:tenant:<uuid>:date:<day>) -- the union of every
//     unbatched obligation due that park+day AND every obligation whose batch resolves to that
//     park+day (matched_batches), across ALL member batches/sources, matching the SAME (park_id,
//     due_date) membership grain canonical_read.go's obligation_drive_membership CTE aggregates for
//     the list/detail drive_summary counts. Batch scope is resolved from obligation_batches/locations
//     directly (NOT from the member obligation's own oi.scope_type/scope_id) because
//     AttachObligationsToBatch only sets batch_id -- it does not sync the obligation's own scope
//     columns to the batch's scope, so an obligation's own scope can be stale once batched. Includes
//     'completed' (unlike the catch-up branch) so the full roster -- done and pending -- is visible,
//     matching drive_summary's total_count = completed + due + overdue + deferred invariant.
const calendarDriveTargetsSQL = `
WITH matched_batches AS (
  SELECT ob.batch_id
  FROM obligation_batches ob
  LEFT JOIN locations shed_loc
    ON shed_loc.tenant_id = ob.tenant_id AND shed_loc.location_id = ob.scope_id
  WHERE ob.tenant_id = $1::uuid
    AND $12::bool
    AND ob.status NOT IN ('superseded', 'canceled')
    AND to_char((COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD') = $3::text
    AND (
      ($4::uuid IS NOT NULL AND shed_loc.parent_location_id = $4::uuid)
      OR ($6::uuid IS NOT NULL AND shed_loc.parent_location_id IS NULL)
    )
),
matched_obligations AS (
SELECT
  oi.obligation_id,
  oi.target_id AS animal_id,
  g.display_id AS display_id,
  aid1.identifier_value AS animal_identifier_1,
  aid2.identifier_value AS animal_identifier_2,
  shed.name AS shed_name,
  g.management_stage AS stage,
  g.lifecycle_status,
  g.health_status,
  g.exit_reason,
  defer_event.defer_status,
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
LEFT JOIN locations goat_current_loc
  ON goat_current_loc.tenant_id = g.tenant_id
 AND goat_current_loc.location_id = COALESCE(g.current_location_id, g.shed_id, g.park_id)
LEFT JOIN locations goat_current_parent
  ON goat_current_parent.tenant_id = g.tenant_id
 AND goat_current_parent.location_id = goat_current_loc.parent_location_id
LEFT JOIN locations goat_current_grand
  ON goat_current_grand.tenant_id = g.tenant_id
 AND goat_current_grand.location_id = goat_current_parent.parent_location_id
LEFT JOIN locations shed
  ON shed.tenant_id = g.tenant_id
 AND shed.location_id = COALESCE(
      CASE WHEN goat_current_loc.location_type = 'shed' THEN goat_current_loc.location_id END,
      CASE WHEN goat_current_parent.location_type = 'shed' THEN goat_current_parent.location_id END,
      CASE WHEN goat_current_grand.location_type = 'shed' THEN goat_current_grand.location_id END,
      g.shed_id
    )
 AND shed.location_type = 'shed'
LEFT JOIN LATERAL (
  SELECT ose.payload->>'defer_status' AS defer_status
  FROM obligation_status_events ose
  WHERE ose.tenant_id = oi.tenant_id
    AND ose.obligation_id = oi.obligation_id
    AND ose.event_type = 'deferred'
  ORDER BY ose.occurred_at DESC, ose.obligation_event_id DESC
  LIMIT 1
) defer_event ON true
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
      AND NOT $12::bool
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
    OR (
      $12::bool
      AND oi.status NOT IN ('waived', 'canceled', 'superseded')
      AND (
        oi.batch_id IN (SELECT batch_id FROM matched_batches)
        OR (
          oi.batch_id IS NULL
          AND to_char((oi.due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD') = $3::text
          AND (
            ($4::uuid IS NOT NULL AND loc.park_id = $4::uuid)
            OR ($6::uuid IS NOT NULL AND oi.tenant_id = $6::uuid AND loc.park_id IS NULL AND loc.shed_id IS NULL)
          )
        )
      )
    )
  )
  AND ($9::bool OR g.park_id = ANY($10::uuid[]) OR g.shed_id = ANY($11::uuid[]))
),
animal_targets AS (
  SELECT DISTINCT ON (animal_id)
    obligation_id,
    animal_id,
    display_id,
    animal_identifier_1,
    animal_identifier_2,
    shed_name,
    stage,
    lifecycle_status,
    health_status,
    exit_reason,
    defer_status,
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
  shed_name,
  stage,
  lifecycle_status,
  health_status,
  exit_reason,
  defer_status,
  status,
  due_at
FROM animal_targets
WHERE ($8::uuid IS NULL OR obligation_id > $8::uuid)
ORDER BY obligation_id ASC
LIMIT $13`

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

	// CR-002: a stable park-drive identity (parkdrive:park:.../parkdrive:tenant:...) resolves the
	// FULL aggregated roster -- every member batch's obligations plus any unbatched obligations --
	// for that park+business-date, not just one batch or the legacy unbatched catch-up set.
	isParkDrive := parsed.ParkDrive

	tenantWide, parkIDs, shedIDs := scopeArgs(q.Scope)
	rows, err := r.pool.Query(ctx, calendarDriveTargetsSQL,
		q.TenantID, batchID, dueDay, parkID, shedID, tenantID, ruleID, cursorID,
		tenantWide, parkIDs, shedIDs, isParkDrive, fetchLimit)
	if err != nil {
		return domain.CalendarDriveTargetListResponse{}, fmt.Errorf("calendar: list drive targets: %w", err)
	}
	defer rows.Close()

	items := make([]domain.CalendarDriveTarget, 0, limit)
	for rows.Next() {
		var item domain.CalendarDriveTarget
		var animalIdentifier1 pgtype.Text
		var animalIdentifier2 pgtype.Text
		var shedName pgtype.Text
		var stage pgtype.Text
		var lifecycleStatus pgtype.Text
		var healthStatus pgtype.Text
		var exitReason pgtype.Text
		var deferReason pgtype.Text
		if err := rows.Scan(&item.ObligationID, &item.AnimalID, &item.DisplayID, &animalIdentifier1, &animalIdentifier2, &shedName, &stage, &lifecycleStatus, &healthStatus, &exitReason, &deferReason, &item.Status, &item.DueAt); err != nil {
			return domain.CalendarDriveTargetListResponse{}, fmt.Errorf("calendar: scan drive target: %w", err)
		}
		item.AnimalIdentifier1 = textPtr(animalIdentifier1)
		item.AnimalIdentifier2 = textPtr(animalIdentifier2)
		item.ShedName = textPtr(shedName)
		item.Stage = textPtr(stage)
		item.LifecycleStatus = textPtr(lifecycleStatus)
		item.HealthStatus = textPtr(healthStatus)
		item.ExitReason = textPtr(exitReason)
		item.DeferReason = textPtr(deferReason)
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

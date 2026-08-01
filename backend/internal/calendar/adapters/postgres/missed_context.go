package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
)

// ResolveMissedObligationContext resolves ONE missed obligation to the facts a "this work was
// missed" notification needs: the owning module, the park/shed it belongs to, its due instant, and
// the operator who was assigned the drive covering it.
//
// Why the operator is resolved from vaccination_drive_assignments by (park, shed, planned business
// date) rather than through the batch: MarkMissedBefore sets obligation_instances.batch_id = NULL as
// part of the very transition that raises obligation.missed, so by the time this runs the batch link
// is already gone. The assignment row survives, and (tenant, park, planned_date) is indexed
// (vaccination_drive_assignments_park_day_idx).
//
// One bounded, indexed read: primary-key lookup of the obligation, then constant-cardinality joins
// to its protocol, its goat/shed, and at most one covering assignment.
func (r *Repository) ResolveMissedObligationContext(ctx context.Context, tenantID, obligationID string) (ports.MissedObligationContext, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// projection-review: producer unique key = obligation_instances (tenant_id, obligation_id) -- a
	// primary-key lookup, exactly one row. Consumer match/group columns = the SAME (tenant_id,
	// obligation_id); there is no GROUP BY and no aggregate here, so no fan-out is possible.
	// Row multiplicity of each joined side: protocol_versions/protocol_definitions 1:1 on the
	// obligation's protocol_version_id; goats 1:1 on target_id; locations 1:1 on the resolved shed
	// id; the drive-assignment side is a LATERAL ... LIMIT 1, so it contributes at most one row and
	// cannot multiply the obligation row even when a shed is split across partitions.
	const query = `
WITH obligation AS (
  SELECT oi.obligation_id,
         oi.due_at,
         oi.scope_type,
         oi.scope_id,
         oi.target_type,
         oi.target_id,
         oi.protocol_version_id
  FROM obligation_instances oi
  WHERE oi.tenant_id = $1::uuid
    AND oi.obligation_id = $2::uuid
),
scoped AS (
  SELECT o.obligation_id,
         o.due_at,
         pd.category AS module,
         COALESCE(
           CASE WHEN o.scope_type = 'shed' THEN o.scope_id END,
           g.shed_id
         ) AS shed_id,
         COALESCE(
           shed.parent_location_id,
           g.park_id,
           CASE WHEN o.scope_type IN ('park', 'center') THEN o.scope_id END
         ) AS park_id,
         shed.name AS shed_label
  FROM obligation o
  JOIN protocol_versions pv
    ON pv.tenant_id = $1::uuid
   AND pv.protocol_version_id = o.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
  LEFT JOIN goats g
    ON g.tenant_id = $1::uuid
   AND o.target_type = 'goat'
   AND g.goat_id = o.target_id
  LEFT JOIN locations shed
    ON shed.tenant_id = $1::uuid
   AND shed.location_id = COALESCE(CASE WHEN o.scope_type = 'shed' THEN o.scope_id END, g.shed_id)
   AND shed.location_type = 'shed'
)
SELECT s.obligation_id::text,
       s.module,
       s.due_at,
       COALESCE(s.park_id::text, ''),
       COALESCE(s.shed_id::text, ''),
       COALESCE(s.shed_label, ''),
       COALESCE(assignment.operator_id::text, '')
FROM scoped s
LEFT JOIN LATERAL (
  SELECT a.operator_id
  FROM vaccination_drive_assignments a
  WHERE a.tenant_id = $1::uuid
    AND a.park_id = s.park_id
    AND a.planned_date = (s.due_at AT TIME ZONE 'Asia/Kolkata')::date
    AND (s.shed_id IS NULL OR a.shed_id IS NULL OR a.shed_id = s.shed_id)
    AND a.operator_id IS NOT NULL
  ORDER BY (a.shed_id = s.shed_id) DESC, a.updated_at DESC
  LIMIT 1
) assignment ON TRUE`

	var out ports.MissedObligationContext
	err := r.pool.QueryRow(ctx, query, tenantID, obligationID).Scan(
		&out.ObligationID,
		&out.Module,
		&out.DueAt,
		&out.ParkID,
		&out.ShedID,
		&out.ShedLabel,
		&out.OperatorID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.MissedObligationContext{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.MissedObligationContext{}, fmt.Errorf("calendar: resolve missed obligation context: %w", err)
	}
	return out, nil
}

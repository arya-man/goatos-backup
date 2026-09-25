package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

// listAssignedBatchesMissingSOPTaskSQL is the ListAssignedBatchesMissingSOPTask projection.
const listAssignedBatchesMissingSOPTaskSQL = `
-- projection-review: membership=open (planned/in_progress) obligation_batches with no sop_task_id, whose batch protocol_version OR open-obligation protocol_version is the swept version (manual and retired-version batches included), and whose open obligations sit on a live drive-assignment member row; group_key=batch_id (one row per batch); join_cardinality=obligation_instances 1:N collapsed by GROUP BY batch_id, assignment membership checked with an EXISTS semijoin so member rows cannot fan out; pagination=keyset over (created_at,batch_id) with caller-carried cursor and LIMIT; scope=tenant plus batch-or-member-obligation protocol version, batch scope_type/scope_id as stored
SELECT ob.batch_id::text,
       COALESCE(MIN(oi.rule_id::text), '')::text AS rule_id,
       ob.scope_type,
       ob.scope_id::text,
       ob.created_at,
       ob.planned_date,
       ob.estimated_targets,
       COUNT(oi.obligation_id)::bigint AS attached_obligations
FROM obligation_batches ob
JOIN obligation_instances oi
  ON oi.tenant_id = ob.tenant_id
 AND oi.batch_id = ob.batch_id
 AND (oi.protocol_version_id = $2 OR ob.protocol_version_id = $2)
 AND oi.status IN ('scheduled', 'due', 'in_progress')
WHERE ob.tenant_id = $1
  AND ob.status IN ('planned', 'in_progress')
  AND ob.sop_task_id IS NULL
  AND (
    $3::timestamptz IS NULL
    OR ob.created_at > $3::timestamptz
    OR (ob.created_at = $3::timestamptz AND ob.batch_id > $4::uuid)
  )
  AND EXISTS (
    SELECT 1
    FROM vaccination_drive_assignment_members m
    JOIN obligation_instances moi
      ON moi.tenant_id = m.tenant_id
     AND moi.obligation_id = m.obligation_id
     AND moi.batch_id = ob.batch_id
     AND (moi.protocol_version_id = $2 OR ob.protocol_version_id = $2)
     AND moi.status IN ('scheduled', 'due', 'in_progress')
    WHERE m.tenant_id = ob.tenant_id
      AND m.canceled_at IS NULL
  )
GROUP BY ob.tenant_id, ob.batch_id, ob.scope_type, ob.scope_id, ob.planned_date, ob.estimated_targets, ob.created_at
ORDER BY ob.created_at ASC, ob.batch_id ASC
LIMIT $5`

// ListAssignedBatchesMissingSOPTask returns open batches that have NO SOP task although at least
// one of their open obligations of the given protocol version is on an operator's active drive
// assignment. The version filter matches the batch OR its member obligations: a maintainer
// manual batch can carry a retired protocol version while its obligations sit on the published
// one, and such a batch must still be tasked (with the published version's SOP config); a
// published-version batch whose obligations sit on a retired version stays tasked as before.
// The ordinary finalization query only sees status='planned' batches produced by the
// sweep; a batch that reached a roster another way (a manual/runbook drive restore, a batch that
// already moved to in_progress) was never tasked, so the phone had no task to write its RFID scans
// and proof against. The sweeper creates the missing task through the same idempotent batch-task
// path it uses for planned batches.
func (r *Repository) ListAssignedBatchesMissingSOPTask(ctx context.Context, tenantID, versionID string, after *domain.PlannedBatchFinalizationCursor, limit int32) ([]domain.PlannedBatchFinalization, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	version, err := pgconv.UUID(versionID)
	if err != nil {
		return nil, fmt.Errorf("obligation: version id: %w", err)
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var afterCreatedAt *time.Time
	var afterBatchID pgtype.UUID
	if after != nil {
		afterCreatedAt = &after.CreatedAt
		afterBatchID, err = pgconv.UUID(after.BatchID)
		if err != nil {
			return nil, fmt.Errorf("obligation: assigned taskless cursor batch id: %w", err)
		}
	}
	rows, err := r.pool.Query(ctx, listAssignedBatchesMissingSOPTaskSQL, tenant, version, afterCreatedAt, afterBatchID, limit)
	if err != nil {
		return nil, fmt.Errorf("obligation: list assigned taskless batches: %w", err)
	}
	defer rows.Close()
	out := make([]domain.PlannedBatchFinalization, 0)
	for rows.Next() {
		var b domain.PlannedBatchFinalization
		var plannedDate pgtype.Date
		if err := rows.Scan(&b.BatchID, &b.RuleID, &b.ScopeType, &b.ScopeID, &b.CreatedAt, &plannedDate, &b.EstimatedTargets, &b.AttachedObligations); err != nil {
			return nil, fmt.Errorf("obligation: scan assigned taskless batch: %w", err)
		}
		if plannedDate.Valid {
			d := plannedDate.Time
			b.PlannedDate = &d
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: iterate assigned taskless batches: %w", err)
	}
	return out, nil
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

const feedTransportIdemScope = "feed.transport.submit"

var _ ports.TransportStore = (*Repository)(nil)

// MaterializeTransportTasks creates today's one-per-operational-location work after 15:30 IST. The
// daily location key makes scheduler retries and overlapping workers harmless.
func (r *Repository) MaterializeTransportTasks(ctx context.Context, p ports.MaterializeTransportParams) (ports.MaterializeTransportResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	now := p.AsOf.In(biztime.DefaultLocation())
	day := biztime.BusinessDayStart(now)
	if now.Before(day.Add(15*time.Hour + 30*time.Minute)) {
		return ports.MaterializeTransportResult{BusinessDate: day.Format("2006-01-02")}, nil
	}
	tag, err := r.pool.Exec(ctx, `
INSERT INTO feed_transport_tasks (tenant_id, park_id, shed_id, partition_label, business_date, scheduled_at)
SELECT s.tenant_id, s.parent_location_id, s.location_id, part.partition_label, $2::date,
       (($2::date + time '15:30') AT TIME ZONE 'Asia/Kolkata')
FROM locations s
JOIN locations p ON p.tenant_id = s.tenant_id AND p.location_id = s.parent_location_id
JOIN LATERAL (
  SELECT sp.partition_label
  FROM shed_partitions sp
  WHERE sp.tenant_id = s.tenant_id
    AND sp.shed_id = s.location_id
    AND sp.status = 'active'
    AND COALESCE(NULLIF(sp.partition_label, ''), 'whole') <> 'whole'
  UNION ALL
  SELECT ''
  WHERE NOT EXISTS (
    SELECT 1
    FROM shed_partitions sp
    WHERE sp.tenant_id = s.tenant_id
      AND sp.shed_id = s.location_id
      AND sp.status = 'active'
      AND COALESCE(NULLIF(sp.partition_label, ''), 'whole') <> 'whole'
  )
) part ON true
WHERE s.tenant_id = $1::uuid AND s.location_type = 'shed' AND s.status = 'active'
  AND p.location_type = 'park' AND p.status = 'active'
ON CONFLICT (tenant_id, business_date, shed_id, COALESCE(NULLIF(btrim(partition_label), ''), 'whole')) DO NOTHING`, p.TenantID, day.Format("2006-01-02"))
	if err != nil {
		return ports.MaterializeTransportResult{}, fmt.Errorf("feeddirection: materialize transport tasks: %w", err)
	}
	return ports.MaterializeTransportResult{BusinessDate: day.Format("2006-01-02"), Inserted: tag.RowsAffected()}, nil
}

func (r *Repository) ListTransportTasks(ctx context.Context, q ports.ListTransportTasksParams) (ports.FeedTransportTaskPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 20
	}
	// projection-review: producer unique=(tenant_id,business_date,shed_id,partition_label); consumer match/group uses
	// the same columns. locations park and shed joins are 1:1 by (tenant_id,location_id). No ratios.
	// projection-review: membership=feed_transport_tasks for one tenant and business date, optionally narrowed by actor, park, shed and status; group_key=none on the row read (one row per task), and the partition join groups shed_partitions by (tenant_id, shed_id); join_cardinality=the partition subquery is pre-aggregated to ONE row per shed by GROUP BY tenant_id, shed_id with HAVING count(*) = 1, so it cannot fan a task row out; pagination=keyset on t.task_id with LIMIT n+1, applied after all filters, and the partition join adds no rows so page boundaries are unchanged; scope=tenant plus optional park/shed resolved from canonical location ids.
	// Returns the shed name and the partition as SEPARATE columns; oploc.Display() composes them
	// in Go below. The display rule lives in exactly one place -- a CASE that concatenates them
	// here is a second implementation, and six of those are what shipped 'Godel 1 1' and
	// 'Mandela 2 - 3' to operators.
	rows, err := r.pool.Query(ctx, `
SELECT t.task_id::text, t.park_id::text, p.name, t.shed_id::text, s.name,
       t.business_date::text, t.status, coalesce(t.operator_id::text,''),
       coalesce(t.current_attempt_id::text,''), coalesce(a.rejection_reason,''), t.scheduled_at,
       coalesce(t.partition_label, '')
FROM feed_transport_tasks t
JOIN locations p ON p.tenant_id=t.tenant_id AND p.location_id=t.park_id
JOIN locations s ON s.tenant_id=t.tenant_id AND s.location_id=t.shed_id
-- ONE grouped join, evaluated once, instead of the same correlated subquery repeated four
-- times. Agree-or-go-bare is HAVING count(*) = 1: exactly one real partition resolves, several
-- or none go bare.
LEFT JOIN feed_transport_attempts a ON a.tenant_id=t.tenant_id AND a.attempt_id=t.current_attempt_id
WHERE t.tenant_id=$1::uuid AND t.business_date=$2::date
  AND ($3::text='' OR t.operator_id IS NULL OR t.operator_id=$3::uuid)
	AND ($4::text='' OR t.park_id=$4::uuid)
	AND ($5::text='' OR t.shed_id=$5::uuid)
	AND ($6::text='' OR COALESCE(NULLIF(btrim(t.partition_label), ''), 'whole') = COALESCE(NULLIF(btrim($6::text), ''), 'whole'))
	AND ($7::text='' OR t.status=$7)
	AND ($8::text='' OR t.task_id > $8::uuid)
ORDER BY t.task_id LIMIT $9`, q.TenantID, q.Day.Format("2006-01-02"), q.ActorID, q.ParkID, q.ShedID, q.PartitionLabel, q.Status, q.Cursor, q.Limit+1)
	if err != nil {
		return ports.FeedTransportTaskPage{}, fmt.Errorf("feeddirection: list transport tasks: %w", err)
	}
	defer rows.Close()
	out := make([]ports.FeedTransportTask, 0, q.Limit+1)
	for rows.Next() {
		var x ports.FeedTransportTask
		if err := rows.Scan(&x.TaskID, &x.ParkID, &x.ParkLabel, &x.ShedID, &x.ShedLabel, &x.BusinessDate, &x.Status, &x.OperatorID, &x.CurrentAttemptID, &x.ReworkReason, &x.ScheduledAt, &x.PartitionLabel); err != nil {
			return ports.FeedTransportTaskPage{}, err
		}
		x.OperationalLocationDisplay = oploc.OperationalLocation{ShedName: x.ShedLabel, PartitionLabel: x.PartitionLabel}.Display()
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return ports.FeedTransportTaskPage{}, err
	}
	next := ""
	if len(out) > q.Limit {
		next = out[q.Limit-1].TaskID
		out = out[:q.Limit]
	}
	filters, err := r.listTransportFilterOptions(ctx, q)
	if err != nil {
		return ports.FeedTransportTaskPage{}, err
	}
	return ports.FeedTransportTaskPage{Items: out, NextCursor: next, Filters: filters}, nil
}

func (r *Repository) listTransportFilterOptions(ctx context.Context, q ports.ListTransportTasksParams) (ports.FeedTransportFilterOptions, error) {
	// projection-review: membership=feed_transport_tasks for the tenant/date/actor filter vocabulary, not the current page; group_key=(t.park_id, p.name) for parks and (t.shed_id, s.name, part.partition_label) for sheds; join_cardinality=the partition join is pre-aggregated to ONE row per shed (GROUP BY tenant_id, shed_id HAVING count(*) = 1), so it cannot duplicate a filter option; pagination=none by design -- filter vocabulary is whole-date scoped so the dropdown never narrows to the visible page; scope=tenant plus optional park, applied before grouping.
	// Filter vocabulary is whole-date and actor scoped, not derived from the current 20-row page.
	// The selected park narrows only the shed vocabulary; status/shed filters never hide choices.
	rows, err := r.pool.Query(ctx, `
SELECT 'park', t.park_id::text, p.name, ''
FROM feed_transport_tasks t
JOIN locations p ON p.tenant_id=t.tenant_id AND p.location_id=t.park_id
WHERE t.tenant_id=$1::uuid AND t.business_date=$2::date
  AND ($3::text='' OR t.operator_id IS NULL OR t.operator_id=$3::uuid)
GROUP BY t.park_id, p.name
UNION ALL
-- The filter DROPDOWN must name the same place the rows name. A bare s.name hides the
-- partition, so two pens of one shed read as one option and the operator cannot tell which
-- they picked. Composed here with the same agree-or-go-bare rule the row reads use.
SELECT 'shed',
       t.shed_id::text || E'\x1f' || COALESCE(NULLIF(BTRIM(t.partition_label), ''), 'whole'),
       s.name,
       coalesce(t.partition_label, '')
FROM feed_transport_tasks t
JOIN locations s ON s.tenant_id=t.tenant_id AND s.location_id=t.shed_id
-- Partition resolved by a GROUPED JOIN, not a correlated subquery: correlating on t.tenant_id
-- inside a query that groups by (t.shed_id, s.name) makes tenant_id an ungrouped outer column
-- and Postgres rejects it (42803). Agree-or-go-bare is kept by HAVING count(*) = 1.
-- The name and the partition come back as SEPARATE columns and are composed in Go by
-- oploc.Display(). Concatenating them here would be a second implementation of the display
-- rule living in SQL -- exactly the drift the guard blocks.
WHERE t.tenant_id=$1::uuid AND t.business_date=$2::date
  AND ($3::text='' OR t.operator_id IS NULL OR t.operator_id=$3::uuid)
  AND ($4::text='' OR t.park_id=$4::uuid)
GROUP BY t.shed_id, s.name, t.partition_label
ORDER BY 1, 3, 2`, q.TenantID, q.Day.Format("2006-01-02"), q.ActorID, q.ParkID)
	if err != nil {
		return ports.FeedTransportFilterOptions{}, fmt.Errorf("feeddirection: list transport filter options: %w", err)
	}
	defer rows.Close()
	var options ports.FeedTransportFilterOptions
	for rows.Next() {
		var kind, shedName, partitionLabel string
		var option ports.FeedTransportFilterOption
		if err := rows.Scan(&kind, &option.ID, &shedName, &partitionLabel); err != nil {
			return ports.FeedTransportFilterOptions{}, err
		}
		// The shed option ID is an opaque operational-location key, not a bare shed UUID.
		// Compose the label through the shared primitive so the dropdown reads exactly like the rows.
		option.PartitionLabel = partitionLabel
		option.Label = oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display()
		if kind == "park" {
			options.Parks = append(options.Parks, option)
		} else {
			options.Sheds = append(options.Sheds, option)
		}
	}
	if err := rows.Err(); err != nil {
		return ports.FeedTransportFilterOptions{}, err
	}
	return options, nil
}

func (r *Repository) GetTransportTask(ctx context.Context, tenantID, taskID string) (ports.FeedTransportTask, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var x ports.FeedTransportTask
	err := r.pool.QueryRow(ctx, `
SELECT t.task_id::text,t.park_id::text,p.name,t.shed_id::text,s.name,t.business_date::text,
       t.status,coalesce(t.operator_id::text,''),coalesce(t.current_attempt_id::text,''),
       coalesce(a.rejection_reason,''),t.scheduled_at,
       -- The DETAIL read must carry the same location the LIST read carries. It did not, so a
       -- partitioned shed showed "Godel 1 - Part 3" in the list and bare "Godel 1" on the task
       -- itself. AGREE-OR-GO-BARE: exactly one active real partition resolves, several or none
       -- go bare. min() is required -- a bare HAVING over a non-aggregated column is rejected
       -- by Postgres (42803).
       coalesce(t.partition_label, '')
FROM feed_transport_tasks t
JOIN locations p ON p.tenant_id=t.tenant_id AND p.location_id=t.park_id
JOIN locations s ON s.tenant_id=t.tenant_id AND s.location_id=t.shed_id
LEFT JOIN feed_transport_attempts a ON a.tenant_id=t.tenant_id AND a.attempt_id=t.current_attempt_id
WHERE t.tenant_id=$1::uuid AND t.task_id=$2::uuid`, tenantID, taskID).Scan(
		&x.TaskID, &x.ParkID, &x.ParkLabel, &x.ShedID, &x.ShedLabel, &x.BusinessDate,
		&x.Status, &x.OperatorID, &x.CurrentAttemptID, &x.ReworkReason, &x.ScheduledAt,
		&x.PartitionLabel,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.FeedTransportTask{}, ports.ErrTransportTaskNotActionable
	}
	if err != nil {
		return ports.FeedTransportTask{}, fmt.Errorf("feeddirection: get transport task: %w", err)
	}
	x.OperationalLocationDisplay = oploc.OperationalLocation{ShedName: x.ShedLabel, PartitionLabel: x.PartitionLabel}.Display()
	return x, nil
}

func (r *Repository) SubmitTransportAttempt(ctx context.Context, p ports.SubmitTransportParams) (ports.SubmitTransportResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.SubmitTransportResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, feedTransportIdemScope, p.IdempotencyKey, requestFingerprint(p.TaskID, p.ProofRef, p.OperatorID))
	if err != nil {
		return ports.SubmitTransportResult{}, err
	}
	if !reservation.proceed {
		var res ports.SubmitTransportResult
		// IDEMPOTENT REPLAY. This branch is the one a RETRY takes, so a bug here only appears on
		// the second submit -- the first succeeds and hides it. It selected l.partition_label
		// from `locations`, which has no such column; partitions live in shed_partitions. Same
		// scalar-subquery shape as the first-submit path above, and the same agree-or-go-bare
		// rule, so a replay returns the identical location the original submit returned.
		err = tx.QueryRow(ctx, `SELECT a.attempt_id::text,a.status,a.attempt_no,t.park_id::text,t.shed_id::text,
       coalesce((SELECT l.name FROM locations l WHERE l.tenant_id=t.tenant_id AND l.location_id=t.shed_id), ''),
       coalesce(t.partition_label, '')
FROM feed_transport_attempts a
JOIN feed_transport_tasks t ON t.tenant_id=a.tenant_id AND t.task_id=a.task_id
WHERE a.tenant_id=$1::uuid AND a.attempt_id=$2::uuid`, p.TenantID, reservation.resultID).Scan(&res.AttemptID, &res.Status, &res.AttemptNo, &res.ParkID, &res.ShedID, &res.ShedName, &res.PartitionLabel)
		if err != nil {
			return res, err
		}
		if err = tx.Commit(ctx); err != nil {
			return res, err
		}
		committed = true
		return res, nil
	}
	var status, assigned string
	var res ports.SubmitTransportResult
	// Row lock on feed_transport_tasks ONLY. The shed name and partition come from SCALAR
	// SUBQUERIES rather than joins: FOR UPDATE cannot be applied to the nullable side of an
	// outer join (0A000), and a plain join also made a bare `status` ambiguous (42702).
	// locations has no partition_label column -- partitions live in shed_partitions.
	// AGREE-OR-GO-BARE via min() + HAVING; a bare HAVING over a non-aggregated column is
	// rejected by Postgres (42803).
	err = tx.QueryRow(ctx, `SELECT t.status,coalesce(t.operator_id::text,''),t.park_id::text,t.shed_id::text,
       coalesce((SELECT l.name FROM locations l WHERE l.tenant_id=t.tenant_id AND l.location_id=t.shed_id), ''),
       coalesce(t.partition_label, '')
FROM feed_transport_tasks t
WHERE t.tenant_id=$1::uuid AND t.task_id=$2::uuid FOR UPDATE`, p.TenantID, p.TaskID).Scan(&status, &assigned, &res.ParkID, &res.ShedID, &res.ShedName, &res.PartitionLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.SubmitTransportResult{}, ports.ErrTransportTaskNotActionable
	}
	if err != nil {
		return ports.SubmitTransportResult{}, err
	}
	if assigned != "" && assigned != p.OperatorID {
		return ports.SubmitTransportResult{}, ports.ErrTransportAssignedToAnotherOperator
	}
	if status != domain.TransportStatusDue && status != domain.TransportStatusRework {
		return ports.SubmitTransportResult{}, ports.ErrTransportTaskNotActionable
	}
	err = tx.QueryRow(ctx, `
INSERT INTO feed_transport_attempts(tenant_id,task_id,attempt_no,proof_ref,operator_id,idempotency_key)
SELECT $1::uuid,$2::uuid,coalesce(max(attempt_no),0)+1,$3,$4::uuid,$5
FROM feed_transport_attempts WHERE tenant_id=$1::uuid AND task_id=$2::uuid
RETURNING attempt_id::text,status,attempt_no`, p.TenantID, p.TaskID, strings.TrimSpace(p.ProofRef), p.OperatorID, p.IdempotencyKey).Scan(&res.AttemptID, &res.Status, &res.AttemptNo)
	if err != nil {
		return res, fmt.Errorf("feeddirection: insert transport attempt: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE feed_transport_tasks SET status='verification_due',operator_id=$3::uuid,current_attempt_id=$4::uuid,updated_at=now(),row_version=row_version+1 WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, p.TenantID, p.TaskID, p.OperatorID, res.AttemptID)
	if err != nil {
		return res, err
	}
	if err = completeIdempotency(ctx, tx, p.TenantID, feedTransportIdemScope, p.IdempotencyKey, "feed_transport_attempt", res.AttemptID); err != nil {
		return res, err
	}
	if err = audit.NewTxRecorder(tx).Record(ctx, audit.Event{TenantID: p.TenantID, ActorID: p.ActorID, ActorType: p.ActorType, Action: "feed.transport.verification_due", ResourceType: "feed_transport_task", ResourceID: p.TaskID, ScopeType: "task", ScopeID: p.TaskID, AfterState: map[string]any{"status": "verification_due", "attempt_id": res.AttemptID, "attempt_no": res.AttemptNo}, TraceID: p.TraceID}); err != nil {
		return res, err
	}
	if err = tx.Commit(ctx); err != nil {
		return res, err
	}
	committed = true
	res.NewlyPending = true
	return res, nil
}

func (r *Repository) ApplyVerifiedTransport(ctx context.Context, p ports.ApplyTransportParams) (bool, error) {
	return r.applyTransportVerdict(ctx, p.TenantID, p.AttemptID, p.VerifiedBy, "", p.TraceID, true)
}
func (r *Repository) BounceTransportForRework(ctx context.Context, p ports.BounceTransportParams) (bool, error) {
	return r.applyTransportVerdict(ctx, p.TenantID, p.AttemptID, p.VerifiedBy, p.Reason, p.TraceID, false)
}

func (r *Repository) applyTransportVerdict(ctx context.Context, tenantID, attemptID, verifier, reason, trace string, approved bool) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	var taskID, current string
	err = tx.QueryRow(ctx, `SELECT task_id::text,status FROM feed_transport_attempts WHERE tenant_id=$1::uuid AND attempt_id=$2::uuid FOR UPDATE`, tenantID, attemptID).Scan(&taskID, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current != domain.TransportStatusVerificationDue {
		return false, nil
	}
	if approved {
		_, err = tx.Exec(ctx, `UPDATE feed_transport_attempts SET status='approved',verified_by=nullif($3,'')::uuid,verified_at=now(),updated_at=now() WHERE tenant_id=$1::uuid AND attempt_id=$2::uuid`, tenantID, attemptID, verifier)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE feed_transport_tasks SET status='completed',completed_at=now(),updated_at=now(),row_version=row_version+1 WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND current_attempt_id=$3::uuid`, tenantID, taskID, attemptID)
		}
	} else {
		_, err = tx.Exec(ctx, `UPDATE feed_transport_attempts SET status='rejected',rejection_reason=$3,verified_by=nullif($4,'')::uuid,verified_at=now(),updated_at=now() WHERE tenant_id=$1::uuid AND attempt_id=$2::uuid`, tenantID, attemptID, reason, verifier)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE feed_transport_tasks SET status='rework',updated_at=now(),row_version=row_version+1 WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND current_attempt_id=$3::uuid`, tenantID, taskID, attemptID)
		}
	}
	if err != nil {
		return false, err
	}
	action := "feed.transport.rework"
	state := "rework"
	actor := verifier
	if approved {
		action = "feed.transport.completed"
		state = "completed"
	}
	if err = audit.NewTxRecorder(tx).Record(ctx, audit.Event{TenantID: tenantID, ActorID: actor, ActorType: "verifier", Action: action, ResourceType: "feed_transport_task", ResourceID: taskID, ScopeType: "task", ScopeID: taskID, AfterState: map[string]any{"status": state, "attempt_id": attemptID, "rejection_reason": reason}, TraceID: trace}); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}

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

// MaterializeTransportTasks creates today's one-per-PHYSICAL-SHED work after 15:30 IST. The daily
// shed key makes scheduler retries and overlapping workers harmless.
//
// GRAIN IS THE SHED, NEVER THE PEN. Transport is one loading/staging run for the shed: the feed for
// every pen of a shed leaves on the same trip, so a per-partition task would ask one operator to
// film the same physical load two or three times. This is the recorded contract in
// docs/decisions/feed-transport-verification.md ("Grain: (tenant_id, business_date, shed_id). There
// is no session, batch, workflow, or consolidation grain") and in AGENTS.md. Migration 000143 broke
// it by fanning out over shed_partitions; 000152 is the forward repair. Partition grain belongs to
// PACKING and DISTRIBUTION, which are per-pen bags -- do not copy their shape back to here.
//
// partition_label is written EMPTY on every new row. The column stays only so pre-000152 rows keep
// naming the pen they were filmed for.
//
// A PEN CAN ALSO REACH THIS QUERY THROUGH THE LOCATION ROW, not just through partition_label, and
// 000152 did not close that half. `locations` still carries the farm's OLD per-pen rows -- active,
// typed 'shed', named "Castro 1" or "Godel 1 - Part 3" -- alongside the canonical parent shed and
// its shed_partitions entry. Membership of "active shed" therefore counted 126 sheds where the farm
// has 21, and a partitioned shed such as Mandela 1 raised ELEVEN transport tasks for one load: the
// shed plus one per pen. The grain looked correct at the column (an empty partition_label on every
// row) while being wrong at the row. Excluding the aliases restores the contract in
// docs/decisions/feed-transport-verification.md and AGENTS.md: ONE task, ONE video, per physical
// shed per day.
//
// The exclusion is the shared oploc predicate rather than a third hand-rolled copy; the two earlier
// copies already drifted, and the counts one does not catch the "- Part N" shape.
func (r *Repository) MaterializeTransportTasks(ctx context.Context, p ports.MaterializeTransportParams) (ports.MaterializeTransportResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	now := p.AsOf.In(biztime.DefaultLocation())
	day := biztime.BusinessDayStart(now)
	if now.Before(day.Add(15*time.Hour + 30*time.Minute)) {
		return ports.MaterializeTransportResult{BusinessDate: day.Format("2006-01-02")}, nil
	}
	var oldShedArbiterPresent bool
	if err := r.pool.QueryRow(ctx, `SELECT to_regclass('public.feed_transport_tasks_daily_shed_uq') IS NOT NULL`).Scan(&oldShedArbiterPresent); err != nil {
		return ports.MaterializeTransportResult{}, fmt.Errorf("feeddirection: inspect transport task arbiter: %w", err)
	}
	if oldShedArbiterPresent {
		tag, err := r.pool.Exec(ctx, `
INSERT INTO feed_transport_tasks (tenant_id, park_id, shed_id, partition_label, business_date, scheduled_at)
SELECT s.tenant_id, s.parent_location_id, s.location_id, '', $2::date,
       (($2::date + time '15:30') AT TIME ZONE 'Asia/Kolkata')
FROM locations s
JOIN locations p ON p.tenant_id = s.tenant_id AND p.location_id = s.parent_location_id
WHERE s.tenant_id = $1::uuid AND s.location_type = 'shed' AND s.status = 'active'
  AND p.location_type = 'park' AND p.status = 'active'
  AND `+oploc.PartitionAliasExclusionSQL("s")+`
ON CONFLICT (tenant_id, business_date, shed_id) DO NOTHING`, p.TenantID, day.Format("2006-01-02"))
		if err != nil {
			return ports.MaterializeTransportResult{}, fmt.Errorf("feeddirection: materialize transport tasks: %w", err)
		}
		return ports.MaterializeTransportResult{BusinessDate: day.Format("2006-01-02"), Inserted: tag.RowsAffected()}, nil
	}
	// ONE row per active shed, partition_label ALWAYS ''. The arbiter kept by 000143 is
	// (tenant, date, shed, COALESCE(NULLIF(btrim(partition_label),''),'whole')); because every row
	// this statement writes carries '', that key degenerates to one row per shed per day, which is
	// exactly the contract. Legacy pen rows keep their own key and survive as history without
	// colliding. 000152 does NOT rebuild the index for this reason -- a CONCURRENTLY rebuild on a
	// live table buys nothing here.
	tag, err := r.pool.Exec(ctx, `
INSERT INTO feed_transport_tasks (tenant_id, park_id, shed_id, partition_label, business_date, scheduled_at)
SELECT s.tenant_id, s.parent_location_id, s.location_id, '', $2::date,
       (($2::date + time '15:30') AT TIME ZONE 'Asia/Kolkata')
FROM locations s
JOIN locations p ON p.tenant_id = s.tenant_id AND p.location_id = s.parent_location_id
WHERE s.tenant_id = $1::uuid AND s.location_type = 'shed' AND s.status = 'active'
  AND p.location_type = 'park' AND p.status = 'active'
  AND `+oploc.PartitionAliasExclusionSQL("s")+`
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
	// projection-review: producer unique=(tenant_id,business_date,shed_id) for every row this build
	// writes (partition_label is always ''); consumer match/group uses the same columns. locations
	// park and shed joins are 1:1 by (tenant_id,location_id). No ratios.
	// projection-review: membership=feed_transport_tasks for one tenant and business date, optionally narrowed by park, shed and status; group_key=none on the row read (one row per task); join_cardinality=both locations joins are 1:1 on (tenant_id, location_id) and nothing joins shed_partitions, so no side can fan a task row out; pagination=keyset on t.task_id with LIMIT n+1, applied after all filters; scope=tenant plus optional park/shed resolved from canonical location ids.
	// Returns the shed name and the stored partition as SEPARATE columns; oploc.Display() composes
	// them in Go below. The display rule lives in exactly one place -- a CASE that concatenates
	// them here is a second implementation, and six of those are what shipped 'Godel 1 1' and
	// 'Mandela 2 - 3' to operators. Every row written since 000152 carries an EMPTY partition, so
	// this reads as the bare shed name; a surviving pre-000152 pen row still names its pen, which
	// is the truth about where that video was filmed.
	rows, err := r.pool.Query(ctx, `
SELECT t.task_id::text, t.park_id::text, p.name, t.shed_id::text, s.name,
       t.business_date::text, t.status, coalesce(t.operator_id::text,''),
       coalesce(t.current_attempt_id::text,''), coalesce(a.rejection_reason,''), t.scheduled_at,
       coalesce(t.partition_label, '')
FROM feed_transport_tasks t
JOIN locations p ON p.tenant_id=t.tenant_id AND p.location_id=t.park_id
JOIN locations s ON s.tenant_id=t.tenant_id AND s.location_id=t.shed_id
LEFT JOIN feed_transport_attempts a ON a.tenant_id=t.tenant_id AND a.attempt_id=t.current_attempt_id
WHERE t.tenant_id=$1::uuid AND t.business_date=$2::date
  AND t.status <> 'retired'
	AND ($3::text='' OR t.park_id=$3::uuid)
	AND (coalesce(cardinality($8::uuid[]), 0) = 0 OR t.park_id = ANY($8::uuid[]))
	-- Shed, not shed+pen. There is no partition filter because there is no partition grain: one
	-- shed is one task, so narrowing further could only hide part of a shed's own work.
	AND ($4::text='' OR t.shed_id=$4::uuid)
	AND ($5::text='' OR t.status=$5)
	AND ($6::text='' OR t.task_id > $6::uuid)
ORDER BY t.task_id LIMIT $7`, q.TenantID, q.Day.Format("2006-01-02"), q.ParkID, q.ShedID, q.Status, q.Cursor, q.Limit+1, q.AuthorizedParkIDs)
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
	// projection-review: membership=feed_transport_tasks for the tenant/date filter vocabulary, not the current page; group_key=(t.park_id, p.name) for parks and (t.shed_id, s.name) for sheds; join_cardinality=the locations join is 1:1 on (tenant_id, location_id) and nothing joins shed_partitions, so it cannot duplicate a filter option; pagination=none by design -- filter vocabulary is whole-date scoped so the dropdown never narrows to the visible page; scope=tenant plus optional park, applied before grouping.
	// Filter vocabulary is whole-date and park scoped, not derived from the current 20-row page.
	// The selected park narrows only the shed vocabulary; status/shed filters never hide choices.
	rows, err := r.pool.Query(ctx, `
SELECT 'park', t.park_id::text, p.name, ''
FROM feed_transport_tasks t
JOIN locations p ON p.tenant_id=t.tenant_id AND p.location_id=t.park_id
WHERE t.tenant_id=$1::uuid AND t.business_date=$2::date
  AND t.status <> 'retired'
  AND (coalesce(cardinality($4::uuid[]), 0) = 0 OR t.park_id = ANY($4::uuid[]))
GROUP BY t.park_id, p.name
UNION ALL
-- The shed option ID is the shed UUID, plainly. It was briefly an opaque
-- "<shed>\x1f<partition>" composite so a partitioned shed could offer one option per pen; the
-- dropdown then listed the same shed several times for work that is one trip. Grouping by
-- (t.shed_id, s.name) -- never by name alone, which merges the two parks' Castro/Gandhi/Yashoda.
SELECT 'shed',
       t.shed_id::text,
       s.name,
       ''
FROM feed_transport_tasks t
JOIN locations s ON s.tenant_id=t.tenant_id AND s.location_id=t.shed_id
WHERE t.tenant_id=$1::uuid AND t.business_date=$2::date
  AND t.status <> 'retired'
  AND ($3::text='' OR t.park_id=$3::uuid)
  AND (coalesce(cardinality($4::uuid[]), 0) = 0 OR t.park_id = ANY($4::uuid[]))
GROUP BY t.shed_id, s.name
ORDER BY 1, 3, 2`, q.TenantID, q.Day.Format("2006-01-02"), q.ParkID, q.AuthorizedParkIDs)
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
		// The shed option ID is the bare shed UUID. The label still goes through the shared
		// primitive so the dropdown reads exactly like the rows, with an empty partition.
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
		if err = r.commitAndInvalidateReadCache(ctx, tx); err != nil {
			return res, err
		}
		committed = true
		return res, nil
	}
	var status string
	var res ports.SubmitTransportResult
	// Row lock on feed_transport_tasks ONLY. The shed name and partition come from SCALAR
	// SUBQUERIES rather than joins: FOR UPDATE cannot be applied to the nullable side of an
	// outer join (0A000), and a plain join also made a bare `status` ambiguous (42702).
	// locations has no partition_label column -- partitions live in shed_partitions.
	// AGREE-OR-GO-BARE via min() + HAVING; a bare HAVING over a non-aggregated column is
	// rejected by Postgres (42803).
	err = tx.QueryRow(ctx, `SELECT t.status,t.park_id::text,t.shed_id::text,
       coalesce((SELECT l.name FROM locations l WHERE l.tenant_id=t.tenant_id AND l.location_id=t.shed_id), ''),
       coalesce(t.partition_label, '')
FROM feed_transport_tasks t
WHERE t.tenant_id=$1::uuid AND t.task_id=$2::uuid FOR UPDATE`, p.TenantID, p.TaskID).Scan(&status, &res.ParkID, &res.ShedID, &res.ShedName, &res.PartitionLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.SubmitTransportResult{}, ports.ErrTransportTaskNotActionable
	}
	if err != nil {
		return ports.SubmitTransportResult{}, err
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
	_, err = tx.Exec(ctx, `UPDATE feed_transport_tasks SET status='verification_due',operator_id=NULL,current_attempt_id=$3::uuid,updated_at=now(),row_version=row_version+1 WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, p.TenantID, p.TaskID, res.AttemptID)
	if err != nil {
		return res, err
	}
	if err = completeIdempotency(ctx, tx, p.TenantID, feedTransportIdemScope, p.IdempotencyKey, "feed_transport_attempt", res.AttemptID); err != nil {
		return res, err
	}
	if err = audit.NewTxRecorder(tx).Record(ctx, audit.Event{TenantID: p.TenantID, ActorID: p.ActorID, ActorType: p.ActorType, Action: "feed.transport.verification_due", ResourceType: "feed_transport_task", ResourceID: p.TaskID, ScopeType: "task", ScopeID: p.TaskID, AfterState: map[string]any{"status": "verification_due", "attempt_id": res.AttemptID, "attempt_no": res.AttemptNo}, TraceID: p.TraceID}); err != nil {
		return res, err
	}
	if err = r.commitAndInvalidateReadCache(ctx, tx); err != nil {
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
	if err = r.commitAndInvalidateReadCache(ctx, tx); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}

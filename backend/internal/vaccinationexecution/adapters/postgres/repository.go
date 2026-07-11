// Package postgres implements vaccination-execution read-model projections over Postgres.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/ports"
)

const (
	defaultQueryTimeout     = 3 * time.Second
	defaultClosedHistoryAge = 14 * 24 * time.Hour
	defaultExecutionHorizon = 30 * 24 * time.Hour
)

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) ListVaccinationExecution(ctx context.Context, q domain.ExecutionQuery) ([]domain.ExecutionProjection, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if q.Limit <= 0 {
		q.Limit = 200
	}
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	workState := ""
	if q.WorkState != nil {
		workState = string(*q.WorkState)
	}
	closedAfter := asOf.Add(-defaultClosedHistoryAge)
	parkID := ""
	if q.ParkID != nil {
		parkID = *q.ParkID
	}
	shedID := ""
	if q.ShedID != nil {
		shedID = *q.ShedID
	}
	rows, err := r.pool.Query(ctx, vaccinationExecutionSQL, q.TenantID, parkID, shedID, q.DueBefore, q.Limit, workState, asOf, closedAfter)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: list vaccination execution: %w", err)
	}
	defer rows.Close()
	out := []domain.ExecutionProjection{}
	for rows.Next() {
		var p domain.ExecutionProjection
		var batchID, batchStatus, taskState, operatorName, parkHeadName, verifierName pgtype.Text
		var obligationID, sopTaskID, sopVersionID, completionID pgtype.Text
		var sopTaskRowVersion pgtype.Int4
		var dueAt pgtype.Timestamptz
		var obligationCount, scheduledCount, dueCount, inProgressCount, completedCount int64
		var missedCount, deferredCount, canceledCount, recordedCount, acceptedCount int64
		var rejectedCount, reversedCount, healthDeferredCount int64
		if err := rows.Scan(
			&p.ParkID,
			&p.ParkName,
			&p.ShedID,
			&p.ShedName,
			&p.AnimalStage,
			&batchID,
			&p.ProtocolName,
			&p.DoseCode,
			&dueAt,
			&obligationCount,
			&scheduledCount,
			&dueCount,
			&inProgressCount,
			&completedCount,
			&missedCount,
			&deferredCount,
			&canceledCount,
			&recordedCount,
			&acceptedCount,
			&rejectedCount,
			&reversedCount,
			&batchStatus,
			&taskState,
			&operatorName,
			&parkHeadName,
			&verifierName,
			&p.UsableForVaccination,
			&p.IsQuarantine,
			&p.IsICU,
			&healthDeferredCount,
			&obligationID,
			&sopTaskID,
			&sopVersionID,
			&sopTaskRowVersion,
			&completionID,
		); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan vaccination execution: %w", err)
		}
		p.BatchID = textPtr(batchID)
		p.DueAt = timePtr(dueAt)
		p.ObligationCount = int(obligationCount)
		p.ScheduledCount = int(scheduledCount)
		p.DueCount = int(dueCount)
		p.InProgressCount = int(inProgressCount)
		p.CompletedCount = int(completedCount)
		p.MissedCount = int(missedCount)
		p.DeferredCount = int(deferredCount)
		p.CanceledCount = int(canceledCount)
		p.CompletionRecorded = int(recordedCount)
		p.CompletionAccepted = int(acceptedCount)
		p.CompletionRejected = int(rejectedCount)
		p.CompletionReversed = int(reversedCount)
		p.BatchStatus = textPtr(batchStatus)
		p.TaskState = textPtr(taskState)
		p.OperatorName = textPtr(operatorName)
		p.ParkHeadName = textPtr(parkHeadName)
		p.VerifierName = textPtr(verifierName)
		p.HealthDeferredCount = int(healthDeferredCount)
		p.ObligationID = textPtr(obligationID)
		p.SOPTaskID = textPtr(sopTaskID)
		p.SOPVersionID = textPtr(sopVersionID)
		p.SOPTaskRowVersion = int32Ptr(sopTaskRowVersion)
		p.CompletionID = textPtr(completionID)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: iterate vaccination execution: %w", err)
	}
	return out, nil
}

// VaccinationOperations returns one row per cohort (park · shed · stage) × vaccination protocol, with the
// cohort headcount, age band, earliest open due date, and the latest ACCEPTED administered_at as last_dose.
// The service rolls these flat rows up into the matrix + per-cohort detail shape.
func (r *Repository) VaccinationOperations(ctx context.Context, q domain.OperationsQuery) ([]domain.OperationsRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if q.Limit <= 0 {
		q.Limit = 500
	}
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	dueBefore := q.DueBefore
	if dueBefore.IsZero() {
		dueBefore = asOf.Add(defaultExecutionHorizon)
	}
	parkID := ""
	if q.ParkID != nil {
		parkID = *q.ParkID
	}
	shedID := ""
	if q.ShedID != nil {
		shedID = *q.ShedID
	}
	rows, err := r.pool.Query(ctx, vaccinationOperationsSQL, q.TenantID, asOf, dueBefore, parkID, shedID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: list operations: %w", err)
	}
	defer rows.Close()
	out := []domain.OperationsRow{}
	for rows.Next() {
		var row domain.OperationsRow
		var ageBand pgtype.Text
		var nextDue, lastDose pgtype.Timestamptz
		var animals, overdue, due, inProgress, scheduled, missed, deferred, accepted, proofPending, rejected, total int64
		if err := rows.Scan(
			&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName, &row.Stage, &ageBand,
			&row.ProtocolID, &row.ProtocolName, &animals, &nextDue, &lastDose,
			&overdue, &due, &inProgress, &scheduled, &missed, &deferred, &accepted, &proofPending, &rejected, &total,
		); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan operations: %w", err)
		}
		row.AgeBand = textPtr(ageBand)
		row.NextDue = timePtr(nextDue)
		row.LastDose = timePtr(lastDose)
		row.Animals = int(animals)
		row.OverdueCount = int(overdue)
		row.DueCount = int(due)
		row.InProgressCount = int(inProgress)
		row.ScheduledCount = int(scheduled)
		row.MissedCount = int(missed)
		row.DeferredCount = int(deferred)
		row.AcceptedCount = int(accepted)
		row.ProofPendingCount = int(proofPending)
		row.RejectedCount = int(rejected)
		row.TotalCount = int(total)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: iterate operations: %w", err)
	}
	return out, nil
}

// VaccinationGaps returns the bounded, keyset-paginated per-animal exclusion rows (missing date of
// birth / breed) for a tenant, optionally scoped to one park. Ordered and cursor-paginated by goat_id
// (the primary key), so a park with many gapped animals is never returned in one unbounded response.
func (r *Repository) VaccinationGaps(ctx context.Context, q domain.GapsQuery) ([]domain.GapProjectionRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	parkID := ""
	if q.ParkID != nil {
		parkID = *q.ParkID
	}
	cursor := zeroUUID
	if q.Cursor != nil && *q.Cursor != "" {
		cursor = *q.Cursor
	}
	rows, err := r.pool.Query(ctx, vaccinationGapsSQL, q.TenantID, parkID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: list gaps: %w", err)
	}
	defer rows.Close()
	out := []domain.GapProjectionRow{}
	for rows.Next() {
		var row domain.GapProjectionRow
		var shedID, shedName pgtype.Text
		var reasonCode string
		if err := rows.Scan(&row.GoatID, &row.DisplayID, &row.ParkID, &row.ParkName, &shedID, &shedName, &reasonCode); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan gaps: %w", err)
		}
		row.ShedID = textPtr(shedID)
		row.ShedName = textPtr(shedName)
		row.ReasonCode = domain.GapReasonCode(reasonCode)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: iterate gaps: %w", err)
	}
	return out, nil
}

// VaccinationGapsSummary returns the scoped reason-count aggregate: at most two GROUP BY buckets
// (no_date_of_birth / no_breed_on_record) over the same tenant+park+lifecycle-scoped predicate as
// VaccinationGaps above — a bounded indexed aggregate, never a raw full-herd COUNT(*)/GROUP BY.
func (r *Repository) VaccinationGapsSummary(ctx context.Context, q domain.GapsQuery) ([]domain.GapReasonCount, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	parkID := ""
	if q.ParkID != nil {
		parkID = *q.ParkID
	}
	rows, err := r.pool.Query(ctx, vaccinationGapsSummarySQL, q.TenantID, parkID)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: gaps summary: %w", err)
	}
	defer rows.Close()
	out := []domain.GapReasonCount{}
	for rows.Next() {
		var reasonCode string
		var count int64
		if err := rows.Scan(&reasonCode, &count); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan gaps summary: %w", err)
		}
		out = append(out, domain.GapReasonCount{ReasonCode: domain.GapReasonCode(reasonCode), Count: int(count)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: iterate gaps summary: %w", err)
	}
	return out, nil
}

const zeroUUID = "00000000-0000-0000-0000-000000000000"

// vaccinationGapsSQL is a bounded keyset-paginated scan: tenant_id + lifecycle_status prune the index,
// goat_id > $3 drives the PK-ordered keyset window, and the LIMIT bounds the response regardless of how
// many animals a park has gapped. Requires a park (INNER JOIN locations park), matching the same
// "located park_uuid IS NOT NULL" convention the execution/operations queries already use; a goat with
// no park at all is out of scope here (not a modeled gap reason).
const vaccinationGapsSQL = `
SELECT
  g.goat_id::text,
  g.display_id,
  g.park_id::text,
  park.name,
  g.shed_id::text,
  shed.name,
  CASE WHEN g.dob IS NULL THEN 'no_date_of_birth' ELSE 'no_breed_on_record' END
FROM goats g
JOIN locations park
  ON park.tenant_id = $1::uuid
 AND park.location_id = g.park_id
 AND park.location_type = 'park'
LEFT JOIN locations shed
  ON shed.tenant_id = $1::uuid
 AND shed.location_id = g.shed_id
 AND shed.location_type = 'shed'
WHERE g.tenant_id = $1::uuid
  AND g.lifecycle_status = 'alive'
  AND g.merged_into_goat_id IS NULL
  AND g.park_id IS NOT NULL
  AND ($2::text = '' OR g.park_id = $2::uuid)
  AND (g.dob IS NULL OR (g.breed IS NULL AND g.breed_id IS NULL))
  AND g.goat_id > $3::uuid
ORDER BY g.goat_id ASC
LIMIT $4;
`

// vaccinationGapsSummarySQL mirrors the same scoped predicate as vaccinationGapsSQL, without the
// per-animal projection/keyset, collapsed to a two-bucket GROUP BY reason count.
const vaccinationGapsSummarySQL = `
SELECT
  CASE WHEN g.dob IS NULL THEN 'no_date_of_birth' ELSE 'no_breed_on_record' END AS reason_code,
  COUNT(*)::bigint
FROM goats g
WHERE g.tenant_id = $1::uuid
  AND g.lifecycle_status = 'alive'
  AND g.merged_into_goat_id IS NULL
  AND g.park_id IS NOT NULL
  AND ($2::text = '' OR g.park_id = $2::uuid)
  AND (g.dob IS NULL OR (g.breed IS NULL AND g.breed_id IS NULL))
GROUP BY 1;
`

func textPtr(v pgtype.Text) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	s := v.String
	return &s
}

func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func int32Ptr(v pgtype.Int4) *int32 {
	if !v.Valid || v.Int32 <= 0 {
		return nil
	}
	i := v.Int32
	return &i
}

// vaccinationExecutionSQL is point-in-time correct as of $7 (as_of): completions are bounded by as_of and
// the obligation bucket is reconstructed AT as_of (completed via completed_at/as_of-bounded completion;
// missed/waived/deferred via the obligation_status_events log; scheduled/due/overdue from due_at/window) rather than
// read off the current obligation_instances.status. Documented residual (no event history to reconstruct):
// sop_tasks.state / obligation_batches.status stay current-state.
const vaccinationExecutionSQL = `
WITH asof_terminal AS (
  -- Latest TERMINAL transition (missed/waived/deferred; no timestamp column on obligation_instances) AT OR BEFORE
  -- as_of from the append-only event log. asof_terminal_type is the terminal status in effect at as_of
  -- (latest of missed/waived/deferred <= as_of); NULL means the obligation's only terminal events are after as_of
  -- (open at as_of), and has_terminal_event then separates that "future-only" case from "no terminal history
  -- at all" (row absent -> trust the current stored status). Restricted to missed/waived/deferred, which are
  -- exceptions at herd scale, so the subset stays small and index-bound. Residual: missed->reschedule->missed
  -- churn is not reopen-aware (last terminal event at/before as_of wins).
  SELECT
    obligation_id,
    (ARRAY_AGG(event_type ORDER BY occurred_at DESC, obligation_event_id DESC)
       FILTER (WHERE occurred_at <= $7::timestamptz))[1] AS asof_terminal_type,
    true AS has_terminal_event
  FROM obligation_status_events
  WHERE tenant_id = $1::uuid
    AND event_type IN ('missed', 'waived', 'deferred')
  GROUP BY obligation_id
),
raw AS (
  SELECT
    oi.obligation_id,
    oi.rule_id,
    oi.batch_id,
    oi.due_at,
    oi.window_start,
    oi.window_end,
    oi.completed_at,
    oi.status AS obligation_status,
    te.asof_terminal_type,
    te.has_terminal_event,
    pr.dose_code,
    pd.name AS protocol_name,
    ob.status AS batch_status,
    ob.conducted_by,
    st.state AS task_state,
    st.task_id AS sop_task_id,
    st.sop_version_id AS sop_version_id,
    st.row_version AS sop_task_row_version,
    st.assigned_to,
    g.lifecycle_status AS goat_lifecycle_status,
    g.health_status AS goat_health_status,
    g.management_stage AS goat_stage,
    -- as_of correctness on the verification: a dose accepted/rejected AFTER as_of was only 'recorded'
    -- (proof pending) at as_of (accept/reject stamps verified_at = now()).
    CASE
      WHEN vc.status IN ('accepted', 'rejected') AND vc.verified_at IS NOT NULL AND vc.verified_at > $7::timestamptz THEN 'recorded'
      ELSE vc.status
    END AS completion_status,
    vc.completion_id,
    CASE
      WHEN g.shed_id IS NOT NULL THEN g.shed_id
      WHEN oi.target_type = 'shed' THEN oi.target_id
      WHEN oi.scope_type = 'shed' THEN oi.scope_id
      ELSE NULL
    END AS shed_uuid,
    CASE
      WHEN g.park_id IS NOT NULL THEN g.park_id
      WHEN oi.target_type = 'park' THEN oi.target_id
      WHEN oi.scope_type = 'park' THEN oi.scope_id
      ELSE NULL
    END AS direct_park_uuid
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  LEFT JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN sop_tasks st
    ON st.tenant_id = oi.tenant_id
   AND st.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
  LEFT JOIN vaccination_completions vc
    ON vc.tenant_id = oi.tenant_id
   AND vc.obligation_id = oi.obligation_id
   -- as_of correctness: a completion recorded/administered AFTER as_of must not count.
   AND COALESCE(vc.administered_at, vc.created_at) <= $7::timestamptz
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND oi.due_at <= $4::timestamptz
    AND (
      oi.status IN ('scheduled', 'due', 'in_progress', 'deferred')
      OR oi.due_at >= $8::timestamptz
      -- A row 'completed' NOW but finalized AFTER as_of was still open at as_of; pull it to re-bucket.
      OR (oi.status = 'completed' AND oi.completed_at > $7::timestamptz)
    )
),
located AS (
  SELECT
    raw.*,
    COALESCE(raw.direct_park_uuid, shed_loc.parent_location_id) AS park_uuid,
    -- as_of-effective obligation status (reconstructed AT as_of, not the current stored status).
    CASE
      WHEN raw.obligation_status = 'completed' THEN
        CASE
          WHEN raw.completed_at IS NOT NULL AND raw.completed_at <= $7::timestamptz THEN 'completed'
          WHEN raw.completed_at IS NULL AND raw.completion_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN raw.due_at < $7::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $7::timestamptz THEN 'due' ELSE 'scheduled' END)
        END
      WHEN raw.obligation_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          WHEN raw.asof_terminal_type IS NOT NULL THEN raw.asof_terminal_type
          WHEN raw.has_terminal_event THEN (CASE WHEN raw.due_at < $7::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $7::timestamptz THEN 'due' ELSE 'scheduled' END)
          ELSE raw.obligation_status
        END
      WHEN raw.obligation_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN raw.due_at < $7::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $7::timestamptz THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM raw
  LEFT JOIN locations shed_loc
    ON shed_loc.tenant_id = $1::uuid
   AND shed_loc.location_id = raw.shed_uuid
   AND shed_loc.location_type = 'shed'
  WHERE raw.shed_uuid IS NOT NULL
),
grouped AS (
  SELECT
    located.park_uuid,
    located.shed_uuid,
    located.batch_id,
    located.rule_id,
    located.protocol_name,
    located.dose_code,
    MIN(located.due_at) AS due_at,
    COUNT(*)::bigint AS obligation_count,
    -- Bucket counts use the as_of-effective status; completion counts below use the as_of-bounded vc join.
    COUNT(*) FILTER (WHERE located.eff_status = 'scheduled')::bigint AS scheduled_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'due')::bigint AS due_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'in_progress')::bigint AS in_progress_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'completed')::bigint AS completed_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'missed')::bigint AS missed_count,
    COUNT(*) FILTER (WHERE located.eff_status IN ('waived', 'deferred'))::bigint AS deferred_count,
    0::bigint AS canceled_count,
    COUNT(*) FILTER (WHERE located.completion_status = 'recorded')::bigint AS completion_recorded,
    COUNT(*) FILTER (WHERE located.completion_status = 'accepted')::bigint AS completion_accepted,
    COUNT(*) FILTER (WHERE located.completion_status = 'rejected')::bigint AS completion_rejected,
    COUNT(*) FILTER (WHERE located.completion_status = 'reversed')::bigint AS completion_reversed,
    (ARRAY_AGG(located.batch_status ORDER BY
      CASE located.batch_status
        WHEN 'in_progress' THEN 0
        WHEN 'planned' THEN 1
        WHEN 'completed' THEN 2
        WHEN 'superseded' THEN 3
        WHEN 'canceled' THEN 4
        ELSE 5
      END,
      located.due_at DESC NULLS LAST
    ) FILTER (WHERE located.batch_status IS NOT NULL))[1] AS batch_status,
    (ARRAY_AGG(located.task_state ORDER BY
      CASE located.task_state
        WHEN 'rework_requested' THEN 0
        WHEN 'rejected' THEN 1
        WHEN 'submitted' THEN 2
        WHEN 'needs_review' THEN 3
        WHEN 'in_progress' THEN 4
        WHEN 'accepted' THEN 5
        ELSE 6
      END,
      located.due_at DESC NULLS LAST
    ) FILTER (WHERE located.task_state IS NOT NULL))[1] AS task_state,
    COALESCE(
      (ARRAY_AGG(operator.display_name ORDER BY
        CASE
          WHEN located.conducted_by IS NOT NULL THEN 0
          WHEN located.assigned_to IS NOT NULL THEN 1
          ELSE 2
        END,
        located.due_at DESC NULLS LAST,
        operator.updated_at DESC NULLS LAST,
        operator.workforce_member_id DESC
      ) FILTER (WHERE operator.display_name IS NOT NULL))[1],
      (ARRAY_AGG(default_operator.display_name ORDER BY
        CASE
          WHEN default_operator.primary_location_id = located.shed_uuid THEN 0
          WHEN default_operator.primary_location_id = located.park_uuid THEN 1
          ELSE 2
        END,
        located.due_at DESC NULLS LAST,
        default_operator.updated_at DESC NULLS LAST,
        default_operator.workforce_member_id DESC
      ) FILTER (WHERE default_operator.display_name IS NOT NULL))[1]
    ) AS operator_name,
    COALESCE(MAX(stage.stage_code), MAX(stage.name), MAX(located.goat_stage), 'Unknown') AS animal_stage,
    COUNT(*) FILTER (
      WHERE located.goat_lifecycle_status IN ('sick', 'under_treatment', 'quarantine', 'icu')
         OR COALESCE(located.goat_health_status, '') IN ('sick', 'under_treatment', 'quarantine', 'icu')
    )::bigint AS health_deferred_count,
    (ARRAY_AGG(located.obligation_id ORDER BY located.due_at DESC NULLS LAST, located.obligation_id DESC))[1]::text AS obligation_id,
    (ARRAY_AGG(located.sop_task_id ORDER BY located.due_at DESC NULLS LAST, located.sop_task_id DESC NULLS LAST))[1]::text AS sop_task_id,
    (ARRAY_AGG(located.sop_version_id ORDER BY located.due_at DESC NULLS LAST, located.sop_task_id DESC NULLS LAST) FILTER (WHERE located.sop_version_id IS NOT NULL))[1]::text AS sop_version_id,
    (ARRAY_AGG(located.sop_task_row_version ORDER BY located.due_at DESC NULLS LAST, located.sop_task_id DESC NULLS LAST) FILTER (WHERE located.sop_task_id IS NOT NULL))[1] AS sop_task_row_version,
    (ARRAY_AGG(located.completion_id ORDER BY located.due_at DESC NULLS LAST, located.completion_id DESC NULLS LAST))[1]::text AS completion_id
  FROM located
  JOIN locations shed
    ON shed.tenant_id = $1::uuid
   AND shed.location_id = located.shed_uuid
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  JOIN locations park
    ON park.tenant_id = $1::uuid
   AND park.location_id = located.park_uuid
   AND park.location_type = 'park'
   AND park.status = 'active'
  LEFT JOIN shed_profiles sp
    ON sp.tenant_id = $1::uuid
   AND sp.location_id = located.shed_uuid
  LEFT JOIN animal_stage_lookup stage
    ON stage.tenant_id = $1::uuid
   AND stage.animal_stage_id = sp.animal_stage_id
  LEFT JOIN workforce_members operator
    ON operator.tenant_id = $1::uuid
   AND operator.workforce_member_id = COALESCE(located.conducted_by, located.assigned_to)
   AND operator.status = 'active'
  LEFT JOIN LATERAL (
    SELECT wm.workforce_member_id, wm.display_name, wm.primary_location_id, wm.updated_at
    FROM workforce_members wm
    WHERE wm.tenant_id = $1::uuid
      AND wm.status = 'active'
      AND wm.primary_role_hint = 'operator'
      AND wm.primary_location_id IN (located.shed_uuid, located.park_uuid)
    ORDER BY CASE WHEN wm.primary_location_id = located.shed_uuid THEN 0 WHEN wm.primary_location_id = located.park_uuid THEN 1 ELSE 2 END,
             wm.updated_at DESC, wm.workforce_member_id DESC
    LIMIT 1
  ) default_operator ON true
  WHERE located.park_uuid IS NOT NULL
    AND ($2::text = '' OR located.park_uuid = $2::uuid)
    AND ($3::text = '' OR located.shed_uuid = $3::uuid)
  GROUP BY located.park_uuid, located.shed_uuid, located.batch_id, located.rule_id, located.protocol_name, located.dose_code
),
enriched AS (
  SELECT
    grouped.*,
    COALESCE(loa.usable_for_vaccination, true) AS usable_for_vaccination,
    COALESCE(loa.is_quarantine, false) AS is_quarantine,
    COALESCE(loa.is_icu, false) AS is_icu
  FROM grouped
  LEFT JOIN location_operational_attributes loa
    ON loa.tenant_id = $1::uuid
   AND loa.location_id = grouped.shed_uuid
),
stateful AS (
  SELECT
    enriched.*,
    CASE
      WHEN enriched.obligation_count > 0
       AND enriched.completed_count = enriched.obligation_count
       AND enriched.completion_rejected = 0
       AND enriched.completion_recorded = 0 THEN 'completed'
      WHEN enriched.completion_rejected > 0 THEN 'rejected'
      WHEN NOT enriched.usable_for_vaccination THEN 'blocked'
      WHEN enriched.deferred_count > 0
        OR enriched.health_deferred_count > 0
        OR enriched.is_quarantine
        OR enriched.is_icu THEN 'deferred'
      WHEN enriched.missed_count > 0 THEN 'missed'
      WHEN enriched.operator_name IS NULL
       AND enriched.completed_count < enriched.obligation_count THEN 'blocked'
      WHEN enriched.task_state IN ('rework_requested', 'rejected') THEN 'rejected'
      WHEN enriched.completion_recorded > 0
        OR enriched.task_state IN ('submitted', 'needs_review') THEN 'verification_pending'
      WHEN enriched.in_progress_count > 0
        OR enriched.batch_status = 'in_progress'
        OR enriched.task_state = 'in_progress' THEN 'in_progress'
      WHEN enriched.due_at < $7::timestamptz THEN 'overdue'
      WHEN enriched.due_count > 0 THEN 'due'
      ELSE 'scheduled'
    END AS work_state
  FROM enriched
)
SELECT
  grouped.park_uuid::text AS park_id,
  park.name AS park_name,
  grouped.shed_uuid::text AS shed_id,
  shed.name AS shed_name,
  grouped.animal_stage,
  grouped.batch_id::text AS batch_id,
  grouped.protocol_name,
  grouped.dose_code,
  grouped.due_at,
  grouped.obligation_count,
  grouped.scheduled_count,
  grouped.due_count,
  grouped.in_progress_count,
  grouped.completed_count,
  grouped.missed_count,
  grouped.deferred_count,
  grouped.canceled_count,
  grouped.completion_recorded,
  grouped.completion_accepted,
  grouped.completion_rejected,
  grouped.completion_reversed,
  grouped.batch_status,
  grouped.task_state,
  grouped.operator_name,
  park_head.display_name AS park_head_name,
  verifier.display_name AS verifier_name,
  grouped.usable_for_vaccination,
  grouped.is_quarantine,
  grouped.is_icu,
  grouped.health_deferred_count,
  grouped.obligation_id,
  grouped.sop_task_id,
  grouped.sop_version_id,
  grouped.sop_task_row_version,
  grouped.completion_id
FROM stateful grouped
JOIN locations park
  ON park.tenant_id = $1::uuid
 AND park.location_id = grouped.park_uuid
JOIN locations shed
  ON shed.tenant_id = $1::uuid
 AND shed.location_id = grouped.shed_uuid
LEFT JOIN LATERAL (
  SELECT wm.display_name
  FROM workforce_members wm
  WHERE wm.tenant_id = $1::uuid
    AND wm.status = 'active'
    AND wm.primary_role_hint = 'park_head'
    AND wm.primary_location_id IN (grouped.shed_uuid, grouped.park_uuid)
  ORDER BY CASE WHEN wm.primary_location_id = grouped.shed_uuid THEN 0 ELSE 1 END, wm.updated_at DESC, wm.workforce_member_id DESC
  LIMIT 1
) park_head ON true
LEFT JOIN LATERAL (
  SELECT wm.display_name
  FROM workforce_members wm
  WHERE wm.tenant_id = $1::uuid
    AND wm.status = 'active'
    AND wm.primary_role_hint = 'verifier'
    AND (wm.primary_location_id IS NULL OR wm.primary_location_id IN (grouped.shed_uuid, grouped.park_uuid))
  ORDER BY CASE WHEN wm.primary_location_id = grouped.shed_uuid THEN 0 WHEN wm.primary_location_id = grouped.park_uuid THEN 1 ELSE 2 END,
           wm.updated_at DESC, wm.workforce_member_id DESC
  LIMIT 1
) verifier ON true
WHERE ($6::text = '' OR grouped.work_state = $6::text)
ORDER BY
  CASE grouped.work_state
    WHEN 'rejected' THEN 0
    WHEN 'blocked' THEN 1
    WHEN 'overdue' THEN 2
    WHEN 'proof_pending' THEN 3
    WHEN 'verification_pending' THEN 4
    WHEN 'due' THEN 5
    WHEN 'in_progress' THEN 6
    WHEN 'deferred' THEN 7
    WHEN 'scheduled' THEN 8
    WHEN 'completed' THEN 9
    ELSE 11
  END,
  CASE WHEN grouped.work_state = 'completed' THEN grouped.due_at END DESC NULLS LAST,
  CASE WHEN grouped.work_state <> 'completed' THEN grouped.due_at END ASC NULLS LAST,
  park.name ASC,
  shed.name ASC,
  grouped.rule_id ASC
LIMIT $5;
`

// vaccinationOperationsSQL is point-in-time correct as of $2 (as_of). It does NOT bucket off the stored
// obligation_instances.status (which is the state NOW); it reconstructs the state the obligation had AT
// as_of so a transition recorded after as_of is not treated as already true:
//   - completed: counts only if finalized at/before as_of (prefer completed_at; fall back to an
//     as_of-bounded completion row when completed_at is null). A completion after as_of -> re-bucket open.
//   - missed/waived: re-bucketed only when the obligation_status_events log PROVES the transition was after
//     as_of. With no event timestamp the transition time is unknown, so the stored status is trusted.
//   - in_progress: trusted (no reliable dispatch timestamp to reconstruct an earlier open state).
//   - scheduled/due/overdue: derived deterministically from due_at/window_* vs as_of (no event history).
//
// Known limitation (handled by the process-integrity completion-status reconstruction, not here): the
// completion sub-state (recorded/accepted/rejected) uses vaccination_completions.status, which is the
// current verification status. A dose administered before as_of but accepted after as_of is bounded out of
// "completed" via completed_at, but its as_of verification sub-state is not yet reconstructed.
const vaccinationOperationsSQL = `
WITH completions AS (
  -- Collapse completion history to ONE effective row per obligation. Migration 000082 dropped the hard
  -- one-row uniqueness and keeps rejected/reversed attempts as immutable history alongside at most one
  -- active (recorded/accepted) row, so joining vaccination_completions directly fans out and double-counts
  -- (a rejected-then-accepted obligation would still read as rejected). Prefer the active attempt; otherwise
  -- the most recent historical attempt. last_accepted_at is the latest ACCEPTED dose (the real last_dose).
  SELECT
    obligation_id,
    (ARRAY_AGG(asof_status ORDER BY
      CASE WHEN asof_status IN ('recorded', 'accepted') THEN 0 ELSE 1 END,
      administered_at DESC,
      created_at DESC))[1] AS effective_status,
    MAX(administered_at) FILTER (WHERE asof_status = 'accepted') AS last_accepted_at
  FROM (
    SELECT
      obligation_id, administered_at, created_at,
      -- as_of correctness on the VERIFICATION, not just existence: accept/reject flips status and stamps
      -- verified_at = now(). A dose administered before as_of but accepted/rejected AFTER as_of was still
      -- only 'recorded' (proof pending) at as_of. So a verification not yet stamped by as_of reads as
      -- 'recorded'; last_accepted_at therefore only counts doses accepted-and-verified by as_of.
      CASE
        WHEN status IN ('accepted', 'rejected') AND verified_at IS NOT NULL AND verified_at > $2::timestamptz THEN 'recorded'
        ELSE status
      END AS asof_status
    FROM vaccination_completions
    WHERE tenant_id = $1::uuid
      -- existence bound: a completion is only "seen" if its event time (administered_at, falling back to
      -- the recording time) is at or before as_of.
      AND COALESCE(administered_at, created_at) <= $2::timestamptz
  ) c
  GROUP BY obligation_id
),
asof_terminal AS (
  -- Latest TERMINAL transition INTO a status with NO timestamp column on obligation_instances (missed/waived/deferred)
  -- AT OR BEFORE as_of. The append-only status-event log is the only source for when those transitions
  -- happened. asof_terminal_type is the terminal status in effect at as_of (latest of missed/waived/deferred <=
  -- as_of); completed has its own completed_at column and is handled below. asof_terminal_type IS NULL with
  -- has_terminal_event TRUE means the only terminal events are after as_of (open at as_of); the row is absent
  -- when there is no terminal history at all (cannot prove transition time -> trust the stored status,
  -- documented). Tenant + event_type indexed (obligation_status_events_tenant_type_recorded_idx);
  -- missed/waived/deferred are exceptions, so this subset stays small at herd scale. Residual: missed->reschedule->
  -- missed churn is not reopen-aware.
  SELECT
    obligation_id,
    (ARRAY_AGG(event_type ORDER BY occurred_at DESC, obligation_event_id DESC)
       FILTER (WHERE occurred_at <= $2::timestamptz))[1] AS asof_terminal_type,
    true AS has_terminal_event
  FROM obligation_status_events
  WHERE tenant_id = $1::uuid
    AND event_type IN ('missed', 'waived', 'deferred')
  GROUP BY obligation_id
),
raw AS (
  SELECT
    oi.obligation_id,
    oi.due_at,
    oi.window_start,
    oi.window_end,
    oi.completed_at,
    oi.status AS stored_status,
    te.asof_terminal_type,
    te.has_terminal_event,
    pd.protocol_id,
    pd.name AS protocol_name,
    g.age_band,
    COALESCE(NULLIF(g.management_stage, ''), 'Unknown') AS stage,
    oi.target_id AS goat_id,
    g.shed_id AS shed_uuid,
    g.park_id AS direct_park_uuid,
    c.effective_status AS completion_status,
    c.last_accepted_at
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  LEFT JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND oi.due_at <= $3::timestamptz
),
located AS (
  SELECT
    raw.*,
    COALESCE(raw.direct_park_uuid, shed_loc.parent_location_id) AS park_uuid,
    -- open_bucket reconstructs the non-terminal state purely from the due window vs as_of (deterministic,
    -- needs no event history): overdue once due_at has passed, due once the window has opened, else scheduled.
    CASE
      WHEN raw.due_at < $2::timestamptz THEN 'overdue'
      WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due'
      ELSE 'scheduled'
    END AS open_bucket
  FROM raw
  LEFT JOIN locations shed_loc
    ON shed_loc.tenant_id = $1::uuid
   AND shed_loc.location_id = raw.shed_uuid
   AND shed_loc.location_type = 'shed'
  WHERE raw.shed_uuid IS NOT NULL
),
effective AS (
  SELECT
    located.*,
    CASE
      WHEN located.stored_status = 'completed' THEN
        CASE
          WHEN located.completed_at IS NOT NULL AND located.completed_at <= $2::timestamptz THEN 'completed'
          WHEN located.completed_at IS NULL AND located.completion_status IS NOT NULL THEN 'completed'
          ELSE located.open_bucket
        END
      WHEN located.stored_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          WHEN located.asof_terminal_type IS NOT NULL THEN located.asof_terminal_type
          WHEN located.has_terminal_event THEN located.open_bucket
          ELSE located.stored_status
        END
      WHEN located.stored_status = 'in_progress' THEN 'in_progress'
      ELSE located.open_bucket
    END AS eff_status
  FROM located
)
SELECT
  effective.park_uuid,
  park.name AS park_name,
  effective.shed_uuid,
  shed.name AS shed_name,
  effective.stage,
  (ARRAY_AGG(effective.age_band) FILTER (WHERE effective.age_band IS NOT NULL))[1] AS age_band,
  effective.protocol_id,
  effective.protocol_name,
  COUNT(DISTINCT effective.goat_id)::bigint AS animals,
  MIN(effective.due_at) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress', 'scheduled')) AS next_due,
  MAX(effective.last_accepted_at) AS last_dose,
  COUNT(*) FILTER (WHERE effective.eff_status = 'overdue')::bigint AS overdue_count,
  COUNT(*) FILTER (WHERE effective.eff_status = 'due')::bigint AS due_count,
  COUNT(*) FILTER (WHERE effective.eff_status = 'in_progress')::bigint AS in_progress_count,
  COUNT(*) FILTER (WHERE effective.eff_status = 'scheduled')::bigint AS scheduled_count,
  COUNT(*) FILTER (WHERE effective.eff_status = 'missed')::bigint AS missed_count,
  COUNT(*) FILTER (WHERE effective.eff_status IN ('waived', 'deferred'))::bigint AS deferred_count,
  COUNT(*) FILTER (WHERE effective.eff_status = 'completed' AND effective.completion_status = 'accepted')::bigint AS accepted_count,
  COUNT(*) FILTER (WHERE effective.completion_status = 'recorded')::bigint AS proof_pending_count,
  COUNT(*) FILTER (WHERE effective.completion_status = 'rejected')::bigint AS rejected_count,
  COUNT(*)::bigint AS total_count
FROM effective
JOIN locations shed
  ON shed.tenant_id = $1::uuid
 AND shed.location_id = effective.shed_uuid
 AND shed.location_type = 'shed'
 AND shed.status = 'active'
JOIN locations park
  ON park.tenant_id = $1::uuid
 AND park.location_id = effective.park_uuid
 AND park.location_type = 'park'
 AND park.status = 'active'
WHERE effective.park_uuid IS NOT NULL
  AND ($4::text = '' OR effective.park_uuid = $4::uuid)
  AND ($5::text = '' OR effective.shed_uuid = $5::uuid)
GROUP BY effective.park_uuid, park.name, effective.shed_uuid, shed.name, effective.stage, effective.protocol_id, effective.protocol_name
ORDER BY park.name ASC, shed.name ASC, effective.stage ASC, effective.protocol_name ASC
LIMIT $6;
`

// ScanRoster returns per-animal vaccination obligations scoped by shed, with RFID tags and vaccine labels.
// Used by the mobile scan screen to match keyboard-wedge tag captures against due animals.
func (r *Repository) ScanRoster(ctx context.Context, q domain.ScanRosterQuery) ([]domain.ScanRosterRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	rows, err := r.pool.Query(ctx, scanRosterSQL, q.TenantID, q.ShedID, limit)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: scan roster: %w", err)
	}
	defer rows.Close()
	out := []domain.ScanRosterRow{}
	for rows.Next() {
		var row domain.ScanRosterRow
		var secondaryTag pgtype.Text
		if err := rows.Scan(
			&row.PrimaryTag,
			&secondaryTag,
			&row.VaccineLabel,
			&row.Status,
			&row.ObligationID,
		); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan roster scan: %w", err)
		}
		row.SecondaryTag = textPtr(secondaryTag)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: scan roster iterate: %w", err)
	}
	return out, nil
}

const scanRosterSQL = `
SELECT
  COALESCE(aid1.identifier_value, '') AS primary_tag,
  aid2.identifier_value AS secondary_tag,
  CONCAT(pr.name, ' · ', pr.dose_code) AS vaccine_label,
  CASE
    WHEN oi.status = 'due' OR (oi.due_at < now() AND oi.status = 'scheduled') THEN 'due'
    WHEN oi.status = 'in_progress' THEN 'in_progress'
    WHEN oi.status = 'completed' THEN 'completed'
    WHEN oi.status IN ('deferred', 'missed', 'waived') THEN 'deferred'
    ELSE 'pending'
  END AS status,
  oi.obligation_id::text
FROM obligation_instances oi
JOIN protocol_versions pv
  ON pv.tenant_id = oi.tenant_id
 AND pv.protocol_version_id = oi.protocol_version_id
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id
 AND pd.protocol_id = pv.protocol_id
 AND pd.category = 'vaccination'
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND oi.target_type = 'goat'
 AND g.merged_into_goat_id IS NULL
 AND g.lifecycle_status = 'alive'
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
WHERE oi.tenant_id = $1::uuid
  AND g.shed_id = $2::uuid
  AND oi.status NOT IN ('waived', 'canceled', 'superseded')
ORDER BY g.goat_id ASC
LIMIT $3;
`

func optStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// shedSummaryOrderBy maps the whitelisted sort enum to a fixed ORDER BY fragment. The fragment is a
// closed constant set (never interpolated from raw client input), so substituting it into the query is
// injection-safe. Columns referenced are outer-SELECT aliases.
func shedSummaryOrderBy(sort domain.ShedSummarySort) string {
	switch sort {
	case domain.ShedSortParkShed:
		return "park_name ASC, shed_name ASC"
	case domain.ShedSortDueDesc:
		return "due_animals DESC, park_name ASC, shed_name ASC"
	case domain.ShedSortAnimalDesc:
		return "animals DESC, park_name ASC, shed_name ASC"
	case domain.ShedSortNextDue:
		return "next_due ASC NULLS LAST, park_name ASC, shed_name ASC"
	default: // ShedSortStatus — merged-status priority (most urgent first), then park, then shed.
		return "CASE shed_status " +
			"WHEN 'overdue' THEN 0 " +
			"WHEN 'needs_review' THEN 1 " +
			"WHEN 'split' THEN 2 " +
			"WHEN 'due' THEN 3 " +
			"WHEN 'scheduled' THEN 4 " +
			"WHEN 'on_track' THEN 5 ELSE 6 END, park_name ASC, shed_name ASC"
	}
}

const capacityConfigSQL = `
SELECT max_per_day, capacity_scope, max_buffer_days, overflow_policy, row_version
FROM vaccination_capacity_config
WHERE tenant_id = $1::uuid;`

// CapacityConfig reads the tenant's daily vaccination cap config, falling back to the code default when
// no row is authored (migration 000155 seeds existing tenants; later tenants use the default). Capacity
// writes are owned by the protocol publish-sync path, not by vaccination execution.
func (r *Repository) CapacityConfig(ctx context.Context, tenantID string) (domain.CapacityConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cfg := domain.DefaultCapacityConfig()
	err := r.pool.QueryRow(ctx, capacityConfigSQL, tenantID).
		Scan(&cfg.MaxPerDay, &cfg.CapacityScope, &cfg.MaxBufferDays, &cfg.OverflowPolicy, &cfg.RowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultCapacityConfig(), nil
	}
	if err != nil {
		return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: capacity config: %w", err)
	}
	return cfg, nil
}

// ShedSummary returns the shed-wise rollup with ANIMAL-LEVEL Due counts plus the session-split planner's
// SQL-side outputs (Sessions, Capacity, merged Status computed from the tenant cap config so the capacity
// and status filters page correctly), filtered + offset-paginated, each row carrying the window total.
// Manager/Backup are attached later by the service.
func (r *Repository) ShedSummary(ctx context.Context, q domain.ShedSummaryQuery) ([]domain.ShedSummaryProjection, error) {
	cfg, err := r.CapacityConfig(ctx, q.TenantID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	dueBefore := q.DueBefore
	if dueBefore.IsZero() {
		dueBefore = asOf.Add(defaultExecutionHorizon)
	}
	status := ""
	if q.Status != nil {
		status = string(*q.Status)
	}
	capacity := ""
	if q.Capacity != nil {
		capacity = string(*q.Capacity)
	}
	query := strings.Replace(shedSummarySQL, "__ORDER_BY__", shedSummaryOrderBy(q.Sort), 1)
	rows, err := r.pool.Query(ctx, query,
		q.TenantID, asOf, dueBefore, optStr(q.ParkID), optStr(q.ShedID), optStr(q.Search),
		status, capacity, cfg.MaxPerDay, cfg.MaxBufferDays, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: shed summary: %w", err)
	}
	defer rows.Close()
	out := []domain.ShedSummaryProjection{}
	for rows.Next() {
		var row domain.ShedSummaryProjection
		var animals, due, openCells, sessions, total int64
		var capacityStatus, shedStatus string
		var lastDone, nextDue pgtype.Timestamptz
		if err := rows.Scan(
			&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName,
			&animals, &due, &openCells, &sessions, &capacityStatus, &shedStatus,
			&lastDone, &nextDue, &total,
		); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan shed summary: %w", err)
		}
		row.Animals = int(animals)
		row.DueAnimals = int(due)
		row.OpenCells = int(openCells)
		row.Sessions = int(sessions)
		row.Capacity = domain.CapacityStatus(capacityStatus)
		row.Status = domain.ShedStatus(shedStatus)
		row.LastDone = timePtr(lastDone)
		row.NextDue = timePtr(nextDue)
		row.TotalCount = int(total)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: iterate shed summary: %w", err)
	}
	return out, nil
}

// shedSummarySQL rolls the per-obligation as-of reconstruction up to ONE row per active shed with
// ANIMAL-LEVEL counts (a goat needing three vaccines is one due animal, not three). It reuses the exact
// completions/asof_terminal/raw/located/effective CTE chain the cohort×protocol operations query uses,
// so the shed row and the vaccine breakdown agree on as-of status. `alive` is the base (all alive goats
// in the shed, INCLUDING zero-obligation animals) so Animals is a true headcount and Done = Animals -
// Due always holds. The shed-due predicate (see domain.go) lives in due_agg. Shed rows are bounded (a
// tenant has at most a few hundred sheds), so COUNT(*) OVER() + LIMIT/OFFSET is scale-safe here; the
// unbounded axis is the per-shed animal list, which is keyset-paginated separately (ShedAnimals). The
// alive aggregate is tenant/lifecycle-scoped and index-backed (goats_tenant_lifecycle_shed_idx, mig 000154).
// __ORDER_BY__ is substituted from a closed whitelist in Go.
const shedSummarySQL = `
WITH alive AS (
  SELECT g.shed_id AS shed_uuid, COUNT(*)::bigint AS animals
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.lifecycle_status = 'alive'
    AND g.merged_into_goat_id IS NULL
    AND g.shed_id IS NOT NULL
  GROUP BY g.shed_id
),
completions AS (
  SELECT
    obligation_id,
    (ARRAY_AGG(asof_status ORDER BY
      CASE WHEN asof_status IN ('recorded', 'accepted') THEN 0 ELSE 1 END,
      administered_at DESC,
      created_at DESC))[1] AS effective_status,
    MAX(administered_at) FILTER (WHERE asof_status = 'accepted') AS last_accepted_at
  FROM (
    SELECT
      obligation_id, administered_at, created_at,
      CASE
        WHEN status IN ('accepted', 'rejected') AND verified_at IS NOT NULL AND verified_at > $2::timestamptz THEN 'recorded'
        ELSE status
      END AS asof_status
    FROM vaccination_completions
    WHERE tenant_id = $1::uuid
      AND COALESCE(administered_at, created_at) <= $2::timestamptz
  ) c
  GROUP BY obligation_id
),
asof_terminal AS (
  SELECT
    obligation_id,
    (ARRAY_AGG(event_type ORDER BY occurred_at DESC, obligation_event_id DESC)
       FILTER (WHERE occurred_at <= $2::timestamptz))[1] AS asof_terminal_type,
    true AS has_terminal_event
  FROM obligation_status_events
  WHERE tenant_id = $1::uuid
    AND event_type IN ('missed', 'waived', 'deferred')
  GROUP BY obligation_id
),
raw AS (
  SELECT
    oi.obligation_id,
    oi.due_at,
    oi.window_start,
    oi.completed_at,
    oi.status AS stored_status,
    te.asof_terminal_type,
    te.has_terminal_event,
    oi.target_id AS goat_id,
    g.shed_id AS shed_uuid,
    c.effective_status AS completion_status,
    c.last_accepted_at
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
   AND g.lifecycle_status = 'alive'
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND oi.due_at <= $3::timestamptz
    AND g.shed_id IS NOT NULL
),
effective AS (
  SELECT
    raw.goat_id,
    raw.shed_uuid,
    raw.due_at,
    raw.last_accepted_at,
    raw.completion_status,
    CASE
      WHEN raw.stored_status = 'completed' THEN
        CASE
          WHEN raw.completed_at IS NOT NULL AND raw.completed_at <= $2::timestamptz THEN 'completed'
          WHEN raw.completed_at IS NULL AND raw.completion_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN raw.due_at < $2::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due' ELSE 'scheduled' END)
        END
      WHEN raw.stored_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          WHEN raw.asof_terminal_type IS NOT NULL THEN raw.asof_terminal_type
          WHEN raw.has_terminal_event THEN (CASE WHEN raw.due_at < $2::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due' ELSE 'scheduled' END)
          ELSE raw.stored_status
        END
      WHEN raw.stored_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN raw.due_at < $2::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $2::timestamptz THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM raw
),
due_agg AS (
  SELECT
    effective.shed_uuid,
    -- animal-level Due (distinct animals with any actionable/unfinished obligation)
    COUNT(DISTINCT effective.goat_id) FILTER (
      WHERE effective.eff_status IN ('overdue', 'due', 'in_progress')
         OR effective.completion_status IN ('recorded', 'rejected')
    )::bigint AS due_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'overdue')::bigint AS overdue_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'scheduled')::bigint AS scheduled_animals,
    -- capacity is VACCINATION-level: count obligation CELLS that need administering (a goat needing FMD +
    -- HS contributes 2), the session planner's input.
    COUNT(*) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress'))::bigint AS open_cells,
    MAX(effective.last_accepted_at) AS last_done,
    MIN(effective.due_at) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress', 'scheduled')) AS next_due
  FROM effective
  GROUP BY effective.shed_uuid
),
shed_rows AS (
  SELECT
    park.location_id::text AS park_id,
    park.name AS park_name,
    shed.location_id::text AS shed_id,
    shed.name AS shed_name,
    alive.animals,
    COALESCE(due_agg.due_animals, 0) AS due_animals,
    COALESCE(due_agg.overdue_animals, 0) AS overdue_animals,
    COALESCE(due_agg.scheduled_animals, 0) AS scheduled_animals,
    COALESCE(due_agg.open_cells, 0) AS open_cells,
    due_agg.last_done,
    due_agg.next_due
  FROM alive
  JOIN locations shed
    ON shed.tenant_id = $1::uuid
   AND shed.location_id = alive.shed_uuid
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  JOIN locations park
    ON park.tenant_id = $1::uuid
   AND park.location_id = shed.parent_location_id
   AND park.location_type = 'park'
   AND park.status = 'active'
  LEFT JOIN due_agg ON due_agg.shed_uuid = alive.shed_uuid
),
-- Session-split planner, mirrored from app.PlanSessions. cap = $9 (max/day), buffer = $10 (max buffer
-- days). sessions = ceil(open cells / cap); allowed window = buffer + 1 days.
scored AS (
  SELECT
    shed_rows.*,
    CASE WHEN open_cells <= 0 THEN 0 ELSE CEIL(open_cells::numeric / GREATEST($9::numeric, 1))::int END AS sessions
  FROM shed_rows
),
classified AS (
  SELECT
    scored.*,
    CASE
      WHEN sessions <= 1 THEN 'within_cap'
      WHEN sessions <= ($10::int + 1) THEN 'over_cap'
      ELSE 'capacity_breach'
    END AS capacity_status,
    CASE
      WHEN overdue_animals > 0 THEN 'overdue'
      WHEN sessions > ($10::int + 1) THEN 'needs_review'  -- capacity breach with no late animal
      WHEN sessions > 1 THEN 'split'                       -- over cap, safely split
      WHEN due_animals > 0 THEN 'due'
      WHEN scheduled_animals > 0 THEN 'scheduled'
      ELSE 'on_track'
    END AS shed_status
  FROM scored
)
SELECT
  park_id, park_name, shed_id, shed_name,
  animals, due_animals, open_cells, sessions, capacity_status, shed_status,
  last_done, next_due,
  COUNT(*) OVER()::bigint AS total_count
FROM classified
WHERE ($4::text = '' OR park_id = $4)
  AND ($5::text = '' OR shed_id = $5)
  AND ($6::text = '' OR shed_name ILIKE '%' || $6 || '%' OR park_name ILIKE '%' || $6 || '%')
  AND ($7::text = '' OR shed_status = $7)
  AND ($8::text = '' OR capacity_status = $8)
ORDER BY __ORDER_BY__
LIMIT $11 OFFSET $12;
`

// ShedAnimals returns the shed's alive animals (Display ID + the two tag identities + a lightweight
// animal-level status hint), keyset-paginated by goat_id, for the shed-detail roster. The authoritative
// shed Due/Done counts are the as-of reconstruction in ShedSummary; the per-row status here is a cheaper
// current-status hint on the animal's own vaccination obligations, sufficient for a detail list.
func (r *Repository) ShedAnimals(ctx context.Context, q domain.ShedAnimalQuery) ([]domain.ShedAnimalRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	cursor := zeroUUID
	if q.Cursor != nil && *q.Cursor != "" {
		cursor = *q.Cursor
	}
	rows, err := r.pool.Query(ctx, shedAnimalListSQL, q.TenantID, q.ShedID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: shed animals: %w", err)
	}
	defer rows.Close()
	out := []domain.ShedAnimalRow{}
	for rows.Next() {
		var row domain.ShedAnimalRow
		var tag1, tag2, breed, age pgtype.Text
		if err := rows.Scan(&row.GoatID, &row.DisplayID, &tag1, &tag2, &breed, &row.Sex, &age, &row.Status); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan shed animals: %w", err)
		}
		row.Tag1 = textPtr(tag1)
		row.Tag2 = textPtr(tag2)
		row.Breed = textPtr(breed)
		row.Age = textPtr(age)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: iterate shed animals: %w", err)
	}
	return out, nil
}

// shedAnimalListSQL is a bounded keyset scan of alive goats in one shed (goat_id > $3 drives the
// PK-ordered window, LIMIT bounds the page). Display ID + both tag identities are returned as-is; a null
// tag becomes NULL -> the UI renders "-", never a "missing id" badge. status is 'due' when the animal
// has any actionable-now vaccination obligation, else 'done'.
const shedAnimalListSQL = `
SELECT
  g.goat_id::text,
  g.display_id,
  aid1.identifier_value AS tag1,
  aid2.identifier_value AS tag2,
  COALESCE(NULLIF(g.breed, ''), b.canonical_name) AS breed,
  g.sex,
  CASE
    WHEN g.dob IS NOT NULL THEN
      floor(extract(epoch FROM (now() - g.dob::timestamptz)) / 86400)::int::text || 'd'
    ELSE NULLIF(g.age_band, '')
  END AS age,
  CASE
    WHEN EXISTS (
      SELECT 1
      FROM obligation_instances oi
      JOIN protocol_versions pv
        ON pv.tenant_id = oi.tenant_id
       AND pv.protocol_version_id = oi.protocol_version_id
      JOIN protocol_definitions pd
        ON pd.tenant_id = pv.tenant_id
       AND pd.protocol_id = pv.protocol_id
       AND pd.category = 'vaccination'
      WHERE oi.tenant_id = g.tenant_id
        AND oi.target_type = 'goat'
        AND oi.target_id = g.goat_id
        AND ( oi.status IN ('due', 'in_progress')
              OR (oi.status = 'scheduled' AND oi.due_at <= now()) )
    ) THEN 'due'
    ELSE 'done'
  END AS status
FROM goats g
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
LEFT JOIN breeds b
  ON b.breed_id = g.breed_id
WHERE g.tenant_id = $1::uuid
  AND g.shed_id = $2::uuid
  AND g.lifecycle_status = 'alive'
  AND g.merged_into_goat_id IS NULL
  AND g.goat_id > $3::uuid
ORDER BY g.goat_id ASC
LIMIT $4;
`

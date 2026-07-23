// Package postgres implements process-integrity projections over Postgres.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	"github.com/vgoats/goatos/backend/internal/processintegrity/ports"
)

const (
	defaultQueryTimeout     = 3 * time.Second
	defaultClosedHistoryAge = 14 * 24 * time.Hour
	defaultLimit            = 100
	maxLimit                = 500
	countQueryArgCount      = 15
	rowsQueryArgCount       = 19
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

// ListRows serves the Action Center / adherence / drilldown read model directly from the canonical
// obligation/SOP/proof/completion tables through one bounded, tenant+due_at index-scoped, keyset-paginated
// query (processIntegrityCanonicalRowsSQL). Per the 5k-to-50k operational-kernel envelope ADR
// (docs/decisions/operational-kernel-5k-50k-scale-envelope.md) the derived process-integrity projection is
// retired as the request-path source: a canonical read cannot be stale relative to the canonical write, so
// the whole projection-drift/freshness-503 failure class is removed. The projection tables
// (process_integrity_projection_rows/_state/_summaries) and their projector were dropped
// (migrations 000187/000188); this canonical read is now the only serving path.
func (r *Repository) ListRows(ctx context.Context, q domain.Query) (domain.ListResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	q = normalizeQuery(q)
	args := queryArgs(q)
	return r.listRowsCanonical(ctx, q, args)
}

func (r *Repository) CountByWorkState(ctx context.Context, q domain.Query) ([]domain.CountByWorkState, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	q = normalizeQuery(q)
	args := queryArgs(q)
	counts, _, err := r.countByWorkStateCanonical(ctx, countQueryArgs(args))
	if err != nil {
		return nil, err
	}
	return counts, nil
}

func (r *Repository) listRowsCanonical(ctx context.Context, q domain.Query, args []any) (domain.ListResult, error) {
	rows, err := r.pool.Query(ctx, processIntegrityCanonicalRowsSQL, append([]any{pgx.QueryExecModeExec}, args...)...)
	if err != nil {
		return domain.ListResult{}, fmt.Errorf("processintegrity: list canonical rows: %w", err)
	}
	defer rows.Close()

	out := []domain.Row{}
	var lastCursor *domain.Cursor
	seenExtra := false
	for rows.Next() {
		row, cursor, err := scanRow(rows)
		if err != nil {
			return domain.ListResult{}, err
		}
		if len(out) < q.Limit {
			out = append(out, row)
			lastCursor = &cursor
		} else {
			seenExtra = true
		}
	}
	if err := rows.Err(); err != nil {
		return domain.ListResult{}, fmt.Errorf("processintegrity: iterate projection rows: %w", err)
	}

	counts := []domain.CountByWorkState{}
	var totalCount int64
	summary := domain.AdherenceSummary{}
	if q.RowID != nil {
		totalCount = int64(len(out))
	} else if q.IncludeAdherenceSummary {
		summaryRows := r.pool.QueryRow(ctx, processIntegrityCanonicalAdherenceSummarySQL, append([]any{pgx.QueryExecModeExec}, countQueryArgs(args)...)...)
		if err := summaryRows.Scan(
			&summary.ExpectedCount,
			&summary.CompletedCount,
			&summary.OpenGapCount,
			&summary.DeferredCount,
			&summary.ProcessIntactCount,
		); err != nil {
			return domain.ListResult{}, fmt.Errorf("processintegrity: canonical adherence summary: %w", err)
		}
		totalCount = int64(summary.OpenGapCount + summary.ProcessIntactCount)
	} else {
		var err error
		counts, totalCount, err = r.countByWorkStateCanonical(ctx, countQueryArgs(args))
		if err != nil {
			return domain.ListResult{}, err
		}
	}

	var next *string
	if seenExtra && lastCursor != nil {
		encoded, err := domain.EncodeCursor(*lastCursor)
		if err != nil {
			return domain.ListResult{}, err
		}
		next = &encoded
	}
	return domain.ListResult{Rows: out, CountsByWorkState: counts, TotalCount: totalCount, AdherenceSummary: summary, NextCursor: next, Projection: canonicalProjectionMetadata(q)}, nil
}

// canonicalProjectionMetadata reports the live-canonical serving contract on the response envelope. A
// canonical read is by construction consistent with the canonical write (no derived projection, no
// freshness watermark), so it is never stale; the metadata mirrors q.AsOf so downstream freshness UI keeps
// a stable, honest signal.
func canonicalProjectionMetadata(q domain.Query) domain.ProjectionMetadata {
	return domain.ProjectionMetadata{
		ProjectedAt:     q.AsOf,
		AsOf:            q.AsOf,
		FreshnessStatus: "green",
		ServingState:    "canonical",
		Stale:           false,
	}
}

func (r *Repository) countByWorkStateCanonical(ctx context.Context, args []any) ([]domain.CountByWorkState, int64, error) {
	countRows, err := r.pool.Query(ctx, processIntegrityCanonicalCountsSQL, append([]any{pgx.QueryExecModeExec}, args...)...)
	if err != nil {
		return nil, 0, fmt.Errorf("processintegrity: count canonical rows: %w", err)
	}
	defer countRows.Close()
	counts := []domain.CountByWorkState{}
	var totalCount int64
	for countRows.Next() {
		var state string
		var count int64
		if err := countRows.Scan(&state, &count); err != nil {
			return nil, 0, fmt.Errorf("processintegrity: scan canonical count: %w", err)
		}
		counts = append(counts, domain.CountByWorkState{WorkState: domain.WorkState(state), Count: count})
		totalCount += count
	}
	if err := countRows.Err(); err != nil {
		return nil, 0, fmt.Errorf("processintegrity: iterate canonical counts: %w", err)
	}
	return counts, totalCount, nil
}

func (r *Repository) GetRow(ctx context.Context, q domain.Query, rowID string) (domain.Row, bool, error) {
	q.RowID = &rowID
	q.Limit = 1
	q.IncludeCompleted = true
	result, err := r.ListRows(ctx, q)
	if err != nil {
		return domain.Row{}, false, err
	}
	if len(result.Rows) == 0 {
		return domain.Row{}, false, nil
	}
	return result.Rows[0], true, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(rows rowScanner) (domain.Row, domain.Cursor, error) {
	var row domain.Row
	var batchID, taskID, submissionID, completionID, cohortID, goatID, driveName, sopVersionID pgtype.Text
	var taskRowVersion pgtype.Int4
	var windowStart, windowEnd, latestEvidenceAt, driveLatestSafeDate pgtype.Timestamptz
	var batchStatus, submissionState, completionState, blockerReason, latestRejection, auditRef pgtype.Text
	var driveCapacityState, driveMedicalDeferReason pgtype.Text
	var driveAnimalsRequired, driveAnimalsAssigned, driveOperatorCap, driveAvailableOperators pgtype.Int4
	var operatorID, operatorName, parkHeadID, parkHeadName, verifierID, verifierName, escalationOwnerID, escalationOwnerName pgtype.Text
	var dueAt pgtype.Timestamptz
	var proofIDsCSV string
	var sortPriority int
	var sopState, proofState, verificationState, ownerState, workState, severity string
	if err := rows.Scan(
		&sortPriority,
		&row.RowID,
		&row.ProcessKey,
		&row.Category,
		&row.ObligationID,
		&batchID,
		&taskID,
		&taskRowVersion,
		&submissionID,
		&completionID,
		&row.ParkID,
		&row.ParkName,
		&row.ShedID,
		&row.ShedName,
		&cohortID,
		&goatID,
		&row.AnimalStage,
		&row.ProtocolID,
		&row.ProtocolVersionID,
		&row.RuleID,
		&row.ProtocolName,
		&row.DoseCode,
		&driveName,
		&sopVersionID,
		&row.ProofPolicy,
		&dueAt,
		&windowStart,
		&windowEnd,
		&row.ExpectedCount,
		&driveCapacityState,
		&driveAnimalsRequired,
		&driveAnimalsAssigned,
		&driveOperatorCap,
		&driveAvailableOperators,
		&driveLatestSafeDate,
		&driveMedicalDeferReason,
		&row.ObligationStatus,
		&batchStatus,
		&sopState,
		&submissionState,
		&proofState,
		&verificationState,
		&completionState,
		&row.CompletedCount,
		&row.ProofCount,
		&row.RejectedCount,
		&row.DeferredCount,
		&workState,
		&row.GapType,
		&severity,
		&blockerReason,
		&ownerState,
		&row.NextAction,
		&row.ProcessIntact,
		&operatorID,
		&operatorName,
		&parkHeadID,
		&parkHeadName,
		&verifierID,
		&verifierName,
		&escalationOwnerID,
		&escalationOwnerName,
		&proofIDsCSV,
		&row.Evidence.EvidenceCount,
		&latestEvidenceAt,
		&latestRejection,
		&auditRef,
	); err != nil {
		return domain.Row{}, domain.Cursor{}, fmt.Errorf("processintegrity: scan row: %w", err)
	}
	row.BatchID = textPtr(batchID)
	row.SOPTaskID = textPtr(taskID)
	row.SOPTaskVersion = int32Ptr(taskRowVersion)
	row.SOPSubmissionID = textPtr(submissionID)
	row.CompletionID = textPtr(completionID)
	row.CohortID = textPtr(cohortID)
	row.GoatID = textPtr(goatID)
	row.DriveName = textPtr(driveName)
	row.SOPVersionID = textPtr(sopVersionID)
	row.DueAt = dueAt.Time
	row.WindowStart = timePtr(windowStart)
	row.WindowEnd = timePtr(windowEnd)
	if driveCapacityState.Valid {
		row.DriveCapacityState = domain.DriveCapacityState(driveCapacityState.String)
	}
	row.DriveAnimalsRequired = int32Value(driveAnimalsRequired)
	row.DriveAnimalsAssigned = int32Value(driveAnimalsAssigned)
	row.DriveOperatorCap = int32Value(driveOperatorCap)
	row.DriveAvailableOperators = int32Value(driveAvailableOperators)
	row.DriveLatestSafeDate = timePtr(driveLatestSafeDate)
	row.DriveMedicalDeferReason = textPtr(driveMedicalDeferReason)
	row.BatchStatus = textPtr(batchStatus)
	row.SOPTaskState = domain.SOPState(sopState)
	row.SubmissionState = textPtr(submissionState)
	row.ProofState = domain.ProofState(proofState)
	row.VerificationState = domain.VerificationState(verificationState)
	row.CompletionState = textPtr(completionState)
	row.WorkState = domain.WorkState(workState)
	row.Severity = domain.Severity(severity)
	row.BlockerReason = textPtr(blockerReason)
	row.OwnerState = domain.OwnerState(ownerState)
	row.Owner = domain.Owner{
		OperatorID:          textPtr(operatorID),
		OperatorName:        textPtr(operatorName),
		ParkHeadID:          textPtr(parkHeadID),
		ParkHeadName:        textPtr(parkHeadName),
		VerifierID:          textPtr(verifierID),
		VerifierName:        textPtr(verifierName),
		EscalationOwnerID:   textPtr(escalationOwnerID),
		EscalationOwnerName: textPtr(escalationOwnerName),
	}
	row.Evidence.ProofIDs = splitCSV(proofIDsCSV)
	row.Evidence.LatestEvidenceAt = timePtr(latestEvidenceAt)
	row.Evidence.LatestRejectionReason = textPtr(latestRejection)
	row.Evidence.AuditRef = textPtr(auditRef)
	return row, domain.Cursor{SortPriority: sortPriority, DueAt: row.DueAt, RowID: row.RowID}, nil
}

func normalizeQuery(q domain.Query) domain.Query {
	if q.Limit <= 0 {
		q.Limit = defaultLimit
	}
	if q.Limit > maxLimit {
		q.Limit = maxLimit
	}
	if q.AsOf.IsZero() {
		q.AsOf = time.Now().In(biztime.DefaultLocation())
	}
	if q.DueBefore.IsZero() {
		q.DueBefore = q.AsOf.Add(30 * 24 * time.Hour)
	}
	// Process-integrity due filters are an operational business-date contract. Normalizing both row
	// and summary reads to the same IST day boundaries keeps totals exact while allowing the projector
	// to collapse arbitrarily many same-day obligations into bounded summary grains.
	if q.DueAfter != nil {
		start := startOfBusinessDay(*q.DueAfter)
		q.DueAfter = &start
	}
	q.DueBefore = startOfBusinessDay(q.DueBefore).Add(24*time.Hour - time.Nanosecond)
	return q
}

func startOfBusinessDay(t time.Time) time.Time {
	local := t.In(biztime.DefaultLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, biztime.DefaultLocation())
}

func queryArgs(q domain.Query) []any {
	closedAfter := startOfBusinessDay(q.AsOf.Add(-defaultClosedHistoryAge))
	cursorSort := -1
	cursorDue := pgtype.Timestamptz{}
	cursorRow := ""
	if q.Cursor != nil {
		cursorSort = q.Cursor.SortPriority
		cursorDue = pgtype.Timestamptz{Time: q.Cursor.DueAt, Valid: true}
		cursorRow = q.Cursor.RowID
	}
	args := []any{
		q.TenantID,
		textValue(q.ParkID),
		textValue(q.ShedID),
		timestamptzValue(q.DueAfter),
		pgtype.Timestamptz{Time: q.DueBefore, Valid: true},
		textEnum(q.WorkState),
		textEnum(q.Severity),
		textValue(q.OwnerID),
		textValue(q.ProtocolVersionID),
		pgtype.Timestamptz{Time: q.AsOf, Valid: true},
		pgtype.Timestamptz{Time: closedAfter, Valid: true},
		textValue(q.RowID),
		q.OnlyBrokenOrAtRisk,
		q.IncludeCompleted,
		textValue(q.Category),
		cursorSort,
		cursorDue,
		cursorRow,
		int32(q.Limit + 1),
	}
	if len(args) != rowsQueryArgCount {
		panic(fmt.Sprintf("processintegrity: query arg count drifted: got %d want %d", len(args), rowsQueryArgCount))
	}
	return args
}

func countQueryArgs(args []any) []any {
	if len(args) != rowsQueryArgCount {
		panic(fmt.Sprintf("processintegrity: rows arg count drifted: got %d want %d", len(args), rowsQueryArgCount))
	}
	if countQueryArgCount <= 0 || countQueryArgCount >= rowsQueryArgCount {
		panic(fmt.Sprintf("processintegrity: count arg boundary drifted: count=%d rows=%d", countQueryArgCount, rowsQueryArgCount))
	}
	return args[:countQueryArgCount]
}

func textValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func textEnum[T ~string](v *T) string {
	if v == nil {
		return ""
	}
	return string(*v)
}

func timestamptzValue(v *time.Time) pgtype.Timestamptz {
	if v == nil || v.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *v, Valid: true}
}

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

func int32Value(v pgtype.Int4) int {
	if !v.Valid {
		return 0
	}
	return int(v.Int32)
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return []string{}
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// processIntegrityBaseSQL is point-in-time correct as of $10 (as_of) for the dose/evidence state it can
// reconstruct: completions and SOP-submission evidence are bounded by as_of, and obligation status is
// reconstructed AT as_of (completed via completed_at/as_of-bounded completion; missed/waived/deferred via the latest
// terminal event AT OR BEFORE as_of in the obligation_status_events log; scheduled/due/overdue from
// due_at/window) instead of being read off the current obligation_instances.status. A missed/waived/deferred row is
// only treated as terminal when a terminal event is proven at/before as_of; an obligation whose terminal
// events are all after as_of re-buckets to open, while one with no terminal history at all falls back to its
// current stored status (explicit, see asof_terminal). Documented residuals (no event history to reconstruct
// exactly): (a) sop_tasks.state and obligation_batches.status remain current-state, so a task/batch
// transition recorded after as_of is trusted as-is; (b) a missed->reschedule->missed churn is not reopen-
// aware. Full task/batch + reopen event-history replay is a later pass.
// projection-review: membership=obligation_instances rows for the tenant in the $4/$5 due window (one obligation per goat/rule/dose), collapsed in the grouped CTE to one grain per park/shed/batch/rule/protocol/business-date; group_key=(park_uuid, shed_uuid, batch_id, rule_id, protocol_id, protocol_version_id, protocol_name, dose_code, unbatched-business-date) with batched due/window display sourced from obligation_batches.planned_date and unbatched rows falling back to obligation due_at/window_start; join_cardinality=completions/asof_terminal deduplicated to 1:1 via DISTINCT ON / ARRAY_AGG-[1] before the join and the goat/sop/batch joins are keyed 1:1, so the COUNT/SUM in the grouped and aggregate wrappers cannot fan-out double-count; pagination=aggregate wrappers (counts/adherence) produce full-window totals independent of the LIST keyset/limit; scope=park/shed via located.park_uuid/shed_uuid + $2/$3, protocol via $9, owner via $8, category via $15, with the every-status buckets driven by the as_of-effective eff_status/work_state.
// scale-guard:ignore: 5k-50k operational-kernel envelope (docs/decisions/operational-kernel-5k-50k-scale-envelope.md). This base join is the canonical request-path read AND the off-request projector recompute source. It is tenant-scoped, bounded by the $4/$5 due window on the tenant+due_at index, keyset-paginated ($16-$19) at the LIST wrapper, and its aggregate wrappers pre-group in the DB — not an unbounded compute-on-read. The projection tables it also feeds are retained additively and removed in unit U7.
const processIntegrityBaseSQL = `
WITH completions AS (
  -- One effective completion per obligation, as_of-bounded. Migration 000082 keeps rejected/reversed
  -- history alongside one active row, so a direct join fans out (double-counting rework) and also leaks
  -- doses recorded AFTER as_of. Bound by event time (administered_at, falling back to created_at) <= as_of,
  -- exclude reversed, and prefer the active (recorded/accepted) attempt, else the latest historical one.
  SELECT DISTINCT ON (obligation_id)
    obligation_id,
    completion_id,
    asof_status AS completion_status,
    -- Only a verification PROVEN to be after as_of (downgrade) is stripped of its verifier/rejection at
    -- as_of. A NULL verified_at is no proof, so the stored status/fields are trusted (no faked history).
    CASE WHEN downgrade THEN NULL ELSE verified_by END AS verified_by,
    CASE WHEN downgrade THEN NULL ELSE verified_at END AS verified_at,
    CASE WHEN downgrade THEN NULL ELSE rejection_reason END AS rejection_reason,
    completion_updated_at
  FROM (
    SELECT
      obligation_id, completion_id, verified_by, verified_at, rejection_reason,
      updated_at AS completion_updated_at, administered_at, created_at,
      (verified_at IS NOT NULL AND verified_at > $10::timestamptz) AS downgrade,
      -- as_of correctness on the VERIFICATION: accept/reject PROVEN after as_of was only 'recorded' at as_of.
      CASE
        WHEN status IN ('accepted', 'rejected') AND verified_at IS NOT NULL AND verified_at > $10::timestamptz THEN 'recorded'
        ELSE status
      END AS asof_status
    FROM vaccination_completions
    WHERE tenant_id = $1::uuid
      AND status <> 'reversed'
      AND COALESCE(administered_at, created_at) <= $10::timestamptz
  ) c
  ORDER BY obligation_id,
    CASE WHEN asof_status IN ('recorded', 'accepted') THEN 0 ELSE 1 END,
    administered_at DESC NULLS LAST,
    created_at DESC
),
asof_terminal AS (
  -- Latest TERMINAL transition (missed/waived/deferred: statuses with no timestamp column on obligation_instances)
  -- AT OR BEFORE as_of, from the append-only event log. asof_terminal_type is the terminal status in effect
  -- at as_of (latest of missed/waived/deferred <= as_of, so sequences resolve to whichever was last
  -- at as_of). It is NULL when the obligation's only terminal events are AFTER as_of (it was still open at
  -- as_of); has_terminal_event then distinguishes that "future-only" case from "no terminal history at all"
  -- (row absent -> cannot reconstruct -> trust the current stored status, documented residual). Restricted
  -- to missed/waived/deferred, which are exceptions at herd scale, so this stays small and index-bound; open buckets
  -- need no log (derived from due_at/window). Residual: a missed->reschedule->missed churn is not reopen-
  -- aware (no reopen event type yet), so the last terminal event at/before as_of wins; full multi-transition
  -- replay (incl. sop_tasks/obligation_batches history) is the deeper task/batch pass.
  SELECT
    obligation_id,
    (ARRAY_AGG(event_type ORDER BY occurred_at DESC, obligation_event_id DESC)
       FILTER (WHERE occurred_at <= $10::timestamptz))[1] AS asof_terminal_type,
    true AS has_terminal_event
  FROM obligation_status_events
  WHERE tenant_id = $1::uuid
    AND event_type IN ('missed', 'waived', 'deferred')
  GROUP BY obligation_id
),
capacity_cfg AS (
  SELECT COALESCE((SELECT max_per_day FROM vaccination_capacity_config WHERE tenant_id = $1::uuid), 200)::int AS max_per_day
),
-- projection-review: membership=obligation_instances after tenant/category/date filtering, optionally decorated with one generated drive-assignment row for the same batch+shed; group_key=obligation_id at raw grain before grouped CTE collapses to park/shed/batch/rule/date; join_cardinality=vaccination_drive_assignments is keyed by batch plus exact shed/partition, so it decorates the obligation's own shed assignment without multiplying obligation membership; pagination=raw feeds grouped/all_rows keyset and full-window aggregates, no page-local count; scope=park/shed/protocol/owner/category filters remain explicit downstream.
raw AS (
  SELECT
    oi.obligation_id,
    oi.protocol_version_id,
    oi.rule_id,
    oi.batch_id,
    oi.sop_task_id AS obligation_sop_task_id,
    oi.target_type,
    oi.target_id,
    oi.scope_type,
    oi.scope_id,
    oi.due_at,
    oi.window_start,
    oi.window_end,
    oi.status AS obligation_status,
    oi.created_at AS obligation_created_at,
    pv.protocol_id,
    COALESCE(pr.sop_version_id, pv.sop_version_id) AS configured_sop_version_id,
    COALESCE(NULLIF(pr.proof_policy, '{}'::jsonb), pv.proof_policy, '{}'::jsonb)::text AS proof_policy,
    pv.published_at,
    pd.name AS protocol_name,
    pr.dose_code,
    (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS batch_planned_at,
    ob.status AS batch_status,
    ob.sop_task_id AS batch_sop_task_id,
    COALESCE(vda.operator_id, ob.conducted_by) AS conducted_by,
    vda.assignment_planned_at,
    ob.created_at AS batch_created_at,
    st.task_id,
    st.row_version AS task_row_version,
    st.state AS task_state,
    st.assigned_to,
    st.sop_version_id AS task_sop_version_id,
    ss.submission_id,
    ss.state AS submission_state,
    ss.proof_refs,
    ss.submitted_at,
    ss.accepted_at,
    g.goat_id,
    g.lifecycle_status AS goat_lifecycle_status,
    g.health_status AS goat_health_status,
    g.management_stage AS goat_stage,
    g.cohort_id AS goat_cohort_id,
    oi.completed_at,
    te.asof_terminal_type,
    te.has_terminal_event,
    c.completion_id,
    c.completion_status,
    c.verified_by,
    c.verified_at,
    c.rejection_reason,
    c.completion_updated_at,
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
  LEFT JOIN LATERAL (
    SELECT
      assignment.operator_id,
      (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
    FROM vaccination_drive_assignments assignment
    WHERE assignment.tenant_id = oi.tenant_id
      AND assignment.batch_id = oi.batch_id
      AND assignment.shed_id = CASE
        WHEN g.shed_id IS NOT NULL THEN g.shed_id
        WHEN oi.target_type = 'shed' THEN oi.target_id
        WHEN oi.scope_type = 'shed' THEN oi.scope_id
        ELSE NULL
      END
    ORDER BY assignment.planned_date ASC,
             assignment.partition_label ASC,
             assignment.operator_id ASC NULLS LAST,
             assignment.assignment_id ASC
    LIMIT 1
  ) vda ON true
  LEFT JOIN sop_tasks st
    ON st.tenant_id = oi.tenant_id
   AND st.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
  LEFT JOIN LATERAL (
    SELECT submission_id, state, proof_refs, submitted_at, accepted_at
    FROM sop_submissions sub
    WHERE sub.tenant_id = oi.tenant_id
      AND sub.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
      -- as_of correctness: evidence submitted AFTER as_of must not be seen.
      AND sub.submitted_at <= $10::timestamptz
    ORDER BY sub.submitted_at DESC, sub.submission_id DESC
    LIMIT 1
  ) ss ON true
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND ($4::timestamptz IS NULL OR COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) >= $4::timestamptz)
    AND COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) <= $5::timestamptz
    AND (
      oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed', 'waived')
      OR $14::boolean
      OR oi.due_at >= $11::timestamptz
      -- A row that is 'completed' NOW but finalized AFTER as_of was still open at as_of, so it must be
      -- pulled (even if old) to re-bucket; completed_at > as_of only matches historical as_of queries, so
      -- the common as_of = now path pulls no extra closed history.
      OR (oi.status = 'completed' AND oi.completed_at > $10::timestamptz)
      -- A row completed within the closed-history window ending at as_of must also be pulled so a
      -- just-completed alert shows as 'completed' even when its due_at is older than the window. The
      -- recency key for completed work is completion time ($11 = as_of - closed-history-age), NOT due
      -- date: a batch drive can finalize an obligation whose due_at is weeks old, and at as_of = now that
      -- row would otherwise vanish from the projection entirely (NEW-E2E-001).
      OR (oi.status = 'completed' AND oi.completed_at >= $11::timestamptz AND oi.completed_at <= $10::timestamptz)
    )
),
located AS (
  SELECT
    raw.*,
    COALESCE(raw.direct_park_uuid, shed_loc.parent_location_id) AS park_uuid,
    COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AS execution_due_at,
    -- as_of-effective obligation status. Reconstructs the state AT as_of instead of reading the current
    -- obligation_instances.status, so a transition recorded after as_of is not treated as already true.
    CASE
      WHEN raw.obligation_status = 'completed' THEN
        CASE
          WHEN raw.completed_at IS NOT NULL AND raw.completed_at <= $10::timestamptz THEN 'completed'
          WHEN raw.completed_at IS NULL AND raw.completion_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN (COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date < ($10::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue' WHEN COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.window_start, raw.due_at) <= $10::timestamptz THEN 'due' ELSE 'scheduled' END)
        END
      WHEN raw.obligation_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          -- The latest terminal transition at/before as_of was in effect at as_of.
          WHEN raw.asof_terminal_type IS NOT NULL THEN raw.asof_terminal_type
          -- Terminal events exist but only AFTER as_of: the obligation was still open at as_of.
          WHEN raw.has_terminal_event THEN (CASE WHEN (COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date < ($10::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue' WHEN COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.window_start, raw.due_at) <= $10::timestamptz THEN 'due' ELSE 'scheduled' END)
          -- No terminal history at all: cannot reconstruct, trust the current stored status (documented residual).
          ELSE raw.obligation_status
        END
      WHEN raw.obligation_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN (COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date < ($10::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue' WHEN COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.window_start, raw.due_at) <= $10::timestamptz THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM raw
  LEFT JOIN locations shed_loc
    ON shed_loc.tenant_id = $1::uuid
   AND shed_loc.location_id = raw.shed_uuid
   AND shed_loc.location_type = 'shed'
  WHERE raw.shed_uuid IS NOT NULL
),
-- projection-review: membership=located obligation-grain rows after tenant/category/scope resolution, with batch planned_date carried as execution_due_at for batched rows; group_key=(park_uuid,shed_uuid,batch_id,rule_id,protocol_id,protocol_version_id,protocol_name,dose_code,unbatched-business-date); join_cardinality=located already contains one row per obligation and only 1:1 goat/batch/task/completion joins, so COUNT/ARRAY_AGG cannot multiply expected counts; pagination=grouped/all_rows feed keyset list plus full-window count/adherence aggregates independent of page size; scope=park/shed/protocol/owner/category filters are applied before grouping.
grouped AS (
  SELECT
    located.park_uuid,
    located.shed_uuid,
    located.batch_id,
    located.rule_id,
    located.protocol_id,
    located.protocol_version_id,
    located.protocol_name,
    located.dose_code,
    MIN(located.obligation_id::text) AS obligation_id,
    (ARRAY_AGG(located.task_id::text ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST) FILTER (WHERE located.task_id IS NOT NULL))[1] AS task_id,
    (ARRAY_AGG(located.task_row_version ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST) FILTER (WHERE located.task_id IS NOT NULL))[1] AS task_row_version,
    (ARRAY_AGG(located.submission_id::text ORDER BY located.submitted_at DESC NULLS LAST) FILTER (WHERE located.submission_id IS NOT NULL))[1] AS submission_id,
    (ARRAY_AGG(located.completion_id::text ORDER BY located.completion_updated_at DESC NULLS LAST) FILTER (WHERE located.completion_id IS NOT NULL))[1] AS completion_id,
    CASE WHEN COUNT(DISTINCT located.goat_id) = 1 THEN MAX(located.goat_id::text) ELSE NULL END AS goat_id,
    CASE WHEN COUNT(DISTINCT located.goat_cohort_id) = 1 THEN MAX(located.goat_cohort_id::text) ELSE NULL END AS cohort_id,
    COALESCE(MAX(located.configured_sop_version_id::text), MAX(located.task_sop_version_id::text)) AS sop_version_id,
    MAX(located.proof_policy) AS proof_policy,
    MIN(located.execution_due_at) AS execution_due_at,
    MIN(located.execution_due_at) AS due_at,
    MIN(COALESCE(located.batch_planned_at, located.window_start)) AS window_start,
    MAX(located.window_end) AS window_end,
    COUNT(*)::int AS expected_count,
    COUNT(DISTINCT located.goat_id)::int AS expected_animals,
    -- Representative status + bucket counts use the as_of-effective status, not the stored status.
    (ARRAY_AGG(located.eff_status ORDER BY
      CASE located.eff_status
        WHEN 'overdue' THEN 0
        WHEN 'missed' THEN 1
        WHEN 'in_progress' THEN 2
        WHEN 'due' THEN 3
        WHEN 'scheduled' THEN 4
        WHEN 'deferred' THEN 5
        WHEN 'waived' THEN 6
        WHEN 'completed' THEN 7
        ELSE 8
      END,
      located.execution_due_at ASC,
      located.due_at ASC
    ))[1] AS obligation_status,
    COUNT(*) FILTER (WHERE located.eff_status = 'scheduled')::int AS scheduled_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'due')::int AS due_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'in_progress')::int AS in_progress_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'completed')::int AS completed_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'missed')::int AS missed_count,
    COUNT(*) FILTER (WHERE located.eff_status IN ('waived', 'deferred'))::int AS deferred_count,
    COUNT(*) FILTER (WHERE located.completion_status = 'recorded')::int AS completion_recorded,
    COUNT(*) FILTER (WHERE located.completion_status = 'accepted')::int AS completion_accepted,
    COUNT(*) FILTER (WHERE located.completion_status = 'rejected')::int AS completion_rejected,
    (ARRAY_AGG(located.completion_status ORDER BY located.completion_updated_at DESC NULLS LAST) FILTER (WHERE located.completion_status IS NOT NULL))[1] AS completion_state,
    (ARRAY_AGG(located.batch_status ORDER BY
      CASE located.batch_status
        WHEN 'in_progress' THEN 0
        WHEN 'planned' THEN 1
        WHEN 'completed' THEN 2
        WHEN 'superseded' THEN 3
        WHEN 'canceled' THEN 4
        ELSE 5
      END,
      located.execution_due_at DESC NULLS LAST,
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
      located.execution_due_at DESC NULLS LAST,
      located.due_at DESC NULLS LAST
    ) FILTER (WHERE located.task_state IS NOT NULL))[1] AS task_state,
    (ARRAY_AGG(located.submission_state ORDER BY located.submitted_at DESC NULLS LAST) FILTER (WHERE located.submission_state IS NOT NULL))[1] AS submission_state,
    (ARRAY_AGG(located.proof_refs ORDER BY located.submitted_at DESC NULLS LAST) FILTER (WHERE located.proof_refs IS NOT NULL))[1] AS latest_proof_refs,
    MAX(jsonb_array_length(COALESCE(located.proof_refs, '[]'::jsonb)))::int AS proof_count,
    MAX(located.submitted_at) AS latest_evidence_at,
    (ARRAY_AGG(located.rejection_reason ORDER BY located.completion_updated_at DESC NULLS LAST) FILTER (WHERE located.rejection_reason IS NOT NULL AND located.rejection_reason <> ''))[1] AS latest_rejection_reason,
    (ARRAY_AGG(located.conducted_by::text ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST) FILTER (WHERE located.conducted_by IS NOT NULL))[1] AS explicit_conducted_by,
    (ARRAY_AGG(located.assigned_to::text ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST) FILTER (WHERE located.assigned_to IS NOT NULL))[1] AS assigned_to,
    (ARRAY_AGG(located.verified_by::text ORDER BY located.verified_at DESC NULLS LAST) FILTER (WHERE located.verified_by IS NOT NULL))[1] AS verified_by,
    COALESCE(MAX(stage.stage_code), MAX(stage.name), MAX(located.goat_stage), 'Unknown') AS animal_stage,
    COUNT(*) FILTER (
      WHERE located.eff_status NOT IN ('waived', 'deferred')
        AND (
          located.goat_lifecycle_status IN ('sick', 'under_treatment', 'quarantine', 'icu')
          OR COALESCE(located.goat_health_status, '') IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
        )
    )::int AS health_deferred_count
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
  WHERE located.park_uuid IS NOT NULL
    AND ($2::text = '' OR located.park_uuid = $2::uuid)
    AND ($3::text = '' OR located.shed_uuid = $3::uuid)
    AND ($9::text = '' OR located.protocol_version_id = $9::uuid)
  GROUP BY located.park_uuid, located.shed_uuid, located.batch_id, located.rule_id, located.protocol_id, located.protocol_version_id, located.protocol_name, located.dose_code,
    CASE WHEN located.batch_id IS NULL THEN (located.execution_due_at AT TIME ZONE 'Asia/Kolkata')::date ELSE NULL END
),
enriched AS (
  SELECT
    grouped.*,
    grouped.explicit_conducted_by AS conducted_by,
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
      WHEN enriched.expected_count > 0
       AND enriched.completed_count = enriched.expected_count
       AND enriched.completion_rejected = 0
       AND enriched.completion_recorded = 0 THEN 'completed'
      WHEN enriched.completion_rejected > 0 THEN 'rejected'
      WHEN NOT enriched.usable_for_vaccination THEN 'blocked'
      WHEN enriched.deferred_count > 0
        OR enriched.health_deferred_count > 0
        OR enriched.is_quarantine
        OR enriched.is_icu THEN 'deferred'
      WHEN enriched.missed_count > 0 THEN 'missed'
      WHEN enriched.conducted_by IS NULL
       AND enriched.assigned_to IS NULL
       AND enriched.completed_count < enriched.expected_count THEN 'blocked'
      WHEN enriched.task_state IN ('rework_requested', 'rejected') THEN 'rejected'
      WHEN enriched.completion_recorded > 0
        OR enriched.task_state IN ('submitted', 'needs_review') THEN 'verification_pending'
      WHEN enriched.in_progress_count > 0
        OR enriched.batch_status = 'in_progress'
        OR enriched.task_state = 'in_progress' THEN 'in_progress'
      WHEN (enriched.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($10::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue'
      WHEN enriched.due_count > 0 THEN 'due'
      ELSE 'scheduled'
    END AS work_state
  FROM enriched
),
derived AS (
  SELECT
    stateful.*,
    CASE stateful.work_state
      WHEN 'completed' THEN 'ok'
      WHEN 'scheduled' THEN 'watch'
      WHEN 'due' THEN 'watch'
      WHEN 'in_progress' THEN 'watch'
      WHEN 'deferred' THEN 'watch'
      WHEN 'verification_pending' THEN 'watch'
      WHEN 'proof_pending' THEN 'at_risk'
      WHEN 'overdue' THEN 'at_risk'
      ELSE 'broken'
    END AS severity,
    CASE stateful.work_state
      WHEN 'completed' THEN ''
      WHEN 'scheduled' THEN ''
      WHEN 'due' THEN ''
      WHEN 'in_progress' THEN ''
      WHEN 'deferred' THEN 'deferred_explained'
      WHEN 'verification_pending' THEN 'verification_pending'
      WHEN 'proof_pending' THEN 'proof_missing'
      WHEN 'overdue' THEN 'overdue'
      WHEN 'rejected' THEN 'proof_rejected'
      WHEN 'blocked' THEN CASE WHEN stateful.missed_count > 0 THEN 'missed' ELSE 'blocked' END
      ELSE stateful.work_state
    END AS gap_type,
    CASE
      WHEN stateful.work_state IN ('completed', 'scheduled', 'due', 'in_progress', 'deferred') THEN true
      ELSE false
    END AS process_intact,
    CASE
      WHEN stateful.conducted_by IS NULL AND stateful.assigned_to IS NULL THEN 'missing'
      ELSE 'assigned'
    END AS owner_state,
    CASE
      WHEN stateful.batch_id IS NOT NULL THEN 'batch:' || stateful.batch_id::text || ':rule:' || stateful.rule_id::text || ':shed:' || stateful.shed_uuid::text
      ELSE 'obligation:' || stateful.obligation_id
    END AS row_id,
    CASE stateful.work_state
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
    END AS sort_priority,
    CASE
      WHEN NOT stateful.usable_for_vaccination THEN 'Shed is not marked usable for vaccination'
      WHEN stateful.is_icu THEN 'Shed is ICU; PC defer/approval required'
      WHEN stateful.is_quarantine THEN 'Shed is quarantine; PC defer/approval required'
      WHEN stateful.health_deferred_count > 0 THEN 'Some goats are sick, under treatment, recovering, quarantined, or in ICU'
      WHEN stateful.missed_count > 0 THEN 'Missed dose escalation required'
      WHEN stateful.conducted_by IS NULL AND stateful.assigned_to IS NULL AND stateful.completed_count < stateful.expected_count THEN 'Operator assignment required before execution'
      ELSE NULL
    END AS blocker_reason,
    CASE stateful.work_state
      WHEN 'completed' THEN 'No action - drive verified'
      WHEN 'rejected' THEN 'Review rejection and request rework'
      WHEN 'blocked' THEN CASE WHEN stateful.missed_count > 0 THEN 'Escalate missed dose to PC' ELSE 'Resolve blocker before execution' END
      WHEN 'deferred' THEN 'Confirm defer reason with PC'
      WHEN 'verification_pending' THEN 'Verifier to accept or reject proof'
      WHEN 'proof_pending' THEN 'Upload required SOP proof'
      WHEN 'in_progress' THEN 'Complete drive and submit proof'
      WHEN 'overdue' THEN 'Start SOP - overdue'
      WHEN 'due' THEN 'Start scheduled vaccination SOP'
      ELSE 'Monitor scheduled drive'
    END AS next_action,
    CASE
      WHEN stateful.completion_rejected > 0 THEN 'rework'
      WHEN stateful.task_state IN ('in_progress') THEN 'in_progress'
      WHEN stateful.task_state IN ('submitted', 'needs_review') THEN 'submitted'
      WHEN stateful.task_state = 'accepted' THEN 'accepted'
      WHEN stateful.task_state IN ('rework_requested', 'rejected') THEN 'rework'
      ELSE 'not_started'
    END AS sop_state,
    CASE
      WHEN stateful.completion_rejected > 0 THEN 'rejected'
      WHEN stateful.completion_accepted > 0 AND stateful.completion_recorded = 0 THEN 'accepted'
      WHEN stateful.proof_count > 0 OR stateful.completion_recorded > 0 OR stateful.task_state IN ('submitted', 'needs_review') THEN 'uploaded'
      ELSE 'missing'
    END AS proof_state,
    CASE
      WHEN stateful.completion_rejected > 0 THEN 'rejected'
      WHEN stateful.completion_recorded > 0 OR stateful.task_state IN ('submitted', 'needs_review') THEN 'pending'
      WHEN stateful.completion_accepted > 0 AND stateful.completed_count = stateful.expected_count THEN 'accepted'
      ELSE 'not_ready'
    END AS verification_state
  FROM stateful
),
with_locations AS (
  SELECT
    derived.*,
    park.name AS park_name,
    shed.name AS shed_name
  FROM derived
  JOIN locations park
    ON park.tenant_id = $1::uuid
   AND park.location_id = derived.park_uuid
  JOIN locations shed
    ON shed.tenant_id = $1::uuid
   AND shed.location_id = derived.shed_uuid
),
with_owners AS (
  SELECT
    with_locations.*,
    operator.workforce_member_id::text AS operator_id,
    operator.display_name AS operator_name,
    park_head.workforce_member_id::text AS park_head_id,
    park_head.display_name AS park_head_name,
    verifier.workforce_member_id::text AS verifier_id,
    verifier.display_name AS verifier_name,
    COALESCE(operator.workforce_member_id::text, park_head.workforce_member_id::text, verifier.workforce_member_id::text) AS escalation_owner_id,
    COALESCE(operator.display_name, park_head.display_name, verifier.display_name) AS escalation_owner_name
  FROM with_locations
  LEFT JOIN workforce_members operator
    ON operator.tenant_id = $1::uuid
   AND operator.workforce_member_id = COALESCE(with_locations.conducted_by::uuid, with_locations.assigned_to::uuid)
   AND operator.status = 'active'
  LEFT JOIN LATERAL (
    SELECT wm.workforce_member_id, wm.display_name
    FROM workforce_members wm
    WHERE wm.tenant_id = $1::uuid
      AND wm.status = 'active'
      AND wm.primary_role_hint = 'park_head'
      AND wm.primary_location_id IN (with_locations.shed_uuid, with_locations.park_uuid)
    ORDER BY CASE WHEN wm.primary_location_id = with_locations.shed_uuid THEN 0 ELSE 1 END, wm.updated_at DESC, wm.workforce_member_id DESC
    LIMIT 1
  ) park_head ON true
  LEFT JOIN LATERAL (
    SELECT wm.workforce_member_id, wm.display_name
    FROM workforce_members wm
    WHERE wm.tenant_id = $1::uuid
      AND wm.status = 'active'
      AND wm.primary_role_hint = 'verifier'
      AND (wm.primary_location_id IS NULL OR wm.primary_location_id IN (with_locations.shed_uuid, with_locations.park_uuid))
    ORDER BY CASE WHEN wm.primary_location_id = with_locations.shed_uuid THEN 0 WHEN wm.primary_location_id = with_locations.park_uuid THEN 1 ELSE 2 END,
             wm.updated_at DESC, wm.workforce_member_id DESC
    LIMIT 1
  ) verifier ON true
),
filtered AS (
  SELECT *
  FROM with_owners
  WHERE ($6::text = '' OR work_state = $6::text)
    AND ($7::text = '' OR severity = $7::text)
    AND (
      $8::text = ''
      OR operator_id = $8::text
      OR park_head_id = $8::text
      OR verifier_id = $8::text
      OR escalation_owner_id = $8::text
    )
    AND ($12::text = '' OR row_id = $12::text)
    AND (
      NOT $13::boolean
      OR work_state IN ('rejected', 'blocked', 'overdue', 'proof_pending', 'verification_pending')
    )
),
labeled AS (
  SELECT
    filtered.*,
    CASE
      WHEN vl.vaccine_label IS NULL THEN filtered.dose_code
      ELSE btrim(concat_ws(' ',
        vl.vaccine_label,
        CASE
          WHEN filtered.dose_code LIKE '%_adult_%' THEN 'adult course'
          WHEN filtered.dose_code LIKE '%_kid_%' THEN 'kid course'
          ELSE NULL
        END,
        CASE
          WHEN filtered.dose_code ~ '_w[0-9]+$' THEN 'dose ' || substring(filtered.dose_code from '_w([0-9]+)$')
          ELSE NULL
        END
      ))
    END AS dose_label
  FROM filtered
  LEFT JOIN LATERAL (
    SELECT ceo_ai.vaccine_label_for(filtered.dose_code) AS vaccine_label
  ) vl ON true
)
`

const processIntegrityFeedExceptionSQL = `,
feed_exception_rows AS (
  SELECT
    CASE
      WHEN e.status IN ('resolved', 'dismissed') THEN 10
      WHEN e.work_state = 'blocked' THEN 1
      ELSE 3
    END AS sort_priority,
    'feed_projection_exception:' || e.count_projection_exception_id::text AS row_id,
    'feed_projection_exception:' || e.count_projection_exception_id::text AS process_key,
    'feed_direction' AS category,
    e.count_projection_exception_id::text AS obligation_id,
    NULL::text AS batch_id,
    NULL::text AS sop_task_id,
    NULL::integer AS sop_task_row_version,
    NULL::text AS sop_submission_id,
    NULL::text AS completion_id,
    COALESCE(e.park_id::text, '') AS park_id,
    COALESCE(park.name, '') AS park_name,
    COALESCE(e.shed_id::text, '') AS shed_id,
    COALESCE(shed.name, '') AS shed_name,
    NULL::text AS cohort_id,
    NULL::text AS goat_id,
    COALESCE(NULLIF(e.stage_tag, ''), 'Counts/Shifting') AS animal_stage,
    ''::text AS protocol_id,
    ''::text AS protocol_version_id,
    ''::text AS rule_id,
    'Feed Direction Counts/Shifting' AS protocol_name,
    e.exception_type AS dose_code,
    NULLIF(
      'Feed exception - ' || e.exception_type ||
        CASE WHEN e.breed_key IS NOT NULL AND e.breed_key <> '' THEN ' / ' || e.breed_key ELSE '' END ||
        CASE WHEN e.stage_tag IS NOT NULL AND e.stage_tag <> '' THEN ' / ' || e.stage_tag ELSE '' END,
      ''
    ) AS drive_name,
    NULL::text AS sop_version_id,
    '{}'::text AS proof_policy,
    e.due_at,
    NULL::timestamptz AS window_start,
    NULL::timestamptz AS window_end,
    1::integer AS expected_count,
    e.status AS obligation_status,
    NULL::text AS batch_status,
    'not_started' AS sop_state,
    NULL::text AS submission_state,
    'not_required' AS proof_state,
    'not_ready' AS verification_state,
    e.status AS completion_state,
    CASE WHEN e.status IN ('resolved', 'dismissed') THEN 1 ELSE 0 END AS completed_count,
    0::integer AS proof_count,
    0::integer AS rejected_count,
    0::integer AS deferred_count,
    CASE
      WHEN e.status IN ('resolved', 'dismissed') THEN 'completed'
      ELSE e.work_state
    END AS work_state,
    e.exception_type AS gap_type,
    CASE
      WHEN e.status IN ('resolved', 'dismissed') THEN 'ok'
      WHEN e.severity = 'warning' THEN 'at_risk'
      ELSE 'broken'
    END AS severity,
    NULLIF(e.blocker_reason, '') AS blocker_reason,
    CASE WHEN e.owner_ref IS NULL OR e.owner_ref = '' THEN 'missing' ELSE 'assigned' END AS owner_state,
    CASE
      WHEN e.status IN ('resolved', 'dismissed') THEN 'No action - exception reviewed'
      ELSE e.next_action
    END AS next_action,
    e.status IN ('resolved', 'dismissed') AS process_intact,
    NULL::text AS operator_id,
    NULL::text AS operator_name,
    NULL::text AS park_head_id,
    NULL::text AS park_head_name,
    NULL::text AS verifier_id,
    NULL::text AS verifier_name,
    NULL::text AS escalation_owner_id,
    e.owner_ref AS escalation_owner_name,
    ''::text AS proof_ids,
    CASE WHEN e.evidence_json <> '{}'::jsonb THEN 1 ELSE 0 END AS evidence_count,
    e.updated_at AS latest_evidence_at,
    e.resolution_reason AS latest_rejection_reason,
    'count_projection_exception:' || e.count_projection_exception_id::text AS audit_ref
  FROM count_projection_exceptions e
  LEFT JOIN locations park
    ON park.tenant_id = e.tenant_id
   AND park.location_id = e.park_id
  LEFT JOIN locations shed
    ON shed.tenant_id = e.tenant_id
   AND shed.location_id = e.shed_id
  WHERE e.tenant_id = $1::uuid
    AND ($15::text = '' OR $15::text = 'feed_direction')
    AND ($2::text = '' OR e.park_id = $2::uuid)
    AND ($3::text = '' OR e.shed_id = $3::uuid)
    AND ($4::timestamptz IS NULL OR e.due_at >= $4::timestamptz)
    AND e.due_at <= $5::timestamptz
    AND ($9::text = '')
    AND (
      e.status = 'open'
      OR $14::boolean
      OR e.resolved_at >= $11::timestamptz
    )
    AND (
      $6::text = ''
      OR CASE WHEN e.status IN ('resolved', 'dismissed') THEN 'completed' ELSE e.work_state END = $6::text
    )
    AND (
      $7::text = ''
      OR CASE WHEN e.status IN ('resolved', 'dismissed') THEN 'ok' WHEN e.severity = 'warning' THEN 'at_risk' ELSE 'broken' END = $7::text
    )
    AND ($8::text = '' OR e.owner_ref = $8::text)
    AND ($12::text = '' OR 'feed_projection_exception:' || e.count_projection_exception_id::text = $12::text)
    AND (
      NOT $13::boolean
      OR CASE WHEN e.status IN ('resolved', 'dismissed') THEN 'completed' ELSE e.work_state END IN ('blocked')
    )
)
`

const processIntegrityAllRowsSQL = processIntegrityBaseSQL + processIntegrityFeedExceptionSQL + `,
all_rows AS (
  SELECT
    sort_priority,
    row_id,
    row_id AS process_key,
    'vaccination' AS category,
    obligation_id,
    batch_id::text AS batch_id,
    task_id AS sop_task_id,
    task_row_version AS sop_task_row_version,
    submission_id AS sop_submission_id,
    completion_id,
    park_uuid::text AS park_id,
    park_name,
    shed_uuid::text AS shed_id,
    shed_name,
    cohort_id,
    goat_id,
    animal_stage,
    protocol_id::text,
    protocol_version_id::text,
    rule_id::text,
    protocol_name,
    dose_label AS dose_code,
    NULLIF(protocol_name || CASE WHEN dose_label <> '' THEN ' - ' || dose_label ELSE '' END, '') AS drive_name,
    sop_version_id,
    proof_policy,
    execution_due_at AS due_at,
    window_start,
    window_end,
    expected_count,
    CASE
      WHEN work_state = 'deferred' THEN 'medical_defer'
      WHEN work_state IN ('completed', 'ok') THEN 'within_cap'
      WHEN COALESCE(window_end, due_at) <= $10::timestamptz
       AND expected_animals > (SELECT max_per_day FROM capacity_cfg) THEN 'over_cap_required'
      WHEN expected_animals > 0 THEN 'within_cap'
      ELSE 'not_planned'
    END AS drive_capacity_state,
    expected_animals::int AS drive_animals_required,
    CASE WHEN conducted_by IS NOT NULL OR assigned_to IS NOT NULL THEN expected_animals::int ELSE 0 END AS drive_animals_assigned,
    (SELECT max_per_day FROM capacity_cfg)::int AS drive_operator_cap,
    CASE WHEN conducted_by IS NOT NULL OR assigned_to IS NOT NULL THEN 1 ELSE 0 END::int AS drive_available_operators,
    COALESCE(window_end, due_at) AS drive_latest_safe_date,
    CASE
      WHEN work_state = 'deferred' THEN COALESCE(blocker_reason, 'medical_defer')
      ELSE NULL
    END AS drive_medical_defer_reason,
    obligation_status,
    batch_status,
    sop_state,
    submission_state,
    proof_state,
    verification_state,
    completion_state,
    completed_count,
    proof_count,
    completion_rejected AS rejected_count,
    deferred_count + health_deferred_count AS deferred_count,
    work_state,
    gap_type,
    severity,
    blocker_reason,
    owner_state,
    next_action,
    process_intact,
    operator_id,
    operator_name,
    park_head_id,
    park_head_name,
    verifier_id,
    verifier_name,
    escalation_owner_id,
    escalation_owner_name,
    COALESCE(array_to_string(ARRAY(
      SELECT elem->>'proof_id'
      FROM jsonb_array_elements(COALESCE(latest_proof_refs, '[]'::jsonb)) elem
      WHERE elem->>'proof_id' IS NOT NULL AND elem->>'proof_id' <> ''
    ), ','), '') AS proof_ids,
    proof_count AS evidence_count,
    latest_evidence_at,
    latest_rejection_reason,
    CASE WHEN submission_id IS NOT NULL THEN 'sop_submission:' || submission_id ELSE NULL END AS audit_ref
  FROM labeled
  WHERE ($15::text = '' OR $15::text = 'vaccination')
  UNION ALL
  SELECT
    sort_priority,
    row_id,
    process_key,
    category,
    obligation_id,
    batch_id,
    sop_task_id,
    sop_task_row_version,
    sop_submission_id,
    completion_id,
    park_id,
    park_name,
    shed_id,
    shed_name,
    cohort_id,
    goat_id,
    animal_stage,
    protocol_id,
    protocol_version_id,
    rule_id,
    protocol_name,
    dose_code,
    drive_name,
    sop_version_id,
    proof_policy,
    due_at,
    window_start,
    window_end,
    expected_count,
    NULL::text AS drive_capacity_state,
    NULL::int AS drive_animals_required,
    NULL::int AS drive_animals_assigned,
    NULL::int AS drive_operator_cap,
    NULL::int AS drive_available_operators,
    NULL::timestamptz AS drive_latest_safe_date,
    NULL::text AS drive_medical_defer_reason,
    obligation_status,
    batch_status,
    sop_state,
    submission_state,
    proof_state,
    verification_state,
    completion_state,
    completed_count,
    proof_count,
    rejected_count,
    deferred_count,
    work_state,
    gap_type,
    severity,
    blocker_reason,
    owner_state,
    next_action,
    process_intact,
    operator_id,
    operator_name,
    park_head_id,
    park_head_name,
    verifier_id,
    verifier_name,
    escalation_owner_id,
    escalation_owner_name,
    proof_ids,
    evidence_count,
    latest_evidence_at,
    latest_rejection_reason,
    audit_ref
  FROM feed_exception_rows
)
`

// Canonical request-path reads (5k-50k operational-kernel envelope,
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md). ListRows/CountByWorkState/GetRow serve
// Action Center, Control Tower, Protocol Adherence, and Workflow drilldowns directly from these canonical
// wrappers over processIntegrityAllRowsSQL — no derived projection, no freshness gate. The former
// projector (RecomputeProjection / processIntegrityProjectionInsertSQL) and the process_integrity_
// projection_rows/_state/_summaries tables it fed were removed (migrations 000187/000188).

// processIntegrityCanonicalRowsSQL is the LIST read: the canonical all_rows reconstruction, keyset-paginated
// on (sort_priority, due_at, row_id) with LIMIT $19. The base joins are tenant+due_at index-bound and all
// scope/state/owner/category filters ($2-$15) are applied inside all_rows, so the outer read only advances
// the cursor and bounds the page.
// scale-guard:ignore: 5k-50k operational-kernel envelope; canonical indexed keyset read (base joins index-bound + LIMIT $19), projection retired as request-path source per operational-kernel-5k-50k-scale-envelope.md.
const processIntegrityCanonicalRowsSQL = processIntegrityAllRowsSQL + `
SELECT
  sort_priority,
  row_id,
  process_key,
  category,
  obligation_id,
  batch_id,
  sop_task_id,
  sop_task_row_version,
  sop_submission_id,
  completion_id,
  park_id,
  park_name,
  shed_id,
  shed_name,
  cohort_id,
  goat_id,
  animal_stage,
  protocol_id,
  protocol_version_id,
  rule_id,
  protocol_name,
  dose_code,
  drive_name,
  sop_version_id,
  proof_policy,
  due_at,
  window_start,
  window_end,
  expected_count,
  drive_capacity_state,
  drive_animals_required,
  drive_animals_assigned,
  drive_operator_cap,
  drive_available_operators,
  drive_latest_safe_date,
  drive_medical_defer_reason,
  obligation_status,
  batch_status,
  sop_state,
  submission_state,
  proof_state,
  verification_state,
  completion_state,
  completed_count,
  proof_count,
  rejected_count,
  deferred_count,
  work_state,
  gap_type,
  severity,
  blocker_reason,
  owner_state,
  next_action,
  process_intact,
  operator_id,
  operator_name,
  park_head_id,
  park_head_name,
  verifier_id,
  verifier_name,
  escalation_owner_id,
  escalation_owner_name,
  proof_ids,
  evidence_count,
  latest_evidence_at,
  latest_rejection_reason,
  audit_ref
FROM all_rows
WHERE (
  $16::int < 0
  OR (sort_priority, due_at, row_id) > ($16::int, $17::timestamptz, $18::text)
)
  -- When IncludeCompleted is off ($14 false), a row that reads 'completed' at as_of but is due before the
  -- closed-history floor ($11 = as_of - closed-history age) is genuine closed history and must not appear in
  -- the default Action Center. The base CTE still PULLS it (by completion recency) so it can re-bucket for a
  -- past as_of probe; this outer clause hides it once it is settled closed history — matching the retired
  -- projection read filter exactly.
  AND ($14::boolean OR work_state <> 'completed' OR due_at >= $11::timestamptz)
ORDER BY sort_priority ASC, due_at ASC, row_id ASC
LIMIT $19;
`

// processIntegrityCanonicalCountsSQL is the NON-keyset indexed AGGREGATE: it collapses the whole canonical
// filtered set into one count per work_state. Membership is one all_rows grain (already GROUP BY'd in the
// base to park/shed/batch/rule/protocol/business-date), so COUNT(*) here is 1:1 with the LIST rows and the
// window total never depends on the LIST page size ($19 is not referenced). Args are countQueryArgs (the
// first 15 = $1..$15); the keyset args $16-$19 are intentionally absent.
// projection-review: membership=all_rows grouped grains (one row per park/shed/batch/rule/protocol/business-date); group_key=work_state; join_cardinality=base joins pre-aggregated to grains in processIntegrityBaseSQL grouped/all_rows before this COUNT so no fan-out; pagination=full-tenant aggregate independent of the LIST keyset/limit; scope=park/shed/protocol/owner/category filters applied inside all_rows ($2/$3/$9/$8/$15).
// scale-guard:ignore: 5k-50k operational-kernel envelope; canonical indexed aggregate over the tenant+due_at bounded base joins, projection retired as request-path source per operational-kernel-5k-50k-scale-envelope.md.
const processIntegrityCanonicalCountsSQL = processIntegrityAllRowsSQL + `
SELECT work_state, COUNT(*)::bigint AS row_count
FROM all_rows
WHERE ($14::boolean OR work_state <> 'completed' OR due_at >= $11::timestamptz)
GROUP BY work_state
ORDER BY work_state;
`

// processIntegrityCanonicalAdherenceSummarySQL is the Protocol Adherence AGGREGATE over the same canonical
// grains: expected/completed sums plus the process-intact split, all independent of the LIST page. The
// deferred total mirrors the projector grain (GREATEST(deferred_count, 1) on deferred-state grains) so a
// canonical read and a projector-built summary agree.
// projection-review: membership=all_rows grouped grains (one row per park/shed/batch/rule/protocol/business-date); group_key=none (single tenant summary row); join_cardinality=base joins pre-aggregated to grains before this SUM so no fan-out double-count; pagination=window totals independent of the LIST keyset/limit; scope=park/shed/protocol/owner/category filters applied inside all_rows ($2/$3/$9/$8/$15).
// scale-guard:ignore: 5k-50k operational-kernel envelope; canonical indexed aggregate over the tenant+due_at bounded base joins, projection retired as request-path source per operational-kernel-5k-50k-scale-envelope.md.
const processIntegrityCanonicalAdherenceSummarySQL = processIntegrityAllRowsSQL + `
SELECT
  COALESCE(SUM(expected_count), 0)::integer AS expected_count,
  COALESCE(SUM(completed_count), 0)::integer AS completed_count,
  COUNT(*) FILTER (WHERE NOT process_intact)::integer AS open_gap_count,
  COALESCE(SUM(CASE WHEN work_state = 'deferred' THEN GREATEST(deferred_count, 1) ELSE 0 END), 0)::integer AS deferred_count,
  COUNT(*) FILTER (WHERE process_intact)::integer AS process_intact_count
FROM all_rows
WHERE ($14::boolean OR work_state <> 'completed' OR due_at >= $11::timestamptz);
`

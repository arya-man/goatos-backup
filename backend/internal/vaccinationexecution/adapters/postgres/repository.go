// Package postgres implements canonical vaccination execution reads over Postgres.
// projection-review: membership=tenant-scoped vaccination obligation/completion rows selected by each endpoint's date window and scope filter; group_key=endpoint-specific park/shed/stage/batch/rule/protocol grain carried through SQL comments below; join_cardinality=completion and terminal status history are pre-aggregated per obligation before joining and goat/location/protocol joins are keyed by tenant plus stable ids; pagination=all serving reads use keyset or bounded cohort pages with totals computed before page truncation; scope=tenant plus explicit park/shed filters resolved from canonical location ids.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/ports"
)

// authorizedParkFilter returns the park ids a park-scoped actor may read, or nil when the caller is
// tenant-wide / grant-less (no restriction). Derived from the request-context grants so read
// queries enforce park scope in-query (defence in depth) without a signature change. A park-scoped
// actor with no resolvable parks gets a non-nil empty slice -> matches nothing.
func authorizedParkFilter(ctx context.Context, tenantID string) []string {
	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	if len(grants) == 0 || httpmiddleware.HasTenantWideGrant(grants, tenantID) {
		return nil
	}
	parks := httpmiddleware.AuthorizedParkIDs(grants)
	if parks == nil {
		return []string{}
	}
	return parks
}

const (
	defaultQueryTimeout     = 3 * time.Second
	defaultClosedHistoryAge = 45 * 24 * time.Hour
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
	page, err := r.ListVaccinationExecutionPage(ctx, q)
	return page.Rows, err
}

func (r *Repository) ListVaccinationExecutionPage(ctx context.Context, q domain.ExecutionQuery) (domain.ExecutionProjectionPage, error) {
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
	parkID := ""
	if q.ParkID != nil {
		parkID = *q.ParkID
	}
	shedID := ""
	if q.ShedID != nil {
		shedID = *q.ShedID
	}
	severity := ""
	if q.Severity != nil {
		severity = string(*q.Severity)
	}
	cursorPresent := q.Cursor != nil
	var cursorRank int
	var cursorDueMicros int64
	var cursorRowKey string
	if q.Cursor != nil {
		cursorRank = q.Cursor.SortRank
		cursorDueMicros = q.Cursor.SortDueMicros
		cursorRowKey = q.Cursor.SortRowKey
	}
	dueBefore := q.DueBefore
	if dueBefore.IsZero() {
		dueBefore = asOf.Add(defaultExecutionHorizon)
	}
	closedAfter := asOf.Add(-defaultClosedHistoryAge)
	// 5k-50k envelope (docs/decisions/operational-kernel-5k-50k-scale-envelope.md): serve the execution
	// list directly from the canonical obligation/completion/SOP tables via the keyset-paginated
	// vaccinationExecutionSQL instead of the vaccination_execution_projection_rows read model. A canonical
	// read cannot be stale relative to the canonical write, so the serving-projection freshness gate (and
	// its read-through-vs-503 failure mode) is removed. Freshness is nil (always current).
	rows, err := r.pool.Query(ctx, vaccinationExecutionSQL, pgx.QueryExecModeExec,
		q.TenantID, parkID, shedID, dueBefore, q.Limit, workState, asOf, closedAfter, severity,
		q.OpenOnly, cursorPresent, cursorRank, cursorDueMicros, cursorRowKey, q.OperatorScopeActorID)
	if err != nil {
		return domain.ExecutionProjectionPage{}, fmt.Errorf("vaccination execution: list vaccination execution: %w", err)
	}
	defer rows.Close()
	return scanExecutionProjectionPage(rows, q.Limit)
}

func scanExecutionProjectionPage(rows pgx.Rows, limit int) (domain.ExecutionProjectionPage, error) {
	out := []domain.ExecutionProjection{}
	var totalCount int64
	for rows.Next() {
		var p domain.ExecutionProjection
		var batchID, batchStatus, taskState, operatorName, parkHeadName, verifierName pgtype.Text
		var obligationID, sopTaskID, sopVersionID, completionID pgtype.Text
		var sopTaskRowVersion pgtype.Int4
		var dueAt pgtype.Timestamptz
		var obligationCount, scheduledCount, dueCount, inProgressCount, completedCount int64
		var missedCount, deferredCount, canceledCount, recordedCount, acceptedCount int64
		var rejectedCount, reversedCount, healthDeferredCount int64
		var workState string
		if err := rows.Scan(
			&p.ParkID,
			&p.ParkName,
			&p.ShedID,
			&p.ShedName,
			&p.PhysicalShed,
			&p.Partition,
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
			&workState,
			&totalCount,
			&p.SortRank,
			&p.SortDueMicros,
			&p.SortRowKey,
		); err != nil {
			return domain.ExecutionProjectionPage{}, fmt.Errorf("vaccination execution: scan vaccination execution: %w", err)
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
		p.WorkState = domain.WorkState(workState)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return domain.ExecutionProjectionPage{}, fmt.Errorf("vaccination execution: iterate vaccination execution: %w", err)
	}
	var next *domain.ExecutionCursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = &domain.ExecutionCursor{SortRank: last.SortRank, SortDueMicros: last.SortDueMicros, SortRowKey: last.SortRowKey}
	}
	return domain.ExecutionProjectionPage{Rows: out, TotalCount: totalCount, NextCursor: next}, nil
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
	fetchLimit := q.Limit + 1
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
	cursorParkID, cursorParkName, cursorShedID, cursorShedName, cursorStage := "", "", "", "", ""
	if q.Cursor != nil {
		cursorParkID = q.Cursor.ParkID
		cursorParkName = q.Cursor.ParkName
		cursorShedID = q.Cursor.ShedID
		cursorShedName = q.Cursor.ShedName
		cursorStage = q.Cursor.Stage
	}
	// 5k-50k envelope (docs/decisions/operational-kernel-5k-50k-scale-envelope.md): serve operations
	// directly from the canonical obligation/completion tables via the keyset-paginated
	// vaccinationOperationsSQL instead of vaccination_operations_projection_rows. Canonical reads are never
	// stale relative to the canonical write, so the serving-projection freshness gate is removed.
	// Freshness is nil (always current).
	rows, err := r.pool.Query(ctx, vaccinationOperationsSQL,
		q.TenantID, asOf, dueBefore, parkID, shedID, cursorParkID, cursorShedID, cursorStage, fetchLimit, cursorParkName, cursorShedName)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: list operations: %w", err)
	}
	defer rows.Close()
	return scanOperationsRows(rows, nil)
}

func (r *Repository) VaccinationSchedule(ctx context.Context, q domain.ScheduleQuery) ([]domain.OperationsRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 500 {
		limit = 500
	}
	monthStart, monthEnd := scheduleMonthWindow(q.MonthStart)
	asOf := time.Now().In(biztime.DefaultLocation())
	rows, err := r.vaccinationScheduleWindowRows(ctx, q, monthStart, monthEnd, asOf, limit+1)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *Repository) vaccinationScheduleWindowRows(ctx context.Context, q domain.ScheduleQuery, monthStart, monthEnd, asOf time.Time, limit int) ([]domain.OperationsRow, error) {
	parkID := ""
	if q.ParkID != nil {
		parkID = *q.ParkID
	}
	restrictParks := authorizedParkFilter(ctx, q.TenantID)
	cursorParkID, cursorParkName, cursorShedID, cursorShedName, cursorStage := "", "", "", "", ""
	if q.Cursor != nil {
		cursorParkID = q.Cursor.ParkID
		cursorParkName = q.Cursor.ParkName
		cursorShedID = q.Cursor.ShedID
		cursorShedName = q.Cursor.ShedName
		cursorStage = q.Cursor.Stage
	}
	rows, err := r.pool.Query(ctx, vaccinationScheduleWindowSQL, q.TenantID, asOf, monthStart, monthEnd, parkID,
		cursorParkID, cursorShedID, cursorStage, cursorParkName, cursorShedName, limit, restrictParks)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: schedule source rows: %w", err)
	}
	defer rows.Close()
	return scanOperationsRows(rows, nil)
}

func operationsRowsPage(rows []domain.OperationsRow, limit int) ([]domain.OperationsRow, *domain.OperationsCursor) {
	if limit <= 0 {
		limit = 500
	}
	out := make([]domain.OperationsRow, 0, len(rows))
	currentKey := ""
	cohorts := 0
	var lastVisible *domain.OperationsRow
	for _, row := range rows {
		key := scheduleCohortKey(row)
		if key != currentKey {
			currentKey = key
			cohorts++
			if cohorts > limit {
				if lastVisible == nil {
					return out, nil
				}
				cursor := operationsCursorFromRow(*lastVisible)
				return out, &cursor
			}
		}
		rowCopy := row
		out = append(out, rowCopy)
		lastVisible = &rowCopy
	}
	return out, nil
}

func (r *Repository) DriveAssignments(ctx context.Context, q domain.DriveAssignmentQuery) ([]domain.DriveAssignmentRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	monthStart, monthEnd := scheduleMonthWindow(q.MonthStart)
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	rows, err := r.pool.Query(ctx, driveAssignmentsSQL, q.TenantID, monthStart, monthEnd, optStr(q.ParkID), limit)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: drive assignments: %w", err)
	}
	defer rows.Close()
	out := make([]domain.DriveAssignmentRow, 0)
	for rows.Next() {
		var row domain.DriveAssignmentRow
		var planned pgtype.Date
		var shedID pgtype.Text
		var vaccineKeys []string
		var vaccineCodes []string
		var capacity string
		if err := rows.Scan(
			&planned,
			&row.OperatorID,
			&row.OperatorName,
			&row.ParkID,
			&row.ParkName,
			&shedID,
			&row.PhysicalShed,
			&row.PartitionLabel,
			&row.Animals,
			&row.DueAnimals,
			&row.DoneAnimals,
			&row.DeferredAnimals,
			&row.OverdueAnimals,
			&vaccineKeys,
			&vaccineCodes,
			&row.TotalDoses,
			&capacity,
		); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan drive assignment: %w", err)
		}
		if planned.Valid {
			row.PlannedDate = planned.Time.Format("2006-01-02")
		}
		row.ShedID = textPtr(shedID)
		row.VaccineNames = driveAssignmentVaccineLabels(vaccineKeys)
		if vaccineCodes == nil {
			vaccineCodes = []string{}
		}
		row.VaccineCodes = vaccineCodes
		row.Capacity = domain.CapacityStatus(capacity)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: drive assignment rows: %w", err)
	}
	return out, nil
}

func driveAssignmentVaccineLabels(keys []string) []string {
	if len(keys) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		protocolName, doseCode, ok := strings.Cut(key, "\x1f")
		if !ok {
			doseCode = key
		}
		label := domain.VaccinationDoseDisplayLabel(protocolName, doseCode)
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

const driveAssignmentsSQL = `
-- projection-review: membership=vaccination_drive_assignments unnested to vaccine-rule grain then regrouped after active vaccination_drive_date_overrides; group_key=(effective_planned_date,operator_id,park_id,shed_id,physical_shed,partition_label,animal_count,capacity_status,batch_status); join_cardinality=rule unnest is intentional 1:N, override lookup is unique by tenant+park+vaccine+original date, regrouping prevents sibling vaccines on the same date from duplicating animal counts; pagination=LIMIT applies only after the full effective-date regroup and month filter, so moved vaccines page by their new date; scope=tenant plus optional park, preserved through assignment park_id.
WITH assignment_vaccines AS (
  SELECT
    vda.tenant_id,
    vda.batch_id,
    vda.planned_date AS original_planned_date,
    COALESCE(override.override_date, vda.planned_date) AS effective_planned_date,
    vda.operator_id,
    vda.park_id,
    vda.shed_id,
    vda.physical_shed,
    vda.partition_label,
    vda.animal_count,
    vda.total_doses,
    vda.capacity_status,
    batch.status AS batch_status,
    pd.name || E'\x1f' || pr.dose_code AS vaccine_key,
    NULLIF(prd.vaccine_code, '') AS vaccine_code
  FROM vaccination_drive_assignments vda
  LEFT JOIN obligation_batches batch
    ON batch.tenant_id = vda.tenant_id
   AND batch.batch_id = vda.batch_id
  LEFT JOIN LATERAL unnest(vda.vaccine_rule_ids) AS assigned_rule(rule_id) ON true
  LEFT JOIN protocol_rules pr
    ON pr.tenant_id = vda.tenant_id
   AND pr.rule_id = assigned_rule.rule_id
  LEFT JOIN protocol_rule_dimensions prd
    ON prd.tenant_id = pr.tenant_id
   AND prd.rule_id = pr.rule_id
  LEFT JOIN protocol_versions pv
    ON pv.tenant_id = pr.tenant_id
   AND pv.protocol_version_id = pr.protocol_version_id
  LEFT JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
  LEFT JOIN vaccination_drive_date_overrides override
    ON override.tenant_id = vda.tenant_id
   AND override.park_id = vda.park_id
   AND override.original_drive_date = vda.planned_date
   AND lower(btrim(override.vaccine_code)) = lower(btrim(NULLIF(prd.vaccine_code, '')))
   AND override.canceled_at IS NULL
  WHERE vda.tenant_id = $1::uuid
    AND ($4::text = '' OR vda.park_id::text = $4)
),
effective_assignments AS (
  SELECT
    effective_planned_date AS planned_date,
    operator_id,
    park_id,
    shed_id,
    physical_shed,
    partition_label,
    animal_count,
    capacity_status,
    batch_status,
    ARRAY_AGG(DISTINCT vaccine_key ORDER BY vaccine_key) FILTER (WHERE vaccine_key IS NOT NULL) AS vaccine_keys,
    ARRAY_AGG(DISTINCT vaccine_code ORDER BY vaccine_code) FILTER (WHERE vaccine_code IS NOT NULL) AS vaccine_codes,
    CASE
      WHEN COUNT(DISTINCT vaccine_key) FILTER (WHERE vaccine_key IS NOT NULL) > 0
      THEN animal_count * COUNT(DISTINCT vaccine_key) FILTER (WHERE vaccine_key IS NOT NULL)
      ELSE MAX(total_doses)
    END::int AS total_doses
  FROM assignment_vaccines
  GROUP BY effective_planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, capacity_status, batch_status
)
SELECT
  effective.planned_date,
  effective.operator_id::text,
  wm.display_name,
  effective.park_id::text,
  park.name,
  effective.shed_id::text,
  effective.physical_shed,
  effective.partition_label,
  effective.animal_count,
  CASE
    WHEN effective.batch_status IN ('planned', 'in_progress')
     AND effective.planned_date >= (now() AT TIME ZONE 'Asia/Kolkata')::date
    THEN effective.animal_count
    ELSE 0
  END AS due_animals,
  CASE WHEN effective.batch_status = 'completed' THEN effective.animal_count ELSE 0 END AS done_animals,
  0 AS deferred_animals,
  CASE
    WHEN effective.batch_status IN ('planned', 'in_progress')
     AND effective.planned_date < (now() AT TIME ZONE 'Asia/Kolkata')::date
    THEN effective.animal_count
    ELSE 0
  END AS overdue_animals,
  COALESCE(effective.vaccine_keys, ARRAY[]::text[]),
  COALESCE(effective.vaccine_codes, ARRAY[]::text[]),
  effective.total_doses,
  effective.capacity_status
FROM effective_assignments effective
JOIN workforce_members wm
  ON wm.tenant_id = $1::uuid
 AND wm.workforce_member_id = effective.operator_id
 AND wm.status = 'active'
JOIN locations park
  ON park.tenant_id = $1::uuid
 AND park.location_id = effective.park_id
 AND park.location_type = 'park'
WHERE effective.planned_date >= $2::date
  AND effective.planned_date < $3::date
ORDER BY effective.planned_date, wm.display_name, effective.physical_shed, effective.partition_label
LIMIT $5;
`

func scheduleCohortKey(row domain.OperationsRow) string {
	return row.ParkID + "|" + row.ShedID + "|" + row.Stage
}

func operationsCursorFromRow(row domain.OperationsRow) domain.OperationsCursor {
	return domain.OperationsCursor{
		ParkID:   row.ParkID,
		ParkName: row.ParkName,
		ShedID:   row.ShedID,
		ShedName: row.ShedName,
		Stage:    row.Stage,
	}
}

func scheduleMonthWindow(anchor time.Time) (time.Time, time.Time) {
	if anchor.IsZero() {
		anchor = time.Now().In(biztime.DefaultLocation())
	}
	local := anchor.In(biztime.DefaultLocation())
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, biztime.DefaultLocation())
	end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	return start, end
}

func scanOperationsRows(rows pgx.Rows, freshness *domain.ProjectionFreshness) ([]domain.OperationsRow, error) {
	out := []domain.OperationsRow{}
	for rows.Next() {
		var row domain.OperationsRow
		var ageBand pgtype.Text
		var nextDue, lastDose pgtype.Timestamptz
		var vaccineNames []string
		var animals, overdue, due, inProgress, scheduled, missed, deferred, accepted, proofPending, rejected, total int64
		if err := rows.Scan(
			&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName, &row.Stage, &ageBand,
			&row.ProtocolID, &row.ProtocolName, &animals, &nextDue, &lastDose,
			&vaccineNames,
			&overdue, &due, &inProgress, &scheduled, &missed, &deferred, &accepted, &proofPending, &rejected, &total,
		); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan operations: %w", err)
		}
		row.AgeBand = textPtr(ageBand)
		row.NextDue = timePtr(nextDue)
		row.LastDose = timePtr(lastDose)
		row.Animals = int(animals)
		if vaccineNames == nil {
			vaccineNames = []string{}
		}
		row.VaccineNames = vaccineNames
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
		row.Freshness = freshness
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
		var aid1, aid2, shedID, shedName pgtype.Text
		var reasonCode string
		if err := rows.Scan(&row.GoatID, &row.DisplayID, &aid1, &aid2, &row.ParkID, &row.ParkName, &shedID, &shedName, &reasonCode); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan gaps: %w", err)
		}
		row.AnimalIdentifier1 = textPtr(aid1)
		row.AnimalIdentifier2 = textPtr(aid2)
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

const vaccinationScheduleWindowSQL = `
-- projection-review: membership=obligation_instances whose due_at or accepted completion falls inside the requested month window; group_key=(park_uuid,shed_uuid,stage,protocol_id,protocol_name) with month-window due/accepted membership applied before cohort pagination; join_cardinality=completions/asof_terminal are pre-aggregated one row per obligation and goat/location/protocol joins are keyed 1:1, while COUNT(DISTINCT goat_id) protects animal counts from multi-vaccine one-to-many obligations; pagination=cohort_page keysets groups before the final aggregate so page boundaries never truncate a cohort or change total_count; scope=tenant plus optional park filter resolved through raw.direct_park_uuid or the shed parent, with status buckets derived from as_of-effective eff_status.
-- scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md — bounded monthly canonical Full Schedule read, keyset-paginated by cohort and query-plan-tested (canonical_read_plan_test.go).
WITH completions AS (
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
-- projection-review: membership=obligation_instances after tenant/category/date/operator filtering, optionally decorated with one generated drive-assignment row for the same batch+shed; group_key=obligation_id at raw grain before grouped CTE collapses to park/shed/batch/rule/dose; join_cardinality=vaccination_drive_assignments is keyed by batch plus exact shed/partition, so operator scoping decorates the obligation without multiplying obligation membership; pagination=raw feeds grouped keyset/list and full filtered total_count, no page-local count; scope=park/shed/operator filters stay explicit in located/grouped predicates.
raw AS (
  SELECT
    oi.obligation_id,
    oi.rule_id,
    oi.due_at,
    oi.window_start,
    oi.window_end,
    oi.completed_at,
    oi.status AS stored_status,
    te.asof_terminal_type,
    te.has_terminal_event,
    pd.protocol_id,
    pd.name AS protocol_name,
    COALESCE(
      NULLIF(pr.eligibility_json -> 'vaccine' ->> 'display_name', ''),
      NULLIF(pr.eligibility_json -> 'vaccine' ->> 'name', ''),
      NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''),
      NULLIF(pr.dose_code, '')
    ) AS vaccine_name,
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
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
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
    AND (
      oi.due_at <= $4::timestamptz
      OR c.last_accepted_at BETWEEN $3::timestamptz AND $4::timestamptz
    )
),
located AS (
  SELECT
    raw.*,
    COALESCE(raw.direct_park_uuid, shed_loc.parent_location_id) AS park_uuid,
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
    END AS eff_status,
    (located.due_at BETWEEN $3::timestamptz AND $4::timestamptz) AS due_in_window,
    (located.last_accepted_at BETWEEN $3::timestamptz AND $4::timestamptz) AS accepted_in_window
  FROM located
),
windowed AS (
  SELECT *
  FROM effective
  WHERE park_uuid IS NOT NULL
    AND (due_in_window OR accepted_in_window)
    AND ($5::text = '' OR park_uuid = NULLIF($5, '')::uuid)
    AND ($12::uuid[] IS NULL OR park_uuid = ANY($12::uuid[]))
),
cohort_page AS (
  SELECT windowed.park_uuid, windowed.shed_uuid, windowed.stage
  FROM windowed
  JOIN locations shed
    ON shed.tenant_id = $1::uuid
   AND shed.location_id = windowed.shed_uuid
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  JOIN locations park
    ON park.tenant_id = $1::uuid
   AND park.location_id = windowed.park_uuid
   AND park.location_type = 'park'
   AND park.status = 'active'
  WHERE (
    NULLIF($6::text, '') IS NULL
    OR (park.name, shed.name, windowed.stage, windowed.park_uuid, windowed.shed_uuid) >
       ($9::text, $10::text, $8::text, NULLIF($6::text, '')::uuid, NULLIF($7::text, '')::uuid)
  )
  GROUP BY windowed.park_uuid, park.name, windowed.shed_uuid, shed.name, windowed.stage
  ORDER BY park.name, shed.name, windowed.stage, windowed.park_uuid, windowed.shed_uuid
  LIMIT $11::int
)
SELECT
  windowed.park_uuid,
  park.name AS park_name,
  windowed.shed_uuid,
  shed.name AS shed_name,
  windowed.stage,
  (ARRAY_AGG(windowed.age_band) FILTER (WHERE windowed.age_band IS NOT NULL))[1] AS age_band,
  windowed.protocol_id,
  windowed.protocol_name,
  COUNT(DISTINCT windowed.goat_id)::bigint AS animals,
  MIN(windowed.due_at) FILTER (WHERE windowed.due_in_window AND windowed.eff_status IN ('overdue', 'due', 'in_progress', 'scheduled')) AS next_due,
  MAX(windowed.last_accepted_at) FILTER (WHERE windowed.accepted_in_window) AS last_dose,
  COALESCE(
    ARRAY_REMOVE(
      ARRAY_AGG(DISTINCT windowed.vaccine_name ORDER BY windowed.vaccine_name)
        FILTER (WHERE windowed.vaccine_name IS NOT NULL AND windowed.vaccine_name <> ''),
      NULL
    ),
    ARRAY[]::text[]
  ) AS vaccine_names,
  COUNT(*) FILTER (WHERE windowed.due_in_window AND windowed.eff_status = 'overdue')::bigint AS overdue_count,
  COUNT(*) FILTER (WHERE windowed.due_in_window AND windowed.eff_status = 'due')::bigint AS due_count,
  COUNT(*) FILTER (WHERE windowed.due_in_window AND windowed.eff_status = 'in_progress')::bigint AS in_progress_count,
  COUNT(*) FILTER (WHERE windowed.due_in_window AND windowed.eff_status = 'scheduled')::bigint AS scheduled_count,
  COUNT(*) FILTER (WHERE windowed.due_in_window AND windowed.eff_status = 'missed')::bigint AS missed_count,
  COUNT(*) FILTER (WHERE windowed.due_in_window AND windowed.eff_status IN ('waived', 'deferred'))::bigint AS deferred_count,
  COUNT(*) FILTER (WHERE windowed.eff_status = 'completed' AND windowed.completion_status = 'accepted' AND (windowed.due_in_window OR windowed.accepted_in_window))::bigint AS accepted_count,
  COUNT(*) FILTER (WHERE windowed.due_in_window AND windowed.completion_status = 'recorded')::bigint AS proof_pending_count,
  COUNT(*) FILTER (WHERE windowed.due_in_window AND windowed.completion_status = 'rejected')::bigint AS rejected_count,
  COUNT(*)::bigint AS total_count
FROM windowed
JOIN cohort_page page
  ON page.park_uuid = windowed.park_uuid
 AND page.shed_uuid = windowed.shed_uuid
 AND page.stage = windowed.stage
JOIN locations shed
  ON shed.tenant_id = $1::uuid
 AND shed.location_id = windowed.shed_uuid
 AND shed.location_type = 'shed'
 AND shed.status = 'active'
JOIN locations park
  ON park.tenant_id = $1::uuid
 AND park.location_id = windowed.park_uuid
 AND park.location_type = 'park'
 AND park.status = 'active'
GROUP BY windowed.park_uuid, park.name, windowed.shed_uuid, shed.name, windowed.stage, windowed.protocol_id, windowed.protocol_name
ORDER BY
  park.name COLLATE "C" ASC, windowed.park_uuid ASC,
  shed.name COLLATE "C" ASC, windowed.shed_uuid ASC,
  windowed.stage COLLATE "C" ASC,
  windowed.protocol_name COLLATE "C" ASC, windowed.protocol_id ASC;`

const zeroUUID = "00000000-0000-0000-0000-000000000000"

// vaccinationGapsSQL is a bounded keyset-paginated scan: tenant_id + lifecycle_status prune the index,
// goat_id > $3 drives the PK-ordered keyset window, and the LIMIT bounds the response regardless of how
// many animals a park has gapped. Requires a park (INNER JOIN locations park), matching the same
// "located park_uuid IS NOT NULL" convention the execution/operations queries already use; a goat with
// no park at all is out of scope here (not a modeled gap reason).
// The two LEFT JOINs on goat_identifiers surface the animal's physical tags ("Tag 1"/"Tag 2") for the
// mobile card; each is a single indexed lookup on goat_identifiers_goat_status_idx (goat_id, status) over
// the already-bounded (<= LIMIT) keyset window, not a full-table scan — same idiom as calendar
// ListDriveTargets. A goat with no active tag of a type yields NULL (rendered as "—" on the card).
const vaccinationGapsSQL = `
SELECT
  g.goat_id::text,
  g.display_id,
  aid1.identifier_value,
  aid2.identifier_value,
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

func timeStringPtr(v pgtype.Timestamptz) *string {
	if !v.Valid {
		return nil
	}
	s := v.Time.Format(time.RFC3339)
	return &s
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
//
// This is now the request-path serving read (ListVaccinationExecutionPage), not only the projector replay.
// projection-review: membership=obligation_instances rows for one tenant due by $4, joined to obligation_batches by batch_id so batched rows use the canonical batch planned_date while unbatched rows fall back to obligation due_at; group_key=(park_uuid, shed_uuid, batch_id, rule_id, protocol_name, dose_code); join_cardinality=completion history is pre-aggregated from 0:N to one as-of-effective row per obligation before joining, while batch/task/goat joins are keyed 1:1 and grouped COUNT/ARRAY_AGG operate on obligation grain so target counts cannot fan out; pagination=classified rows are keyset paginated after grouped aggregation with total_count over the full filtered set; scope=park/shed filters are resolved through located.park_uuid/shed_uuid with tenant scoping and status buckets from as_of-effective eff_status/work_state.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md — keyset-paginated (~20 rows) canonical execution list, query-plan-tested (canonical_read_plan_test.go).
const vaccinationExecutionSQL = `
WITH completion_candidates AS (
  SELECT
    obligation_id,
    completion_id,
    administered_at,
    created_at,
    CASE
      WHEN status IN ('accepted', 'rejected') AND verified_at IS NOT NULL AND verified_at > $7::timestamptz THEN 'recorded'
      ELSE status
    END AS asof_status
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
    AND COALESCE(administered_at, created_at) <= $7::timestamptz
),
completions AS (
  SELECT
    obligation_id,
    (ARRAY_AGG(asof_status ORDER BY
      CASE WHEN asof_status IN ('recorded', 'accepted') THEN 0 ELSE 1 END,
      administered_at DESC,
      created_at DESC,
      completion_id DESC))[1] AS effective_status,
    (ARRAY_AGG(completion_id ORDER BY
      CASE WHEN asof_status IN ('recorded', 'accepted') THEN 0 ELSE 1 END,
      administered_at DESC,
      created_at DESC,
      completion_id DESC))[1] AS completion_id
  FROM completion_candidates
  GROUP BY obligation_id
),
operator_scope_member AS (
  SELECT wm.workforce_member_id
  FROM workforce_members wm
  WHERE wm.tenant_id = $1::uuid
    AND wm.status = 'active'
    AND $15::text <> ''
    AND (
      wm.workforce_member_id = NULLIF($15::text, '')::uuid
      OR wm.user_id = NULLIF($15::text, '')::uuid
    )
  ORDER BY CASE WHEN wm.workforce_member_id = NULLIF($15::text, '')::uuid THEN 0 ELSE 1 END,
           wm.updated_at DESC,
           wm.workforce_member_id DESC
  LIMIT 1
),
asof_terminal AS (
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
    ob.planned_date::timestamptz AS batch_planned_at,
    ob.status AS batch_status,
    vda.operator_id AS conducted_by,
    vda.physical_shed,
    vda.partition_label,
    st.state AS task_state,
    st.task_id AS sop_task_id,
    st.sop_version_id AS sop_version_id,
    st.row_version AS sop_task_row_version,
    st.assigned_to,
    g.lifecycle_status AS goat_lifecycle_status,
    g.health_status AS goat_health_status,
    g.management_stage AS goat_stage,
    c.effective_status AS completion_status,
    c.completion_id,
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
    SELECT assignment.operator_id, assignment.physical_shed, assignment.partition_label
    FROM vaccination_drive_assignments assignment
    WHERE assignment.tenant_id = oi.tenant_id
      AND assignment.batch_id = oi.batch_id
      AND assignment.shed_id = CASE
        WHEN g.shed_id IS NOT NULL THEN g.shed_id
        WHEN oi.target_type = 'shed' THEN oi.target_id
        WHEN oi.scope_type = 'shed' THEN oi.scope_id
        ELSE NULL
      END
      AND (
        $15::text = ''
        OR assignment.operator_id IN (SELECT workforce_member_id FROM operator_scope_member)
      )
    ORDER BY
      CASE
        WHEN assignment.shed_id = g.shed_id THEN 0
        WHEN assignment.shed_id IS NOT NULL THEN 1
        ELSE 2
      END,
      assignment.planned_date ASC,
      assignment.partition_label ASC,
      assignment.operator_id ASC NULLS LAST,
      assignment.assignment_id ASC
    LIMIT 1
  ) vda ON true
  LEFT JOIN sop_tasks st
    ON st.tenant_id = oi.tenant_id
   AND st.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND COALESCE(ob.status, '') NOT IN ('canceled', 'superseded')
    AND COALESCE(st.state, '') <> 'canceled'
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
    COALESCE(raw.batch_planned_at, raw.due_at) AS execution_due_at,
    -- as_of-effective obligation status (reconstructed AT as_of, not the current stored status).
    CASE
      WHEN raw.obligation_status = 'completed' THEN
        CASE
          WHEN raw.completed_at IS NOT NULL AND raw.completed_at <= $7::timestamptz THEN 'completed'
          WHEN raw.completed_at IS NULL AND raw.completion_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN COALESCE(raw.batch_planned_at, raw.due_at) < $7::timestamptz THEN 'overdue' WHEN COALESCE(raw.batch_planned_at, raw.window_start, raw.due_at) <= $7::timestamptz THEN 'due' ELSE 'scheduled' END)
        END
      WHEN raw.obligation_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          WHEN raw.asof_terminal_type IS NOT NULL THEN raw.asof_terminal_type
          WHEN raw.has_terminal_event THEN (CASE WHEN COALESCE(raw.batch_planned_at, raw.due_at) < $7::timestamptz THEN 'overdue' WHEN COALESCE(raw.batch_planned_at, raw.window_start, raw.due_at) <= $7::timestamptz THEN 'due' ELSE 'scheduled' END)
          ELSE raw.obligation_status
        END
      WHEN raw.obligation_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN COALESCE(raw.batch_planned_at, raw.due_at) < $7::timestamptz THEN 'overdue' WHEN COALESCE(raw.batch_planned_at, raw.window_start, raw.due_at) <= $7::timestamptz THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM raw
  LEFT JOIN locations shed_loc
    ON shed_loc.tenant_id = $1::uuid
   AND shed_loc.location_id = raw.shed_uuid
   AND shed_loc.location_type = 'shed'
  WHERE raw.shed_uuid IS NOT NULL
),
-- projection-review: membership=located obligation-grain rows after tenant/category/scope resolution, with batch planned_date carried as execution_due_at for batched rows; group_key=(park_uuid,shed_uuid,batch_id,rule_id,protocol_name,dose_code); join_cardinality=completions pre-aggregates 0:N completion history to one effective row per obligation, goat/batch/task joins are keyed 1:1, drive assignments are collapsed through LEFT JOIN LATERAL ... LIMIT 1 before grouping, and animal-stage joins use tenant-scoped unique id/code keys so COUNT/ARRAY_AGG stay at obligation grain; pagination=grouped rows feed classified keyset pagination and total_count over the full filtered set; scope=park/shed via located.park_uuid/shed_uuid and tenant-scoped location joins.
grouped AS (
  SELECT
    located.park_uuid,
    located.shed_uuid,
    located.batch_id,
    located.rule_id,
    located.protocol_name,
    located.dose_code,
    MIN(located.execution_due_at) AS due_at,
    COUNT(*)::bigint AS obligation_count,
    -- Bucket counts use the as_of-effective status; completion counts use the pre-aggregated,
    -- as-of-bounded completion projection above.
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
      located.execution_due_at DESC NULLS LAST
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
      located.execution_due_at DESC NULLS LAST
    ) FILTER (WHERE located.task_state IS NOT NULL))[1] AS task_state,
    (ARRAY_AGG(operator.display_name ORDER BY
      CASE
        WHEN located.conducted_by IS NOT NULL THEN 0
        WHEN located.assigned_to IS NOT NULL THEN 1
        ELSE 2
      END,
      located.execution_due_at DESC NULLS LAST,
      operator.updated_at DESC NULLS LAST,
      operator.workforce_member_id DESC
    ) FILTER (WHERE operator.display_name IS NOT NULL))[1] AS operator_name,
    COALESCE(
      (ARRAY_AGG(located.physical_shed ORDER BY located.execution_due_at DESC NULLS LAST, located.partition_label ASC NULLS LAST)
        FILTER (WHERE NULLIF(located.physical_shed, '') IS NOT NULL))[1],
      MAX(shed.name)
    ) AS physical_shed,
    COALESCE(
      (ARRAY_AGG(located.partition_label ORDER BY located.execution_due_at DESC NULLS LAST, located.partition_label ASC NULLS LAST)
        FILTER (WHERE NULLIF(located.partition_label, '') IS NOT NULL))[1],
      'whole'
    ) AS partition_label,
    -- This value is rendered directly on mobile shed cards. Prefer the governed
    -- human name; stage_code (K1/K2/...) is an internal fallback only.
    COALESCE(
      MAX(profile_stage.name),
      MAX(goat_stage.name),
      MAX(profile_stage.stage_code),
      MAX(goat_stage.stage_code),
      MAX(NULLIF(regexp_replace(initcap(replace(located.goat_stage, '_', ' ')), '\s+', ' ', 'g'), '')),
      'Unknown'
    ) AS animal_stage,
    COUNT(*) FILTER (
      WHERE located.goat_lifecycle_status IN ('sick', 'under_treatment', 'quarantine', 'icu')
         OR COALESCE(located.goat_health_status, '') IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
    )::bigint AS health_deferred_count,
    (ARRAY_AGG(located.obligation_id ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST, located.obligation_id DESC))[1]::text AS obligation_id,
    (ARRAY_AGG(located.sop_task_id ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST, located.sop_task_id DESC NULLS LAST))[1]::text AS sop_task_id,
    (ARRAY_AGG(located.sop_version_id ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST, located.sop_task_id DESC NULLS LAST) FILTER (WHERE located.sop_version_id IS NOT NULL))[1]::text AS sop_version_id,
    (ARRAY_AGG(located.sop_task_row_version ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST, located.sop_task_id DESC NULLS LAST) FILTER (WHERE located.sop_task_id IS NOT NULL))[1] AS sop_task_row_version,
    (ARRAY_AGG(located.completion_id ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST, located.completion_id DESC NULLS LAST))[1]::text AS completion_id
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
  LEFT JOIN animal_stage_lookup profile_stage
    ON profile_stage.tenant_id = $1::uuid
   AND profile_stage.animal_stage_id = sp.animal_stage_id
   AND profile_stage.status = 'active'
  -- Some legacy sheds do not yet carry shed_profiles.animal_stage_id. The
  -- obligation still has the goat's governed stage code, so resolve that code
  -- through the same lookup instead of leaking K1/K2 onto the mobile card.
  LEFT JOIN animal_stage_lookup goat_stage
    ON goat_stage.tenant_id = $1::uuid
   AND goat_stage.stage_code = located.goat_stage
   AND goat_stage.status = 'active'
  LEFT JOIN workforce_members operator
    ON operator.tenant_id = $1::uuid
   AND operator.workforce_member_id = COALESCE(located.conducted_by, located.assigned_to)
   AND operator.status = 'active'
  WHERE located.park_uuid IS NOT NULL
    AND ($2::text = '' OR located.park_uuid = $2::uuid)
    AND ($3::text = '' OR located.shed_uuid = $3::uuid)
    AND (
      $15::text = ''
      OR located.conducted_by IN (SELECT workforce_member_id FROM operator_scope_member)
    )
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
),
classified AS (
  SELECT
    stateful.*,
    CASE stateful.work_state
      WHEN 'rejected' THEN 0
      WHEN 'blocked' THEN 1
      WHEN 'missed' THEN 2
      WHEN 'overdue' THEN 3
      WHEN 'proof_pending' THEN 4
      WHEN 'verification_pending' THEN 5
      WHEN 'due' THEN 6
      WHEN 'in_progress' THEN 7
      WHEN 'deferred' THEN 8
      WHEN 'scheduled' THEN 9
      WHEN 'completed' THEN 10
      ELSE 11
    END AS sort_rank,
    COALESCE(
      CASE
        WHEN stateful.work_state = 'completed' THEN -(EXTRACT(EPOCH FROM stateful.due_at) * 1000000)::bigint
        ELSE (EXTRACT(EPOCH FROM stateful.due_at) * 1000000)::bigint
      END,
      9223372036854775807::bigint
    ) AS sort_due_micros,
    stateful.park_uuid::text || '|' || stateful.shed_uuid::text || '|' || stateful.rule_id::text || '|' ||
      COALESCE(stateful.batch_id::text, '00000000-0000-0000-0000-000000000000') || '|' ||
      stateful.obligation_id::text AS sort_row_key,
    CASE
      WHEN stateful.work_state = 'completed' THEN 'ok'
      WHEN stateful.work_state IN ('scheduled', 'due', 'in_progress', 'deferred', 'verification_pending') THEN 'watch'
      WHEN stateful.work_state IN ('proof_pending', 'overdue', 'missed') THEN 'at_risk'
      ELSE 'broken'
    END AS severity
  FROM stateful
),
filtered AS (
  -- projection-review: membership=classified rows after tenant/category/scope resolution and work_state/severity filters; group_key=classified.sort_row_key; join_cardinality=classified is already grouped to one row per execution cohort before COUNT(*) OVER(); pagination=total_count is computed over the full filtered result before keyset LIMIT; scope=park/shed inherited from located.park_uuid/located.shed_uuid.
  SELECT classified.*, COUNT(*) OVER()::bigint AS total_count
  FROM classified
  WHERE ($6::text = '' OR classified.work_state = $6::text)
    AND ($9::text = '' OR classified.severity = $9::text)
    AND (
      NOT $10::boolean
      OR (
        classified.obligation_count
        - GREATEST(
            classified.completed_count,
            classified.completion_recorded + classified.completion_accepted + classified.completion_rejected
          )
        - classified.deferred_count
        - classified.missed_count
        - classified.canceled_count
      ) > 0
    )
)
SELECT
  grouped.park_uuid::text AS park_id,
  park.name AS park_name,
  grouped.shed_uuid::text AS shed_id,
  shed.name AS shed_name,
  grouped.physical_shed,
  grouped.partition_label,
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
  grouped.completion_id,
  grouped.work_state,
  grouped.total_count,
  grouped.sort_rank,
  grouped.sort_due_micros,
  grouped.sort_row_key
FROM filtered grouped
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
WHERE NOT $11::boolean
   OR (grouped.sort_rank, grouped.sort_due_micros, grouped.sort_row_key) > ($12::int, $13::bigint, $14::text)
ORDER BY grouped.sort_rank, grouped.sort_due_micros, grouped.sort_row_key
LIMIT ($5::int + 1);
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
//
// This is now the request-path serving read (VaccinationOperations), not only the projector replay.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md — keyset-paginated (~20 cohorts) canonical operations list, query-plan-tested (canonical_read_plan_test.go).
const vaccinationOperationsSQL = `
WITH cursor_location AS (
  -- Cursors emitted before the human-order fix carry IDs/stage only. Resolve
  -- their names from the same tenant so those short-lived cursors remain valid;
  -- new cursors embed names to keep the comparison stable across page requests.
  SELECT
    COALESCE(NULLIF($10::text, ''), park.name) AS park_name,
    COALESCE(NULLIF($11::text, ''), shed.name) AS shed_name
  FROM locations park
  JOIN locations shed
    ON shed.tenant_id = park.tenant_id
   AND shed.location_id = NULLIF($7::text, '')::uuid
   AND shed.location_type = 'shed'
  WHERE park.tenant_id = $1::uuid
    AND park.location_id = NULLIF($6::text, '')::uuid
    AND park.location_type = 'park'
),
completions AS (
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
    oi.rule_id,
    oi.due_at,
    oi.window_start,
    oi.window_end,
    oi.completed_at,
    oi.status AS stored_status,
    te.asof_terminal_type,
    te.has_terminal_event,
    pd.protocol_id,
    pd.name AS protocol_name,
    COALESCE(
      NULLIF(pr.eligibility_json -> 'vaccine' ->> 'display_name', ''),
      NULLIF(pr.eligibility_json -> 'vaccine' ->> 'name', ''),
      NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''),
      NULLIF(pr.dose_code, '')
    ) AS vaccine_name,
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
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
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
),
cohort_page AS (
  SELECT effective.park_uuid, effective.shed_uuid, effective.stage
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
    AND ($4::text = '' OR effective.park_uuid = NULLIF($4, '')::uuid)
    AND ($5::text = '' OR effective.shed_uuid = NULLIF($5, '')::uuid)
    AND (
      $6::text = ''
      OR (
        lower(park.name) COLLATE "C", park.name COLLATE "C", effective.park_uuid,
        lower(shed.name) COLLATE "C", shed.name COLLATE "C", effective.shed_uuid,
        effective.stage COLLATE "C"
      ) > (
        SELECT
          lower(cursor_location.park_name) COLLATE "C", cursor_location.park_name COLLATE "C", NULLIF($6, '')::uuid,
          lower(cursor_location.shed_name) COLLATE "C", cursor_location.shed_name COLLATE "C", NULLIF($7, '')::uuid,
          $8::text COLLATE "C"
        FROM cursor_location
      )
    )
  GROUP BY effective.park_uuid, park.name, effective.shed_uuid, shed.name, effective.stage
  ORDER BY
    lower(park.name) COLLATE "C" ASC, park.name COLLATE "C" ASC, effective.park_uuid ASC,
    lower(shed.name) COLLATE "C" ASC, shed.name COLLATE "C" ASC, effective.shed_uuid ASC,
    effective.stage COLLATE "C" ASC
  LIMIT $9
)
-- projection-review: membership=effective vaccination obligation rows inside the tenant due horizon after as-of status reconstruction; group_key=(park_uuid,shed_uuid,stage,protocol_id,protocol_name) selected by cohort_page; join_cardinality=completions and terminal events are pre-aggregated to one row per obligation, goat/protocol/location joins are tenant-keyed 1:1, and COUNT(DISTINCT goat_id) protects animal membership; pagination=cohort_page keysets whole cohort groups before this final aggregate so page size cannot split a group or alter its totals; scope=tenant plus explicit optional park/shed filters with park resolved from direct goat park or shed parent.
SELECT
  -- projection-review: membership=effective vaccination obligation rows inside the tenant due horizon after as-of status reconstruction; group_key=(park_uuid,shed_uuid,stage,protocol_id,protocol_name); join_cardinality=cohort_page joins on the grouped cohort key and goat/protocol/location dimensions are tenant-keyed 1:1, while COUNT(DISTINCT effective.goat_id) protects animal membership; pagination=cohort_page keysets whole cohort groups before final aggregation; scope=tenant plus explicit optional park/shed filters.
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
  COALESCE(
    ARRAY_REMOVE(
      ARRAY_AGG(DISTINCT effective.vaccine_name ORDER BY effective.vaccine_name)
        FILTER (WHERE effective.vaccine_name IS NOT NULL AND effective.vaccine_name <> ''),
      NULL
    ),
    ARRAY[]::text[]
  ) AS vaccine_names,
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
JOIN cohort_page page
  ON page.park_uuid = effective.park_uuid
 AND page.shed_uuid = effective.shed_uuid
 AND page.stage = effective.stage
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
GROUP BY effective.park_uuid, park.name, effective.shed_uuid, shed.name, effective.stage, effective.protocol_id, effective.protocol_name
ORDER BY
  lower(park.name) COLLATE "C" ASC, park.name COLLATE "C" ASC, effective.park_uuid ASC,
  lower(shed.name) COLLATE "C" ASC, shed.name COLLATE "C" ASC, effective.shed_uuid ASC,
  effective.stage COLLATE "C" ASC,
  effective.protocol_name COLLATE "C" ASC, effective.protocol_id ASC;
`

// ScanRoster returns per-animal vaccination obligations scoped by shed, with RFID tags and vaccine labels.
// Used by the mobile scan screen to match keyboard-wedge tag captures against due animals.
func (r *Repository) ScanRoster(ctx context.Context, q domain.ScanRosterQuery) (domain.ScanRosterResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// task_id is OPTIONAL: when present the roster is pinned to that task's batch (task-scoped);
	// when absent it falls back to the shed-wide roster (the pre-refactor behaviour the current
	// app still relies on). The pinned identity is only resolved when a task is supplied.
	identity := taskExecutionIdentity{}
	if q.TaskID != "" {
		var err error
		identity, err = r.taskExecutionIdentity(ctx, q.TenantID, q.TaskID, q.ShedID)
		if err != nil {
			return domain.ScanRosterResult{}, fmt.Errorf("vaccination execution: scan roster identity: %w", err)
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	cursorGoatID, cursorObligationID := "", ""
	if q.Cursor != nil {
		cursorGoatID, cursorObligationID = q.Cursor.GoatID, q.Cursor.ObligationID
	}
	// Park-scope clamp (defence in depth): a park-scoped app actor may only read rosters for sheds
	// in their authorized parks. Tenant-wide (or grant-less internal) callers pass nil = no filter.
	restrictParks := authorizedParkFilter(ctx, q.TenantID)
	rows, err := r.pool.Query(ctx, scanRosterSQL, q.TenantID, q.ShedID, q.TaskID, identity.BatchID, cursorGoatID, cursorObligationID, limit+1, restrictParks, q.OperatorScopeActorID)
	if err != nil {
		return domain.ScanRosterResult{}, fmt.Errorf("vaccination execution: scan roster: %w", err)
	}
	defer rows.Close()
	out := []domain.ScanRosterRow{}
	for rows.Next() {
		var row domain.ScanRosterRow
		var secondaryTag pgtype.Text
		var scannedAt pgtype.Timestamptz
		var protocolName, doseCode string
		if err := rows.Scan(
			&row.GoatID,
			&row.PrimaryTag,
			&secondaryTag,
			&protocolName,
			&doseCode,
			&row.Status,
			&scannedAt,
			&row.ObligationID,
		); err != nil {
			return domain.ScanRosterResult{}, fmt.Errorf("vaccination execution: scan roster scan: %w", err)
		}
		row.SecondaryTag = textPtr(secondaryTag)
		if scannedAt.Valid {
			value := scannedAt.Time.UTC().Format(time.RFC3339Nano)
			row.ScannedAt = &value
		}
		row.VaccineLabel = domain.VaccinationDoseDisplayLabel(protocolName, doseCode)
		row.BatchID = identity.BatchID
		row.TaskID = identity.TaskID
		row.SOPVersionID = identity.SOPVersionID
		row.TaskRowVersion = identity.TaskRowVersion
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return domain.ScanRosterResult{}, fmt.Errorf("vaccination execution: scan roster iterate: %w", err)
	}
	result := domain.ScanRosterResult{Rows: out}
	if len(out) > limit {
		result.Rows = out[:limit]
		last := result.Rows[len(result.Rows)-1]
		result.NextCursor = &domain.ScanRosterCursor{GoatID: last.GoatID, ObligationID: last.ObligationID}
	}
	return result, nil
}

const scanRosterSQL = `
-- projection-review: membership=one task-pinned shed roster row per vaccination obligation for the selected shed; group_key=(tenant_id,shed_id,task_id,goat_id,obligation_id); join_cardinality=goat/protocol/tag joins are tenant-keyed and the scan capture table is collapsed through LEFT JOIN LATERAL ... LIMIT 1 so multiple scans cannot duplicate an obligation row; pagination=keyset over (goat_id,obligation_id) after status/scanned_at projection, so page boundaries do not change row membership; scope=tenant plus explicit shed_id, optional task_id/batch_id pinning, and authorized park filter.
WITH operator_scope_member AS (
  SELECT wm.workforce_member_id
  FROM workforce_members wm
  WHERE wm.tenant_id = $1::uuid
    AND wm.status = 'active'
    AND $9::text <> ''
    AND (
      wm.workforce_member_id = NULLIF($9::text, '')::uuid
      OR wm.user_id = NULLIF($9::text, '')::uuid
    )
  ORDER BY CASE WHEN wm.workforce_member_id = NULLIF($9::text, '')::uuid THEN 0 ELSE 1 END,
           wm.updated_at DESC,
           wm.workforce_member_id DESC
  LIMIT 1
)
SELECT
  g.goat_id::text,
  COALESCE(aid1.identifier_value, '') AS primary_tag,
  aid2.identifier_value AS secondary_tag,
  pd.name AS protocol_name,
  pr.dose_code,
  CASE
    WHEN sc.capture_id IS NOT NULL THEN 'done'
    WHEN oi.status = 'due' OR (oi.due_at < now() AND oi.status = 'scheduled') THEN 'due'
    WHEN oi.status = 'in_progress' THEN 'in_progress'
    WHEN oi.status = 'completed' THEN 'completed'
    WHEN oi.status IN ('deferred', 'missed', 'waived') THEN 'deferred'
    ELSE 'pending'
  END AS status,
  sc.captured_at AS scanned_at,
  oi.obligation_id::text
FROM obligation_instances oi
LEFT JOIN obligation_batches ob
  ON ob.tenant_id = oi.tenant_id
 AND ob.batch_id = oi.batch_id
LEFT JOIN sop_tasks st
  ON st.tenant_id = oi.tenant_id
 AND st.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
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
LEFT JOIN LATERAL (
  SELECT c.capture_id, c.captured_at
  FROM sop_task_scan_captures c
  WHERE c.tenant_id = oi.tenant_id
    AND c.task_id = NULLIF($3, '')::uuid
    AND c.field_key IN ('goat_ids', '__scan_roster__')
    AND (
      c.obligation_id = oi.obligation_id
      OR (c.obligation_id IS NULL AND c.goat_id = g.goat_id)
    )
  ORDER BY c.captured_at DESC, c.capture_id DESC
  LIMIT 1
) sc ON $3 <> ''
WHERE oi.tenant_id = $1::uuid
  AND g.shed_id = $2::uuid
  AND (
    $3 = ''
    OR oi.sop_task_id = NULLIF($3, '')::uuid
    OR ($4 <> '' AND oi.batch_id = NULLIF($4, '')::uuid)
  )
  AND ($4 = '' OR oi.batch_id = NULLIF($4, '')::uuid)
  AND oi.status NOT IN ('waived', 'canceled', 'superseded')
  AND (
    $9::text = ''
    OR EXISTS (
      SELECT 1
      FROM vaccination_drive_assignments assignment
      WHERE assignment.tenant_id = oi.tenant_id
        AND assignment.batch_id = oi.batch_id
        AND assignment.shed_id = g.shed_id
        AND assignment.operator_id IN (SELECT workforce_member_id FROM operator_scope_member)
    )
  )
  AND (
    $5 = '' OR g.goat_id > NULLIF($5, '')::uuid
    OR (g.goat_id = NULLIF($5, '')::uuid AND oi.obligation_id > NULLIF($6, '')::uuid)
  )
  AND ($8::uuid[] IS NULL OR g.park_id = ANY($8::uuid[]))
ORDER BY g.goat_id ASC, oi.obligation_id ASC
LIMIT $7;
`

type taskExecutionIdentity struct {
	TaskID         string
	BatchID        string
	SOPVersionID   string
	TaskRowVersion int32
}

func (r *Repository) taskExecutionIdentity(ctx context.Context, tenantID, taskID, shedID string) (taskExecutionIdentity, error) {
	var identity taskExecutionIdentity
	err := r.pool.QueryRow(ctx, `
SELECT st.task_id::text, ob.batch_id::text, st.sop_version_id::text, st.row_version
FROM sop_tasks st
JOIN obligation_batches ob
  ON ob.tenant_id = st.tenant_id
 AND ob.sop_task_id = st.task_id
 AND ob.batch_id = nullif(st.context ->> 'obligation_batch_id', '')::uuid
WHERE st.tenant_id = $1::uuid
  AND st.task_id = $2::uuid
  AND EXISTS (
        SELECT 1
        FROM obligation_instances oi
        JOIN goats g
          ON g.tenant_id = oi.tenant_id
         AND g.goat_id = oi.target_id
         AND oi.target_type = 'goat'
        WHERE oi.tenant_id = ob.tenant_id
          AND oi.batch_id = ob.batch_id
          AND g.shed_id = $3::uuid
          AND (
            (st.scope_type = 'shed' AND st.scope_id = $3::uuid AND ob.scope_type = 'shed' AND ob.scope_id = $3::uuid)
            OR
            (st.scope_type = 'park' AND ob.scope_type = 'park' AND st.scope_id = ob.scope_id AND g.park_id = ob.scope_id)
          )
      )
LIMIT 1`, tenantID, taskID, shedID).Scan(
		&identity.TaskID, &identity.BatchID, &identity.SOPVersionID, &identity.TaskRowVersion,
	)
	return identity, err
}

func (r *Repository) TaskOptionValues(ctx context.Context, tenantID, taskID string) (domain.TaskOptionValuesResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var identity taskExecutionIdentity
	var inventoryScopeLocationID string
	var formDSL []byte
	err := r.pool.QueryRow(ctx, `
SELECT st.task_id::text, ob.batch_id::text, st.sop_version_id::text, st.row_version,
       st.scope_id::text, sv.form_dsl
FROM sop_tasks st
JOIN obligation_batches ob
  ON ob.tenant_id = st.tenant_id
 AND ob.sop_task_id = st.task_id
 AND ob.batch_id = nullif(st.context ->> 'obligation_batch_id', '')::uuid
JOIN sop_versions sv
  ON sv.tenant_id = st.tenant_id
 AND sv.sop_version_id = st.sop_version_id
WHERE st.tenant_id = $1::uuid
  AND st.task_id = $2::uuid
  AND st.scope_type IN ('shed', 'park')
  AND ob.scope_type = st.scope_type
  AND ob.scope_id = st.scope_id
  AND (
    $3::uuid[] IS NULL
    OR (st.scope_type = 'park' AND st.scope_id = ANY($3::uuid[]))
    OR (st.scope_type = 'shed' AND EXISTS (
      SELECT 1 FROM goats g
      WHERE g.tenant_id = st.tenant_id
        AND g.shed_id = st.scope_id
        AND g.park_id = ANY($3::uuid[])
    ))
  )`, tenantID, taskID, authorizedParkFilter(ctx, tenantID)).Scan(
		&identity.TaskID, &identity.BatchID, &identity.SOPVersionID, &identity.TaskRowVersion, &inventoryScopeLocationID, &formDSL,
	)
	if err != nil {
		return domain.TaskOptionValuesResponse{}, fmt.Errorf("vaccination execution: task option identity: %w", err)
	}
	requested := pinnedOptionSources(formDSL)
	response := domain.TaskOptionValuesResponse{
		TaskID: identity.TaskID, BatchID: identity.BatchID, SOPVersionID: identity.SOPVersionID,
		TaskRowVersion: identity.TaskRowVersion, Sources: []domain.TaskOptionSource{},
	}
	if requested["inventory.vaccine_lots.fefo"] {
		source, err := r.vaccineLotOptions(ctx, tenantID, identity.BatchID, inventoryScopeLocationID)
		if err != nil {
			return domain.TaskOptionValuesResponse{}, err
		}
		response.Sources = append(response.Sources, source)
	}
	if requested["vaccination.route_sites"] {
		source, err := r.routeSiteOptions(ctx, tenantID, identity.BatchID)
		if err != nil {
			return domain.TaskOptionValuesResponse{}, err
		}
		response.Sources = append(response.Sources, source)
	}
	return response, nil
}

func pinnedOptionSources(raw []byte) map[string]bool {
	var form struct {
		Fields []struct {
			OptionSource string `json:"option_source"`
		} `json:"fields"`
	}
	_ = json.Unmarshal(raw, &form)
	out := map[string]bool{}
	for _, field := range form.Fields {
		if field.OptionSource == "inventory.vaccine_lots.fefo" || field.OptionSource == "vaccination.route_sites" {
			out[field.OptionSource] = true
		}
	}
	return out
}

func (r *Repository) vaccineLotOptions(ctx context.Context, tenantID, batchID, inventoryScopeLocationID string) (domain.TaskOptionSource, error) {
	// FEFO "not expired" is an India-business-day comparison, not the DB server's UTC CURRENT_DATE
	// (a lot expiring today in Asia/Kolkata must not rank as usable past IST midnight). See
	// GoatOS time semantics: derive the business day and pass it as a bound parameter.
	// Capture one business date for both SQL ordering and response availability. Using a
	// second UTC clock below made the same lot simultaneously rank usable and render expired
	// around India midnight.
	bizToday := biztime.BusinessDate(time.Now())
	rows, err := r.pool.Query(ctx, `
WITH RECURSIVE chain AS (
  SELECT location_id, parent_location_id, 0 AS depth FROM locations
  WHERE tenant_id=$1::uuid AND location_id=$3::uuid
  UNION ALL
  SELECT l.location_id, l.parent_location_id, c.depth+1 FROM locations l
  JOIN chain c ON l.location_id=c.parent_location_id
  WHERE l.tenant_id=$1::uuid AND c.depth < 8
), items AS (
  SELECT DISTINCT item_id FROM inventory_stock_movements
  WHERE tenant_id=$1::uuid AND batch_id=$2::uuid AND movement_type='reserve'
  UNION
  SELECT nullif(ob.context #>> '{stock_block,item_id}', '')::uuid
  FROM obligation_batches ob WHERE ob.tenant_id=$1::uuid AND ob.batch_id=$2::uuid
    AND nullif(ob.context #>> '{stock_block,item_id}', '') IS NOT NULL
)
SELECT s.stock_id::text, COALESCE(NULLIF(s.lot_code,''), s.stock_id::text),
       (s.quantity_in_stock-s.quantity_reserved)::text, s.quantity_unit, s.expiry_date, s.status,
       c.depth
FROM chain c JOIN inventory_stock s ON s.tenant_id=$1::uuid AND s.location_id=c.location_id
JOIN items i ON i.item_id=s.item_id
ORDER BY CASE WHEN s.status='active' AND s.quantity_in_stock>s.quantity_reserved
                   AND (s.expiry_date IS NULL OR s.expiry_date>=$4::date) THEN 0 ELSE 1 END,
         c.depth, s.expiry_date ASC NULLS LAST, s.stock_id`, tenantID, batchID, inventoryScopeLocationID, bizToday)
	if err != nil {
		return domain.TaskOptionSource{}, fmt.Errorf("vaccination execution: lot options: %w", err)
	}
	defer rows.Close()
	source := domain.TaskOptionSource{Source: "inventory.vaccine_lots.fefo", Options: []domain.TaskOptionValue{}}
	rank := 0
	for rows.Next() {
		var value, label, available, unit, status string
		var expiry pgtype.Date
		var depth int
		if err := rows.Scan(&value, &label, &available, &unit, &expiry, &status, &depth); err != nil {
			return source, err
		}
		option := domain.TaskOptionValue{Value: value, Label: label, AvailableQuantity: &available, QuantityUnit: &unit}
		if expiry.Valid {
			d := expiry.Time.UTC()
			option.ExpiryDate = &d
		}
		reason := vaccineLotDisabledReason(status, available, expiry, bizToday)
		if reason != "" {
			option.Disabled = true
			option.DisabledReason = &reason
		} else {
			rank++
			rr := rank
			option.FEFORank = &rr
		}
		source.Options = append(source.Options, option)
	}
	if len(source.Options) == 0 {
		reason := "no_vaccine_lots_for_task"
		source.DisabledReason = &reason
	}
	return source, rows.Err()
}

func vaccineLotDisabledReason(status, available string, expiry pgtype.Date, businessDate string) string {
	if status != "active" {
		return "lot_" + status
	}
	// expiry_date is a PostgreSQL DATE. Keep the comparison date-only instead of turning it
	// into an instant whose timezone can silently change the operational day.
	if expiry.Valid && expiry.Time.Format("2006-01-02") < businessDate {
		return "lot_expired"
	}
	if available == "0" {
		return "no_available_quantity"
	}
	return ""
}

func (r *Repository) routeSiteOptions(ctx context.Context, tenantID, batchID string) (domain.TaskOptionSource, error) {
	rows, err := r.pool.Query(ctx, `
-- projection-review: membership=obligation_instances for one batch with schedule route_site entries; group_key=route_site text; join_cardinality=protocol_versions is keyed by protocol_version_id and the lateral schedule expansion is intentionally DISTINCTed to option grain; pagination=one task batch option list, no page boundary; scope=explicit tenant_id+batch_id.
SELECT DISTINCT schedule ->> 'route_site'
FROM obligation_instances oi
JOIN protocol_versions pv ON pv.tenant_id=oi.tenant_id AND pv.protocol_version_id=oi.protocol_version_id
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(pv.rule_dsl -> 'schedule','[]'::jsonb)) schedule
WHERE oi.tenant_id=$1::uuid AND oi.batch_id=$2::uuid
  AND COALESCE(schedule ->> 'route_site','') <> ''
ORDER BY 1`, tenantID, batchID)
	if err != nil {
		return domain.TaskOptionSource{}, fmt.Errorf("vaccination execution: route site options: %w", err)
	}
	defer rows.Close()
	source := domain.TaskOptionSource{Source: "vaccination.route_sites", Options: []domain.TaskOptionValue{}}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return source, err
		}
		source.Options = append(source.Options, domain.TaskOptionValue{Value: value, Label: value})
	}
	if len(source.Options) == 0 {
		reason := "no_route_sites_for_task"
		source.DisabledReason = &reason
	}
	return source, rows.Err()
}

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

// CapacityConfig reads the tenant's daily operator animal cap config, falling back to the code default when
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

func (r *Repository) PlannedDriveSessionsForShed(ctx context.Context, tenantID, shedID string) ([]domain.PlannedSession, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cfg, err := r.CapacityConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
SELECT planned_date::date::text,
       SUM(animal_count)::int,
       BOOL_OR(capacity_status IN ('over_cap_required', 'capacity_action')) AS over_cap_required
FROM vaccination_drive_assignments
WHERE tenant_id = $1::uuid
  AND shed_id = $2::uuid
-- projection-review: membership=vaccination_drive_assignments rows for one tenant+shed; group_key=planned_date; join_cardinality=no joins, SUM/BOOL_OR aggregate only assignment rows generated by the planner; pagination=all sessions for one shed detail, bounded by persisted assignments and no page truncation; scope=explicit shed_id.
GROUP BY planned_date::date
ORDER BY planned_date::date`, tenantID, shedID)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: planned drive sessions: %w", err)
	}
	defer rows.Close()
	out := make([]domain.PlannedSession, 0)
	for rows.Next() {
		var session domain.PlannedSession
		var overCap bool
		if err := rows.Scan(&session.Date, &session.Vaccinations, &overCap); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan planned drive session: %w", err)
		}
		session.DailyLimit = cfg.MaxPerDay
		session.Capacity = domain.CapacityWithinCap
		if overCap {
			session.Capacity = domain.CapacityBreach
		}
		out = append(out, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: planned drive session rows: %w", err)
	}
	return out, nil
}

// ShedSummary returns the shed-wise rollup with ANIMAL-LEVEL Due counts plus the session-split planner's
// outputs (Sessions, Capacity, merged Status), filtered + offset-paginated, each row carrying the window
// total. Manager/Backup are attached later by the service.
//
// 5k-50k envelope (docs/decisions/operational-kernel-5k-50k-scale-envelope.md): this serves
// GET /vaccination/sheds directly from the canonical obligation/goat/capacity-config tables
// (shedSummaryCanonicalReadSQL) reconstructed point-in-time as of q.AsOf. A canonical read cannot be
// stale relative to the canonical write, so the serving-projection freshness gate and per-shed shard
// staleness gate (and their read-through-vs-503 failure mode) are removed. The
// vaccination_shed_projection_rows/_state/_shard_state read model and its projector were dropped
// (migrations 000187/000188); this canonical read is now the only serving path for GET /vaccination/sheds.
// Shed rows are bounded (a tenant has at most a few hundred sheds), so the god-CTE + COUNT(*) OVER() +
// LIMIT/OFFSET stays scale-safe within this envelope; the driving obligation_instances scan is
// tenant/status/due-indexed and query-plan-tested (canonical_read_plan_test.go). Freshness is nil.
func (r *Repository) ShedSummary(ctx context.Context, q domain.ShedSummaryQuery) ([]domain.ShedSummaryProjection, error) {
	return r.listShedCanonical(ctx, q)
}

// listShedCanonical runs the shed-wise rollup straight from canonical tables. The prior projection
// read model (vaccination_shed_projection_rows/_state/_shard_state) and its projector were dropped
// (migrations 000187/000188); this is now the only serving path, not a fallback alongside it.
func (r *Repository) listShedCanonical(ctx context.Context, q domain.ShedSummaryQuery) ([]domain.ShedSummaryProjection, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	dueBefore := q.DueBefore
	if dueBefore.IsZero() {
		dueBefore = asOf.Add(defaultExecutionHorizon)
	}
	cfg, err := r.CapacityConfig(ctx, q.TenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: shed summary capacity config: %w", err)
	}
	status, capacity := "", ""
	if q.Status != nil {
		status = string(*q.Status)
	}
	if q.Capacity != nil {
		capacity = string(*q.Capacity)
	}
	query := strings.Replace(shedSummaryCanonicalReadSQL, "__ORDER_BY__", shedSummaryOrderBy(q.Sort), 1)
	rows, err := r.pool.Query(ctx, query,
		q.TenantID, asOf, dueBefore, cfg.MaxPerDay, cfg.MaxBufferDays,
		optStr(q.ParkID), optStr(q.ShedID), optStr(q.Search), status, capacity, limit, q.Offset)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: shed summary canonical read: %w", err)
	}
	defer rows.Close()
	out := []domain.ShedSummaryProjection{}
	for rows.Next() {
		var row domain.ShedSummaryProjection
		var lastDone, nextDue pgtype.Timestamptz
		var capacityStatus, shedStatus, driveOperatorNames string
		if err := rows.Scan(&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName, &row.Animals, &row.DueAnimals, &row.OpenCells, &row.Sessions, &capacityStatus, &shedStatus, &lastDone, &nextDue, &driveOperatorNames, &row.TotalCount); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan shed summary: %w", err)
		}
		row.Capacity, row.Status = domain.CapacityStatus(capacityStatus), domain.ShedStatus(shedStatus)
		row.LastDone, row.NextDue = timePtr(lastDone), timePtr(nextDue)
		for _, name := range strings.Split(driveOperatorNames, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				row.DriveOperatorNames = append(row.DriveOperatorNames, name)
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// shedSummaryCanonicalReadSQL is the 5k-50k-envelope request-path read for GET /vaccination/sheds. It
// reconstructs each shed's animal-level Due/Done rollup and the session-split planner outputs
// (Sessions/Capacity/merged Status) straight from canonical obligation/goat/capacity history AS OF $2
// (as_of), then applies the request's park/shed/search/status/capacity filters and sort/offset page. This
// is now the ONLY serving path for the shed rollup; the former projector (vaccinationShedProjectionInsertSQL)
// and its parity test were removed with the dropped vaccination_shed_projection_* tables
// (migrations 000187/000188). Params: $1 tenant, $2 as_of, $3 due_before, $4 max_per_day, $5
// max_buffer_days, then $6 park_id, $7 shed_id, $8 search, $9 shed_status, $10 capacity_status, $11
// limit, $12 offset. __ORDER_BY__ is substituted from a closed whitelist in Go (shedSummaryOrderBy).
//
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md — shed rows are bounded (a few hundred sheds/tenant), driving obligation_instances scan is tenant/status/due-indexed and query-plan-tested (canonical_read_plan_test.go). Keyset replacement for the offset page is tracked as C35-020.
const shedSummaryCanonicalReadSQL = `
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
    COUNT(DISTINCT effective.goat_id) FILTER (
      WHERE effective.eff_status IN ('overdue', 'due', 'in_progress')
         OR effective.completion_status IN ('recorded', 'rejected')
    )::bigint AS due_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'overdue')::bigint AS overdue_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'scheduled')::bigint AS scheduled_animals,
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
scored AS (
  SELECT
    shed_rows.*,
    CASE WHEN open_cells <= 0 THEN 0 ELSE CEIL(open_cells::numeric / GREATEST($4::numeric, 1))::int END AS sessions
  FROM shed_rows
),
classified AS (
  SELECT
    scored.*,
    CASE
      WHEN sessions <= 1 THEN 'within_cap'
      WHEN sessions <= ($5::int + 1) THEN 'over_cap'
      ELSE 'capacity_breach'
    END AS capacity_status,
    CASE
      WHEN overdue_animals > 0 THEN 'overdue'
      WHEN sessions > ($5::int + 1) THEN 'needs_review'
      WHEN sessions > 1 THEN 'split'
      WHEN due_animals > 0 THEN 'due'
      WHEN scheduled_animals > 0 THEN 'scheduled'
      ELSE 'on_track'
    END AS shed_status
  FROM scored
),
drive_ops AS (
  -- projection-review: membership=vaccination_drive_assignments rows at persisted operator/date/physical-shed/partition grain; group_key=(park_id,physical_shed); join_cardinality=workforce_members is tenant+operator keyed 1:1 and DISTINCT operator names prevents partition rows from duplicating visible operators; pagination=drive_ops is pre-aggregated before the classified shed OFFSET/LIMIT window so page boundaries cannot change operator membership; scope=tenant plus optional park/shed filters applied by the outer classified shed row.
  SELECT
    vda.park_id::text AS park_id,
    vda.physical_shed AS shed_name,
    STRING_AGG(DISTINCT wm.display_name, ', ' ORDER BY wm.display_name) AS drive_operator_names
  FROM vaccination_drive_assignments vda
  JOIN workforce_members wm
    ON wm.tenant_id = vda.tenant_id
   AND wm.workforce_member_id = vda.operator_id
   AND wm.status = 'active'
  WHERE vda.tenant_id = $1::uuid
  GROUP BY vda.park_id, vda.physical_shed
)
SELECT
  park_id, park_name, shed_id, shed_name,
  animals, due_animals, open_cells, sessions, capacity_status, shed_status,
  last_done, next_due,
  COALESCE(drive_ops.drive_operator_names, '') AS drive_operator_names,
  COUNT(*) OVER()::bigint AS total_count
FROM classified
LEFT JOIN drive_ops USING (park_id, shed_name)
WHERE ($6::text = '' OR park_id = $6)
  AND ($7::text = '' OR shed_id = $7)
  AND ($8::text = '' OR shed_name ILIKE '%' || $8 || '%' OR park_name ILIKE '%' || $8 || '%')
  AND ($9::text = '' OR shed_status = $9)
  AND ($10::text = '' OR capacity_status = $10)
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
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	driveDueDate := pgtype.Date{}
	if q.DriveDueDate != nil {
		driveDueDate = pgtype.Date{Time: *q.DriveDueDate, Valid: true}
	}
	rows, err := r.pool.Query(ctx, shedAnimalListSQL, q.TenantID, q.ShedID, cursor, limit, asOf, driveDueDate)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: shed animals: %w", err)
	}
	defer rows.Close()
	out := []domain.ShedAnimalRow{}
	for rows.Next() {
		var row domain.ShedAnimalRow
		var tag1, tag2, breed, age, health pgtype.Text
		var lastDose, nextDue pgtype.Timestamptz
		if err := rows.Scan(&row.GoatID, &row.DisplayID, &tag1, &tag2, &breed, &row.Sex, &age, &row.Lifecycle, &health, &lastDose, &nextDue, &row.Status); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan shed animals: %w", err)
		}
		row.Tag1 = textPtr(tag1)
		row.Tag2 = textPtr(tag2)
		row.Breed = textPtr(breed)
		row.Age = textPtr(age)
		row.Health = textPtr(health)
		row.LastDose = timeStringPtr(lastDose)
		row.NextDue = timeStringPtr(nextDue)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: iterate shed animals: %w", err)
	}
	return out, nil
}

// shedAnimalListSQL is a bounded keyset scan of alive goats in one shed (goat_id > $3 drives the
// PK-ordered window, LIMIT bounds the page). Display ID + both tag identities are returned as-is; a null
// tag becomes NULL -> the UI renders "-", never a "missing id" badge. Last dose is the latest accepted
// vaccination completion; next due is the earliest open vaccination obligation date. status describes
// current vaccination work, not animal health or lifecycle.
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
      floor(extract(epoch FROM ($5::timestamptz - g.dob::timestamptz)) / 86400)::int::text || 'd'
    ELSE NULLIF(g.age_band, '')
  END AS age,
  g.lifecycle_status,
  NULLIF(g.health_status, '') AS health_status,
  vhist.last_dose,
  vnext.next_due,
  CASE
    WHEN g.lifecycle_status <> 'alive' THEN g.lifecycle_status
    WHEN COALESCE(g.health_status, '') IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu') THEN g.health_status
    WHEN vnext.actionable_now THEN 'due'
    WHEN vnext.next_due IS NOT NULL THEN 'scheduled'
    WHEN vhist.last_dose IS NOT NULL THEN 'up_to_date'
    ELSE 'no_record'
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
LEFT JOIN LATERAL (
  SELECT MAX(vc.administered_at) AS last_dose
  FROM vaccination_completions vc
  WHERE vc.tenant_id = g.tenant_id
    AND vc.goat_id = g.goat_id
    AND vc.status = 'accepted'
) vhist ON true
LEFT JOIN LATERAL (
  SELECT
    MIN(oi.due_at) AS next_due,
    BOOL_OR(oi.status IN ('due', 'in_progress', 'missed') OR (oi.status = 'scheduled' AND oi.due_at <= $5::timestamptz)) AS actionable_now
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
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')
) vnext ON true
WHERE g.tenant_id = $1::uuid
  AND g.shed_id = $2::uuid
  AND g.merged_into_goat_id IS NULL
  AND g.goat_id > $3::uuid
  AND (
    $6::date IS NULL OR EXISTS (
      SELECT 1
      FROM obligation_instances doi
      JOIN protocol_versions dpv
        ON dpv.tenant_id = doi.tenant_id
       AND dpv.protocol_version_id = doi.protocol_version_id
      JOIN protocol_definitions dpd
        ON dpd.tenant_id = dpv.tenant_id
       AND dpd.protocol_id = dpv.protocol_id
       AND dpd.category = 'vaccination'
      LEFT JOIN obligation_batches dob
        ON dob.tenant_id = doi.tenant_id
       AND dob.batch_id = doi.batch_id
      WHERE doi.tenant_id = g.tenant_id
        AND doi.target_type = 'goat'
        AND doi.target_id = g.goat_id
        AND doi.status NOT IN ('superseded', 'canceled', 'waived')
        AND (
          CASE
            WHEN doi.batch_id IS NOT NULL THEN COALESCE(dob.planned_date, (dob.window_start AT TIME ZONE 'Asia/Kolkata')::date, (dob.window_end AT TIME ZONE 'Asia/Kolkata')::date)
            ELSE (doi.due_at AT TIME ZONE 'Asia/Kolkata')::date
          END
        ) = $6::date
    )
  )
ORDER BY g.goat_id ASC
LIMIT $4;
`

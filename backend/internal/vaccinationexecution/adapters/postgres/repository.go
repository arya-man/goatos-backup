// Package postgres implements canonical vaccination execution reads over Postgres.
// projection-review: membership=tenant-scoped vaccination obligation/completion rows selected by each endpoint's date window and scope filter; group_key=endpoint-specific park/shed/stage/batch/rule/protocol grain carried through SQL comments below; join_cardinality=completion and terminal status history are pre-aggregated per obligation before joining and goat/location/protocol joins are keyed by tenant plus stable ids; pagination=all serving reads use keyset or bounded cohort pages with totals computed before page truncation; scope=tenant plus explicit park/shed filters resolved from canonical location ids.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	vaccinatdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/ports"
)

// authorizedParkFilter returns the park ids a park-scoped actor may read, or nil when the
// caller is tenant-wide with vaccination authority (no restriction). Derived from the
// request-context grants so read queries enforce park scope in-query (defence in depth)
// without a signature change.
//
// The authority definition is imported from the domain package rather than restated here.
// This adapter used to keep its own copy "because the HTTP adapter must not become an
// import dependency of the Postgres adapter" -- true, but the copy then drifted: the
// handler's tenant-wide test accepted VaccinationCampaign while this one accepted
// VaccinationRead, and the two park sets differed. A defence-in-depth filter that disagrees
// with the gate in front of it does not catch anything; it silently returns zero rows to an
// actor the gate already allowed (a tenant-wide Park Head holds Oversee, not Campaign, and
// got an empty screen). Both adapters legitimately depend on the domain, so the domain is
// where the one definition lives.
//
// A park-scoped actor with no resolvable parks gets a non-nil empty slice -> matches nothing.
func authorizedParkFilter(ctx context.Context, tenantID string) []string {
	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	// No grants = internal/test context (e.g., context.Background()): allow unrestricted access
	// for backward compatibility with integration tests and internal callers.
	if len(grants) == 0 {
		return nil
	}
	if domain.HasTenantWideAuthority(grants, tenantID) {
		return nil
	}
	return domain.AuthorizedParks(grants)
}

const (
	defaultQueryTimeout     = 3 * time.Second
	defaultClosedHistoryAge = 45 * 24 * time.Hour
	defaultExecutionHorizon = 30 * 24 * time.Hour

	// defaultDriveOptionsLimit bounds the command board's drive picker. The picker must stay
	// bounded (a tenant accumulates drives forever, so an unbounded read is a time bomb), but
	// the previous bound of 50 was set when a row was one BATCH; a row is now (batch, park), so
	// one drive running in two parks spends two slots and a 30-drive two-park programme
	// produces 60 rows. Ten real, scheduled drives then vanished from the picker with no signal
	// at all, and the operator looking for one concluded it was never planned. 200 covers a
	// two-park annual programme with headroom while staying a single indexed page; the overflow
	// signal below is what makes the number safe to reason about rather than merely larger.
	defaultDriveOptionsLimit = 200
)

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	// driveOptionsLimit is injectable so the page-boundary regression can prove overflow
	// behaviour on a two-row fixture instead of seeding 200 drives, which would make the
	// truncation test slow enough that nobody runs it.
	driveOptionsLimit int
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout, driveOptionsLimit: defaultDriveOptionsLimit}
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
	partitionLabel := ""
	if q.PartitionLabel != nil {
		partitionLabel = strings.TrimSpace(*q.PartitionLabel)
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
		q.OpenOnly, cursorPresent, cursorRank, cursorDueMicros, cursorRowKey, q.OperatorScopeActorID, partitionLabel)
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
		var sourceShedName, obligationID, sopTaskID, sopVersionID, completionID pgtype.Text
		var sopTaskRowVersion pgtype.Int4
		var dueAt pgtype.Timestamptz
		var obligationCount, scheduledCount, dueCount, inProgressCount, completedCount int64
		var missedCount, deferredCount, canceledCount, recordedCount, acceptedCount, scannedCount, proofSubmittedCount int64
		var rejectedCount, reversedCount, healthDeferredCount int64
		var workState string
		if err := rows.Scan(
			&p.ParkID,
			&p.ParkName,
			&p.ShedID,
			&p.ShedName,
			&p.PhysicalShed,
			&p.Partition,
			&sourceShedName,
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
			&scannedCount,
			&proofSubmittedCount,
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
		p.SourceShedName = textPtr(sourceShedName)
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
		p.ScannedCount = int(scannedCount)
		p.ProofSubmittedCount = int(proofSubmittedCount)
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
	cursorParkID, cursorParkName, cursorShedID, cursorShedName, cursorPartitionLabel, cursorStage := "", "", "", "", "", ""
	if q.Cursor != nil {
		cursorParkID = q.Cursor.ParkID
		cursorParkName = q.Cursor.ParkName
		cursorShedID = q.Cursor.ShedID
		cursorShedName = q.Cursor.ShedName
		cursorPartitionLabel = q.Cursor.PartitionLabel
		cursorStage = q.Cursor.Stage
	}
	// 5k-50k envelope (docs/decisions/operational-kernel-5k-50k-scale-envelope.md): serve operations
	// directly from the canonical obligation/completion tables via the keyset-paginated
	// vaccinationOperationsSQL instead of vaccination_operations_projection_rows. Canonical reads are never
	// stale relative to the canonical write, so the serving-projection freshness gate is removed.
	// Freshness is nil (always current).
	rows, err := r.pool.Query(ctx, vaccinationOperationsSQL,
		q.TenantID, asOf, dueBefore, parkID, shedID, cursorParkID, cursorShedID, cursorStage, fetchLimit, cursorParkName, cursorShedName, cursorPartitionLabel)
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
	cursorParkID, cursorParkName, cursorShedID, cursorShedName, cursorPartitionLabel, cursorStage := "", "", "", "", "", ""
	if q.Cursor != nil {
		cursorParkID = q.Cursor.ParkID
		cursorParkName = q.Cursor.ParkName
		cursorShedID = q.Cursor.ShedID
		cursorShedName = q.Cursor.ShedName
		cursorPartitionLabel = q.Cursor.PartitionLabel
		cursorStage = q.Cursor.Stage
	}
	rows, err := r.pool.Query(ctx, vaccinationScheduleWindowSQL, q.TenantID, asOf, monthStart, monthEnd, parkID,
		cursorParkID, cursorShedID, cursorStage, cursorParkName, cursorShedName, limit, restrictParks, cursorPartitionLabel)
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
		var originalPlanned pgtype.Date
		var shedID pgtype.Text
		var vaccineKeys []string
		var vaccineCodes []string
		var vaccineOriginalDates []byte
		var capacity string
		if err := rows.Scan(
			&planned,
			&originalPlanned,
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
			&vaccineOriginalDates,
			&row.TotalDoses,
			&capacity,
		); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan drive assignment: %w", err)
		}
		if planned.Valid {
			row.PlannedDate = planned.Time.Format("2006-01-02")
		}
		if originalPlanned.Valid {
			row.OriginalPlannedDate = originalPlanned.Time.Format("2006-01-02")
		}
		row.ShedID = textPtr(shedID)
		row.VaccineNames = driveAssignmentVaccineLabels(vaccineKeys)
		if vaccineCodes == nil {
			vaccineCodes = []string{}
		}
		row.VaccineCodes = vaccineCodes
		if len(vaccineOriginalDates) > 0 {
			if err := json.Unmarshal(vaccineOriginalDates, &row.VaccineOriginalDates); err != nil {
				return nil, fmt.Errorf("vaccination execution: decode vaccine original dates: %w", err)
			}
		}
		if row.VaccineOriginalDates == nil {
			row.VaccineOriginalDates = map[string]string{}
		}
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
    vda.assignment_id,
    vda.batch_id,
    COALESCE(override.original_drive_date, vda.planned_date) AS original_planned_date,
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
  LEFT JOIN LATERAL (
    SELECT MIN(NULLIF(dim.vaccine_code, '')) AS vaccine_code
    FROM protocol_rule_dimensions dim
    WHERE dim.tenant_id = pr.tenant_id
      AND dim.rule_id = pr.rule_id
  ) prd ON true
  LEFT JOIN protocol_versions pv
    ON pv.tenant_id = pr.tenant_id
   AND pv.protocol_version_id = pr.protocol_version_id
  LEFT JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
  LEFT JOIN vaccination_drive_date_overrides override
    ON override.tenant_id = vda.tenant_id
   AND override.park_id = vda.park_id
   AND (override.original_drive_date = vda.planned_date OR override.override_date = vda.planned_date)
   AND lower(btrim(override.vaccine_code)) = lower(btrim(NULLIF(prd.vaccine_code, '')))
   AND override.canceled_at IS NULL
  WHERE vda.tenant_id = $1::uuid
    AND ($4::text = '' OR vda.park_id::text = $4)
),
effective_assignments AS (
  SELECT
    effective_planned_date AS planned_date,
    MIN(original_planned_date) AS original_planned_date,
    assignment_id,
    batch_id,
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
    COALESCE(jsonb_object_agg(vaccine_code, original_planned_date::text) FILTER (WHERE vaccine_code IS NOT NULL), '{}'::jsonb) AS vaccine_original_dates,
    CASE
      WHEN COUNT(DISTINCT vaccine_key) FILTER (WHERE vaccine_key IS NOT NULL) > 0
      THEN animal_count * COUNT(DISTINCT vaccine_key) FILTER (WHERE vaccine_key IS NOT NULL)
      ELSE MAX(total_doses)
    END::int AS total_doses
  FROM assignment_vaccines
  GROUP BY effective_planned_date, assignment_id, batch_id, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, capacity_status, batch_status
)
SELECT
  effective.planned_date,
  effective.original_planned_date,
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
    THEN GREATEST(0, effective.animal_count - assignment_progress.done_animals)
    ELSE 0
  END AS due_animals,
  CASE
    WHEN effective.batch_status = 'completed' THEN effective.animal_count
    ELSE LEAST(effective.animal_count, assignment_progress.done_animals)
  END AS done_animals,
  0 AS deferred_animals,
  CASE
    WHEN effective.batch_status IN ('planned', 'in_progress')
     AND effective.planned_date < (now() AT TIME ZONE 'Asia/Kolkata')::date
    THEN GREATEST(0, effective.animal_count - assignment_progress.done_animals)
    ELSE 0
  END AS overdue_animals,
  COALESCE(effective.vaccine_keys, ARRAY[]::text[]),
  COALESCE(effective.vaccine_codes, ARRAY[]::text[]),
  effective.vaccine_original_dates,
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
LEFT JOIN LATERAL (
  SELECT COUNT(DISTINCT oi.target_id)::int AS done_animals
  FROM obligation_instances oi
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
   AND gsp.shed_id = effective.shed_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.batch_id = effective.batch_id
    AND oi.target_type = 'goat'
    AND g.shed_id = effective.shed_id
    AND (
      effective.partition_label = 'whole'
      OR regexp_replace(lower(btrim(effective.partition_label)), '^part[[:space:]]+', '')
       = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
    )
    AND (
      oi.status = 'completed'
      OR EXISTS (
        SELECT 1
        FROM vaccination_completions vc
        WHERE vc.tenant_id = oi.tenant_id
          AND vc.obligation_id = oi.obligation_id
          AND vc.status = 'accepted'
      )
    )
) assignment_progress ON true
WHERE effective.planned_date >= $2::date
  AND effective.planned_date < $3::date
ORDER BY effective.planned_date, wm.display_name, effective.physical_shed, effective.partition_label
LIMIT $5;
`

func scheduleCohortKey(row domain.OperationsRow) string {
	partition := ""
	if row.PartitionLabel != nil {
		partition = *row.PartitionLabel
	}
	return row.ParkID + "|" + row.ShedID + "|" + partition + "|" + row.Stage
}

func operationsCursorFromRow(row domain.OperationsRow) domain.OperationsCursor {
	return domain.OperationsCursor{
		ParkID:   row.ParkID,
		ParkName: row.ParkName,
		ShedID:   row.ShedID,
		ShedName: row.ShedName,
		PartitionLabel: func() string {
			if row.PartitionLabel == nil {
				return ""
			}
			return *row.PartitionLabel
		}(),
		Stage: row.Stage,
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
		var ageBand, partitionLabel pgtype.Text
		var nextDue, lastDose pgtype.Timestamptz
		var vaccineNames []string
		var animals, overdue, due, inProgress, scheduled, missed, deferred, accepted, proofPending, rejected, total int64
		if err := rows.Scan(
			&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName, &partitionLabel, &row.Stage, &ageBand,
			&row.ProtocolID, &row.ProtocolName, &animals, &nextDue, &lastDose,
			&vaccineNames,
			&overdue, &due, &inProgress, &scheduled, &missed, &deferred, &accepted, &proofPending, &rejected, &total,
		); err != nil {
			return nil, fmt.Errorf("vaccination execution: scan operations: %w", err)
		}
		row.AgeBand = textPtr(ageBand)
		row.PartitionLabel = textPtr(partitionLabel)
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
    -- projection-review: membership=accepted completions UNION the rejection archive, so an
    -- animal whose clip was sent back is outstanding work again instead of vanishing;
    -- group_key=obligation_id at this grain; join_cardinality=each branch yields at most one
    -- row per obligation and the archive branch is ranked BELOW accepted, so the union cannot
    -- multiply obligation membership; pagination=none here, the caller's keyset pages the
    -- outer projection; scope=tenant plus the caller's as_of instant.
    UNION ALL
    -- Rejected completions are MOVED to the rejection archive (migration 000093) so the animal
    -- becomes outstanding work again by default on every read. This reads them back so a
    -- sent-back animal stays visible instead of silently vanishing. Ranked BELOW
    -- recorded/accepted by the ordering above, so once the operator redoes the animal and the
    -- new clip is accepted, the live completion wins and the sent-back state clears itself.
    SELECT
      obligation_id, administered_at, original_created_at AS created_at,
      CASE
        WHEN rejected_at > $2::timestamptz THEN 'recorded'
        ELSE 'rejected'
      END AS asof_status
    FROM vaccination_completion_rejections
    WHERE tenant_id = $1::uuid
      AND COALESCE(administered_at, original_created_at) <= $2::timestamptz
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
    CASE WHEN oi.target_type = 'goat' THEN oi.target_id ELSE NULL END AS animal_id,
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
    NULLIF(gsp.partition_label, 'whole'::text) AS partition_label,
    g.park_id AS direct_park_uuid,
    c.effective_status AS completion_status,
    c.last_accepted_at,
    COALESCE(vda_member.assignment_planned_at, vda_guess.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) AS execution_due_at
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
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  -- projection-review: membership=this obligation's exact vaccination_drive_assignment_members row (obligation_id UNIQUE), falling back to the guess LATERAL only when the obligation has no member row; group_key=(tenant_id, obligation_id) one member row per obligation; join_cardinality=members->assignment many-to-one on the assignment PK so execution_due_at is 1:1 per obligation, guess LATERAL is a LIMIT 1 scalar fallback; pagination=n/a -- per-obligation scalar assignment date, not an aggregate across a page boundary; scope=park/shed from the batch's own scope, unchanged by this member join
  -- HYBRID: prefer exact member assignment, fall back to guess LATERAL when unbound
  LEFT JOIN vaccination_drive_assignment_members m
    ON m.tenant_id = oi.tenant_id
   AND m.obligation_id = oi.obligation_id
  LEFT JOIN vaccination_drive_assignments assignment
    ON assignment.tenant_id = m.tenant_id
   AND assignment.assignment_id = m.assignment_id
   AND assignment.shed_id = g.shed_id
  -- Guess path: find assignment via LATERAL when no membership
  LEFT JOIN LATERAL (
    SELECT (vda_guess.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
    FROM vaccination_drive_assignments vda_guess
    WHERE vda_guess.tenant_id = oi.tenant_id
      AND vda_guess.batch_id = oi.batch_id
      AND vda_guess.shed_id = g.shed_id
      AND (
        vda_guess.partition_label = 'whole'
        OR regexp_replace(lower(btrim(vda_guess.partition_label)), '^part[[:space:]]+', '')
         = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
      )
    ORDER BY vda_guess.created_at DESC
    LIMIT 1
  ) vda_guess ON true
  -- Member path: formatted assignment when membership exists
  LEFT JOIN LATERAL (
    SELECT (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
  ) vda_member ON assignment.assignment_id IS NOT NULL
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND (
      COALESCE(COALESCE(vda_member.assignment_planned_at, vda_guess.assignment_planned_at), ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) <= $4::timestamptz
      OR c.last_accepted_at BETWEEN $3::timestamptz AND $4::timestamptz
    )
),
located AS (
  SELECT
    raw.*,
    COALESCE(raw.direct_park_uuid, shed_loc.parent_location_id) AS park_uuid,
    CASE
      -- projection-review: bucket-grain=business-day overdue/due/scheduled compares the IST (Asia/Kolkata) calendar DATE of the execution date against the IST date of as_of, so a drive planned for today is due (not overdue) at any clock instant of that day and rolls to overdue only on the next business day
      WHEN (raw.execution_due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue'
      WHEN (COALESCE(raw.execution_due_at, raw.window_start, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'due'
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
    (located.execution_due_at BETWEEN $3::timestamptz AND $4::timestamptz) AS due_in_window,
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
  SELECT windowed.park_uuid, windowed.shed_uuid, windowed.partition_label, windowed.stage
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
    OR (park.name, shed.name, COALESCE(windowed.partition_label, ''), windowed.stage, windowed.park_uuid, windowed.shed_uuid) >
       ($9::text, $10::text, $13::text, $8::text, NULLIF($6::text, '')::uuid, NULLIF($7::text, '')::uuid)
  )
  GROUP BY windowed.park_uuid, park.name, windowed.shed_uuid, shed.name, windowed.partition_label, windowed.stage
  ORDER BY park.name, shed.name, COALESCE(windowed.partition_label, ''), windowed.stage, windowed.park_uuid, windowed.shed_uuid
  LIMIT $11::int
)
SELECT
  windowed.park_uuid,
  park.name AS park_name,
  windowed.shed_uuid,
  shed.name AS shed_name,
  windowed.partition_label,
  windowed.stage,
  (ARRAY_AGG(windowed.age_band) FILTER (WHERE windowed.age_band IS NOT NULL))[1] AS age_band,
  windowed.protocol_id,
  windowed.protocol_name,
  COUNT(DISTINCT windowed.goat_id)::bigint AS animals,
  MIN(windowed.execution_due_at) FILTER (WHERE windowed.due_in_window AND windowed.eff_status IN ('overdue', 'due', 'in_progress', 'scheduled')) AS next_due,
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
 AND COALESCE(page.partition_label, '') = COALESCE(windowed.partition_label, '')
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
GROUP BY windowed.park_uuid, park.name, windowed.shed_uuid, shed.name, windowed.partition_label, windowed.stage, windowed.protocol_id, windowed.protocol_name
ORDER BY
  park.name COLLATE "C" ASC, windowed.park_uuid ASC,
  shed.name COLLATE "C" ASC, windowed.shed_uuid ASC,
  COALESCE(windowed.partition_label, '') COLLATE "C" ASC,
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
  UNION ALL
  -- projection-review: membership=accepted completions UNION the rejection archive, so an animal
  -- whose clip was sent back is outstanding work again; group_key=obligation_id at this grain;
  -- join_cardinality=each side contributes at most one row per obligation before the outer join,
  -- so the union cannot multiply obligation membership; pagination=inherited from the caller's
  -- keyset, this CTE is not paged itself; scope=tenant plus the caller's as_of instant.
  -- Rejected completions no longer live in vaccination_completions: they are MOVED to the
  -- rejection archive (migration 000093) so the animal becomes outstanding work again on every
  -- read by default. The shed's Sent-back state still has to be visible, so the archive is read
  -- back in here as a candidate with status 'rejected', keyed on the SAME obligation_id.
  --
  -- Self-clearing, which is the whole point of keying on the obligation rather than the animal:
  -- the ordering in the completions CTE below ranks recorded/accepted ahead of everything else, so
  -- moment the operator redoes that animal and the new clip is accepted, the live completion wins
  -- and the shed stops reading Sent back. No flag to reset, no cleanup job -- and a goat rejected
  -- in a previous cycle can never make today's drive look sent back, because that rejection
  -- belongs to a different obligation.
  SELECT
    obligation_id,
    completion_id,
    administered_at,
    original_created_at AS created_at,
    CASE
      WHEN rejected_at > $7::timestamptz THEN 'recorded'
      ELSE 'rejected'
    END AS asof_status
  FROM vaccination_completion_rejections
  WHERE tenant_id = $1::uuid
    AND COALESCE(administered_at, original_created_at) <= $7::timestamptz
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
    CASE WHEN oi.target_type = 'goat' THEN oi.target_id ELSE NULL END AS animal_id,
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
    (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS batch_planned_at,
    ob.status AS batch_status,
    -- projection-review: membership=this obligation's exact vaccination_drive_assignment_members row (obligation_id UNIQUE), falling back to the guess LATERAL only when the obligation has no member row; group_key=(tenant_id, obligation_id) one member row per obligation; join_cardinality=members->assignment many-to-one on the assignment PK so operator/date/shed/partition are 1:1 per obligation, guess LATERAL is a LIMIT 1 scalar fallback; pagination=n/a -- per-obligation scalar columns, not an aggregate across a page boundary; scope=park/shed from the batch's own scope, unchanged by this member join
    COALESCE(vda_member.operator_id, vda_guess.operator_id) AS conducted_by,
    COALESCE(vda_member.assignment_planned_at, vda_guess.assignment_planned_at) AS assignment_planned_at,
    COALESCE(vda_member.physical_shed, vda_guess.physical_shed) AS physical_shed,
    COALESCE(NULLIF(btrim(gsp.partition_label), ''), NULLIF(btrim(vda_member.partition_label), ''), 'whole') AS partition_label,
    regexp_replace(lower(btrim(COALESCE(NULLIF(btrim(gsp.partition_label), ''), NULLIF(btrim(vda_member.partition_label), ''), 'whole'))), '^part[[:space:]]+', '') AS partition_key,
    NULLIF(btrim(gsp.source_shed_name), '') AS source_shed_name,
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
    sc.capture_id IS NOT NULL AS scanned,
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
  LEFT JOIN LATERAL (
    SELECT MIN(NULLIF(dim.vaccine_code, '')) AS vaccine_code
    FROM protocol_rule_dimensions dim
    WHERE dim.tenant_id = pr.tenant_id
      AND dim.rule_id = pr.rule_id
  ) prd ON true
  LEFT JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
   AND gsp.shed_id = g.shed_id
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  -- projection-review: membership=this obligation's exact vaccination_drive_assignment_members row (obligation_id UNIQUE), HYBRID fallback to the guess LATERAL (vda_guess LIMIT 1) only when the obligation has no member row; group_key=(park_uuid, shed_uuid, batch_id, rule_id, protocol_name, dose_code) with COUNT(*) at obligation grain; join_cardinality=members->assignment many-to-one on the assignment PK so operator/date is 1:1 per obligation (vda_member LATERAL 1:1 when bound, vda_guess LATERAL LIMIT 1 fallback when unbound); pagination=keyset-grouped rows pre-aggregated before GROUP BY, total_count over the full tenant/category/scope/date-filtered set; scope=park/shed via located.park_uuid/shed_uuid with explicit tenant filter, location joins 1:1 per shed_uuid
  -- HYBRID: prefer exact member assignment, fall back to guess LATERAL when unbound;
  -- operator_filter applies on COALESCE(vda_member.operator_id, vda_guess.operator_id) result
  LEFT JOIN vaccination_drive_assignment_members m
    ON m.tenant_id = oi.tenant_id
   AND m.obligation_id = oi.obligation_id
  LEFT JOIN vaccination_drive_assignments assignment
    ON assignment.tenant_id = m.tenant_id
   AND assignment.assignment_id = m.assignment_id
   AND assignment.shed_id = CASE
        WHEN g.shed_id IS NOT NULL THEN g.shed_id
        WHEN oi.target_type = 'shed' THEN oi.target_id
        WHEN oi.scope_type = 'shed' THEN oi.scope_id
        ELSE NULL
      END
   AND (
        assignment.partition_label = 'whole'
        OR gsp.partition_label IS NULL
        OR regexp_replace(lower(btrim(assignment.partition_label)), '^part[[:space:]]+', '')
         = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
      )
  -- Guess path: find assignment via LATERAL when no membership
  LEFT JOIN LATERAL (
    SELECT
      vda_guess.operator_id,
      (COALESCE(override.override_date, vda_guess.planned_date)::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at,
      vda_guess.physical_shed,
      vda_guess.partition_label
    FROM vaccination_drive_assignments vda_guess
    LEFT JOIN vaccination_drive_date_overrides override
      ON override.tenant_id = vda_guess.tenant_id
     AND override.park_id = vda_guess.park_id
     AND (override.original_drive_date = vda_guess.planned_date OR override.override_date = vda_guess.planned_date)
     AND lower(btrim(override.vaccine_code)) = lower(btrim(NULLIF(prd.vaccine_code, '')))
     AND override.canceled_at IS NULL
    WHERE vda_guess.tenant_id = oi.tenant_id
      AND vda_guess.batch_id = oi.batch_id
      AND vda_guess.shed_id = CASE
            WHEN g.shed_id IS NOT NULL THEN g.shed_id
            WHEN oi.target_type = 'shed' THEN oi.target_id
            WHEN oi.scope_type = 'shed' THEN oi.scope_id
            ELSE NULL
          END
      AND (
            vda_guess.partition_label = 'whole'
            OR (gsp.partition_label IS NOT NULL
             AND regexp_replace(lower(btrim(vda_guess.partition_label)), '^part[[:space:]]+', '')
             = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
            )
          )
    ORDER BY vda_guess.created_at DESC
    LIMIT 1
  ) vda_guess ON true
  -- Member path: formatted assignment when membership exists
  LEFT JOIN LATERAL (
    SELECT
      member_assignment.operator_id,
      (COALESCE(override.override_date, member_assignment.planned_date)::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at,
      member_assignment.physical_shed,
      member_assignment.partition_label
    FROM vaccination_drive_assignments member_assignment
    LEFT JOIN vaccination_drive_date_overrides override
      ON override.tenant_id = member_assignment.tenant_id
     AND override.park_id = member_assignment.park_id
     AND (override.original_drive_date = member_assignment.planned_date OR override.override_date = member_assignment.planned_date)
     AND lower(btrim(override.vaccine_code)) = lower(btrim(NULLIF(prd.vaccine_code, '')))
     AND override.canceled_at IS NULL
    WHERE member_assignment.tenant_id = assignment.tenant_id
      AND member_assignment.assignment_id = assignment.assignment_id
  ) vda_member ON assignment.assignment_id IS NOT NULL
  LEFT JOIN sop_tasks st
    ON st.tenant_id = oi.tenant_id
   AND st.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN LATERAL (
    SELECT scan.capture_id
    FROM sop_task_scan_captures scan
    WHERE scan.tenant_id = oi.tenant_id
      AND scan.task_id = st.task_id
      AND scan.field_key IN ('goat_ids', '__scan_roster__')
      AND (
        scan.obligation_id = oi.obligation_id
        OR (scan.obligation_id IS NULL AND scan.goat_id = oi.target_id)
      )
    ORDER BY scan.captured_at DESC, scan.capture_id DESC
    LIMIT 1
  ) sc ON st.task_id IS NOT NULL
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND COALESCE(ob.status, '') NOT IN ('canceled', 'superseded')
    AND COALESCE(st.state, '') <> 'canceled'
    AND ($15::text = '' OR COALESCE(vda_member.operator_id, vda_guess.operator_id) IS NOT NULL)
    AND COALESCE(COALESCE(vda_member.assignment_planned_at, vda_guess.assignment_planned_at), ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) <= $4::timestamptz
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
    COALESCE(shed_proof.submitted_count, 0) > 0 AS shed_proof_submitted,
    COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AS execution_due_at,
    -- as_of-effective obligation status (reconstructed AT as_of, not the current stored status).
    CASE
      WHEN raw.obligation_status = 'completed' THEN
        CASE
          WHEN raw.completed_at IS NOT NULL AND raw.completed_at <= $7::timestamptz THEN 'completed'
          WHEN raw.completed_at IS NULL AND raw.completion_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN (COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date < ($7::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue' WHEN (COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.window_start, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date <= ($7::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'due' ELSE 'scheduled' END)
        END
      WHEN raw.obligation_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          WHEN raw.asof_terminal_type IS NOT NULL THEN raw.asof_terminal_type
          WHEN raw.has_terminal_event THEN (CASE WHEN (COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date < ($7::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue' WHEN (COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.window_start, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date <= ($7::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'due' ELSE 'scheduled' END)
          ELSE raw.obligation_status
        END
      WHEN raw.obligation_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN (COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date < ($7::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue' WHEN (COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.window_start, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date <= ($7::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM raw
  LEFT JOIN locations shed_loc
    ON shed_loc.tenant_id = $1::uuid
   AND shed_loc.location_id = raw.shed_uuid
   AND shed_loc.location_type = 'shed'
  LEFT JOIN LATERAL (
    SELECT COUNT(*)::bigint AS submitted_count
    FROM sop_submissions submission
    CROSS JOIN LATERAL jsonb_array_elements(submission.proof_refs) AS proof(ref)
    WHERE submission.tenant_id = $1::uuid
      AND submission.task_id = raw.sop_task_id
      AND submission.state IN ('submitted', 'needs_review')
      AND proof.ref ->> 'upload_state' = 'completed'
      AND proof.ref ->> 'proof_type' = 'video'
      AND proof.ref ->> 'subject_type' = 'shed'
      AND proof.ref ->> 'subject_id' = raw.shed_uuid::text
  ) shed_proof ON raw.sop_task_id IS NOT NULL AND raw.shed_uuid IS NOT NULL
  WHERE raw.shed_uuid IS NOT NULL
),
animal_rollup AS (
  SELECT
    located.park_uuid,
    located.shed_uuid,
    located.partition_key,
    located.batch_id,
    located.animal_id,
    BOOL_OR(located.eff_status = 'scheduled') AS has_scheduled,
    BOOL_OR(located.eff_status = 'due') AS has_due,
    BOOL_OR(located.eff_status = 'in_progress') AS has_in_progress,
    BOOL_OR(located.eff_status = 'completed') AS has_completed,
    BOOL_OR(located.eff_status = 'missed') AS has_missed,
    BOOL_OR(located.eff_status IN ('waived', 'deferred')) AS has_deferred,
    BOOL_OR(located.completion_status = 'recorded') AS has_recorded_completion,
    BOOL_OR(located.completion_status = 'accepted') AS has_accepted_completion,
    BOOL_OR(located.completion_status = 'rejected') AS has_rejected_completion,
    BOOL_OR(located.completion_status = 'reversed') AS has_reversed_completion,
    BOOL_AND(COALESCE(located.completion_status = 'accepted', false)) AS all_completions_accepted,
    BOOL_OR(located.scanned) AS has_scan,
    BOOL_OR(located.shed_proof_submitted) AS has_shed_proof
  FROM located
  WHERE located.park_uuid IS NOT NULL
    AND ($2::text = '' OR located.park_uuid = $2::uuid)
    AND ($3::text = '' OR located.shed_uuid = $3::uuid)
    AND (
      $15::text = ''
      OR located.conducted_by IN (SELECT workforce_member_id FROM operator_scope_member)
    )
  GROUP BY located.park_uuid, located.shed_uuid, located.partition_key, located.batch_id, located.animal_id
),
animal_counts AS (
  SELECT
    animal_rollup.park_uuid,
    animal_rollup.shed_uuid,
    animal_rollup.partition_key,
    animal_rollup.batch_id,
    COUNT(*)::bigint AS obligation_count,
    COUNT(*) FILTER (WHERE animal_rollup.has_scheduled)::bigint AS scheduled_count,
    COUNT(*) FILTER (WHERE animal_rollup.has_due)::bigint AS due_count,
    COUNT(*) FILTER (WHERE animal_rollup.has_in_progress)::bigint AS in_progress_count,
    COUNT(*) FILTER (WHERE animal_rollup.has_completed)::bigint AS completed_count,
    COUNT(*) FILTER (WHERE animal_rollup.has_missed)::bigint AS missed_count,
    COUNT(*) FILTER (WHERE animal_rollup.has_deferred)::bigint AS deferred_count,
    COUNT(*) FILTER (WHERE animal_rollup.has_recorded_completion AND NOT animal_rollup.all_completions_accepted)::bigint AS completion_recorded,
    COUNT(*) FILTER (WHERE animal_rollup.has_accepted_completion AND animal_rollup.all_completions_accepted)::bigint AS completion_accepted,
    COUNT(*) FILTER (WHERE animal_rollup.has_rejected_completion AND NOT animal_rollup.all_completions_accepted)::bigint AS completion_rejected,
    COUNT(*) FILTER (WHERE animal_rollup.has_reversed_completion AND NOT animal_rollup.all_completions_accepted)::bigint AS completion_reversed,
    COUNT(*) FILTER (WHERE animal_rollup.has_scan)::bigint AS scanned_count,
    COUNT(*) FILTER (WHERE animal_rollup.has_shed_proof)::bigint AS proof_submitted_count
  FROM animal_rollup
  GROUP BY animal_rollup.park_uuid, animal_rollup.shed_uuid, animal_rollup.partition_key, animal_rollup.batch_id
),
-- projection-review: membership=located obligation-grain rows after tenant/category/scope resolution, with batch planned_date carried as execution_due_at for batched rows; group_key=(park_uuid,shed_uuid,partition_key,batch_id) so sibling partitions under one physical shed remain separate mobile/admin execution cards while counts stay at distinct-animal grain; join_cardinality=completions pre-aggregates 0:N completion history to one effective row per obligation, goat/batch/task joins are keyed 1:1, drive assignments are collapsed through LEFT JOIN LATERAL ... LIMIT 1 before grouping, and animal-stage joins use tenant-scoped unique id/code keys so COUNT/ARRAY_AGG stay at animal grain; pagination=grouped rows feed classified keyset pagination and total_count over the full filtered set; scope=park/shed/partition via located.park_uuid/shed_uuid/partition_key and tenant-scoped location joins.
-- projection-review: bucket-grain=business-day eff_status and work_state overdue compare the IST (Asia/Kolkata) calendar DATE of the execution date against the IST date of as_of ($7), so a row whose drive is planned for today reads due (not overdue) at any clock instant and rolls to overdue only on the next business day
grouped AS (
  SELECT
    located.park_uuid,
    located.shed_uuid,
    located.partition_key,
    located.batch_id,
    (ARRAY_AGG(located.rule_id ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST, located.rule_id DESC))[1] AS rule_id,
    (ARRAY_AGG(located.protocol_name ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST, located.protocol_name ASC))[1] AS protocol_name,
    (ARRAY_AGG(located.dose_code ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST, located.dose_code ASC))[1] AS dose_code,
    MIN(located.execution_due_at) AS due_at,
    MAX(animal_counts.obligation_count) AS obligation_count,
    -- Mobile shows drive animals, not obligation/dose rows. Bucket counts use the
    -- as_of-effective status at distinct-animal grain; completion counts use the
    -- pre-aggregated, as-of-bounded completion projection above.
    MAX(animal_counts.scheduled_count) AS scheduled_count,
    MAX(animal_counts.due_count) AS due_count,
    MAX(animal_counts.in_progress_count) AS in_progress_count,
    MAX(animal_counts.completed_count) AS completed_count,
    MAX(animal_counts.missed_count) AS missed_count,
    MAX(animal_counts.deferred_count) AS deferred_count,
    0::bigint AS canceled_count,
    MAX(animal_counts.completion_recorded) AS completion_recorded,
    MAX(animal_counts.completion_accepted) AS completion_accepted,
    MAX(animal_counts.completion_rejected) AS completion_rejected,
    MAX(animal_counts.completion_reversed) AS completion_reversed,
    MAX(animal_counts.scanned_count) AS scanned_count,
    MAX(animal_counts.proof_submitted_count) AS proof_submitted_count,
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
    (ARRAY_AGG(located.source_shed_name ORDER BY located.execution_due_at DESC NULLS LAST, located.source_shed_name ASC NULLS LAST)
      FILTER (WHERE NULLIF(located.source_shed_name, '') IS NOT NULL))[1] AS source_shed_name,
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
    COUNT(DISTINCT located.animal_id) FILTER (
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
  JOIN animal_counts
    ON animal_counts.park_uuid = located.park_uuid
   AND animal_counts.shed_uuid = located.shed_uuid
   AND animal_counts.partition_key = located.partition_key
   AND animal_counts.batch_id IS NOT DISTINCT FROM located.batch_id
  WHERE located.park_uuid IS NOT NULL
    AND ($2::text = '' OR located.park_uuid = $2::uuid)
    AND ($3::text = '' OR located.shed_uuid = $3::uuid)
    AND (
      $15::text = ''
      OR located.conducted_by IN (SELECT workforce_member_id FROM operator_scope_member)
    )
    AND (
      $16::text = ''
      OR located.partition_key = regexp_replace(lower(btrim($16::text)), '^part[[:space:]]+', '')
    )
  GROUP BY located.park_uuid, located.shed_uuid, located.partition_key, located.batch_id
),
enriched AS (
  SELECT
    grouped.*,
    assignment_operator.display_name AS assignment_operator_name,
    COALESCE(loa.usable_for_vaccination, true) AS usable_for_vaccination,
    COALESCE(loa.is_quarantine, false) AS is_quarantine,
    COALESCE(loa.is_icu, false) AS is_icu
  FROM grouped
  LEFT JOIN location_operational_attributes loa
    ON loa.tenant_id = $1::uuid
   AND loa.location_id = grouped.shed_uuid
  LEFT JOIN LATERAL (
    SELECT wm.display_name
    FROM vaccination_drive_assignments vda
    JOIN workforce_members wm
      ON wm.tenant_id = vda.tenant_id
     AND wm.workforce_member_id = vda.operator_id
     AND wm.status = 'active'
    WHERE vda.tenant_id = $1::uuid
      AND vda.batch_id = grouped.batch_id
      AND vda.shed_id = grouped.shed_uuid
      AND vda.operator_id IS NOT NULL
      AND (
        grouped.partition_label = 'whole'
        OR vda.partition_label = grouped.partition_label
      )
    ORDER BY vda.planned_date DESC, vda.updated_at DESC, vda.assignment_id DESC
    LIMIT 1
  ) assignment_operator ON grouped.operator_name IS NULL
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
      WHEN COALESCE(enriched.operator_name, enriched.assignment_operator_name) IS NULL
       AND enriched.completed_count < enriched.obligation_count THEN 'blocked'
      WHEN enriched.task_state IN ('rework_requested', 'rejected') THEN 'rejected'
      WHEN enriched.completion_recorded > 0
        OR enriched.proof_submitted_count > 0 THEN 'verification_pending'
      WHEN enriched.in_progress_count > 0
        OR enriched.batch_status = 'in_progress'
        OR enriched.task_state = 'in_progress' THEN 'in_progress'
      WHEN (enriched.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($7::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue'
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
    stateful.park_uuid::text || '|' || stateful.shed_uuid::text || '|' ||
      stateful.partition_key || '|' ||
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
  grouped.source_shed_name,
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
  grouped.scanned_count,
  grouped.proof_submitted_count,
  grouped.batch_status,
  grouped.task_state,
  COALESCE(grouped.operator_name, grouped.assignment_operator_name, scoped_operator.display_name) AS operator_name,
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
LEFT JOIN LATERAL (
  SELECT wm.display_name
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
) scoped_operator ON grouped.operator_name IS NULL AND grouped.assignment_operator_name IS NULL
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
// SERVING SHAPE (BUG-036a): the effective-due-date window is bounded by an INDEX-USABLE superset on
// bare obligation_instances columns (due_at window OR a batch id from due_window_batches) before the
// exact COALESCE bound is applied as a residual filter, and the per-obligation drive-assignment
// LATERAL is pre-aggregated once per (batch, shed) in drive_assignment_dates. Without both, the
// planner had to read the whole obligation table per request. Gated by
// TestVaccinationOperationsAggregateQueryPlanUsesIndexesAtScale at ~500k rows and by the
// EffectiveDueWindowSuperset case in make validate-sqlc-plans.
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
    -- projection-review: membership=accepted completions UNION the rejection archive, so an
    -- animal whose clip was sent back is outstanding work again instead of vanishing;
    -- group_key=obligation_id at this grain; join_cardinality=each branch yields at most one
    -- row per obligation and the archive branch is ranked BELOW accepted, so the union cannot
    -- multiply obligation membership; pagination=none here, the caller's keyset pages the
    -- outer projection; scope=tenant plus the caller's as_of instant.
    UNION ALL
    -- Rejected completions are MOVED to the rejection archive (migration 000093), so they must be
    -- read back here or a sent-back animal disappears from this surface instead of showing as work
    -- still owed. Ranked BELOW recorded/accepted by the ordering above, so the state clears itself
    -- once the animal is redone and the new clip is accepted.
    SELECT
      obligation_id, administered_at, original_created_at AS created_at,
      CASE
        WHEN rejected_at > $2::timestamptz THEN 'recorded'
        ELSE 'rejected'
      END AS asof_status
    FROM vaccination_completion_rejections
    WHERE tenant_id = $1::uuid
      AND COALESCE(administered_at, original_created_at) <= $2::timestamptz
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
drive_assignment_dates AS (
  -- ONE representative planned date per (batch, shed, partition), pre-aggregated ONCE instead of probed per
  -- obligation. This was a LEFT JOIN LATERAL ... LIMIT 1 correlated on (oi.batch_id, g.shed_id): a
  -- per-row index probe into vaccination_drive_assignments, i.e. one round trip per obligation row,
  -- which dominated the plan cost at the 500k envelope even after the due window became index-bound.
  -- Distinct (batch, shed) pairs are bounded by planned DRIVES, not by animals, so collapsing to one
  -- row per pair up front is strictly cheaper and set-based. DISTINCT ON reproduces the LATERAL's
  -- ORDER BY ... LIMIT 1 tie-break exactly, so the selected assignment date is unchanged.
  SELECT DISTINCT ON (
    assignment.batch_id,
    assignment.shed_id,
    regexp_replace(lower(btrim(COALESCE(assignment.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
    assignment.batch_id,
    assignment.shed_id,
    regexp_replace(lower(btrim(COALESCE(assignment.partition_label, 'whole'))), '^part[[:space:]]+', '') AS partition_key,
    (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
  FROM vaccination_drive_assignments assignment
  WHERE assignment.tenant_id = $1::uuid
  ORDER BY assignment.batch_id,
           assignment.shed_id,
           regexp_replace(lower(btrim(COALESCE(assignment.partition_label, 'whole'))), '^part[[:space:]]+', ''),
           assignment.planned_date ASC,
           assignment.partition_label ASC,
           assignment.operator_id ASC NULLS LAST,
           assignment.assignment_id ASC
),
due_window_batches AS (
  -- SARGABLE PRE-FILTER SOURCE for the effective-due-date window below. The serving predicate is
  -- COALESCE(assignment_planned_at, batch_planned_date, oi.due_at) <= $3, whose first two arms come from a
  -- LEFT JOIN / LEFT JOIN LATERAL output. A predicate over a join output is not index-usable, so the
  -- planner was forced to materialize EVERY obligation row of the tenant before it could filter -- a full
  -- 500k sequential scan of obligation_instances (BUG-036a).
  -- An obligation's effective date can only differ from oi.due_at when a batch or a drive assignment
  -- overrides it, and BOTH override sources hang off oi.batch_id (ob.batch_id = oi.batch_id, and the
  -- LATERAL assignment join keys on assignment.batch_id = oi.batch_id). Therefore:
  --   effective_due <= $3 AND oi.due_at > $3  =>  oi.batch_id is a batch planned <= $3 (by the batch row
  --   itself or by one of its drive assignments).
  -- Collecting those batch ids from the SMALL planning tables lets the driving scan ride
  -- obligation_instances_due_window_idx (tenant_id, status, due_at, obligation_id) and
  -- obligation_instances_batch_idx (tenant_id, batch_id, status) instead of reading the whole table.
  -- This is a strict SUPERSET: the exact COALESCE predicate is still applied afterwards, so no row that
  -- qualified before is dropped and no new row is admitted. Serving shape only, semantics unchanged.
  SELECT ob.batch_id
  FROM obligation_batches ob
  WHERE ob.tenant_id = $1::uuid
    AND (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') <= $3::timestamptz
  UNION
  SELECT assignment.batch_id
  FROM vaccination_drive_assignments assignment
  WHERE assignment.tenant_id = $1::uuid
    AND (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') <= $3::timestamptz
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
    NULLIF(gsp.partition_label, 'whole'::text) AS partition_label,
    g.park_id AS direct_park_uuid,
    c.effective_status AS completion_status,
    c.last_accepted_at,
    COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) AS execution_due_at
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
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN drive_assignment_dates vda
    ON vda.batch_id = oi.batch_id
   AND vda.shed_id = g.shed_id
   AND (
     vda.partition_key = 'whole'
     OR vda.partition_key = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
   )
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    -- INDEX-USABLE SUPERSET of the effective-due-date bound below. Both arms are bare
    -- obligation_instances column predicates, so the planner can BitmapOr
    -- obligation_instances_due_window_idx (tenant_id, status, due_at, obligation_id) with
    -- obligation_instances_batch_idx (tenant_id, batch_id, status) and prune the table instead of
    -- scanning it whole. The batch list is materialized as an InitPlan ARRAY (not a correlated
    -- IN-subquery) precisely so it stays a constant the index can be probed with.
    AND (
      oi.due_at <= $3::timestamptz
      OR oi.batch_id = ANY (ARRAY(SELECT batch_id FROM due_window_batches))
    )
    -- Exact effective-due-date bound (unchanged). Kept as the authoritative filter so a batch/assignment
    -- that moved the date LATER than $3 is still excluded even though the superset admitted it.
    AND COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) <= $3::timestamptz
),
located AS (
  SELECT
    raw.*,
    COALESCE(raw.direct_park_uuid, shed_loc.parent_location_id) AS park_uuid,
    -- open_bucket reconstructs the non-terminal state purely from the due window vs as_of (deterministic,
    -- needs no event history): overdue once due_at has passed, due once the window has opened, else scheduled.
    CASE
      -- projection-review: bucket-grain=business-day overdue/due/scheduled compares the IST (Asia/Kolkata) calendar DATE of the execution date against the IST date of as_of, so a drive planned for today is due (not overdue) at any clock instant of that day and rolls to overdue only on the next business day
      WHEN (raw.execution_due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue'
      WHEN (COALESCE(raw.execution_due_at, raw.window_start, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'due'
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
  SELECT effective.park_uuid, effective.shed_uuid, effective.partition_label, effective.stage
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
        COALESCE(effective.partition_label, '') COLLATE "C",
        effective.stage COLLATE "C"
      ) > (
        SELECT
          lower(cursor_location.park_name) COLLATE "C", cursor_location.park_name COLLATE "C", NULLIF($6, '')::uuid,
          lower(cursor_location.shed_name) COLLATE "C", cursor_location.shed_name COLLATE "C", NULLIF($7, '')::uuid,
          $12::text COLLATE "C",
          $8::text COLLATE "C"
        FROM cursor_location
      )
    )
  GROUP BY effective.park_uuid, park.name, effective.shed_uuid, shed.name, effective.partition_label, effective.stage
  ORDER BY
    lower(park.name) COLLATE "C" ASC, park.name COLLATE "C" ASC, effective.park_uuid ASC,
    lower(shed.name) COLLATE "C" ASC, shed.name COLLATE "C" ASC, effective.shed_uuid ASC,
    COALESCE(effective.partition_label, '') COLLATE "C" ASC,
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
  effective.partition_label,
  effective.stage,
  (ARRAY_AGG(effective.age_band) FILTER (WHERE effective.age_band IS NOT NULL))[1] AS age_band,
  effective.protocol_id,
  effective.protocol_name,
  COUNT(DISTINCT effective.goat_id)::bigint AS animals,
  MIN(effective.execution_due_at) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress', 'scheduled')) AS next_due,
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
 AND COALESCE(page.partition_label, '') = COALESCE(effective.partition_label, '')
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
GROUP BY effective.park_uuid, park.name, effective.shed_uuid, shed.name, effective.partition_label, effective.stage, effective.protocol_id, effective.protocol_name
ORDER BY
  lower(park.name) COLLATE "C" ASC, park.name COLLATE "C" ASC, effective.park_uuid ASC,
  lower(shed.name) COLLATE "C" ASC, shed.name COLLATE "C" ASC, effective.shed_uuid ASC,
  COALESCE(effective.partition_label, '') COLLATE "C" ASC,
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
	rows, err := r.pool.Query(ctx, scanRosterSQL, q.TenantID, q.ShedID, q.TaskID, identity.BatchID, cursorGoatID, cursorObligationID, limit+1, restrictParks, q.OperatorScopeActorID, strings.TrimSpace(q.PartitionLabel))
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
			&row.ObligationRowVersion,
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
-- projection-review: membership=one task-pinned shed roster row per vaccination obligation for the selected shed, additionally bounded to the operator's drive-assignment partition when operator-scoped and the caller's explicit partition when supplied; group_key=(tenant_id,shed_id,partition_key,task_id,goat_id,obligation_id); join_cardinality=goat/protocol/tag joins are tenant-keyed and the scan capture table is collapsed through LEFT JOIN LATERAL ... LIMIT 1 so multiple scans cannot duplicate an obligation row; pagination=keyset over (goat_id,obligation_id) after status/scanned_at projection, so page boundaries do not change row membership; scope=tenant plus explicit shed_id, optional partition/task_id/batch_id pinning, operator partition, and authorized park filter.
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
    -- projection-review: membership=obligations decorated with this animal's own latest verdict;
    -- group_key=obligation_id (per ANIMAL, never per shed); join_cardinality=verdict and scan
    -- presence are each pre-reduced to one row per obligation before this CASE reads them, so a
    -- re-scanned animal cannot duplicate its obligation; pagination=applied by the caller after
    -- this projection; scope=tenant plus the caller's park/shed filter.
    -- A verifier's verdict on THIS animal outranks the fact that it was scanned. Proof is
    -- captured per animal, so the verdict is issued per animal: an animal whose clip was sent
    -- back is outstanding work again, and an accepted animal is finished and must not be
    -- offered for re-capture. Reading only the scan-capture presence made every scanned
    -- animal 'done' regardless of verdict, so a rejected animal stayed green on the scan
    -- screen and the operator could not tell WHICH animal to redo -- while the four accepted
    -- ones stayed re-scannable.
    -- SENT BACK: the verifier refused this animal's clip, so its completion was moved to the
    -- rejection archive and the animal owes the work again. It reports 'due', NOT 'done' and not
    -- a bespoke status: 'due' is the vocabulary every client already treats as outstanding, so
    -- the animal reappears in the pending list, is re-scannable, and needs no special case.
    -- Its old scan capture is deliberately ignored below (scanned_at goes NULL) -- the animal WAS
    -- scanned, but that scan's proof was rejected, so presenting it as scanned would put a green
    -- tick on the one animal the operator has to redo.
    WHEN vc.completion_status = 'rejected' THEN 'due'
    WHEN vc.completion_status = 'accepted' THEN 'completed'
    -- A live completion IS the done evidence, and it does not depend on the scan-capture join
    -- below (which only resolves when a task id was supplied). Without this, a shed-wide roster
    -- read reported every animal as 'due' -- the four that were accepted looked identical to the
    -- one that was sent back.
    WHEN vc.completion_status = 'recorded' THEN 'done'
    WHEN sc.capture_id IS NOT NULL THEN 'done'
    WHEN oi.status = 'due' OR (COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) < now() AND oi.status = 'scheduled') THEN 'due'
    WHEN oi.status = 'in_progress' THEN 'in_progress'
    WHEN oi.status = 'completed' THEN 'completed'
    WHEN oi.status IN ('deferred', 'missed', 'waived') THEN 'deferred'
    ELSE 'pending'
  END AS status,
  -- NULL for a sent-back animal: it must present as not-yet-scanned so the row carries no
  -- "Proof synced" tick and the client's own done/pending split puts it back in pending.
  CASE WHEN vc.completion_status = 'rejected' THEN NULL ELSE COALESCE(sc.captured_at, vcm.administered_at) END AS scanned_at,
  oi.obligation_id::text,
  oi.row_version
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
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id
 AND gsp.goat_id = g.goat_id
 AND gsp.shed_id = g.shed_id
LEFT JOIN LATERAL (
  SELECT (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
  FROM vaccination_drive_assignments assignment
  WHERE assignment.tenant_id = oi.tenant_id
    AND assignment.batch_id = oi.batch_id
    AND assignment.shed_id = g.shed_id
    AND (
      assignment.partition_label = 'whole'
      OR regexp_replace(lower(btrim(assignment.partition_label)), '^part[[:space:]]+', '')
       = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
    )
    AND (
      $9::text = ''
      OR assignment.operator_id IN (SELECT workforce_member_id FROM operator_scope_member)
    )
    AND (
      cardinality(assignment.vaccine_rule_ids) = 0
      OR assignment.vaccine_rule_ids @> ARRAY[oi.rule_id]
    )
  ORDER BY assignment.planned_date ASC,
           assignment.partition_label ASC,
           assignment.operator_id ASC NULLS LAST,
           assignment.assignment_id ASC
  LIMIT 1
) vda ON true
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
-- This animal's own latest verdict, keyed on its obligation so one goat's rejection can never
-- be read onto another's row. Collapsed through LIMIT 1 exactly like the scan-capture lateral
-- above, so a re-capture cannot duplicate the roster row.
--
-- It MUST read the rejection archive as well as the live table. A rejected completion is moved
-- out of vaccination_completions (migration 000093), so a lookup against the live table alone can
-- never see status='rejected' -- the branch would be dead code, the rejected animal would fall
-- through to the scan-capture rule below and render DONE/green exactly like its accepted
-- shed-mates, and the operator would have no way to tell which animal to redo. That is the very
-- defect this whole change exists to remove.
--
-- Ordering puts a live recorded/accepted row ahead of an archived rejection for the same
-- obligation, so the state clears itself the moment the animal is redone and accepted.
LEFT JOIN LATERAL (
  SELECT vcx.administered_at
  FROM vaccination_completions vcx
  WHERE vcx.tenant_id = oi.tenant_id
    AND vcx.obligation_id = oi.obligation_id
  ORDER BY vcx.updated_at DESC, vcx.completion_id DESC
  LIMIT 1
) vcm ON true
LEFT JOIN LATERAL (
  SELECT completion_status
  FROM (
    SELECT vcc.status AS completion_status, vcc.updated_at, vcc.completion_id,
           CASE WHEN vcc.status IN ('recorded', 'accepted') THEN 0 ELSE 1 END AS live_rank
    FROM vaccination_completions vcc
    WHERE vcc.tenant_id = oi.tenant_id
      AND vcc.obligation_id = oi.obligation_id
    UNION ALL
    SELECT 'rejected', vcr.rejected_at, vcr.completion_id, 1
    FROM vaccination_completion_rejections vcr
    WHERE vcr.tenant_id = oi.tenant_id
      AND vcr.obligation_id = oi.obligation_id
  ) verdicts
  ORDER BY live_rank ASC, updated_at DESC, completion_id DESC
  LIMIT 1
) vc ON true
WHERE oi.tenant_id = $1::uuid
  AND g.shed_id = $2::uuid
  AND (
    $3 = ''
    OR oi.sop_task_id = NULLIF($3, '')::uuid
    OR ($4 <> '' AND oi.batch_id = NULLIF($4, '')::uuid)
  )
  AND ($4 = '' OR oi.batch_id = NULLIF($4, '')::uuid)
  AND oi.status NOT IN ('waived', 'canceled', 'superseded')
  AND ($9::text = '' OR vda.assignment_planned_at IS NOT NULL)
  AND vda.assignment_planned_at IS NOT NULL
  AND (
    $10::text = ''
    OR regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
     = regexp_replace(lower(btrim($10::text)), '^part[[:space:]]+', '')
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
            OR
            (st.scope_type = 'park' AND ob.scope_type = 'tenant' AND ob.scope_id = st.tenant_id AND g.park_id = st.scope_id)
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

// projection-review: membership=one vaccination_capacity_config row per tenant; group_key=tenant_id (PK); join_cardinality=1:1 tenant-to-config, single-table read; pagination=none (single-row read); scope=tenant-scoped
const capacityConfigSQL = `
SELECT max_per_day, capacity_scope, max_buffer_days, overflow_policy, row_version, max_shots_per_animal_per_drive
FROM vaccination_capacity_config
WHERE tenant_id = $1::uuid;`

// CapacityConfig reads the tenant's daily operator animal cap config, falling back to the code default when
// no row is authored (migration 000155 seeds existing tenants; later tenants use the default). Reads are
// served by this package; writes are also owned here (see UpsertCapacityConfig) for the admin-editable
// max_per_day + max_shots_per_animal_per_drive override (migration 000045).
func (r *Repository) CapacityConfig(ctx context.Context, tenantID string) (domain.CapacityConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cfg := domain.DefaultCapacityConfig()
	err := r.pool.QueryRow(ctx, capacityConfigSQL, tenantID).
		Scan(&cfg.MaxPerDay, &cfg.CapacityScope, &cfg.MaxBufferDays, &cfg.OverflowPolicy, &cfg.RowVersion, &cfg.MaxShotsPerAnimalPerDrive)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultCapacityConfig(), nil
	}
	if err != nil {
		return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: capacity config: %w", err)
	}
	return cfg, nil
}

// UpsertCapacityConfig idempotently writes the tenant's daily operator animal cap + per-animal shot-cap
// override (migration 000045) with optimistic concurrency and durably enqueues vaccination.capacity.changed
// to outbox_messages in the SAME transaction as the config write, so either both commit or neither does.
// Unlike UpsertOperatorAssignmentConfig (one row per park), vaccination_capacity_config is tenant-scoped
// (PK is tenant_id, migration 000001) -- there is no single park to key the cascade event on. The write
// therefore fans the cascade out to every active park of the tenant (one event per park, same
// OperatorConfigReplanHandler consumer, same idempotency-key discipline per park), so
// RecomputeFutureVaccinationDrives re-plans every park's future drives against the new cap/shot-cap.
// cfg.RowVersion == 0 means "first write, row must not already exist" (in practice every tenant already
// has a seeded row, so this path is defensive); any other value must match the currently stored
// row_version or ports.ErrCapacityConfigConflict is returned.
func (r *Repository) UpsertCapacityConfig(ctx context.Context, tenantID string, cfg domain.CapacityConfig) (domain.CapacityConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: begin capacity config update tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var newVersion int
	if cfg.RowVersion == 0 {
		err := tx.QueryRow(ctx, `
INSERT INTO vaccination_capacity_config
  (tenant_id, max_per_day, capacity_scope, max_buffer_days, overflow_policy, max_shots_per_animal_per_drive, row_version, updated_at)
VALUES ($1::uuid, $2, $3, $4, $5, $6, 1, now())
ON CONFLICT (tenant_id) DO NOTHING
RETURNING row_version;`, tenantID, cfg.MaxPerDay, cfg.CapacityScope, cfg.MaxBufferDays, cfg.OverflowPolicy, cfg.MaxShotsPerAnimalPerDrive).Scan(&newVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CapacityConfig{}, ports.ErrCapacityConfigConflict
		}
		if err != nil {
			return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: insert capacity config: %w", err)
		}
	} else {
		err := tx.QueryRow(ctx, `
UPDATE vaccination_capacity_config
SET max_per_day = $2,
    capacity_scope = $3,
    max_buffer_days = $4,
    overflow_policy = $5,
    max_shots_per_animal_per_drive = $6,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid AND row_version = $7
RETURNING row_version;`, tenantID, cfg.MaxPerDay, cfg.CapacityScope, cfg.MaxBufferDays, cfg.OverflowPolicy, cfg.MaxShotsPerAnimalPerDrive, cfg.RowVersion).Scan(&newVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CapacityConfig{}, ports.ErrCapacityConfigConflict
		}
		if err != nil {
			return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: update capacity config: %w", err)
		}
	}
	cfg.RowVersion = newVersion

	// Fan the cascade out to every active park of the tenant (see doc comment above for why: this
	// config is tenant-scoped, not park-scoped).
	// projection-review: membership=active park locations for the tenant; group_key=location_id (one row each); join_cardinality=1 row per active park, single-table read; pagination=none (bounded park fan-out, no user paging); scope=tenant-scoped, park_id carried per emitted event
	parkRows, err := tx.Query(ctx, `
SELECT location_id::text
FROM locations
WHERE tenant_id = $1::uuid AND location_type = 'park' AND status = 'active'
ORDER BY location_id ASC`, tenantID)
	if err != nil {
		return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: list active parks for capacity cascade: %w", err)
	}
	var parkIDs []string
	for parkRows.Next() {
		var id string
		if err := parkRows.Scan(&id); err != nil {
			parkRows.Close()
			return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: scan active park id: %w", err)
		}
		parkIDs = append(parkIDs, id)
	}
	if err := parkRows.Err(); err != nil {
		parkRows.Close()
		return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: active parks rows: %w", err)
	}
	parkRows.Close()

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	for _, parkID := range parkIDs {
		idempotencyKey := fmt.Sprintf("vaccination.capacity-config.capacity:%s:%d", parkID, newVersion)
		eventID := platformoutbox.DeterministicUUID(idempotencyKey)
		envelope, err := json.Marshal(map[string]any{
			"event_id":       eventID,
			"event_type":     "vaccination.capacity.changed",
			"schema_version": "1.0.0",
			"schema_ref":     "domain-event-envelope.v1",
			"aggregate_type": "park",
			"aggregate_id":   parkID,
			"occurred_at":    now,
			"recorded_at":    now,
			"producer": map[string]any{
				"service": "goatos-api",
				"module":  "vaccination-execution",
				"version": nil,
			},
			"idempotency_key": idempotencyKey,
			"actor": map[string]any{
				"actor_type": "system_rule",
				"actor_id":   nil,
				"actor_ref":  nil,
			},
			"subject_type": "location",
			"subject_id":   parkID,
			"visibility_scope": map[string]any{
				"tenant_id": tenantID,
				"park_id":   parkID,
			},
			"evidence_refs": []map[string]string{{
				"evidence_type": "location",
				"evidence_id":   parkID,
			}},
			"payload":  map[string]any{"park_id": parkID},
			"trace_id": idempotencyKey,
		})
		if err != nil {
			return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: marshal capacity.changed envelope: %w", err)
		}
		headers, err := json.Marshal(map[string]any{
			"producer":        "vaccination-execution.UpsertCapacityConfig",
			"schema_version":  "1.0.0",
			"park_id":         parkID,
			"idempotency_key": idempotencyKey,
		})
		if err != nil {
			return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: marshal capacity.changed headers: %w", err)
		}
		// Bounded fan-out over a tenant's active parks (tens, not millions) on a rare admin cap edit,
		// not a per-request/per-goat path; one outbox row per park is the intended cascade grain.
		// scale-guard:ignore: bounded active-park fan-out on rare admin cap edit, O(parks-per-tenant)
		if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES ($1::uuid, $2::uuid, 'vaccination.capacity.changed', '1.0.0', 'park', $3::uuid,
  'vaccination.events', $4::jsonb, $5::jsonb, $6, $6, 'pending', now())
ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'vaccination.capacity.changed' DO NOTHING`,
			tenantID, eventID, parkID, envelope, headers, idempotencyKey); err != nil {
			return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: enqueue capacity.changed to outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.CapacityConfig{}, fmt.Errorf("vaccination execution: commit capacity config update tx: %w", err)
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
		if err := rows.Scan(&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName, &row.PartitionLabel, &row.Animals, &row.DueAnimals, &row.OpenCells, &row.Sessions, &capacityStatus, &shedStatus, &lastDone, &nextDue, &driveOperatorNames, &row.TotalCount); err != nil {
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
  -- projection-review: membership=all alive goats at tenant grain; group_key=(shed_id, partition_label)
  -- so animals from different partitions do not merge; partition_label comes from goat_shed_partitions
  -- and is NULLIF-ed to 'whole' so undivided sheds carry a stable false-partition value for GROUP BY consistency.
  -- join_cardinality=goat_shed_partitions is PK (tenant_id, goat_id) so LEFT JOIN is 0..1 per goat;
  -- scope=tenant_id, carried on both sides of the JOIN.
  SELECT g.shed_id AS shed_uuid, NULLIF(gsp.partition_label, 'whole'::text) AS partition_label, COUNT(*)::bigint AS animals
  FROM goats g
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  WHERE g.tenant_id = $1::uuid
    AND g.lifecycle_status = 'alive'
    AND g.merged_into_goat_id IS NULL
    AND g.shed_id IS NOT NULL
  GROUP BY g.shed_id, NULLIF(gsp.partition_label, 'whole'::text)
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
    -- projection-review: membership=accepted completions UNION the rejection archive, so an
    -- animal whose clip was sent back is outstanding work again instead of vanishing;
    -- group_key=obligation_id at this grain; join_cardinality=each branch yields at most one
    -- row per obligation and the archive branch is ranked BELOW accepted, so the union cannot
    -- multiply obligation membership; pagination=none here, the caller's keyset pages the
    -- outer projection; scope=tenant plus the caller's as_of instant.
    UNION ALL
    -- Rejected completions are MOVED to the rejection archive (migration 000093) so the animal
    -- becomes outstanding work again by default on every read. This reads them back so a
    -- sent-back animal stays visible instead of silently vanishing. Ranked BELOW
    -- recorded/accepted by the ordering above, so once the operator redoes the animal and the
    -- new clip is accepted, the live completion wins and the sent-back state clears itself.
    SELECT
      obligation_id, administered_at, original_created_at AS created_at,
      CASE
        WHEN rejected_at > $2::timestamptz THEN 'recorded'
        ELSE 'rejected'
      END AS asof_status
    FROM vaccination_completion_rejections
    WHERE tenant_id = $1::uuid
      AND COALESCE(administered_at, original_created_at) <= $2::timestamptz
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
    NULLIF(gsp.partition_label, 'whole'::text) AS partition_label,
    c.effective_status AS completion_status,
    c.last_accepted_at,
    COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) AS execution_due_at
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
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN LATERAL (
    SELECT (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
    FROM vaccination_drive_assignments assignment
    WHERE assignment.tenant_id = oi.tenant_id
      AND assignment.batch_id = oi.batch_id
      AND assignment.shed_id = g.shed_id
    ORDER BY assignment.planned_date ASC,
             assignment.partition_label ASC,
             assignment.operator_id ASC NULLS LAST,
             assignment.assignment_id ASC
    LIMIT 1
  ) vda ON true
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) <= $3::timestamptz
    AND g.shed_id IS NOT NULL
),
-- projection-review: bucket-grain=business-day the overdue/due/scheduled reconstruction compares the IST (Asia/Kolkata) calendar DATE of execution_due_at against the IST date of as_of ($2), so a shed whose drive is planned for today reads due (not overdue) at any clock instant and rolls to overdue only on the next business day; grain=(shed_uuid, partition_label) so animals from different partitions do not merge
effective AS (
  SELECT
    raw.goat_id,
    raw.shed_uuid,
    raw.partition_label,
    raw.execution_due_at,
    raw.last_accepted_at,
    raw.completion_status,
    CASE
      WHEN raw.stored_status = 'completed' THEN
        CASE
          WHEN raw.completed_at IS NOT NULL AND raw.completed_at <= $2::timestamptz THEN 'completed'
          WHEN raw.completed_at IS NULL AND raw.completion_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN (raw.execution_due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue' WHEN (COALESCE(raw.execution_due_at, raw.window_start, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'due' ELSE 'scheduled' END)
        END
      WHEN raw.stored_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          WHEN raw.asof_terminal_type IS NOT NULL THEN raw.asof_terminal_type
          WHEN raw.has_terminal_event THEN (CASE WHEN (raw.execution_due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue' WHEN (COALESCE(raw.execution_due_at, raw.window_start, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'due' ELSE 'scheduled' END)
          ELSE raw.stored_status
        END
      WHEN raw.stored_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN (raw.execution_due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue' WHEN (COALESCE(raw.execution_due_at, raw.window_start, raw.due_at) AT TIME ZONE 'Asia/Kolkata')::date <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM raw
),
due_agg AS (
  -- projection-review: group_key=(shed_uuid, partition_label) so work metrics from different partitions
  -- do not merge; count is at animal grain (DISTINCT goat_id) so completion/rejection status is captured
  -- once per unique animal-metric pair; scope=tenant plus effective filtering applied earlier by effective CTE.
  SELECT
    effective.shed_uuid,
    effective.partition_label,
    COUNT(DISTINCT effective.goat_id) FILTER (
      WHERE effective.eff_status IN ('overdue', 'due', 'in_progress')
         OR effective.completion_status IN ('recorded', 'rejected')
    )::bigint AS due_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'overdue')::bigint AS overdue_animals,
    COUNT(DISTINCT effective.goat_id) FILTER (WHERE effective.eff_status = 'scheduled')::bigint AS scheduled_animals,
    COUNT(*) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress'))::bigint AS open_cells,
    MAX(effective.last_accepted_at) AS last_done,
    MIN(effective.execution_due_at) FILTER (WHERE effective.eff_status IN ('overdue', 'due', 'in_progress', 'scheduled')) AS next_due
  FROM effective
  GROUP BY effective.shed_uuid, effective.partition_label
),
shed_rows AS (
  SELECT
    park.location_id::text AS park_id,
    park.name AS park_name,
    shed.location_id::text AS shed_id,
    shed.name AS shed_name,
    alive.partition_label,
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
  LEFT JOIN due_agg ON due_agg.shed_uuid = alive.shed_uuid AND COALESCE(due_agg.partition_label, '') = COALESCE(alive.partition_label, '')
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
  -- projection-review: membership=vaccination_drive_assignments rows at persisted operator/date/physical-shed/partition grain plus accepted seed-history completions whose source packet resolves to the park default operator; group_key=(park_id,physical_shed); join_cardinality=workforce_members is tenant+operator keyed 1:1, completion->goat is 1:1 by goat_id, shed->park is 1:1 by location parent, and DISTINCT operator names prevents partition/history rows from duplicating visible operators; pagination=drive_ops is pre-aggregated before the classified shed OFFSET/LIMIT window so page boundaries cannot change operator membership; scope=tenant plus optional park/shed filters applied by the outer classified shed row.
  SELECT
    operator_sources.park_id,
    operator_sources.shed_name,
    STRING_AGG(DISTINCT wm.display_name, ', ' ORDER BY wm.display_name) AS drive_operator_names
  FROM (
    SELECT
      vda.park_id::text AS park_id,
      vda.physical_shed AS shed_name,
      vda.operator_id
    FROM vaccination_drive_assignments vda
    WHERE vda.tenant_id = $1::uuid

    UNION ALL

    SELECT
      park.location_id::text AS park_id,
      shed.name AS shed_name,
      cfg.default_operator_id AS operator_id
    FROM vaccination_completions vc
    JOIN goats g
      ON g.tenant_id = vc.tenant_id
     AND g.goat_id = vc.goat_id
     AND g.lifecycle_status = 'alive'
     AND g.merged_into_goat_id IS NULL
    JOIN locations shed
      ON shed.tenant_id = vc.tenant_id
     AND shed.location_id = g.shed_id
     AND shed.location_type = 'shed'
     AND shed.status = 'active'
    JOIN locations park
      ON park.tenant_id = vc.tenant_id
     AND park.location_id = shed.parent_location_id
     AND park.location_type = 'park'
     AND park.status = 'active'
    JOIN vaccination_operator_assignment_config cfg
      ON cfg.tenant_id = vc.tenant_id
     AND cfg.park_id = park.location_id
    WHERE vc.tenant_id = $1::uuid
      AND vc.status = 'accepted'
      AND COALESCE(vc.administered_at, vc.created_at) <= $2::timestamptz
  ) operator_sources
  JOIN workforce_members wm
    ON wm.tenant_id = $1::uuid
   AND wm.workforce_member_id = operator_sources.operator_id
   AND wm.status = 'active'
  -- operational-location:ignore: owner=ravi issue=OL-17 scope=park-scoped-shed-grain expiry=2026-11-30
  -- Keyed by (park_id, shed_name), not shed_id, and that is deliberate for THIS read:
  -- vaccination_drive_assignments stores physical_shed as a NAME, and the sibling
  -- UNION branch derives the name from locations, so there is no shared id to key on
  -- without changing the assignment table. The park_id in the key prevents the
  -- cross-park merge the guard exists to catch (two Castro, two Gandhi, two Yashoda
  -- across parks stay separate). Partitions of one shed intentionally collapse here:
  -- the column answers "who ran drives in this shed", which is shed grain, not
  -- partition grain.
  -- RESIDUAL RISK, stated rather than hidden: if one park ever holds two sheds with
  -- the same name, their operator lists merge. Retiring this ignore means giving
  -- vaccination_drive_assignments a real shed_id and keying both sides on it.
  GROUP BY operator_sources.park_id, operator_sources.shed_name -- operational-location:ignore: owner=ravi issue=OL-17 scope=park-scoped-shed-grain expiry=2026-11-30
)
SELECT
  park_id, park_name, shed_id, shed_name, partition_label,
  animals, due_animals, open_cells, sessions, capacity_status, shed_status,
  last_done, next_due,
  COALESCE(drive_ops.drive_operator_names, '') AS drive_operator_names,
  COUNT(*) OVER()::bigint AS total_count
FROM classified
-- operational-location:ignore: owner=ravi issue=OL-17 scope=park-scoped-shed-grain expiry=2026-11-30
-- Consumer half of the drive_ops key documented at its GROUP BY above: park_id is in the
-- key, so the cross-park name collision this rule guards against cannot happen here.
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
    MIN(COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at)) AS next_due,
    BOOL_OR(oi.status IN ('due', 'in_progress', 'missed') OR (oi.status = 'scheduled' AND COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) <= $5::timestamptz)) AS actionable_now
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN LATERAL (
    SELECT (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
    FROM vaccination_drive_assignments assignment
    WHERE assignment.tenant_id = oi.tenant_id
      AND assignment.batch_id = oi.batch_id
      AND assignment.shed_id = g.shed_id
    ORDER BY assignment.planned_date ASC,
             assignment.partition_label ASC,
             assignment.operator_id ASC NULLS LAST,
             assignment.assignment_id ASC
    LIMIT 1
  ) vda ON true
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
      LEFT JOIN LATERAL (
        SELECT assignment.planned_date AS assignment_planned_date
        FROM vaccination_drive_assignments assignment
        WHERE assignment.tenant_id = doi.tenant_id
          AND assignment.batch_id = doi.batch_id
          AND assignment.shed_id = g.shed_id
        ORDER BY assignment.planned_date ASC,
                 assignment.partition_label ASC,
                 assignment.operator_id ASC NULLS LAST,
                 assignment.assignment_id ASC
        LIMIT 1
      ) drive_date ON true
      WHERE doi.tenant_id = g.tenant_id
        AND doi.target_type = 'goat'
        AND doi.target_id = g.goat_id
        AND doi.status NOT IN ('superseded', 'canceled', 'waived')
        AND (
          CASE
            WHEN doi.batch_id IS NOT NULL THEN COALESCE(drive_date.assignment_planned_date, dob.planned_date, (dob.window_start AT TIME ZONE 'Asia/Kolkata')::date, (dob.window_end AT TIME ZONE 'Asia/Kolkata')::date)
            ELSE (doi.due_at AT TIME ZONE 'Asia/Kolkata')::date
          END
        ) = $6::date
    )
  )
ORDER BY g.goat_id ASC
LIMIT $4;
`

// ---- Vaccination operator shift + assignment config ----
// (vaccination_operator_assignment_config / vaccination_operator_shift_config, migration 000035).
// The admin config screen authors these rows; the drive/obligation scheduler consumes them.

// UpsertOperatorAssignmentConfig returns ports.ErrOperatorAssignmentConfigConflict when the caller's
// RowVersion does not match the currently stored row (optimistic concurrency: reject, never clobber).

const operatorAssignmentConfigSQL = `
SELECT active_operators_per_day,
       default_operator_id,
       ARRAY(SELECT operator_id::text FROM unnest(selected_operator_ids) AS operator_id),
       row_version
FROM vaccination_operator_assignment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid;`

// OperatorAssignmentConfig reads the park's N-active-operators-per-day + default-operator config.
// found=false (no error) when no row is authored yet for this park.
func (r *Repository) OperatorAssignmentConfig(ctx context.Context, tenantID, parkID string) (domain.OperatorAssignmentConfig, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cfg := domain.OperatorAssignmentConfig{ParkID: parkID}
	err := r.pool.QueryRow(ctx, operatorAssignmentConfigSQL, tenantID, parkID).
		Scan(&cfg.ActiveOperatorsPerDay, &cfg.DefaultOperatorID, &cfg.SelectedOperatorIDs, &cfg.RowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OperatorAssignmentConfig{}, false, nil
	}
	if err != nil {
		return domain.OperatorAssignmentConfig{}, false, fmt.Errorf("vaccination execution: operator assignment config: %w", err)
	}
	return cfg, true, nil
}

const operatorShiftsSQL = `
SELECT s.operator_id, COALESCE(m.display_name, ''), s.shift_label, s.shift_start_minute, s.shift_end_minute,
       COALESCE(s.week_off_weekday, '')
FROM vaccination_operator_shift_config s
LEFT JOIN workforce_members m
  ON m.tenant_id = s.tenant_id AND m.workforce_member_id = s.operator_id
WHERE s.tenant_id = $1::uuid AND s.park_id = $2::uuid
ORDER BY CASE s.shift_label WHEN 'am' THEN 0 WHEN 'rover' THEN 1 WHEN 'pm' THEN 2 ELSE 3 END, s.operator_id ASC;`

// OperatorShifts returns every operator's authored shift row for this park.
func (r *Repository) OperatorShifts(ctx context.Context, tenantID, parkID string) ([]domain.OperatorShift, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, operatorShiftsSQL, tenantID, parkID)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: operator shifts: %w", err)
	}
	defer rows.Close()
	var out []domain.OperatorShift
	for rows.Next() {
		var s domain.OperatorShift
		if err := rows.Scan(&s.OperatorID, &s.DisplayName, &s.ShiftLabel, &s.ShiftStartMinute, &s.ShiftEndMinute, &s.WeekOffWeekday); err != nil {
			return nil, fmt.Errorf("vaccination execution: operator shifts scan: %w", err)
		}
		s.ParkID = parkID
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: operator shifts rows: %w", err)
	}
	return out, nil
}

// UpsertOperatorAssignmentConfig idempotently writes the park's assignment config with optimistic
// concurrency and durably enqueues cascade events (vaccination.capacity.changed, vaccination.roster.changed)
// to outbox_messages in the same transaction. cfg.RowVersion == 0 means "first write, row must not already
// exist"; any other value must match the currently stored row_version or ports.ErrOperatorAssignmentConfigConflict
// is returned. The config write and event enqueue are atomic: if either fails, the whole write fails.
func (r *Repository) UpsertOperatorAssignmentConfig(ctx context.Context, tenantID string, cfg domain.OperatorAssignmentConfig) (domain.OperatorAssignmentConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// selected_operator_ids is uuid[] NOT NULL; a nil Go slice marshals to SQL NULL
	// and violates the constraint at runtime.
	if cfg.SelectedOperatorIDs == nil {
		cfg.SelectedOperatorIDs = []string{}
	}

	// Begin transaction for atomic config write + outbox enqueue
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.OperatorAssignmentConfig{}, fmt.Errorf("vaccination execution: begin config update tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Capture the pre-write state (locked) so we can emit precisely which cascade event(s) fired:
	// vaccination.capacity.changed when N (active operators/day) changed, vaccination.roster.changed
	// when the default operator changed. A first write emits both. FOR UPDATE serializes concurrent
	// writers on this park row so the before/after comparison is race-free.
	var (
		prevN       int
		prevDefault string
		foundBefore bool
	)
	if err := tx.QueryRow(ctx, `
SELECT active_operators_per_day, default_operator_id::text
FROM vaccination_operator_assignment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
FOR UPDATE`, tenantID, cfg.ParkID).Scan(&prevN, &prevDefault); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.OperatorAssignmentConfig{}, fmt.Errorf("vaccination execution: read prior operator assignment config: %w", err)
		}
	} else {
		foundBefore = true
	}

	var newVersion int64
	if cfg.RowVersion == 0 {
		// Insert: new config for this park
		err := tx.QueryRow(ctx, `
INSERT INTO vaccination_operator_assignment_config
  (tenant_id, park_id, active_operators_per_day, default_operator_id, selected_operator_ids, row_version, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::uuid[], 1, now())
ON CONFLICT (tenant_id, park_id) DO NOTHING
RETURNING row_version;`, tenantID, cfg.ParkID, cfg.ActiveOperatorsPerDay, cfg.DefaultOperatorID, cfg.SelectedOperatorIDs).Scan(&newVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.OperatorAssignmentConfig{}, ports.ErrOperatorAssignmentConfigConflict
		}
		if err != nil {
			return domain.OperatorAssignmentConfig{}, fmt.Errorf("vaccination execution: insert operator assignment config: %w", err)
		}
		cfg.RowVersion = newVersion
	} else {
		// Update: config already exists, optimistic lock on row_version
		err := tx.QueryRow(ctx, `
UPDATE vaccination_operator_assignment_config
SET active_operators_per_day = $3,
    default_operator_id = $4::uuid,
    selected_operator_ids = $5::uuid[],
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND row_version = $6
RETURNING row_version;`, tenantID, cfg.ParkID, cfg.ActiveOperatorsPerDay, cfg.DefaultOperatorID, cfg.SelectedOperatorIDs, cfg.RowVersion).Scan(&newVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.OperatorAssignmentConfig{}, ports.ErrOperatorAssignmentConfigConflict
		}
		if err != nil {
			return domain.OperatorAssignmentConfig{}, fmt.Errorf("vaccination execution: update operator assignment config: %w", err)
		}
		cfg.RowVersion = newVersion
	}

	// Enqueue cascade events to outbox_messages (same transaction as config write) for at-least-once
	// delivery via the outbox relay. Emit precisely which changed:
	//   - vaccination.capacity.changed when N (active operators/day) changed
	//   - vaccination.roster.changed   when the default operator changed
	// A first write (no prior row) emits both. Both events drive the same OperatorConfigReplanHandler
	// recompute; the two-phase watermark makes redundant delivery idempotent.
	capacityChanged := !foundBefore || prevN != cfg.ActiveOperatorsPerDay
	rosterChanged := !foundBefore || prevDefault != cfg.DefaultOperatorID
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")

	enqueueCascade := func(eventType, keyKind string) error {
		idempotencyKey := fmt.Sprintf("vaccination.operator-assignment-config.%s:%s:%d", keyKind, cfg.ParkID, newVersion)
		eventID := platformoutbox.DeterministicUUID(idempotencyKey)
		// Conformant domain-event envelope (contracts/jsonschema/domain-event-envelope.schema.json,
		// additionalProperties:false). subject is the park (a location); aggregate is the park config.
		envelope, err := json.Marshal(map[string]any{
			"event_id":       eventID,
			"event_type":     eventType,
			"schema_version": "1.0.0",
			"schema_ref":     "domain-event-envelope.v1",
			"aggregate_type": "park",
			"aggregate_id":   cfg.ParkID,
			"occurred_at":    now,
			"recorded_at":    now,
			"producer": map[string]any{
				"service": "goatos-api",
				"module":  "vaccination-execution",
				"version": nil,
			},
			"idempotency_key": idempotencyKey,
			"actor": map[string]any{
				"actor_type": "system_rule",
				"actor_id":   nil,
				"actor_ref":  nil,
			},
			"subject_type": "location",
			"subject_id":   cfg.ParkID,
			"visibility_scope": map[string]any{
				"tenant_id": tenantID,
				"park_id":   cfg.ParkID,
			},
			"evidence_refs": []map[string]string{{
				"evidence_type": "location",
				"evidence_id":   cfg.ParkID,
			}},
			"payload":  map[string]any{"park_id": cfg.ParkID},
			"trace_id": idempotencyKey,
		})
		if err != nil {
			return fmt.Errorf("vaccination execution: marshal %s envelope: %w", eventType, err)
		}
		headers, err := json.Marshal(map[string]any{
			"producer":        "vaccination-execution.UpsertOperatorAssignmentConfig",
			"schema_version":  "1.0.0",
			"park_id":         cfg.ParkID,
			"idempotency_key": idempotencyKey,
		})
		if err != nil {
			return fmt.Errorf("vaccination execution: marshal %s headers: %w", eventType, err)
		}
		// The ON CONFLICT arbiter is a per-event-type partial unique index (migration 000038), so the
		// WHERE predicate must name the same event_type literal. eventType here is a controlled
		// constant, never user input.
		sql := `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES ($1::uuid, $2::uuid, $3, '1.0.0', 'park', $4::uuid,
  'vaccination.events', $5::jsonb, $6::jsonb, $7, $7, 'pending', now())
ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = '` + eventType + `' DO NOTHING`
		if _, err := tx.Exec(ctx, sql, tenantID, eventID, eventType, cfg.ParkID, envelope, headers, idempotencyKey); err != nil {
			return fmt.Errorf("vaccination execution: enqueue %s to outbox: %w", eventType, err)
		}
		return nil
	}

	if capacityChanged {
		if err := enqueueCascade("vaccination.capacity.changed", "capacity"); err != nil {
			return domain.OperatorAssignmentConfig{}, err
		}
	}
	if rosterChanged {
		if err := enqueueCascade("vaccination.roster.changed", "roster"); err != nil {
			return domain.OperatorAssignmentConfig{}, err
		}
	}

	// Commit: if we reach here, both config write and outbox enqueue are atomic
	if err := tx.Commit(ctx); err != nil {
		return domain.OperatorAssignmentConfig{}, fmt.Errorf("vaccination execution: commit config update tx: %w", err)
	}

	return cfg, nil
}

// ReassignPlannedDrives applies an operator config change to already-planned open drive work
// immediately. For one-operator mode every open planned row moves to the default operator. For
// parallel mode the first N available roster operators for that drive date are selected, skipping
// week-off, and existing assignment rows are distributed across them.
func (r *Repository) ReassignPlannedDrives(ctx context.Context, tenantID, parkID, defaultOperatorID string, selectedOperatorIDs []string, activeOperatorsPerDay int, effectiveFrom time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	effectiveDate := biztime.BusinessDayStart(effectiveFrom).Format("2006-01-02")
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("vaccination execution: begin planned drive reassignment tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
WITH target_assignments AS (
  SELECT
    vda.assignment_id,
    vda.batch_id,
    vda.planned_date,
    row_number() OVER (
      PARTITION BY vda.planned_date
      ORDER BY vda.physical_shed, vda.partition_label, vda.assignment_id
    ) AS assignment_rank
  FROM obligation_batches ob
  JOIN vaccination_drive_assignments vda
    ON vda.tenant_id = ob.tenant_id
   AND vda.batch_id = ob.batch_id
  WHERE ob.tenant_id = $1::uuid
    AND ob.status = 'planned'
    AND vda.park_id = $2::uuid
    AND vda.planned_date >= $6::date
),
selected_operators AS (
  SELECT
    ta.assignment_id,
    ta.batch_id,
    selected.operator_id
  FROM target_assignments ta
  JOIN LATERAL (
    SELECT ranked.operator_id
    FROM (
      SELECT
        osc.operator_id,
        count(*) OVER () AS available_count,
        row_number() OVER (
          ORDER BY
            COALESCE(array_position($4::uuid[], osc.operator_id), 999),
            CASE WHEN $5::int = 1 AND osc.operator_id = $3::uuid THEN 0 ELSE 1 END,
            CASE osc.shift_label WHEN 'am' THEN 0 WHEN 'rover' THEN 1 WHEN 'pm' THEN 2 ELSE 3 END,
            osc.operator_id
        ) AS roster_rank
      FROM vaccination_operator_shift_config osc
      JOIN workforce_members wm
        ON wm.tenant_id = osc.tenant_id
       AND wm.workforce_member_id = osc.operator_id
       AND wm.status = 'active'
      WHERE osc.tenant_id = $1::uuid
        AND osc.park_id = $2::uuid
        AND (
          osc.week_off_weekday IS NULL
          OR osc.week_off_weekday <> lower(to_char(ta.planned_date::timestamp, 'FMDay'))
        )
    ) ranked
    WHERE ranked.roster_rank = ((ta.assignment_rank - 1) % GREATEST(ranked.available_count, 1)) + 1
    ORDER BY ranked.roster_rank
    LIMIT 1
  ) selected ON true
),
updated_batches AS (
  UPDATE obligation_batches ob
  SET conducted_by = CASE WHEN $5::int = 1 THEN $3::uuid ELSE ob.conducted_by END,
      updated_at = now(),
      row_version = row_version + 1
  FROM (
    SELECT DISTINCT batch_id FROM selected_operators
  ) tb
  WHERE ob.tenant_id = $1::uuid
    AND ob.batch_id = tb.batch_id
    AND $5::int = 1
    AND ob.conducted_by IS DISTINCT FROM $3::uuid
  RETURNING ob.batch_id
),
updated_assignments AS (
  UPDATE vaccination_drive_assignments vda
  SET operator_id = so.operator_id,
      updated_at = now()
  FROM selected_operators so
  WHERE vda.tenant_id = $1::uuid
    AND vda.assignment_id = so.assignment_id
    AND vda.operator_id IS DISTINCT FROM so.operator_id
  RETURNING vda.assignment_id
)
SELECT 'batch' AS kind, count(*)::bigint FROM updated_batches
UNION ALL
SELECT 'assignment' AS kind, count(*)::bigint FROM updated_assignments;
`, tenantID, parkID, defaultOperatorID, selectedOperatorIDs, activeOperatorsPerDay, effectiveDate)
	if err != nil {
		return 0, fmt.Errorf("vaccination execution: reassign planned drives: %w", err)
	}
	defer rows.Close()

	var changed int64
	for rows.Next() {
		var kind string
		var n int64
		if err := rows.Scan(&kind, &n); err != nil {
			return 0, fmt.Errorf("vaccination execution: scan planned drive reassignment: %w", err)
		}
		if kind == "assignment" {
			changed = n
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("vaccination execution: iterate planned drive reassignment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("vaccination execution: commit planned drive reassignment tx: %w", err)
	}
	return changed, nil
}

// authorizedParkOptionsSQL reads the tenant's active parks from canonical `locations`, optionally
// narrowed to the caller's park-scoped grants. BOUNDED configuration catalog (a handful of parks per
// tenant, sized by parks the business physically operates, not by herd size) so it is returned whole
// and deliberately not paginated. The park-id predicate keeps the indexed column BARE and casts the
// bound array instead (`location_id = ANY($2::uuid[])`), so a column-side cast can never disable the
// index; an empty array means "no grant narrowing" (tenant-wide actor).
const authorizedParkOptionsSQL = `
SELECT location_id::text,
       COALESCE(location_code, '') AS code,
       name
FROM locations
WHERE tenant_id = $1::uuid
  AND location_type = 'park'
  AND status = 'active'
  AND (cardinality($2::uuid[]) = 0 OR location_id = ANY($2::uuid[]))
ORDER BY name ASC, location_id ASC;`

// AuthorizedParkOptions returns the park vocabulary the caller may act in (see ports.Repository).
func (r *Repository) AuthorizedParkOptions(ctx context.Context, tenantID string, parkIDs []string) ([]domain.ParkOption, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	ids := parkIDs
	if ids == nil {
		ids = []string{}
	}
	rows, err := r.pool.Query(ctx, authorizedParkOptionsSQL, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: authorized park options: %w", err)
	}
	defer rows.Close()
	out := make([]domain.ParkOption, 0, 8)
	for rows.Next() {
		var p domain.ParkOption
		if err := rows.Scan(&p.ParkID, &p.Code, &p.Name); err != nil {
			return nil, fmt.Errorf("vaccination execution: authorized park options scan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: authorized park options rows: %w", err)
	}
	return out, nil
}

func (r *Repository) VaccinationExecutionCarrySummary(ctx context.Context, q domain.ExecutionQuery) ([]domain.VaccineCarryLine, error) {
	if q.OperatorScopeActorID == "" {
		return nil, fmt.Errorf("carry summary requires operator scope")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Real SQL from coordinator: aggregates obligation_instances by (eff_date, protocol_name, dose_code)
	// for the scoped operator, with override-aware date resolution.
	// OperatorScopeActorID is already the workforce_member_id; no external resolution.
	// Note: vaccine_label is constructed in Go using domain.DoseDisplayLabel after scanning.
	sql := `
WITH scoped AS (
  SELECT oi.obligation_id, oi.status, m.goat_id,
         pd.name AS protocol_name,
         pr.dose_code,
         COALESCE(ovr.override_date, vda.planned_date) AS eff_date,
         vda.operator_id
  FROM obligation_instances oi
  JOIN vaccination_drive_assignment_members m ON m.obligation_id = oi.obligation_id AND m.tenant_id = oi.tenant_id
  JOIN vaccination_drive_assignments vda ON vda.assignment_id = m.assignment_id
  JOIN protocol_rules pr ON pr.rule_id = oi.rule_id
  JOIN protocol_versions pv ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id AND pd.category = 'vaccination'
  LEFT JOIN vaccination_drive_date_overrides ovr
    ON ovr.tenant_id = vda.tenant_id AND ovr.park_id = vda.park_id AND ovr.canceled_at IS NULL
   AND lower(btrim(ovr.vaccine_code)) = lower(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code','')))
   AND (ovr.original_drive_date = vda.planned_date OR ovr.override_date = vda.planned_date)
  WHERE oi.tenant_id = $1
)
-- projection-review: membership=obligation_instances joined 1:1 to their vaccination_drive_assignment_members row (obligation_id unique) and that row's assignment, for the operator's day range; group_key=(effective_drive_date, protocol_name, dose_code) where effective_drive_date=COALESCE(active override.override_date, vda.planned_date) so a moved-away vaccine counts on its NEW day only; join_cardinality=member->assignment many-to-one and obligation->protocol_rule 1:1, and count(DISTINCT goat_id) collapses any multi-row fan-out so remaining/total are per-animal not per-obligation-row; pagination=NONE — full-day carry aggregate computed independently of the paginated row window, total never changes with the loaded page; scope=tenant ($1) + operator workforce_member ($2) + effective-date BETWEEN $3 and $4, park/shed implicit via the operator's own assignments.
SELECT eff_date::date as eff_date, protocol_name, dose_code,
       count(DISTINCT goat_id) FILTER (WHERE status IN ('scheduled','due','in_progress')) AS remaining,
       count(DISTINCT goat_id) AS total
FROM scoped
WHERE operator_id = (SELECT wm.workforce_member_id FROM workforce_members wm
                     WHERE wm.tenant_id = $1
                       AND (
                         wm.workforce_member_id = NULLIF($2::text,'')::uuid
                         OR wm.user_id = NULLIF($2::text,'')::uuid
                       )
                       AND wm.status='active'
                     ORDER BY CASE WHEN wm.workforce_member_id = NULLIF($2::text,'')::uuid THEN 0 ELSE 1 END,
                              wm.updated_at DESC,
                              wm.workforce_member_id DESC
                     LIMIT 1)
  AND eff_date BETWEEN $3::date AND $4::date
GROUP BY eff_date::date, protocol_name, dose_code
ORDER BY eff_date::date, protocol_name, dose_code
`
	rows, err := r.pool.Query(ctx, sql, q.TenantID, q.OperatorScopeActorID, q.AsOf, q.DueBefore)
	if err != nil {
		return nil, fmt.Errorf("vaccination execution: carry summary query: %w", err)
	}
	defer rows.Close()

	out := make([]domain.VaccineCarryLine, 0, 16)
	for rows.Next() {
		var line domain.VaccineCarryLine
		var effDate time.Time
		var protocolName, doseCode string
		if err := rows.Scan(&effDate, &protocolName, &doseCode, &line.RemainingDoses, &line.TotalDoses); err != nil {
			return nil, fmt.Errorf("vaccination execution: carry summary scan: %w", err)
		}
		line.Date = effDate.Format("2006-01-02") // ISO date
		line.VaccineLabel = vaccinatdomain.DoseDisplayLabel(protocolName, doseCode)
		out = append(out, line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination execution: carry summary rows: %w", err)
	}
	return out, nil
}

// VaccinationCommandBoard returns the CEO closure view: KPIs, cohort matrix, shed dose matrix,
// weekly given, and verification queue. All reads are indexed canonical SQL (5k-50k envelope).
func (r *Repository) VaccinationCommandBoard(ctx context.Context, q domain.CommandBoardQuery) (domain.CommandBoardResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}

	resp := domain.CommandBoardResponse{
		Source:            domain.SourceAPI,
		CohortMatrix:      []domain.CommandBoardCohortCell{},
		ShedDoseMatrix:    []domain.ShedDoseMatrixCell{},
		WeeklyGiven:       []domain.WeeklyGivenRow{},
		VerificationQueue: []domain.VerificationQueueRow{},
	}

	// 1. KPIs query
	// projection-review:
	// (a) producer: obligation_id, status, due_at, batch_id | consumer: obligation_id GROUP BY none
	// (b) completions pre-aggregated per obligation (1:1 after CTE), shed lookup 1:1
	// (c) all KPI numerators count distinct animals, not obligation/dose fan-out.
	//     doses_verified numerator: bool_or(status='accepted');
	//     awaiting_verification numerator: bool_or(recorded unverified) AND NOT bool_or(accepted);
	//     overdue_not_given numerator: OPEN AND (due_at's IST business date)<(asOf's IST business
	//     date) AND no completions; scheduled_ahead numerator: OPEN AND no completions
	//     AND (due_at's IST business date)>=(asOf's IST business date), denominator: all obligations.
	//     Vaccination time grain is the IST business DAY, never an instant — a dose due today at
	//     00:00 IST must never read overdue merely because as_of is later the same day.
	// (d) OPEN is the repo's canonical open-obligation set
	//     ('scheduled','due','in_progress','deferred','missed') — the SAME set that defines
	//     obligation_instances_open_logical_due_idx. It is NOT 'scheduled' alone: the sweeper flips
	//     scheduled -> 'due' on the due business day, so a 'scheduled'-only predicate silently
	//     dropped every currently-actionable obligation out of BOTH overdue_not_given and
	//     scheduled_ahead (live CEO board read targets=40 but bucketed only 20 — the other park's
	//     20 animals, with zero work done, were invisible to leadership).
	// (e) BUCKET CONTRACT (docs/architecture/operational-read-model-contract.md,
	//     "GET /vaccination/command — Grain and Buckets (disjoint unless noted)" + Bucket
	//     Invariant): the FIVE numerator buckets are a DISJOINT and EXHAUSTIVE partition of
	//     targets, evaluated as a priority chain —
	//       missed     = status 'missed', regardless of what else the animal holds
	//       verified   = has_accepted
	//       awaiting   = has_recorded_unverified AND NOT has_accepted
	//       overdue    = no completion AND OPEN AND due business date <  as_of business date
	//       scheduled  = no completion AND OPEN AND due business date >= as_of business date
	//       closed_without_dose = the residual: none of the above
	//     so missed+verified+awaiting+overdue+scheduled+closed_without_dose = targets.
	//
	//     The chain used to be evaluated on the OBLIGATION row while targets counted DISTINCT
	//     target_id — two different grains. An animal holding two obligations in different
	//     states (one dose accepted, the next dose still scheduled) therefore satisfied two
	//     bucket predicates on two different rows and was counted by BOTH COUNT(DISTINCT
	//     target_id) expressions, while targets counted it once: the four tiles summed to more
	//     than the total they sit under, and a CEO reading the board could not reconcile them.
	//     That is the ordinary multi-dose case, not a corner case — every animal on a kid
	//     schedule holds several doses at once.
	//
	//     The chain is now evaluated ONCE PER ANIMAL (per_animal below folds every one of that
	//     animal's obligations into four booleans, then the priority chain picks exactly one),
	//     so the buckets are disjoint at the SAME grain targets uses and the sum is restored.
	//
	//     PRECEDENCE: missed > verified > awaiting > overdue > scheduled, i.e. a missed dose wins
	//     outright and otherwise the most-progressed dose wins. Below missed this is the same order
	//     the per-obligation chain already used, so no tile changes meaning for a single-dose
	//     animal; extending it unchanged to the animal grain keeps the contract one rule instead of
	//     two. The consequence is stated rather than hidden: an animal with one accepted dose and
	//     one overdue dose still reports as verified, so the tiles answer "how far has this animal
	//     got" and NOT "how much work is outstanding" — the outstanding-work question is answered at
	//     dose grain by the shed dose matrix and the verification queue below, which stay
	//     per-obligation.
	//
	//     MISSED LEADS THE CHAIN, and it is the one exception to "most-progressed wins", because
	//     letting verified lead made the board report the opposite of the truth. Folding to one row
	//     per animal via bool_or means a single accepted dose anywhere in an animal's history sets
	//     any_verified for good. On the live stg board 137 animals held a MISSED ET+TT dose; every
	//     one of them also held an accepted dose of something else, so all 137 landed in
	//     doses_verified and OVERDUE read 0 — a herd with 137 missed doses presented as fully green,
	//     and the missed obligations were additionally invisible to any_overdue/any_scheduled
	//     because each carried a recorded-but-unverified completion (no_completion = false). A
	//     "how far has this animal got" reading cannot be allowed to answer "clear" for an animal
	//     whose dose window closed unvaccinated: missed is not progress, it is the failure the board
	//     exists to report, so it outranks every state an animal can simultaneously be in.
	//
	//     any_missed is deliberately NOT gated on no_completion. A missed obligation routinely holds
	//     a recorded-unverified completion (the operator submitted proof after the window shut, or
	//     the sweeper closed it while proof sat in the verification queue). Gating on no_completion
	//     is exactly what hid all 137 rows.
	//
	//     CLOSED WITHOUT DOSE is why the sum used to be <= targets rather than = targets. An
	//     animal whose every obligation reached a closed status with no completion row against it
	//     ('canceled', 'waived', 'superseded', or a 'completed' whose completion was never
	//     written) satisfies none of the four predicates: it is not open, so it cannot be overdue
	//     or scheduled, and nothing was recorded, so it cannot be awaiting or verified. It still
	//     counts in targets, because targets is COUNT(DISTINCT target_id) over the drive's
	//     animals. A 100-animal drive with 3 withdrawn animals therefore read "Total 100" over
	//     tiles summing to 97, and a leader could not tell whether that 3-animal hole was a
	//     display bug, missing data, or three animals still owing work — the cheapest reading of
	//     an unexplained gap is "the board is broken", which costs more trust than the three
	//     animals are worth.
	//
	//     It is NAMED as a fifth bucket rather than subtracted out of targets. Subtracting would
	//     also reconcile the arithmetic, but it would make targets drift below the roster the
	//     operator was actually handed and below the cohort matrix's animal_count for the same
	//     filter, and it would erase the withdrawal itself — which is the one fact in that hole a
	//     leader can act on ("who took 3 animals off this drive, and why"). Naming it keeps the
	//     total anchored to the roster and turns the silence into a number.
	//
	//     It is defined as the RESIDUAL of the other four rather than by enumerating closed
	//     statuses, so the partition stays exhaustive by construction: adding a status to the
	//     OPEN set above, or introducing a new terminal status, cannot reopen the gap.
	//
	// projection-review: membership=obligation_instances in the drive window, folded to one row per animal by per_animal; group_key=target_id (the ANIMAL), which is exactly the grain COUNT(DISTINCT target_id) uses for targets, so buckets and total share one key set; join_cardinality=comp is pre-aggregated per obligation before the fold, so a dose with several completions cannot multiply its animal, and every remaining join is 0..1 on a PK; pagination=NONE, these are whole-filter tile aggregates computed in the database and are page-size independent by construction; scope=tenant_id plus the capability-resolved park filter, parented through locations.parent_location_id
	kpiSQL := `
-- HERD MEMBERSHIP. Every read on this board joins goats and keeps only animals that are actually in
-- the herd, spelled as the same positive IN-list the rest of the backend uses
-- ('alive','sick','under_treatment','quarantine','icu').
--
-- It is a POSITIVE list on purpose. The filter here used to compare lifecycle_status against
-- 'terminated', which excluded NOTHING: that is not one of the values goats_lifecycle_status_check
-- permits, so the comparison is true for every row ever written. A dead, sold, culled, transferred
-- or lost animal sailed straight through a filter that looked like it was doing the job. A
-- negative list also silently readmits every status added later, which is how that hole would
-- reopen.
-- Merged animals are excluded separately via merged_into_goat_id, because a merge RETIRES the source
-- goat into another record and counting it is counting the same animal twice.
--
-- This matters because the board's own halves disagree otherwise: the cohort matrix reads the live herd while
-- these aggregates read obligation_instances directly, so a sold or merged goat still holding an
-- open obligation inflates targets and the shed cells while the cohort head count drops it -- two
-- numbers on one screen describing different herds, with no visible reconciliation break to hint at
-- it.
WITH comp AS (
  SELECT
    obligation_id,
    bool_or(status = 'accepted') AS has_accepted,
    bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
),
scoped AS (
  SELECT
    oi.target_id,
    COALESCE(comp.has_accepted, false) AS has_accepted,
    COALESCE(comp.has_recorded_unverified, false) AS has_recorded_unverified,
    comp.obligation_id IS NULL AS no_completion,
    oi.status = 'missed' AS is_missed,
    oi.status IN ('scheduled','due','in_progress','deferred','missed') AS is_open,
    (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date AS due_before_as_of
  FROM obligation_instances oi
  JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
  LEFT JOIN comp ON oi.obligation_id = comp.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
    AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
      SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
    ))
),
-- projection-review: membership=obligation_instances in the drive window, folded to ONE ROW PER ANIMAL here; group_key=target_id, the same grain COUNT(DISTINCT target_id) uses for targets, so the tiles and the total they sit under share one key set; join_cardinality=comp is pre-aggregated per obligation before this fold, so a dose with several completions cannot multiply its animal, and every other join is 0..1 on a PK; pagination=NONE, whole-filter tile aggregates computed in the database, page-size independent by construction; scope=tenant_id plus the capability-resolved park filter, parented through locations.parent_location_id
per_animal AS (
  SELECT
    target_id,
    -- MISSED means no dose reached the animal. An obligation swept to 'missed' that carries a
    -- recorded completion is a VERIFICATION backlog, not a missed dose: on stg all 137 such
    -- obligations were dosed on the day they were due. Counting them here reported 137 vaccinated
    -- animals as unvaccinated and simultaneously showed awaiting_verification = 0 while 137 proofs
    -- sat in the queue -- both tiles wrong, in opposite directions, from the same predicate.
    bool_or(is_missed AND no_completion) AS any_missed,
    bool_or(has_accepted) AS any_verified,
    bool_or(has_recorded_unverified AND NOT has_accepted) AS any_awaiting,
    bool_or(is_open AND no_completion AND due_before_as_of) AS any_overdue,
    bool_or(is_open AND no_completion AND NOT due_before_as_of) AS any_scheduled
  FROM scoped
  GROUP BY target_id
)
SELECT
  COUNT(*) AS targets,
  COUNT(*) FILTER (WHERE any_missed) AS missed_not_given,
  COUNT(*) FILTER (WHERE any_verified AND NOT any_missed) AS doses_verified,
  COUNT(*) FILTER (WHERE any_awaiting AND NOT any_verified AND NOT any_missed) AS awaiting_verification,
  COUNT(*) FILTER (WHERE any_overdue AND NOT any_awaiting AND NOT any_verified AND NOT any_missed) AS overdue_not_given,
  COUNT(*) FILTER (WHERE any_scheduled AND NOT any_overdue AND NOT any_awaiting AND NOT any_verified AND NOT any_missed) AS scheduled_ahead,
  COUNT(*) FILTER (WHERE NOT any_missed AND NOT any_verified AND NOT any_awaiting AND NOT any_overdue AND NOT any_scheduled) AS closed_without_dose
FROM per_animal
`
	var parkID *string
	if q.ParkID != nil && strings.TrimSpace(*q.ParkID) != "" {
		parkID = q.ParkID
	}
	row := r.pool.QueryRow(ctx, kpiSQL, q.TenantID, asOf, q.DriveBatchID, parkID)
	if err := row.Scan(&resp.KPIs.Targets, &resp.KPIs.MissedNotGiven, &resp.KPIs.DosesVerified, &resp.KPIs.AwaitingVerification, &resp.KPIs.OverdueNotGiven, &resp.KPIs.ScheduledAhead, &resp.KPIs.ClosedWithoutDose); err != nil {
		return resp, fmt.Errorf("vaccination command board: kpi query: %w", err)
	}

	// 1b. The animals behind the ClosedWithoutDose tile.
	//
	// The tile is a dead end without them: "3 closed with no dose" starts the CEO's question and the
	// only way to finish it was a database query. The per-animal predicate below is the SAME
	// four-boolean fold the tile counts with (per_animal, priority chain residual), so the list and
	// the number can never describe different animals.
	//
	// projection-review: membership=obligation_instances folded to one row per animal by per_animal, filtered to the residual bucket, then joined 1:1 to that animal's identity and location; group_key=target_id (the ANIMAL), identical to the tile's key set; join_cardinality=comp pre-aggregated per obligation before the fold; goats/locations/goat_shed_partitions 0..1 on a PK; the primary-tag and closed-dose lookups are LATERAL LIMIT 1 scalars so neither can multiply an animal; pagination=whole-scope COUNT stays on the tile, this LIST is capped in Go at CommandBoardClosedWithoutDoseListCap; scope=tenant + optional batch + optional park EXISTS, same predicates as the tile.
	// (a) producer unique columns: target_id after the per_animal fold | consumer GROUP BY: none —
	//     one row per animal is already the grain, so there is no aggregate to fan out.
	// (b) join multiplicity: every join is 0..1 (PK lookups) or a LIMIT 1 LATERAL.
	// (c) key set: the residual filter is byte-identical to the tile's
	//     `NOT any_verified AND NOT any_awaiting AND NOT any_overdue AND NOT any_scheduled`, so
	//     count and list range over the same animals.
	closedWithoutDoseSQL := `
WITH comp AS (
  SELECT
    obligation_id,
    bool_or(status = 'accepted') AS has_accepted,
    bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
),
scoped AS (
  SELECT
    oi.target_id,
    oi.obligation_id,
    oi.status,
    oi.rule_id,
    oi.due_at,
    COALESCE(comp.has_accepted, false) AS has_accepted,
    COALESCE(comp.has_recorded_unverified, false) AS has_recorded_unverified,
    comp.obligation_id IS NULL AS no_completion,
    oi.status IN ('scheduled','due','in_progress','deferred','missed') AS is_open,
    oi.status = 'missed' AS is_missed,
    (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date AS due_before_as_of
  FROM obligation_instances oi
  JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
  LEFT JOIN comp ON oi.obligation_id = comp.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
    AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
      SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
    ))
),
-- any_missed is carried HERE TOO, not only in kpiSQL, and the two must be changed together.
-- This CTE is the drill-down list behind the ClosedWithoutDose TILE, and the pair is only
-- trustworthy as a pair: the tile's count is kpiSQL's residual and this list is the residual
-- below, so a predicate present in one and absent from the other lets the drawer name animals the
-- tile does not count -- the exact "the list and the number can never describe different animals"
-- guarantee this query's own header asserts.
--
-- Concretely, without any_missed here: an animal whose only obligation is 'missed' and whose
-- completion has been REVERSED holds no accepted proof, no recorded-unverified proof, and is not
-- open-with-no-completion, so it fails all four of the older booleans. kpiSQL now counts it under
-- missed_not_given and excludes it from closed_without_dose; this query would still hand it to the
-- ClosedWithoutDose drawer. Reversing a wrongly-accepted dose after the obligation has already
-- been swept to missed is an ordinary correction, not a corner case.
per_animal AS (
  SELECT
    target_id,
    bool_or(is_missed) AS any_missed,
    bool_or(has_accepted) AS any_verified,
    bool_or(has_recorded_unverified AND NOT has_accepted) AS any_awaiting,
    bool_or(is_open AND no_completion AND due_before_as_of) AS any_overdue,
    bool_or(is_open AND no_completion AND NOT due_before_as_of) AS any_scheduled
  FROM scoped
  GROUP BY target_id
)
SELECT
  g.goat_id::text,
  g.display_id,
  COALESCE(aid1.identifier_value, '') AS animal_identifier_1,
  COALESCE(aid2.identifier_value, '') AS animal_identifier_2,
  COALESCE(park.name, '') AS park_name,
  COALESCE(shed.name, '') AS shed_name,
  COALESCE(gsp.partition_label, '') AS partition_label,
  COALESCE(closed.status, '') AS reason_status,
  COALESCE(closed.dose_code, '') AS dose_code
FROM per_animal pa
JOIN goats g ON g.goat_id = pa.target_id AND g.tenant_id = $1::uuid
LEFT JOIN locations shed ON g.shed_id = shed.location_id AND g.tenant_id = shed.tenant_id
LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
-- The animal's REAL identity is its physical tag(s), and an animal may carry two. Same
-- canonical source and types the shed roster and calendar drawer read, so one animal reads
-- identically on every surface.
--
-- SCALAR picks, not plain joins. goat_identifiers is unique per (goat_id, identifier_type) ONLY
-- for is_primary_for_goat AND status='active'; a goat may hold several active NON-primary rows
-- of one type, and this fixture already does. A plain join would emit that goat once per extra
-- row, so the drawer would repeat animals, push distinct animals past the LIMIT, and stop matching
-- kpis.closedWithoutDose. The primary row wins; identifier_id only breaks ties so the pick is
-- stable across reads.
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
    AND gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
  ORDER BY gi.is_primary_for_goat DESC, gi.identifier_id
  LIMIT 1
) aid1 ON true
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
    AND gi.identifier_type = 'animal_identifier_2' AND gi.status = 'active'
  ORDER BY gi.is_primary_for_goat DESC, gi.identifier_id
  LIMIT 1
) aid2 ON true
LEFT JOIN LATERAL (
  SELECT s.status, pr.dose_code
  FROM scoped s
  JOIN protocol_rules pr ON pr.rule_id = s.rule_id AND pr.tenant_id = $1::uuid
  WHERE s.target_id = pa.target_id
  -- An animal can hold more than one closed obligation (a waived Dose 1 and a cancelled Dose 2).
  -- The drawer has one reason line, so show the MOST RECENT closure -- that is the fact a reader
  -- acts on -- rather than whichever UUID happened to sort first. obligation_id only breaks ties
  -- so the pick stays stable across reads.
  ORDER BY s.due_at DESC NULLS LAST, s.obligation_id
  LIMIT 1
) closed ON true
WHERE NOT pa.any_verified AND NOT pa.any_awaiting AND NOT pa.any_overdue AND NOT pa.any_scheduled
ORDER BY g.display_id
LIMIT $5
`
	closedRows, err := r.pool.Query(ctx, closedWithoutDoseSQL, q.TenantID, asOf, q.DriveBatchID, parkID,
		domain.CommandBoardClosedWithoutDoseListCap)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: closed-without-dose query: %w", err)
	}
	defer closedRows.Close()
	// Initialised, not left nil: the contract declares this list required, and a nil slice marshals
	// to null. An empty residual bucket must read as "no animals closed without a dose", never as a
	// field the server declined to send.
	resp.ClosedWithoutDoseAnimals = []domain.CommandBoardClosedWithoutDoseAnimal{}
	for closedRows.Next() {
		var animal domain.CommandBoardClosedWithoutDoseAnimal
		var reasonStatus, doseCode string
		if err := closedRows.Scan(&animal.GoatID, &animal.DisplayID, &animal.Tag1, &animal.Tag2,
			&animal.ParkName, &animal.ShedName, &animal.PartitionLabel, &reasonStatus, &doseCode); err != nil {
			return resp, fmt.Errorf("vaccination command board: closed-without-dose scan: %w", err)
		}
		// Ground location is park + physical shed + partition. shed.name alone would print "Godel 1"
		// for an animal standing in "Godel 1 - Part 3".
		animal.LocationDisplay = oploc.OperationalLocation{
			ParkName:       animal.ParkName,
			ShedName:       animal.ShedName,
			PartitionLabel: animal.PartitionLabel,
		}.Display()
		// NormalizePartition collapses every non-partitioned encoding to the "whole" MATCHING
		// sentinel, which is a grouping key and never copy. A non-partitioned shed carries no
		// partition label on the wire at all.
		if normalized := oploc.NormalizePartition(animal.PartitionLabel); oploc.IsPartitioned(normalized) {
			animal.PartitionLabel = strings.TrimSpace(animal.PartitionLabel)
		} else {
			animal.PartitionLabel = ""
		}
		animal.Reason = closureReasonLabel(reasonStatus)
		animal.VaccineLabel = vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode)
		resp.ClosedWithoutDoseAnimals = append(resp.ClosedWithoutDoseAnimals, animal)
	}
	if err := closedRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: closed-without-dose rows: %w", err)
	}

	// 2. Cohort matrix query (management_stage × sex × vaccine × pending/submitted/verified)
	//
	// THREE disjoint buckets, partitioned by WHO OWES THE NEXT MOVE. pending_count used to fuse the
	// first two, because obligation status advances only on VERIFICATION and never on submission:
	// a park whose every animal had been vaccinated and submitted rendered byte-identically to a
	// park nobody had touched, so the page showed "40 awaiting verification" in the KPI row and
	// "40 pending" in the matrix directly below it with no column reconciling them, and a CEO could
	// not tell that all 40 animals had in fact been vaccinated. The query was conformant to its
	// written contract; the DEFINITION was the defect. Contract updated in the same change
	// (docs/architecture/operational-read-model-contract.md, "Cohort Submission Matrix").
	//
	// projection-review:
	// (a) PRODUCER unique column list: obligation_instances is unique on (obligation_id); the comp
	//     CTE is pre-aggregated to exactly one row per obligation_id. Every bucket counts
	//     DISTINCT oi.obligation_id, so one obligation contributes at most 1 to at most one bucket.
	//     CONSUMER match/group column list: GROUP BY (park.location_id, park.name,
	//     g.management_stage, g.sex, pr.dose_code) — the identical key set for all three buckets and
	//     for animal_count; no bucket carries an extra or missing WHERE dimension.
	// (b) Join multiplicity: comp is 1:1 on obligation_id (GROUP BY obligation_id in the CTE);
	//     goats 1:1 on (target_id, tenant_id) (PK); protocol_rules 1:1 on (rule_id, tenant_id) (PK);
	//     locations shed 1:1 on (scope_id, tenant_id) and park 1:1 on shed.parent_location_id. No
	//     join fans the obligation grain out, so animal_count (DISTINCT goat_id) cannot multiply by
	//     the number of vaccines or doses.
	// (c) DISJOINT + TOTAL partition over the cell's obligations, evaluated per row from columns of
	//     the SAME row (oi.status, oi.due_at, comp.has_accepted, comp.has_recorded_unverified), so
	//     bucketing is a pure per-row partition with no cross-grain dependency:
	//       verified  = comp.has_accepted                                  (nobody owes anything)
	//       submitted = NOT has_accepted AND has_recorded_unverified       (the VERIFIER owes review)
	//       pending   = NOT has_accepted AND NOT has_recorded_unverified
	//                   AND status IN (open set) AND due business date <= as-of business date
	//                                                                     (the OPERATOR owes work)
	//     has_accepted is checked first in every branch, so no obligation lands in two buckets, and
	//     pending + submitted + verified <= the cell's obligation total.
	//     Reconciliation key sets, shown identical: submitted_count's key set is
	//     {obligation_id : has_recorded_unverified AND NOT has_accepted}, the obligation-grain image
	//     of the KPI row's awaiting_verification key set {target_id : has_recorded_unverified AND
	//     NOT has_accepted} computed over the SAME tenant/batch/park filter as the KPI query above;
	//     the two agree exactly at one-obligation-per-animal-per-vaccine (the grain every live drive
	//     uses), and the matrix stays obligation grain BY DESIGN so a multi-vaccine animal is
	//     visible once per vaccine. Business-day grain, Asia/Kolkata, on both sides of the due-date
	//     comparison — never an instant, never now()±N.
	//     animal_count numerator: DISTINCT target_id per cohort; denominator: all non-terminated
	//     animals in the cohort.
	// (d) min/max_administered_at are MIN/MAX over the VERIFIED (accepted) completions of the same
	//     obligation rows only — they are a date range read off the cell's verified bucket, never a
	//     count, so they add no grain and cannot double count. They are NULL when verified_count is
	//     0. They carry the real medical dates so a clubbed adult drive does not report its planned
	//     date as the administration date.
	cohortSQL := `
WITH comp AS (
  SELECT
    obligation_id,
    bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified,
    bool_or(status = 'accepted') AS has_accepted,
    MIN(CASE WHEN status = 'accepted' THEN administered_at END) AS min_administered_at,
    MAX(CASE WHEN status = 'accepted' THEN administered_at END) AS max_administered_at
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
)
SELECT
  COALESCE(park.location_id::text, '') as park_id,
  COALESCE(park.name, '') as park_name,
  g.management_stage,
  g.sex,
  pr.dose_code,
  COUNT(DISTINCT g.goat_id) as animal_count,
  COUNT(DISTINCT CASE
    WHEN NOT COALESCE(comp.has_accepted, false)
     AND NOT COALESCE(comp.has_recorded_unverified, false)
     AND oi.status IN ('scheduled','due','in_progress','deferred','missed')
     AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
    THEN oi.obligation_id END) as pending_count,
  COUNT(DISTINCT CASE
    WHEN NOT COALESCE(comp.has_accepted, false) AND COALESCE(comp.has_recorded_unverified, false)
    THEN oi.obligation_id END) as submitted_count,
  COUNT(DISTINCT CASE WHEN comp.has_accepted THEN oi.obligation_id END) as verified_count,
  MIN(CASE WHEN comp.has_accepted THEN comp.min_administered_at END) as min_administered_at,
  MAX(CASE WHEN comp.has_accepted THEN comp.max_administered_at END) as max_administered_at
FROM obligation_instances oi
JOIN goats g ON oi.target_id = g.goat_id AND oi.tenant_id = g.tenant_id
JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
LEFT JOIN comp ON oi.obligation_id = comp.obligation_id
LEFT JOIN locations shed ON oi.scope_id = shed.location_id AND oi.tenant_id = shed.tenant_id
LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
WHERE oi.tenant_id = $1::uuid
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
  AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
    SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
  ))
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
GROUP BY park.location_id, park.name, g.management_stage, g.sex, pr.dose_code
ORDER BY park.name, g.management_stage, g.sex, pr.dose_code
`
	cohortRows, err := r.pool.Query(ctx, cohortSQL, q.TenantID, asOf, q.DriveBatchID, parkID)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: cohort query: %w", err)
	}
	defer cohortRows.Close()

	// Build cohort matrix cells. SQL rows arrive per dose_code; the board cell grain is
	// cohort (stage × sex) × VACCINE, so fold dose rows into their vaccine label here
	// (pending/submitted/verified counts sum across doses -- they stay disjoint under summation
	// because each bucket is disjoint within every dose row; animal count is per-cohort and
	// identical per row).
	// Farm (park) is part of the cell key: leadership reads this matrix farmwise, so a cohort in
	// Channapatna must never merge with the same cohort in Coimbatore.
	type cohortKey struct{ parkID, stage, sex, vaccine string }
	cohortAgg := map[cohortKey]*domain.CommandBoardCohortCell{}
	cohortOrder := []cohortKey{}
	for cohortRows.Next() {
		var parkID, parkName, stage, sex, doseCode string
		var animalCount, pendingCount, submittedCount, verifiedCount int
		var minAdministeredAt, maxAdministeredAt pgtype.Timestamptz
		if err := cohortRows.Scan(&parkID, &parkName, &stage, &sex, &doseCode, &animalCount, &pendingCount, &submittedCount, &verifiedCount, &minAdministeredAt, &maxAdministeredAt); err != nil {
			return resp, fmt.Errorf("vaccination command board: cohort scan: %w", err)
		}

		// Dose-QUALIFIED, not vaccine-collapsed. Collapsing ET+TT's three doses into one column
		// summed Dose 1 + Dose 2 + Revaccination into a single number, which buried the figure
		// leadership actually asks for (ET+TT Dose 2: 210 pending, 114 verified) behind a total
		// that also exceeded the cohort head count. Same grain the shed matrix already uses.
		key := cohortKey{parkID, stage, sex, vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode)}
		cell, ok := cohortAgg[key]
		if !ok {
			cell = &domain.CommandBoardCohortCell{
				Cohort: domain.CommandBoardCohort{
					ParkID:          parkID,
					ParkName:        parkName,
					ManagementStage: stage,
					Sex:             sex,
					AnimalCount:     animalCount,
				},
				VaccineLabel: key.vaccine,
			}
			cohortAgg[key] = cell
			cohortOrder = append(cohortOrder, key)
		}
		cell.PendingCount += pendingCount
		cell.SubmittedCount += submittedCount
		cell.VerifiedCount += verifiedCount
		if minAdministeredAt.Valid && (cell.MinAdministeredDate == nil || minAdministeredAt.Time.Before(*cell.MinAdministeredDate)) {
			administeredAt := minAdministeredAt.Time
			cell.MinAdministeredDate = &administeredAt
		}
		if maxAdministeredAt.Valid && (cell.MaxAdministeredDate == nil || maxAdministeredAt.Time.After(*cell.MaxAdministeredDate)) {
			administeredAt := maxAdministeredAt.Time
			cell.MaxAdministeredDate = &administeredAt
		}
	}
	if err := cohortRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: cohort rows: %w", err)
	}
	// 2a-bis. TRUE cohort head count, read from the live herd rather than from the obligations.
	// The cohort query above can only see animals that carry an obligation for THAT dose, so the
	// "Animals" column reported 229 for a cohort of 324 live adults — every animal whose Dose 1
	// obligation had been closed out of the window vanished from its own head count. Head count is a
	// herd fact, not an obligation fact, so it is read from goats.
	// projection-review: membership=goats (live, shed-resolved); group_key=park x management_stage x sex; join_cardinality=locations 1:1 (shed -> park); pagination=bounded cohort aggregate; scope=tenant + optional park
	// (a) producer unique columns: goat_id | consumer GROUP BY: park.location_id, management_stage, sex.
	// (b) join multiplicity: shed 1:1 on goats.shed_id, park 1:1 on shed.parent_location_id — no fan-out.
	// (c) key set: identical park x stage x sex key the cohort cells use, so the head count and the
	//     cell counts describe the same cohort. Drive-batch scope is deliberately NOT applied: a
	//     cohort's head count does not shrink because a drive covers part of it.
	cohortHeadSQL := `
SELECT
  COALESCE(park.location_id::text, '') as park_id,
  g.management_stage,
  g.sex,
  COUNT(*) as head_count
FROM goats g
LEFT JOIN locations shed ON g.shed_id = shed.location_id AND g.tenant_id = shed.tenant_id
LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
WHERE g.tenant_id = $1::uuid
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR park.location_id = $2::uuid)
GROUP BY park.location_id, g.management_stage, g.sex
`
	headRows, err := r.pool.Query(ctx, cohortHeadSQL, q.TenantID, parkID)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: cohort head count query: %w", err)
	}
	defer headRows.Close()
	type headKey struct{ parkID, stage, sex string }
	headCounts := map[headKey]int{}
	for headRows.Next() {
		var parkIDValue, stage, sex string
		var headCount int
		if err := headRows.Scan(&parkIDValue, &stage, &sex, &headCount); err != nil {
			return resp, fmt.Errorf("vaccination command board: cohort head count scan: %w", err)
		}
		headCounts[headKey{parkIDValue, stage, sex}] = headCount
	}
	if err := headRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: cohort head count rows: %w", err)
	}
	for _, key := range cohortOrder {
		cell := cohortAgg[key]
		if head, ok := headCounts[headKey{cell.Cohort.ParkID, cell.Cohort.ManagementStage, cell.Cohort.Sex}]; ok {
			cell.Cohort.AnimalCount = head
		}
	}

	// 2b. Per-day administration split for the cohort cells.
	// projection-review: membership=accepted vaccination_completions of the same tenant/batch/park obligations; group_key=park x management_stage x sex x dose_code x IST administered date; join_cardinality=goats 1:1 on target_id, protocol_rules 1:1 on rule_id, locations 1:1; pagination=bounded (one row per cohort-dose-DAY, days bounded by the drive window); scope=tenant + optional batch + optional park EXISTS
	// (a) producer unique columns: (obligation_id, completion_id) accepted rows | consumer GROUP BY:
	//     park.location_id, g.management_stage, g.sex, pr.dose_code, administered IST date.
	// (b) join multiplicity: vaccination_completions is the MANY side and is the row source here, so
	//     the animal count is COUNT(DISTINCT g.goat_id) — two accepted completions for the same
	//     animal on the same day count that animal once. goats/protocol_rules/locations are 1:1.
	// (c) ratio/cap check: none. This query only splits the cell's verified bucket by day; the
	//     per-day counts range over the SAME key set as verified_count above plus the date, so
	//     SUM(day counts) >= verified_count is expected only when an animal was dosed on two days
	//     for the same dose — impossible for an accepted single dose, and the UI shows days, not a
	//     re-derived total.
	cohortDaySQL := `
SELECT
  COALESCE(park.location_id::text, '') as park_id,
  g.management_stage,
  g.sex,
  pr.dose_code,
  (vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date as administered_date,
  COUNT(DISTINCT g.goat_id) as animal_count
FROM obligation_instances oi
JOIN vaccination_completions vc ON vc.obligation_id = oi.obligation_id AND vc.tenant_id = oi.tenant_id AND vc.status = 'accepted'
JOIN goats g ON oi.target_id = g.goat_id AND oi.tenant_id = g.tenant_id
JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
LEFT JOIN locations shed ON oi.scope_id = shed.location_id AND oi.tenant_id = shed.tenant_id
LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
WHERE oi.tenant_id = $1::uuid
  AND vc.administered_at IS NOT NULL
  AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $2::uuid)
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
    SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $3::uuid
  ))
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
GROUP BY park.location_id, g.management_stage, g.sex, pr.dose_code, administered_date
ORDER BY administered_date
`
	dayRows, err := r.pool.Query(ctx, cohortDaySQL, q.TenantID, q.DriveBatchID, parkID)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: cohort day query: %w", err)
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var parkIDValue, stage, sex, doseCode string
		var administeredDate pgtype.Date
		var animalCount int
		if err := dayRows.Scan(&parkIDValue, &stage, &sex, &doseCode, &administeredDate, &animalCount); err != nil {
			return resp, fmt.Errorf("vaccination command board: cohort day scan: %w", err)
		}
		if !administeredDate.Valid {
			continue
		}
		// Same cell key the cohort loop folds to, so a day row can only land on the cell it
		// describes: dose codes collapse into the dose-qualified display label.
		key := cohortKey{parkIDValue, stage, sex, vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode)}
		cell, ok := cohortAgg[key]
		if !ok {
			continue
		}
		date := administeredDate.Time.Format("2006-01-02")
		merged := false
		for i := range cell.AdministeredDays {
			if cell.AdministeredDays[i].Date == date {
				// Two dose codes fold into one display label (ET+TT Dose 1 kid vs adult course):
				// same cell, same day, so the day counts add.
				cell.AdministeredDays[i].AnimalCount += animalCount
				merged = true
				break
			}
		}
		if !merged {
			cell.AdministeredDays = append(cell.AdministeredDays, domain.CommandBoardCohortDay{Date: date, AnimalCount: animalCount})
		}
	}
	if err := dayRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: cohort day rows: %w", err)
	}

	// 2c. Dose-sequence exceptions: an animal holding an accepted LATER dose of the same vaccine
	// course while THIS dose has no accepted completion. Reported on the missing dose's cell.
	// projection-review: membership=obligation_instances of the same tenant/batch/park; group_key=park x management_stage x sex x dose_code; join_cardinality=goats 1:1, protocol_rules 1:1, accepted-later EXISTS (no fan-out), accepted-self NOT EXISTS; pagination=whole-cohort count + list capped in Go; scope=tenant + optional batch + optional park EXISTS
	// (a) producer unique columns: obligation_id | consumer GROUP BY: park.location_id,
	//     management_stage, sex, dose_code — plus the animal identity carried per row for the list.
	// (b) join multiplicity: both dose-history probes are EXISTS/NOT EXISTS subqueries, so an
	//     animal with three later accepted doses still contributes exactly ONE row.
	// (c) key sets: the exception count ranges over the SAME park x stage x sex x dose key set as
	//     verified_count, so "321 verified · 3 exceptions" compares like with like.
	// Course family = the dose code with its position suffix removed (et_tt_adult_w1 -> et_tt_adult,
	// fmd_kid_12w -> fmd_kid), so an adult Dose 2 never claims a kid-course Dose 1 is missing.
	cohortExceptionSQL := `
WITH dose_family AS (
  SELECT
    pr.tenant_id,
    pr.rule_id,
    pr.dose_code,
    pr.sequence,
    regexp_replace(pr.dose_code, '_(w[0-9]+|[0-9]+w|revac|booster|first)$', '') as family
  FROM protocol_rules pr
  WHERE pr.tenant_id = $1::uuid
)
SELECT
  COALESCE(park.location_id::text, '') as park_id,
  g.management_stage,
  g.sex,
  df.dose_code,
  g.goat_id::text,
  g.display_id,
  COALESCE((
    SELECT gi.identifier_value FROM goat_identifiers gi
    WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
      AND gi.status = 'active' AND gi.is_primary_for_goat
    LIMIT 1
  ), '') as tag
FROM obligation_instances oi
JOIN goats g ON oi.target_id = g.goat_id AND oi.tenant_id = g.tenant_id
JOIN dose_family df ON oi.rule_id = df.rule_id AND oi.tenant_id = df.tenant_id
LEFT JOIN locations shed ON oi.scope_id = shed.location_id AND oi.tenant_id = shed.tenant_id
LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
WHERE oi.tenant_id = $1::uuid
  AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $2::uuid)
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
    SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $3::uuid
  ))
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM obligation_instances self_oi
    JOIN vaccination_completions self_vc ON self_vc.obligation_id = self_oi.obligation_id AND self_vc.tenant_id = self_oi.tenant_id AND self_vc.status = 'accepted'
    JOIN dose_family self_df ON self_oi.rule_id = self_df.rule_id AND self_oi.tenant_id = self_df.tenant_id
    WHERE self_oi.tenant_id = oi.tenant_id AND self_oi.target_id = oi.target_id AND self_df.dose_code = df.dose_code
  )
  AND EXISTS (
    SELECT 1 FROM obligation_instances later_oi
    JOIN vaccination_completions later_vc ON later_vc.obligation_id = later_oi.obligation_id AND later_vc.tenant_id = later_oi.tenant_id AND later_vc.status = 'accepted'
    JOIN dose_family later_df ON later_oi.rule_id = later_df.rule_id AND later_oi.tenant_id = later_df.tenant_id
    WHERE later_oi.tenant_id = oi.tenant_id AND later_oi.target_id = oi.target_id
      AND later_df.family = df.family AND later_df.sequence > df.sequence
  )
GROUP BY park.location_id, g.management_stage, g.sex, df.dose_code, g.goat_id, g.display_id, g.tenant_id
ORDER BY g.display_id
`
	exceptionRows, err := r.pool.Query(ctx, cohortExceptionSQL, q.TenantID, q.DriveBatchID, parkID)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: cohort exception query: %w", err)
	}
	defer exceptionRows.Close()
	for exceptionRows.Next() {
		var parkIDValue, stage, sex, doseCode, goatID, displayID, tag string
		if err := exceptionRows.Scan(&parkIDValue, &stage, &sex, &doseCode, &goatID, &displayID, &tag); err != nil {
			return resp, fmt.Errorf("vaccination command board: cohort exception scan: %w", err)
		}
		key := cohortKey{parkIDValue, stage, sex, vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode)}
		cell, ok := cohortAgg[key]
		if !ok {
			continue
		}
		cell.MissingPriorDoseCount++
		if len(cell.MissingPriorDoseGoats) < domain.CommandBoardCohortExceptionListCap {
			cell.MissingPriorDoseGoats = append(cell.MissingPriorDoseGoats, domain.CommandBoardCohortAnimal{
				GoatID:    goatID,
				DisplayID: displayID,
				Tag:       tag,
			})
		}
	}
	if err := exceptionRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: cohort exception rows: %w", err)
	}

	for _, key := range cohortOrder {
		resp.CohortMatrix = append(resp.CohortMatrix, *cohortAgg[key])
	}

	// 3. Shed dose matrix query
	// projection-review: membership=shed-scoped obligation_instances per tenant/batch/park; group_key=shed_id x dose_code x state; join_cardinality=comp CTE 1:1, protocol_rules 1:1, locations 1:1; pagination=bounded tenant aggregate (no paging); scope=tenant + optional batch + optional park EXISTS
	// Detail (landed-review P1): grain=shed_id x dose_code x state — one row per cell, aggregated ONLY in the outer SELECT.
	// (a) producer unique columns: obligation_id (with scope_id, dose_code, per-obligation state)
	//     | consumer GROUP BY: shed_id, shed_name, dose_code, state
	// (b) join multiplicity: completions pre-aggregated per obligation (comp CTE, 1:1),
	//     protocol_rules 1:1 on rule_id, locations 1:1 on scope_id; inner CTE has NO GROUP BY
	//     so due_at/state cannot split cells (landed-review P1 regression:
	//     TestVaccinationCommandBoardShedDoseDateShiftOneCellPerState). overdue/scheduled state is an
	//     IST business-DATE comparison ((due_at AT TIME ZONE 'Asia/Kolkata')::date vs
	//     (asOf AT TIME ZONE 'Asia/Kolkata')::date), never an instant comparison — a dose due today at
	//     00:00 IST stays 'scheduled' all day, never 'overdue' (TestVaccinationCommandBoardDueTodayDateShiftNotOverdue).
	// (c) animal_count numerator: COUNT(DISTINCT target_id) over the cell's obligations;
	//     denominator/key set: same shed x dose x state cell — identical key sets.
	shedDoseSQL := `
WITH comp AS (
  SELECT
    obligation_id,
    bool_or(status = 'accepted') AS has_accepted,
    bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified,
    MAX(CASE WHEN administered_at IS NOT NULL THEN administered_at END) as max_administered_at,
    MIN(CASE WHEN administered_at IS NOT NULL THEN administered_at END) as min_administered_at
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
),
shed_dose_obligations AS (
  -- per-OBLIGATION state; aggregation to the shed x dose x state cell happens ONLY in
  -- the outer SELECT so one cell is always exactly one row regardless of due dates.
  SELECT
    oi.scope_id as shed_id,
    loc.name as shed_name,
    CASE
      WHEN lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
      ELSE btrim(gsp.partition_label)
    END AS partition_label,
    pr.dose_code,
    CASE
      WHEN comp.has_accepted THEN 'verified'
      WHEN comp.has_recorded_unverified THEN 'awaiting'
      WHEN oi.status IN ('scheduled','due','in_progress','deferred','missed') AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue'
      WHEN oi.status IN ('scheduled','due','in_progress','deferred','missed') THEN 'scheduled'
      ELSE 'other'
    END as state,
    oi.target_id,
    comp.min_administered_at,
    comp.max_administered_at,
    CASE WHEN oi.status IN ('scheduled','due','in_progress','deferred','missed') THEN oi.due_at END as due_at
  FROM obligation_instances oi
  JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
  JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
  LEFT JOIN comp ON oi.obligation_id = comp.obligation_id
  LEFT JOIN locations loc ON oi.scope_id = loc.location_id AND oi.tenant_id = loc.tenant_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.scope_type = 'shed'
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
    AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR loc.parent_location_id = $4::uuid)
)
SELECT shed_id, shed_name, partition_label, dose_code, state,
  COUNT(DISTINCT target_id) as animal_count,
  MIN(min_administered_at) as min_administered_at,
  MAX(max_administered_at) as max_administered_at,
  MIN(due_at) as min_due_at,
  MAX(due_at) as max_due_at
FROM shed_dose_obligations
WHERE state != 'other'
GROUP BY shed_id, shed_name, partition_label, dose_code, state
ORDER BY shed_name, partition_label, dose_code, state
`
	shedDoseRows, err := r.pool.Query(ctx, shedDoseSQL, q.TenantID, asOf, q.DriveBatchID, parkID)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: shed dose query: %w", err)
	}
	defer shedDoseRows.Close()

	for shedDoseRows.Next() {
		var shedID, shedName, partitionLabel, doseCode, state string
		var animalCount int
		var minAdministeredAt, maxAdministeredAt, minDueAt, maxDueAt pgtype.Timestamptz
		if err := shedDoseRows.Scan(&shedID, &shedName, &partitionLabel, &doseCode, &state, &animalCount, &minAdministeredAt, &maxAdministeredAt, &minDueAt, &maxDueAt); err != nil {
			return resp, fmt.Errorf("vaccination command board: shed dose scan: %w", err)
		}

		cell := domain.ShedDoseMatrixCell{
			ShedID:                     shedID,
			ShedName:                   shedName,
			PartitionLabel:             partitionLabel,
			OperationalLocationDisplay: oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display(),
			DoseRule:                   vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode),
			State:                      state,
			AnimalCount:                animalCount,
		}

		if minAdministeredAt.Valid {
			cell.MinAdministeredDate = &minAdministeredAt.Time
		}
		if maxAdministeredAt.Valid {
			cell.MaxAdministeredDate = &maxAdministeredAt.Time
		}
		if minDueAt.Valid {
			cell.MinDueDate = &minDueAt.Time
		}
		if maxDueAt.Valid {
			cell.MaxDueDate = &maxDueAt.Time
		}

		resp.ShedDoseMatrix = append(resp.ShedDoseMatrix, cell)
	}
	if err := shedDoseRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: shed dose rows: %w", err)
	}

	// 3b. Shed × VACCINE matrix, dose collapsed, reported as a flag.
	//
	// This is NOT a vaccine-collapsed rewrite of the dose matrix above, which stays dose-qualified
	// because leadership asks per-dose figures and an earlier collapse was reverted for SUMMING
	// Dose 1 + Dose 2 + Revaccination into a number that exceeded the cohort head count. The cell
	// here carries no sum: state is bool_or over the shed's doses of that vaccine, so collapsing
	// cannot over-count by construction. The two matrices answer different questions — "how much of
	// each dose" and "is anything behind at all" — and neither can be derived from the other on the
	// client without losing a guarantee.
	//
	// BEHIND is deliberately broader than the KPI row's missed bucket: an animal counts as behind
	// when it holds a dose of this vaccine that is 'missed' OR is still open with its due business
	// date already past, and has no accepted completion for it. A park head walking the shed cannot
	// act on the distinction between "the sweeper has flipped this to missed" and "the sweeper has
	// not run yet" — both mean the animal is unvaccinated past its window.
	//
	// projection-review: membership=obligation_instances in scope, scope_type='shed', joined to their rule's vaccine; group_key=(shed location_id, vaccine_code) — the cell grain the UI renders, so no client-side regrouping can change a cell's meaning; join_cardinality=comp pre-aggregated per obligation before the fold so a dose with several completions cannot multiply its animal, protocol_rule_dimensions is 1..N per rule_id (see (b)), locations 0..1 on PK; pagination=NONE, a bounded sheds × vaccines aggregate (11 × 6 on the live tenant) computed in the database; scope=tenant_id plus the capability-resolved park filter parented through locations.parent_location_id, PLUS the same scope_type='shed' guard the dose-matrix query carries, so the two cannot disagree about what counts as a shed.
	// (a) producer unique columns: obligation_id | consumer GROUP BY: shed.location_id, d.vaccine_code.
	// (b) join multiplicity: protocol_rule_dimensions is NOT 0..1 per rule_id — publish compiles one
	//     rule into up to maxCompiledRuleDimensionsPerRule rows, one per selector combination. The
	//     fan-out is harmless HERE, and only here, because vaccine_code is computed once per rule and
	//     is therefore identical on every dimension row of that rule, while behind/total are
	//     COUNT(DISTINCT target_id): the same (target_id, vaccine_code) pair collapses no matter how
	//     many dimension rows the join emits. It is a real scan cost, not a real count error.
	// (c) cap check: behind_animals <= total_animals by construction — the FILTER is a subset of the
	//     same COUNT(DISTINCT target_id) key set.
	// (d) scope_type='shed' is REQUIRED, not defensive. obligation_instances.scope_type also takes
	//     'park', 'cohort', 'tenant' and 'custodian_party'. Without the guard a park-scoped
	//     obligation joins locations on the PARK row and renders as a phantom shed named after the
	//     park, and cohort/tenant/custodian-scoped rows miss the join entirely, COALESCE to an empty
	//     shed id, and pile into a single blank row that silently mixes animals from everywhere.
	shedVaccineSQL := `
WITH comp AS (
  SELECT obligation_id,
         bool_or(status = 'accepted') AS has_accepted,
         bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
)
SELECT
  shed.location_id::text AS shed_id,
  COALESCE(shed.name, '') AS shed_name,
  CASE
    WHEN lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
    ELSE btrim(gsp.partition_label)
  END AS partition_label,
  COALESCE(park.name, '') AS park_name,
  d.vaccine_code,
  -- BEHIND is "no dose reached this animal": no accepted completion AND no recorded proof waiting on
  -- a verifier. The recorded-proof exclusion is the whole point. On stg all 137 obligations carrying
  -- status='missed' were DOSED ON THE DAY THEY WERE DUE (due 2026-08-05 IST, administered
  -- 2026-08-05 IST) and carry a 'recorded' completion whose verifier has not looked at it yet.
  -- Counting those as behind told a park head that 76 goats in Sumathi 1 were unvaccinated when the
  -- operator had already vaccinated every one of them -- the opposite of the truth, and it
  -- contradicted the dose matrix on the same screen, which correctly showed them amber "given,
  -- awaiting verification". A missed obligation with proof against it is a VERIFICATION backlog, not
  -- a vaccination failure, and the two need completely different actions.
  COUNT(DISTINCT oi.target_id) FILTER (
    WHERE NOT COALESCE(comp.has_accepted, false)
      AND NOT COALESCE(comp.has_recorded_unverified, false)
      AND (
        oi.status = 'missed'
        OR (oi.status IN ('scheduled','due','in_progress','deferred')
            AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
      )
  )::bigint AS behind_animals,
  -- Dose given, proof recorded, verifier has not accepted it yet.
  COUNT(DISTINCT oi.target_id) FILTER (
    WHERE NOT COALESCE(comp.has_accepted, false)
      AND COALESCE(comp.has_recorded_unverified, false)
  )::bigint AS verifying_animals,
  COUNT(DISTINCT oi.target_id)::bigint AS total_animals
FROM obligation_instances oi
JOIN protocol_rule_dimensions d ON d.rule_id = oi.rule_id AND d.tenant_id = oi.tenant_id
JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
LEFT JOIN comp ON comp.obligation_id = oi.obligation_id
JOIN locations shed ON shed.location_id = oi.scope_id AND shed.tenant_id = oi.tenant_id
LEFT JOIN locations park ON park.location_id = shed.parent_location_id AND park.tenant_id = shed.tenant_id
WHERE oi.tenant_id = $1::uuid
  AND oi.scope_type = 'shed'
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND d.vaccine_code <> ''
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
  AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
    SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
  ))
GROUP BY shed.location_id, shed.name, partition_label, park.name, d.vaccine_code
`
	shedVaccineRows, err := r.pool.Query(ctx, shedVaccineSQL, q.TenantID, asOf, q.DriveBatchID, parkID)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: shed vaccine query: %w", err)
	}
	defer shedVaccineRows.Close()
	type shedVaccineKey struct{ shedID, partitionLabel, vaccine string }
	type shedIdentity struct{ id, name, partitionLabel, display, park string }
	shedVaccineCells := map[shedVaccineKey]domain.CommandBoardShedVaccineCell{}
	sheds := map[string]shedIdentity{}
	shedOrder := []string{}
	for shedVaccineRows.Next() {
		var shedID, shedName, partitionLabel, parkName, vaccineCode string
		var behind, verifying, total int64
		if err := shedVaccineRows.Scan(&shedID, &shedName, &partitionLabel, &parkName, &vaccineCode, &behind, &verifying, &total); err != nil {
			return resp, fmt.Errorf("vaccination command board: shed vaccine scan: %w", err)
		}
		shedKey := shedID + "|" + partitionLabel
		if _, seen := sheds[shedKey]; !seen {
			display := oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display()
			sheds[shedKey] = shedIdentity{id: shedID, name: shedName, partitionLabel: partitionLabel, display: display, park: parkName}
			shedOrder = append(shedOrder, shedKey)
		}
		// BEHIND outranks VERIFYING: an animal nobody dosed is a bigger problem than one whose proof
		// is queued, so a shed holding both reads red. Verifying is amber on its own -- the work is
		// done and the wait is on a person at a desk, not on the herd.
		state := "ok"
		switch {
		case behind > 0:
			state = "behind"
		case verifying > 0:
			state = "verifying"
		}
		shedVaccineCells[shedVaccineKey{shedID, partitionLabel, vaccineCode}] = domain.CommandBoardShedVaccineCell{
			ShedID:                     shedID,
			ShedName:                   shedName,
			PartitionLabel:             partitionLabel,
			OperationalLocationDisplay: oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display(),
			ParkName:                   parkName,
			VaccineCode:                vaccineCode,
			State:                      state,
			BehindAnimals:              int(behind),
			VerifyingAnimals:           int(verifying),
			TotalAnimals:               int(total),
		}
	}
	if err := shedVaccineRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: shed vaccine rows: %w", err)
	}

	// Columns come from the tenant's PUBLISHED vaccination protocol -- not from the cells above, and
	// not from every dimension row that has ever existed.
	//
	// Deriving them from the CELLS would drop a vaccine the protocol requires but which generated no
	// obligations anywhere, and that silence is exactly what a reader needs: an all-grey column then
	// means "the protocol asks for this and nothing is planned", which is a real finding.
	//
	// Taking the WHOLE dimension table instead invents findings. Dimensions accumulate per protocol
	// VERSION and retired versions keep their rows forever -- on this tenant BLUE_TONGUE exists only
	// on a RETIRED version, so listing it produced an all-grey column that read as a protocol gap
	// when the vaccine had simply been withdrawn. Published versions of the vaccination category are
	// the source-of-truth boundary: what the herd is currently required to receive.
	vaccineCodeSQL := `
SELECT DISTINCT d.vaccine_code
FROM protocol_rule_dimensions d
JOIN protocol_versions pv
  ON pv.protocol_version_id = d.protocol_version_id
 AND pv.tenant_id = d.tenant_id
WHERE d.tenant_id = $1::uuid
  AND d.vaccine_code <> ''
  AND d.category = 'vaccination'
  AND pv.status = 'published'
ORDER BY d.vaccine_code
`
	vaccineCodeRows, err := r.pool.Query(ctx, vaccineCodeSQL, q.TenantID)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: vaccine catalogue query: %w", err)
	}
	defer vaccineCodeRows.Close()
	for vaccineCodeRows.Next() {
		var code string
		if err := vaccineCodeRows.Scan(&code); err != nil {
			return resp, fmt.Errorf("vaccination command board: vaccine catalogue scan: %w", err)
		}
		// Labelled HERE, from the one canonical vaccine labeller, so the column header is server
		// copy like every other visible string on this board.
		resp.ShedVaccineColumns = append(resp.ShedVaccineColumns, domain.CommandBoardVaccineColumn{
			Code:  code,
			Label: vaccinatdomain.VaccineAntigenLabel(code),
		})
	}
	if err := vaccineCodeRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: vaccine catalogue rows: %w", err)
	}

	// 3c. The ANIMALS behind each red cell.
	//
	// A red cell without them is a dead end on the screen: "Sumathi 1 is behind on ET+TT" is where a
	// leader's question starts, and the next one is always "which animals, and since when". Without
	// this the only way to answer was a database query, so the matrix could raise an alarm it could
	// not explain. Same reasoning, and the same shape, as the ClosedWithoutDose tile's animal list.
	//
	// Only BEHIND cells are listed — green and grey cells have nothing to explain — and the whole
	// list is capped. The cell's behindAnimals COUNT stays whole-scope truth; the list is evidence,
	// not the number, and the UI says so when it is truncated.
	//
	// projection-review: membership=behind obligations of shed-scoped instances in the same filter as the cell aggregate above; group_key=(shed, vaccine, animal) with one row per animal per cell; join_cardinality=goats 1:1 on target_id, locations 0..1 on PK, the identifier lookup is a LIMIT-1 scalar subquery, and DISTINCT ON collapses the protocol_rule_dimensions fan-out plus an animal's several behind doses of one vaccine to a single row; pagination=hard LIMIT, ordered so the cap is deterministic; scope=identical tenant/scope_type/batch/park predicate to the cell aggregate, so the list can never name an animal from outside the cell it explains.
	shedVaccineAnimalSQL := `
WITH per_animal_behind AS (
WITH comp AS (
  SELECT obligation_id,
         bool_or(status = 'accepted') AS has_accepted,
         bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified,
         max(administered_at) FILTER (WHERE status = 'recorded' AND verified_at IS NULL) AS recorded_at
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
)
SELECT DISTINCT ON (
  oi.scope_id,
  CASE
    WHEN lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
    ELSE btrim(gsp.partition_label)
  END,
  d.vaccine_code,
  g.goat_id
)
  oi.scope_id::text AS shed_id,
  d.vaccine_code,
  g.goat_id::text,
  g.display_id,
  -- BOTH ear tags, not just the primary. 1004 of this tenant's 1670 goats carry two active
  -- identifiers and 172 carry three, so a LIMIT-1 on is_primary_for_goat printed one tag and hid the
  -- other -- and an operator reading the other ear could not match the animal in front of them to
  -- the row. The internal display_id is NOT an identity on the farm; it is a fallback for the rare
  -- animal with no active tag at all.
  COALESCE((
    SELECT gi.identifier_value FROM goat_identifiers gi
    WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
      AND gi.status = 'active' AND gi.identifier_type = 'animal_identifier_1'
    LIMIT 1
  ), '') AS tag1,
  COALESCE((
    SELECT gi.identifier_value FROM goat_identifiers gi
    WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
      AND gi.status = 'active' AND gi.identifier_type = 'animal_identifier_2'
    LIMIT 1
  ), '') AS tag2,
  oi.status,
  oi.due_at,
  COALESCE(park.name, '') AS park_name,
  COALESCE(shed.name, '') AS shed_name,
  -- Ground location is park + physical shed + PARTITION. The shed name alone sends a person to
  -- "Godel 1" when the animal is standing in "Godel 1 - Part 3", which on a partitioned shed is a
  -- different pen and a wasted trip. Same source the closed-without-dose drawer already uses.
  CASE
    WHEN lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
    ELSE btrim(gsp.partition_label)
  END AS partition_label,
  COALESCE(comp.has_recorded_unverified, false) AS awaiting_verification,
  comp.recorded_at
FROM obligation_instances oi
JOIN protocol_rule_dimensions d ON d.rule_id = oi.rule_id AND d.tenant_id = oi.tenant_id
JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
LEFT JOIN comp ON comp.obligation_id = oi.obligation_id
LEFT JOIN locations shed ON shed.location_id = oi.scope_id AND shed.tenant_id = oi.tenant_id
LEFT JOIN locations park ON park.location_id = shed.parent_location_id AND park.tenant_id = shed.tenant_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
WHERE oi.tenant_id = $1::uuid
  AND oi.scope_type = 'shed'
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND d.vaccine_code <> ''
  AND NOT COALESCE(comp.has_accepted, false)
  AND (
    -- Both flagged states drill down here: genuinely-behind animals AND animals whose proof is
    -- waiting on a verifier. The row carries which one it is, so the drawer can say "video
    -- verification pending" instead of accusing an operator who already did the work.
    COALESCE(comp.has_recorded_unverified, false)
    OR oi.status = 'missed'
    OR (oi.status IN ('scheduled','due','in_progress','deferred')
        AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
  )
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
  AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
    SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
  ))
ORDER BY oi.scope_id, partition_label, d.vaccine_code, g.goat_id, oi.due_at ASC NULLS LAST
)
-- The cap is a GLOBAL budget across every behind cell, so the order that decides who survives it
-- has to be applied HERE, over the whole set, and not inside the DISTINCT ON. Ordering by due date
-- first means truncation drops the most recently missed rather than the longest waiting; without
-- this wrapper the effective order was goat_id, i.e. arbitrary.
SELECT * FROM per_animal_behind
ORDER BY due_at ASC NULLS LAST, shed_id, partition_label, vaccine_code, goat_id
LIMIT $5
`
	behindAnimals := map[shedVaccineKey][]domain.CommandBoardShedVaccineAnimal{}
	behindRows, err := r.pool.Query(ctx, shedVaccineAnimalSQL, q.TenantID, asOf, q.DriveBatchID, parkID,
		domain.CommandBoardShedVaccineAnimalListCap)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: shed vaccine animals query: %w", err)
	}
	defer behindRows.Close()
	for behindRows.Next() {
		var shedID, vaccineCode, goatID, displayID, tag1, tag2, status string
		var parkName, shedName, partitionLabel string
		var dueAt, recordedAt pgtype.Timestamptz
		var awaitingVerification bool
		if err := behindRows.Scan(&shedID, &vaccineCode, &goatID, &displayID, &tag1, &tag2, &status, &dueAt,
			&parkName, &shedName, &partitionLabel, &awaitingVerification, &recordedAt); err != nil {
			return resp, fmt.Errorf("vaccination command board: shed vaccine animals scan: %w", err)
		}
		animal := domain.CommandBoardShedVaccineAnimal{
			GoatID:               goatID,
			DisplayID:            displayID,
			Tag:                  tag1,
			Tag2:                 tag2,
			Status:               status,
			AwaitingVerification: awaitingVerification,
			LocationDisplay: oploc.OperationalLocation{
				ParkName:       parkName,
				ShedName:       shedName,
				PartitionLabel: partitionLabel,
			}.Display(),
		}
		// NormalizePartition collapses every non-partitioned encoding onto the "whole" MATCHING
		// sentinel, which is a grouping key and never copy, so an unpartitioned shed puts no
		// partition on the wire at all.
		if normalized := oploc.NormalizePartition(partitionLabel); oploc.IsPartitioned(normalized) {
			animal.PartitionLabel = strings.TrimSpace(partitionLabel)
		}
		if recordedAt.Valid {
			rec := recordedAt.Time
			animal.RecordedAt = &rec
		}
		if dueAt.Valid {
			due := dueAt.Time
			animal.DueAt = &due
		}
		key := shedVaccineKey{shedID, partitionLabel, vaccineCode}
		behindAnimals[key] = append(behindAnimals[key], animal)
	}
	if err := behindRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: shed vaccine animals rows: %w", err)
	}

	// 3d. The shed's proof VIDEOS for the day its pending doses were recorded.
	//
	// Grain matters here and getting it wrong is what made the first attempt useless. Vaccination
	// proof is filmed PER SHED for the operator day -- Sumathi 1 has five clips for 2026-08-05
	// covering 76 goats -- so hanging a video off each animal row repeated one link 76 times and
	// implied per-goat footage that does not exist. The videos belong to the CELL, and the drawer
	// header is where a verifier reaches them.
	//
	// The field_key predicate EXCLUDES the weighing captures by name rather than requiring the word
	// "vaccination". Weighing writes weighing_individual_video and weighing_shed_partition_video --
	// explicit, self-describing keys -- while the vaccination shed clip is the generic shed_video.
	// An include-list keyed on "vaccination" therefore matched nothing and reported "no video
	// uploaded" for 137 doses whose footage was sitting in GCS the whole time. Excluding the keys
	// that are known to be something else keeps a verifier away from footage of a goat on a scale
	// while still surfacing the clips that exist.
	//
	// projection-review: membership=proof_artifacts scoped to a shed in view with a completed video upload on the IST day that shed's pending doses were recorded; group_key=(shed, IST day) folded onto the cell; join_cardinality=locations 0..1 on PK, no obligation join exists on proof_artifacts so the day is the only correlation available and it is stated as such rather than implied to be per-animal; pagination=bounded by sheds-in-view x clips-per-day (11 on the live tenant); scope=tenant_id plus the same shed set the cell aggregate produced.
	shedVideoSQL := `
SELECT
  pa.scope_id::text AS shed_id,
  pa.proof_id::text,
  pa.uploaded_at,
  COALESCE(pa.duration_ms, 0)
FROM proof_artifacts pa
WHERE pa.tenant_id = $1::uuid
  AND pa.proof_type = 'video'
  AND pa.upload_state = 'completed'
  AND COALESCE(pa.metadata->>'field_key', '') NOT LIKE 'weighing%'
  AND pa.scope_id = ANY($2::uuid[])
  AND (pa.uploaded_at AT TIME ZONE 'Asia/Kolkata')::date = ANY($3::date[])
ORDER BY pa.uploaded_at
`
	// Only the sheds and days that actually have doses waiting on a verifier are asked for, so the
	// query cannot drag in unrelated footage from other days.
	videoShedIDs := []string{}
	videoDays := []time.Time{}
	seenDay := map[string]bool{}
	for key, cell := range shedVaccineCells {
		if cell.State != "verifying" {
			continue
		}
		videoShedIDs = append(videoShedIDs, key.shedID)
		for _, animal := range behindAnimals[key] {
			if animal.RecordedAt == nil {
				continue
			}
			day := animal.RecordedAt.In(biztime.DefaultLocation()).Format("2006-01-02")
			if !seenDay[day] {
				seenDay[day] = true
				parsed, err := time.ParseInLocation("2006-01-02", day, biztime.DefaultLocation())
				if err == nil {
					videoDays = append(videoDays, parsed)
				}
			}
		}
	}
	shedVideos := map[string][]domain.CommandBoardShedVideo{}
	if len(videoShedIDs) > 0 && len(videoDays) > 0 {
		videoRows, err := r.pool.Query(ctx, shedVideoSQL, q.TenantID, videoShedIDs, videoDays)
		if err != nil {
			return resp, fmt.Errorf("vaccination command board: shed video query: %w", err)
		}
		defer videoRows.Close()
		for videoRows.Next() {
			var shedID, proofID string
			var uploadedAt pgtype.Timestamptz
			var durationMS int64
			if err := videoRows.Scan(&shedID, &proofID, &uploadedAt, &durationMS); err != nil {
				return resp, fmt.Errorf("vaccination command board: shed video scan: %w", err)
			}
			video := domain.CommandBoardShedVideo{
				// Playback PATH, not a bare id: the client must not have to know how proof URLs are
				// built, and the signed GCS URL is minted per request by the proof service.
				Path:       "/app/proofs/" + proofID + "/download",
				DurationMS: durationMS,
			}
			if uploadedAt.Valid {
				at := uploadedAt.Time
				video.UploadedAt = &at
			}
			shedVideos[shedID] = append(shedVideos[shedID], video)
		}
		if err := videoRows.Err(); err != nil {
			return resp, fmt.Errorf("vaccination command board: shed video rows: %w", err)
		}
	}
	for key, cell := range shedVaccineCells {
		if vids := shedVideos[key.shedID]; len(vids) > 0 {
			cell.ProofVideos = vids
			shedVaccineCells[key] = cell
		}
	}

	// Densify: every shed in view gets a cell for every vaccine in the catalogue. A missing cell and
	// a clean cell are different facts and the UI must not have to guess which a gap means.
	//
	// Ordered by shed NAME for the reader, but keyed throughout by shed id: the live tenant has 175
	// sheds under only 99 distinct names ("Godel 1" exists in two parks), so name is a label, never
	// an identity. Grouping by it merges two parks' sheds into one row and attributes one park's red
	// cell to the other's shed.
	sort.SliceStable(shedOrder, func(i, j int) bool {
		a, b := sheds[shedOrder[i]], sheds[shedOrder[j]]
		if a.name != b.name {
			return a.name < b.name
		}
		if a.partitionLabel != b.partitionLabel {
			return a.partitionLabel < b.partitionLabel
		}
		return a.park < b.park
	})
	for _, shedKey := range shedOrder {
		identity := sheds[shedKey]
		for _, column := range resp.ShedVaccineColumns {
			code := column.Code
			key := shedVaccineKey{identity.id, identity.partitionLabel, code}
			if cell, ok := shedVaccineCells[key]; ok {
				cell.FlaggedAnimals = behindAnimals[key]
				resp.ShedVaccineMatrix = append(resp.ShedVaccineMatrix, cell)
				continue
			}
			resp.ShedVaccineMatrix = append(resp.ShedVaccineMatrix, domain.CommandBoardShedVaccineCell{
				ShedID:                     identity.id,
				ShedName:                   identity.name,
				PartitionLabel:             identity.partitionLabel,
				OperationalLocationDisplay: identity.display,
				ParkName:                   identity.park,
				VaccineCode:                code,
				State:                      "not_planned",
			})
		}
	}

	// 4. Weekly given query (ISO week × vaccine × status)
	// projection-review: membership=completions with administered_at; grain=ISO week × vaccine × status;
	// join_cardinality=none
	weeklySQL := `
SELECT
  EXTRACT(ISOYEAR FROM vc.administered_at AT TIME ZONE 'Asia/Kolkata')::int as iso_year,
  EXTRACT(WEEK FROM vc.administered_at AT TIME ZONE 'Asia/Kolkata')::int as iso_week,
  pr.dose_code as dose_code,
  vc.status,
  COUNT(*) as count,
  MIN(vc.administered_at) as min_administered_at,
  MAX(vc.administered_at) as max_administered_at
FROM vaccination_completions vc
JOIN obligation_instances oi ON vc.obligation_id = oi.obligation_id AND vc.tenant_id = oi.tenant_id
JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
WHERE vc.tenant_id = $1::uuid
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND vc.administered_at IS NOT NULL
  AND vc.administered_at <= $2::timestamptz
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
  AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
    SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
  ))
GROUP BY iso_year, iso_week, pr.dose_code, vc.status
ORDER BY iso_year DESC, iso_week DESC, pr.dose_code, vc.status
`
	weeklyRows, err := r.pool.Query(ctx, weeklySQL, q.TenantID, asOf, q.DriveBatchID, parkID)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: weekly query: %w", err)
	}
	defer weeklyRows.Close()

	for weeklyRows.Next() {
		var isoYear, isoWeek, count int
		var doseCode, status string
		var minAt, maxAt time.Time
		if err := weeklyRows.Scan(&isoYear, &isoWeek, &doseCode, &status, &count, &minAt, &maxAt); err != nil {
			return resp, fmt.Errorf("vaccination command board: weekly scan: %w", err)
		}
		vaccineLabel := vaccinatdomain.DoseDisplayLabel("", doseCode)
		resp.WeeklyGiven = append(resp.WeeklyGiven, domain.WeeklyGivenRow{
			ISOYear:           isoYear,
			ISOWeek:           isoWeek,
			VaccineLabel:      vaccineLabel,
			CompletionStatus:  status,
			Count:             count,
			MinAdministeredAt: minAt,
			MaxAdministeredAt: maxAt,
		})
	}
	if err := weeklyRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: weekly rows: %w", err)
	}

	// 5. Verification queue query (shed × dose awaiting verification)
	// projection-review: rows = shed×dose with ≥1 recorded-unverified completion; grain=shed×dose;
	// join_cardinality: shed 1:1, exists gate filters obligations with unverified completions only;
	// row cardinality: one row per shed×dose pair that has ≥1 unverified completion (EXISTS ensures non-empty)
	verifyQueueSQL := `
SELECT
  oi.scope_id as shed_id,
  loc.name as shed_name,
  CASE
    WHEN lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
    ELSE btrim(gsp.partition_label)
  END AS partition_label,
  pr.dose_code,
  COUNT(DISTINCT vc.completion_id) as awaiting_count,
  COUNT(DISTINCT oi.obligation_id) as total_count,
  MAX(vc.administered_at) as last_given_date,
  MIN(vc.administered_at) as first_given_date
FROM obligation_instances oi
JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
LEFT JOIN vaccination_completions vc ON oi.obligation_id = vc.obligation_id AND vc.status = 'recorded' AND vc.verified_at IS NULL
LEFT JOIN locations loc ON oi.scope_id = loc.location_id AND oi.tenant_id = loc.tenant_id
WHERE oi.tenant_id = $1::uuid
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND $2::timestamptz IS NOT NULL
  AND oi.scope_type = 'shed'
  AND EXISTS (
    SELECT 1 FROM vaccination_completions vc2
    WHERE vc2.obligation_id = oi.obligation_id AND vc2.status = 'recorded' AND vc2.verified_at IS NULL
  )
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
  AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR loc.parent_location_id = $4::uuid)
GROUP BY oi.scope_id, loc.name, partition_label, pr.dose_code
ORDER BY shed_name, partition_label, pr.dose_code
`
	verifyRows, err := r.pool.Query(ctx, verifyQueueSQL, q.TenantID, asOf, q.DriveBatchID, parkID)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: verification queue query: %w", err)
	}
	defer verifyRows.Close()

	for verifyRows.Next() {
		var shedID, shedName, partitionLabel, doseCode string
		var awaitingCount, totalCount int
		var lastGivenDate, firstGivenDate pgtype.Timestamptz
		if err := verifyRows.Scan(&shedID, &shedName, &partitionLabel, &doseCode, &awaitingCount, &totalCount, &lastGivenDate, &firstGivenDate); err != nil {
			return resp, fmt.Errorf("vaccination command board: verification queue scan: %w", err)
		}

		row := domain.VerificationQueueRow{
			ShedID:                     shedID,
			ShedName:                   shedName,
			PartitionLabel:             partitionLabel,
			OperationalLocationDisplay: oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display(),
			DoseRule:                   vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode),
			AwaitingCount:              awaitingCount,
			TotalCount:                 totalCount,
		}

		if lastGivenDate.Valid {
			row.LastGivenOnDate = &lastGivenDate.Time
		}

		// Compute business days from first_given_date to as_of
		if firstGivenDate.Valid && awaitingCount > 0 {
			// Farm operations run 7 days a week: queue age is whole business-day
			// difference in Asia/Kolkata, no weekend subtraction.
			firstDay := biztime.BusinessDayStart(firstGivenDate.Time)
			asOfDay := biztime.BusinessDayStart(asOf)
			days := int(asOfDay.Sub(firstDay).Hours() / 24)
			if days < 0 {
				days = 0
			}
			row.DaysInQueue = &days
		}

		resp.VerificationQueue = append(resp.VerificationQueue, row)
	}
	if err := verifyRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: verification queue rows: %w", err)
	}

	// 6. Drive options — the drives the board can be narrowed to. Deliberately NOT filtered by
	// the selected drive (the select must still offer the others), but it IS park-scoped so the
	// list matches the top bar's park.
	//
	// TRUNCATION IS REPORTED, NOT SWALLOWED. The list stays bounded on purpose, but a bound that
	// silently drops rows makes the picker LIE: the drive is scheduled, the board just does not
	// offer it, and the reader's only available conclusion is that it was never planned. That
	// failure got materially worse when the row grain became (batch, park), because a drive
	// spanning two parks now spends two slots. The query therefore asks for limit+1 rows and
	// keeps limit: the extra row is never rendered, it exists only to answer "was there more?",
	// which is the cheapest honest overflow probe on an ordered bounded read (no second COUNT
	// query, no OFFSET scan). The caller gets DriveOptionsTruncated so the UI can say "more
	// drives exist, narrow by park" instead of lying by omission.
	//
	// projection-review: membership=obligation_batches rows for one tenant, park-scoped through the shed's parent location and restricted to batches carrying obligations; group_key=(batch_id, park_id, status, planned_date, window_start, window_end) -- park is part of the grain because an all-parks board must be able to tell two same-vaccine, same-window drives apart, and their counts must not be summed into one row; join_cardinality=batch_id is the obligation_batches primary key so the grouped set is exactly one row per batch with the other grouped columns functionally dependent on it, obligation_instances is the many side and is collapsed by COUNT(DISTINCT target_id), COUNT(DISTINCT obligation_id), array_agg(DISTINCT dose_code), and array_agg(DISTINCT shed name), while protocol_rules and locations are each 1:1 per obligation; pagination=bounded to one row per (batch, park) ordered by actionable execution status first and then newest executable day with a hard LIMIT of driveOptionsLimit fetched as limit+1, the surplus row discarded and reported as DriveOptionsTruncated so the bound can never drop a drive silently; scope=tenant plus optional park resolved from canonical locations.parent_location_id
	//
	// Producer unique columns: obligation_batches(batch_id). Consumer match/group columns:
	// (batch_id, park_id, status, planned_date, window_start, window_end). Row multiplicity: obligation_instances N:1 to
	// batch (pre-aggregated), protocol_rules 1:1 to obligation, locations 1:1 to obligation scope.
	// No ratio or cap check is computed, so there is no numerator/denominator key set to compare.
	driveOptionsSQL := `
SELECT
  b.batch_id,
  COALESCE(park.location_id::text, '') AS park_id,
  COALESCE(park.name, '') AS park_name,
  b.status,
  b.planned_date,
  b.window_start,
  b.window_end,
  array_agg(DISTINCT pr.dose_code) AS dose_codes,
  COUNT(DISTINCT oi.target_id)::int AS target_count,
  COUNT(DISTINCT oi.obligation_id)::int AS dose_count,
  COALESCE(operator_days.days, '[]'::jsonb) AS operator_days,
  COALESCE(array_agg(DISTINCT loc.name) FILTER (WHERE loc.name IS NOT NULL), ARRAY[]::text[]) AS shed_names,
  COALESCE(
    jsonb_agg(DISTINCT jsonb_build_object(
      'shedId', loc.location_id::text,
      'shedName', COALESCE(NULLIF(loc.name, ''), loc.location_code, ''),
      'partition_label', CASE
        WHEN lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
        ELSE btrim(gsp.partition_label)
      END
    )) FILTER (WHERE loc.location_id IS NOT NULL),
    '[]'::jsonb
  ) AS shed_locations
FROM obligation_batches b
JOIN obligation_instances oi ON oi.batch_id = b.batch_id AND oi.tenant_id = b.tenant_id
JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
LEFT JOIN locations loc ON oi.scope_id = loc.location_id AND oi.tenant_id = loc.tenant_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = oi.tenant_id AND gsp.goat_id = oi.target_id AND gsp.shed_id = oi.scope_id
LEFT JOIN locations park ON park.location_id = loc.parent_location_id AND park.tenant_id = loc.tenant_id
LEFT JOIN LATERAL (
  SELECT jsonb_agg(
    jsonb_build_object(
      'date', to_char(day_row.day, 'YYYY-MM-DD'),
      'targetCount', day_row.target_count,
      'doseCount', day_row.dose_count
    )
    ORDER BY day_row.day
  ) AS days
  FROM (
    SELECT
      (vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date AS day,
      COUNT(DISTINCT vc.goat_id)::int AS target_count,
      COUNT(DISTINCT vc.obligation_id)::int AS dose_count
    FROM vaccination_completions vc
    JOIN obligation_instances day_oi ON day_oi.obligation_id = vc.obligation_id AND day_oi.tenant_id = vc.tenant_id
    LEFT JOIN locations day_loc ON day_oi.scope_id = day_loc.location_id AND day_oi.tenant_id = day_loc.tenant_id
    WHERE vc.tenant_id = b.tenant_id
      AND vc.batch_id = b.batch_id
      AND (park.location_id IS NULL OR day_loc.parent_location_id = park.location_id)
    GROUP BY 1
  ) day_row
) operator_days ON true
WHERE b.tenant_id = $1::uuid
  AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR loc.parent_location_id = $2::uuid)
GROUP BY b.batch_id, park.location_id, park.name, b.status, b.planned_date, b.window_start, b.window_end, operator_days.days
ORDER BY
  CASE b.status
    WHEN 'in_progress' THEN 0
    WHEN 'completed' THEN 1
    ELSE 2
  END,
  b.planned_date DESC NULLS LAST,
  b.window_start DESC NULLS LAST,
  b.batch_id,
  park.name NULLS LAST
LIMIT $3
`
	driveOptionsLimit := r.driveOptionsLimit
	if driveOptionsLimit <= 0 {
		// A Repository built as a zero value (or by a future constructor that forgets the field)
		// must not degrade into LIMIT 0 and render an empty picker, which reads exactly like
		// "no drives are scheduled".
		driveOptionsLimit = defaultDriveOptionsLimit
	}
	driveRows, err := r.pool.Query(ctx, driveOptionsSQL, q.TenantID, parkID, driveOptionsLimit+1)
	if err != nil {
		return resp, fmt.Errorf("vaccination command board: drive options query: %w", err)
	}
	defer driveRows.Close()

	for driveRows.Next() {
		var batchID, parkOptionID, parkOptionName, status string
		var plannedDate pgtype.Date
		var windowStart, windowEnd pgtype.Timestamptz
		var doseCodes []string
		var targetCount, doseCount int
		var operatorDaysJSON []byte
		var shedLocationsJSON []byte
		var shedNames []string
		if err := driveRows.Scan(&batchID, &parkOptionID, &parkOptionName, &status, &plannedDate, &windowStart, &windowEnd, &doseCodes, &targetCount, &doseCount, &operatorDaysJSON, &shedNames, &shedLocationsJSON); err != nil {
			return resp, fmt.Errorf("vaccination command board: drive options scan: %w", err)
		}
		var operatorDays []domain.CommandBoardDriveDay
		if len(operatorDaysJSON) > 0 {
			if err := json.Unmarshal(operatorDaysJSON, &operatorDays); err != nil {
				return resp, fmt.Errorf("vaccination command board: drive option operator days: %w", err)
			}
		}
		var shedLocations []domain.CommandBoardDriveShedLocation
		if len(shedLocationsJSON) > 0 {
			if err := json.Unmarshal(shedLocationsJSON, &shedLocations); err != nil {
				return resp, fmt.Errorf("vaccination command board: drive option shed locations: %w", err)
			}
		}
		for i := range shedLocations {
			shedLocations[i].OperationalLocationDisplay = oploc.OperationalLocation{
				ShedName:       shedLocations[i].ShedName,
				PartitionLabel: shedLocations[i].PartitionLabel,
			}.Display()
		}
		shedIDSet := make(map[string]struct{}, len(shedLocations))
		shedIDs := make([]string, 0, len(shedLocations))
		for _, location := range shedLocations {
			if location.ShedID == "" {
				continue
			}
			if _, ok := shedIDSet[location.ShedID]; ok {
				continue
			}
			shedIDSet[location.ShedID] = struct{}{}
			shedIDs = append(shedIDs, location.ShedID)
		}
		sort.Strings(shedIDs)

		driveName := commandBoardDriveName(doseCodes)
		option := domain.CommandBoardDriveOption{
			DriveBatchID:  batchID,
			ParkID:        parkOptionID,
			ParkName:      parkOptionName,
			DriveName:     driveName,
			Status:        status,
			Label:         commandBoardDriveLabel(driveName, plannedDate, windowStart, status, targetCount),
			TargetCount:   targetCount,
			DoseCount:     doseCount,
			OperatorDays:  operatorDays,
			ShedNames:     shedNames,
			ShedIDs:       shedIDs,
			ShedLocations: shedLocations,
		}
		if plannedDate.Valid {
			planned := biztime.BusinessDayStart(plannedDate.Time)
			option.PlannedDate = &planned
		}
		if windowStart.Valid {
			option.WindowStart = &windowStart.Time
		}
		if windowEnd.Valid {
			option.WindowEnd = &windowEnd.Time
		}
		if len(resp.DriveOptions) >= driveOptionsLimit {
			// The limit+1'th row proves more drives exist. Stop before rendering it: it is a
			// probe, not a choice the caller may act on, and admitting it would put the list one
			// row over its own published bound.
			resp.DriveOptionsTruncated = true
			break
		}
		resp.DriveOptions = append(resp.DriveOptions, option)
	}
	if err := driveRows.Err(); err != nil {
		return resp, fmt.Errorf("vaccination command board: drive options rows: %w", err)
	}

	return resp, nil
}

// commandBoardDriveName renders the vaccine set without dose-rule noise. Initial/catch-up and
// repeat instructions may share one physical drive, so one FMD visit must not read as two drives.
func commandBoardDriveName(doseCodes []string) string {
	labels := make([]string, 0, len(doseCodes))
	seen := map[string]bool{}
	for _, code := range doseCodes {
		label := vaccinatdomain.DoseDisplayLabel("", code)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		labels = append(labels, label)
	}
	sort.Strings(labels)

	name := strings.Join(labels, " + ")
	if name == "" {
		name = "Drive"
	}
	return name
}

// commandBoardDriveLabel identifies one executable operator day. Two whole-shed batches can
// share the same medical window, so a window-only selector made separate days look duplicated.
func commandBoardDriveLabel(name string, plannedDate pgtype.Date, windowStart pgtype.Timestamptz, status string, targetCount int) string {
	if plannedDate.Valid {
		name = fmt.Sprintf("%s — %s", name, plannedDate.Time.Format(time.DateOnly))
	} else if windowStart.Valid {
		name = fmt.Sprintf("%s — %s", name, biztime.BusinessDate(windowStart.Time))
	}
	if targetCount > 0 {
		name = fmt.Sprintf("%s · %d animals", name, targetCount)
	}
	if status != "" {
		name = fmt.Sprintf("%s (%s)", name, status)
	}
	return name
}

// closureReasonLabel renders an obligation's closed status in farm language. Raw status tokens are
// storage vocabulary and must never reach a CEO screen; an unmapped status degrades to the generic
// business phrasing rather than leaking the token.
func closureReasonLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "cancelled", "canceled":
		return "Cancelled"
	case "waived":
		return "Waived"
	case "superseded":
		return "Superseded"
	case "deferred":
		return "Deferred for recovery"
	case "":
		return "Closed with no dose"
	default:
		return "Closed with no dose"
	}
}

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

// visitShotLockNamespace prefixes every per-visit advisory-lock key so it cannot collide with
// other advisory-lock users in the database (e.g. CreateBatchWithObligations' own scope lock, or
// platform/worker's kernel-stage lock).
const visitShotLockNamespace = "goatos:obligation:visit-shot:"

// driveCapacityLockNamespace serializes the whole park/date vaccination drive capacity ledger.
// The unit is eligible animal slots for one tenant+park+planned_date, across all sheds, vaccines,
// operators, and planner paths.
const driveCapacityLockNamespace = "goatos:obligation:drive-capacity:"

// canonicalUUID parses id and re-renders it in Postgres's canonical lowercase-hex form (RV-03).
// hashtext() -- used by every advisory-lock key in this file -- hashes the RAW BYTES of its input
// text argument, so two textually different representations of the SAME Postgres uuid value (e.g.
// an uppercase-hex tenant id one caller passes vs the lowercase form another caller passes for the
// identical tenant) hash to two DIFFERENT lock ids unless every caller first normalizes to one
// canonical string form. Without this, two sweepers racing the same tenant/visit under
// differently-cased UUID strings would each acquire what they believe is "the" lock for that
// tenant/visit and run concurrently, defeating the single-writer guarantee LockTenantSweep and
// LockVisitShots exist to provide.
func canonicalUUID(id string) (string, error) {
	u, err := pgconv.UUID(id)
	if err != nil {
		return "", fmt.Errorf("invalid uuid %q: %w", id, err)
	}
	canon := pgconv.UUIDString(u)
	if canon == "" {
		return "", fmt.Errorf("empty or nil uuid %q", id)
	}
	return canon, nil
}

func (r *Repository) UpsertVaccinationDriveAssignments(ctx context.Context, tenantID string, assignments []domain.DriveAssignment) error {
	if len(assignments) == 0 {
		return nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("obligation: tenant id: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("obligation: begin drive assignment tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := upsertVaccinationDriveAssignmentsTx(ctx, tx, tenant, assignments); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("obligation: commit drive assignments: %w", err)
	}
	committed = true
	return nil
}

// ReplaceVaccinationDriveAssignmentsForBatch persists the FINAL, attached-only drive-assignment
// row set for one (tenant, batch) atomically: delete every existing row for that batch, then
// insert exactly the supplied set, all inside one transaction. This is the F2 fix's persistence
// half -- an upsert alone can only add/update keys present in the new set, so a row for an
// obligation that was selected but did not actually attach (a shed/partition/operator key absent
// from the new set) would survive forever. Replace removes it. Idempotent/replay-safe: calling it
// twice with the identical assignment set leaves the same rows in place.
func (r *Repository) ReplaceVaccinationDriveAssignmentsForBatch(ctx context.Context, tenantID, batchID string, assignments []domain.DriveAssignment) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("obligation: tenant id: %w", err)
	}
	batch, err := pgconv.UUID(batchID)
	if err != nil {
		return fmt.Errorf("obligation: batch id: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("obligation: begin drive assignment replace tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, `
DELETE FROM vaccination_drive_assignments
WHERE tenant_id = $1 AND batch_id = $2`, tenant, batch); err != nil {
		return fmt.Errorf("obligation: delete stale drive assignments: %w", err)
	}
	if len(assignments) > 0 {
		if err := upsertVaccinationDriveAssignmentsTx(ctx, tx, tenant, assignments); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("obligation: commit drive assignment replace: %w", err)
	}
	committed = true
	return nil
}

func upsertVaccinationDriveAssignmentsTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, assignments []domain.DriveAssignment) error {
	batchIDs := make([]pgtype.UUID, 0, len(assignments))
	plannedDates := make([]time.Time, 0, len(assignments))
	operatorIDs := make([]pgtype.UUID, 0, len(assignments))
	parkIDs := make([]pgtype.UUID, 0, len(assignments))
	shedIDs := make([]pgtype.UUID, 0, len(assignments))
	physicalSheds := make([]string, 0, len(assignments))
	partitions := make([]string, 0, len(assignments))
	animalCounts := make([]int32, 0, len(assignments))
	vaccineRuleIDsJSON := make([]string, 0, len(assignments))
	totalDoses := make([]int32, 0, len(assignments))
	statuses := make([]string, 0, len(assignments))
	warningsJSON := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		batchID, err := pgconv.UUID(assignment.BatchID)
		if err != nil {
			return fmt.Errorf("obligation: drive assignment batch id: %w", err)
		}
		parkID, err := pgconv.UUID(assignment.ParkID)
		if err != nil {
			return fmt.Errorf("obligation: drive assignment park id: %w", err)
		}
		shedID := pgtype.UUID{}
		if assignment.ShedID != nil && strings.TrimSpace(*assignment.ShedID) != "" {
			shedID, err = pgconv.UUID(*assignment.ShedID)
			if err != nil {
				return fmt.Errorf("obligation: drive assignment shed id: %w", err)
			}
		}
		operatorID := pgtype.UUID{}
		if assignment.OperatorID != nil && strings.TrimSpace(*assignment.OperatorID) != "" {
			operatorID, err = pgconv.UUID(*assignment.OperatorID)
			if err != nil {
				return fmt.Errorf("obligation: drive assignment operator id: %w", err)
			}
		}
		ruleIDs := make([]string, 0, len(assignment.VaccineRuleIDs))
		for _, rawRuleID := range assignment.VaccineRuleIDs {
			rawRuleID = strings.TrimSpace(rawRuleID)
			if rawRuleID == "" {
				continue
			}
			if _, err := pgconv.UUID(rawRuleID); err != nil {
				return fmt.Errorf("obligation: drive assignment vaccine rule id: %w", err)
			}
			ruleIDs = append(ruleIDs, rawRuleID)
		}
		ruleIDsJSONBytes, err := json.Marshal(ruleIDs)
		if err != nil {
			return fmt.Errorf("obligation: drive assignment vaccine rule ids: %w", err)
		}
		warnings := assignment.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		warningsJSONBytes, err := json.Marshal(warnings)
		if err != nil {
			return fmt.Errorf("obligation: drive assignment warnings: %w", err)
		}
		status := strings.TrimSpace(assignment.CapacityStatus)
		if status == "" {
			status = "within_cap"
		}
		physicalShed := strings.TrimSpace(assignment.PhysicalShed)
		if physicalShed == "" {
			physicalShed = "park"
		}
		partition := strings.TrimSpace(assignment.PartitionLabel)
		if partition == "" {
			partition = "whole"
		}
		batchIDs = append(batchIDs, batchID)
		plannedDates = append(plannedDates, biztime.BusinessDayStart(assignment.PlannedDate))
		operatorIDs = append(operatorIDs, operatorID)
		parkIDs = append(parkIDs, parkID)
		shedIDs = append(shedIDs, shedID)
		physicalSheds = append(physicalSheds, physicalShed)
		partitions = append(partitions, partition)
		animalCounts = append(animalCounts, assignment.AnimalCount)
		vaccineRuleIDsJSON = append(vaccineRuleIDsJSON, string(ruleIDsJSONBytes))
		totalDoses = append(totalDoses, assignment.TotalDoses)
		statuses = append(statuses, status)
		warningsJSON = append(warningsJSON, string(warningsJSONBytes))
	}
	if _, err := tx.Exec(ctx, `
	-- projection-review: membership=one generated DriveAssignment row per batch/date/operator/park/shed/physical_shed/partition; group_key=conflict key (tenant_id,batch_id,planned_date,park_id,shed_id,physical_shed,partition_label,operator_id); join_cardinality=no joins, UNNEST arrays are positional one-to-one inputs from the planner; pagination=single generated batch write, no paging or truncation; scope=explicit assignment park_id/shed_id.
INSERT INTO vaccination_drive_assignments (
  tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
  physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses, capacity_status, warnings
)
SELECT
  $1,
  u.batch_id,
  u.planned_date,
  u.operator_id,
  u.park_id,
  u.shed_id,
  u.physical_shed,
  u.partition_label,
  u.animal_count,
  COALESCE((
    SELECT array_agg(rule_id::uuid ORDER BY rule_id)
    FROM jsonb_array_elements_text(u.vaccine_rule_ids_json::jsonb) AS rule_ids(rule_id)
  ), '{}'::uuid[]),
  u.total_doses,
  u.capacity_status,
  u.warnings::jsonb
FROM unnest(
  $2::uuid[],
  $3::date[],
  $4::uuid[],
  $5::uuid[],
  $6::uuid[],
  $7::text[],
  $8::text[],
  $9::int[],
  $10::text[],
  $11::int[],
  $12::text[],
  $13::text[]
) AS u(batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids_json, total_doses, capacity_status, warnings)
	ON CONFLICT (
	  tenant_id,
	  batch_id,
	  planned_date,
	  park_id,
	  COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
	  physical_shed,
	  partition_label,
	  COALESCE(operator_id, '00000000-0000-0000-0000-000000000000'::uuid)
	)
	DO UPDATE SET
  operator_id = EXCLUDED.operator_id,
  animal_count = EXCLUDED.animal_count,
  vaccine_rule_ids = EXCLUDED.vaccine_rule_ids,
  total_doses = EXCLUDED.total_doses,
  capacity_status = EXCLUDED.capacity_status,
  warnings = EXCLUDED.warnings,
  updated_at = now()`,
		tenant, batchIDs, plannedDates, operatorIDs, parkIDs, shedIDs, physicalSheds, partitions, animalCounts, vaccineRuleIDsJSON, totalDoses, statuses, warningsJSON); err != nil {
		return fmt.Errorf("obligation: upsert drive assignments: %w", err)
	}
	return nil
}

// visitShotLockKeyArg is the stable advisory-lock key argument for one (tenant, target, date)
// visit. tenantID and targetID are canonicalized (RV-03) before being folded into the key, so any
// two processes (two sweep passes, two concurrent worker replicas) compute the identical Postgres
// advisory-lock key for the same animal's visit and therefore mutually exclude regardless of
// process identity, call order, OR the textual case/representation of the UUIDs each caller holds.
func visitShotLockKeyArg(tenantID, targetID, dateKey string) (string, error) {
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("obligation: visit-shot lock tenant id: %w", err)
	}
	targetCanon, err := canonicalUUID(targetID)
	if err != nil {
		return "", fmt.Errorf("obligation: visit-shot lock target id: %w", err)
	}
	return visitShotLockNamespace + tenantCanon + ":" + targetCanon + ":" + dateKey, nil
}

func driveCapacityLockKeyArg(tenantID, parkID, dateKey string) (string, error) {
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("obligation: drive-capacity lock tenant id: %w", err)
	}
	parkCanon, err := canonicalUUID(parkID)
	if err != nil {
		return "", fmt.Errorf("obligation: drive-capacity lock park id: %w", err)
	}
	return driveCapacityLockNamespace + tenantCanon + ":" + parkCanon + ":" + dateKey, nil
}

// pgxQuerier is satisfied by both *pgxpool.Pool and *pgxpool.Conn, letting countVisitShots run
// identically whether or not the caller already holds a dedicated locked connection.
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// countVisitShots reads the persisted, already-committed shot count per target for one planned
// visit date: obligations attached (via batch_id) to a NON-canceled, non-superseded batch whose
// planned_date equals date. Bounded by the caller's explicit targetIDs (never a full-table scan),
// so it stays scale-safe at 1-5M animals regardless of how many total obligations/batches exist
// for the tenant.
func countVisitShots(ctx context.Context, q pgxQuerier, tenantID string, targetIDs []string, date time.Time) (map[string]int32, error) {
	out := make(map[string]int32, len(targetIDs))
	if len(targetIDs) == 0 {
		return out, nil
	}
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	ids, err := obligationUUIDs(targetIDs)
	if err != nil {
		return nil, fmt.Errorf("obligation: visit shot target ids: %w", err)
	}
	day := biztime.BusinessDayStart(date)
	// projection-review: membership=obligation_instances attached via oi.batch_id to obligation_batches (same tenant); group_key=oi.target_id for a single planned_date; join_cardinality=1:1 (each oi carries one batch_id and obligation_batches is keyed by (tenant_id, batch_id), so the JOIN is a semijoin to the batch's planned_date/status and COUNT(*) counts obligation rows, never a batch fan-out); pagination=bounded by the caller's explicit target_ids + one planned_date, computed whole (no user page — this is the cap-enforcement count, not a paged UI projection); scope=n/a (explicit target-id list, not a park/shed/cohort hierarchy)
	rows, err := q.Query(ctx, `
SELECT oi.target_id::text, count(*)::int
FROM obligation_instances oi
JOIN obligation_batches ob
  ON ob.tenant_id = oi.tenant_id
 AND ob.batch_id = oi.batch_id
WHERE oi.tenant_id = $1
  AND oi.target_id = ANY($2::uuid[])
  AND ob.planned_date = $3::date
  AND ob.status NOT IN ('canceled', 'superseded')
  AND oi.status NOT IN ('canceled')
GROUP BY oi.target_id`, tenant, ids, pgconv.Date(&day))
	if err != nil {
		return nil, fmt.Errorf("obligation: count visit shots: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var targetID string
		var count int32
		if err := rows.Scan(&targetID, &count); err != nil {
			return nil, fmt.Errorf("obligation: scan visit shot count: %w", err)
		}
		out[targetID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: visit shot count rows: %w", err)
	}
	return out, nil
}

// CountVisitShotsForTargets is the read-only half of the VAX-REV-01 fix (see
// app.visitShotCounter): the persisted shot count already committed for each of targetIDs on one
// planned visit date, from ANY prior sweep pass. Used to seed a fresh SweepSession so
// MaxShotsPerAnimalPerDrive is enforced across passes, not just within one. Safe to call without
// any lock -- callers that also need write-time atomicity against concurrent workers use
// LockVisitShots instead.
func (r *Repository) CountVisitShotsForTargets(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (map[string]int32, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return countVisitShots(ctx, r.pool, tenantID, targetIDs, date)
}

// LockVisitShots is the write-path half of the VAX-REV-01 fix (see app.visitShotLocker). It takes
// a session-level Postgres advisory lock (pg_advisory_lock, hashtext(visitShotLockKeyArg(...)))
// for every distinct target in targetIDs, in ascending sorted order (a stable order shared by
// every caller, so two workers racing over an overlapping target set always attempt their locks
// in the same order and cannot deadlock each other), on ONE dedicated connection acquired from the
// pool. It then reads the fresh persisted shot count for those now-locked targets and returns it
// alongside a release func.
//
// The lock is held on the dedicated connection until the caller invokes release, NOT scoped to
// any one transaction -- the caller's subsequent CreateBatchWithObligations call runs in its own,
// separate transaction/connection, and the advisory lock (a global, connection-independent
// Postgres primitive) is what actually serializes a concurrent caller attempting the very same
// (tenant, target, date) key, regardless of which connection eventually performs the write. A
// second caller's own LockVisitShots call for an overlapping key blocks on pg_advisory_lock until
// this caller's release() runs, so it always observes the first caller's committed write in its
// own fresh count read.
// projection-review: membership=explicit targetIDs for one tenant/date lock+count request; group_key=target_id in countVisitShots; join_cardinality=lock keys are deduplicated in memory and countVisitShots semijoins each obligation to one batch; pagination=bounded caller-provided target list, no page-local aggregate; scope=explicit target-id list.
func (r *Repository) LockVisitShots(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (map[string]int32, func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	keys := dedupNonBlank(targetIDs)
	if len(keys) == 0 {
		return map[string]int32{}, noop, nil
	}
	sort.Strings(keys)
	dateKey := biztime.BusinessDayStart(date).Format("2006-01-02")

	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("obligation: acquire visit-shot lock connection: %w", err)
	}

	lockArgs := make([]string, 0, len(keys))
	for _, targetID := range keys {
		key, keyErr := visitShotLockKeyArg(tenantID, targetID, dateKey)
		if keyErr != nil {
			conn.Release()
			return nil, nil, fmt.Errorf("obligation: build visit-shot lock key: %w", keyErr)
		}
		lockArgs = append(lockArgs, key)
	}
	// Acquire every per-visit advisory lock in ONE round trip. unnest preserves array element order
	// and lockArgs is built from the already-sorted key set, so every caller acquires any shared
	// locks in the same global order and cannot deadlock. pg_advisory_lock is parallel-unsafe, so the
	// function scan runs sequentially in array order -- this is the batched, single-statement
	// equivalent of a per-target lock loop, without the per-target round trips.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext(k)) FROM unnest($1::text[]) AS t(k)", lockArgs); err != nil {
		_ = releaseVisitShotConn(context.Background(), conn)
		return nil, nil, fmt.Errorf("obligation: lock visit shots: %w", err)
	}

	release := func(releaseCtx context.Context) error {
		return releaseVisitShotConn(releaseCtx, conn)
	}

	counts, err := countVisitShots(ctx, conn, tenantID, keys, date)
	if err != nil {
		_ = release(context.Background())
		return nil, nil, err
	}
	return counts, release, nil
}

func countDriveCellsForParkDate(ctx context.Context, q pgxQuerier, tenantID, parkID string, date time.Time) (int32, error) {
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	park, err := pgconv.UUID(parkID)
	if err != nil {
		return 0, fmt.Errorf("obligation: drive capacity park id: %w", err)
	}
	day := biztime.BusinessDayStart(date)
	rows, err := q.Query(ctx, `
WITH drive_animals AS (
  -- projection-review: membership=scheduled obligation_instances attached to non-canceled batches on one planned_date, collapsed to DISTINCT target_id before counting; group_key=target_id for one tenant+park+date animal-cap ledger; join_cardinality=obligation_batches is keyed by (tenant_id,batch_id), goats/location are 1:1 lookup dimensions, and DISTINCT target_id prevents multi-vaccine obligation rows from consuming extra operator slots; pagination=full single park/date ledger count, no UI page or LIMIT can truncate capacity truth; scope=park scope accepts direct park batches, shed children, and goat park fallback under the explicit park_id.
  SELECT DISTINCT oi.target_id
  FROM obligation_instances oi
  JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
  LEFT JOIN locations scope_loc
    ON scope_loc.tenant_id = ob.tenant_id
   AND scope_loc.location_id = ob.scope_id
  WHERE oi.tenant_id = $1
    AND ob.planned_date = $3::date
    AND ob.status NOT IN ('canceled', 'superseded')
    AND oi.status NOT IN ('canceled')
    AND (
      (ob.scope_type = 'park' AND ob.scope_id = $2)
      OR (ob.scope_type = 'shed' AND scope_loc.parent_location_id = $2)
      OR g.park_id = $2
    )
)
SELECT COALESCE(count(*), 0)::int FROM drive_animals`, tenant, park, pgconv.Date(&day))
	if err != nil {
		return 0, fmt.Errorf("obligation: count drive animals: %w", err)
	}
	defer rows.Close()
	var count int32
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("obligation: scan drive animal count: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("obligation: drive animal count rows: %w", err)
	}
	return count, nil
}

// CountDriveCellsForParkDate returns persisted planned animal slots for one park/date drive, across all
// shed/park batch rows. The historical method name is kept for interface compatibility; stock/proof dose
// cells remain in planned_quantity and are not the operator-capacity unit.
func (r *Repository) CountDriveCellsForParkDate(ctx context.Context, tenantID, parkID string, date time.Time) (int32, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return countDriveCellsForParkDate(ctx, r.pool, tenantID, parkID, date)
}

// LockDriveCapacity serializes capacity planning for one tenant/park/date. The caller must hold
// the returned release until its batch attach/update is committed or abandoned.
func (r *Repository) LockDriveCapacity(ctx context.Context, tenantID, parkID string, date time.Time) (int32, func(context.Context) error, error) {
	dateKey := biztime.BusinessDayStart(date).Format("2006-01-02")
	lockArg, err := driveCapacityLockKeyArg(tenantID, parkID, dateKey)
	if err != nil {
		return 0, nil, err
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("obligation: acquire drive-capacity lock connection: %w", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", lockArg); err != nil {
		_ = releaseVisitShotConn(context.Background(), conn)
		return 0, nil, fmt.Errorf("obligation: lock drive capacity: %w", err)
	}
	release := func(releaseCtx context.Context) error {
		return releaseVisitShotConn(releaseCtx, conn)
	}
	count, err := countDriveCellsForParkDate(ctx, conn, tenantID, parkID, date)
	if err != nil {
		_ = release(context.Background())
		return 0, nil, err
	}
	return count, release, nil
}

// PickVaccinationOperatorForDrive returns the currently least-loaded active vaccination operator for
// the park/date. The planner uses the returned member as the durable batch conducted_by assignment;
// when every available operator is already at cap, the least-loaded operator is still returned so
// last-safe work is recorded as over-cap/overtime instead of being silently pushed later.
func (r *Repository) PickVaccinationOperatorForDrive(ctx context.Context, tenantID, parkID string, date time.Time, capPerOperator int32) (*string, error) {
	operators, err := r.AvailableVaccinationOperatorsForDrive(ctx, tenantID, parkID, date, capPerOperator)
	if err != nil {
		return nil, err
	}
	if len(operators) == 0 {
		return nil, nil
	}
	operatorID := operators[0].OperatorID
	return &operatorID, nil
}

func (r *Repository) AvailableVaccinationOperatorsForDrive(ctx context.Context, tenantID, parkID string, date time.Time, capPerOperator int32) ([]domain.DriveOperatorCapacity, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	park, err := pgconv.UUID(parkID)
	if err != nil {
		return nil, fmt.Errorf("obligation: park id: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
WITH capacity_config AS (
  SELECT COALESCE((SELECT max_per_day FROM vaccination_capacity_config WHERE tenant_id = $1), NULLIF($4::int, 0), 200)::int AS default_cap
),
candidate AS (
  SELECT wm.workforce_member_id, wm.updated_at,
         COALESCE(MAX(wp.vaccination_daily_animal_cap), (SELECT default_cap FROM capacity_config))::int AS daily_cap
  FROM workforce_members wm
  JOIN locations park_loc
    ON park_loc.tenant_id = $1
   AND park_loc.location_id = $2
   AND park_loc.status = 'active'
  JOIN workforce_positions wp
    ON wp.tenant_id = wm.tenant_id
   AND wp.workforce_member_id = wm.workforce_member_id
   AND wp.status = 'active'
   AND wp.valid_from <= $3::date + interval '1 day'
   AND (wp.valid_to IS NULL OR wp.valid_to > $3::date)
   AND wp.position_tier <> 'director'
   AND (
     (wp.scope_type = 'center' AND wp.scope_id IN ($2, park_loc.parent_location_id))
     OR (wp.scope_type = 'shed' AND wp.scope_id IN (
       SELECT location_id FROM locations WHERE tenant_id = $1 AND parent_location_id = $2 AND location_type = 'shed' AND status = 'active'
     ))
   )
  JOIN position_module_duties pmd
    ON pmd.tenant_id = wp.tenant_id
   AND pmd.position_code = wp.position_code
   AND pmd.status = 'active'
   AND pmd.effective_from <= $3::date + interval '1 day'
   AND (pmd.effective_to IS NULL OR pmd.effective_to > $3::date)
   AND pmd.duty_type = 'execute'
   AND pmd.module_code IN ('preventive_care', 'vaccination', 'pc.vaccination')
  WHERE wm.tenant_id = $1
    AND wm.status = 'active'
    AND NOT EXISTS (
      SELECT 1
      FROM workforce_absences wa
      WHERE wa.tenant_id = wm.tenant_id
        AND wa.workforce_member_id = wm.workforce_member_id
        AND wa.status IN ('approved', 'escalation_required')
        AND wa.starts_at < $3::date + interval '1 day'
        AND wa.ends_at > $3::date
    )
    AND NOT EXISTS (
      SELECT 1
      FROM workforce_positions offpos
      WHERE offpos.tenant_id = wm.tenant_id
        AND offpos.workforce_member_id = wm.workforce_member_id
        AND offpos.status = 'active'
        AND offpos.valid_from <= $3::date + interval '1 day'
        AND (offpos.valid_to IS NULL OR offpos.valid_to > $3::date)
        AND offpos.week_off_weekday = lower(to_char($3::date, 'FMDay'))
    )
  GROUP BY wm.workforce_member_id, wm.updated_at
), load AS (
  -- projection-review: membership=active planned/in-progress batches with conducted_by workforce_member_id on one park/planned_date; group_key=conducted_by workforce_member_id; join_cardinality=OneToMany (obligation_batches:obligation_instances=1:N collapsed with COUNT(DISTINCT oi.target_id) so multi-vaccine rows do not inflate operator animal load); pagination=Pagination (full available-operator candidate set for one park/date, no keyset paging); scope=ParkScope (explicit park scope only); date=ExecutionDate (planned_date); status=StatusMatrix (scheduled,due,in_progress statuses counted, others excluded).
  SELECT ob.conducted_by AS workforce_member_id, count(DISTINCT oi.target_id)::int AS animals
  FROM obligation_batches ob
  JOIN obligation_instances oi
    ON oi.tenant_id = ob.tenant_id
   AND oi.batch_id = ob.batch_id
  WHERE ob.tenant_id = $1
    AND ob.scope_type = 'park'
    AND ob.scope_id = $2
    AND ob.planned_date = $3::date
    AND ob.status IN ('planned', 'in_progress')
    AND ob.conducted_by IS NOT NULL
    AND oi.status IN ('scheduled', 'due', 'in_progress')
  GROUP BY ob.conducted_by
)
SELECT c.workforce_member_id::text, GREATEST(c.daily_cap - COALESCE(l.animals, 0), 0)::int
FROM candidate c
LEFT JOIN load l ON l.workforce_member_id = c.workforce_member_id
ORDER BY
  CASE WHEN c.daily_cap <= 0 THEN 0 WHEN COALESCE(l.animals, 0) < c.daily_cap THEN 0 ELSE 1 END,
  COALESCE(l.animals, 0) ASC,
  c.updated_at ASC NULLS FIRST,
  c.workforce_member_id ASC`, tenant, park, biztime.BusinessDayStart(date), capPerOperator)
	if err != nil {
		return nil, fmt.Errorf("obligation: list vaccination operators: %w", err)
	}
	defer rows.Close()
	out := make([]domain.DriveOperatorCapacity, 0)
	for rows.Next() {
		var operator domain.DriveOperatorCapacity
		if err := rows.Scan(&operator.OperatorID, &operator.Cap); err != nil {
			return nil, fmt.Errorf("obligation: scan vaccination operator: %w", err)
		}
		out = append(out, operator)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: vaccination operator rows: %w", err)
	}
	return out, nil
}

// tenantSweepLockNamespace prefixes the single per-tenant whole-sweep advisory-lock key so it
// cannot collide with the per-visit locks or any other advisory-lock user.
const tenantSweepLockNamespace = "goatos:obligation:tenant-sweep:"

// LockTenantSweep takes ONE tenant-scoped session advisory lock that serializes the ENTIRE
// obligation sweep (preflight + every version) for a tenant across processes (RV-03). It uses
// pg_try_advisory_lock (non-blocking): if another sweeper already holds it -- a second kernel-worker
// replica, or the standalone cmd/obligation-sweeper running alongside the kernel stage -- acquired
// is false and the caller SKIPS this run, since the in-progress writer already covers the tenant.
// When acquired, this sweep is the single priority-ordered writer for the tenant, so its in-memory
// priority arbitration (SortSweepVersionsByPriority + SweepSession.visitClaims) is authoritative and
// no concurrent lower-priority writer can commit a competing shot mid-sweep and invert the medical
// plan by lock-acquisition order. The caller MUST call the returned release exactly once.
func (r *Repository) LockTenantSweep(ctx context.Context, tenantID string) (bool, func(context.Context) error, error) {
	// RV-03: canonicalize tenantID BEFORE folding it into the lock key. Without this, an uppercase-
	// and a lowercase-hex string for the SAME tenant uuid hash to two different advisory-lock ids
	// (hashtext hashes raw text bytes, not the parsed uuid value), so two sweepers for the same
	// tenant could both observe pg_try_advisory_lock == true and run concurrently -- exactly the
	// single-writer race this lock exists to prevent.
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return false, nil, fmt.Errorf("obligation: tenant-sweep lock tenant id: %w", err)
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("obligation: acquire tenant-sweep lock connection: %w", err)
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", tenantSweepLockNamespace+tenantCanon).Scan(&acquired); err != nil {
		conn.Release()
		return false, nil, fmt.Errorf("obligation: try tenant-sweep lock: %w", err)
	}
	if !acquired {
		// No advisory lock is held on this connection, so a plain Release (no unlock) is correct.
		conn.Release()
		return false, func(context.Context) error { return nil }, nil
	}
	return true, func(releaseCtx context.Context) error { return releaseVisitShotConn(releaseCtx, conn) }, nil
}

// visitShotUnlockTimeout bounds the independent cleanup context releaseVisitShotConn uses so a
// canceled caller context can never skip pg_advisory_unlock_all and leak session locks (RV-05).
const visitShotUnlockTimeout = 5 * time.Second

// releaseVisitShotConn releases every session advisory lock this dedicated connection holds in ONE
// round trip (pg_advisory_unlock_all frees all locks the session owns) and returns the connection
// to the pool. Using unlock_all rather than a per-key unlock loop keeps release a single call and
// cannot leak a lock on the pooled connection regardless of how large the key set was.
//
// RV-05: the unlock runs on an independent, bounded context derived with context.WithoutCancel, so
// a caller whose context is already canceled (a shut-down sweep, a timed-out request) still frees
// its session locks instead of silently skipping the unlock. If the unlock nonetheless fails, the
// connection may still own session advisory locks; returning it to the pool would leave an
// invisible lock that blocks every later worker on that visit key forever, so it is DESTROYED
// (hijacked out of the pool and closed) rather than released back.
func releaseVisitShotConn(ctx context.Context, conn *pgxpool.Conn) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), visitShotUnlockTimeout)
	defer cancel()
	if _, err := conn.Exec(cleanupCtx, "SELECT pg_advisory_unlock_all()"); err != nil {
		hijacked := conn.Hijack()
		_ = hijacked.Close(cleanupCtx)
		return fmt.Errorf("obligation: unlock visit shots (connection destroyed to avoid leaking session locks): %w", err)
	}
	conn.Release()
	return nil
}

func dedupNonBlank(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

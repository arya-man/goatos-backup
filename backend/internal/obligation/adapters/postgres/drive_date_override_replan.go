package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
)

// A vaccine drive-date override MOVES already-planned work onto a different calendar day. The
// operator who was planned for the source day is not authority on the target day: that operator
// may be on week-off/leave there, may not be one of the N operators the park's assignment config
// resolves for that weekday, and the target day may already be loaded to (or past) its remaining
// animal capacity by other vaccines. Copying operator_id / animal_count / capacity_status verbatim
// therefore books phantom capacity and reports within_cap for a day that is over cap.
//
// The move (and the symmetric restore when an override is cleared) instead RE-PLANS the moved rows
// against the target date's real capacity, using the same OperatorDrivePlanner the sweeper uses, so
// the drive plan a date move produces is the plan the sweeper would have produced for that day.
//
// SCALE: every statement here is bounded by ONE (tenant, park, planned_date) slice of
// vaccination_drive_assignments -- a drive-plan table whose grain is batch x shed x partition x
// operator for a single day, not a herd-wide scan.

// movedDriveAssignment is one source-date assignment row carrying at least one rule of the moved
// vaccine, split into the rule ids that move and the rule ids that stay behind.
type movedDriveAssignment struct {
	assignmentID   string
	batchID        string
	parkID         string
	shedID         *string
	physicalShed   string
	partitionLabel string
	// animalCount is the CELL-wide count (every vaccine in the row). It is only a legacy fallback
	// for rows with no membership ledger; the lane-scoped counters below are the sizing truth.
	animalCount      int32
	movedRuleIDs     []string
	remainingRuleIDs []string
	// Per-lane counters derived from the row's OWN per-goat membership ledger. A date move splits
	// the cell along the VACCINE axis, so each side must be sized over that lane's animals only.
	movedAnimalCount     int32
	movedDoseCount       int32
	remainingAnimalCount int32
	remainingDoseCount   int32
	// ledgerAnimalCount is the DISTINCT goats the ledger holds for the whole row. The per-lane
	// counters are only usable when it accounts for the row completely (see ledgerCoversRow).
	ledgerAnimalCount int32
	hasLedger         bool
}

// plannedDriveAssignment is one row to write on the target date after re-planning.
type plannedDriveAssignment struct {
	plannedDate    time.Time
	batchID        string
	parkID         string
	shedID         *string
	physicalShed   string
	partitionLabel string
	operatorID     *string
	animalCount    int32
	totalDoses     int32
	ruleIDs        []string
	capacityStatus string
	warnings       []string
}

// vaccinationOperatorCapacityForDate resolves the executable operators for one park/date with their
// REMAINING animal capacity (daily cap minus already-committed batch load), applying week-off,
// leave, and the park's operator-assignment config exactly like the planner path does. A config
// that is present but resolves to no executable operator fails closed to "no operator", which makes
// the moved work land as capacity_action for a human instead of silently keeping a stale operator.
func (r *Repository) vaccinationOperatorCapacityForDate(ctx context.Context, tenantID, parkID string, date time.Time) ([]vaccexecapp.DriveOperator, error) {
	operators, err := r.AvailableVaccinationOperatorsForDrive(ctx, tenantID, parkID, date, 0)
	if err != nil {
		if errors.Is(err, domain.ErrOperatorAssignmentConfigPresentButEmpty) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]vaccexecapp.DriveOperator, 0, len(operators))
	for _, operator := range operators {
		operatorID := strings.TrimSpace(operator.OperatorID)
		if operatorID == "" {
			continue
		}
		out = append(out, vaccexecapp.DriveOperator{
			ID:            operatorID,
			Name:          operatorID,
			Cap:           int(operator.Cap),
			ConfiguredCap: int(operator.ConfiguredCap),
			Available:     operator.Cap > 0,
		})
	}
	return out, nil
}

func (r *Repository) vaccinationOperatorAvailabilityForDateRange(ctx context.Context, tenantID, parkID string, start, end time.Time) ([]vaccexecapp.DriveDateAvailability, error) {
	start = businessDateOnly(start)
	end = businessDateOnly(end)
	if end.Before(start) {
		end = start
	}
	out := make([]vaccexecapp.DriveDateAvailability, 0, int(end.Sub(start).Hours()/24)+1)
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		operators, err := r.vaccinationOperatorCapacityForDate(ctx, tenantID, parkID, date)
		if err != nil {
			return nil, err
		}
		out = append(out, vaccexecapp.DriveDateAvailability{Date: date, Operators: operators})
	}
	return out, nil
}

// replanVaccinationDriveAssignmentsForDateMoveTx detaches the moved vaccine's membership from the
// rows on `from` and re-plans that work onto `to` under `to`'s real operator capacity.
func replanVaccinationDriveAssignmentsForDateMoveTx(
	ctx context.Context,
	tx pgx.Tx,
	tenant, park pgtype.UUID,
	vaccineCode string,
	from, to time.Time,
	availability []vaccexecapp.DriveDateAvailability,
) error {
	rows, err := selectMovedDriveAssignmentsTx(ctx, tx, tenant, park, vaccineCode, from, to)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if err := detachMovedDriveAssignmentsTx(ctx, tx, tenant, rows); err != nil {
		return err
	}
	// Read the already-moved-in load AFTER detaching, so a second move of the same vaccine does not
	// count its own rows as pre-existing target-date load.
	movedInLoad, err := movedInDriveOperatorLoadByDateTx(ctx, tx, tenant, park, driveAvailabilityDates(availability))
	if err != nil {
		return err
	}
	planned := planMovedDriveAssignments(rows, availabilityAfterMovedInLoad(availability, movedInLoad), to)
	if err := insertPlannedDriveAssignmentsTx(ctx, tx, tenant, planned); err != nil {
		return err
	}
	// A date move re-splits the SAME work across two calendar days (some rules stay on `from`, the
	// moved rules land on `to`), so per-goat membership for every touched batch must be recomputed in
	// this same transaction. Without it the two dates carry counts only, and no downstream action can
	// say which exact goat is vaccinated on which day by which operator.
	batchIDs, err := movedDriveAssignmentBatchUUIDs(rows)
	if err != nil {
		return err
	}
	if err := syncVaccinationDriveAssignmentMembersTx(ctx, tx, tenant, batchIDs); err != nil {
		return err
	}
	// A moved lane can land on a target-date row that ALREADY EXISTS for the same cell and operator.
	// That upsert merges the two vaccine lanes, and no arithmetic on the two incoming counts can
	// describe the merged row (see the ON CONFLICT branch). Membership has just been recomputed from
	// canonical obligations, so the merged row's OWN ledger is now the only thing that knows which
	// animals it really covers -- derive both counters from it.
	return reconcileDriveAssignmentCountersFromMembersTx(ctx, tx, tenant, batchIDs)
}

// reconcileDriveAssignmentCountersFromMembersTx re-derives animal_count and total_doses of every
// touched drive-assignment row from that row's OWN per-goat membership ledger, restoring the
// invariant the rest of the system reads these rows under:
//
//	animal_count == COUNT(DISTINCT goat_id) of the row's members  (the operator cap unit)
//	total_doses  == COUNT(*) of the row's members                 (the dose/stock unit, one member
//	                                                               row per (animal, rule) dose)
//
// This is what makes a lane MERGE correct without guessing the overlap between the two lanes: the
// union of two animal sets is counted, never added. It is idempotent (a row already in parity is
// left untouched) and set-based over one batch list -- no per-row statement.
//
// It keeps the ledgerCoversRow completeness gate: a row whose ledger does not account for it
// completely (no members at all -- written before migration 000040 -- or fewer distinct animals than
// it claims) is SKIPPED, never shrunk to the part of itself the ledger happens to see. Only rows the
// ledger fully covers are rewritten, which is exactly the set a merge can grow.
func reconcileDriveAssignmentCountersFromMembersTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, batchIDs []pgtype.UUID) error {
	if len(batchIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
-- projection-review: membership=vaccination_drive_assignment_members of the drive-assignment rows belonging to ONE tenant's touched batch id list, aggregated back onto the row that owns them; group_key=assignment_id; join_cardinality=members are aggregated to exactly one row per assignment_id before the join, and assignment_id is vaccination_drive_assignments' PK, so the UPDATE join is strictly 1:1 and no row can be written twice; pagination=whole touched batch list recomputed in one set-based statement, no LIMIT can truncate it; scope=explicit tenant + batch id list.
-- GRAIN PROOF (producer vs consumer, mandatory per AGENTS.md):
--   producer unique key   = vaccination_drive_assignment_members (tenant_id, obligation_id) UNIQUE, one member row per dose obligation, carrying (assignment_id, goat_id).
--   consumer match key    = assignment_id, the assignment table's PK -- identical to the key the ledger CTE groups by, so nothing the producer distinguishes is dropped and nothing the consumer keys on is invented.
--   row multiplicity      = ledger: exactly 1 row per assignment_id (GROUP BY); UPDATE ... FROM ledger: at most 1 source row per target row.
--   cap/ratio key sets    = animal_count is count(DISTINCT goat_id) and total_doses is count(*) over the SAME member set of the SAME assignment row, so the operator cap unit (unique animals) and the stock unit (distinct (animal, rule) doses) are each measured over the row they describe -- a lane merge counts the UNION of the two lanes' animals instead of adding two overlapping counts.
--   completeness gate     = ledger_animals >= vda.animal_count (the SQL form of ledgerCoversRow): a row whose ledger cannot account for it completely keeps its existing counters instead of being shrunk to the part of itself the ledger can see.
WITH scoped AS (
  SELECT assignment_id
  FROM vaccination_drive_assignments
  WHERE tenant_id = $1 AND batch_id = ANY($2::uuid[])
), ledger AS (
  SELECT m.assignment_id,
         count(DISTINCT m.goat_id)::int AS ledger_animals,
         count(*)::int AS ledger_doses
  FROM vaccination_drive_assignment_members m
  JOIN scoped s ON s.assignment_id = m.assignment_id
  WHERE m.tenant_id = $1
  GROUP BY m.assignment_id
)
UPDATE vaccination_drive_assignments vda
SET animal_count = ledger.ledger_animals,
    total_doses = ledger.ledger_doses,
    updated_at = now()
FROM ledger
WHERE vda.tenant_id = $1
  AND vda.assignment_id = ledger.assignment_id
  AND ledger.ledger_animals >= vda.animal_count
  AND (vda.animal_count <> ledger.ledger_animals OR vda.total_doses <> ledger.ledger_doses)`,
		tenant, batchIDs); err != nil {
		return fmt.Errorf("obligation: reconcile drive assignment counters from membership: %w", err)
	}
	return nil
}

// movedDriveAssignmentBatchUUIDs is the distinct batch id set touched by one date move.
func movedDriveAssignmentBatchUUIDs(rows []movedDriveAssignment) ([]pgtype.UUID, error) {
	out := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		batchID, err := pgconv.UUID(row.batchID)
		if err != nil {
			return nil, fmt.Errorf("obligation: moved drive assignment batch id: %w", err)
		}
		out = append(out, batchID)
	}
	return dedupUUIDs(out), nil
}

func selectMovedDriveAssignmentsTx(ctx context.Context, tx pgx.Tx, tenant, park pgtype.UUID, vaccineCode string, from, to time.Time) ([]movedDriveAssignment, error) {
	rows, err := tx.Query(ctx, `
-- projection-review: membership=vaccination_drive_assignments rows for ONE tenant/park drive-date move that carry at least one rule of the moved vaccine, decorated with that row's OWN per-goat membership ledger split into the moved and the remaining vaccine lane; group_key=assignment_id; join_cardinality=moved_rules is a single-row CTE cross-joined for rule-id membership, obligation_batches is many-to-one by batch_id, and the ledger LATERAL is a per-assignment aggregate over vaccination_drive_assignment_members (UNIQUE (tenant_id, obligation_id), so one member row per dose) joined 1:1 to its obligation for the rule id -- it collapses to exactly one row per assignment and cannot fan the outer row out; pagination=bounded single park move horizon, ledger LATERAL bounded by the members (tenant_id, assignment_id) index; scope=explicit tenant+park.
-- GRAIN PROOF (producer vs consumer, mandatory per AGENTS.md):
--   producer unique key   = vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, COALESCE(shed_id,0), physical_shed, partition_label, COALESCE(operator_id,0)); vaccination_drive_assignment_members (tenant_id, obligation_id) UNIQUE, one row per dose obligation.
--   consumer match key    = assignment_id on both sides of the ledger LATERAL -- the assignment table's PK, so the decoration is strictly 1:1 with the row it decorates and no assignment column is dropped.
--   row multiplicity      = 1 output row per assignment row (unchanged by the LATERAL); the ledger aggregate fans members IN, never out.
--   cap/ratio key sets    = animal counters use count(DISTINCT goat_id) and dose counters use count(*) over the SAME lane-filtered member set, so an operator's cap unit (unique animals in the lane) and the stock unit (distinct (animal, rule) doses in the lane) are each measured over the lane they belong to -- never the cell's other vaccine.
--   completeness gate    = the lane counters are used only when ledger_animals >= animal_count (ledgerCoversRow); a row the ledger does not fully account for keeps the pre-000040 cell-wide assumption instead of being shrunk to the part of itself the ledger can see.
WITH moved_rules AS (
  SELECT COALESCE(array_agg(DISTINCT rule_id ORDER BY rule_id), '{}'::uuid[]) AS rule_ids
  FROM protocol_rule_dimensions
  WHERE tenant_id = $1
    AND lower(btrim(vaccine_code)) = lower(btrim($3))
)
SELECT
  vda.assignment_id::text,
  vda.batch_id::text,
  vda.park_id::text,
  vda.shed_id::text,
  vda.physical_shed,
  vda.partition_label,
  vda.animal_count,
  (ARRAY(
    SELECT DISTINCT rule_id
    FROM unnest(vda.vaccine_rule_ids) AS rule_ids(rule_id)
    WHERE rule_id = ANY(moved_rules.rule_ids)
    ORDER BY rule_id
  ))::text[],
  (ARRAY(
    SELECT DISTINCT rule_id
    FROM unnest(vda.vaccine_rule_ids) AS rule_ids(rule_id)
    WHERE NOT (rule_id = ANY(moved_rules.rule_ids))
    ORDER BY rule_id
  ))::text[],
  ledger.ledger_animals::int,
  ledger.moved_animals::int,
  ledger.moved_doses::int,
  ledger.remaining_animals::int,
  ledger.remaining_doses::int,
  (ledger.member_rows > 0) AS has_ledger
FROM vaccination_drive_assignments vda
CROSS JOIN moved_rules
LEFT JOIN obligation_batches ob
  ON ob.tenant_id = vda.tenant_id
 AND ob.batch_id = vda.batch_id
LEFT JOIN LATERAL (
  SELECT
    COALESCE(count(DISTINCT m.goat_id) FILTER (WHERE oi.rule_id = ANY(moved_rules.rule_ids)), 0) AS moved_animals,
    COALESCE(count(*) FILTER (WHERE oi.rule_id = ANY(moved_rules.rule_ids)), 0) AS moved_doses,
    COALESCE(count(DISTINCT m.goat_id) FILTER (WHERE NOT (oi.rule_id = ANY(moved_rules.rule_ids))), 0) AS remaining_animals,
    COALESCE(count(*) FILTER (WHERE NOT (oi.rule_id = ANY(moved_rules.rule_ids))), 0) AS remaining_doses,
    count(*) AS member_rows,
    COALESCE(count(DISTINCT m.goat_id), 0) AS ledger_animals
  FROM vaccination_drive_assignment_members m
  JOIN obligation_instances oi
    ON oi.tenant_id = m.tenant_id
   AND oi.obligation_id = m.obligation_id
  WHERE m.tenant_id = vda.tenant_id
    AND m.assignment_id = vda.assignment_id
    AND oi.status <> 'canceled'
) ledger ON TRUE
WHERE vda.tenant_id = $1
  AND vda.park_id = $2
  AND (
    vda.planned_date = $4
    OR (
      $5::date < $4::date
      AND ob.planned_date = $5
      AND vda.planned_date > $4
      AND vda.planned_date <= ($4::date + ($6::int * INTERVAL '1 day'))::date
    )
  )
  AND vda.vaccine_rule_ids && moved_rules.rule_ids
ORDER BY vda.planned_date, vda.assignment_id`, tenant, park, strings.TrimSpace(vaccineCode), businessDateOnly(from), businessDateOnly(to), vaccinationDriveOverrideSafeHorizonDays)
	if err != nil {
		return nil, fmt.Errorf("obligation: select vaccination drive assignments for date move: %w", err)
	}
	defer rows.Close()
	out := make([]movedDriveAssignment, 0)
	for rows.Next() {
		var row movedDriveAssignment
		if err := rows.Scan(&row.assignmentID, &row.batchID, &row.parkID, &row.shedID, &row.physicalShed,
			&row.partitionLabel, &row.animalCount, &row.movedRuleIDs, &row.remainingRuleIDs,
			&row.ledgerAnimalCount, &row.movedAnimalCount, &row.movedDoseCount, &row.remainingAnimalCount, &row.remainingDoseCount,
			&row.hasLedger); err != nil {
			return nil, fmt.Errorf("obligation: scan vaccination drive assignment for date move: %w", err)
		}
		if len(row.movedRuleIDs) == 0 {
			continue
		}
		applyLegacyLaneCounts(&row)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: vaccination drive assignment rows for date move: %w", err)
	}
	return out, nil
}

// ledgerCoversRow reports whether the row's per-goat membership ledger accounts for the row
// COMPLETELY -- it holds a member for every animal the row claims. Only then can the ledger say
// which of the row's animals belong to which vaccine lane. A row with no ledger at all (written
// before migration 000040) or a ledger that covers only part of its claimed animals cannot be split
// per lane from evidence, and must keep the pre-000040 assumption rather than have the split
// silently shrink the row to the part of it the ledger happens to see.
func ledgerCoversRow(row movedDriveAssignment) bool {
	return row.hasLedger && row.ledgerAnimalCount >= row.animalCount
}

// applyLegacyLaneCounts fills the per-lane counters for a row whose ledger does not cover it (see
// ledgerCoversRow). Nothing knows which animal needs which vaccine for such a row, so the only safe
// assumption is the pre-000040 one: every animal in the cell needs every vaccine in it. That is
// exactly the old cell-wide count and the old animal_count x cardinality(rule_ids) dose product --
// kept ONLY here, where no better information exists, instead of being applied to every row as if
// it were the grain.
func applyLegacyLaneCounts(row *movedDriveAssignment) {
	if row == nil || ledgerCoversRow(*row) {
		return
	}
	row.movedAnimalCount = row.animalCount
	row.movedDoseCount = row.animalCount * int32(len(row.movedRuleIDs))
	row.remainingAnimalCount = row.animalCount
	row.remainingDoseCount = row.animalCount * int32(len(row.remainingRuleIDs))
}

// laneDoseCount scales a lane's dose count to a planner chunk that took only part of the lane's
// animals, so a forced operator split never inflates doses past the lane's real dose total.
func laneDoseCount(laneDoses, laneAnimals, chunkAnimals int32) int32 {
	if laneDoses <= 0 || laneAnimals <= 0 || chunkAnimals <= 0 {
		return 0
	}
	if chunkAnimals >= laneAnimals {
		return laneDoses
	}
	value := (int64(laneDoses)*int64(chunkAnimals) + int64(laneAnimals) - 1) / int64(laneAnimals)
	if value > int64(laneDoses) {
		value = int64(laneDoses)
	}
	return int32(value)
}

// detachMovedDriveAssignmentsTx removes the moved vaccine's membership from the source date: rows
// that still carry other vaccines are trimmed to those, rows that carried only the moved vaccine are
// deleted.
func detachMovedDriveAssignmentsTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, rows []movedDriveAssignment) error {
	deleteIDs := make([]string, 0, len(rows))
	trimIDs := make([]string, 0, len(rows))
	trimRuleIDs := make([]string, 0, len(rows))
	trimAnimalCounts := make([]int32, 0, len(rows))
	trimDoseCounts := make([]int32, 0, len(rows))
	for _, row := range rows {
		// A row whose remaining lane holds no animals at all is entirely moved work, even if the
		// rule array still lists other vaccines: leaving it behind would book a source-date row for
		// zero animals against an operator's day.
		if len(row.remainingRuleIDs) == 0 || row.remainingAnimalCount <= 0 {
			deleteIDs = append(deleteIDs, row.assignmentID)
			continue
		}
		encoded, err := json.Marshal(row.remainingRuleIDs)
		if err != nil {
			return fmt.Errorf("obligation: encode remaining vaccine rule ids: %w", err)
		}
		trimIDs = append(trimIDs, row.assignmentID)
		trimRuleIDs = append(trimRuleIDs, string(encoded))
		trimAnimalCounts = append(trimAnimalCounts, row.remainingAnimalCount)
		trimDoseCounts = append(trimDoseCounts, row.remainingDoseCount)
	}
	if len(trimIDs) > 0 {
		if _, err := tx.Exec(ctx, `
UPDATE vaccination_drive_assignments vda
SET vaccine_rule_ids = u.rule_ids,
    animal_count = u.animal_count,
    total_doses = u.total_doses,
    updated_at = now()
FROM (
  SELECT
    t.assignment_id,
    t.animal_count,
    t.total_doses,
    COALESCE((
      SELECT array_agg(rule_id::uuid ORDER BY rule_id)
      FROM jsonb_array_elements_text(t.rule_ids_json::jsonb) AS rule_ids(rule_id)
    ), '{}'::uuid[]) AS rule_ids
  FROM unnest($2::uuid[], $3::text[], $4::int[], $5::int[])
       AS t(assignment_id, rule_ids_json, animal_count, total_doses)
) u
WHERE vda.tenant_id = $1 AND vda.assignment_id = u.assignment_id`,
			tenant, trimIDs, trimRuleIDs, trimAnimalCounts, trimDoseCounts); err != nil {
			return fmt.Errorf("obligation: trim vaccination drive assignments for date move: %w", err)
		}
	}
	if len(deleteIDs) > 0 {
		if _, err := tx.Exec(ctx, `
DELETE FROM vaccination_drive_assignments
WHERE tenant_id = $1 AND assignment_id = ANY($2::uuid[])`, tenant, deleteIDs); err != nil {
			return fmt.Errorf("obligation: delete vaccination drive assignments for date move: %w", err)
		}
	}
	return nil
}

// movedInDriveOperatorLoadTx sums the animal load already sitting on the target date that came from
// an EARLIER date move (the row's batch is planned on a different day, so the batch-grain operator
// load query behind AvailableVaccinationOperatorsForDrive cannot see it). Native rows for the target
// date are excluded because that load is already netted off by the operator-capacity query.
func movedInDriveOperatorLoadByDateTx(ctx context.Context, tx pgx.Tx, tenant, park pgtype.UUID, dates []time.Time) (map[string]map[string]int32, error) {
	out := make(map[string]map[string]int32)
	if len(dates) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
-- projection-review: membership=vaccination_drive_assignments rows on ONE tenant/park/planned_date whose batch is planned on another date (override-moved work); group_key=operator_id; join_cardinality=assignment:batch is many-to-one on (tenant_id,batch_id); pagination=bounded single park-day drive-plan slice; scope=explicit tenant+park.
SELECT vda.planned_date, vda.operator_id::text, COALESCE(sum(vda.animal_count), 0)::int
FROM vaccination_drive_assignments vda
JOIN obligation_batches ob
  ON ob.tenant_id = vda.tenant_id
 AND ob.batch_id = vda.batch_id
WHERE vda.tenant_id = $1
  AND vda.park_id = $2
  AND vda.planned_date = ANY($3::date[])
  AND vda.operator_id IS NOT NULL
  AND (ob.planned_date IS NULL OR ob.planned_date <> vda.planned_date)
GROUP BY vda.planned_date, vda.operator_id`, tenant, park, dates)
	if err != nil {
		return nil, fmt.Errorf("obligation: moved-in drive operator load: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var date time.Time
		var operatorID string
		var animals int32
		if err := rows.Scan(&date, &operatorID, &animals); err != nil {
			return nil, fmt.Errorf("obligation: scan moved-in drive operator load: %w", err)
		}
		key := biztime.BusinessDate(date)
		if out[key] == nil {
			out[key] = make(map[string]int32)
		}
		out[key][strings.TrimSpace(operatorID)] = animals
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: moved-in drive operator load rows: %w", err)
	}
	return out, nil
}

func availabilityAfterMovedInLoad(availability []vaccexecapp.DriveDateAvailability, movedInLoad map[string]map[string]int32) []vaccexecapp.DriveDateAvailability {
	out := make([]vaccexecapp.DriveDateAvailability, 0, len(availability))
	for _, day := range availability {
		dateLoad := movedInLoad[biztime.BusinessDate(day.Date)]
		operators := make([]vaccexecapp.DriveOperator, 0, len(day.Operators))
		for _, operator := range day.Operators {
			operator.Cap -= int(dateLoad[strings.TrimSpace(operator.ID)])
			if operator.Cap <= 0 {
				continue
			}
			operator.Available = true
			operators = append(operators, operator)
		}
		out = append(out, vaccexecapp.DriveDateAvailability{Date: businessDateOnly(day.Date), Operators: operators})
	}
	return out
}

func driveAvailabilityDates(availability []vaccexecapp.DriveDateAvailability) []time.Time {
	out := make([]time.Time, 0, len(availability))
	seen := make(map[string]struct{}, len(availability))
	for _, day := range availability {
		date := businessDateOnly(day.Date)
		key := biztime.BusinessDate(date)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, date)
	}
	return out
}

// planMovedDriveAssignments runs the shared OperatorDrivePlanner over the moved rows from the
// selected target date forward. A manual move chooses the START of the drive, not a promise that the
// entire drive fits on that one day, so normal overflow should fill later available operator-days
// instead of creating an over-cap row on the first date.
func planMovedDriveAssignments(rows []movedDriveAssignment, availability []vaccexecapp.DriveDateAvailability, to time.Time) []plannedDriveAssignment {
	target := businessDateOnly(to)
	type movedBlock struct {
		block vaccexecapp.DriveWorkBlock
		rows  []movedDriveAssignment
	}
	blocksByKey := make(map[string]*movedBlock, len(rows))
	blockOrder := make([]string, 0, len(rows))
	for _, row := range rows {
		physicalShed := strings.TrimSpace(row.physicalShed)
		if physicalShed == "" {
			physicalShed = "park"
		}
		partition := strings.TrimSpace(row.partitionLabel)
		if partition == "" {
			partition = "whole"
		}
		key := strings.Join([]string{strings.TrimSpace(row.parkID), physicalShed, partition}, "\x00")
		group := blocksByKey[key]
		if group == nil {
			id := strconv.Itoa(len(blockOrder))
			group = &movedBlock{block: vaccexecapp.DriveWorkBlock{
				ID:           id,
				Park:         row.parkID,
				PhysicalShed: physicalShed,
				Partition:    partition,
				DueDate:      target,
			}}
			blocksByKey[key] = group
			blockOrder = append(blockOrder, key)
		}
		if int(row.movedAnimalCount) > group.block.Animals {
			group.block.Animals = int(row.movedAnimalCount)
		}
		group.rows = append(group.rows, row)
	}
	byID := make(map[string][]movedDriveAssignment, len(rows))
	blocks := make([]vaccexecapp.DriveWorkBlock, 0, len(rows))
	for _, key := range blockOrder {
		group := blocksByKey[key]
		if movedDriveAssignmentLaneCountsMatch(group.rows) {
			group.block.ID = strconv.Itoa(len(blocks))
			blocks = append(blocks, group.block)
			byID[group.block.ID] = group.rows
			continue
		}
		for _, row := range group.rows {
			block := group.block
			block.ID = strconv.Itoa(len(blocks))
			block.Animals = int(row.movedAnimalCount)
			blocks = append(blocks, block)
			byID[block.ID] = []movedDriveAssignment{row}
		}
	}
	planned := make([]plannedDriveAssignment, 0, len(rows))
	appendRow := func(row movedDriveAssignment, plannedDate time.Time, operatorID *string, animals int32, status string, warnings []string) {
		if animals <= 0 {
			return
		}
		planned = append(planned, plannedDriveAssignment{
			plannedDate:    businessDateOnly(plannedDate),
			totalDoses:     laneDoseCount(row.movedDoseCount, row.movedAnimalCount, animals),
			batchID:        row.batchID,
			parkID:         row.parkID,
			shedID:         row.shedID,
			physicalShed:   row.physicalShed,
			partitionLabel: row.partitionLabel,
			operatorID:     operatorID,
			animalCount:    animals,
			ruleIDs:        row.movedRuleIDs,
			capacityStatus: status,
			warnings:       warnings,
		})
	}

	plan, err := (vaccexecapp.OperatorDrivePlanner{}).Plan(vaccexecapp.DrivePlanRequest{
		StartDate:             target,
		Availability:          availability,
		ConfiguredOperatorCap: configuredOperatorCapFromAvailability(availability),
		WorkBlocks:            blocks,
	})
	if err != nil {
		// A planner error must never silently drop planned work: park every moved row on the target
		// date unassigned so the day is visibly awaiting a capacity decision.
		for _, row := range rows {
			appendRow(row, target, nil, row.movedAnimalCount, "capacity_action", []string{"vaccination drive moved without an operator plan for the new date"})
		}
		return mergePlannedDriveAssignments(planned)
	}
	remainingByBlockID := make(map[string]int32, len(blocks))
	for _, block := range blocks {
		remainingByBlockID[block.ID] = int32(block.Animals)
	}
	for _, day := range plan.Days {
		plannedDate, parseErr := time.ParseInLocation("2006-01-02", day.Date, biztime.DefaultLocation())
		if parseErr != nil {
			plannedDate = target
		}
		for _, assignment := range day.Assignments {
			operatorID := strings.TrimSpace(assignment.OperatorID)
			blockIDs := uniqueNonBlank(assignment.BlockIDs)
			for _, blockID := range blockIDs {
				rows, ok := byID[blockID]
				if !ok {
					continue
				}
				chunkAnimals := remainingByBlockID[blockID]
				if len(blockIDs) == 1 && assignment.Animals > 0 && int32(assignment.Animals) < chunkAnimals {
					chunkAnimals = int32(assignment.Animals)
				}
				if chunkAnimals <= 0 {
					continue
				}
				for _, row := range rows {
					animals := row.movedAnimalCount
					if chunkAnimals < animals {
						animals = chunkAnimals
					}
					status := "within_cap"
					warnings := make([]string, 0, 2)
					if drivePlanHasWarning(assignment.Warnings, "over_cap_required_latest_safe") {
						status = "over_cap_required"
						warnings = append(warnings, "operator animal cap exceeded to keep the moved vaccination drive date")
					}
					if drivePlanHasWarning(assignment.Warnings, "forced_partition_split") {
						warnings = append(warnings, "partition split because one partition exceeded available operator capacity")
					}
					var operator *string
					if operatorID != "" {
						value := operatorID
						operator = &value
					} else {
						status = "capacity_action"
						warnings = append(warnings, "no vaccination operator is available on the moved drive date")
					}
					appendRow(row, plannedDate, operator, animals, status, warnings)
				}
				remainingByBlockID[blockID] -= chunkAnimals
			}
		}
	}
	for _, block := range plan.Unassigned {
		rows, ok := byID[block.ID]
		if !ok {
			continue
		}
		for _, row := range rows {
			appendRow(row, target, nil, int32(block.Animals), "capacity_action",
				[]string{"no vaccination operator capacity is available on the moved drive date"})
		}
	}
	return mergePlannedDriveAssignments(planned)
}

func movedDriveAssignmentLaneCountsMatch(rows []movedDriveAssignment) bool {
	if len(rows) <= 1 {
		return true
	}
	animals := rows[0].movedAnimalCount
	for _, row := range rows[1:] {
		if row.movedAnimalCount != animals {
			return false
		}
	}
	return true
}

func configuredOperatorCapFromAvailability(availability []vaccexecapp.DriveDateAvailability) int {
	maxCap := 0
	for _, day := range availability {
		for _, operator := range day.Operators {
			if operator.ConfiguredCap > maxCap {
				maxCap = operator.ConfiguredCap
			}
		}
	}
	return maxCap
}

// mergePlannedDriveAssignments folds rows that share the persisted uniqueness key (batch, park,
// shed, physical shed, partition, operator) into one row, summing animals and keeping the worst
// capacity status, so two planner chunks for the same operator cannot collide on write and lose
// animals to a last-writer-wins upsert.
func mergePlannedDriveAssignments(rows []plannedDriveAssignment) []plannedDriveAssignment {
	order := make([]string, 0, len(rows))
	byKey := make(map[string]plannedDriveAssignment, len(rows))
	for _, row := range rows {
		shed := ""
		if row.shedID != nil {
			shed = *row.shedID
		}
		operator := ""
		if row.operatorID != nil {
			operator = *row.operatorID
		}
		key := strings.Join([]string{biztime.BusinessDate(row.plannedDate), row.batchID, row.parkID, shed, row.physicalShed, row.partitionLabel, operator}, "\x00")
		existing, ok := byKey[key]
		if !ok {
			order = append(order, key)
			byKey[key] = row
			continue
		}
		existing.animalCount += row.animalCount
		existing.totalDoses += row.totalDoses
		existing.capacityStatus = worstCapacityStatus(existing.capacityStatus, row.capacityStatus)
		existing.ruleIDs = unionSortedIDs(existing.ruleIDs, row.ruleIDs)
		existing.warnings = unionSortedIDs(existing.warnings, row.warnings)
		byKey[key] = existing
	}
	out := make([]plannedDriveAssignment, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	return out
}

func worstCapacityStatus(left, right string) string {
	rank := map[string]int{"within_cap": 0, "over_cap_required": 1, "capacity_action": 2}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func unionSortedIDs(left, right []string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	out := make([]string, 0, len(left)+len(right))
	for _, values := range [][]string{left, right} {
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func uniqueNonBlank(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func drivePlanHasWarning(warnings []string, want string) bool {
	for _, warning := range warnings {
		if strings.TrimSpace(warning) == want {
			return true
		}
	}
	return false
}

func insertPlannedDriveAssignmentsTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, rows []plannedDriveAssignment) error {
	if len(rows) == 0 {
		return nil
	}
	batchIDs := make([]string, 0, len(rows))
	operatorIDs := make([]*string, 0, len(rows))
	parkIDs := make([]string, 0, len(rows))
	plannedDates := make([]time.Time, 0, len(rows))
	shedIDs := make([]*string, 0, len(rows))
	physicalSheds := make([]string, 0, len(rows))
	partitions := make([]string, 0, len(rows))
	animalCounts := make([]int32, 0, len(rows))
	doseCounts := make([]int32, 0, len(rows))
	ruleIDsJSON := make([]string, 0, len(rows))
	statuses := make([]string, 0, len(rows))
	warningsJSON := make([]string, 0, len(rows))
	for _, row := range rows {
		encodedRules, err := json.Marshal(row.ruleIDs)
		if err != nil {
			return fmt.Errorf("obligation: encode moved vaccine rule ids: %w", err)
		}
		warnings := row.warnings
		if warnings == nil {
			warnings = []string{}
		}
		encodedWarnings, err := json.Marshal(warnings)
		if err != nil {
			return fmt.Errorf("obligation: encode moved drive assignment warnings: %w", err)
		}
		physicalShed := strings.TrimSpace(row.physicalShed)
		if physicalShed == "" {
			physicalShed = "park"
		}
		partition := strings.TrimSpace(row.partitionLabel)
		if partition == "" {
			partition = "whole"
		}
		batchIDs = append(batchIDs, row.batchID)
		plannedDates = append(plannedDates, biztime.BusinessDayStart(row.plannedDate))
		operatorIDs = append(operatorIDs, row.operatorID)
		parkIDs = append(parkIDs, row.parkID)
		shedIDs = append(shedIDs, row.shedID)
		physicalSheds = append(physicalSheds, physicalShed)
		partitions = append(partitions, partition)
		animalCounts = append(animalCounts, row.animalCount)
		doseCounts = append(doseCounts, row.totalDoses)
		ruleIDsJSON = append(ruleIDsJSON, string(encodedRules))
		statuses = append(statuses, row.capacityStatus)
		warningsJSON = append(warningsJSON, string(encodedWarnings))
	}
	if _, err := tx.Exec(ctx, `
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
  $2::uuid[], $3::date[], $4::uuid[], $5::uuid[], $6::uuid[], $7::text[], $8::text[], $9::int[], $10::int[], $11::text[], $12::text[], $13::text[]
) AS u(batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, total_doses, vaccine_rule_ids_json, capacity_status, warnings)
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
  vaccine_rule_ids = (
    SELECT array_agg(DISTINCT rule_id ORDER BY rule_id)
    FROM unnest(vaccination_drive_assignments.vaccine_rule_ids || EXCLUDED.vaccine_rule_ids) AS rule_ids(rule_id)
  ),
  -- PROVISIONAL, not the answer. On conflict the row's vaccine lane becomes the UNION of the
  -- existing lane and the arriving one (above), so the two counters that describe that union cannot
  -- be either side alone: EXCLUDED.animal_count silently deletes the pre-existing lane's animals
  -- from a row that still plans them, and vda.animal_count + EXCLUDED.animal_count double-counts
  -- every animal the two lanes SHARE (a goat due both vaccines is one cap unit, not two). Neither
  -- number knows the overlap; only the merged row's own per-goat membership can. GREATEST is
  -- therefore used purely as a NON-SHRINKING lower bound to carry the row into
  -- syncVaccinationDriveAssignmentMembersTx (whose split window reads animal_count), and
  -- reconcileDriveAssignmentCountersFromMembersTx sets the authoritative values right after.
  animal_count = GREATEST(vaccination_drive_assignments.animal_count, EXCLUDED.animal_count),
  total_doses = GREATEST(vaccination_drive_assignments.total_doses, EXCLUDED.total_doses),
  capacity_status = EXCLUDED.capacity_status,
  warnings = EXCLUDED.warnings,
  updated_at = now()`,
		tenant, batchIDs, plannedDates, operatorIDs, parkIDs, shedIDs, physicalSheds,
		partitions, animalCounts, doseCounts, ruleIDsJSON, statuses, warningsJSON); err != nil {
		return fmt.Errorf("obligation: write re-planned vaccination drive assignments: %w", err)
	}
	return nil
}

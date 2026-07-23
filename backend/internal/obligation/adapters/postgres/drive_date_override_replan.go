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
	assignmentID     string
	batchID          string
	parkID           string
	shedID           *string
	physicalShed     string
	partitionLabel   string
	animalCount      int32
	movedRuleIDs     []string
	remainingRuleIDs []string
}

// plannedDriveAssignment is one row to write on the target date after re-planning.
type plannedDriveAssignment struct {
	batchID        string
	parkID         string
	shedID         *string
	physicalShed   string
	partitionLabel string
	operatorID     *string
	animalCount    int32
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
			ID:        operatorID,
			Name:      operatorID,
			Cap:       int(operator.Cap),
			Available: operator.Cap > 0,
		})
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
	operators []vaccexecapp.DriveOperator,
) error {
	rows, err := selectMovedDriveAssignmentsTx(ctx, tx, tenant, park, vaccineCode, from)
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
	movedInLoad, err := movedInDriveOperatorLoadTx(ctx, tx, tenant, park, to)
	if err != nil {
		return err
	}
	planned := planMovedDriveAssignments(rows, availableOperatorsAfterMovedInLoad(operators, movedInLoad), to)
	if err := insertPlannedDriveAssignmentsTx(ctx, tx, tenant, to, planned); err != nil {
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
	return syncVaccinationDriveAssignmentMembersTx(ctx, tx, tenant, batchIDs)
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

func selectMovedDriveAssignmentsTx(ctx context.Context, tx pgx.Tx, tenant, park pgtype.UUID, vaccineCode string, from time.Time) ([]movedDriveAssignment, error) {
	rows, err := tx.Query(ctx, `
-- projection-review: membership=vaccination_drive_assignments rows for ONE tenant/park/planned_date that carry at least one rule of the moved vaccine; group_key=assignment_id; join_cardinality=moved_rules is a single-row CTE cross-joined for rule-id membership; pagination=bounded single park-day drive-plan slice; scope=explicit tenant+park.
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
  ))::text[]
FROM vaccination_drive_assignments vda
CROSS JOIN moved_rules
WHERE vda.tenant_id = $1
  AND vda.park_id = $2
  AND vda.planned_date = $4
  AND vda.vaccine_rule_ids && moved_rules.rule_ids
ORDER BY vda.assignment_id`, tenant, park, strings.TrimSpace(vaccineCode), businessDateOnly(from))
	if err != nil {
		return nil, fmt.Errorf("obligation: select vaccination drive assignments for date move: %w", err)
	}
	defer rows.Close()
	out := make([]movedDriveAssignment, 0)
	for rows.Next() {
		var row movedDriveAssignment
		if err := rows.Scan(&row.assignmentID, &row.batchID, &row.parkID, &row.shedID, &row.physicalShed,
			&row.partitionLabel, &row.animalCount, &row.movedRuleIDs, &row.remainingRuleIDs); err != nil {
			return nil, fmt.Errorf("obligation: scan vaccination drive assignment for date move: %w", err)
		}
		if len(row.movedRuleIDs) == 0 {
			continue
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: vaccination drive assignment rows for date move: %w", err)
	}
	return out, nil
}

// detachMovedDriveAssignmentsTx removes the moved vaccine's membership from the source date: rows
// that still carry other vaccines are trimmed to those, rows that carried only the moved vaccine are
// deleted.
func detachMovedDriveAssignmentsTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, rows []movedDriveAssignment) error {
	deleteIDs := make([]string, 0, len(rows))
	trimIDs := make([]string, 0, len(rows))
	trimRuleIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row.remainingRuleIDs) == 0 {
			deleteIDs = append(deleteIDs, row.assignmentID)
			continue
		}
		encoded, err := json.Marshal(row.remainingRuleIDs)
		if err != nil {
			return fmt.Errorf("obligation: encode remaining vaccine rule ids: %w", err)
		}
		trimIDs = append(trimIDs, row.assignmentID)
		trimRuleIDs = append(trimRuleIDs, string(encoded))
	}
	if len(trimIDs) > 0 {
		if _, err := tx.Exec(ctx, `
UPDATE vaccination_drive_assignments vda
SET vaccine_rule_ids = u.rule_ids,
    total_doses = vda.animal_count * cardinality(u.rule_ids),
    updated_at = now()
FROM (
  SELECT
    t.assignment_id,
    COALESCE((
      SELECT array_agg(rule_id::uuid ORDER BY rule_id)
      FROM jsonb_array_elements_text(t.rule_ids_json::jsonb) AS rule_ids(rule_id)
    ), '{}'::uuid[]) AS rule_ids
  FROM unnest($2::uuid[], $3::text[]) AS t(assignment_id, rule_ids_json)
) u
WHERE vda.tenant_id = $1 AND vda.assignment_id = u.assignment_id`, tenant, trimIDs, trimRuleIDs); err != nil {
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
func movedInDriveOperatorLoadTx(ctx context.Context, tx pgx.Tx, tenant, park pgtype.UUID, to time.Time) (map[string]int32, error) {
	rows, err := tx.Query(ctx, `
-- projection-review: membership=vaccination_drive_assignments rows on ONE tenant/park/planned_date whose batch is planned on another date (override-moved work); group_key=operator_id; join_cardinality=assignment:batch is many-to-one on (tenant_id,batch_id); pagination=bounded single park-day drive-plan slice; scope=explicit tenant+park.
SELECT vda.operator_id::text, COALESCE(sum(vda.animal_count), 0)::int
FROM vaccination_drive_assignments vda
JOIN obligation_batches ob
  ON ob.tenant_id = vda.tenant_id
 AND ob.batch_id = vda.batch_id
WHERE vda.tenant_id = $1
  AND vda.park_id = $2
  AND vda.planned_date = $3
  AND vda.operator_id IS NOT NULL
  AND (ob.planned_date IS NULL OR ob.planned_date <> $3)
GROUP BY vda.operator_id`, tenant, park, businessDateOnly(to))
	if err != nil {
		return nil, fmt.Errorf("obligation: moved-in drive operator load: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int32)
	for rows.Next() {
		var operatorID string
		var animals int32
		if err := rows.Scan(&operatorID, &animals); err != nil {
			return nil, fmt.Errorf("obligation: scan moved-in drive operator load: %w", err)
		}
		out[strings.TrimSpace(operatorID)] = animals
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: moved-in drive operator load rows: %w", err)
	}
	return out, nil
}

func availableOperatorsAfterMovedInLoad(operators []vaccexecapp.DriveOperator, movedInLoad map[string]int32) []vaccexecapp.DriveOperator {
	out := make([]vaccexecapp.DriveOperator, 0, len(operators))
	for _, operator := range operators {
		operator.Cap -= int(movedInLoad[strings.TrimSpace(operator.ID)])
		if operator.Cap <= 0 {
			continue
		}
		operator.Available = true
		out = append(out, operator)
	}
	return out
}

// planMovedDriveAssignments runs the shared OperatorDrivePlanner over the moved rows for the target
// date. The moved drive date IS the latest safe date for that work (an admin explicitly chose it),
// so the planner's latest-safe path is used: it fills real remaining capacity first and only then
// records the residue as over_cap_required instead of silently dropping it. Work the target date
// cannot take at all is written unassigned as capacity_action for a human capacity decision.
func planMovedDriveAssignments(rows []movedDriveAssignment, operators []vaccexecapp.DriveOperator, to time.Time) []plannedDriveAssignment {
	target := businessDateOnly(to)
	byID := make(map[string]movedDriveAssignment, len(rows))
	blocks := make([]vaccexecapp.DriveWorkBlock, 0, len(rows))
	for i, row := range rows {
		id := strconv.Itoa(i)
		byID[id] = row
		physicalShed := strings.TrimSpace(row.physicalShed)
		if physicalShed == "" {
			physicalShed = "park"
		}
		partition := strings.TrimSpace(row.partitionLabel)
		if partition == "" {
			partition = "whole"
		}
		blocks = append(blocks, vaccexecapp.DriveWorkBlock{
			ID:             id,
			Park:           row.parkID,
			PhysicalShed:   physicalShed,
			Partition:      partition,
			Animals:        int(row.animalCount),
			DueDate:        target,
			LatestSafeDate: target,
		})
	}
	planned := make([]plannedDriveAssignment, 0, len(rows))
	appendRow := func(row movedDriveAssignment, operatorID *string, animals int32, status string, warnings []string) {
		if animals <= 0 {
			return
		}
		planned = append(planned, plannedDriveAssignment{
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
		StartDate:    target,
		Availability: []vaccexecapp.DriveDateAvailability{{Date: target, Operators: operators}},
		WorkBlocks:   blocks,
	})
	if err != nil {
		// A planner error must never silently drop planned work: park every moved row on the target
		// date unassigned so the day is visibly awaiting a capacity decision.
		for _, row := range rows {
			appendRow(row, nil, row.animalCount, "capacity_action", []string{"vaccination drive moved without an operator plan for the new date"})
		}
		return mergePlannedDriveAssignments(planned)
	}
	for _, day := range plan.Days {
		for _, assignment := range day.Assignments {
			operatorID := strings.TrimSpace(assignment.OperatorID)
			blockIDs := uniqueNonBlank(assignment.BlockIDs)
			for _, blockID := range blockIDs {
				row, ok := byID[blockID]
				if !ok {
					continue
				}
				animals := row.animalCount
				if len(blockIDs) == 1 && assignment.Animals > 0 && int32(assignment.Animals) < animals {
					animals = int32(assignment.Animals)
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
				appendRow(row, operator, animals, status, warnings)
			}
		}
	}
	for _, block := range plan.Unassigned {
		row, ok := byID[block.ID]
		if !ok {
			continue
		}
		appendRow(row, nil, int32(block.Animals), "capacity_action",
			[]string{"no vaccination operator capacity is available on the moved drive date"})
	}
	return mergePlannedDriveAssignments(planned)
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
		key := strings.Join([]string{row.batchID, row.parkID, shed, row.physicalShed, row.partitionLabel, operator}, "\x00")
		existing, ok := byKey[key]
		if !ok {
			order = append(order, key)
			byKey[key] = row
			continue
		}
		existing.animalCount += row.animalCount
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

func insertPlannedDriveAssignmentsTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, to time.Time, rows []plannedDriveAssignment) error {
	if len(rows) == 0 {
		return nil
	}
	batchIDs := make([]string, 0, len(rows))
	operatorIDs := make([]*string, 0, len(rows))
	parkIDs := make([]string, 0, len(rows))
	shedIDs := make([]*string, 0, len(rows))
	physicalSheds := make([]string, 0, len(rows))
	partitions := make([]string, 0, len(rows))
	animalCounts := make([]int32, 0, len(rows))
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
		operatorIDs = append(operatorIDs, row.operatorID)
		parkIDs = append(parkIDs, row.parkID)
		shedIDs = append(shedIDs, row.shedID)
		physicalSheds = append(physicalSheds, physicalShed)
		partitions = append(partitions, partition)
		animalCounts = append(animalCounts, row.animalCount)
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
  $2,
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
  u.animal_count * COALESCE((
    SELECT count(*)
    FROM jsonb_array_elements_text(u.vaccine_rule_ids_json::jsonb) AS rule_ids(rule_id)
  ), 0),
  u.capacity_status,
  u.warnings::jsonb
FROM unnest(
  $3::uuid[], $4::uuid[], $5::uuid[], $6::uuid[], $7::text[], $8::text[], $9::int[], $10::text[], $11::text[], $12::text[]
) AS u(batch_id, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids_json, capacity_status, warnings)
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
  animal_count = EXCLUDED.animal_count,
  total_doses = EXCLUDED.animal_count * cardinality((
    SELECT array_agg(DISTINCT rule_id ORDER BY rule_id)
    FROM unnest(vaccination_drive_assignments.vaccine_rule_ids || EXCLUDED.vaccine_rule_ids) AS rule_ids(rule_id)
  )),
  capacity_status = EXCLUDED.capacity_status,
  warnings = EXCLUDED.warnings,
  updated_at = now()`,
		tenant, biztime.BusinessDayStart(to), batchIDs, operatorIDs, parkIDs, shedIDs, physicalSheds,
		partitions, animalCounts, ruleIDsJSON, statuses, warningsJSON); err != nil {
		return fmt.Errorf("obligation: write re-planned vaccination drive assignments: %w", err)
	}
	return nil
}

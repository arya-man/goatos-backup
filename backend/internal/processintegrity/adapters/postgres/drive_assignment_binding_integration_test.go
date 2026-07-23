package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

const (
	piOperatorTwo = "71000000-0000-4000-8000-000000000031"
	piPositionTwo = "71000000-0000-4000-8000-000000000032"
)

// TestCanonicalRowsBindDriveAssignmentByPartitionAndReadRealOperatorDayCapacity is the adversarial
// split-assignment case for Control Tower / Action Center / Protocol Adherence / Workflow drilldown.
//
// One batch, one shed, TWO vaccination_drive_assignments rows for that batch+shed that differ in
// every discriminating column (partition_label, operator_id, planned_date, animal_count,
// capacity_status). The obligation's goat physically lives in partition '2', so the row MUST bind to
// the partition-'2' assignment (later date, second operator, 7 animals, over_cap_required) and NOT to
// the arbitrary earliest row.
//
// It also pins the drive_* capacity fields to the real assignment / operator-day capacity source:
// drive_animals_assigned is the bound assignment's animal_count (not the obligation expectation),
// drive_capacity_state is the persisted capacity_status, drive_operator_cap is the bound operator's
// workforce_positions.vaccination_daily_animal_cap for that day (not the tenant-wide default), and
// drive_available_operators counts the operators actually assigned to this grain.
func TestCanonicalRowsBindDriveAssignmentByPartitionAndReadRealOperatorDayCapacity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	execPI(t, ctx, pool, "second operator",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'OP-PI-2', 'Operator PI Two', 'active', 'operator', $3)`,
		piOperatorTwo, piTenant, piShed)
	// Real per-operator-day capacity source: the position row carries this operator's daily animal cap.
	execPI(t, ctx, pool, "second operator position cap",
		`INSERT INTO workforce_positions (position_id, tenant_id, workforce_member_id, scope_type, scope_id,
		   position_code, position_tier, status, valid_from, vaccination_daily_animal_cap)
		 VALUES ($1, $2, $3, 'center', $4, 'vaccination_operator', 'assistant', 'active',
		   TIMESTAMPTZ '2026-06-01 00:00:00+05:30', 150)`,
		piPositionTwo, piTenant, piOperatorTwo, piPark)
	// The obligation's goat physically sits in partition '2' of the shed.
	execPI(t, ctx, pool, "goat partition",
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
		 VALUES ($1, $2, $3, '2', 'Process Shed - Part 2')`,
		piTenant, piGoat, piShed)

	// Earliest row: a DIFFERENT partition of the same shed, a different operator, an earlier date,
	// a different animal count and a different capacity status. Binding to it is the defect.
	execPI(t, ctx, pool, "assignment partition 1",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
		   physical_shed, partition_label, animal_count, capacity_status, vaccine_rule_ids, total_doses)
		 VALUES ($1, $2, DATE '2026-06-24', $3, $4, $5, 'Process Shed', '1', 40, 'within_cap', ARRAY[$6::uuid], 40)`,
		piTenant, piBatch, piOperator, piPark, piShed, piRule)
	// Matching row: the goat's own partition.
	execPI(t, ctx, pool, "assignment partition 2",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
		   physical_shed, partition_label, animal_count, capacity_status, vaccine_rule_ids, total_doses)
		 VALUES ($1, $2, DATE '2026-06-25', $3, $4, $5, 'Process Shed', '2', 7, 'over_cap_required', ARRAY[$6::uuid], 7)`,
		piTenant, piBatch, piOperatorTwo, piPark, piShed, piRule)

	repo := NewRepository(pool, 10*time.Second)
	result, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListRows() error = %v", err)
	}
	row := rowByBatchSubstr(result.Rows, piBatch)
	if row == nil {
		t.Fatalf("batch row missing: %#v", result.Rows)
	}

	if row.Owner.OperatorName == nil || *row.Owner.OperatorName != "Operator PI Two" {
		got := "<nil>"
		if row.Owner.OperatorName != nil {
			got = *row.Owner.OperatorName
		}
		t.Errorf("operator bound to wrong assignment: got %q want %q", got, "Operator PI Two")
	}
	if row.Owner.OperatorID == nil || *row.Owner.OperatorID != piOperatorTwo {
		t.Errorf("operator id = %v want %s", row.Owner.OperatorID, piOperatorTwo)
	}
	gotDate := row.DueAt.In(biztime.DefaultLocation()).Format("2006-01-02")
	if gotDate != "2026-06-25" {
		t.Errorf("execution date bound to wrong assignment: got %s want 2026-06-25", gotDate)
	}
	if row.DriveAnimalsAssigned != 7 {
		t.Errorf("drive_animals_assigned = %d want 7 (bound assignment animal_count)", row.DriveAnimalsAssigned)
	}
	if row.DriveCapacityState != domain.DriveCapacityStateOverCapRequired {
		t.Errorf("drive_capacity_state = %q want %q (bound assignment capacity_status)", row.DriveCapacityState, domain.DriveCapacityStateOverCapRequired)
	}
	if row.DriveOperatorCap != 150 {
		t.Errorf("drive_operator_cap = %d want 150 (bound operator workforce_positions.vaccination_daily_animal_cap, not the tenant-wide default)", row.DriveOperatorCap)
	}
	if row.DriveAvailableOperators != 1 {
		t.Errorf("drive_available_operators = %d want 1", row.DriveAvailableOperators)
	}
}

// TestCanonicalRowsAggregateSamePartitionSplitDriveAssignments is the same-partition split case
// raised in review: the operator drive planner can hand ONE partition of ONE shed on ONE batch to
// SEVERAL operators (and several dates). See
// backend/internal/vaccinationexecution/app/operator_drive_planner.go ->
// splitLatestSafeGroupAcrossOperators / splitOversizedBlockAcrossOperators, which emit one
// DrivePlanAssignment per capacity chunk of the SAME DriveWorkBlock, each tagged
// "forced_partition_split", differing only in operator (and planned date across days).
//
// vaccination_drive_assignments has NO goat-level membership, so no ranking on the existing columns
// can bind a specific goat to a specific split arm -- the rows are identical on (batch, shed,
// partition_label, vaccine_rule_ids). Picking one arm and presenting it as authoritative silently
// hides the other operator and undercounts the assigned load.
//
// Locked behavior: the LIMIT 1 pick stays as the DETERMINISTIC representative for the single-valued
// operator/date display (earliest planned_date, then lowest operator_id), but the capacity facts are
// AGGREGATED over the whole split cohort, so the split is explicit rather than silent:
// drive_available_operators > 1 and drive_animals_assigned is the full split load, not one arm.
//
// True per-goat binding needs a schema change (goat-level membership in
// vaccination_drive_assignments, or an assignment_members table). NOT done here.
func TestCanonicalRowsAggregateSamePartitionSplitDriveAssignments(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	execPI(t, ctx, pool, "split operator",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'OP-PI-2', 'Operator PI Two', 'active', 'operator', $3)`,
		piOperatorTwo, piTenant, piShed)
	execPI(t, ctx, pool, "split operator position cap",
		`INSERT INTO workforce_positions (position_id, tenant_id, workforce_member_id, scope_type, scope_id,
		   position_code, position_tier, status, valid_from, vaccination_daily_animal_cap)
		 VALUES ($1, $2, $3, 'center', $4, 'vaccination_operator', 'assistant', 'active',
		   TIMESTAMPTZ '2026-06-01 00:00:00+05:30', 150)`,
		piPositionTwo, piTenant, piOperatorTwo, piPark)
	execPI(t, ctx, pool, "goat partition",
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
		 VALUES ($1, $2, $3, '2', 'Process Shed - Part 2')`,
		piTenant, piGoat, piShed)

	// SAME partition '2' of the SAME shed on the SAME batch, split across two operators/dates.
	// Nothing in these rows distinguishes which goat belongs to which arm.
	execPI(t, ctx, pool, "split arm A",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
		   physical_shed, partition_label, animal_count, capacity_status, vaccine_rule_ids, total_doses)
		 VALUES ($1, $2, DATE '2026-06-24', $3, $4, $5, 'Process Shed', '2', 40, 'within_cap', ARRAY[$6::uuid], 40)`,
		piTenant, piBatch, piOperator, piPark, piShed, piRule)
	execPI(t, ctx, pool, "split arm B",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
		   physical_shed, partition_label, animal_count, capacity_status, vaccine_rule_ids, total_doses)
		 VALUES ($1, $2, DATE '2026-06-25', $3, $4, $5, 'Process Shed', '2', 7, 'over_cap_required', ARRAY[$6::uuid], 7)`,
		piTenant, piBatch, piOperatorTwo, piPark, piShed, piRule)

	repo := NewRepository(pool, 10*time.Second)
	result, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListRows() error = %v", err)
	}
	row := rowByBatchSubstr(result.Rows, piBatch)
	if row == nil {
		t.Fatalf("batch row missing: %#v", result.Rows)
	}

	// Deterministic documented representative: earliest planned_date arm.
	if row.Owner.OperatorID == nil || *row.Owner.OperatorID != piOperator {
		t.Errorf("representative operator id = %v want %s (earliest split arm)", row.Owner.OperatorID, piOperator)
	}
	gotDate := row.DueAt.In(biztime.DefaultLocation()).Format("2006-01-02")
	if gotDate != "2026-06-24" {
		t.Errorf("representative execution date = %s want 2026-06-24 (earliest split arm)", gotDate)
	}

	// Explicit split indicator: BOTH arms are reported, not one arbitrary arm.
	if row.DriveAvailableOperators != 2 {
		t.Errorf("drive_available_operators = %d want 2 (same-partition split across two operators must be explicit, not silently collapsed to one)", row.DriveAvailableOperators)
	}
	if row.DriveAnimalsAssigned != 47 {
		t.Errorf("drive_animals_assigned = %d want 47 (40+7 across the split cohort, not one arm)", row.DriveAnimalsAssigned)
	}
	// Worst capacity status across the split cohort wins (safe direction).
	if row.DriveCapacityState != domain.DriveCapacityStateOverCapRequired {
		t.Errorf("drive_capacity_state = %q want %q (worst status across split cohort)", row.DriveCapacityState, domain.DriveCapacityStateOverCapRequired)
	}
	// Cap is summed per DISTINCT operator across the cohort: op1 has no position row (falls back to
	// the seeded tenant vaccination_capacity_config max_per_day = 100), op2 has an explicit 150 cap.
	if row.DriveOperatorCap != 250 {
		t.Errorf("drive_operator_cap = %d want 250 (tenant-config 100 for the uncapped operator + 150 for the capped one across the split cohort)", row.DriveOperatorCap)
	}
}

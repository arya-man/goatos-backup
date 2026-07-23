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

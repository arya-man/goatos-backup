package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// The Full Schedule named one pen two ways (2026-09-26): an assignment snapshot stores whatever
// label was current when it was written ("Part 2", "1"), so the schedule read "Old Yashoda - Part 1"
// / "Gandhi - Part 2" while the pen board, reading the partition catalog, read "Old Yashoda 1" /
// "Gandhi 2". The displayed label is now the CATALOG's, matched on the normalized key the query
// already groups by; a partition the catalog does not know keeps its stored label.
//
// Grain: shed_partitions is keyed (tenant_id, shed_id, normalized_label), the same key the query
// derives as partition_key, so the LEFT JOIN is 0..1 per assignment row and cannot fan out -- the
// row count and every animal count below are the pre-join values.
func TestDriveAssignmentsLabelThePenFromTheCatalogOneToManyPageBoundaryDateShiftParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	execProjectionSQL(t, ctx, pool, "catalog partitions", `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source)
VALUES ($1,$2,'2','2','manual'), ($1,$2,'Part 3','3','manual')`, testTenant, testShed)
	const labelBatch = "78000000-0000-4000-8000-0000000000b1"
	insertProjectionBatch(t, ctx, pool, labelBatch, "planned")
	execProjectionSQL(t, ctx, pool, "three snapshot assignments", `
INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses)
VALUES ('78000000-0000-4000-8000-000000000001',$1,$5,DATE '2026-12-23',$2,$3,$4,'Label Shed','Part 2',7,ARRAY['78000000-0000-4000-8000-0000000000a1'::uuid],7),
       ('78000000-0000-4000-8000-000000000002',$1,$5,DATE '2026-12-23',$2,$3,$4,'Label Shed','3',5,ARRAY['78000000-0000-4000-8000-0000000000a1'::uuid],5),
       ('78000000-0000-4000-8000-000000000003',$1,$5,DATE '2026-12-23',$2,$3,$4,'Label Shed','4',3,ARRAY['78000000-0000-4000-8000-0000000000a1'::uuid],3)`,
		testTenant, testOperator, testPark, testShed, labelBatch)

	repo := NewRepository(pool, 5*time.Second)
	rows, err := repo.DriveAssignments(ctx, domain.DriveAssignmentQuery{
		TenantID:   testTenant,
		ParkID:     strPtr(testPark),
		MonthStart: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		Limit:      50,
	})
	if err != nil {
		t.Fatalf("DriveAssignments: %v", err)
	}
	got := map[string]int{}
	for _, row := range rows {
		if row.PlannedDate == "2026-12-23" && row.PhysicalShed == "Label Shed" {
			got[row.PartitionLabel] = row.Animals
		}
	}
	want := map[string]int{"2": 7, "Part 3": 5, "4": 3}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v (the catalog join must not add or drop an assignment)", got, want)
	}
	for label, animals := range want {
		if got[label] != animals {
			t.Fatalf("rows = %v, want %v (the catalog's label wins over the stored snapshot; an uncatalogued one keeps its own)", got, want)
		}
	}

	// Park scope: the catalog join is keyed on the assignment's own shed, so scoping the read to
	// another park still returns none of these rows (it cannot pull a pen in through the catalog).
	otherPark := "78000000-0000-4000-8000-0000000000c1"
	scoped, err := repo.DriveAssignments(ctx, domain.DriveAssignmentQuery{
		TenantID:   testTenant,
		ParkID:     strPtr(otherPark),
		MonthStart: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		Limit:      50,
	})
	if err != nil {
		t.Fatalf("DriveAssignments other park: %v", err)
	}
	for _, row := range scoped {
		if row.PhysicalShed == "Label Shed" {
			t.Fatalf("another park's read returned %q %q", row.PhysicalShed, row.PartitionLabel)
		}
	}
}

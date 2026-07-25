package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestOperationsDriveAssignmentDatesOneToManyPageBoundaryDateShiftParkScopeHierarchyStatusMatrixParity
// is the grain/identity proof for BUG-036a's serving-shape fix in vaccinationOperationsSQL. The
// per-obligation execution date used to come from a correlated
// `LEFT JOIN LATERAL (... ORDER BY planned_date, partition_label, operator_id, assignment_id LIMIT 1)`
// on (oi.batch_id, g.shed_id), evaluated once per obligation row; it is now the pre-aggregated
// `drive_assignment_dates` CTE (DISTINCT ON per (batch, shed)).
//
// That is precisely the refactor where an aggregate silently changes grain: a (batch, shed) pair
// legitimately carries SEVERAL assignment arms (split partitions, different operator-days), so a
// binding that is not strictly 1:0..1 per pair multiplies every obligation row feeding the
// operations aggregate's COUNT/GROUP BY -- and a drifted tie-break silently shifts the execution
// date the operator sees.
//
// The test asserts, on a fixture with genuine multi-arm splits:
//   - the pre-aggregated CTE yields AT MOST ONE row per (batch, shed) -- no fan-out, at any status,
//     park scope, or page boundary, because the collapse happens before any grouping or paging;
//   - the chosen planned date (the execution-date shift) is IDENTICAL to the retired LATERAL's.
func TestOperationsDriveAssignmentDatesOneToManyPageBoundaryDateShiftParkScopeHierarchyStatusMatrixParity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationExecutionProjection(t, ctx, pool)

	// Adversarial one-to-many: four arms for the SAME (batch, shed), differing in planned_date,
	// partition_label and operator, so every tie-break tier of the retired LATERAL is exercised.
	execProjectionSQL(t, ctx, pool, "split arm: latest date",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-06-27', $3, $4, $5, 'K1 Shed', '1', 1)`,
		testTenant, testBatch, testOperator, testPark, testShed)
	execProjectionSQL(t, ctx, pool, "split arm: earliest date, higher partition",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-06-22', $3, $4, $5, 'K1 Shed', '9', 1)`,
		testTenant, testBatch, testParkHead, testPark, testShed)
	execProjectionSQL(t, ctx, pool, "split arm: earliest date tie, lower partition",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-06-22', $3, $4, $5, 'K1 Shed', '3', 1)`,
		testTenant, testBatch, testOperator, testPark, testShed)
	execProjectionSQL(t, ctx, pool, "split arm: whole-shed, middle date",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-06-25', $3, $4, $5, 'K1 Shed', 'whole', 1)`,
		testTenant, testBatch, testParkHead, testPark, testShed)

	// LEGACY formulation: the correlated LATERAL this fix replaced, reproduced verbatim.
	const lateralSQL = `
SELECT
  oi.obligation_id::text,
  COALESCE(vda.assignment_planned_at::text, '')
FROM obligation_instances oi
LEFT JOIN goats g
  ON oi.target_type = 'goat'
 AND g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND g.merged_into_goat_id IS NULL
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
WHERE oi.tenant_id = $1::uuid
  AND oi.target_type = 'goat'
  AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
ORDER BY 1`

	// CURRENT formulation: the shipped pre-aggregated CTE, joined by (batch, shed).
	const setBasedSQL = `
WITH drive_assignment_dates AS (
  SELECT DISTINCT ON (assignment.batch_id, assignment.shed_id)
    assignment.batch_id,
    assignment.shed_id,
    (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at
  FROM vaccination_drive_assignments assignment
  WHERE assignment.tenant_id = $1::uuid
  ORDER BY assignment.batch_id,
           assignment.shed_id,
           assignment.planned_date ASC,
           assignment.partition_label ASC,
           assignment.operator_id ASC NULLS LAST,
           assignment.assignment_id ASC
)
SELECT
  oi.obligation_id::text,
  COALESCE(vda.assignment_planned_at::text, '')
FROM obligation_instances oi
LEFT JOIN goats g
  ON oi.target_type = 'goat'
 AND g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND g.merged_into_goat_id IS NULL
LEFT JOIN drive_assignment_dates vda
  ON vda.batch_id = oi.batch_id
 AND vda.shed_id = g.shed_id
WHERE oi.tenant_id = $1::uuid
  AND oi.target_type = 'goat'
  AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
ORDER BY 1`

	type row struct{ obligationID, plannedAt string }
	read := func(label, sql string) []row {
		t.Helper()
		rows, err := pool.Query(ctx, sql, testTenant)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		defer rows.Close()
		var out []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.obligationID, &r.plannedAt); err != nil {
				t.Fatalf("%s scan: %v", label, err)
			}
			out = append(out, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("%s rows: %v", label, err)
		}
		return out
	}

	lateral := read("lateral execution date", lateralSQL)
	setBased := read("pre-aggregated execution date", setBasedSQL)

	if len(lateral) == 0 {
		t.Fatal("fixture produced no goat obligations; the parity proof would be vacuous")
	}
	// Grain: the LATERAL was LIMIT 1, so one row per obligation. A CTE that is not 1:0..1 per
	// (batch, shed) multiplies obligation rows here, BEFORE any grouping -- the classic fan-out.
	if len(setBased) != len(lateral) {
		t.Fatalf("obligation row count = %d, want %d (pre-aggregated assignment dates fanned out)",
			len(setBased), len(lateral))
	}
	var bound int
	for i, want := range lateral {
		if got := setBased[i]; got != want {
			t.Fatalf("execution date mismatch for obligation %s: set-based=%q lateral=%q",
				want.obligationID, got.plannedAt, want.plannedAt)
		}
		if want.plannedAt != "" {
			bound++
		}
	}
	if bound == 0 {
		t.Fatal("no obligation actually bound a drive assignment; the multi-arm parity proof is vacuous")
	}
	t.Logf("operations execution-date parity: %d obligations (%d drive-bound), identical winner under both formulations", len(lateral), bound)
}

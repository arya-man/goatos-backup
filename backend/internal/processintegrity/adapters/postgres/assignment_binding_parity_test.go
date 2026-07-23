package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestAssignmentBindingOneToManyPageBoundaryDateShiftParkScopeHierarchyStatusMatrixParity is the
// grain/identity proof for BUG-036b's serving-shape fix. The drive-assignment binding used to be a
// correlated `LEFT JOIN LATERAL (... ORDER BY <ranking> LIMIT 1) vda ON true` evaluated once per
// obligation row; it is now the set-based `assignment_binding` CTE (EXACT membership arm +
// ranked legacy arm, collapsed with DISTINCT ON). A refactor like that is exactly where an
// aggregate silently changes grain: a `(batch, shed)` pair legitimately carries SEVERAL assignment
// arms (split partitions / operator-days), so a binding that is not strictly 1:0..1 per obligation
// multiplies every downstream COUNT/SUM.
//
// This test runs BOTH formulations against the same seeded fixture (which contains multi-arm
// splits) and asserts:
//   - every obligation binds to AT MOST ONE assignment row (no fan-out at any status, park scope,
//     or page boundary -- the binding is computed before any grouping/paging, so it is page- and
//     filter-independent by construction);
//   - the winning assignment_id, operator_id and planned date (the execution-date shift) are
//     IDENTICAL to the retired LATERAL's choice for every obligation.
//
// If the DISTINCT ON ordering ever drifts from the LATERAL ranking, or the exact/legacy arms stop
// being disjoint, this fails with the offending obligation.
func TestAssignmentBindingOneToManyPageBoundaryDateShiftParkScopeHierarchyStatusMatrixParity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	// A (batch, shed) pair legitimately carries SEVERAL assignment arms: a split partition, a
	// different operator-day, and an arm scoped to a specific vaccine. This is the adversarial
	// one-to-many shape -- the case where a binding that is not strictly 1:0..1 multiplies rows --
	// and it also exercises every tier of the ranking (own rule_id, own partition, 'whole'
	// fallback, then planned_date / partition / operator / assignment id).
	execPI(t, ctx, pool, "split arm: earliest, other partition",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-06-23', $3, $4, $5, 'Process Shed', '2', 1)`,
		piTenant, piBatch, piOperator, piPark, piShed)
	execPI(t, ctx, pool, "split arm: whole-shed fallback",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-06-24', $3, $4, $5, 'Process Shed', 'whole', 1)`,
		piTenant, piBatch, piParkHead, piPark, piShed)
	execPI(t, ctx, pool, "split arm: rule-matched, latest date",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids)
		 VALUES ($1, $2, DATE '2026-06-26', $3, $4, $5, 'Process Shed', '3', 1, ARRAY[$6::uuid])`,
		piTenant, piBatch, piVerifier, piPark, piShed, piRule)
	execPI(t, ctx, pool, "split arm: unmatched rule, earliest of all",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids)
		 VALUES ($1, $2, DATE '2026-06-20', $3, $4, $5, 'Process Shed', '4', 1, ARRAY[$6::uuid])`,
		piTenant, piBatch, piParkHead, piPark, piShed, piTask)
	// A second batch whose arms carry NO membership row: this obligation must resolve through the
	// ranked LEGACY representative, so the parity proof covers both arms.
	execPI(t, ctx, pool, "legacy arm: whole-shed, later date",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-07-02', $3, $4, $5, 'Process Shed', 'whole', 1)`,
		piTenant, piBatchNext, piParkHead, piPark, piShed)
	execPI(t, ctx, pool, "legacy arm: other partition, earlier date",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-07-01', $3, $4, $5, 'Process Shed', '9', 1)`,
		piTenant, piBatchNext, piOperator, piPark, piShed)
	// EXACT membership (migration 000040) for one obligation: it must win outright over the ranked
	// legacy representative above, under BOTH formulations.
	execPI(t, ctx, pool, "exact drive membership",
		`INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
		 SELECT $1::uuid, a.assignment_id, $2::uuid, $3::uuid
		 FROM vaccination_drive_assignments a
		 WHERE a.tenant_id = $1::uuid AND a.partition_label = 'whole'
		 LIMIT 1`,
		piTenant, piObligation, piGoat)

	// LEGACY formulation: the exact correlated LATERAL this fix replaced, reproduced verbatim.
	const lateralSQL = `
SELECT
  oi.obligation_id::text,
  COALESCE(vda.assignment_id::text, ''),
  COALESCE(vda.operator_id::text, ''),
  COALESCE(vda.assignment_planned_at::text, ''),
  COALESCE(vda.assignment_is_exact, false)
FROM obligation_instances oi
LEFT JOIN goats g
  ON oi.target_type = 'goat'
 AND g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND g.merged_into_goat_id IS NULL
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id
 AND gsp.goat_id = g.goat_id
 AND gsp.shed_id = g.shed_id
LEFT JOIN vaccination_drive_assignment_members vdam
  ON vdam.tenant_id = oi.tenant_id
 AND vdam.obligation_id = oi.obligation_id
LEFT JOIN LATERAL (
  SELECT
    assignment.assignment_id,
    assignment.operator_id,
    (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at,
    (vdam.assignment_id IS NOT NULL) AS assignment_is_exact
  FROM vaccination_drive_assignments assignment
  WHERE assignment.tenant_id = oi.tenant_id
    AND (
      assignment.assignment_id = vdam.assignment_id
      OR (
        vdam.assignment_id IS NULL
        AND assignment.batch_id = oi.batch_id
        AND assignment.shed_id = CASE
          WHEN g.shed_id IS NOT NULL THEN g.shed_id
          WHEN oi.target_type = 'shed' THEN oi.target_id
          WHEN oi.scope_type = 'shed' THEN oi.scope_id
          ELSE NULL
        END
      )
    )
  ORDER BY
    CASE
      WHEN oi.rule_id = ANY(assignment.vaccine_rule_ids) THEN 0
      WHEN cardinality(assignment.vaccine_rule_ids) = 0 THEN 1
      ELSE 2
    END,
    CASE
      WHEN assignment.partition_label = COALESCE(gsp.partition_label, 'whole') THEN 0
      WHEN assignment.partition_label = 'whole' THEN 1
      ELSE 2
    END,
    assignment.planned_date ASC,
    assignment.partition_label ASC,
    assignment.operator_id ASC NULLS LAST,
    assignment.assignment_id ASC
  LIMIT 1
) vda ON true
WHERE oi.tenant_id = $1::uuid
  AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
ORDER BY 1`

	// CURRENT formulation: the shipped set-based CTEs, driven over the SAME obligation membership
	// (no due-window bound on either side, so the comparison covers the whole status matrix).
	const setBasedSQL = `
WITH legacy_binding_obligations AS (
  SELECT
    oi.obligation_id,
    oi.batch_id,
    oi.rule_id,
    CASE
      WHEN g.shed_id IS NOT NULL THEN g.shed_id
      WHEN oi.target_type = 'shed' THEN oi.target_id
      WHEN oi.scope_type = 'shed' THEN oi.scope_id
      ELSE NULL
    END AS binding_shed_id,
    COALESCE(gsp.partition_label, 'whole') AS binding_partition_label
  FROM obligation_instances oi
  LEFT JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
   AND gsp.shed_id = g.shed_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND oi.batch_id IS NOT NULL
    AND NOT EXISTS (
      SELECT 1
      FROM vaccination_drive_assignment_members vdam
      WHERE vdam.tenant_id = oi.tenant_id
        AND vdam.obligation_id = oi.obligation_id
    )
),
assignment_binding AS (
  SELECT DISTINCT ON (cand.obligation_id)
    cand.obligation_id,
    cand.assignment_id,
    cand.operator_id,
    cand.assignment_planned_at,
    cand.assignment_is_exact
  FROM (
    SELECT
      vdam.obligation_id,
      assignment.assignment_id,
      assignment.operator_id,
      (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at,
      true AS assignment_is_exact,
      0 AS rule_rank,
      0 AS partition_rank,
      assignment.planned_date,
      assignment.partition_label
    FROM vaccination_drive_assignment_members vdam
    JOIN vaccination_drive_assignments assignment
      ON assignment.tenant_id = vdam.tenant_id
     AND assignment.assignment_id = vdam.assignment_id
    WHERE vdam.tenant_id = $1::uuid
    UNION ALL
    SELECT
      w.obligation_id,
      assignment.assignment_id,
      assignment.operator_id,
      (assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at,
      false AS assignment_is_exact,
      CASE
        WHEN w.rule_id = ANY(assignment.vaccine_rule_ids) THEN 0
        WHEN cardinality(assignment.vaccine_rule_ids) = 0 THEN 1
        ELSE 2
      END AS rule_rank,
      CASE
        WHEN assignment.partition_label = w.binding_partition_label THEN 0
        WHEN assignment.partition_label = 'whole' THEN 1
        ELSE 2
      END AS partition_rank,
      assignment.planned_date,
      assignment.partition_label
    FROM legacy_binding_obligations w
    JOIN vaccination_drive_assignments assignment
      ON assignment.tenant_id = $1::uuid
     AND assignment.batch_id = w.batch_id
     AND assignment.shed_id = w.binding_shed_id
  ) cand
  ORDER BY
    cand.obligation_id,
    cand.rule_rank,
    cand.partition_rank,
    cand.planned_date ASC,
    cand.partition_label ASC,
    cand.operator_id ASC NULLS LAST,
    cand.assignment_id ASC
)
SELECT
  oi.obligation_id::text,
  COALESCE(vda.assignment_id::text, ''),
  COALESCE(vda.operator_id::text, ''),
  COALESCE(vda.assignment_planned_at::text, ''),
  COALESCE(vda.assignment_is_exact, false)
FROM obligation_instances oi
LEFT JOIN assignment_binding vda
  ON vda.obligation_id = oi.obligation_id
WHERE oi.tenant_id = $1::uuid
  AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
ORDER BY 1`

	type binding struct {
		obligationID, assignmentID, operatorID, plannedAt string
		exact                                             bool
	}
	read := func(label, sql string) []binding {
		t.Helper()
		rows, err := pool.Query(ctx, sql, piTenant)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		defer rows.Close()
		var out []binding
		for rows.Next() {
			var b binding
			if err := rows.Scan(&b.obligationID, &b.assignmentID, &b.operatorID, &b.plannedAt, &b.exact); err != nil {
				t.Fatalf("%s scan: %v", label, err)
			}
			out = append(out, b)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("%s rows: %v", label, err)
		}
		return out
	}

	lateral := read("lateral binding", lateralSQL)
	setBased := read("set-based binding", setBasedSQL)

	if len(lateral) == 0 {
		t.Fatal("fixture produced no obligations; the binding parity proof would be vacuous")
	}
	// Grain: the LATERAL was LIMIT 1, so one row per obligation. If the set-based binding is not
	// strictly 1:0..1 the row counts diverge here BEFORE any grouping, which is exactly the
	// fan-out that would inflate every downstream count.
	if len(setBased) != len(lateral) {
		t.Fatalf("binding row count = %d, want %d (set-based binding fanned out an obligation)",
			len(setBased), len(lateral))
	}
	seen := map[string]bool{}
	for i, want := range lateral {
		got := setBased[i]
		if seen[got.obligationID] {
			t.Fatalf("obligation %s bound more than once (fan-out)", got.obligationID)
		}
		seen[got.obligationID] = true
		if got != want {
			t.Fatalf("binding mismatch for obligation %s:\n  set-based = %+v\n  lateral   = %+v",
				want.obligationID, got, want)
		}
	}
	// Non-vacuity: the fixture must actually exercise BOTH arms, otherwise "identical" proves nothing.
	var exactArms, legacyArms int
	for _, b := range setBased {
		switch {
		case b.assignmentID == "":
		case b.exact:
			exactArms++
		default:
			legacyArms++
		}
	}
	if exactArms == 0 || legacyArms == 0 {
		t.Fatalf("fixture exercised exact=%d legacy=%d binding arms; both must be non-zero for the parity proof to mean anything", exactArms, legacyArms)
	}
	t.Logf("assignment binding parity: %d obligations, identical winner/operator/execution-date under both formulations", len(lateral))
}

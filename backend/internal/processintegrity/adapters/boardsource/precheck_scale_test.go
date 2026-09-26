package boardsource

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestVaccinationDueWorkPrecheckQueryPlanUsesIndexesAtScale is the at-scale plan proof for
// vaccinationDueWorkPrecheckSQL (the Work Board's "any vaccination work today?" gate, whose due_at
// arm now counts UNBATCHED obligations only). It loads a 50k-animal-envelope obligation history --
// 5,000 goats x 100 obligations = 500k obligation_instances over two years, a third of them batched
// into 700 past drives, each drive with its assignment and one member row per batched obligation --
// ANALYZEs, and EXPLAIN ANALYZEs the exact production SQL for the common empty day, a drive day, and
// both completed-work modes. No arm may seq-scan obligation_instances, goats or the member table.
func TestVaccinationDueWorkPrecheckQueryPlanUsesIndexesAtScale(t *testing.T) {
	if os.Getenv("GOATOS_SCALE_CERT") == "" {
		t.Skip("scale certification gate — set GOATOS_SCALE_CERT=1 (make scale-cert)")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedDrivePen(t, ctx, pool)

	execST(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, custodian_party_id, park_id, shed_id, current_location_id, origin_type, dob, entry_date, management_stage)
SELECT gen_random_uuid(), $1::uuid, 'G-9' || lpad(i::text, 6, '0'), 'goat', 'female', 'alive', $2::uuid,
       CASE WHEN i % 2 = 0 THEN $3::uuid ELSE $4::uuid END, $5::uuid, $5::uuid, 'procured', DATE '2024-01-01', DATE '2024-01-01', 'adult'
FROM generate_series(1, 5000) i`, stTenant, stParty, stPark, stOtherPk, stShed)
	execST(t, ctx, pool, `
INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date)
SELECT gen_random_uuid(), $1::uuid, $2::uuid, 'shed', $3::uuid, 'completed', DATE '2024-06-01' + i
FROM generate_series(1, 700) i`, stTenant, stVersion, stShed)
	execST(t, ctx, pool, `
INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
SELECT gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid,
       CASE WHEN i % 3 = 0 THEN b.batch_id END,
       'goat', g.goat_id, 'shed', $4::uuid,
       TIMESTAMPTZ '2024-06-01 00:00+05:30' + ((i * 7 + g.n) % 730) * interval '1 day',
       CASE WHEN i % 5 = 0 THEN 'canceled' ELSE 'completed' END,
       'scale-' || i || '-' || g.goat_id, 100 + i
FROM generate_series(1, 100) i
CROSS JOIN (SELECT goat_id, row_number() OVER (ORDER BY goat_id) AS n FROM goats WHERE tenant_id = $1::uuid AND display_id LIKE 'G-9%') g
LEFT JOIN LATERAL (
  SELECT batch_id FROM obligation_batches
  WHERE tenant_id = $1::uuid AND planned_date = DATE '2024-06-01' + (1 + (i * 7 + g.n) % 700)::int
) b ON true`, stTenant, stVersion, stRule, stShed)
	execST(t, ctx, pool, `
INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
SELECT gen_random_uuid(), $1::uuid, ob.batch_id, ob.planned_date, $2::uuid, $3::uuid, $4::uuid, 'Godel 1', 'whole', 1
FROM obligation_batches ob
WHERE ob.tenant_id = $1::uuid AND ob.status = 'completed'`, stTenant, stMember, stPark, stShed)
	execST(t, ctx, pool, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT oi.tenant_id, vda.assignment_id, oi.obligation_id, oi.target_id
FROM obligation_instances oi
JOIN vaccination_drive_assignments vda ON vda.tenant_id = oi.tenant_id AND vda.batch_id = oi.batch_id
WHERE oi.tenant_id = $1::uuid AND oi.idempotency_key LIKE 'scale-%'`, stTenant)
	for _, table := range []string{"obligation_instances", "obligation_batches", "vaccination_drive_assignments", "vaccination_drive_assignment_members", "goats", "protocol_versions", "protocol_definitions"} {
		execST(t, ctx, pool, `ANALYZE `+table)
	}
	var obligations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM obligation_instances WHERE tenant_id = $1::uuid`, stTenant).Scan(&obligations); err != nil || obligations < 500_000 {
		t.Fatalf("seeded %d obligations (err %v), want >= 500k", obligations, err)
	}

	for _, tc := range []struct {
		name, day        string
		includeCompleted bool
	}{
		{"empty day (future, no drive)", "2026-11-20", true},
		{"drive day in the history", "2025-03-10", true},
		{"drive day, open work only", "2025-03-10", false},
		{"seeded drive day", stDate, true},
	} {
		plan := pgtest.ExplainAnalyzeAtScale(t, ctx, pool, vaccinationDueWorkPrecheckSQL, precheckArgs(stPark, tc.day, tc.includeCompleted)...)
		plan.AssertNoSeqScan(t, "vaccinationDueWorkPrecheckSQL @500k obligations, "+tc.name, 200,
			"obligation_instances", "goats", "vaccination_drive_assignment_members")
	}
}

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestComboAlignKeysetQueryUsesPartialIndex is the R2-06b guard: the AlignComboDrives keyset query
// (ListPlannedComboBatchesKeyset) must be served by the partial composite index added in
// 000209_combo_align_keyset_index.sql, NOT a tenant-wide sequential scan + sort. It seeds a
// realistically large planned-combo batch table and asserts the EXPLAIN plan for the query's outer
// scan uses obligation_batches_combo_align_keyset_idx with no Seq Scan on obligation_batches.
func TestComboAlignKeysetQueryUsesPartialIndex(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.combo.index", Name: "ComboIndex", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}

	// Bulk-seed a large planned combo batch table (distinct session per row avoids the unfinalized
	// planned-unique index), plus non-matching rows so the partial index is clearly the cheap path.
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date, status, sop_task_id, context)
SELECT $1::uuid, $2::uuid, 'tenant', $1::uuid, 'combo:v'||g, DATE '2026-08-01', 'planned', NULL, '{}'::jsonb
FROM generate_series(1, 4000) g`, tenantID, versionID); err != nil {
		t.Fatalf("seed combo batches: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date, status, sop_task_id, context)
SELECT $1::uuid, $2::uuid, 'tenant', $1::uuid, 'shed:v'||g, DATE '2026-08-01', 'completed', NULL, '{}'::jsonb
FROM generate_series(1, 4000) g`, tenantID, versionID); err != nil {
		t.Fatalf("seed non-combo batches: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE obligation_batches`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// EXPLAIN the query's outer scan (the part the index must serve; the LATERAL target_ids aggregate
	// does not influence the outer index choice). EXPLAIN returns one row per plan line.
	rows, err := pool.Query(ctx, `
EXPLAIN (FORMAT TEXT)
SELECT b.batch_id
FROM obligation_batches b
WHERE b.tenant_id = $1
  AND b.status = 'planned'
  AND b.session LIKE 'combo:%'
  AND b.sop_task_id IS NULL
  AND NOT (b.context ? 'stock_reservation')
  AND (b.planned_date IS NULL OR b.planned_date <= $2::date)
ORDER BY b.scope_type, b.scope_id, b.session, b.batch_id
LIMIT 1000`, tenantID, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatalf("scan explain line: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	rows.Close()
	plan := strings.Join(lines, "\n")

	if !strings.Contains(plan, "obligation_batches_combo_align_keyset_idx") {
		t.Fatalf("keyset query does not use the partial index. Plan:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on obligation_batches") {
		t.Fatalf("keyset query still does a sequential scan of obligation_batches. Plan:\n%s", plan)
	}
}

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestOversightAnalyticsQueryPlanUsesIndexesAtScale is the at-scale plan proof (PP-22) for the
// oversight reads this PR changed over verification_items: oversightPendingByModuleSQL (in-sample
// pending backlog), oversightVerdictWindowsSQL (7/30-day human verdicts) and oversightIntegritySQL
// (14-day watch integrity over the 000431 per-(item, actor) summary).
//
// ~500k items over a year (~1.4k a day, 8 verifiers, 1 in 5 settled by the sampling closeout, a
// three-day pending tail) plus a watch-summary row for most decided items are bulk-loaded, VACUUM
// ANALYZEd, and each exact production statement is EXPLAIN (ANALYZE)'d WITHOUT enable_seqscan=off.
// None may Seq Scan verification_items, each must read at most its own time window (30 days is
// ~41k items, never the ~500k table), and each must execute under the 200ms budget.
func TestOversightAnalyticsQueryPlanUsesIndexesAtScale(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedOversightTenant(t, ctx, pool)
	seedOversightItemsAtScale(t, ctx, pool)

	for _, st := range []struct {
		name, sql string
		ceiling   float64
	}{
		{"oversightPendingByModuleSQL", oversightPendingByModuleSQL, 10_000},
		{"oversightVerdictWindowsSQL", oversightVerdictWindowsSQL, 60_000},
		{"oversightIntegritySQL", oversightIntegritySQL, 60_000},
	} {
		plan, ms := explainAnalyzeOversight(t, ctx, pool, st.sql, oversightTestTenantID)
		if plan.ActualRows == 0 {
			t.Errorf("%s returned no rows: the plan proves nothing", st.name)
		}
		assertOversightIndexBound(t, st.name, plan, ms, st.ceiling)
	}
}

func seedOversightItemsAtScale(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(what, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	// g = 1 is the newest item; each step is ~63s back, so 500k items span a year.
	exec("items", `
INSERT INTO verification_items (
  item_id, tenant_id, vertical, module, category, source_module,
  source_ref_type, source_ref_id, status, verdict_reason, verified_by, verified_at, auto_resolution,
  captured_at, idempotency_key)
SELECT gen_random_uuid(), $1::uuid, 'preventive_care', m, m || '_proof', m,
       'sop_submission', gen_random_uuid(),
       CASE WHEN pending THEN 'pending' WHEN auto OR g % 10 <> 0 THEN 'approved' ELSE 'rejected' END,
       CASE WHEN NOT pending AND NOT auto AND g % 10 = 0 THEN 'seeded rejection' END,
       CASE WHEN NOT pending AND NOT auto THEN md5('verifier|' || (g % 8)::text)::uuid END,
       CASE WHEN NOT pending THEN cap + interval '6 hours' END,
       CASE WHEN auto AND NOT pending THEN 'not_sampled' END,
       cap, 'oversight-scale:' || g
FROM generate_series(1, 500000) AS g,
     LATERAL (SELECT now() - g * interval '63 seconds' AS cap,
                     (ARRAY['vaccination', 'feed', 'weighing', 'shifting', 'health'])[1 + g % 5] AS m) x,
     LATERAL (SELECT g <= 4000 AND g % 4 = 0 AS pending, g % 5 = 1 AS auto) y`, oversightTestTenantID)
	exec("watch summary", `
INSERT INTO verification_review_item_watch (tenant_id, item_id, actor_id, opened, played, max_position_ms, max_duration_ms)
SELECT tenant_id, item_id, verified_by, true, (hashtext(item_id::text) % 7) <> 0, 54000, 60000
FROM verification_items
WHERE tenant_id = $1::uuid AND verified_by IS NOT NULL AND (hashtext(item_id::text) % 10) <> 0`, oversightTestTenantID)
	// VACUUM, not only ANALYZE: a live table has set hint bits and a visibility map; a freshly
	// bulk-loaded one makes the first read pay for writing them, which is a fixture artefact.
	exec("vacuum analyze", `VACUUM (ANALYZE) verification_items, verification_review_item_watch, verification_sampling_policies`)
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM verification_items`).Scan(&n); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if n < 500_000 {
		t.Fatalf("at-scale fixture has %d items, want >= 500k", n)
	}
}

type oversightPlanNode struct {
	NodeType     string              `json:"Node Type"`
	RelationName string              `json:"Relation Name"`
	ActualRows   float64             `json:"Actual Rows"`
	ActualLoops  float64             `json:"Actual Loops"`
	Plans        []oversightPlanNode `json:"Plans"`
}

func explainAnalyzeOversight(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) (oversightPlanNode, float64) {
	t.Helper()
	// Production API pools run with jit=off (platform/postgres), so the gate does too: JIT compile
	// time is not part of the served latency. One un-timed run first warms the page cache, so the
	// budget measures the plan, not the fixture's cold disk; the plan assertions hold either way.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
		t.Fatalf("jit off: %v", err)
	}
	warm, err := tx.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("warm run: %v", err)
	}
	warm.Close()
	// The budget is judged on the best of three executions: the OCI test server is shared, and one
	// slow run there is contention, not the plan. The plan itself is the same on every run.
	var best oversightPlanNode
	bestMS := -1.0
	for run := 0; run < 3; run++ {
		var raw []byte
		if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...).Scan(&raw); err != nil {
			t.Fatalf("explain: %v", err)
		}
		var out []struct {
			Plan          oversightPlanNode `json:"Plan"`
			ExecutionTime float64           `json:"Execution Time"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || len(out) == 0 {
			t.Fatalf("decode explain: %v", err)
		}
		if bestMS < 0 || out[0].ExecutionTime < bestMS {
			best, bestMS = out[0].Plan, out[0].ExecutionTime
		}
	}
	return best, bestMS
}

func assertOversightIndexBound(t *testing.T, name string, plan oversightPlanNode, execMS, rowCeiling float64) {
	t.Helper()
	var walk func(n oversightPlanNode)
	walk = func(n oversightPlanNode) {
		if n.RelationName == "verification_items" {
			if strings.Contains(n.NodeType, "Seq Scan") {
				t.Errorf("%s: Seq Scan on verification_items at ~500k rows", name)
			}
			if touched := n.ActualRows * n.ActualLoops; touched > rowCeiling {
				t.Errorf("%s: %s verification_items touched %.0f rows (> %.0f)", name, n.NodeType, touched, rowCeiling)
			}
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	walk(plan)
	if execMS > 200 {
		t.Errorf("%s: execution %.1fms over the 200ms budget at scale", name, execMS)
	}
	t.Logf("%s: %.1fms", name, execMS)
}

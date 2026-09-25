package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// TestFeedAnalyticsAndStockLoadsQueryPlanUsesIndexesAtScale is the at-scale plan proof (PP-22) for
// the two feed analytics reads this PR rewrote over feed_direction_issue_rows:
//
//   - directedAnalyticsCombinedSQL (the directed overview, now over the 000433 per-sheet collapses;
//     raw sheet rows are read only for pens two sheets of one day share), over the widest window the
//     page allows (92 days, both parks);
//   - stockLoadsSQL (purchased vs consumed per load, the window-function FIFO rewrite), whose
//     consumption side is every locked sheet of the tenant since its ledger started.
//
// ~525k sheet rows (2 parks x 365 days: a 110-pen normal sheet and a 10-pen experiment sheet that
// shares its pens with the normal one, 2 sessions x 3 items per pen) plus a year of purchases are
// bulk-loaded through the production triggers (so the 000433 collapses are real), ANALYZEd, and each
// statement is EXPLAIN (ANALYZE)'d with its exact production arguments WITHOUT enable_seqscan=off.
// Neither may Seq Scan feed_direction_issue_rows or read more than a bounded slice of it, and each
// must execute under the 200ms budget.
func TestFeedAnalyticsAndStockLoadsQueryPlanUsesIndexesAtScale(t *testing.T) {
	ctx := context.Background()
	_, pool := setupIssueDB(t, ctx)
	const park2 = "fd100000-0000-4000-8000-000000003002"
	seedFeedSheetsAtScale(t, ctx, pool, park2)

	parks := []uuid.UUID{uuid.MustParse(fdiPark), uuid.MustParse(park2)}

	plan, ms := explainAnalyzeFeed(t, ctx, pool, directedAnalyticsCombinedSQL, fdiTenant, parks, "2026-05-01", "2026-07-31")
	if !assertFeedIndexBound(t, "directedAnalyticsCombinedSQL", plan, ms) {
		logFeedPlanText(t, ctx, pool, directedAnalyticsCombinedSQL, fdiTenant, parks, "2026-05-01", "2026-07-31")
	}
	if plan.ActualRows == 0 {
		t.Error("directedAnalyticsCombinedSQL returned no rows over a bulk window: the plan proves nothing")
	}

	mergeMembers, mergeFamilies, _ := domain.StockFamilyMergeArrays()
	rateFarms, rateFeeds, rateKg := domain.StockRateOverrideArrays()
	plan, ms = explainAnalyzeFeed(t, ctx, pool, stockLoadsSQL, fdiTenant, parks, "", "", 25, 0,
		mergeMembers, mergeFamilies, rateFarms, rateFeeds, rateKg)
	if !assertFeedIndexBound(t, "stockLoadsSQL", plan, ms) {
		logFeedPlanText(t, ctx, pool, stockLoadsSQL, fdiTenant, parks, "", "", 25, 0,
			mergeMembers, mergeFamilies, rateFarms, rateFeeds, rateKg)
	}
	if plan.ActualRows == 0 {
		t.Error("stockLoadsSQL returned no loads: the plan proves nothing")
	}
}

func seedFeedSheetsAtScale(t *testing.T, ctx context.Context, pool *pgxpool.Pool, park2 string) {
	t.Helper()
	exec := func(what, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	exec("second park", `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, park2)
	exec("issues", `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version, locked_at)
SELECT md5('fd-scale|' || p::text || '|' || d::text || '|' || wf)::uuid, $1::uuid, p, d, wf,
       CASE WHEN d < DATE '2026-08-25' THEN 'locked' ELSE 'issued' END, now(),
       'fd-scale|' || p::text || '|' || d::text || '|' || wf, 'fd-scale|' || p::text || '|' || d::text || '|' || wf,
       'fd-scale|' || p::text || '|' || d::text || '|' || wf, 'test', 'feed.direction.sheet', '1',
       CASE WHEN d < DATE '2026-08-25' THEN now() END
FROM unnest(ARRAY[$2::uuid, $3::uuid]) AS p,
     generate_series(DATE '2025-09-01', DATE '2026-08-31', interval '1 day') AS g(dts),
     LATERAL (SELECT dts::date AS d) dd,
     unnest(ARRAY['normal', 'experiment']) AS wf`, fdiTenant, fdiPark, park2)
	// The experiment sheet's 10 pens are pens 1..10 of the normal sheet, so the shared-pen path
	// (the only one that still reads raw rows) is exercised every day.
	exec("sheet rows", `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
SELECT i.tenant_id, i.feed_direction_issue_id, i.park_id, 'P',
       md5('fd-shed|' || i.park_id::text || '|' || s::text)::uuid, 'Pen ' || s, NULL,
       CASE WHEN s % 3 = 0 THEN 'Dry' WHEN s % 3 = 1 THEN 'Pregnant' ELSE 'Kids' END, 'Beetal', 'Beetal',
       ses, CASE WHEN ses = 1 THEN 'Morning' ELSE 'Evening' END,
       10 + s % 40, i.workflow = 'experiment', i.workflow,
       (ARRAY['Mesha Kids Goat Concentrate', 'Hay', 'Silage'])[it], 1.5, 4.5, false, s * 2 + ses, it, false
FROM feed_direction_issues i
CROSS JOIN LATERAL generate_series(1, CASE WHEN i.workflow = 'normal' THEN 110 ELSE 10 END) AS s,
     generate_series(1, 2) AS ses,
     generate_series(1, 3) AS it
WHERE i.tenant_id = $1::uuid AND i.idempotency_key LIKE 'fd-scale|%'`, fdiTenant)
	// A year of loads: one per farm per item every 5 days, all reached.
	exec("purchases", `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status, reached_on, days_of_stock)
SELECT $1::uuid, f.park, f.farm, item, n, d, 2000, 40, 80000, 0, d + 1, 'Navaladi', 'Paid', 'reached', d + 1, 5
FROM (VALUES ($2::uuid, 'CBE'), ($3::uuid, 'CPT')) AS f(park, farm),
     unnest(ARRAY['Mesha Kids Goat Concentrate', 'Hay', 'Silage']) AS item,
     generate_series(0, 72) AS n,
     LATERAL (SELECT DATE '2025-08-25' + n * 5 AS d) dd`, fdiTenant, fdiPark, park2)
	exec("catalog", `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, status)
SELECT $1::uuid, l, 'active' FROM unnest(ARRAY['Mesha Kids Goat Concentrate', 'Hay', 'Silage']) AS l
ON CONFLICT (tenant_id, feed_item_key) DO UPDATE SET status = 'active'`, fdiTenant)
	// VACUUM, not only ANALYZE: a live table has set hint bits and a visibility map; a freshly
	// bulk-loaded one makes the first read pay for writing them, which is a fixture artefact.
	exec("vacuum analyze", `VACUUM (ANALYZE) feed_direction_issues, feed_direction_issue_rows, feed_direction_issue_pens,
  feed_direction_issue_items, feed_direction_issue_tag_items, feed_direction_issue_session_items,
  feed_purchases, feed_item_catalog, feed_sale_depletions`)
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_direction_issue_rows`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n < 500_000 {
		t.Fatalf("at-scale fixture has %d sheet rows, want >= 500k", n)
	}
}

type feedPlanNode struct {
	NodeType     string         `json:"Node Type"`
	RelationName string         `json:"Relation Name"`
	ActualRows   float64        `json:"Actual Rows"`
	ActualLoops  float64        `json:"Actual Loops"`
	Plans        []feedPlanNode `json:"Plans"`
}

func explainAnalyzeFeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) (feedPlanNode, float64) {
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
	var best feedPlanNode
	bestMS := -1.0
	for run := 0; run < 3; run++ {
		var raw []byte
		if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...).Scan(&raw); err != nil {
			t.Fatalf("explain: %v", err)
		}
		var out []struct {
			Plan          feedPlanNode `json:"Plan"`
			ExecutionTime float64      `json:"Execution Time"`
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

// assertFeedIndexBound fails on a Seq Scan of the raw sheet rows, on any scan of them that touched
// more than a bounded slice (rows x loops), and on an execution over the 200ms budget.
func assertFeedIndexBound(t *testing.T, name string, plan feedPlanNode, execMS float64) bool {
	t.Helper()
	ok := true
	const rowCeiling = 50_000.0 // the table is ~525k; the shared-pen slice of 92 days is ~11k
	var walk func(n feedPlanNode)
	walk = func(n feedPlanNode) {
		if n.RelationName == "feed_direction_issue_rows" {
			if strings.Contains(n.NodeType, "Seq Scan") {
				t.Errorf("%s: Seq Scan on feed_direction_issue_rows at ~525k rows", name)
				ok = false
			}
			if touched := n.ActualRows * n.ActualLoops; touched > rowCeiling {
				t.Errorf("%s: %s feed_direction_issue_rows touched %.0f rows (> %.0f)", name, n.NodeType, touched, rowCeiling)
				ok = false
			}
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	walk(plan)
	if execMS > 200 {
		t.Errorf("%s: execution %.1fms over the 200ms budget at scale", name, execMS)
		ok = false
	}
	t.Logf("%s: %.1fms", name, execMS)
	return ok
}

// logFeedPlanText logs the text plan of a statement that failed the gate, so the failure names the
// node that regressed.
func logFeedPlanText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	_, _ = tx.Exec(ctx, "SET LOCAL jit = off")
	rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+sql, args...)
	if err != nil {
		t.Logf("explain text: %v", err)
		return
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if rows.Scan(&line) == nil {
			b.WriteString(line + "\n")
		}
	}
	t.Log("\n" + b.String())
}

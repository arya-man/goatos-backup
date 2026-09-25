package boardsource

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// TestFeedBoardActivityCardsQueryPlanUsesIndexesAtScale is the at-scale plan proof (PP-22) for the
// feed Work Board reads over feed_direction_issue_rows: metricsSQL and subtasksSQL for EVERY activity,
// which compose sheetSessionUnits (packing, direction) and wastageUnits (wastage) -- the exact bound
// statements readCardsFresh / PrimeStatements / ListSubtasks send.
//
// ~525k sheet rows (2 parks x 2 workflows x 365 days x 60 sheds x 2 sessions x 3 items) are
// bulk-loaded, ANALYZEd, and each statement is EXPLAIN (ANALYZE)'d for ONE park-day WITHOUT
// enable_seqscan=off. The plan must reach feed_direction_issue_rows and feed_direction_issues through
// an index (the issue's (tenant, park, feed_day) slice, then the rows' issue-id prefix), touch at
// most one day's sheet (never the ~525k table), and execute well under the 200ms budget.
func TestFeedBoardActivityCardsQueryPlanUsesIndexesAtScale(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	seedFeedBoardSheetsAtScale(t, ctx, pool)

	const day = "2026-06-15" // a bulk day: full sheets for both workflows, D+1 packing too
	for _, a := range activities {
		cardNo := 0 // a one-card activity
		if a.perSession {
			cardNo = 1 // the morning card
		}
		for _, st := range []struct {
			name string
			q    sqlbind.BoundQuery
		}{
			{"metricsSQL/" + a.key, sqlbind.MustBind(metricsSQL(a), bsTenant, day, bsPark)},
			{"subtasksSQL/" + a.key, sqlbind.MustBind(subtasksSQL(a), bsTenant, day, bsPark, -1, "", 51, cardNo)},
		} {
			plan, execMS := explainAnalyzeBoard(t, ctx, pool, st.q.SQL(), st.q.Args()...)
			assertBoardIndexBound(t, st.name, plan, execMS)
			if a.key != "transport" && plan.ActualRows == 0 {
				t.Errorf("%s returned no rows on a bulk day: the plan proves nothing about a real sheet", st.name)
			}
		}
	}
}

// seedFeedBoardSheetsAtScale lays down a year of issued sheets for both parks and both workflows in
// two set-based statements. Days end 2026-08-31, so the hand-seeded 2026-09-10/11 rows never collide.
func seedFeedBoardSheetsAtScale(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version, locked_at)
SELECT md5('bs-scale|' || p::text || '|' || d::text || '|' || wf)::uuid, $1::uuid, p, d, wf,
       CASE WHEN d < DATE '2026-08-25' THEN 'locked' ELSE 'issued' END, now(),
       'bs-scale|' || p::text || '|' || d::text || '|' || wf, 'bs-scale|' || p::text || '|' || d::text || '|' || wf,
       'bs-scale|' || p::text || '|' || d::text || '|' || wf, 'test', 'feed.direction.sheet', '1',
       CASE WHEN d < DATE '2026-08-25' THEN now() END
FROM unnest(ARRAY[$2::uuid, $3::uuid]) AS p,
     generate_series(DATE '2025-09-01', DATE '2026-08-31', interval '1 day') AS g(dts),
     LATERAL (SELECT dts::date AS d) dd,
     unnest(ARRAY['normal', 'experiment']) AS wf`, bsTenant, bsPark, bsOtherPk)
	exec(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
SELECT i.tenant_id, i.feed_direction_issue_id, i.park_id, 'P',
       md5('bs-shed|' || i.park_id::text || '|' || s::text)::uuid, 'Pen ' || s, NULL, 'Dry', 'Beetal', 'Beetal',
       ses, CASE WHEN ses = 1 THEN 'Morning' ELSE 'Evening' END,
       10 + s, i.workflow = 'experiment', i.workflow, 'Item ' || it, 1.5, 4.5, false, s * 2 + ses, it, false
FROM feed_direction_issues i,
     generate_series(1, 60) AS s,
     generate_series(1, 2) AS ses,
     generate_series(1, 3) AS it
WHERE i.tenant_id = $1::uuid AND i.idempotency_key LIKE 'bs-scale|%'`, bsTenant)
	// VACUUM, not only ANALYZE: a live table has set hint bits and a visibility map; a freshly
	// bulk-loaded one makes the first read pay for writing them, which is a fixture artefact.
	exec(t, ctx, pool, `VACUUM (ANALYZE) feed_direction_issues, feed_direction_issue_rows, feed_packing_completions,
  feed_distribution_completions, feed_wastage_completions, feed_transport_tasks, locations`)
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_direction_issue_rows`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n < 500_000 {
		t.Fatalf("at-scale fixture has %d sheet rows, want >= 500k", n)
	}
}

type boardPlanNode struct {
	NodeType     string          `json:"Node Type"`
	RelationName string          `json:"Relation Name"`
	IndexName    string          `json:"Index Name"`
	ActualRows   float64         `json:"Actual Rows"`
	ActualLoops  float64         `json:"Actual Loops"`
	Plans        []boardPlanNode `json:"Plans"`
}

func explainAnalyzeBoard(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) (boardPlanNode, float64) {
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
	var best boardPlanNode
	bestMS := -1.0
	for run := 0; run < 3; run++ {
		var raw []byte
		if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...).Scan(&raw); err != nil {
			t.Fatalf("explain: %v", err)
		}
		var out []struct {
			Plan          boardPlanNode `json:"Plan"`
			ExecutionTime float64       `json:"Execution Time"`
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

// assertBoardIndexBound fails on any Seq Scan of the big sheet tables and on any scan of them that
// touched more than a day's worth of rows (rows x loops), and on an execution over the 200ms budget.
func assertBoardIndexBound(t *testing.T, name string, plan boardPlanNode, execMS float64) {
	t.Helper()
	big := map[string]bool{"feed_direction_issue_rows": true, "feed_direction_issues": true}
	const rowCeiling = 5_000.0 // one park-day sheet is 360 rows per workflow; the table is ~525k
	var walk func(n boardPlanNode)
	walk = func(n boardPlanNode) {
		if big[n.RelationName] {
			if strings.Contains(n.NodeType, "Seq Scan") {
				t.Errorf("%s: Seq Scan on %s at ~525k rows", name, n.RelationName)
			}
			if touched := n.ActualRows * n.ActualLoops; touched > rowCeiling {
				t.Errorf("%s: %s %s touched %.0f rows (> %.0f): not bounded by the park-day", name, n.NodeType, n.RelationName, touched, rowCeiling)
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

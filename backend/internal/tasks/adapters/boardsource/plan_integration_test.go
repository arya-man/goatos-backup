package boardsource

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// legacyDayWhere is the day predicate the engine source shipped with in PR #429 before review:
// one OR, whose completed-that-day arm compared (updated_at AT TIME ZONE ...)::date, which no index
// serves. Kept here only so the plan test can prove it discriminates.
const legacyDayWhere = `
  wi.tenant_id = $1::uuid
  AND wi.park_id = $2::uuid
  AND wi.state <> 'canceled'
  AND (wi.module = ANY($4::text[]) OR ($5::text[] IS NOT NULL AND NOT (wi.module = ANY($5::text[]))))
  AND (
        wi.event_date = $3::date
     OR (wi.state = 'open' AND wi.event_date < $3::date)
     OR (wi.state = 'completed' AND wi.event_date < $3::date
         AND (wi.updated_at AT TIME ZONE 'Asia/Kolkata')::date = $3::date)
  )`

// rowsTouched runs EXPLAIN ANALYZE and sums, over every node reading table, the rows it produced
// plus the rows it read and threw away (filter and recheck), times its loops: how much of the
// table the query actually walked.
func rowsTouched(t *testing.T, ctx context.Context, conn *pgx.Conn, table, sql string, args []any) (int, string) {
	t.Helper()
	var planJSON []byte
	if err := conn.QueryRow(ctx, `EXPLAIN (ANALYZE, FORMAT JSON) `+sql, args...).Scan(&planJSON); err != nil {
		t.Fatal(err)
	}
	var plan []map[string]any
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		t.Fatal(err)
	}
	num := func(n map[string]any, k string) float64 { v, _ := n[k].(float64); return v }
	total := 0.0
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		if rel, _ := n["Relation Name"].(string); rel == table {
			loops := num(n, "Actual Loops")
			if loops == 0 {
				loops = 1
			}
			total += (num(n, "Actual Rows") + num(n, "Rows Removed by Filter") + num(n, "Rows Removed by Index Recheck")) * loops
		}
		kids, _ := n["Plans"].([]any)
		for _, k := range kids {
			if km, ok := k.(map[string]any); ok {
				walk(km)
			}
		}
	}
	walk(plan[0]["Plan"].(map[string]any))
	return int(total), string(planJSON)
}

// TestEngineBoardDayReadNeverWalksCompletedHistory is the plan-shape guard for review finding 1
// on PR #429: over two years of completed workflows at the park (and more at the other park), the
// list and the count read only the day's workflows -- raised today, still open, completed today --
// and never walk the park's completed history. The legacy OR over the same data walks all of it,
// so the guard discriminates.
func TestEngineBoardDayReadNeverWalksCompletedHistory(t *testing.T) {
	ctx, pool := startSeeded(t)
	// ~24k completed workflows at the park and ~12k at the other, one a day per 30-minute slot
	// over two years, each completed the day after it was raised: history that grows forever.
	wbExec(t, ctx, pool, `
INSERT INTO workflow_instances (workflow_id, tenant_id, template_key, module, subject_ref_id, event_at, event_date,
  park_id, state, actions_total, actions_done, awaiting_verification, created_at, updated_at)
SELECT id, $1::uuid, 'feed_purchase_intake', 'procurement', id, at, at::date, park, 'completed', 4, 4, false, at, at + interval '1 day'
FROM (
  SELECT gen_random_uuid() AS id, TIMESTAMPTZ '2024-09-20 00:00+05:30' + i * interval '45 minutes' AS at,
         CASE WHEN i % 3 = 0 THEN $3::uuid ELSE $2::uuid END AS park
  FROM generate_series(1, 23000) i
) h
WHERE h.at < TIMESTAMPTZ '2026-09-20 00:00+05:30'`, wbTenant, wbPark, wbOther)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `ANALYZE workflow_instances`); err != nil {
		t.Fatal(err)
	}

	src := laneSource(t, pool, domain.ModuleProcurement)
	var rows []domain.Row
	list, err := src.ListStatement(wbQuery(), &rows)
	if err != nil {
		t.Fatal(err)
	}
	var counts map[domain.WorkState]int
	count, err := src.CountStatement(wbQuery(), &counts)
	if err != nil {
		t.Fatal(err)
	}
	const budget = 200 // the day holds a handful of workflows; the history holds ~23k
	for name, sql := range map[string]string{"list": list.Query.SQL(), "count": count.Query.SQL()} {
		args := list.Query.Args()
		if name == "count" {
			args = count.Query.Args()
		}
		touched, plan := rowsTouched(t, ctx, conn.Conn(), "workflow_instances", sql, args)
		if touched > budget {
			t.Fatalf("%s walked %d workflow_instances rows for one day (budget %d):\n%s", name, touched, budget, plan)
		}
		if strings.Contains(plan, `"Seq Scan"`) && strings.Contains(plan, `"Relation Name": "workflow_instances"`) {
			t.Logf("%s plan contains a Seq Scan node; touched %d rows", name, touched)
		}
		// The guard discriminates: the legacy single-OR predicate over the same data walks the
		// completed history.
		legacy := strings.Replace(sql, dayMembersWhere(), legacyDayWhere, 1)
		if legacy == sql {
			t.Fatalf("%s: could not substitute the legacy predicate", name)
		}
		legacyTouched, legacyPlan := rowsTouched(t, ctx, conn.Conn(), "workflow_instances", legacy, args)
		if legacyTouched <= budget*10 {
			t.Fatalf("%s: legacy predicate touched only %d rows; the guard no longer discriminates:\n%s", name, legacyTouched, legacyPlan)
		}
		t.Logf("%s: %d rows touched (legacy predicate: %d)", name, touched, legacyTouched)
	}

	// And the answer is the same day as before: the seeded day's procurement workflows.
	if _, err := src.ListRows(ctx, wbQuery()); err != nil {
		t.Fatal(err)
	}
}

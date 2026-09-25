package boardsource

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// legacyBaseWhere is the day predicate the toxin source shipped with in PR #429 before review:
// the accepted rounds kept by (reviewed_at AT TIME ZONE ...)::date >= D, which no index serves.
// Kept here only so the plan test can prove it discriminates.
const legacyBaseWhere = `
  t.tenant_id = $1::uuid
  AND upper(btrim(t.farm_label)) = (
        SELECT upper(btrim(l.location_code)) FROM locations l
        WHERE l.tenant_id = $1::uuid AND l.location_id = $2::uuid AND l.location_type = 'park')
  AND t.status IN ('in_progress', 'pending_review', 'accepted')
  AND (t.created_at AT TIME ZONE 'Asia/Kolkata')::date <= $3::date
  AND (t.status <> 'accepted' OR (t.reviewed_at AT TIME ZONE 'Asia/Kolkata')::date >= $3::date)`

// tasksTouched runs EXPLAIN ANALYZE and sums, over every toxin_test_tasks read, the rows it
// produced plus the rows it read and threw away, times its loops.
func tasksTouched(t *testing.T, ctx context.Context, conn *pgx.Conn, sql string, args []any) (int, string) {
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
		if rel, _ := n["Relation Name"].(string); rel == "toxin_test_tasks" {
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

// TestToxinBoardDayReadNeverWalksAcceptedHistory is the plan-shape guard for review finding 2 on
// PR #429: over two years of accepted rounds at the park (and more at the other park), the list
// and the count read only the day's rounds -- live ones and those accepted on or after the day --
// and never walk the park's accepted history. The legacy predicate over the same data walks all of
// it, so the guard discriminates.
func TestToxinBoardDayReadNeverWalksAcceptedHistory(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)
	txSeed(t, ctx, pool)
	// ~11k accepted rounds over two years, two in three at this park: each opened and signed off
	// the same week, long before the day read.
	txExec(t, ctx, pool, `
INSERT INTO toxin_test_tasks (tenant_id, task_id, feed_purchase_id, farm_label, feed_item_key, feed_item_label, vendor,
  batch_no, purchase_date, quantity_kg, status, origin, outcome, created_at, reviewed_at)
SELECT $1::uuid, gen_random_uuid(), gen_random_uuid(), CASE WHEN i % 3 = 0 THEN 'CPT' ELSE 'CBE' END,
       'dry_masoor_bhusa', 'Dry Masoor Bhusa', 'Sri Balaji', i, DATE '2024-09-01', 2400, 'accepted', 'purchase', 'negative',
       TIMESTAMPTZ '2024-09-01 09:00+05:30' + i * interval '90 minutes',
       TIMESTAMPTZ '2024-09-01 09:00+05:30' + i * interval '90 minutes' + interval '2 days'
FROM generate_series(1, 11000) i`, txTenant)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `ANALYZE toxin_test_tasks`); err != nil {
		t.Fatal(err)
	}

	src := New(pool, 5*time.Second)
	var rows []domain.Row
	list, err := src.ListStatement(txQuery(), &rows)
	if err != nil {
		t.Fatal(err)
	}
	var counts map[domain.WorkState]int
	count, err := src.CountStatement(txQuery(), &counts)
	if err != nil {
		t.Fatal(err)
	}
	const budget = 100 // the day holds a handful of rounds; the history holds ~7k at this park
	for _, c := range []struct {
		name string
		sql  string
		args []any
	}{{"list", list.Query.SQL(), list.Query.Args()}, {"count", count.Query.SQL(), count.Query.Args()}} {
		touched, plan := tasksTouched(t, ctx, conn.Conn(), c.sql, c.args)
		if touched > budget {
			t.Fatalf("%s walked %d toxin_test_tasks rows for one day (budget %d):\n%s", c.name, touched, budget, plan)
		}
		legacy := strings.Replace(c.sql, baseWhere, legacyBaseWhere, 1)
		if legacy == c.sql {
			t.Fatalf("%s: could not substitute the legacy predicate", c.name)
		}
		legacyTouched, legacyPlan := tasksTouched(t, ctx, conn.Conn(), legacy, c.args)
		if legacyTouched <= budget*10 {
			t.Fatalf("%s: legacy predicate touched only %d rows; the guard no longer discriminates:\n%s", c.name, legacyTouched, legacyPlan)
		}
		t.Logf("%s: %d rows touched (legacy predicate: %d)", c.name, touched, legacyTouched)
	}

	// The day's answer is unchanged by the history: the seeded day's CBE rounds only.
	got, err := src.ListRows(ctx, txQuery())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if r.SourceID == tAcceptedBefore || r.SourceID == tCancelled || r.SourceID == tOtherPark || r.SourceID == tTomorrow {
			t.Fatalf("round %s is not the day's: %+v", r.SourceID, r)
		}
	}
	if len(got) != 5 {
		t.Fatalf("the day holds 5 CBE rounds (open, started, in review, accepted today, retest); got %d", len(got))
	}
}

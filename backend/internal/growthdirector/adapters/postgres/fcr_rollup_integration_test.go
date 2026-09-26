package postgres

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
)

// fcrRawRows runs one FCR statement with the production binds and returns its rows as values.
func fcrRawRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) [][]any {
	t.Helper()
	bound, err := sqlbind.Bind(sql, args...)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	rows, err := pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatalf("values: %v", err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// fcrLeadingColumns trims each row of rolled to the width of the oracle's rows.
func fcrLeadingColumns(rolled, live [][]any) [][]any {
	if len(live) == 0 {
		return rolled
	}
	width := len(live[0])
	out := make([][]any, len(rolled))
	for i, row := range rolled {
		if len(row) > width {
			row = row[:width]
		}
		out[i] = row
	}
	return out
}

// fcrRowsEqual compares two result sets cell by cell; float8 cells within a relative 1e-9 (the
// rollup sums the same products in a different order), everything else exactly.
func fcrRowsEqual(a, b [][]any) (bool, string) {
	if len(a) != len(b) {
		return false, fmt.Sprintf("row count %d vs %d", len(a), len(b))
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false, fmt.Sprintf("row %d width %d vs %d", i, len(a[i]), len(b[i]))
		}
		for j := range a[i] {
			x, y := a[i][j], b[i][j]
			xf, xok := x.(float64)
			yf, yok := y.(float64)
			if xok && yok {
				if math.Abs(xf-yf) > 1e-9*math.Max(1, math.Abs(xf)) {
					return false, fmt.Sprintf("row %d col %d: %v vs %v", i, j, xf, yf)
				}
				continue
			}
			if !reflect.DeepEqual(x, y) {
				return false, fmt.Sprintf("row %d col %d: %#v vs %#v", i, j, x, y)
			}
		}
	}
	return true, ""
}

// The rollup is an optimisation, never a second answer: for every window, park set and weighing
// category the pens and segments it produces must equal the pre-rollup live statements' rows.
func TestFCRRollupMatchesTheLiveStatementsAcrossParameterSets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)
	const otherPark = "22222222-0000-4000-8000-000000003099"
	// Extra loads: one re-prices mid-window, one belongs to another park and must price nothing here.
	execGD(t, ctx, pool, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg, total_cost, per_kg_cost, depletes_from)
VALUES
  ($1::uuid, $2::uuid, 'CBE', 'Maize Crush', 2, '2026-07-11', 1000, 30000, 30, '2026-07-11'),
  ($1::uuid, $3::uuid, 'CPT', 'Maize Crush', 1, '2026-07-05', 1000, 99000, 99, '2026-07-05')`,
		gdTenant, gdPark, otherPark)

	repo := NewRepository(pool, 30*time.Second)
	if _, err := repo.RefreshFCRRollup(ctx, ""); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	loc := biztime.DefaultLocation()
	windows := [][2]string{
		{"2026-07-06", "2026-07-20"}, {"2026-07-01", "2026-07-31"}, {"2026-07-08", "2026-07-15"},
		{"2026-07-13", "2026-07-14"}, {"2026-06-01", "2026-09-01"}, {"2026-07-10", "2026-07-12"},
	}
	compared := 0
	for _, w := range windows {
		from, _ := time.ParseInLocation("2006-01-02", w[0], loc)
		to, _ := time.ParseInLocation("2006-01-02", w[1], loc)
		for _, parks := range [][]string{{gdPark}, {gdPark, otherPark}, {otherPark}} {
			idMap, err := weighingpg.ResolveAnimalIdentityMap(ctx, pool, gdTenant, parks, from, to)
			if err != nil {
				t.Fatal(err)
			}
			for _, category := range []string{"", "individual_animal", "per_shed_partition"} {
				args := []any{gdTenant, parks, from, to, category, idMap.Tags, idMap.CanonicalTags, w[0], w[1], false, []string{}}
				for _, pair := range [][3]string{{"pens", fcrPensLiveSQL, fcrPensSQL}, {"segments", fcrSegmentsLiveSQL, fcrSegmentsSQL}} {
					live := fcrRawRows(t, ctx, pool, pair[1], args[:9]...) // the oracle predates $10/$11
					// The oracle predates the columns appended after the feed ones (the General-tab
					// ADG pair and the approved wastage); none of them reads the rollup, and each has
					// its own test, so the equivalence is checked over the columns the oracle carries.
					rolledArgs := args[:9]
					if pair[0] == "pens" { // the pens statement also takes the General-ADG kid filter ($10/$11)
						rolledArgs = args
					}
					rolled := fcrLeadingColumns(fcrRawRows(t, ctx, pool, pair[2], rolledArgs...), live)
					if ok, why := fcrRowsEqual(live, rolled); !ok {
						t.Fatalf("%s window %v parks %v category %q: rollup differs from live: %s", pair[0], w, parks, category, why)
					}
					compared++
				}
			}
		}
	}
	if compared != len(windows)*3*3*2 {
		t.Fatalf("compared %d parameter sets", compared)
	}
}

// A feed write committed before a read is always in that read: the trigger marks the park dirty
// in the writer's transaction and the FCR read refreshes the parks it is about to sum.
func TestFCRRollupReadAfterFeedWritesSeesTheWrite(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)
	from := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	lumpCost := func() float64 {
		t.Helper()
		// A fresh repository each time: this test is about the rollup, not the response cache.
		got, err := NewRepository(pool, 30*time.Second).GetFCR(ctx, gdTenant, []string{gdPark}, from, to, "", "", "")
		if err != nil {
			t.Fatalf("GetFCR: %v", err)
		}
		for _, pen := range got.Pens {
			if pen.OperationalLocationDisplay == "CBE · Lump 1" && pen.FeedCostINR != nil {
				return *pen.FeedCostINR
			}
		}
		t.Fatal("no lump pen cost")
		return 0
	}
	before := lumpCost()

	// A new, dearer load dated before the window re-prices every feed day after it.
	execGD(t, ctx, pool, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg, total_cost, per_kg_cost, depletes_from)
VALUES ($1::uuid, $2::uuid, 'CBE', 'Maize Crush', 9, '2026-07-05', 1000, 50000, 50, '2026-07-05')`, gdTenant, gdPark)
	afterPurchase := lumpCost()
	if afterPurchase == before {
		t.Fatalf("a load dated before the window did not re-price it (still %.2f)", before)
	}

	// An amended cell (more kilograms directed) must raise the next read's cost.
	execGD(t, ctx, pool, `
UPDATE feed_direction_issue_rows r SET quantity_kg = r.quantity_kg * 2
FROM feed_direction_issues i
WHERE i.feed_direction_issue_id = r.feed_direction_issue_id AND i.tenant_id = $1::uuid
  AND r.quantity_kg IS NOT NULL AND i.feed_day = '2026-07-10'`, gdTenant)
	if afterAmend := lumpCost(); afterAmend <= afterPurchase {
		t.Fatalf("amending a cell did not raise the cost (%.2f -> %.2f)", afterPurchase, afterAmend)
	}
	var dirty int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM growth_fcr_rollup_dirty`).Scan(&dirty); err != nil {
		t.Fatal(err)
	}
	if dirty != 0 {
		t.Fatalf("the read left %d dirty ranges for the park it read", dirty)
	}
}

// The daily reconcile finds a row that no longer matches its sources, re-marks the park, and the
// next refresh repairs it; a clean rollup reconciles with zero drift, once per business day.
func TestFCRRollupReconcileDetectsAndRepairsDrift(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)
	if _, err := repo.RefreshFCRRollup(ctx, ""); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 3, 0, 0, 0, time.UTC)
	if drift, err := repo.ReconcileFCRRollup(ctx, "", now); err != nil || drift != 0 {
		t.Fatalf("clean reconcile drift=%d err=%v", drift, err)
	}
	// Simulate a writer that bypassed the triggers: the stored rollup no longer matches its sources.
	execGD(t, ctx, pool, `UPDATE growth_fcr_pen_feed_days SET feed_kg = feed_kg + 5 WHERE feed_day = (SELECT min(feed_day) FROM growth_fcr_pen_feed_days)`)
	// Same business day: already reconciled, not re-run.
	if drift, _ := repo.ReconcileFCRRollup(ctx, "", now); drift != 0 {
		t.Fatalf("reconcile ran twice on one business day (drift %d)", drift)
	}
	next := now.Add(24 * time.Hour)
	drift, err := repo.ReconcileFCRRollup(ctx, "", next)
	if err != nil || drift == 0 {
		t.Fatalf("tampered rollup reconciled with drift=%d err=%v", drift, err)
	}
	if _, err := repo.RefreshFCRRollup(ctx, ""); err != nil {
		t.Fatal(err)
	}
	var stored, fresh float64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(feed_kg),0)::float8 FROM growth_fcr_pen_feed_days WHERE tenant_id = $1::uuid`, gdTenant).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(r.quantity_kg),0)::float8 FROM feed_direction_issue_rows r JOIN feed_direction_issues i ON i.feed_direction_issue_id = r.feed_direction_issue_id WHERE i.tenant_id = $1::uuid AND i.state IN ('issued','amended','locked')`, gdTenant).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if math.Abs(stored-fresh) > 1e-9 {
		t.Fatalf("repair left stored %.3f vs source %.3f", stored, fresh)
	}
}

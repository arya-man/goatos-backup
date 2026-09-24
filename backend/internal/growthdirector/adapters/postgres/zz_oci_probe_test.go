//go:build perfprobe

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
)

func explainMs(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args []any) float64 {
	b := sqlbind.MustBind(sql, args...)
	var plan string
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+b.SQL(), b.Args()...).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	var p []map[string]any
	_ = json.Unmarshal([]byte(plan), &p)
	return p[0]["Execution Time"].(float64) + p[0]["Planning Time"].(float64)
}

func rawRowsTx(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args []any) [][]any {
	b := sqlbind.MustBind(sql, args...)
	rows, err := tx.Query(ctx, b.SQL(), b.Args()...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		v, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

func pct(xs []float64, p float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(float64(len(s)-1)*p)]
}

func TestOCIFCRRollupEquivalence(t *testing.T) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{}) // only a TEMP table is written; the transaction is rolled back
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	tenant := "00000000-0000-4000-8000-000000000001"
	p1, p2 := "00000000-0000-4000-8000-000000003001", "00000000-0000-4000-8000-000000003002"
	// Session-local rollup: a TEMP table shadows the public name for this transaction only.
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE growth_fcr_pen_feed_days (
  tenant_id uuid, park_id uuid, pen_shed_id uuid, pen_key text, feed_day date, feed_kg numeric,
  feed_cost double precision, unpriced_kg numeric, blocked_cells integer, head_days numeric, feed_label text,
  refreshed_at timestamptz, PRIMARY KEY (tenant_id, park_id, pen_shed_id, pen_key, feed_day)) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `CREATE INDEX ON growth_fcr_pen_feed_days (tenant_id, park_id, feed_day)`); err != nil {
		t.Fatal(err)
	}
	var build []float64
	for _, p := range []string{p1, p2} {
		st := time.Now()
		b := sqlbind.MustBind(fcrRollupInsertSQL, tenant, p, "0001-01-01")
		if _, err := tx.Exec(ctx, b.SQL(), b.Args()...); err != nil {
			t.Fatal(err)
		}
		build = append(build, float64(time.Since(st).Microseconds())/1000)
	}
	var n int
	tx.QueryRow(ctx, "ANALYZE growth_fcr_pen_feed_days").Scan()
	tx.QueryRow(ctx, "SELECT count(*) FROM growth_fcr_pen_feed_days").Scan(&n)
	t.Logf("rollup rows=%d full build per park ms=%v", n, build)

	loc := biztime.DefaultLocation()
	var windows [][2]string
	for _, span := range []int{7, 14, 21, 28, 45, 60} {
		for _, end := range []string{"2026-09-23", "2026-09-15", "2026-09-01", "2026-08-20"} {
			e, _ := time.ParseInLocation("2006-01-02", end, loc)
			windows = append(windows, [2]string{e.AddDate(0, 0, -span).Format("2006-01-02"), end})
		}
	}
	var liveMs, rollMs []float64
	maxDiff := 0.0
	sets := 0
	for _, w := range windows {
		from, _ := time.ParseInLocation("2006-01-02", w[0], loc)
		to, _ := time.ParseInLocation("2006-01-02", w[1], loc)
		for _, parks := range [][]string{{p1}, {p2}, {p1, p2}} {
			m, err := weighingpg.ResolveAnimalIdentityMap(ctx, pool, tenant, parks, from, to)
			if err != nil {
				t.Fatal(err)
			}
			idm := [2][]string{m.Tags, m.CanonicalTags}
			for _, cat := range []string{"", "individual_animal", "per_shed_partition"} {
				args := []any{tenant, parks, from, to, cat, idm[0], idm[1], w[0], w[1]}
				for _, pair := range [][2]string{{fcrPensLiveSQL, fcrPensSQL}, {fcrSegmentsLiveSQL, fcrSegmentsSQL}} {
					a := rawRowsTx(t, ctx, tx, pair[0], args)
					b := rawRowsTx(t, ctx, tx, pair[1], args)
					if ok, why := fcrRowsEqual(a, b); !ok {
						t.Fatalf("window %v parks %v cat %q: %s", w, parks, cat, why)
					}
					for i := range a {
						for j := range a[i] {
							if x, ok := a[i][j].(float64); ok {
								y := b[i][j].(float64)
								if d := abs(x - y); d > maxDiff {
									maxDiff = d
								}
							}
						}
					}
					liveMs = append(liveMs, explainMs(t, ctx, tx, pair[0], args))
					rollMs = append(rollMs, explainMs(t, ctx, tx, pair[1], args))
					sets++
				}
			}
		}
	}
	fmt.Printf("OCI equivalence: %d statement comparisons, all equal, max abs float diff %.3g\n", sets, maxDiff)
	fmt.Printf("server-side ms (plan+exec) live p50/p95 %.1f/%.1f  rollup p50/p95 %.1f/%.1f\n", pct(liveMs, .5), pct(liveMs, .95), pct(rollMs, .5), pct(rollMs, .95))
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}


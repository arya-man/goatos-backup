package postgres

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// KEEP THE ITEMS UNLESS 'OTHER' CHANGED (maintainer decision 2026-09-25).
//
// A sheet-imported load is itemised: animal, transport, booking, labour, transit, transition feed.
// Editing the animal or transport figure used to delete EVERY line and write back three, so the
// four itemised "other" costs collapsed into one line the moment anybody corrected the transport.
// Now an edit rewrites only the bucket whose figure changed, and the itemised "other" lines are
// replaced by one line only when the submitted other total differs from their sum. The three
// roll-up columns always equal the lines.
func TestEditingOneCostBucketKeepsTheOtherItemisedLines(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	load := fx.loadB

	if _, err := pool.Exec(ctx, `
UPDATE procurement_loads SET animal_cost = 50000, transport_cost = 2000, other_cost = 1100
WHERE tenant_id = $1 AND load_id = $2`, testTenant, load); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM procurement_load_cost_lines WHERE tenant_id = $1 AND load_id = $2`, testTenant, load); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_cost_lines (tenant_id, load_id, kind, amount, source)
SELECT $1, $2, k, a, 'sheet_import'
FROM unnest(ARRAY['animal','transport','booking','labour','transit','transition_feed'],
            ARRAY[50000, 2000, 500, 300, 200, 100]::numeric[]) AS t(k, a)`, testTenant, load); err != nil {
		t.Fatal(err)
	}

	type line struct {
		kind   string
		amount float64
	}
	readLines := func() map[string][]float64 {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT kind, amount::float8 FROM procurement_load_cost_lines WHERE tenant_id = $1 AND load_id = $2 ORDER BY kind`, testTenant, load)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string][]float64{}
		for rows.Next() {
			var l line
			if err := rows.Scan(&l.kind, &l.amount); err != nil {
				t.Fatal(err)
			}
			out[l.kind] = append(out[l.kind], l.amount)
		}
		return out
	}
	rollUpsMatchLines := func() {
		t.Helper()
		var animal, transport, other *float64
		if err := pool.QueryRow(ctx, `SELECT animal_cost::float8, transport_cost::float8, other_cost::float8 FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2`, testTenant, load).Scan(&animal, &transport, &other); err != nil {
			t.Fatal(err)
		}
		lines := []domain.LoadCostLine{}
		for kind, amounts := range readLines() {
			for _, a := range amounts {
				lines = append(lines, domain.LoadCostLine{Kind: kind, Amount: a})
			}
		}
		la, lt, lo := domain.RollUpCostLines(lines)
		for name, pair := range map[string][2]*float64{"animal": {animal, la}, "transport": {transport, lt}, "other": {other, lo}} {
			col, sum := pair[0], pair[1]
			if (col == nil) != (sum == nil) || (col != nil && math.Abs(*col-*sum) > 0.005) {
				t.Fatalf("%s roll-up column %v disagrees with its lines %v", name, col, sum)
			}
		}
	}

	// Edit transport only.
	if err := repo.SetLoadCost(ctx, testTenant, load, domain.LoadCostEdit{AnimalCost: lwf(50000), TransportCost: lwf(2500), OtherCost: lwf(1100)}, ""); err != nil {
		t.Fatal(err)
	}
	got := readLines()
	for _, kind := range []string{"booking", "labour", "transit", "transition_feed"} {
		if len(got[kind]) != 1 {
			t.Fatalf("transport-only edit dropped the itemised %s line: %v", kind, got)
		}
	}
	if len(got["other"]) != 0 || len(got["transport"]) != 1 || got["transport"][0] != 2500 || got["animal"][0] != 50000 {
		t.Fatalf("after transport edit: %v", got)
	}
	rollUpsMatchLines()

	// Edit other: the itemised non-animal/non-transport lines collapse into one 'other' line.
	if err := repo.SetLoadCost(ctx, testTenant, load, domain.LoadCostEdit{AnimalCost: lwf(50000), TransportCost: lwf(2500), OtherCost: lwf(1500)}, ""); err != nil {
		t.Fatal(err)
	}
	got = readLines()
	for _, kind := range []string{"booking", "labour", "transit", "transition_feed"} {
		if len(got[kind]) != 0 {
			t.Fatalf("an 'other' edit must replace the itemised %s line: %v", kind, got)
		}
	}
	if len(got["other"]) != 1 || got["other"][0] != 1500 || len(got["transport"]) != 1 || got["transport"][0] != 2500 {
		t.Fatalf("after other edit: %v", got)
	}
	rollUpsMatchLines()

	// Clearing the cost removes every line, as before.
	if err := repo.SetLoadCost(ctx, testTenant, load, domain.LoadCostEdit{}, ""); err != nil {
		t.Fatal(err)
	}
	if got := readLines(); len(got) != 0 {
		t.Fatalf("cleared cost left lines: %v", got)
	}
	rollUpsMatchLines()
}

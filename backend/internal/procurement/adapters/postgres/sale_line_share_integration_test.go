package postgres

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// OPTION B (maintainer decision 2026-09-25): a tagged animal is priced from its OWN sale line --
// the animal line of its species (and breed, when the deal has more than one line of that
// species) -- divided over the animals tagged to that line. Feed, manure and other lines are never
// attributed to an animal or a load. Load wise and Farm born read the SAME shared CTE.

type saleLineFixture struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	seq  int
}

func (f *saleLineFixture) load(templateLoad string) string {
	f.t.Helper()
	f.seq++
	var id string
	if err := f.pool.QueryRow(f.ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, idempotency_key)
SELECT tenant_id, source_party_id, '2026-08-14', status, $3
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid
RETURNING load_id::text`, testTenant, templateLoad, fmt.Sprintf("sl-load-%d", f.seq)).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *saleLineFixture) deal(lines ...[4]any) string {
	f.t.Helper()
	return f.dealWithStatus("Deal Closed", lines...)
}

func (f *saleLineFixture) dealWithStatus(status string, lines ...[4]any) string {
	f.t.Helper()
	var id string
	var total float64
	for _, l := range lines {
		total += l[3].(float64)
	}
	if err := f.pool.QueryRow(f.ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, sales_value, status)
VALUES ($1, '2026-09-10', 'CPT', 'Line Buyer', 'Mixed', 'Mixed', $2, $3) RETURNING id::text`, testTenant, total, status).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	for i, l := range lines {
		// {product_code, product_kind, breed, sales_value}
		if _, err := f.pool.Exec(f.ctx, `
INSERT INTO sales_deal_lines (tenant_id, deal_id, line_no, product_type, product_code, product_kind, breed, sales_value)
VALUES ($1, $2::uuid, $3, initcap($4), $4, $5, $6, $7)`, testTenant, id, i+1, l[0], l[1], l[2], l[3]); err != nil {
			f.t.Fatal(err)
		}
	}
	return id
}

// animals creates n sold animals of a species/breed, puts them on the load (when load != ""),
// marks them farm born otherwise, and tags them to the deal.
func (f *saleLineFixture) animals(n int, species, breed, load, deal string) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		f.seq++
		var goat string
		origin := "birth"
		if load != "" {
			origin = "procured"
		}
		if err := f.pool.QueryRow(f.ctx, `
INSERT INTO goats (tenant_id, sex, species, breed, lifecycle_status, exit_reason, origin_type, custodian_party_id)
VALUES ($1, 'female', $2, $3, 'sold', 'sold', $4, (SELECT party_id FROM parties WHERE display_name = 'Sardar Traders' LIMIT 1))
RETURNING goat_id::text`, testTenant, species, breed, origin).Scan(&goat); err != nil {
			f.t.Fatal(err)
		}
		if load != "" {
			if _, err := f.pool.Exec(f.ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-08-14T10:00:00Z')`, testTenant, load, goat); err != nil {
				f.t.Fatal(err)
			}
		}
		if _, err := f.pool.Exec(f.ctx, `
INSERT INTO goat_sale_allocations (tenant_id, goat_id, sales_deal_id, status, idempotency_key)
VALUES ($1, $2::uuid, $3::uuid, 'tagged', $4)`, testTenant, goat, deal, fmt.Sprintf("sl-alloc-%d", f.seq)); err != nil {
			f.t.Fatal(err)
		}
	}
}

func TestOneToManyLinesMultipleDimensionsEachTaggedAnimalIsPricedFromItsOwnSaleLine(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	f := &saleLineFixture{t: t, ctx: ctx, pool: pool}

	// (a) goats + feed + manure: only the goat line reaches the load.
	loadA := f.load(fx.loadA)
	dealA := f.deal([4]any{"goat", "animal", "Malai", 100000.0}, [4]any{"feed", "feed", "Maize", 42000.0}, [4]any{"manure", "other", "Manure", 8000.0})
	f.animals(10, "goat", "Malai", loadA, dealA)

	// (b) goats on load X, sheep on load Y, one deal.
	loadBX, loadBY := f.load(fx.loadA), f.load(fx.loadA)
	dealB := f.deal([4]any{"goat", "animal", "Malai", 100000.0}, [4]any{"sheep", "animal", "Nellore", 90000.0})
	f.animals(10, "goat", "Malai", loadBX, dealB)
	f.animals(5, "sheep", "Nellore", loadBY, dealB)

	// (c) two goat lines of different breeds, priced differently.
	loadC1, loadC2 := f.load(fx.loadA), f.load(fx.loadA)
	dealC := f.deal([4]any{"goat", "animal", "Malai", 40000.0}, [4]any{"goat", "animal", "Sirohi", 60000.0})
	f.animals(4, "goat", "Malai", loadC1, dealC)
	f.animals(3, "goat", "Sirohi", loadC2, dealC)

	// (d) legacy single-line deal: the line IS the deal, so the result is today's (value / tagged).
	loadD := f.load(fx.loadA)
	dealD := f.deal([4]any{"goat", "animal", "Malai", 55000.0})
	f.animals(5, "goat", "Malai", loadD, dealD)

	// (e) farm born animals in a mixed deal, next to a load's goats.
	loadE := f.load(fx.loadA)
	dealE := f.deal([4]any{"goat", "animal", "Malai", 30000.0}, [4]any{"sheep", "animal", "Nellore", 20000.0}, [4]any{"manure", "other", "Manure", 5000.0})
	f.animals(3, "goat", "Malai", loadE, dealE)
	f.animals(2, "sheep", "Nellore", "", dealE)

	out, err := repo.LoadwiseSales(ctx, testTenant, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.LoadwiseLoad{}
	for _, l := range out.Loads {
		byID[l.LoadID] = l
	}
	for _, c := range []struct {
		name  string
		load  string
		sold  int
		value float64
	}{
		{"(a) goats only, not feed or manure", loadA, 10, 100000},
		{"(b) goats load", loadBX, 10, 100000},
		{"(b) sheep load", loadBY, 5, 90000},
		{"(c) Malai load", loadC1, 4, 40000},
		{"(c) Sirohi load", loadC2, 3, 60000},
		{"(d) legacy single line", loadD, 5, 55000},
		{"(e) the load's goats", loadE, 3, 30000},
	} {
		got, ok := byID[c.load]
		if !ok {
			t.Fatalf("%s: load missing", c.name)
		}
		if got.Sold != c.sold || math.Abs(got.SoldValue-c.value) > 0.01 {
			t.Errorf("%s: sold=%d value=%v, want %d / %v", c.name, got.Sold, got.SoldValue, c.sold, c.value)
		}
	}

	facts, err := repo.FarmBornAnimals(ctx, testTenant, domain.FarmBornFilter{From: "2026-09-01", To: "2026-09-30"})
	if err != nil {
		t.Fatal(err)
	}
	var bornValue float64
	var bornSold int
	for _, fact := range facts {
		if fact.DealID != dealE {
			continue
		}
		bornSold++
		if fact.SaleValue == nil || math.Abs(*fact.SaleValue-10000) > 0.01 {
			t.Errorf("(e) farm born sheep priced %v, want 10000 (its own sheep line / 2)", fact.SaleValue)
		} else {
			bornValue += *fact.SaleValue
		}
	}
	if bornSold != 2 {
		t.Fatalf("(e) farm born sold animals on the mixed deal = %d, want 2", bornSold)
	}
	// Invariant for the mixed deal: Load wise + Farm born == the deal's animal lines (50,000); the
	// manure line reaches neither page.
	if total := byID[loadE].SoldValue + bornValue; math.Abs(total-50000) > 0.01 {
		t.Fatalf("(e) animal value across both pages = %v, want the animal lines' 50000", total)
	}

	t.Run("StatusMatrixOnlyAClosedDealPricesItsLine", func(t *testing.T) {
		for _, status := range []string{"Advance Paid", "In Discussion", "Deal Failed"} {
			load := f.load(fx.loadA)
			deal := f.dealWithStatus(status, [4]any{"goat", "animal", "Malai", 50000.0})
			f.animals(5, "goat", "Malai", load, deal)
			out, err := repo.LoadwiseSales(ctx, testTenant, "", 200)
			if err != nil {
				t.Fatal(err)
			}
			for _, l := range out.Loads {
				if l.LoadID == load && (l.Sold != 0 || l.SoldValue != 0) {
					t.Fatalf("%s deal priced on Load wise: sold=%d value=%v", status, l.Sold, l.SoldValue)
				}
			}
		}
	})

	t.Run("ParkScopeKeepsEachLoadsOwnLineValue", func(t *testing.T) {
		cpt := parkIDByCode(t, ctx, pool, "CPT")
		if _, err := pool.Exec(ctx, `UPDATE goats SET park_id = $2::uuid WHERE tenant_id = $1 AND goat_id IN (
SELECT goat_id FROM procurement_load_goats WHERE tenant_id = $1 AND load_id = ANY($3::uuid[]))`,
			testTenant, cpt, []string{loadBX, loadBY}); err != nil {
			t.Fatal(err)
		}
		scoped, err := repo.LoadwiseSales(ctx, testTenant, cpt, 200)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]float64{}
		for _, l := range scoped.Loads {
			seen[l.LoadID] = l.SoldValue
		}
		if math.Abs(seen[loadBX]-100000) > 0.01 || math.Abs(seen[loadBY]-90000) > 0.01 {
			t.Fatalf("park-scoped goats/sheep loads = %v / %v, want 100000 / 90000", seen[loadBX], seen[loadBY])
		}
	})

	t.Run("PageBoundaryWindowDoesNotMoveAnyLoadsValueOrTheAverage", func(t *testing.T) {
		full, err := repo.LoadwiseSales(ctx, testTenant, "", 200)
		if err != nil {
			t.Fatal(err)
		}
		one, err := repo.LoadwiseSales(ctx, testTenant, "", 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(one.Loads) != 1 {
			t.Fatalf("window of one served %d loads", len(one.Loads))
		}
		for _, l := range full.Loads {
			if l.LoadID == one.Loads[0].LoadID && math.Abs(l.SoldValue-one.Loads[0].SoldValue) > 0.01 {
				t.Fatalf("the page window moved a load's value: %v vs %v", l.SoldValue, one.Loads[0].SoldValue)
			}
		}
		if (full.OverallAvgSoldPrice == nil) != (one.OverallAvgSoldPrice == nil) ||
			(full.OverallAvgSoldPrice != nil && math.Abs(*full.OverallAvgSoldPrice-*one.OverallAvgSoldPrice) > 0.01) {
			t.Fatalf("the page window moved the overall average: %v vs %v", full.OverallAvgSoldPrice, one.OverallAvgSoldPrice)
		}
	})
}

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestAuthoredAnimalNamesPreserveBuyerAndLoadwiseReporting(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)
	savedSQL, err := os.ReadFile("../../../../../.agents/skills/mesha-data-map/references/load-wise-sales.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE procurement_loads SET context=jsonb_set(coalesce(context,'{}'::jsonb),'{load_ref}','"reporting-fixture"'::jsonb) WHERE tenant_id=$1 AND load_id=$2::uuid`, testTenant, fx.loadA); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(pool, 10*time.Second)
	check := func(wantAnimals float64, wantPrice bool) {
		t.Helper()
		started := time.Now()
		var savedPrice *float64
		savedQuery := "SELECT sale_price_per_kg::float8 FROM (" + strings.TrimSuffix(strings.TrimSpace(string(savedSQL)), ";") + ") saved WHERE load_no='reporting-fixture'"
		if err := pool.QueryRow(ctx, savedQuery).Scan(&savedPrice); err != nil {
			t.Fatal(err)
		}
		if wantPrice && (savedPrice == nil || *savedPrice != 500) {
			t.Fatalf("saved query price=%v want 500", savedPrice)
		}
		if !wantPrice && savedPrice != nil {
			t.Fatalf("saved query priced non-animal as animal: %v", *savedPrice)
		}
		deals, err := repo.ClosedBuyerDeals(ctx, testTenant, "")
		if err != nil {
			t.Fatal(err)
		}
		var animals float64
		for _, d := range deals {
			animals += d.Animals
		}
		if len(deals) != 1 || animals != wantAnimals {
			t.Fatalf("buyer facts=%d animals=%v want 1/%v", len(deals), animals, wantAnimals)
		}
		loads, err := repo.LoadwiseSales(ctx, testTenant, "", 60)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range loads.Loads {
			if row.LoadID == fx.loadA {
				if wantPrice && (row.SalePricePerKg == nil || *row.SalePricePerKg != 500) {
					t.Fatalf("sale price=%v want 500/kg", row.SalePricePerKg)
				}
				if !wantPrice && row.SalePricePerKg != nil {
					t.Fatalf("non-animal sale priced as animal: %v", *row.SalePricePerKg)
				}
				t.Logf("buyer/loadwise SQL fixture: 1 deal, %d loads, animals=%v; combined read time=%s", len(loads.Loads), animals, time.Since(started))
				return
			}
		}
		t.Fatal("fixture load disappeared")
	}
	check(3, true)
	// Same immutable classification and money under the newly authored names.
	if _, err := pool.Exec(ctx, `UPDATE sales_deal_lines SET product_type=CASE product_code WHEN 'sheep' THEN 'Mutton sheep' ELSE 'Meat goats' END WHERE tenant_id=$1`, testTenant); err != nil {
		t.Fatal(err)
	}
	check(3, true)
	// A single custom animal item must also pass the single-product deal guard.
	if _, err := pool.Exec(ctx, `DELETE FROM sales_deal_lines WHERE tenant_id=$1 AND product_code='sheep'`, testTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sales_deal_lines SET product_type='Buffalo',product_code='buffalo',animal_count=3,total_weight_kg=60,sales_value=30000 WHERE tenant_id=$1`, testTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sales_deals SET product_type='Buffalo' WHERE tenant_id=$1 AND buyer_name='Loadwise Buyer'`, testTenant); err != nil {
		t.Fatal(err)
	}
	check(3, true)
	// Names cannot turn a non-animal row into a live-price sample either.
	if _, err := pool.Exec(ctx, `UPDATE sales_deal_lines SET product_type='Sheep',product_kind='other',animal_count=NULL WHERE tenant_id=$1`, testTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sales_deals SET product_type='Sheep' WHERE tenant_id=$1 AND buyer_name='Loadwise Buyer'`, testTenant); err != nil {
		t.Fatal(err)
	}
	check(0, false)
}

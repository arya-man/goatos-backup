package postgres

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// Load wise counts a sale exactly like Summary and Farm born: only a 'Deal Closed' deal is a sale
// (maintainer decision 2026-09-25). Tagging animals to an Advance Paid deal exits them in the
// register, but until the deal closes they add no sold animal, no sold value and nothing to the
// overall average sold price. Closing the deal brings all three in.
func TestLoadwiseCountsOnlyClosedDealsAsSold(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	var loadX, dealID string
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, idempotency_key)
SELECT tenant_id, source_party_id, '2026-08-13', status, 'lw-load-open-deal'
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid
RETURNING load_id::text`, testTenant, fx.loadA).Scan(&loadX); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1, '2026-09-30', 'CPT', 'Expected Buyer', 'Goat', 'Malai', 3, 90000, 'Advance Paid')
RETURNING id::text`, testTenant).Scan(&dealID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		var goat string
		if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, sex, lifecycle_status, exit_reason, custodian_party_id, park_id)
SELECT tenant_id, 'female', 'sold', 'sold', source_party_id, NULL
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid
RETURNING goat_id::text`, testTenant, loadX).Scan(&goat); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-08-13T10:00:00Z')`, testTenant, loadX, goat); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_sale_allocations (tenant_id, goat_id, sales_deal_id, status, idempotency_key)
VALUES ($1, $2::uuid, $3::uuid, 'tagged', $4)`, testTenant, goat, dealID, fmt.Sprintf("lw-open-deal-%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	read := func() (domain.LoadwiseLoad, *float64) {
		t.Helper()
		out, err := repo.LoadwiseSales(ctx, testTenant, "", 60)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range out.Loads {
			if l.LoadID == loadX {
				return l, out.OverallAvgSoldPrice
			}
		}
		t.Fatalf("load %s missing", loadX)
		return domain.LoadwiseLoad{}, nil
	}

	open, openAvg := read()
	if open.Sold != 0 || open.SoldValue != 0 {
		t.Fatalf("an Advance Paid deal counted as sold on Load wise: sold=%d value=%v", open.Sold, open.SoldValue)
	}
	// The fixture's closed deals price the overall average at 10000 per animal; the open deal's
	// 30000-per-animal share must not move it.
	if openAvg == nil || math.Abs(*openAvg-10000) > 0.01 {
		t.Fatalf("an open deal moved the overall average sold price: %v", openAvg)
	}

	if _, err := pool.Exec(ctx, `UPDATE sales_deals SET status = 'Deal Closed' WHERE id = $1::uuid`, dealID); err != nil {
		t.Fatal(err)
	}
	closed, closedAvg := read()
	if closed.Sold != 3 || math.Abs(closed.SoldValue-90000) > 0.01 {
		t.Fatalf("closing the deal must bring its animals in: sold=%d value=%v", closed.Sold, closed.SoldValue)
	}
	if closedAvg == nil || *closedAvg <= 10000 {
		t.Fatalf("closing the deal must count it in the overall average: %v", closedAvg)
	}
}

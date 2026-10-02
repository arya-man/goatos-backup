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

	read := func() domain.LoadwiseLoad {
		t.Helper()
		out, err := repo.LoadwiseSales(ctx, testTenant, "", 60)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range out.Loads {
			if l.LoadID == loadX {
				return l
			}
		}
		t.Fatalf("load %s missing", loadX)
		return domain.LoadwiseLoad{}
	}

	open := read()
	if open.Sold != 0 || open.SoldValue != 0 {
		t.Fatalf("an Advance Paid deal counted as sold on Load wise: sold=%d value=%v", open.Sold, open.SoldValue)
	}

	if _, err := pool.Exec(ctx, `UPDATE sales_deals SET status = 'Deal Closed' WHERE id = $1::uuid`, dealID); err != nil {
		t.Fatal(err)
	}
	closed := read()
	if closed.Sold != 3 || math.Abs(closed.SoldValue-90000) > 0.01 {
		t.Fatalf("closing the deal must bring its animals in: sold=%d value=%v", closed.Sold, closed.SoldValue)
	}
}

// Tagging exits an animal from the herd at once, but only a closed deal is a sale. An animal
// tagged to a deal still open (Advance Paid) is therefore neither sold nor on farm: it has its
// own bucket, "Tagged, sale not closed", so the load still balances (maintainer decision
// 2026-09-25) instead of reading red Unaccounted. Closing the deal moves it to Sold.
func TestLoadwiseTaggedToAnOpenDealIsItsOwnBucket(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	var loadX, dealID string
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, idempotency_key, expected_count)
SELECT tenant_id, source_party_id, '2026-08-13', status, 'lw-load-tagged-open', 3
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
VALUES ($1, $2::uuid, $3::uuid, 'tagged', $4)`, testTenant, goat, dealID, fmt.Sprintf("lw-tagged-open-%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	read := func() (domain.LoadwiseLoad, domain.LoadwiseSummary) {
		t.Helper()
		out, err := repo.LoadwiseSales(ctx, testTenant, "", 60)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range out.Loads {
			if l.LoadID == loadX {
				return l, out.Summary
			}
		}
		t.Fatalf("load %s missing", loadX)
		return domain.LoadwiseLoad{}, domain.LoadwiseSummary{}
	}

	open, openSummary := read()
	if open.TaggedNotClosed != 3 || open.Unaccounted != 0 || open.Sold != 0 {
		t.Fatalf("an Advance Paid deal's animals must sit in their own bucket: tagged_not_closed=%d unaccounted=%d sold=%d",
			open.TaggedNotClosed, open.Unaccounted, open.Sold)
	}
	if open.Purchased != open.Sold+open.Mortality+open.OtherExits+open.Remaining+open.TaggedNotClosed+open.Unaccounted {
		t.Fatalf("the load does not balance: %+v", open)
	}
	if open.SoldValue != 0 {
		t.Fatalf("an open deal's value is not a sale: %v", open.SoldValue)
	}
	if openSummary.TaggedNotClosed < 3 {
		t.Fatalf("the summary must carry the bucket: %d", openSummary.TaggedNotClosed)
	}

	if _, err := pool.Exec(ctx, `UPDATE sales_deals SET status = 'Deal Closed' WHERE id = $1::uuid`, dealID); err != nil {
		t.Fatal(err)
	}
	closed, _ := read()
	if closed.Sold != 3 || closed.TaggedNotClosed != 0 || closed.Unaccounted != 0 {
		t.Fatalf("closing the deal moves the animals to Sold: sold=%d tagged_not_closed=%d unaccounted=%d",
			closed.Sold, closed.TaggedNotClosed, closed.Unaccounted)
	}
}

// "Fattening days on farm: sold, alive in any case" (maintainer, 2026-09-25). A load sold through
// GoatOS -- animals tagged to a closed deal -- has no imported fattening span, so its sold animals
// had no bar. The span is now derived from the closed deals themselves: arrival to sale date,
// animal-weighted. The animals still on farm keep their running clock beside it.
func TestLoadwiseFatteningSpanComesFromClosedSalesWhenNoneWasImported(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	var loadX string
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, arrived_on, status, idempotency_key, expected_count)
SELECT tenant_id, source_party_id, '2026-08-12', '2026-08-13', status, 'lw-load-fattening-app', 5
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid
RETURNING load_id::text`, testTenant, fx.loadA).Scan(&loadX); err != nil {
		t.Fatal(err)
	}
	deal := func(date string, n int) string {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1, $2::date, 'CPT', 'Buyer', 'Goat', 'Malai', $3, 30000, 'Deal Closed')
RETURNING id::text`, testTenant, date, n).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	early, late := deal("2026-09-12", 1), deal("2026-09-22", 2) // 30 and 40 days after arrival
	addGoat := func(i int, lifecycle, dealID string) {
		var goat string
		exit := any(nil)
		if lifecycle == "sold" {
			exit = "sold"
		}
		if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, sex, lifecycle_status, exit_reason, custodian_party_id, park_id)
SELECT tenant_id, 'female', $3, $4, source_party_id, NULL
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid
RETURNING goat_id::text`, testTenant, loadX, lifecycle, exit).Scan(&goat); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-08-13T10:00:00Z')`, testTenant, loadX, goat); err != nil {
			t.Fatal(err)
		}
		if dealID != "" {
			if _, err := pool.Exec(ctx, `
INSERT INTO goat_sale_allocations (tenant_id, goat_id, sales_deal_id, status, idempotency_key)
VALUES ($1, $2::uuid, $3::uuid, 'tagged', $4)`, testTenant, goat, dealID, fmt.Sprintf("lw-fattening-%d", i)); err != nil {
				t.Fatal(err)
			}
		}
	}
	addGoat(0, "sold", early)
	addGoat(1, "sold", late)
	addGoat(2, "sold", late)
	addGoat(3, "alive", "")
	addGoat(4, "alive", "")

	out, err := repo.LoadwiseSales(ctx, testTenant, "", 60)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range out.Loads {
		if l.LoadID != loadX {
			continue
		}
		// (30 + 40 + 40) / 3 = 36.67, rounded to whole days.
		if l.FatteningDays == nil || *l.FatteningDays != 37 {
			t.Fatalf("sold animals' span = %v, want 37 days (animal-weighted arrival to sale)", l.FatteningDays)
		}
		if l.DaysOnFarmSoFar == nil || l.Remaining != 2 {
			t.Fatalf("the 2 animals still on farm keep their running clock: %v remaining=%d", l.DaysOnFarmSoFar, l.Remaining)
		}
		return
	}
	t.Fatalf("load %s missing", loadX)
}

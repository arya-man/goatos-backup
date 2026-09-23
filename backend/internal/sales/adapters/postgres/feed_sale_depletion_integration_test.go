package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// feedSaleRepo is a sales repository on a live database, for the feed-store ledger cases.
func feedSaleRepo(t *testing.T, ctx context.Context) *Repository {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)

	// The farm puts feed in its registry, which is the whole act of deciding to sell it -- no code
	// names Feed anywhere. Goat is there beside it for the mixed-sale case. The migration seeds
	// the built-ins for tenants that exist when it runs, so a test tenant created afterwards
	// writes its own.
	if _, err := pool.Exec(ctx, `
INSERT INTO public.sellable_product_catalog (tenant_id, product_code, name, kind, unit, species_code, sort_order, is_builtin)
VALUES ($1, 'feed', 'Feed', 'feed', 'kg', NULL, 40, false),
       ($1, 'goat', 'Goat', 'animal', 'head', 'goat', 20, true)
ON CONFLICT (tenant_id, product_code) DO NOTHING`, salesTestTenant); err != nil {
		t.Fatalf("seed the farm's sellable products: %v", err)
	}
	return NewRepository(pool, 15*time.Second)
}

// The buyer every one of these sales names. Only its SHAPE matters here: the register itself is
// the caller's guarantee (the 000173 lock), not this repository's.
const feedSaleBuyerVendorID = "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34"

func feedProducts() domain.ProductCatalog {
	return domain.NewProductCatalog([]domain.Product{
		{Code: "feed", Name: "Feed", Kind: domain.KindFeed, Unit: "kg", SortOrder: 40},
		{Code: domain.ProductCodeGoat, Name: domain.ProductGoat, Kind: domain.KindAnimal, Unit: "head", SpeciesCode: "goat", SortOrder: 20},
	})
}

func kg(v float64) *float64 { return &v }

// soldKg is what the feed store's ledger says this deal took off one farm.
func soldKg(t *testing.T, ctx context.Context, r *Repository, tenantID, farm, feed string) float64 {
	t.Helper()
	var total float64
	if err := r.pool.QueryRow(ctx, `
SELECT COALESCE(SUM(quantity_kg), 0) FROM public.feed_sale_depletions
WHERE tenant_id = $1 AND farm_label = $2 AND feed_item_label = $3`,
		tenantID, farm, feed).Scan(&total); err != nil {
		t.Fatalf("read depletion: %v", err)
	}
	return total
}

// TWO FEED LINES NAMING ONE FEED ARE TWO SALES OF IT (the OneToMany case).
//
// The ledger is written row for row from the deal's own lines, so a deal selling maize twice
// deplete the store twice. Grouping them first, or keying the ledger by (deal, feed), would make
// the second sale overwrite the first and the store would keep kilograms that left.
func TestFeedDepletionCountsEveryLineWhenOneToManyProductsRepeat(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)

	write := domain.DealWrite{
		SaleDate: "2026-09-23", Farm: "CPT",
		BuyerName: "Ramesh Traders", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{
			{ProductType: "Feed", Breed: "Maize", Quantity: kg(1200), RatePerUnit: kg(21)},
			{ProductType: "Feed", Breed: "Maize", Quantity: kg(800), RatePerUnit: kg(22)},
		},
	}.Normalize(feedProducts())
	if _, err := repo.CreateDeal(ctx, salesTestTenant, write, "", "two-line-key"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != 2000 {
		t.Fatalf("two maize lines must deplete 2000kg together, got %v", got)
	}
}

// A SALE STILL IN DISCUSSION HAS MOVED NO FEED (the StatusBuckets case).
//
// Feed leaves the store when the sale CLOSES. Depleting on the mere recording of an expected sale
// would take two tonnes off a store that still physically holds them, and the shortage would
// surface days later as a low-stock alert nobody could explain. Closing it later takes the feed;
// failing it afterwards gives it back.
func TestFeedDepletionFollowsTheDealStatusThroughEveryStatusBucket(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)

	write := domain.DealWrite{
		SaleDate: "2026-09-23", Farm: "CPT", Status: domain.StatusInDiscussion,
		BuyerName: "Ramesh Traders", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{
			{ProductType: "Feed", Breed: "Maize", Quantity: kg(2000), RatePerUnit: kg(21)},
		},
	}.Normalize(feedProducts())
	deal, err := repo.CreateDeal(ctx, salesTestTenant, write, "", "status-key")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != 0 {
		t.Fatalf("a sale in discussion must move no feed, got %v kg", got)
	}

	for _, step := range []struct {
		status string
		wantKg float64
		why    string
	}{
		{domain.StatusDealClosed, 2000, "closing the sale takes its feed off the store"},
		{domain.StatusDealFailed, 0, "a sale that fell through gives its feed back"},
		{domain.StatusDealClosed, 2000, "closing it again takes it once, never twice"},
		{domain.StatusAdvancePaid, 0, "an advance is not a delivery"},
	} {
		if _, err := repo.SetDealStatus(ctx, salesTestTenant, deal.DealID, step.status, ""); err != nil {
			t.Fatalf("set %s: %v", step.status, err)
		}
		if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != step.wantKg {
			t.Fatalf("%s: store shows %v kg sold, want %v", step.why, got, step.wantKg)
		}
	}
}

// ONE FARM'S SALE IS NOT ANOTHER FARM'S (the ParkScope case). The ledger carries the deal's own
// farm, so selling maize out of CPT must leave CBE's maize exactly where it was.
func TestFeedDepletionStaysInsideItsOwnParkScope(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)

	write := domain.DealWrite{
		SaleDate: "2026-09-23", Farm: "CPT",
		BuyerName: "Ramesh Traders", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{
			{ProductType: "Feed", Breed: "Maize", Quantity: kg(2000), RatePerUnit: kg(21)},
		},
	}.Normalize(feedProducts())
	if _, err := repo.CreateDeal(ctx, salesTestTenant, write, "", "scope-key"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != 2000 {
		t.Fatalf("CPT must show the sale, got %v", got)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CBE", "Maize"); got != 0 {
		t.Fatalf("CBE's store must be untouched by a CPT sale, got %v kg", got)
	}
}

// AN ANIMAL LINE TOUCHES NO FEED AT ALL, and a mixed sale depletes only its feed line. The
// insert is narrowed on product_kind, so selling goats and maize together takes the maize and
// leaves the goats out of the store's ledger entirely.
func TestFeedDepletionIgnoresAnimalLinesAcrossMultipleDimensions(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)

	write := domain.DealWrite{
		SaleDate: "2026-09-23", Farm: "CPT",
		BuyerName: "Ramesh Traders", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{
			{ProductType: "Goat", Breed: "Sojat", AnimalCount: kg(12), SalesValue: 90000},
			{ProductType: "Feed", Breed: "Maize", Quantity: kg(500), RatePerUnit: kg(21)},
		},
	}.Normalize(feedProducts())
	if _, err := repo.CreateDeal(ctx, salesTestTenant, write, "", "mixed-key"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != 500 {
		t.Fatalf("the feed line must deplete 500kg, got %v", got)
	}
	var rows int
	if err := repo.pool.QueryRow(ctx, `
SELECT count(*) FROM public.feed_sale_depletions WHERE tenant_id = $1`, salesTestTenant).Scan(&rows); err != nil {
		t.Fatalf("count depletions: %v", err)
	}
	if rows != 1 {
		t.Fatalf("a mixed sale must write ONE ledger row, its feed line; got %d", rows)
	}
}

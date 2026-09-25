package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	salesapp "github.com/vgoats/goatos/backend/internal/sales/app"
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
       ($1, 'goat', 'Goat', 'animal', 'number', 'goat', 20, true)
ON CONFLICT (tenant_id, product_code) DO NOTHING`, salesTestTenant); err != nil {
		t.Fatalf("seed the farm's sellable products: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public.feed_item_catalog (tenant_id, feed_item_label)
VALUES ($1, 'Maize'), ($1, 'Groundnut Cake') ON CONFLICT (tenant_id, feed_item_key) DO NOTHING`, salesTestTenant); err != nil {
		t.Fatalf("seed active feed catalog: %v", err)
	}
	return NewRepository(pool, 15*time.Second)
}

// The buyer every one of these sales names. Only its SHAPE matters here: the register itself is
// the caller's guarantee (the 000173 lock), not this repository's.
const feedSaleBuyerVendorID = "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34"

func feedProducts() domain.ProductCatalog {
	return domain.NewProductCatalog([]domain.Product{
		{Code: "feed", Name: "Feed", Kind: domain.KindFeed, Unit: "kg", SortOrder: 40},
		{Code: domain.ProductCodeGoat, Name: domain.ProductGoat, Kind: domain.KindAnimal, Unit: domain.UnitNumber, SpeciesCode: "goat", SortOrder: 20},
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
		{domain.StatusAdvancePaid, 0, "an advance is not a delivery"},
		{domain.StatusDealClosed, 2000, "closing it again takes it once, never twice"},
		// Deal Failed is final (2026-09-25), so it is the last step of the walk.
		{domain.StatusDealFailed, 0, "a sale that fell through gives its feed back"},
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

// WHAT A RECORDED DEAL TAKES OFF THE STORE, summed per feed, is the question the CLOSE asks --
// closing an expected sale is the moment its kilograms actually leave, and by then the store has
// moved. The sum is per FEED and not per line: a deal writing maize twice takes one amount off one
// balance, and asking each line on its own is how two lots slipped past a store neither exceeded.
func TestFeedDemandForDealSumsPerFeedNotPerLine(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)

	write := domain.DealWrite{
		SaleDate: "2026-09-24", Farm: "CPT", Status: domain.StatusInDiscussion,
		BuyerName: "Ramesh Traders", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{
			{ProductType: "Feed", Breed: "Maize", Quantity: kg(1200), RatePerUnit: kg(21)},
			{ProductType: domain.ProductGoat, Breed: "Sirohi", AnimalCount: kg(4), TotalWeightKg: kg(100), SalesValue: 45000},
			{ProductType: "Feed", Breed: "Maize", Quantity: kg(800), RatePerUnit: kg(22)},
			{ProductType: "Feed", Breed: "Groundnut Cake", Quantity: kg(300), RatePerUnit: kg(40)},
		},
	}.Normalize(feedProducts())
	deal, err := repo.CreateDeal(ctx, salesTestTenant, write, "", "demand-key")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	farm, status, demand, err := repo.FeedDemandForDeal(ctx, salesTestTenant, deal.DealID)
	if err != nil {
		t.Fatalf("read demand: %v", err)
	}
	if status != domain.StatusInDiscussion {
		t.Fatalf("status = %q, want In Discussion", status)
	}
	if farm != "CPT" {
		t.Fatalf("farm = %q, want CPT: the store asked is the one the sale leaves", farm)
	}
	// Two feeds, never three rows, and NEVER the animal line -- an animal takes nothing off the
	// feed store. The order is the one the farm typed, so the sentence names the row they would
	// look at first.
	if len(demand) != 2 {
		t.Fatalf("demand = %+v, want one row per feed", demand)
	}
	if demand[0].FeedItem != "Maize" || demand[0].Kg != 2000 || demand[0].LineNo != 1 {
		t.Fatalf("maize = %+v, want 2000kg reported at line 1", demand[0])
	}
	if demand[1].FeedItem != "Groundnut Cake" || demand[1].Kg != 300 {
		t.Fatalf("groundnut = %+v, want 300kg", demand[1])
	}
}

// A sale with no feed line asks the store nothing, and must not be mistaken for a missing deal --
// closing an animal sale is never held up by feed.
func TestFeedDemandForADealWithNoFeedLineIsEmptyAndNotAnError(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)

	write := domain.DealWrite{
		SaleDate: "2026-09-24", Farm: "CBE",
		BuyerName: "Tanveer", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{
			{ProductType: domain.ProductGoat, Breed: "Sirohi", AnimalCount: kg(4), TotalWeightKg: kg(100), SalesValue: 45000},
		},
	}.Normalize(feedProducts())
	deal, err := repo.CreateDeal(ctx, salesTestTenant, write, "", "no-feed-key")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	farm, status, demand, err := repo.FeedDemandForDeal(ctx, salesTestTenant, deal.DealID)
	if err != nil {
		t.Fatalf("an animal sale must read cleanly: %v", err)
	}
	if status != domain.StatusDealClosed {
		t.Fatalf("status = %q, want Deal Closed", status)
	}
	if farm != "CBE" || len(demand) != 0 {
		t.Fatalf("farm/demand = %q/%+v, want CBE and nothing owed to the store", farm, demand)
	}
}

type closeReplayStore struct {
	balance float64
	calls   int
}

func (s *closeReplayStore) FeedStockIdentity(feed string) (string, string) { return feed, feed }

func (s *closeReplayStore) FeedBalancesKg(context.Context, string, string) (map[string]float64, error) {
	s.calls++
	return map[string]float64{"Maize": s.balance}, nil
}

func TestFeedCloseReplayUsesPersistedStatusAndDepletesOnlyOnce(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)
	write := domain.DealWrite{
		SaleDate: "2026-09-24", Farm: "CPT", Status: domain.StatusInDiscussion,
		BuyerName: "Replay buyer", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{{ProductType: "Feed", Breed: "Maize", Quantity: kg(600), RatePerUnit: kg(20)}},
	}.Normalize(feedProducts())
	deal, err := repo.CreateDeal(ctx, salesTestTenant, write, "", "close-replay")
	if err != nil {
		t.Fatal(err)
	}
	stock := &closeReplayStore{balance: 1000}
	service := salesapp.NewSalesService(repo).WithFeedStock(stock)
	if _, err := service.SetDealStatus(ctx, salesTestTenant, deal.DealID, domain.StatusDealClosed, false, ""); err != nil {
		t.Fatal(err)
	}
	stock.balance = 1000 - soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize")
	if _, err := service.SetDealStatus(ctx, salesTestTenant, deal.DealID, domain.StatusDealClosed, false, ""); err != nil {
		t.Fatalf("replayed close: %v", err)
	}
	if stock.calls != 1 {
		t.Fatalf("stock reads = %d, want 1", stock.calls)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != 600 {
		t.Fatalf("depleted %v kg, want 600", got)
	}
	if _, err := service.SetDealStatus(ctx, salesTestTenant, deal.DealID, domain.StatusInDiscussion, false, ""); err != nil {
		t.Fatal(err)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != 0 {
		t.Fatalf("reopening left %v kg depleted", got)
	}
	_, err = service.SetDealStatus(ctx, salesTestTenant, deal.DealID, domain.StatusDealClosed, false, "")
	var short domain.ErrFeedStockShort
	if !errors.As(err, &short) {
		t.Fatalf("reopened sale must check current stock: %v", err)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != 0 {
		t.Fatalf("unconfirmed close depleted %v kg", got)
	}
}

func TestFeedSaleRejectsUnknownFeedInsideTransaction(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)
	write := domain.DealWrite{
		SaleDate: "2026-09-23", Farm: "CPT", BuyerName: "Unknown feed buyer", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{{ProductType: "Feed", Breed: "Unknown feed", Quantity: kg(50), RatePerUnit: kg(20)}},
	}.Normalize(feedProducts())
	_, err := repo.CreateDeal(ctx, salesTestTenant, write, "", "unknown-feed")
	var invalid domain.ErrDealValidation
	if !errors.As(err, &invalid) {
		t.Fatalf("expected field error, got %v", err)
	}
	var count int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM sales_deals WHERE tenant_id=$1 AND buyer_name='Unknown feed buyer'`, salesTestTenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("invalid feed sale committed")
	}
}

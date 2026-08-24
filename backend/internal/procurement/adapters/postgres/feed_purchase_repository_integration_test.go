package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// seedFeedPurchaseFixture creates a two-item feed catalog: one ACTIVE feed and one RETIRED feed.
// The retired row is the point -- it exists in the catalog, so a naive existence check would
// accept it. The CBE/CPT parks the farm labels resolve to are already in the pgtest baseline; the
// test reads their ids rather than seeding its own, so it exercises the same rows production does.
func seedFeedPurchaseFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, display_order, status)
VALUES ($1, 'Dry Sorghum Forage', 1, 'active'),
       ($1, 'Corn Silage', 2, 'retired')
ON CONFLICT (tenant_id, feed_item_key) DO NOTHING`, testTenant); err != nil {
		t.Fatalf("seed feed catalog: %v", err)
	}
}

func feedWrite() domain.FeedPurchaseWrite {
	return domain.FeedPurchaseWrite{
		PurchaseDate:  "2026-08-20",
		FarmLabel:     domain.FeedFarmCPT,
		FeedItemLabel: "Dry Sorghum Forage",
		QuantityKg:    5420,
		FeedCost:      f64(48980),
		TransportCost: f64(28000),
		Vendor:        "Siddi Srilekha",
		PaymentStatus: domain.FeedPaymentPaid,
	}
}

func f64(v float64) *float64 { return &v }

// parkIDByCode reads the baseline park a farm label must resolve to, the same way the ledger's
// insert does.
func parkIDByCode(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
SELECT location_id::text FROM locations
WHERE tenant_id = $1 AND location_type = 'park' AND upper(location_code) = $2`, testTenant, code).Scan(&id); err != nil {
		t.Fatalf("read %s park: %v", code, err)
	}
	return id
}

// TestFeedPurchaseLedgerPostgresPaths exercises the record-purchase write against a real Postgres.
//
// It is an integration test rather than a unit test on purpose: every rule it covers lives in the
// SQL and in the transaction boundary, not in Go. The catalog gate, the batch-number assignment,
// the natural-key duplicate, and the idempotency reservation are all invisible to a fake
// repository -- a service-level test would pass with none of them implemented.
func TestFeedPurchaseLedgerPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFeedPurchaseFixture(t, ctx, pool)

	repo := NewRepository(pool, 10*time.Second)

	t.Run("records a load, derives its costs and resolves its park", func(t *testing.T) {
		created, err := repo.CreateFeedPurchase(ctx, testTenant, feedWrite(), "", "load-1")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.EntrySource != "app" {
			t.Fatalf("entry_source = %q want app", created.EntrySource)
		}
		// The FIRST load of this feed at this farm is batch 1, assigned by the write.
		if created.BatchNo != 1 {
			t.Fatalf("batch_no = %d want 1", created.BatchNo)
		}
		// Landed cost is the split sum, and per-kg is derived from it -- neither was sent.
		if created.TotalCost == nil || *created.TotalCost != 76980 {
			t.Fatalf("total_cost = %v want 76980", created.TotalCost)
		}
		if created.PerKgCost == nil || *created.PerKgCost < 14.20 || *created.PerKgCost > 14.21 {
			t.Fatalf("per_kg_cost = %v want ~14.2", created.PerKgCost)
		}
		if created.PurchaseDate != "2026-08-20" {
			t.Fatalf("purchase_date = %q -- a business DATE must not shift through a timezone", created.PurchaseDate)
		}

		// The stock read depends on these three: park resolution by location_code, a zero
		// consumed-at-import (GoatOS has directed nothing against a load bought today), and
		// depletion starting on the purchase date.
		var parkID *string
		var consumed float64
		var depletesFrom time.Time
		if err := pool.QueryRow(ctx, `
SELECT park_id::text, consumed_at_import_kg, depletes_from
FROM feed_purchases WHERE feed_purchase_id = $1`, created.FeedPurchaseID).
			Scan(&parkID, &consumed, &depletesFrom); err != nil {
			t.Fatalf("read back: %v", err)
		}
		wantPark := parkIDByCode(t, ctx, pool, domain.FeedFarmCPT)
		if parkID == nil || *parkID != wantPark {
			t.Fatalf("park_id = %v want the CPT park %s", parkID, wantPark)
		}
		if consumed != 0 {
			t.Fatalf("consumed_at_import_kg = %v want 0", consumed)
		}
		if depletesFrom.Format("2006-01-02") != "2026-08-20" {
			t.Fatalf("depletes_from = %s want the purchase date", depletesFrom.Format("2006-01-02"))
		}
	})

	t.Run("assigns the next batch number per farm and feed", func(t *testing.T) {
		second, err := repo.CreateFeedPurchase(ctx, testTenant, feedWrite(), "", "load-2")
		if err != nil {
			t.Fatalf("second load: %v", err)
		}
		if second.BatchNo != 2 {
			t.Fatalf("second load batch_no = %d want 2", second.BatchNo)
		}
		// The counter is per (farm, feed): the SAME feed at the OTHER farm starts again at 1.
		other := feedWrite()
		other.FarmLabel = domain.FeedFarmCBE
		cbe, err := repo.CreateFeedPurchase(ctx, testTenant, other, "", "load-3")
		if err != nil {
			t.Fatalf("other farm: %v", err)
		}
		if cbe.BatchNo != 1 {
			t.Fatalf("CBE batch_no = %d want 1 -- the counter must not be shared across farms", cbe.BatchNo)
		}
		// Each farm label resolves to its OWN park by location_code -- the same mapping the
		// importer applies. A label that matched the wrong park would attribute a load, and the
		// stock it carries, to the farm that never bought it.
		var parkID *string
		if err := pool.QueryRow(ctx, `SELECT park_id::text FROM feed_purchases WHERE feed_purchase_id = $1`, cbe.FeedPurchaseID).Scan(&parkID); err != nil {
			t.Fatalf("read back CBE: %v", err)
		}
		wantCBE := parkIDByCode(t, ctx, pool, domain.FeedFarmCBE)
		if parkID == nil || *parkID != wantCBE {
			t.Fatalf("CBE park_id = %v want %s", parkID, wantCBE)
		}
		if wantCBE == parkIDByCode(t, ctx, pool, domain.FeedFarmCPT) {
			t.Fatal("the two farms must resolve to different parks, or this assertion proves nothing")
		}
	})

	t.Run("concurrent next-batch writes both land with consecutive batch numbers", func(t *testing.T) {
		before, err := repo.CreateFeedPurchase(ctx, testTenant, feedWrite(), "", "concurrent-before")
		if err != nil {
			t.Fatalf("seed preceding load: %v", err)
		}

		start := make(chan struct{})
		results := make(chan domain.FeedPurchase, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i, key := range []string{"concurrent-a", "concurrent-b"} {
			wg.Add(1)
			go func(i int, key string) {
				defer wg.Done()
				write := feedWrite()
				write.Vendor = []string{"Parallel Traders A", "Parallel Traders B"}[i]
				<-start
				created, err := repo.CreateFeedPurchase(ctx, testTenant, write, "", key)
				if err != nil {
					errs <- err
					return
				}
				results <- created
			}(i, key)
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)

		for err := range errs {
			t.Fatalf("concurrent create: %v", err)
		}
		got := make(map[int]bool, 2)
		for created := range results {
			got[created.BatchNo] = true
		}
		if !got[before.BatchNo+1] || !got[before.BatchNo+2] || len(got) != 2 {
			t.Fatalf("concurrent batch numbers = %v want %d and %d", got, before.BatchNo+1, before.BatchNo+2)
		}
	})

	t.Run("refuses a feed the active catalog does not carry", func(t *testing.T) {
		unknown := feedWrite()
		unknown.FeedItemLabel = "Green Guinea Grass"
		if _, err := repo.CreateFeedPurchase(ctx, testTenant, unknown, "", "load-unknown"); !errors.Is(err, ports.ErrFeedItemNotInCatalog) {
			t.Fatalf("unknown feed => %v, want ErrFeedItemNotInCatalog", err)
		}
		// RETIRED is the case a bare existence check would let through: the row is in the catalog,
		// but GoatOS can no longer ration it, so the purchase would have no stock card to appear on.
		retired := feedWrite()
		retired.FeedItemLabel = "Corn Silage"
		if _, err := repo.CreateFeedPurchase(ctx, testTenant, retired, "", "load-retired"); !errors.Is(err, ports.ErrFeedItemNotInCatalog) {
			t.Fatalf("retired feed => %v, want ErrFeedItemNotInCatalog", err)
		}
	})

	t.Run("normalizes the feed label to the catalog spelling", func(t *testing.T) {
		// feed_config_norm resolves the typed spelling; the CATALOG's label is what is stored, or
		// the stock cards would group one feed against itself.
		typed := feedWrite()
		typed.FeedItemLabel = "dry sorghum forage"
		created, err := repo.CreateFeedPurchase(ctx, testTenant, typed, "", "load-spelling")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.FeedItemLabel != "Dry Sorghum Forage" {
			t.Fatalf("stored feed label = %q want the catalog spelling", created.FeedItemLabel)
		}
	})

	t.Run("rejects a batch number already recorded", func(t *testing.T) {
		explicit := feedWrite()
		one := 1
		explicit.BatchNo = &one
		_, err := repo.CreateFeedPurchase(ctx, testTenant, explicit, "", "load-dup")
		if !errors.Is(err, ports.ErrFeedPurchaseDuplicateBatch) {
			t.Fatalf("duplicate batch => %v, want ErrFeedPurchaseDuplicateBatch", err)
		}
	})

	t.Run("an exact replay returns the original load and records nothing new", func(t *testing.T) {
		before := feedPurchaseCount(t, ctx, pool)
		write := feedWrite()
		write.Vendor = "Replay Traders"
		first, err := repo.CreateFeedPurchase(ctx, testTenant, write, "", "replay-key")
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		replay, err := repo.CreateFeedPurchase(ctx, testTenant, write, "", "replay-key")
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if replay.FeedPurchaseID != first.FeedPurchaseID {
			t.Fatalf("replay returned %s want the original %s", replay.FeedPurchaseID, first.FeedPurchaseID)
		}
		if got := feedPurchaseCount(t, ctx, pool); got != before+1 {
			t.Fatalf("ledger grew by %d over one load and its replay", got-before)
		}
	})

	t.Run("the same key with different fields is refused, not recorded", func(t *testing.T) {
		before := feedPurchaseCount(t, ctx, pool)
		changed := feedWrite()
		changed.Vendor = "Replay Traders"
		changed.QuantityKg = 6000 // a different load under a key already used
		_, err := repo.CreateFeedPurchase(ctx, testTenant, changed, "", "replay-key")
		if !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("same key, different payload => %v, want ErrIdempotencyConflict", err)
		}
		if got := feedPurchaseCount(t, ctx, pool); got != before {
			t.Fatalf("a refused replay wrote %d row(s)", got-before)
		}
	})

	t.Run("the ledger page carries whole-filter totals, not page sums", func(t *testing.T) {
		// Page size 1 over a ledger holding several loads: the totals must describe the FILTER,
		// never the single row on the page.
		page, err := repo.ListFeedPurchases(ctx, testTenant, "", 1, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(page.Purchases) != 1 {
			t.Fatalf("page rows = %d want 1", len(page.Purchases))
		}
		if page.Total <= 1 {
			t.Fatalf("total = %d -- must be the whole-filter count, not the page length", page.Total)
		}
		if page.QuantityKg <= page.Purchases[0].QuantityKg {
			t.Fatalf("quantity_kg = %v -- must aggregate the whole filter", page.QuantityKg)
		}

		// The farm filter narrows rows and totals through the SAME predicate.
		cbe, err := repo.ListFeedPurchases(ctx, testTenant, domain.FeedFarmCBE, 25, 0)
		if err != nil {
			t.Fatalf("list CBE: %v", err)
		}
		if cbe.Total != len(cbe.Purchases) {
			t.Fatalf("CBE total %d vs %d rows -- one page holds them all, so they must agree", cbe.Total, len(cbe.Purchases))
		}
		for _, p := range cbe.Purchases {
			if p.FarmLabel != domain.FeedFarmCBE {
				t.Fatalf("CBE filter returned a %s row", p.FarmLabel)
			}
		}
	})

	t.Run("the entry form offers only active catalog feeds", func(t *testing.T) {
		opts, err := repo.FeedPurchaseOptions(ctx, testTenant)
		if err != nil {
			t.Fatalf("options: %v", err)
		}
		labels := map[string]bool{}
		for _, item := range opts.FeedItems {
			labels[item.Label] = true
		}
		if !labels["Dry Sorghum Forage"] {
			t.Fatal("the active feed must be offered")
		}
		// The form must not offer a feed whose submit the write path would refuse.
		if labels["Corn Silage"] {
			t.Fatal("a retired feed must not be offered")
		}
		if len(opts.Vendors) == 0 {
			t.Fatal("vendors already bought from must be suggested")
		}
	})
}

func feedPurchaseCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_purchases WHERE tenant_id = $1`, testTenant).Scan(&count); err != nil {
		t.Fatalf("count feed purchases: %v", err)
	}
	return count
}

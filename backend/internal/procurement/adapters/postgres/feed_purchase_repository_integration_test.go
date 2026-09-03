package postgres

import (
	"context"
	"encoding/json"
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
		// DELIVERY (maintainer decision 2026-09-03): a load recorded with no arrival date is still
		// on the road -- not reached, no arrival day, and worth NOTHING in stock yet.
		if created.DeliveryStatus != domain.FeedDeliveryPurchased || created.ReachedOn != nil || created.StockKg() != nil {
			t.Fatalf("fresh load delivery = %q reached_on=%v stock=%v; want on the road", created.DeliveryStatus, created.ReachedOn, created.StockKg())
		}
		var stockKg float64
		if err := pool.QueryRow(ctx, `SELECT stock_kg FROM feed_purchases WHERE feed_purchase_id = $1`, created.FeedPurchaseID).Scan(&stockKg); err != nil {
			t.Fatalf("read stock_kg: %v", err)
		}
		if stockKg != 0 {
			t.Fatalf("stock_kg = %v for a load on the road, want 0", stockKg)
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
		page, err := repo.ListFeedPurchases(ctx, testTenant, "", "", 1, 0)
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
		cbe, err := repo.ListFeedPurchases(ctx, testTenant, domain.FeedFarmCBE, "", 25, 0)
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

// TestFeedPurchasePaymentPostgresPaths exercises the instalment ledger against a real Postgres.
//
// Integration rather than unit for the same reason as the create test: the running-total update,
// the status derivation under the row lock, and the idempotency reservation all live in SQL and
// in the transaction boundary. A fake repository proves none of them.
func TestFeedPurchasePaymentPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFeedPurchaseFixture(t, ctx, pool)

	repo := NewRepository(pool, 10*time.Second)

	// A load with a known landed cost and nothing released yet.
	write := feedWrite()
	write.PaymentStatus = domain.FeedPaymentPending
	created, err := repo.CreateFeedPurchase(ctx, testTenant, write, "", "pay-load-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.TotalCost == nil || *created.TotalCost != 76980 {
		t.Fatalf("fixture total = %v want 76980", created.TotalCost)
	}

	t.Run("first instalment advances the running total and stays Pending", func(t *testing.T) {
		after, err := repo.RecordFeedPurchasePayment(ctx, testTenant, created.FeedPurchaseID,
			domain.FeedPurchasePaymentWrite{PaidOn: "2026-08-21", AmountRupees: 30000, Note: "advance"}, "", "pay-1")
		if err != nil {
			t.Fatalf("record payment: %v", err)
		}
		if after.PaymentReleased == nil || *after.PaymentReleased != 30000 {
			t.Fatalf("payment_released = %v want 30000", after.PaymentReleased)
		}
		if after.PaymentStatus != domain.FeedPaymentPending {
			t.Fatalf("status = %q want Pending while money is still owed", after.PaymentStatus)
		}
		if len(after.Payments) != 1 || after.Payments[0].AmountRupees != 30000 || after.Payments[0].PaidOn != "2026-08-21" {
			t.Fatalf("payments = %#v want the one instalment", after.Payments)
		}
		if balance := after.PaymentBalance(); balance == nil || *balance != 46980 {
			t.Fatalf("balance = %v want 46980", balance)
		}
	})

	t.Run("exact replay records nothing twice", func(t *testing.T) {
		after, err := repo.RecordFeedPurchasePayment(ctx, testTenant, created.FeedPurchaseID,
			domain.FeedPurchasePaymentWrite{PaidOn: "2026-08-21", AmountRupees: 30000, Note: "advance"}, "", "pay-1")
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if after.PaymentReleased == nil || *after.PaymentReleased != 30000 || len(after.Payments) != 1 {
			t.Fatalf("replay changed the ledger: released=%v payments=%d", after.PaymentReleased, len(after.Payments))
		}
	})

	t.Run("same key with a different amount is refused", func(t *testing.T) {
		_, err := repo.RecordFeedPurchasePayment(ctx, testTenant, created.FeedPurchaseID,
			domain.FeedPurchasePaymentWrite{PaidOn: "2026-08-21", AmountRupees: 31000, Note: "advance"}, "", "pay-1")
		if !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("want ErrIdempotencyConflict, got %v", err)
		}
	})

	t.Run("the settling instalment flips the load to Paid", func(t *testing.T) {
		after, err := repo.RecordFeedPurchasePayment(ctx, testTenant, created.FeedPurchaseID,
			domain.FeedPurchasePaymentWrite{PaidOn: "2026-08-24", AmountRupees: 46980}, "", "pay-2")
		if err != nil {
			t.Fatalf("settling payment: %v", err)
		}
		if after.PaymentStatus != domain.FeedPaymentPaid {
			t.Fatalf("status = %q want Paid once the total is covered", after.PaymentStatus)
		}
		if len(after.Payments) != 2 {
			t.Fatalf("payments = %d want 2", len(after.Payments))
		}
		if balance := after.PaymentBalance(); balance == nil || *balance != 0 {
			t.Fatalf("balance = %v want 0", balance)
		}
	})

	t.Run("status edit flips directly and an unknown purchase is not found", func(t *testing.T) {
		after, err := repo.SetFeedPurchasePaymentStatus(ctx, testTenant, created.FeedPurchaseID, domain.FeedPaymentPending, "")
		if err != nil {
			t.Fatalf("set status: %v", err)
		}
		if after.PaymentStatus != domain.FeedPaymentPending {
			t.Fatalf("status = %q want Pending after the edit", after.PaymentStatus)
		}
		if _, err := repo.SetFeedPurchasePaymentStatus(ctx, testTenant, "00000000-0000-4000-8000-00000000dead", domain.FeedPaymentPaid, ""); !errors.Is(err, ports.ErrFeedPurchaseNotFound) {
			t.Fatalf("unknown purchase => %v want ErrFeedPurchaseNotFound", err)
		}
	})

	t.Run("payment against an unknown purchase writes nothing", func(t *testing.T) {
		_, err := repo.RecordFeedPurchasePayment(ctx, testTenant, "00000000-0000-4000-8000-00000000dead",
			domain.FeedPurchasePaymentWrite{PaidOn: "2026-08-24", AmountRupees: 10}, "", "pay-3")
		if !errors.Is(err, ports.ErrFeedPurchaseNotFound) {
			t.Fatalf("want ErrFeedPurchaseNotFound, got %v", err)
		}
		var rows int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_purchase_payments WHERE tenant_id = $1`, testTenant).Scan(&rows); err != nil {
			t.Fatalf("count payments: %v", err)
		}
		if rows != 2 {
			t.Fatalf("payment rows = %d want 2 (the refused write must leave nothing behind)", rows)
		}
	})

	t.Run("the page read carries every instalment batched", func(t *testing.T) {
		page, err := repo.ListFeedPurchases(ctx, testTenant, "", "", 25, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var found bool
		for _, p := range page.Purchases {
			if p.FeedPurchaseID == created.FeedPurchaseID {
				found = true
				if len(p.Payments) != 2 {
					t.Fatalf("listed payments = %d want 2", len(p.Payments))
				}
				if p.Payments[0].PaidOn != "2026-08-21" || p.Payments[1].PaidOn != "2026-08-24" {
					t.Fatalf("payments must list oldest first, got %#v", p.Payments)
				}
			}
		}
		if !found {
			t.Fatal("created purchase missing from the page read")
		}
	})
}

// TestFeedPurchaseEditPostgresPaths exercises the edit-purchase write against a real Postgres:
// the derived landed total, per-kg rate and payment status must all move with the edit, inside
// the same transaction as the row update.
func TestFeedPurchaseEditPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFeedPurchaseFixture(t, ctx, pool)

	repo := NewRepository(pool, 10*time.Second)

	write := feedWrite()
	write.PaymentStatus = domain.FeedPaymentPending
	created, err := repo.CreateFeedPurchase(ctx, testTenant, write, "", "edit-load-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Run("an edit re-derives total, per-kg and payment status", func(t *testing.T) {
		// Pay the load off in full first, then RAISE the cost: the derived status must drop back
		// to Pending because the released total no longer covers the new landed cost.
		if _, err := repo.RecordFeedPurchasePayment(ctx, testTenant, created.FeedPurchaseID,
			domain.FeedPurchasePaymentWrite{PaidOn: "2026-08-21", AmountRupees: 76980}, "", "edit-pay-1"); err != nil {
			t.Fatalf("settle: %v", err)
		}
		after, err := repo.UpdateFeedPurchase(ctx, testTenant, created.FeedPurchaseID, domain.FeedPurchaseEdit{
			PurchaseDate: "2026-08-19", QuantityKg: 12000,
			FeedCost: f64(90000), TransportCost: f64(10000),
			Vendor: "Siddi Srilekha",
		}, "")
		if err != nil {
			t.Fatalf("edit: %v", err)
		}
		if after.PurchaseDate != "2026-08-19" || after.QuantityKg != 12000 {
			t.Fatalf("date/quantity = %s/%v", after.PurchaseDate, after.QuantityKg)
		}
		if after.TotalCost == nil || *after.TotalCost != 100000 {
			t.Fatalf("total = %v want the re-derived 100000", after.TotalCost)
		}
		if after.PerKgCost == nil || *after.PerKgCost < 8.33 || *after.PerKgCost > 8.34 {
			t.Fatalf("per-kg = %v want ~8.33", after.PerKgCost)
		}
		if after.PaymentStatus != domain.FeedPaymentPending {
			t.Fatalf("status = %q want Pending once the raised cost exceeds the released total", after.PaymentStatus)
		}
		if balance := after.PaymentBalance(); balance == nil || *balance != 23020 {
			t.Fatalf("balance = %v want 23020", balance)
		}
	})

	t.Run("an identity-free edit keeps farm, feed and batch untouched", func(t *testing.T) {
		after, err := repo.getFeedPurchase(ctx, testTenant, created.FeedPurchaseID)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if after.FarmLabel != created.FarmLabel || after.FeedItemLabel != created.FeedItemLabel || after.BatchNo != created.BatchNo {
			t.Fatalf("identity moved: %s/%s/%d", after.FarmLabel, after.FeedItemLabel, after.BatchNo)
		}
	})

	t.Run("an edit of an unknown purchase is not found", func(t *testing.T) {
		_, err := repo.UpdateFeedPurchase(ctx, testTenant, "00000000-0000-4000-8000-00000000dead",
			domain.FeedPurchaseEdit{PurchaseDate: "2026-08-19", QuantityKg: 1, Vendor: "X"}, "")
		if !errors.Is(err, ports.ErrFeedPurchaseNotFound) {
			t.Fatalf("want ErrFeedPurchaseNotFound, got %v", err)
		}
	})
}

// TestFeedPurchaseDeliveryPostgresPaths exercises the arrival write against a real Postgres
// (maintainer decision 2026-09-03). Every rule it covers lives in SQL or on the transaction
// boundary: the generated stock_kg column, depletes_from following the arrival day, the
// transition-only outbox event, the received weight entered later, and the natural idempotency.
func TestFeedPurchaseDeliveryPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFeedPurchaseFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	reachedEvents := func() int {
		var n int
		if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1 AND event_type = 'procurement.feed_purchase.reached'`, testTenant).Scan(&n); err != nil {
			t.Fatalf("count reached events: %v", err)
		}
		return n
	}
	readStock := func(id string) (status string, stockKg float64, depletesFrom string) {
		var from time.Time
		if err := pool.QueryRow(ctx, `
SELECT delivery_status, stock_kg, depletes_from FROM feed_purchases WHERE feed_purchase_id = $1`, id).
			Scan(&status, &stockKg, &from); err != nil {
			t.Fatalf("read stock: %v", err)
		}
		return status, stockKg, from.Format("2006-01-02")
	}

	today := time.Now().In(time.FixedZone("IST", 5*3600+1800)).Format("2006-01-02")

	created, err := repo.CreateFeedPurchase(ctx, testTenant, feedWrite(), "", "delivery-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n := reachedEvents(); n != 0 {
		t.Fatalf("recording a load on the road emitted %d reached event(s); the toxin test must wait for the arrival", n)
	}

	t.Run("an arrival before the purchase date is refused under the row lock", func(t *testing.T) {
		_, err := repo.RecordFeedPurchaseDelivery(ctx, testTenant, created.FeedPurchaseID, domain.FeedPurchaseDeliveryWrite{ReachedOn: "2026-08-19"}, "")
		var v domain.ErrFeedPurchaseValidation
		if !errors.As(err, &v) || v.Field != "reached_on" {
			t.Fatalf("err = %v want a reached_on validation error", err)
		}
		if status, _, _ := readStock(created.FeedPurchaseID); status != domain.FeedDeliveryPurchased {
			t.Fatalf("a refused arrival flipped the load to %q", status)
		}
	})

	t.Run("marking reached makes the load stock from that day and raises the toxin test once", func(t *testing.T) {
		reached, err := repo.RecordFeedPurchaseDelivery(ctx, testTenant, created.FeedPurchaseID, domain.FeedPurchaseDeliveryWrite{ReachedOn: "2026-08-23"}, "")
		if err != nil {
			t.Fatalf("mark reached: %v", err)
		}
		if reached.DeliveryStatus != domain.FeedDeliveryReached || reached.ReachedOn == nil || *reached.ReachedOn != "2026-08-23" {
			t.Fatalf("after arrival: %+v", reached)
		}
		// No weighbridge figure yet: the buying weight is what the store counts.
		if got := reached.StockKg(); got == nil || *got != 5420 {
			t.Fatalf("stock after unweighed arrival = %v want 5420", got)
		}
		status, stockKg, depletesFrom := readStock(created.FeedPurchaseID)
		if status != domain.FeedDeliveryReached || stockKg != 5420 || depletesFrom != "2026-08-23" {
			t.Fatalf("row after arrival: status=%s stock_kg=%v depletes_from=%s", status, stockKg, depletesFrom)
		}
		if n := reachedEvents(); n != 1 {
			t.Fatalf("reached events = %d want exactly 1", n)
		}
		var payload struct {
			FeedPurchaseID string  `json:"feed_purchase_id"`
			ReachedOn      string  `json:"reached_on"`
			QuantityKg     float64 `json:"quantity_kg"`
			FeedItemKey    string  `json:"feed_item_key"`
			BatchNo        int     `json:"batch_no"`
		}
		var raw []byte
		if err := pool.QueryRow(ctx, `
SELECT payload -> 'payload' FROM outbox_messages
WHERE tenant_id = $1 AND event_type = 'procurement.feed_purchase.reached'`, testTenant).Scan(&raw); err != nil {
			t.Fatalf("read reached event: %v", err)
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload.FeedPurchaseID != created.FeedPurchaseID || payload.ReachedOn != "2026-08-23" ||
			payload.QuantityKg != 5420 || payload.FeedItemKey == "" || payload.BatchNo != created.BatchNo {
			t.Fatalf("reached payload = %+v", payload)
		}
	})

	t.Run("the received weight entered later corrects stock and the rate without a second test", func(t *testing.T) {
		kg := 5000.0
		weighed, err := repo.RecordFeedPurchaseDelivery(ctx, testTenant, created.FeedPurchaseID, domain.FeedPurchaseDeliveryWrite{ReachedOn: "2026-08-23", ReachedWeightKg: &kg}, "")
		if err != nil {
			t.Fatalf("enter weight: %v", err)
		}
		if got := weighed.StockKg(); got == nil || *got != 5000 {
			t.Fatalf("stock after weighing = %v want 5000", got)
		}
		if weighed.QuantityKg != 5420 {
			t.Fatalf("buying weight moved to %v; the received weight must not overwrite it", weighed.QuantityKg)
		}
		// 76980 landed over the 5000 kg actually received.
		if weighed.PerKgCost == nil || *weighed.PerKgCost < 15.39 || *weighed.PerKgCost > 15.40 {
			t.Fatalf("per_kg_cost = %v want ~15.396 over the received weight", weighed.PerKgCost)
		}
		if _, stockKg, _ := readStock(created.FeedPurchaseID); stockKg != 5000 {
			t.Fatalf("stock_kg column = %v want 5000", stockKg)
		}
		if n := reachedEvents(); n != 1 {
			t.Fatalf("a correction emitted another reached event (total %d)", n)
		}

		// Same values again: nothing changes, nothing is audited, nothing is emitted.
		var auditsBefore int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND resource_id = $2`, testTenant, created.FeedPurchaseID).Scan(&auditsBefore); err != nil {
			t.Fatalf("count audits: %v", err)
		}
		if _, err := repo.RecordFeedPurchaseDelivery(ctx, testTenant, created.FeedPurchaseID, domain.FeedPurchaseDeliveryWrite{ReachedOn: "2026-08-23", ReachedWeightKg: &kg}, ""); err != nil {
			t.Fatalf("replay: %v", err)
		}
		var auditsAfter int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND resource_id = $2`, testTenant, created.FeedPurchaseID).Scan(&auditsAfter); err != nil {
			t.Fatalf("recount audits: %v", err)
		}
		if auditsAfter != auditsBefore {
			t.Fatalf("an unchanged arrival write audited %d new row(s)", auditsAfter-auditsBefore)
		}
	})

	t.Run("a load recorded as already reached is stock and tested at once", func(t *testing.T) {
		write := feedWrite()
		write.FarmLabel = domain.FeedFarmCBE
		write.ReachedOn = "2026-08-22"
		kg := 5400.0
		write.ReachedWeightKg = &kg
		arrived, err := repo.CreateFeedPurchase(ctx, testTenant, write, "", "delivery-2")
		if err != nil {
			t.Fatalf("create reached: %v", err)
		}
		if arrived.DeliveryStatus != domain.FeedDeliveryReached || arrived.StockKg() == nil || *arrived.StockKg() != 5400 {
			t.Fatalf("reached-on-record: %+v", arrived)
		}
		if _, _, depletesFrom := readStock(arrived.FeedPurchaseID); depletesFrom != "2026-08-22" {
			t.Fatalf("depletes_from = %s want the arrival day, not the purchase date", depletesFrom)
		}
		if n := reachedEvents(); n != 2 {
			t.Fatalf("reached events = %d want 2 (one per load that reached)", n)
		}
		// An exact idempotent replay of the record form emits no second event.
		if _, err := repo.CreateFeedPurchase(ctx, testTenant, write, "", "delivery-2"); err != nil {
			t.Fatalf("replay: %v", err)
		}
		if n := reachedEvents(); n != 2 {
			t.Fatalf("reached events after replay = %d want still 2", n)
		}
	})

	t.Run("the ledger's delivery filter narrows rows and totals through one predicate", func(t *testing.T) {
		onRoad, err := repo.CreateFeedPurchase(ctx, testTenant, feedWrite(), "", "delivery-3")
		if err != nil {
			t.Fatalf("create third: %v", err)
		}
		page, err := repo.ListFeedPurchases(ctx, testTenant, "", domain.FeedDeliveryPurchased, 25, 0)
		if err != nil {
			t.Fatalf("list purchased: %v", err)
		}
		if page.Total != 1 || len(page.Purchases) != 1 || page.Purchases[0].FeedPurchaseID != onRoad.FeedPurchaseID {
			t.Fatalf("purchased filter: total=%d rows=%d", page.Total, len(page.Purchases))
		}
		if page.QuantityKg != 5420 {
			t.Fatalf("purchased filter quantity = %v want the one load on the road", page.QuantityKg)
		}
		_ = today
	})
}

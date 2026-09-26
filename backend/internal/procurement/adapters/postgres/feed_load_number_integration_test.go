package postgres

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// THE LOAD NUMBER IS AUTOMATIC (maintainer decision 2026-09-26), proved on real Postgres.
//
// Nobody types a load number any more, so the ledger's own assignment is the only thing that
// numbers the farm's loads. This pins every property a person relies on when they read "Load 12":
//
//   - it is ONE RUNNING COUNT across both farms and every feed (how the farm's sheet numbered its
//     loads), continuing from the highest number already in the ledger -- sheet history included --
//     never restarting at 1 and never repeating a number another farm or feed already carries;
//   - an exact retry of the same submit returns the same load and uses up no number;
//   - a refused write (a supplied number) uses up no number either;
//   - loads recorded AT THE SAME MOMENT (two desks, a double tap, a phone and the web) each get a
//     different number, with no gaps and no failures -- the advisory lock serialises the max+1.
func TestFeedLoadNumbersAreAssignedByTheLedger(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFeedPurchaseFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, display_order, status)
VALUES ($1, 'Maize', 3, 'active')
ON CONFLICT (tenant_id, feed_item_key) DO NOTHING`, testTenant); err != nil {
		t.Fatalf("seed second feed: %v", err)
	}
	// Sheet history: this farm's Dry Sorghum Forage already reached load 7 before the app.
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg,
    per_kg_cost, total_cost, consumed_at_import_kg, depletes_from, vendor, payment_status, delivery_status, reached_on)
VALUES ($1, $2, 'CPT', 'Dry Sorghum Forage', 7, '2026-08-01', 1000, 10, 10000, 0, '2026-08-01', 'Sheet Vendor', 'Paid', 'reached', '2026-08-01')`,
		testTenant, parkIDByCode(t, ctx, pool, "CPT")); err != nil {
		t.Fatalf("seed sheet history: %v", err)
	}

	repo := NewRepository(pool, 30*time.Second)
	record := func(key string, mutate func(*domain.FeedPurchaseWrite)) (int, error) {
		w := feedWrite()
		if mutate != nil {
			mutate(&w)
		}
		got, err := repo.CreateFeedPurchase(ctx, testTenant, w, "", key)
		return got.BatchNo, err
	}
	mustRecord := func(key string, mutate func(*domain.FeedPurchaseWrite)) int {
		t.Helper()
		n, err := record(key, mutate)
		if err != nil {
			t.Fatalf("record %s: %v", key, err)
		}
		return n
	}

	// Continues from the ledger's highest, one at a time.
	if n := mustRecord("seq-1", nil); n != 8 {
		t.Fatalf("first app load after sheet load 7 = %d, want 8", n)
	}
	if n := mustRecord("seq-2", nil); n != 9 {
		t.Fatalf("next load = %d, want 9", n)
	}

	// ONE count: another feed and the other farm continue the SAME sequence.
	if n := mustRecord("maize-1", func(w *domain.FeedPurchaseWrite) { w.FeedItemLabel = "Maize" }); n != 10 {
		t.Fatalf("a Maize load at CPT = %d, want 10 (every feed shares the one running count)", n)
	}
	if n := mustRecord("cbe-1", func(w *domain.FeedPurchaseWrite) { w.FarmLabel = "CBE" }); n != 11 {
		t.Fatalf("a load at CBE = %d, want 11 (both farms share the one running count)", n)
	}

	// An exact retry returns the same load and consumes nothing.
	if n := mustRecord("seq-2", nil); n != 9 {
		t.Fatalf("retry of seq-2 = %d, want the original 9", n)
	}
	// A refused write consumes nothing.
	if _, err := record("refused", func(w *domain.FeedPurchaseWrite) { b := 50; w.BatchNo = &b }); err == nil {
		t.Fatal("a supplied load number must be refused")
	}
	if n := mustRecord("seq-3", nil); n != 12 {
		t.Fatalf("load after a retry and a refusal = %d, want 12 -- neither may use up a number", n)
	}

	// At the same moment: twelve submits race, spread over both farms and two feeds, because they
	// all draw on the one count.
	const racers = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	numbers := make([]int, racers)
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			numbers[i], errs[i] = record(fmt.Sprintf("race-%02d", i), func(w *domain.FeedPurchaseWrite) {
				if i%2 == 1 {
					w.FarmLabel = "CBE"
				}
				if i%3 == 0 {
					w.FeedItemLabel = "Maize"
				}
			})
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d failed: %v -- a concurrent submit must get a number, not an error", i, err)
		}
	}
	sort.Ints(numbers)
	for i, n := range numbers {
		if want := 13 + i; n != want {
			t.Fatalf("concurrent numbers = %v, want 13..%d each exactly once", numbers, 12+racers)
		}
	}

	// The ledger agrees: every load in the tenant carries a different number, and none is missing.
	var total, distinct, maxNo int
	if err := pool.QueryRow(ctx, `
SELECT count(*), count(DISTINCT batch_no), max(batch_no)
FROM feed_purchases WHERE tenant_id = $1`, testTenant).Scan(&total, &distinct, &maxNo); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if want := 1 + 5 + racers; total != want || distinct != want || maxNo != 12+racers {
		t.Fatalf("ledger rows=%d distinct=%d max=%d; want %d distinct numbers ending at %d", total, distinct, maxNo, want, 12+racers)
	}
}

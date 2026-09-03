package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// TestStockCountsOnlyLoadsThatReachedOneToManyParkScopeStatusBucketsNoPageBoundary pins the stock side of the feed-purchase delivery decision
// (maintainer 2026-09-03): a load still on the road contributes NOTHING to a feed's balance, and
// the moment it is marked reached it contributes the weight actually received -- at the ledger's
// generated stock_kg, so the Stock card, the forecast and the low-stock alert all agree by
// construction. The failing shape it guards against is the pre-decision one, where a recorded
// purchase went straight into stock on its purchase date. Two loads (one-to-many onto one item
// row), both delivery buckets, park-scoped, and no page: the read is a bounded card list.
func TestStockCountsOnlyLoadsThatReachedOneToManyParkScopeStatusBucketsNoPageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	park := fdiPark

	insert := func(batch int, delivery string, qty string, reachedOn *string, reachedKg *string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status, reached_on, reached_weight_kg)
VALUES ($1, $2, 'CBE', 'UHT Milk', $3, DATE '2026-08-07', $4::numeric, 63.64, 38184,
        0, COALESCE($6::date, DATE '2026-08-07'), 'Balamurugan Enterprises', 'Pending',
        $5, $6::date, $7::numeric)`,
			fdiTenant, park, batch, qty, delivery, reachedOn, reachedKg); err != nil {
			t.Fatalf("insert load %d: %v", batch, err)
		}
	}
	day := "2026-08-07"
	insert(326, "reached", "600", &day, nil)
	// A second load of the same feed, bought but still on the road.
	insert(327, "purchased", "1000", nil, nil)

	for _, d := range []struct{ day, qty string }{{"2026-08-18", "40"}, {"2026-08-19", "30"}, {"2026-08-20", "28"}, {"2026-08-21", "26"}} {
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_external_consumption (tenant_id, park_id, farm_label, feed_item_label, feed_day, quantity_kg, batch_no, source_ref)
VALUES ($1, $2, 'CBE', 'UHT Milk', $3::date, $4::numeric, 326, 'test')`, fdiTenant, park, d.day, d.qty); err != nil {
			t.Fatalf("insert consumption %s: %v", d.day, err)
		}
	}

	balance := func() string {
		t.Helper()
		got, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{ParkIDs: []uuid.UUID{uuid.MustParse(fdiPark)}})
		if err != nil {
			t.Fatalf("StockAnalytics: %v", err)
		}
		for _, item := range got.Items {
			if item.FeedItemKey == "uht_milk" && item.FarmLabel == "CBE" {
				return item.BalanceKg
			}
		}
		t.Fatalf("UHT Milk stock item missing: %+v", got.Items)
		return ""
	}

	// Only the reached load counts: 600 - (40+30+28+26) = 476. The 1000 kg on the road is absent.
	if got := balance(); got != "476.0" {
		t.Fatalf("balance with a load on the road = %q, want 476.0 (the in-transit 1000 kg must not count)", got)
	}

	// The truck comes in and is weighed at 900 kg: stock is what was RECEIVED, not what was bought.
	if _, err := pool.Exec(ctx, `
UPDATE feed_purchases SET delivery_status = 'reached', reached_on = DATE '2026-08-22',
       depletes_from = DATE '2026-08-22', reached_weight_kg = 900
WHERE tenant_id = $1 AND batch_no = 327`, fdiTenant); err != nil {
		t.Fatalf("mark reached: %v", err)
	}
	if got := balance(); got != "1376.0" {
		t.Fatalf("balance after arrival = %q, want 1376.0 (476 + 900 received)", got)
	}

	// Park scope: another park's caller sees neither load, reached or not.
	other, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{ParkIDs: []uuid.UUID{uuid.New()}})
	if err != nil {
		t.Fatalf("StockAnalytics other park: %v", err)
	}
	for _, item := range other.Items {
		if item.FeedItemKey == "uht_milk" {
			t.Fatalf("another park's scope saw this farm's load: %+v", item)
		}
	}

	// The low-stock alert reads the same ledger rule: with 1376 kg at 28 kg/day nothing is low.
	low, err := repo.LowStockFeeds(ctx, fdiTenant, 7)
	if err != nil {
		t.Fatalf("LowStockFeeds: %v", err)
	}
	for _, f := range low {
		if f.FeedItemKey == "uht_milk" {
			t.Fatalf("UHT Milk reported low after the second load reached: %+v", f)
		}
	}
}

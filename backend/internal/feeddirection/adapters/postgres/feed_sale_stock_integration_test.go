package postgres

import (
	"context"
	"testing"
)

// SOLD FEED LEAVES THE STORE, AND IS NEVER EATEN (migration 000393).
//
// The farm sells feed it holds, and those kilograms are gone: the Stock card's balance must fall
// by exactly what was sold. What must NOT move is the burn rate -- a sale is one truck on one day,
// not the farm's daily draw -- so days-left may only change because the balance did, never because
// the divisor grew. Both halves are asserted on a real database round trip, because the arithmetic
// they pin lives in SQL and a Go-level test would prove nothing about it.
func TestAFeedSaleReducesTheBalanceWithoutTouchingTheBurnRate(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	park := fdiPark
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status)
VALUES ($1, $2, 'CBE', 'Maize', 701, DATE '2026-08-01', 5000, 20, 100000, 0,
        DATE '2026-08-01', 'Navaladi', 'Paid', 'reached')`, fdiTenant, park); err != nil {
		t.Fatalf("insert purchase: %v", err)
	}

	itemFor := func(t *testing.T, label string) (balance, rate string, daysLeft *int64) {
		t.Helper()
		items, err := repo.stockItems(ctx, fdiTenant, nil)
		if err != nil {
			t.Fatalf("stock items: %v", err)
		}
		for _, it := range items {
			if it.FeedItemLabel == label {
				return it.BalanceKg, it.AvgDailyKg, it.DaysLeft
			}
		}
		t.Fatalf("%s missing from the stock cards", label)
		return "", "", nil
	}

	beforeBalance, beforeRate, beforeDays := itemFor(t, "Maize")

	// The farm sells two tonnes of it.
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, sales_value)
VALUES ($1, DATE '2026-08-20', 'CBE', 'Ramesh Traders', 'Feed', 'Maize', 42000)`, fdiTenant); err != nil {
		t.Fatalf("insert deal: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deal_lines (tenant_id, deal_id, line_no, product_type, product_code, product_kind,
                              breed, quantity, unit, rate_per_unit, sales_value)
SELECT $1, d.id, 1, 'Feed', 'feed', 'feed', 'Maize', 2000, 'kg', 21, 42000
FROM sales_deals d WHERE d.tenant_id = $1 AND d.buyer_name = 'Ramesh Traders'`, fdiTenant); err != nil {
		t.Fatalf("insert line: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_sale_depletions (tenant_id, deal_id, line_id, park_id, farm_label,
                                  feed_item_label, feed_day, quantity_kg)
SELECT $1, l.deal_id, l.line_id, $2, 'CBE', 'Maize', DATE '2026-08-20', 2000
FROM sales_deal_lines l WHERE l.tenant_id = $1 AND l.product_kind = 'feed'`, fdiTenant, park); err != nil {
		t.Fatalf("insert depletion: %v", err)
	}

	afterBalance, afterRate, afterDays := itemFor(t, "Maize")

	if beforeBalance != "5000.0" {
		t.Fatalf("balance before the sale = %q, want the whole load", beforeBalance)
	}
	if afterBalance != "3000.0" {
		t.Fatalf("balance after selling 2000kg = %q, want 3000.0", afterBalance)
	}
	// The rate is what decides days-left. A sale must not appear in it: if it did, this farm would
	// read as eating two tonnes of maize and the card would announce a store about to run dry.
	if afterRate != beforeRate {
		t.Fatalf("the burn rate moved on a sale: %q -> %q", beforeRate, afterRate)
	}
	if beforeDays != nil || afterDays != nil {
		t.Fatalf("a feed nobody has eaten has no days-left: before=%v after=%v", beforeDays, afterDays)
	}
}

// A feed NOBODY has sold keeps its balance. The sold ledger is LEFT JOINed for exactly this
// reason; an inner join would drop every unsold feed off the Stock tab entirely.
func TestAFeedNobodyHasSoldKeepsItsBalance(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	park := fdiPark
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status)
VALUES ($1, $2, 'CBE', 'Groundnut Cake', 702, DATE '2026-08-01', 800, 30, 24000, 0,
        DATE '2026-08-01', 'Navaladi', 'Paid', 'reached')`, fdiTenant, park); err != nil {
		t.Fatalf("insert purchase: %v", err)
	}
	items, err := repo.stockItems(ctx, fdiTenant, nil)
	if err != nil {
		t.Fatalf("stock items: %v", err)
	}
	for _, it := range items {
		if it.FeedItemLabel == "Groundnut Cake" {
			if it.BalanceKg != "800.0" {
				t.Fatalf("an unsold feed's balance = %q, want 800.0", it.BalanceKg)
			}
			return
		}
	}
	t.Fatal("an unsold feed dropped off the stock cards entirely")
}

// The Go twin of feed_config_norm() must agree with the database function label for label. It is
// hand-written, so this is the only thing standing between it and silent drift -- a sale of
// "Mesha  Kids-Goat Concentrate" must find the store row that holds "Mesha Kids Goat Concentrate".
func TestTheGoFeedKeyAgreesWithTheDatabaseFunction(t *testing.T) {
	ctx := context.Background()
	_, pool := setupIssueDB(t, ctx)

	for _, label := range []string{
		"Maize",
		"Mesha Kids Goat Concentrate",
		"  Mesha  Kids-Goat_Concentrate  ",
		"GROUNDNUT CAKE",
		"UHT Milk",
		"Silage - Maize",
		"a",
	} {
		var want string
		if err := pool.QueryRow(ctx, `SELECT feed_config_norm($1)`, label).Scan(&want); err != nil {
			t.Fatalf("feed_config_norm(%q): %v", label, err)
		}
		if got := feedConfigNorm(label); got != want {
			t.Fatalf("feed key for %q: Go says %q, the database says %q", label, got, want)
		}
	}
}

// A sale is answered at the balance the Stock tab SHOWS, and a feed the ledger has never carried
// answers "unknown" rather than "nothing left" -- the two are different facts and only one of them
// is worth warning a person about.
func TestFeedBalanceAnswersTheStoreAndKnowsWhenItCannot(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	park := fdiPark
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status)
VALUES ($1, $2, 'CBE', 'Maize', 703, DATE '2026-08-01', 5000, 20, 100000, 0,
        DATE '2026-08-01', 'Navaladi', 'Paid', 'reached')`, fdiTenant, park); err != nil {
		t.Fatalf("insert purchase: %v", err)
	}

	kg, known, err := repo.FeedBalanceKg(ctx, fdiTenant, "CBE", "  maize ")
	if err != nil {
		t.Fatalf("feed balance: %v", err)
	}
	if !known || kg != 5000 {
		t.Fatalf("CBE maize = %v (known=%v), want 5000 found by the normalised key", kg, known)
	}

	// Another farm's store is not this farm's store.
	if _, known, err := repo.FeedBalanceKg(ctx, fdiTenant, "CPT", "Maize"); err != nil || known {
		t.Fatalf("CPT must not answer from CBE's ledger: known=%v err=%v", known, err)
	}
	// A feed the ledger never carried has no opinion. Reporting 0 here would tell the desk the
	// store is empty of something it has simply never bought.
	if kg, known, err := repo.FeedBalanceKg(ctx, fdiTenant, "CBE", "Lucerne Hay"); err != nil || known || kg != 0 {
		t.Fatalf("an unledgered feed must be unknown, got kg=%v known=%v err=%v", kg, known, err)
	}
}

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// AN ADVANCE BEFORE ANYTHING IS CHOSEN (maintainer decision 2026-10-02), end to end on real
// Postgres: the advance is recorded with no products and opens no work, cannot close, takes its
// products once -- which is when sales.deal.recorded is emitted and the workflow would open -- and,
// on a sale that falls through, its money is refunded or kept.
func TestAdvanceOnlySalePostgresPathsOneToManyPageBoundaryEveryStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	advance := func(key string) domain.Deal {
		t.Helper()
		return seedDeal(t, repo, ctx, key, domain.DealWrite{
			SaleDate: "2026-10-02", Farm: "CBE", BuyerName: "Tanveer",
			BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
			AdvanceAmount: f64(50000), AdvanceOnly: true,
		})
	}
	recordedEvents := func(dealID string) (n int, hasAnimals, status string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `
SELECT count(*),
       coalesce(max(payload->'payload'->>'has_live_animals'), ''),
       coalesce(max(payload->'payload'->>'status'), '')
FROM outbox_messages
WHERE event_type = 'sales.deal.recorded' AND aggregate_id = $1::uuid`, dealID).Scan(&n, &hasAnimals, &status); err != nil {
			t.Fatal(err)
		}
		return
	}
	sheep := func(value float64) domain.DealLinesWrite {
		return domain.DealLinesWrite{Lines: []domain.DealLineWrite{
			{ProductType: domain.ProductSheep, Breed: "Anantapur", AnimalCount: f64(10), SalesValue: value},
		}}
	}
	addLines := func(dealID, key string, w domain.DealLinesWrite) (domain.Deal, error) {
		n, r := w.Normalize(builtinCatalog())
		if err := n.Validate(builtinCatalog(), r); err != nil {
			t.Fatalf("lines fixture invalid: %v", err)
		}
		return repo.AddDealLines(ctx, salesTestTenant, dealID, n, r, "", key)
	}

	bare := advance("adv-1")

	t.Run("the advance is recorded with no products, its money listed, and no work opened", func(t *testing.T) {
		if !bare.AdvanceOnly() || bare.Status != domain.StatusAdvancePaid || bare.SalesValue != 0 {
			t.Fatalf("advance-only sale = %+v", bare)
		}
		if bare.PaymentReceived == nil || *bare.PaymentReceived != 50000 || len(bare.Payments) != 1 {
			t.Fatalf("advance money = %v receipts %d, want 50000 in one receipt", bare.PaymentReceived, len(bare.Payments))
		}
		if n, _, _ := recordedEvents(bare.DealID); n != 0 {
			t.Fatalf("an advance-only sale emitted %d sales.deal.recorded; its work must wait for its products", n)
		}
	})

	t.Run("it cannot close before its products are added", func(t *testing.T) {
		if _, err := repo.SetDealStatus(ctx, salesTestTenant, bare.DealID, domain.StatusDealClosed, ""); !errors.Is(err, domain.ErrAdvanceOnlyCannotClose) {
			t.Fatalf("closing an advance-only sale: got %v", err)
		}
	})

	t.Run("products worth less than the money received are refused", func(t *testing.T) {
		_, err := addLines(bare.DealID, "lines-cheap", sheep(40000))
		var v domain.ErrDealValidation
		if !errors.As(err, &v) || v.Field != "lines" {
			t.Fatalf("lines below the advance: got %v", err)
		}
	})

	var sold domain.Deal
	t.Run("OneToMany lines roll up to one sale and emit recorded once", func(t *testing.T) {
		var err error
		sold, err = addLines(bare.DealID, "lines-1", domain.DealLinesWrite{Lines: []domain.DealLineWrite{
			{ProductType: domain.ProductSheep, Breed: "Anantapur", AnimalCount: f64(10), SalesValue: 90000},
			{ProductType: domain.ProductSheep, Breed: "Anantapur", AnimalCount: f64(2), SalesValue: 18000},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if sold.AdvanceOnly() || sold.ProductType != domain.ProductSheep || sold.SalesValue != 108000 || len(sold.Lines) != 2 {
			t.Fatalf("sale after lines = %+v", sold)
		}
		if sold.PaymentBalance() != 58000 {
			t.Fatalf("balance = %v, want 58000", sold.PaymentBalance())
		}
		n, hasAnimals, status := recordedEvents(bare.DealID)
		if n != 1 || hasAnimals != "true" || status != domain.StatusAdvancePaid {
			t.Fatalf("recorded events = %d (animals %q, status %q), want one, true, Advance Paid", n, hasAnimals, status)
		}
	})

	t.Run("PageBoundary replay returns the same rollup and a second add is refused", func(t *testing.T) {
		again, err := addLines(bare.DealID, "lines-1", domain.DealLinesWrite{Lines: []domain.DealLineWrite{
			{ProductType: domain.ProductSheep, Breed: "Anantapur", AnimalCount: f64(10), SalesValue: 90000},
			{ProductType: domain.ProductSheep, Breed: "Anantapur", AnimalCount: f64(2), SalesValue: 18000},
		}})
		if err != nil || again.SalesValue != 108000 {
			t.Fatalf("replay: %v %+v", err, again)
		}
		if _, err := addLines(bare.DealID, "lines-2", sheep(95000)); !errors.Is(err, domain.ErrDealAlreadyHasLines) {
			t.Fatalf("second add: got %v", err)
		}
		if n, _, _ := recordedEvents(bare.DealID); n != 1 {
			t.Fatalf("recorded events after replay = %d, want 1", n)
		}
	})

	t.Run("with its products it closes like any sale", func(t *testing.T) {
		closed, err := repo.SetDealStatus(ctx, salesTestTenant, bare.DealID, domain.StatusDealClosed, "")
		if err != nil || closed.Status != domain.StatusDealClosed {
			t.Fatalf("close: %v %+v", err, closed)
		}
	})

	t.Run("EveryStatus failed sale money is refunded or kept, and only then", func(t *testing.T) {
		gone := advance("adv-2")
		settle := func(key string, w domain.AdvanceSettlementWrite) (domain.Deal, error) {
			return repo.SettleDealAdvance(ctx, salesTestTenant, gone.DealID, w.Normalize(), "", key)
		}
		if _, err := settle("s-live", domain.AdvanceSettlementWrite{}); !errors.Is(err, domain.ErrSettlementNeedsFailedDeal) {
			t.Fatalf("settling a live sale: got %v", err)
		}
		if _, err := repo.SetDealStatus(ctx, salesTestTenant, gone.DealID, domain.StatusDealFailed, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := addLines(gone.DealID, "lines-gone", sheep(90000)); !errors.Is(err, domain.ErrDealFailedIsFinal) {
			t.Fatalf("adding products to a failed sale: got %v", err)
		}
		var v domain.ErrDealValidation
		if _, err := settle("s-over", domain.AdvanceSettlementWrite{RefundedRupees: 60000, RefundedOn: "2026-10-01"}); !errors.As(err, &v) || v.Field != "refunded_rupees" {
			t.Fatalf("refunding more than was paid: got %v", err)
		}
		part, err := settle("s-part", domain.AdvanceSettlementWrite{RefundedRupees: 20000, RefundedOn: "2026-10-01", Note: "cash"})
		if err != nil || part.Settlement == nil || part.Settlement.RefundedRupees != 20000 ||
			domain.SettlementOutcome(*part.Settlement, 50000) != domain.SettlementPartRefunded {
			t.Fatalf("part refund: %v %+v", err, part.Settlement)
		}
		kept, err := settle("s-keep", domain.AdvanceSettlementWrite{})
		if err != nil || kept.Settlement == nil || kept.Settlement.RefundedOn != nil ||
			domain.SettlementOutcome(*kept.Settlement, 50000) != domain.SettlementKept {
			t.Fatalf("keep replaces the part refund: %v %+v", err, kept.Settlement)
		}
		page, err := repo.ListDeals(ctx, salesTestTenant, "CBE", 25, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range page.Deals {
			if d.DealID == gone.DealID && (d.Settlement == nil || d.Settlement.RefundedRupees != 0) {
				t.Fatalf("ledger page settlement = %+v", d.Settlement)
			}
		}
	})
}

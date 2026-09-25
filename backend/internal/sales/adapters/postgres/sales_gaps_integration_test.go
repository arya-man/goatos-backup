package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	salesapp "github.com/vgoats/goatos/backend/internal/sales/app"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// An EDIT names the row it edits by its code. A code the registry does not carry is not an edit
// of anything -- it used to fall through the upsert and INSERT a brand-new row under a code the
// CLIENT chose, which is the one thing the add path exists to prevent (the code is derived from
// the name, once, by the server).
func TestEditingAnUnknownProductCodeIsRefusedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)
	service := salesapp.NewSalesService(repo)

	var before int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM public.sellable_product_catalog WHERE tenant_id = $1`, salesTestTenant).Scan(&before); err != nil {
		t.Fatal(err)
	}
	_, err := service.SaveSellableProduct(ctx, salesTestTenant, domain.ProductWrite{
		Code: "made_up_code", Name: "Sheep tags", Kind: domain.KindOther, Unit: domain.UnitNumber,
	}, "")
	if !errors.Is(err, ports.ErrProductNotFound) {
		t.Fatalf("edit of an unknown code: want ErrProductNotFound, got %v", err)
	}
	var after int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM public.sellable_product_catalog WHERE tenant_id = $1`, salesTestTenant).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("registry rows %d -> %d: a refused edit must write nothing", before, after)
	}

	// The two legitimate shapes still work: adding (no code) and editing a code that exists.
	added, err := service.SaveSellableProduct(ctx, salesTestTenant, domain.ProductWrite{Name: "Sheep tags", Kind: domain.KindOther, Unit: domain.UnitNumber}, "")
	if err != nil || added.Code != "sheep_tags" {
		t.Fatalf("add: %+v %v", added, err)
	}
	if _, err := service.SaveSellableProduct(ctx, salesTestTenant, domain.ProductWrite{Code: "sheep_tags", Name: "Ear tags", Kind: domain.KindOther, Unit: domain.UnitNumber}, ""); err != nil {
		t.Fatalf("edit of an existing code: %v", err)
	}
}

func advanceDeal(value, advance float64, saleDate string) domain.DealWrite {
	return domain.DealWrite{
		SaleDate: saleDate, Farm: "CBE", ProductType: "Goat", Breed: "Malai",
		BuyerName: "Mahendran", BuyerVendorID: feedSaleBuyerVendorID,
		AnimalCount: f64(17), SalesValue: value, AdvanceAmount: f64(advance),
	}
}

// The advance paid when the sale is recorded is a RECEIPT like any other: it is listed, it can be
// corrected or removed, and the running total is exactly the sum of the listed receipts. It used
// to live only inside payment_received, so the desk could not see it in the receipts list, re-
// entered it as a receipt, and the CBE deal of 02/09/2026 (value 1,97,415) read 3,94,830 received.
func TestTheAdvanceAtSaleIsTheFirstReceipt(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)
	service := salesapp.NewSalesService(repo)

	deal, err := service.CreateDeal(ctx, salesTestTenant, advanceDeal(197415, 20000, "2026-09-02"), "", "adv-1")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if len(deal.Payments) != 1 || deal.Payments[0].AmountRupees != 20000 {
		t.Fatalf("payments = %#v, want the advance as the first receipt", deal.Payments)
	}
	if deal.PaymentReceived == nil || *deal.PaymentReceived != 20000 {
		t.Fatalf("payment_received = %v, want 20000 (the advance counted once)", deal.PaymentReceived)
	}
	today := biztime.BusinessDate(time.Now())
	wantDate := "2026-09-02"
	if today < wantDate {
		wantDate = today
	}
	if deal.Payments[0].ReceivedOn != wantDate {
		t.Fatalf("advance dated %s, want %s", deal.Payments[0].ReceivedOn, wantDate)
	}

	// Exact replay: no second receipt.
	replay, err := service.CreateDeal(ctx, salesTestTenant, advanceDeal(197415, 20000, "2026-09-02"), "", "adv-1")
	if err != nil || len(replay.Payments) != 1 || *replay.PaymentReceived != 20000 {
		t.Fatalf("replay: %+v %v", replay.Payments, err)
	}

	// The advance is editable like any receipt, and the total follows.
	paymentID := deal.Payments[0].PaymentID
	edited, err := service.UpdateDealPayment(ctx, salesTestTenant, deal.DealID, paymentID,
		domain.DealPaymentWrite{ReceivedOn: deal.Payments[0].ReceivedOn, AmountRupees: 25000, Note: "Advance at sale"}, "", "adv-edit")
	if err != nil || *edited.PaymentReceived != 25000 {
		t.Fatalf("edit advance: %v %v", edited.PaymentReceived, err)
	}
	// ...and removable, leaving nothing received.
	removed, err := service.DeleteDealPayment(ctx, salesTestTenant, deal.DealID, paymentID, "", "adv-del")
	if err != nil || len(removed.Payments) != 0 || removed.PaymentReceived == nil || *removed.PaymentReceived != 0 {
		t.Fatalf("remove advance: %+v %v %v", removed.Payments, removed.PaymentReceived, err)
	}

	// No advance -> no receipt row.
	none, err := service.CreateDeal(ctx, salesTestTenant, advanceDeal(50000, 0, "2026-09-02"), "", "adv-0")
	if err != nil || len(none.Payments) != 0 {
		t.Fatalf("zero advance: %+v %v", none.Payments, err)
	}
}

// An advance larger than the sale is money the ledger cannot explain; it is refused with a field
// error, and nothing is recorded.
func TestAnAdvanceAboveTheSaleValueIsRefused(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)
	service := salesapp.NewSalesService(repo)

	_, err := service.CreateDeal(ctx, salesTestTenant, advanceDeal(100000, 100001, "2026-09-02"), "", "adv-over")
	var v domain.ErrDealValidation
	if !errors.As(err, &v) || v.Field != "advance_amount" {
		t.Fatalf("want advance_amount validation, got %v", err)
	}
	if herr := salesapp.SalesHTTPError(err); herr == nil || herr.Code != "sales_invalid_advance_amount" {
		t.Fatalf("http error = %+v", herr)
	}
	var deals int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM public.sales_deals WHERE tenant_id = $1`, salesTestTenant).Scan(&deals); err != nil {
		t.Fatal(err)
	}
	if deals != 0 {
		t.Fatalf("a refused sale left %d deals behind", deals)
	}
	// Exactly the sale value is a fully paid sale, not an error.
	if _, err := service.CreateDeal(ctx, salesTestTenant, advanceDeal(100000, 100000, "2026-09-02"), "", "adv-full"); err != nil {
		t.Fatalf("advance == value: %v", err)
	}
}

// A receipt that would take the money received past the sale value is refused -- that is the shape
// of the double entry above. A correction that LOWERS an over-received total is always allowed, so
// a deal already over cannot get stuck.
func TestAReceiptCannotTakeTheTotalPastTheSaleValue(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)
	service := salesapp.NewSalesService(repo)

	deal, err := service.CreateDeal(ctx, salesTestTenant, advanceDeal(197415, 197415, "2026-09-02"), "", "cap-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RecordDealPayment(ctx, salesTestTenant, deal.DealID,
		domain.DealPaymentWrite{ReceivedOn: "2026-09-02", AmountRupees: 197415, Note: "re-entered advance"}, "", "cap-dup")
	if !errors.Is(err, ports.ErrPaymentExceedsSaleValue) {
		t.Fatalf("double entry: want ErrPaymentExceedsSaleValue, got %v", err)
	}
	if herr := salesapp.SalesHTTPError(err); herr == nil || herr.Code != "payment_exceeds_sale_value" {
		t.Fatalf("http error = %+v", herr)
	}
	after, err := repo.getDeal(ctx, salesTestTenant, deal.DealID)
	if err != nil || len(after.Payments) != 1 || *after.PaymentReceived != 197415 {
		t.Fatalf("refused receipt changed the ledger: %+v %v", after.Payments, err)
	}
	// Raising the one receipt past the value is refused too; lowering it is fine.
	if _, err := service.UpdateDealPayment(ctx, salesTestTenant, deal.DealID, after.Payments[0].PaymentID,
		domain.DealPaymentWrite{ReceivedOn: after.Payments[0].ReceivedOn, AmountRupees: 200000}, "", "cap-raise"); !errors.Is(err, ports.ErrPaymentExceedsSaleValue) {
		t.Fatalf("raise past value: %v", err)
	}
	if _, err := service.UpdateDealPayment(ctx, salesTestTenant, deal.DealID, after.Payments[0].PaymentID,
		domain.DealPaymentWrite{ReceivedOn: after.Payments[0].ReceivedOn, AmountRupees: 100000}, "", "cap-lower"); err != nil {
		t.Fatalf("lower: %v", err)
	}

	// A deal ALREADY over-received (history) can still be corrected downwards.
	if _, err := repo.pool.Exec(ctx, `UPDATE public.sales_deals SET payment_received = 394830 WHERE id = $1`, deal.DealID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateDealPayment(ctx, salesTestTenant, deal.DealID, after.Payments[0].PaymentID,
		domain.DealPaymentWrite{ReceivedOn: after.Payments[0].ReceivedOn, AmountRupees: 90000}, "", "cap-lower-hist"); err != nil {
		t.Fatalf("lowering an already-over total must be allowed: %v", err)
	}
}

// Migration 000440 gives every existing advance its receipt row, once: afterwards the running
// total is the sum of the listed receipts, and a second run changes nothing. A deal whose total
// disagrees for some OTHER reason is left alone.
func TestAdvanceReceiptBackfillIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)
	pool := repo.pool

	// Three sheet-era shapes, written as the pre-000440 code left them: the advance only in the
	// running total, with no receipt row.
	insert := func(advance, received float64, saleDate string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO public.sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, sales_value, advance_amount, payment_received, status)
VALUES ($1, $2::date, 'CBE', 'Mahendran', 'Goat', 'Malai', 197415, $3, $4, 'Deal Closed') RETURNING id::text`,
			salesTestTenant, saleDate, advance, received).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	plain := insert(20000, 20000, "2026-09-02")
	// The OCI shape: the advance, plus the desk re-entering it as a receipt.
	doubled := insert(197415, 394830, "2026-09-02")
	if _, err := pool.Exec(ctx, `INSERT INTO public.sales_deal_payments (tenant_id, deal_id, received_on, amount_rupees, note) VALUES ($1, $2, '2026-09-02', 197415, 're-entered')`, salesTestTenant, doubled); err != nil {
		t.Fatal(err)
	}
	// Disagrees for another reason: received is below the advance. Not touched.
	odd := insert(50000, 10000, "2026-09-02")

	for run := 1; run <= 2; run++ {
		if _, err := pool.Exec(ctx, advanceBackfillUp(t)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		for _, c := range []struct {
			id           string
			rows         int
			sumEqualsRec bool
		}{{plain, 1, true}, {doubled, 2, true}, {odd, 0, false}} {
			var rows int
			var sum, received float64
			if err := pool.QueryRow(ctx, `
SELECT count(p.payment_id), COALESCE(sum(p.amount_rupees), 0), d.payment_received
FROM public.sales_deals d LEFT JOIN public.sales_deal_payments p ON p.tenant_id = d.tenant_id AND p.deal_id = d.id
WHERE d.id = $1 GROUP BY d.payment_received`, c.id).Scan(&rows, &sum, &received); err != nil {
				t.Fatal(err)
			}
			if rows != c.rows || (sum == received) != c.sumEqualsRec {
				t.Fatalf("run %d deal %s: rows=%d sum=%v received=%v", run, c.id, rows, sum, received)
			}
		}
	}
}

// advanceBackfillUp is the Up half of the backfill migration, run by hand so the test can stage the
// pre-migration rows first (the pgtest template has already applied every migration).
func advanceBackfillUp(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../../../../migrations/postgres/000440_sales_advance_is_a_receipt.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	down := strings.Index(s, "-- +goose Down")
	if down < 0 {
		t.Fatal("migration has no Down marker")
	}
	return strings.Replace(s[:down], "-- +goose Up", "", 1)
}

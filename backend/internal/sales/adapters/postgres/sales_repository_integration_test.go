package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

const salesTestTenant = "00000000-0000-4000-8000-000000000001"

func f64(v float64) *float64 { return &v }

func seedDeal(t *testing.T, repo *Repository, ctx context.Context, key string, w domain.DealWrite) domain.Deal {
	t.Helper()
	deal, err := repo.CreateDeal(ctx, salesTestTenant, w.Normalize(), "", key)
	if err != nil {
		t.Fatalf("seed deal %s: %v", key, err)
	}
	return deal
}

// TestSalesLedgerPostgresPaths exercises the sales ledger against a real Postgres. Integration
// rather than unit on purpose: the defects this file catches live in the SQL -- swapped same-typed
// columns in the 22-column scan, the shared WHERE between page and total, and the idempotency
// reservation racing the insert inside one transaction.
func TestSalesLedgerPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	created := seedDeal(t, repo, ctx, "key-sheep", domain.DealWrite{
		SaleDate: "2025-04-15", Farm: "CPT", ProductType: "Sheep", Breed: "Anantapur",
		BuyerName: "Tanveer", BuyerPlace: "Madur",
		BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		AnimalCount:   f64(23), MaleCount: f64(12), FemaleCount: f64(11),
		TotalWeightKg: f64(600), SalesValue: 201500, AdvanceAmount: f64(50000),
		Comments: "prefers morning delivery",
	})

	t.Run("every column lands in its own field", func(t *testing.T) {
		if created.SaleDate != "2025-04-15" || created.Farm != "CPT" {
			t.Fatalf("date/farm = %s/%s", created.SaleDate, created.Farm)
		}
		if created.BuyerName != "Tanveer" || created.BuyerPlace == nil || *created.BuyerPlace != "Madur" {
			t.Fatalf("buyer mismapped: %+v", created)
		}
		// The vendor link round-trips as the id it was handed. It is scanned right after
		// buyer_place, so a swapped pair in the column list would surface here rather than as a
		// sale silently attributed to the wrong counterparty.
		if created.BuyerVendorID == nil || *created.BuyerVendorID != "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34" {
			t.Fatalf("vendor link mismapped: %+v", created.BuyerVendorID)
		}
		if created.ProductType != "Sheep" || created.Breed != "Anantapur" {
			t.Fatalf("product/breed = %s/%s", created.ProductType, created.Breed)
		}
		if created.AnimalCount == nil || *created.AnimalCount != 23 ||
			created.MaleCount == nil || *created.MaleCount != 12 ||
			created.FemaleCount == nil || *created.FemaleCount != 11 {
			t.Fatalf("counts mismapped: %+v", created)
		}
		if created.TotalWeightKg == nil || *created.TotalWeightKg != 600 {
			t.Fatalf("weight mismapped: %+v", created.TotalWeightKg)
		}
		if created.SalesValue != 201500 || created.AdvanceAmount == nil || *created.AdvanceAmount != 50000 {
			t.Fatalf("money mismapped: %v/%v", created.SalesValue, created.AdvanceAmount)
		}
		if created.Status != domain.StatusDealClosed {
			t.Fatalf("status = %q want the recorded default", created.Status)
		}
		if created.Comments == nil || *created.Comments != "prefers morning delivery" {
			t.Fatalf("comments mismapped: %+v", created.Comments)
		}
	})

	t.Run("create writes the audit row in the same transaction", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM audit_log
			WHERE tenant_id = $1 AND action = 'sales.deal.record' AND resource_type = 'sales_deal' AND resource_id = $2`,
			salesTestTenant, created.DealID).Scan(&count); err != nil {
			t.Fatalf("audit read: %v", err)
		}
		if count != 1 {
			t.Fatalf("audit rows = %d want 1", count)
		}
	})

	t.Run("idempotency trio", func(t *testing.T) {
		write := domain.DealWrite{
			SaleDate: "2025-05-01", Farm: "CBE", ProductType: "Goat", Breed: "Sojat",
			BuyerName: "Irshad", SalesValue: 90000, TotalWeightKg: f64(200),
		}
		first, err := repo.CreateDeal(ctx, salesTestTenant, write.Normalize(), "", "key-goat")
		if err != nil {
			t.Fatalf("first call: %v", err)
		}

		// Exact replay: the ORIGINAL row comes back and nothing new is written.
		replay, err := repo.CreateDeal(ctx, salesTestTenant, write.Normalize(), "", "key-goat")
		if err != nil {
			t.Fatalf("exact replay: %v", err)
		}
		if replay.DealID != first.DealID {
			t.Fatalf("replay returned a different deal: %s vs %s", replay.DealID, first.DealID)
		}
		var deals int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sales_deals WHERE tenant_id = $1 AND buyer_name = 'Irshad'`, salesTestTenant).Scan(&deals); err != nil {
			t.Fatalf("count: %v", err)
		}
		if deals != 1 {
			t.Fatalf("replay duplicated the deal: %d rows", deals)
		}
		var audits int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM audit_log
			WHERE tenant_id = $1 AND action = 'sales.deal.record' AND resource_id = $2`, salesTestTenant, first.DealID).Scan(&audits); err != nil {
			t.Fatalf("audit count: %v", err)
		}
		if audits != 1 {
			t.Fatalf("replay re-ran the audit side effect: %d rows", audits)
		}

		// Same key, different payload: refused, and still nothing new written.
		mutated := write
		mutated.SalesValue = 95000
		if _, err := repo.CreateDeal(ctx, salesTestTenant, mutated.Normalize(), "", "key-goat"); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("conflicting replay: want ErrIdempotencyConflict, got %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sales_deals WHERE tenant_id = $1 AND buyer_name = 'Irshad'`, salesTestTenant).Scan(&deals); err != nil {
			t.Fatalf("count: %v", err)
		}
		if deals != 1 {
			t.Fatalf("conflicting replay mutated state: %d rows", deals)
		}
	})

	t.Run("list pages with a whole-filter total", func(t *testing.T) {
		seedDeal(t, repo, ctx, "key-manure", domain.DealWrite{
			SaleDate: "2025-05-10", Farm: "CBE", ProductType: "Manure", Breed: "Manure",
			BuyerName: "Raitha FPO", SalesValue: 30000, TotalWeightKg: f64(3000),
		})

		page, err := repo.ListDeals(ctx, salesTestTenant, "", 2, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(page.Deals) != 2 || page.Total != 3 {
			t.Fatalf("page rows/total = %d/%d want 2/3 (total is whole-filter)", len(page.Deals), page.Total)
		}
		// Newest sale first.
		if page.Deals[0].SaleDate < page.Deals[1].SaleDate {
			t.Fatalf("order wrong: %s before %s", page.Deals[0].SaleDate, page.Deals[1].SaleDate)
		}

		cbe, err := repo.ListDeals(ctx, salesTestTenant, "CBE", 25, 0)
		if err != nil {
			t.Fatalf("farm list: %v", err)
		}
		if cbe.Total != 2 || len(cbe.Deals) != 2 {
			t.Fatalf("CBE rows/total = %d/%d", len(cbe.Deals), cbe.Total)
		}
		for _, d := range cbe.Deals {
			if d.Farm != "CBE" {
				t.Fatalf("farm filter leaked: %+v", d)
			}
		}
	})

	t.Run("overview aggregates the whole filter on a DB round trip", func(t *testing.T) {
		// Pipeline + evidence fixtures, written directly: those tables are import/read surfaces
		// with no API write path.
		if _, err := pool.Exec(ctx, `
			INSERT INTO sales_buyer_leads (tenant_id, buyer_name, buyer_place, farm, call_status) VALUES
			($1, 'Mutton shop', 'Coimbatore', 'CBE', 'Interested'),
			($1, 'Trader A', 'Coimbatore', 'CBE', NULL),
			($1, 'Trader B', 'Salem', 'CPT', 'No Answer')`, salesTestTenant); err != nil {
			t.Fatalf("seed leads: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO sales_fpo_leads (tenant_id, fpo_name, district, call_status) VALUES
			($1, 'Raitha Dwani FPCL', 'Chamarajanagar', 'No Answer')`, salesTestTenant); err != nil {
			t.Fatalf("seed fpo: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO sales_sold_animal_tags (tenant_id, animal_label, tag_number, farm, source_sales_id) VALUES
			($1, 'Malai Goat', '155', 'CBE', 7),
			($1, 'Malai Goat', '156', 'CBE', 7),
			($1, 'Anantapur Sheep', 'V-90', 'CPT', 8)`, salesTestTenant); err != nil {
			t.Fatalf("seed tags: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO sales_weight_audit (tenant_id, video_weight_kg, book_weight_kg, farm_born) VALUES
			($1, 23, 23, false),
			($1, 22.5, 23, true),
			($1, 30, 25, false)`, salesTestTenant); err != nil {
			t.Fatalf("seed audit: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO sales_market_benchmarks (tenant_id, market, category, breed, market_price_per_kg) VALUES
			($1, 'Chennai -Sheep - 370 Rs Per kg', 'Sheep', 'Ongole', 370)`, salesTestTenant); err != nil {
			t.Fatalf("seed benchmarks: %v", err)
		}

		overview, err := repo.GetOverview(ctx, salesTestTenant, "")
		if err != nil {
			t.Fatalf("overview: %v", err)
		}
		// Closed deals: sheep 201500 + goat 90000 + manure 30000.
		if overview.Summary.Deals != 3 || overview.Summary.Revenue != 321500 {
			t.Fatalf("summary deals/revenue = %d/%v", overview.Summary.Deals, overview.Summary.Revenue)
		}
		// The goat deal recorded no counts (animal_count and the split all NULL), so only the
		// sheep deal's 23 animals count -- absence stays absence, never an invented count.
		if overview.Summary.Animals != 23 || overview.Summary.Sheep != 23 || overview.Summary.Goats != 0 {
			t.Fatalf("summary animals/sheep/goats = %v/%v/%v",
				overview.Summary.Animals, overview.Summary.Sheep, overview.Summary.Goats)
		}
		if overview.Summary.ManureKg != 3000 || overview.Summary.ManureRevenue != 30000 {
			t.Fatalf("manure block = %v/%v", overview.Summary.ManureKg, overview.Summary.ManureRevenue)
		}
		if len(overview.Monthly) != 2 {
			t.Fatalf("monthly rows = %d", len(overview.Monthly))
		}

		if overview.BuyerPipeline.Total != 3 {
			t.Fatalf("buyer pipeline total = %d", overview.BuyerPipeline.Total)
		}
		// The NULL call status must be reported, not dropped, and under the backend-owned key.
		foundUncontacted := false
		for _, s := range overview.BuyerPipeline.Statuses {
			if s.Status == domain.UncontactedStatusKey && s.Count == 1 {
				foundUncontacted = true
			}
		}
		if !foundUncontacted {
			t.Fatalf("uncontacted bucket missing: %+v", overview.BuyerPipeline.Statuses)
		}

		if overview.FPOPipeline.Total != 1 || len(overview.FPOPipeline.Districts) != 1 {
			t.Fatalf("fpo pipeline: %+v", overview.FPOPipeline)
		}

		if overview.TagRoster.Total != 3 || overview.TagRoster.SalesCount != 2 {
			t.Fatalf("tag roster total/sales = %d/%d (sales_count must be DISTINCT source_sales_id)",
				overview.TagRoster.Total, overview.TagRoster.SalesCount)
		}
		if overview.TagRoster.ByType[0].Label != "Malai Goat" || overview.TagRoster.ByType[0].Count != 2 {
			t.Fatalf("tag roster by_type: %+v", overview.TagRoster.ByType)
		}

		if overview.WeightAudit.Total != 3 || overview.WeightAudit.Within03Kg != 1 ||
			overview.WeightAudit.Within1Kg != 1 || overview.WeightAudit.Over1Kg != 1 {
			t.Fatalf("weight audit: %+v", overview.WeightAudit)
		}
		if overview.WeightAudit.MaxGapKg != 5 {
			t.Fatalf("max gap = %v", overview.WeightAudit.MaxGapKg)
		}

		if len(overview.MarketBenchmarks) != 1 || overview.MarketBenchmarks[0].MarketPricePerKg == nil ||
			*overview.MarketBenchmarks[0].MarketPricePerKg != 370 {
			t.Fatalf("benchmarks: %+v", overview.MarketBenchmarks)
		}

		// Farm scope: CPT sees only its own deals and leads, and the whole-filter blocks stay
		// consistent with that scope.
		cpt, err := repo.GetOverview(ctx, salesTestTenant, "CPT")
		if err != nil {
			t.Fatalf("cpt overview: %v", err)
		}
		if cpt.Summary.Deals != 1 || cpt.Summary.Revenue != 201500 {
			t.Fatalf("cpt summary = %d/%v", cpt.Summary.Deals, cpt.Summary.Revenue)
		}
		if cpt.BuyerPipeline.Total != 1 || cpt.TagRoster.Total != 1 {
			t.Fatalf("cpt pipeline/tags = %d/%d", cpt.BuyerPipeline.Total, cpt.TagRoster.Total)
		}
		// Company-wide panels do not pretend to filter.
		if cpt.WeightAudit.Total != 3 || len(cpt.MarketBenchmarks) != 1 || cpt.FPOPipeline.Total != 1 {
			t.Fatalf("company-wide panels changed under a farm filter: %+v", cpt.WeightAudit)
		}
	})

	t.Run("PageBoundaryPaginationNeverChangesTheTotalOrTheOverview", func(t *testing.T) {
		before, err := repo.GetOverview(ctx, salesTestTenant, "")
		if err != nil {
			t.Fatalf("overview: %v", err)
		}
		// Walk the ledger page by page across a page boundary: the whole-filter total is
		// identical on every page, the pages are disjoint, and together they cover the total.
		seen := map[string]bool{}
		total := -1
		for offset := 0; ; offset += 2 {
			page, err := repo.ListDeals(ctx, salesTestTenant, "", 2, offset)
			if err != nil {
				t.Fatalf("page at %d: %v", offset, err)
			}
			if total == -1 {
				total = page.Total
			}
			if page.Total != total {
				t.Fatalf("total changed across pages: %d vs %d", page.Total, total)
			}
			for _, d := range page.Deals {
				if seen[d.DealID] {
					t.Fatalf("page boundary repeated deal %s", d.DealID)
				}
				seen[d.DealID] = true
			}
			if len(page.Deals) == 0 {
				break
			}
		}
		if len(seen) != total {
			t.Fatalf("pages covered %d deals, whole-filter total says %d", len(seen), total)
		}
		// The overview is whole-filter and cannot be affected by anyone paging the ledger.
		after, err := repo.GetOverview(ctx, salesTestTenant, "")
		if err != nil {
			t.Fatalf("overview after paging: %v", err)
		}
		if after.Summary != before.Summary {
			t.Fatalf("overview summary moved while paging: %+v vs %+v", after.Summary, before.Summary)
		}
	})

	t.Run("EveryStatusBucketsOnlyClosedDealsEnterTheOverview", func(t *testing.T) {
		before, err := repo.GetOverview(ctx, salesTestTenant, "")
		if err != nil {
			t.Fatalf("overview: %v", err)
		}
		// One deal in EVERY non-closed status, inserted directly because the API write records
		// closed sales only. None of them may move a single overview number.
		if _, err := pool.Exec(ctx, `
			INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, sales_value, status) VALUES
			($1, '2025-06-01', 'CBE', 'Pending Buyer', 'Sheep', 'Anantapur', 50000, 'In Discussion'),
			($1, '2025-06-02', 'CBE', 'Advance Buyer', 'Goat', 'Sojat', 80000, 'Advance Paid'),
			($1, '2025-06-03', 'CPT', 'Failed Buyer', 'Sheep', 'Anantapur', 0, 'Deal Failed')`, salesTestTenant); err != nil {
			t.Fatalf("seed statuses: %v", err)
		}
		after, err := repo.GetOverview(ctx, salesTestTenant, "")
		if err != nil {
			t.Fatalf("overview: %v", err)
		}
		if after.Summary != before.Summary {
			t.Fatalf("a non-closed status leaked into the summary: %+v vs %+v", after.Summary, before.Summary)
		}
		if len(after.Monthly) != len(before.Monthly) {
			t.Fatalf("a non-closed status created a month: %+v", after.Monthly)
		}
		// The LEDGER, by contrast, lists every status -- the two reads deliberately disagree
		// about membership because one is the record and the other is the closed-revenue rollup.
		page, err := repo.ListDeals(ctx, salesTestTenant, "", 100, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		statuses := map[string]bool{}
		for _, d := range page.Deals {
			statuses[d.Status] = true
		}
		for _, want := range []string{domain.StatusDealClosed, domain.StatusInDiscussion, domain.StatusAdvancePaid, domain.StatusDealFailed} {
			if !statuses[want] {
				t.Fatalf("ledger must list every status; %q missing from %v", want, statuses)
			}
		}
	})
}

// TestSalesDealPaymentPostgresPaths exercises the buyer-receipts ledger against a real Postgres.
//
// Integration rather than unit for the same reason as the feed-purchase instalment test: the
// running-total update under the row lock, the advance seeding, and the idempotency reservation
// all live in SQL and in the transaction boundary.
func TestSalesDealPaymentPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	deal := seedDeal(t, repo, ctx, "pay-deal-1", domain.DealWrite{
		SaleDate: "2026-08-20", Farm: "CPT", ProductType: "Goat", Breed: "Sirohi",
		BuyerName: "Tanveer", BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		AnimalCount: f64(10), SalesValue: 100000, AdvanceAmount: f64(20000),
	})

	t.Run("a recorded advance seeds the running total and the balance", func(t *testing.T) {
		if deal.PaymentReceived == nil || *deal.PaymentReceived != 20000 {
			t.Fatalf("payment_received = %v want the advance 20000", deal.PaymentReceived)
		}
		if got := deal.PaymentBalance(); got != 80000 {
			t.Fatalf("balance = %v want 80000", got)
		}
	})

	t.Run("a receipt advances the running total", func(t *testing.T) {
		after, err := repo.RecordDealPayment(ctx, salesTestTenant, deal.DealID,
			domain.DealPaymentWrite{ReceivedOn: "2026-08-25", AmountRupees: 50000, Note: "on pickup"}, "", "rcpt-1")
		if err != nil {
			t.Fatalf("record receipt: %v", err)
		}
		if after.PaymentReceived == nil || *after.PaymentReceived != 70000 {
			t.Fatalf("payment_received = %v want 70000", after.PaymentReceived)
		}
		if got := after.PaymentBalance(); got != 30000 {
			t.Fatalf("balance = %v want 30000", got)
		}
		if len(after.Payments) != 1 || after.Payments[0].AmountRupees != 50000 || after.Payments[0].ReceivedOn != "2026-08-25" {
			t.Fatalf("payments = %#v want the one receipt", after.Payments)
		}
		// Status stays a human decision: money must not flip the deal lifecycle.
		if after.Status != domain.StatusDealClosed {
			t.Fatalf("status = %q, a receipt must not change it", after.Status)
		}
	})

	t.Run("exact replay records nothing twice", func(t *testing.T) {
		after, err := repo.RecordDealPayment(ctx, salesTestTenant, deal.DealID,
			domain.DealPaymentWrite{ReceivedOn: "2026-08-25", AmountRupees: 50000, Note: "on pickup"}, "", "rcpt-1")
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if after.PaymentReceived == nil || *after.PaymentReceived != 70000 || len(after.Payments) != 1 {
			t.Fatalf("replay changed the ledger: received=%v payments=%d", after.PaymentReceived, len(after.Payments))
		}
	})

	t.Run("same key with a different amount is refused", func(t *testing.T) {
		_, err := repo.RecordDealPayment(ctx, salesTestTenant, deal.DealID,
			domain.DealPaymentWrite{ReceivedOn: "2026-08-25", AmountRupees: 51000, Note: "on pickup"}, "", "rcpt-1")
		if !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("want ErrIdempotencyConflict, got %v", err)
		}
	})

	t.Run("receipt against an unknown deal writes nothing", func(t *testing.T) {
		_, err := repo.RecordDealPayment(ctx, salesTestTenant, "00000000-0000-4000-8000-00000000dead",
			domain.DealPaymentWrite{ReceivedOn: "2026-08-25", AmountRupees: 10}, "", "rcpt-2")
		if !errors.Is(err, ports.ErrDealNotFound) {
			t.Fatalf("want ErrDealNotFound, got %v", err)
		}
		var rows int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sales_deal_payments WHERE tenant_id = $1`, salesTestTenant).Scan(&rows); err != nil {
			t.Fatalf("count receipts: %v", err)
		}
		if rows != 1 {
			t.Fatalf("receipt rows = %d want 1 (the refused write must leave nothing behind)", rows)
		}
	})

	t.Run("the page read carries every receipt batched", func(t *testing.T) {
		page, err := repo.ListDeals(ctx, salesTestTenant, "", 25, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var found bool
		for _, d := range page.Deals {
			if d.DealID == deal.DealID {
				found = true
				if len(d.Payments) != 1 || d.Payments[0].AmountRupees != 50000 {
					t.Fatalf("listed payments = %#v want the one receipt", d.Payments)
				}
			}
		}
		if !found {
			t.Fatal("seeded deal missing from the page read")
		}
	})
}

// TestExpectedSaleAdvanceStory pins the maintainer's 2026-08-31 scenario end to end: an advance
// received TODAY for a sale expected on a FUTURE date is recorded as an Advance Paid deal, its
// receipt is dated by when the money arrived, and on the day the animals leave the status is
// flipped to Deal Closed -- by the desk, never by the money.
func TestExpectedSaleAdvanceStory(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	// Recorded 2026-08-31 for animals leaving 2026-09-02.
	deal := seedDeal(t, repo, ctx, "expected-1", domain.DealWrite{
		SaleDate: "2026-09-02", Farm: "CPT", ProductType: "Goat", Breed: "Sirohi",
		BuyerName: "Tanveer", BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		AnimalCount: f64(12), SalesValue: 150000,
		Status: domain.StatusAdvancePaid,
	})
	if deal.Status != domain.StatusAdvancePaid {
		t.Fatalf("status = %q want the named Advance Paid, not the Deal Closed default", deal.Status)
	}
	if deal.SaleDate != "2026-09-02" {
		t.Fatalf("sale_date = %q want the expected future date", deal.SaleDate)
	}

	// The advance arrives today, dated by when the money moved, not by the sale date.
	after, err := repo.RecordDealPayment(ctx, salesTestTenant, deal.DealID,
		domain.DealPaymentWrite{ReceivedOn: "2026-08-31", AmountRupees: 40000, Note: "advance"}, "", "expected-rcpt-1")
	if err != nil {
		t.Fatalf("advance receipt: %v", err)
	}
	if after.PaymentReceived == nil || *after.PaymentReceived != 40000 || after.PaymentBalance() != 110000 {
		t.Fatalf("received/balance = %v/%v want 40000/110000", after.PaymentReceived, after.PaymentBalance())
	}
	if after.Status != domain.StatusAdvancePaid {
		t.Fatalf("status = %q, money must not move the lifecycle", after.Status)
	}

	// A blank status still records the sheet's default -- the named status is opt-in.
	defaulted := seedDeal(t, repo, ctx, "expected-2", domain.DealWrite{
		SaleDate: "2026-08-30", Farm: "CBE", ProductType: "Sheep", Breed: "Anantapur",
		BuyerName: "Keethiraj", BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		AnimalCount: f64(5), SalesValue: 50000,
	})
	if defaulted.Status != domain.StatusDealClosed {
		t.Fatalf("blank status = %q want the Deal Closed default", defaulted.Status)
	}

	// On 2026-09-02 the animals leave and the desk closes the deal.
	closed, err := repo.SetDealStatus(ctx, salesTestTenant, deal.DealID, domain.StatusDealClosed, "")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if closed.Status != domain.StatusDealClosed {
		t.Fatalf("status = %q want Deal Closed", closed.Status)
	}
	if closed.PaymentReceived == nil || *closed.PaymentReceived != 40000 {
		t.Fatalf("closing must not touch the money: received = %v", closed.PaymentReceived)
	}
	if _, err := repo.SetDealStatus(ctx, salesTestTenant, "00000000-0000-4000-8000-00000000dead", domain.StatusDealClosed, ""); !errors.Is(err, ports.ErrDealNotFound) {
		t.Fatalf("unknown deal => %v want ErrDealNotFound", err)
	}
}

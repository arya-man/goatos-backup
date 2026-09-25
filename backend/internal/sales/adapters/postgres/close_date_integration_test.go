package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// A CLOSED SALE USES THE CLOSE DATE (maintainer decision 2026-09-25). A sale recorded open for a
// planned day and closed today is a sale made TODAY: its sale_date -- which dates its revenue and
// the feed store's depletion -- becomes the close business date (Asia/Kolkata, the server's
// clock), and the planned date is kept as history on the FIRST close only. A re-close changes
// nothing; a sale recorded already closed keeps the date it was recorded with.
func TestClosingASaleStampsTheCloseDateAndKeepsThePlannedOne(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)
	today := biztime.BusinessDate(time.Now())
	planned := biztime.BusinessDayStart(time.Now()).AddDate(0, 0, 20).Format("2006-01-02")

	write := domain.DealWrite{
		SaleDate: planned, Farm: "CPT", Status: domain.StatusAdvancePaid,
		BuyerName: "Planned buyer", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{{ProductType: "Feed", Breed: "Maize", Quantity: kg(500), RatePerUnit: kg(20)}},
	}.Normalize(feedProducts())
	deal, err := repo.CreateDeal(ctx, salesTestTenant, write, "", "close-date-key")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := repo.SetDealStatus(ctx, salesTestTenant, deal.DealID, domain.StatusDealClosed, "")
	if err != nil {
		t.Fatal(err)
	}
	if closed.SaleDate != today || closed.PlannedSaleDate == nil || *closed.PlannedSaleDate != planned {
		t.Fatalf("closed sale date=%s planned=%v, want %s and the planned %s kept", closed.SaleDate, closed.PlannedSaleDate, today, planned)
	}
	var feedDay string
	if err := repo.pool.QueryRow(ctx, `SELECT feed_day::text FROM public.feed_sale_depletions WHERE tenant_id = $1 AND deal_id = $2`,
		salesTestTenant, deal.DealID).Scan(&feedDay); err != nil {
		t.Fatal(err)
	}
	if feedDay != today {
		t.Fatalf("the feed depletion's day = %s, want the close date %s", feedDay, today)
	}
	// A re-close is a no-op; reopening and closing again keeps the ORIGINAL planned date.
	again, err := repo.SetDealStatus(ctx, salesTestTenant, deal.DealID, domain.StatusDealClosed, "")
	if err != nil || again.SaleDate != today || *again.PlannedSaleDate != planned {
		t.Fatalf("re-close = %+v, %v", again, err)
	}
	if _, err := repo.SetDealStatus(ctx, salesTestTenant, deal.DealID, domain.StatusInDiscussion, ""); err != nil {
		t.Fatal(err)
	}
	reclosed, err := repo.SetDealStatus(ctx, salesTestTenant, deal.DealID, domain.StatusDealClosed, "")
	if err != nil || reclosed.PlannedSaleDate == nil || *reclosed.PlannedSaleDate != planned {
		t.Fatalf("a second close must keep the first planned date, got %+v, %v", reclosed, err)
	}

	// A sale recorded AS closed keeps the date it was recorded with and has no planned date.
	yesterday := biztime.BusinessDayStart(time.Now()).AddDate(0, 0, -1).Format("2006-01-02")
	direct := write
	direct.SaleDate, direct.Status = yesterday, domain.StatusDealClosed
	recorded, err := repo.CreateDeal(ctx, salesTestTenant, direct, "", "closed-at-record-key")
	if err != nil {
		t.Fatal(err)
	}
	if recorded.SaleDate != yesterday || recorded.PlannedSaleDate != nil {
		t.Fatalf("a sale recorded closed = date %s planned %v, want %s and none", recorded.SaleDate, recorded.PlannedSaleDate, yesterday)
	}
}

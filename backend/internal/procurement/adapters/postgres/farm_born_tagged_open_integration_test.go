package postgres

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// Farm born treats an OPEN sale exactly as Load wise does (maintainer decision 2026-09-25): an
// animal TAGGED to a deal that is not 'Deal Closed' has left the herd but is not a sale. It must
// not count as sold (it used to, with no buyer and no value), it is its own "Tagged, sale not
// closed" figure, and it is not in the sold ledger. Closing the deal moves it to Sold, with the
// buyer and the per-line share, on the deal's sale date.
func TestFarmBornAnimalTaggedToAnOpenDealIsNotSoldUntilTheDealCloses(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	fx := seedFarmBornFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	filter := domain.FarmBornFilter{From: "2026-08-18", To: "2026-09-18"}

	read := func() (map[string]domain.FarmBornAnimalFact, domain.FarmBornSales) {
		t.Helper()
		facts, err := repo.FarmBornAnimals(ctx, testTenant, filter)
		if err != nil {
			t.Fatal(err)
		}
		return farmBornByName(fx, facts), domain.BuildFarmBornSales(facts, filter, 100, 0)
	}
	_, before := read()

	// A farm-born kid exited as sold by TAGGING on 10/09 (inside the window) to an Advance Paid
	// deal dated 12/09.
	var goatID, dealID string
	if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, species, breed, sex, management_stage, lifecycle_status, exit_reason, origin_type, custodian_party_id, park_id, shed_id, exited_at)
SELECT tenant_id, 'goat', 'Sirohi', 'male', 'F2-Male', 'sold', 'sold', 'birth', custodian_party_id, park_id, shed_id, '2026-09-10T04:00:00Z'
FROM goats WHERE tenant_id = $1 AND goat_id = $2::uuid
RETURNING goat_id::text`, testTenant, fx.facts["alive-cbe-p3-a"]).Scan(&goatID); err != nil {
		t.Fatal(err)
	}
	fx.facts["tagged-open-new"] = goatID
	if err := pool.QueryRow(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1, '2026-09-12', 'CBE', 'Open Deal Buyer', 'Goat', 'Sirohi', 1, 30000, 'Advance Paid')
RETURNING id::text`, testTenant).Scan(&dealID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_sale_allocations (tenant_id, goat_id, sales_deal_id, status, idempotency_key, park_id, shed_id, partition_label)
VALUES ($1, $2::uuid, $3::uuid, 'tagged', 'fb-tagged-open-new', $4::uuid, $5::uuid, 'Part 3')`,
		testTenant, goatID, dealID, fx.cbePark, fx.cbeShed); err != nil {
		t.Fatal(err)
	}

	open, openPage := read()
	if got := open["tagged-open-new"]; got.Bucket != domain.FarmBornTaggedOpen || got.SaleValue != nil || got.BuyerName != "" || got.SaleDate != "" || got.DealID != "" {
		t.Fatalf("an animal tagged to an open deal = %+v, want tagged_open with no sale facts", got)
	}
	if openPage.Summary.Sold != before.Summary.Sold || openPage.TotalSold != before.TotalSold ||
		math.Abs(openPage.Summary.Revenue-before.Summary.Revenue) > 0.01 {
		t.Fatalf("an open deal changed Farm born's sold: %+v -> %+v", before.Summary, openPage.Summary)
	}
	if openPage.Summary.TaggedNotClosed != before.Summary.TaggedNotClosed+1 {
		t.Fatalf("tagged, sale not closed = %d, want %d", openPage.Summary.TaggedNotClosed, before.Summary.TaggedNotClosed+1)
	}
	if openPage.Summary.OnFarm != before.Summary.OnFarm {
		t.Fatalf("a tagged animal is not on the farm: %d -> %d", before.Summary.OnFarm, openPage.Summary.OnFarm)
	}
	for _, row := range openPage.Sold {
		if row.GoatID == goatID {
			t.Fatal("an animal on an open deal must not be listed in the sold ledger")
		}
	}
	// Every breakdown agrees with the headline, bucket for bucket.
	for name, rows := range map[string][]domain.FarmBornBucket{"breed": openPage.ByBreed, "sex": openPage.BySex, "stage": openPage.ByStage, "pen": openPage.ByPen} {
		tagged := 0
		for _, b := range rows {
			tagged += b.TaggedNotClosed
		}
		if tagged != openPage.Summary.TaggedNotClosed {
			t.Fatalf("by %s tagged-not-closed sums to %d, headline %d", name, tagged, openPage.Summary.TaggedNotClosed)
		}
	}

	if _, err := pool.Exec(ctx, `UPDATE sales_deals SET status = 'Deal Closed' WHERE id = $1::uuid`, dealID); err != nil {
		t.Fatal(err)
	}
	closed, closedPage := read()
	got := closed["tagged-open-new"]
	if got.Bucket != domain.FarmBornSold || got.SaleValue == nil || math.Abs(*got.SaleValue-30000) > 0.01 ||
		got.BuyerName != "Open Deal Buyer" || got.SaleDate != "2026-09-12" || got.DealID != dealID {
		t.Fatalf("closing the deal must make it a sale with the deal's facts: %+v", got)
	}
	if closedPage.Summary.Sold != before.Summary.Sold+1 || closedPage.Summary.TaggedNotClosed != before.Summary.TaggedNotClosed {
		t.Fatalf("after close: sold %d (want %d), tagged-not-closed %d (want %d)",
			closedPage.Summary.Sold, before.Summary.Sold+1, closedPage.Summary.TaggedNotClosed, before.Summary.TaggedNotClosed)
	}
}

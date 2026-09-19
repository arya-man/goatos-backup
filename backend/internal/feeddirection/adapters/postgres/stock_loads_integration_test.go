package postgres

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Purchased vs consumed, load by load (maintainer request 2026-09-19).
//
// The adversarial fixture: ONE feed at ONE farm with THREE loads -- two reached (so FIFO has an
// order to honour) and one still in transit -- fed over FOUR locked days so that the first load
// FINISHES on a day that also starts the second (a straddling day must count for both), plus a
// SECOND farm holding the same feed with more directed against it than it ever bought (an
// overrun the newest load must carry as negative kg, never clamped), and a load with NO stated
// days (the check must be absent, not zero). Park scope and the farm/feed filters are proved on
// the same data, as is the whole-filter negative count under a page boundary.
func TestStockLoadsFifoOneToManyStatusBucketsParkScopePageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	const park2 = "fd100000-0000-4000-8000-000000003002"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, park2); err != nil {
		t.Fatalf("seed second park: %v", err)
	}
	const label, key = "Mesha Kids Goat Concentrate", "mesha_kids_goat_concentrate"
	purchase := func(park, farm string, batch int64, bought, reachedOn, qty string, days *int, delivery string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status, reached_on, days_of_stock)
VALUES ($1, $2, $3, $4, $5, $6::date, $7::numeric, 40, 1000, 0, $8::date, 'Navaladi', 'Paid',
        $9, CASE WHEN $9 = 'reached' THEN $8::date END, $10)`,
			fdiTenant, park, farm, label, batch, bought, qty, reachedOn, delivery, days); err != nil {
			t.Fatalf("purchase %s#%d: %v", farm, batch, err)
		}
	}
	four, two := 4, 2
	// CBE: load 1 reached first, 100 kg, said 4 days; load 2 reached the same day (bought later),
	// 100 kg, said 2 days; load 3 bought 20 Aug, still on the road, 50 kg, nothing said.
	purchase(fdiPark, "CBE", 1, "2026-08-01", "2026-08-10", "100.000", &four, "reached")
	purchase(fdiPark, "CBE", 2, "2026-08-05", "2026-08-10", "100.000", &two, "reached")
	purchase(fdiPark, "CBE", 3, "2026-08-20", "2026-08-20", "50.000", nil, "purchased")
	// CPT: one 50 kg load, said 3 days, then 60 kg directed -- 10 kg the ledger never bought.
	three := 3
	purchase(park2, "CPT", 1, "2026-08-09", "2026-08-10", "50.000", &three, "reached")

	feed := func(park, parkLabel, shed, day, qty string) {
		t.Helper()
		at := time.Date(2026, 8, 11, 9, 0, 0, 0, biztime.DefaultLocation())
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: park, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: at, Fingerprint: "loads" + park + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + park + ":" + day + ":loads",
			GeneratedBy:    "test",
			Cells: []domain.StoredCell{{
				ParkID: park, ParkLabel: parkLabel, ShedID: shed, ShedLabel: "Castro",
				PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
				RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
				HeadCount: 10, Workflow: domain.WorkflowNormal,
				FeedItemLabel: label, FeedItemKey: key, QuantityKg: kg(qty), SessionTotalKg: qty,
			}},
		}); err != nil {
			t.Fatalf("persist %s %s: %v", parkLabel, day, err)
		}
		if lock, err := repo.LockIssue(ctx, ports.LockIssueCommand{
			TenantID: fdiTenant, ParkID: park, FeedDay: day,
			Workflow: domain.WorkflowNormal, LockedAt: at,
		}); err != nil || lock.Outcome != "locked" {
			t.Fatalf("lock %s %s = (%v, %v)", parkLabel, day, lock.Outcome, err)
		}
	}
	// CBE: 30 kg a day for four days = 120 kg. Load 1 (100 kg) is drawn on all four days and
	// finishes on the 14th; that same day starts load 2 (cum 120 > 100) -- one day, two loads.
	for _, day := range []string{"2026-08-11", "2026-08-12", "2026-08-13", "2026-08-14"} {
		feed(fdiPark, "CBE", fdiShedA, day, "30.000")
	}
	feed(park2, "CPT", fdiShedB, "2026-08-11", "60.000")

	page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{})
	if err != nil {
		t.Fatalf("StockLoads: %v", err)
	}
	if page.Total != 4 || len(page.Rows) != 4 {
		t.Fatalf("want every load (4), got total %d rows %d: %+v", page.Total, len(page.Rows), page.Rows)
	}
	byKey := map[string]domain.StockLoadRow{}
	for _, row := range page.Rows {
		byKey[row.FarmLabel+"#"+itoa(row.BatchNo)] = row
	}
	str := func(v *int64) string {
		if v == nil {
			return "nil"
		}
		return itoa(*v)
	}

	// Load 1: fully consumed over four days, said four -- the check comes out at zero.
	l1 := byKey["CBE#1"]
	if l1.Status != domain.StockLoadFinished || l1.ConsumedKg != "100.0" || l1.LeftKg != "0.0" {
		t.Errorf("load 1 must be finished, 100 consumed, 0 left: %+v", l1)
	}
	if l1.ConsumptionFrom != "2026-08-11" || l1.FinishedOn != "2026-08-14" || l1.DaysConsumed != 4 {
		t.Errorf("load 1 used 11-14 Aug (4 days), finishing on the 14th: %+v", l1)
	}
	if str(l1.DaysSaid) != "4" || str(l1.DaysLeft) != "0" || str(l1.GapDays) != "0" {
		t.Errorf("load 1 said 4, consumed 4, 0 left -> gap 0: said %s left %s gap %s", str(l1.DaysSaid), str(l1.DaysLeft), str(l1.GapDays))
	}
	// Load 2: took the 20 kg remainder on the 14th (the straddling day counts for it too), 80 kg
	// left over a 30 kg/day rate = 2 days; said 2, consumed 1, left 2 -> gap -1, the highlighted case.
	l2 := byKey["CBE#2"]
	if l2.Status != domain.StockLoadInUse || l2.ConsumedKg != "20.0" || l2.LeftKg != "80.0" {
		t.Errorf("load 2 must be in use with the FIFO remainder, 20 consumed / 80 left: %+v", l2)
	}
	if l2.ConsumptionFrom != "2026-08-14" || l2.FinishedOn != "" || l2.DaysConsumed != 1 {
		t.Errorf("load 2 started on the straddling 14th, one day so far, not finished: %+v", l2)
	}
	if l2.AvgDailyKg != "30.0" || str(l2.DaysLeft) != "2" || str(l2.GapDays) != "-1" {
		t.Errorf("load 2 at 30 kg/day has 2 days left; said 2 - 1 - 2 = -1: avg %q left %s gap %s", l2.AvgDailyKg, str(l2.DaysLeft), str(l2.GapDays))
	}
	// Load 3: on the road -- not stock, nothing consumed, nothing said, no check.
	l3 := byKey["CBE#3"]
	if l3.Status != domain.StockLoadInTransit || l3.ConsumedKg != "0.0" || l3.LeftKg != "50.0" || l3.ReachedOn != "" {
		t.Errorf("in-transit load is untouched stock-to-be: %+v", l3)
	}
	if l3.DaysSaid != nil || l3.DaysLeft != nil || l3.GapDays != nil {
		t.Errorf("no figure stated and no rate: every day field absent, got said %s left %s gap %s", str(l3.DaysSaid), str(l3.DaysLeft), str(l3.GapDays))
	}
	// CPT: 60 directed against 50 bought. The newest (only) load carries the -10, and reads overrun.
	cpt := byKey["CPT#1"]
	if cpt.Status != domain.StockLoadOverrun || cpt.ConsumedKg != "60.0" || cpt.LeftKg != "-10.0" {
		t.Errorf("overrun must show negative kg left, never clamped: %+v", cpt)
	}
	if str(cpt.DaysLeft) != "0" || str(cpt.GapDays) != "2" {
		// Said 3, consumed 1, nothing left: gap 2 -- the load lasted less time than said, but that is
		// a POSITIVE gap here because the days-left projection is 0 rather than a shortfall in days;
		// the negative kg is what flags this row.
		t.Errorf("overrun load: left 0 days, gap 3-1-0 = 2: left %s gap %s", str(cpt.DaysLeft), str(cpt.GapDays))
	}
	if page.NegativeGaps != 1 {
		t.Errorf("exactly one load (CBE#2) is short of what was said: got %d", page.NegativeGaps)
	}
	if len(page.FeedItems) != 1 || page.FeedItems[0].Key != key {
		t.Errorf("feed-item facet lists the one feed in the ledger: %+v", page.FeedItems)
	}
	if len(page.Farms) != 2 || page.Farms[0] != "CBE" || page.Farms[1] != "CPT" {
		t.Errorf("farm facet lists both farms, CBE first: %+v", page.Farms)
	}

	t.Run("ParkScopeFarmFilterPageBoundaryKeepWholeFilterCounts", func(t *testing.T) {
		scoped, err := repo.StockLoads(ctx, fdiTenant, []uuid.UUID{uuid.MustParse(park2)}, domain.StockLoadsQuery{})
		if err != nil {
			t.Fatalf("scoped: %v", err)
		}
		if scoped.Total != 1 || len(scoped.Rows) != 1 || scoped.Rows[0].FarmLabel != "CPT" || scoped.NegativeGaps != 0 {
			t.Fatalf("park scope must narrow to CPT's one load with its own counts: %+v", scoped)
		}
		// The park filter must not disturb FIFO inside the other farm either.
		if scoped.Rows[0].LeftKg != "-10.0" {
			t.Fatalf("CPT overrun unchanged under scope: %+v", scoped.Rows[0])
		}
		farm, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{FarmLabel: "CBE", Limit: 1, Offset: 0})
		if err != nil {
			t.Fatalf("farm page: %v", err)
		}
		if farm.Total != 3 || len(farm.Rows) != 1 || farm.NegativeGaps != 1 {
			t.Fatalf("page of one must still carry the whole-filter total (3) and negatives (1): %+v", farm)
		}
		// Newest purchase first: the in-transit 20 Aug load.
		if farm.Rows[0].BatchNo != 3 {
			t.Fatalf("ordered newest bought first: %+v", farm.Rows[0])
		}
		if _, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{Limit: 500}); err == nil {
			t.Fatal("an out-of-range page must be refused, not clamped")
		}
		none, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{FeedItemKey: "no_such_feed"})
		if err != nil {
			t.Fatalf("unknown feed: %v", err)
		}
		if none.Total != 0 || len(none.Rows) != 0 {
			t.Fatalf("unknown feed filter matches nothing: %+v", none)
		}
	})
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

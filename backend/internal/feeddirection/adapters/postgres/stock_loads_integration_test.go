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

// THE HANDOFF: when does a NEWLY PURCHASED load of a feed the farm is ALREADY feeding start being
// drawn down? (maintainer question, 2026-09-21).
//
// The answer this pins: NOT on the day it arrives. A load starts depleting on the first locked feed
// day whose RUNNING TOTAL for that (farm, feed) passes the kilograms of every load that arrived
// before it. Arrival only makes a load ELIGIBLE; the queue decides when it is reached.
//
// Six families, one per edge of that rule, each its own feed at one farm so their running totals
// cannot touch, each on its own stretch of locked days:
//
//	overlap  a load arriving while the previous one is still being fed -- the headline case
//	exact    a load arriving on the day the previous one lands exactly on zero (strict boundary)
//	gap      a load arriving AFTER the farm had already fed past everything it had bought
//	weighed  a received weight under the bought weight -- the queue must move at the WEIGHED kg
//	transit  a load still on the road -- it must not hold a place in the queue in front of a
//	         load that has actually arrived
//	backdate a load recorded later but arriving EARLIER -- arrival day orders the queue, not the
//	         order rows were entered or numbered
func TestStockLoadsHandoffWhenANewLoadOfAFeedAlreadyInUseStartsDepleting(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	// load records one purchase. reachedWeight is the weighbridge figure when it differs from the
	// bought quantity; "" leaves the bought quantity standing. reached "" means still on the road.
	load := func(feedKey string, batch int64, bought, reached, qty, reachedWeight string, days *int) {
		t.Helper()
		delivery, depletes := "reached", reached
		var reachedOn, weight any
		if reached == "" {
			delivery, depletes, reachedOn = "purchased", bought, nil
		} else {
			reachedOn = reached
		}
		if reachedWeight != "" {
			weight = reachedWeight
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status, reached_on, reached_weight_kg, days_of_stock)
VALUES ($1, $2, 'CBE', $3, $4, $5::date, $6::numeric, 40, 1000, 0, $7::date, 'Navaladi', 'Paid',
        $8, $9::date, $10::numeric, $11)`,
			fdiTenant, fdiPark, feedKey, batch, bought, qty, depletes, delivery, reachedOn, weight, days); err != nil {
			t.Fatalf("purchase %s#%d: %v", feedKey, batch, err)
		}
	}
	// feed locks one day's sheet directing qty of feedKey. One cell, one day, one feed: the
	// families never share a day, so each scenario's running total is its own.
	feed := func(feedKey, day, qty string) {
		t.Helper()
		at := time.Date(2026, 9, 1, 9, 0, 0, 0, biztime.DefaultLocation())
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: at, Fingerprint: "handoff" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":handoff",
			GeneratedBy:    "test",
			Cells: []domain.StoredCell{{
				ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
				PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
				RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
				HeadCount: 10, Workflow: domain.WorkflowNormal,
				FeedItemLabel: feedKey, FeedItemKey: feedKey, QuantityKg: kg(qty), SessionTotalKg: qty,
			}},
		}); err != nil {
			t.Fatalf("persist %s %s: %v", feedKey, day, err)
		}
		if lock, err := repo.LockIssue(ctx, ports.LockIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day,
			Workflow: domain.WorkflowNormal, LockedAt: at,
		}); err != nil || lock.Outcome != "locked" {
			t.Fatalf("lock %s = (%v, %v)", day, lock.Outcome, err)
		}
	}
	feedDays := func(feedKey, qty string, days ...string) {
		t.Helper()
		for _, day := range days {
			feed(feedKey, day, qty)
		}
	}

	four, three := 4, 3

	// overlap -- load 2 ARRIVES on 3 Sep while load 1 (100 kg) still has 50 kg in it. 25 kg a day
	// from the 1st: the running total passes 100 on the 5th, so THAT is load 2's first day.
	load("overlap", 1, "2026-09-01", "2026-09-01", "100.000", "", &four)
	load("overlap", 2, "2026-09-02", "2026-09-03", "100.000", "", &three)
	feedDays("overlap", "25.000", "2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04", "2026-09-05", "2026-09-06")

	// exact -- 30 kg a day against a 60 kg load: the total lands ON 60 on the 2nd, and load 2
	// arrives on the 3rd. The boundary is STRICT, so the 2nd belongs to load 1 alone.
	load("exact", 1, "2026-10-01", "2026-10-01", "60.000", "", nil)
	load("exact", 2, "2026-10-02", "2026-10-03", "60.000", "", nil)
	feedDays("exact", "30.000", "2026-10-01", "2026-10-02", "2026-10-03")

	// gap -- 25 kg a day for four days against a single 50 kg load, and the next load does not
	// arrive until the 10th. The farm fed 50 kg it had not bought.
	load("gap", 1, "2026-11-01", "2026-11-01", "50.000", "", &three)
	load("gap", 2, "2026-11-09", "2026-11-10", "100.000", "", nil)
	feedDays("gap", "25.000", "2026-11-01", "2026-11-02", "2026-11-03", "2026-11-04")

	// weighed -- bought 100, weighbridge said 80. The queue must hand over at 80, not at 100.
	load("weighed", 1, "2026-12-01", "2026-12-01", "100.000", "80.000", nil)
	load("weighed", 2, "2026-12-02", "2026-12-02", "100.000", "", nil)
	feedDays("weighed", "40.000", "2026-12-01", "2026-12-02", "2026-12-03")

	// transit -- load 1 is still on the road. It must not stand in front of load 2, which is here.
	load("transit", 1, "2027-01-01", "", "100.000", "", nil)
	load("transit", 2, "2027-01-02", "2027-01-02", "100.000", "", nil)
	feedDays("transit", "30.000", "2027-01-02", "2027-01-03")

	// backdate -- the two orderings DISAGREE on purpose: batch 2 was BOUGHT first (1 Feb) but
	// arrives LAST (5 Feb), while batch 1 was bought later (4 Feb) and arrived FIRST (1 Feb).
	// Arrival day orders the queue; ordering by the purchase day would put batch 2 in front.
	load("backdate", 2, "2027-02-01", "2027-02-05", "100.000", "", nil)
	load("backdate", 1, "2027-02-04", "2027-02-01", "100.000", "", nil)
	feedDays("backdate", "50.000", "2027-02-01", "2027-02-02", "2027-02-03", "2027-02-04")

	page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{Limit: 100})
	if err != nil {
		t.Fatalf("StockLoads: %v", err)
	}
	rows := map[string]domain.StockLoadRow{}
	for _, row := range page.Rows {
		rows[row.FeedItemKey+"#"+itoa(row.BatchNo)] = row
	}
	if len(rows) != 12 {
		t.Fatalf("want all twelve loads, got %d: %+v", len(rows), page.Rows)
	}
	num := func(v *int64) string {
		if v == nil {
			return "nil"
		}
		return itoa(*v)
	}
	check := func(name string, want, got domain.StockLoadRow) {
		t.Helper()
		if got.Status != want.Status || got.PurchasedKg != want.PurchasedKg ||
			got.ConsumedKg != want.ConsumedKg || got.LeftKg != want.LeftKg ||
			got.ConsumptionFrom != want.ConsumptionFrom || got.FinishedOn != want.FinishedOn ||
			got.DaysConsumed != want.DaysConsumed || num(got.DaysLeft) != num(want.DaysLeft) ||
			num(got.GapDays) != num(want.GapDays) {
			t.Errorf("%s\n want status=%s bought=%s used=%s left=%s from=%q finished=%q daysUsed=%d daysLeft=%s gap=%s\n  got status=%s bought=%s used=%s left=%s from=%q finished=%q daysUsed=%d daysLeft=%s gap=%s",
				name,
				want.Status, want.PurchasedKg, want.ConsumedKg, want.LeftKg, want.ConsumptionFrom, want.FinishedOn, want.DaysConsumed, num(want.DaysLeft), num(want.GapDays),
				got.Status, got.PurchasedKg, got.ConsumedKg, got.LeftKg, got.ConsumptionFrom, got.FinishedOn, got.DaysConsumed, num(got.DaysLeft), num(got.GapDays))
		}
	}
	n := func(v int64) *int64 { return &v }

	// THE HEADLINE. Load 2 arrived on the 3rd and its first drawn day is the 5th -- the day the
	// running total (125 kg) first passes load 1's 100 kg. The two days in between fed load 1,
	// which finishes on the 4th. Nothing is consumed twice and no day is lost between them.
	check("overlap load 1 (fed 1-4 Sep, finishes as the total reaches 100)",
		domain.StockLoadRow{Status: domain.StockLoadFinished, PurchasedKg: "100.0", ConsumedKg: "100.0", LeftKg: "0.0",
			ConsumptionFrom: "2026-09-01", FinishedOn: "2026-09-04", DaysConsumed: 4, DaysLeft: n(0), GapDays: n(0)},
		rows["overlap#1"])
	check("overlap load 2 (arrived 3 Sep, first drawn on the 5th)",
		domain.StockLoadRow{Status: domain.StockLoadInUse, PurchasedKg: "100.0", ConsumedKg: "50.0", LeftKg: "50.0",
			ConsumptionFrom: "2026-09-05", FinishedOn: "", DaysConsumed: 2, DaysLeft: n(2), GapDays: n(-1)},
		rows["overlap#2"])

	// Strict boundary: the day the total lands EXACTLY on load 1's last kilogram is load 1's day
	// only. A `>=` here would hand that day to both loads and double-count it.
	check("exact load 1 (lands on zero on the 2nd, which is its own day)",
		domain.StockLoadRow{Status: domain.StockLoadFinished, PurchasedKg: "60.0", ConsumedKg: "60.0", LeftKg: "0.0",
			ConsumptionFrom: "2026-10-01", FinishedOn: "2026-10-02", DaysConsumed: 2, DaysLeft: n(0), GapDays: nil},
		rows["exact#1"])
	check("exact load 2 (starts clean on the 3rd)",
		domain.StockLoadRow{Status: domain.StockLoadInUse, PurchasedKg: "60.0", ConsumedKg: "30.0", LeftKg: "30.0",
			ConsumptionFrom: "2026-10-03", FinishedOn: "", DaysConsumed: 1, DaysLeft: n(1), GapDays: nil},
		rows["exact#2"])

	// The weighbridge figure, not the invoice, moves the queue: load 1 hands over after 80 kg, so
	// the third day's 40 kg belongs to load 2.
	check("weighed load 1 (80 received against 100 bought)",
		domain.StockLoadRow{Status: domain.StockLoadFinished, PurchasedKg: "80.0", ConsumedKg: "80.0", LeftKg: "0.0",
			ConsumptionFrom: "2026-12-01", FinishedOn: "2026-12-02", DaysConsumed: 2, DaysLeft: n(0), GapDays: nil},
		rows["weighed#1"])
	check("weighed load 2 (takes over at 80 kg, not 100)",
		domain.StockLoadRow{Status: domain.StockLoadInUse, PurchasedKg: "100.0", ConsumedKg: "40.0", LeftKg: "60.0",
			ConsumptionFrom: "2026-12-03", FinishedOn: "", DaysConsumed: 1, DaysLeft: n(1), GapDays: nil},
		rows["weighed#2"])

	// A load on the road is stock-to-be: it holds no place in the queue, so the load that has
	// actually arrived is drawn on from its own first day.
	check("transit load 1 (on the road, blocks nothing)",
		domain.StockLoadRow{Status: domain.StockLoadInTransit, PurchasedKg: "100.0", ConsumedKg: "0.0", LeftKg: "100.0",
			ConsumptionFrom: "", FinishedOn: "", DaysConsumed: 0, DaysLeft: nil, GapDays: nil},
		rows["transit#1"])
	check("transit load 2 (arrived, so it is first in the queue)",
		domain.StockLoadRow{Status: domain.StockLoadInUse, PurchasedKg: "100.0", ConsumedKg: "60.0", LeftKg: "40.0",
			ConsumptionFrom: "2027-01-02", FinishedOn: "", DaysConsumed: 2, DaysLeft: n(1), GapDays: nil},
		rows["transit#2"])

	// Recorded second, arrived first: batch 1 leads the queue. Batch order and entry order are
	// both irrelevant -- only the arrival day counts.
	check("backdate load 1 (bought later but arrived first, so it leads the queue)",
		domain.StockLoadRow{Status: domain.StockLoadFinished, PurchasedKg: "100.0", ConsumedKg: "100.0", LeftKg: "0.0",
			ConsumptionFrom: "2027-02-01", FinishedOn: "2027-02-02", DaysConsumed: 2, DaysLeft: n(0), GapDays: nil},
		rows["backdate#1"])
	// Batch 2 arrived on the 5th, after every locked day, so it hits the same limitation as the gap
	// family above: charged the 100 kg remainder it could not have served, with no day to show for
	// it. Pinned deliberately -- if the attribution is ever corrected, this is one of the two rows
	// that must move, and it should move to not_started / 100 kg left.
	check("backdate load 2 (arrived after every locked day -- known limitation)",
		domain.StockLoadRow{Status: domain.StockLoadFinished, PurchasedKg: "100.0", ConsumedKg: "100.0", LeftKg: "0.0",
			ConsumptionFrom: "", FinishedOn: "", DaysConsumed: 0, DaysLeft: n(0), GapDays: nil},
		rows["backdate#2"])

	// THE GAP, and the KNOWN LIMITATION it exposes -- pinned here so it cannot change unnoticed.
	//
	// The farm fed 100 kg against a 50 kg load and the next load did not land for another six days,
	// so 50 kg came from somewhere the ledger never recorded. The kg side charges that 50 to the
	// next load to arrive (the newest reached load carries the whole remainder, by design, so an
	// overrun cannot be clamped away); the DAY side will not, because no locked day falls on or
	// after that load's arrival. The row therefore reads "in use, 50 kg used" beside "used 0 days,
	// never started", and its 50 kg left understates the 100 kg physically in the store.
	//
	// This is the family-level truth the stock cards already show (150 bought - 100 directed = 50),
	// so the two surfaces agree; what is wrong is WHICH LOAD carries the deficit. Charging it to a
	// load that had not arrived is the defect, and the honest owner is the load that was newest on
	// the day the feed actually went out. Changing that is a maintainer decision about what the
	// number means, not a bug fix -- see the handoff note in docs/decisions/feed-purchased-vs-consumed.md.
	check("gap load 1 (ran out on the 2nd, said three days)",
		domain.StockLoadRow{Status: domain.StockLoadFinished, PurchasedKg: "50.0", ConsumedKg: "50.0", LeftKg: "0.0",
			ConsumptionFrom: "2026-11-01", FinishedOn: "2026-11-02", DaysConsumed: 2, DaysLeft: n(0), GapDays: n(1)},
		rows["gap#1"])
	check("gap load 2 (arrived 10 Nov, after every locked day)",
		domain.StockLoadRow{Status: domain.StockLoadInUse, PurchasedKg: "100.0", ConsumedKg: "50.0", LeftKg: "50.0",
			ConsumptionFrom: "", FinishedOn: "", DaysConsumed: 0, DaysLeft: n(2), GapDays: nil},
		rows["gap#2"])
}

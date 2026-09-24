package postgres

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	procurementpg "github.com/vgoats/goatos/backend/internal/procurement/adapters/postgres"
	procdomain "github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// Purchased vs consumed, load by load (maintainer request 2026-09-19).
//
// The adversarial fixture: ONE feed at ONE farm with THREE loads -- two reached (so FIFO has an
// order to honour) and one still in transit -- fed over FOUR locked days so that the first load
// FINISHES on a day that also starts the second (a straddling day must count for both), plus a
// SECOND farm holding the same feed with more directed against it than it ever bought (the
// newest load must carry that as negative kg, never clamped, and still read as IN USE because it
// is the load the store is drawing on), and a load with NO stated days (the check must be absent,
// not zero). The FINISHED load proves the other half of the maintainer's 2026-09-22 instruction:
// it is computed, it moves the FIFO queue, and it is NOT SERVED -- the table answers what is in
// the store now. Park scope and the farm/feed filters are proved on the same data, as is the
// whole-filter count under a page boundary.
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

	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{})
	if err != nil {
		t.Fatalf("StockLoads: %v", err)
	}
	// Four loads exist; the FINISHED one is history and is not served, so three rows and a
	// whole-filter total of three.
	if page.Total != 3 || len(page.Rows) != 3 {
		t.Fatalf("want the three loads still in the store, got total %d rows %d: %+v", page.Total, len(page.Rows), page.Rows)
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

	// Load 1: fully consumed over four days. It is FINISHED, so it is not on the table at all --
	// and its 100 kg still moved the queue, which is what load 2 below proves.
	if _, listed := byKey["CBE#1"]; listed {
		t.Errorf("a finished load is history and must not be listed: %+v", byKey["CBE#1"])
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
	// CPT: 60 directed against 50 bought. The newest (only) load carries the -10 and stays IN USE:
	// it is the load the store is drawing on, and the negative kg is the finding.
	cpt := byKey["CPT#1"]
	if cpt.Status != domain.StockLoadInUse || cpt.ConsumedKg != "60.0" || cpt.LeftKg != "-10.0" {
		t.Errorf("feeding past the ledger stays in use with negative kg left, never clamped: %+v", cpt)
	}
	if str(cpt.DaysLeft) != "0" || str(cpt.GapDays) != "2" {
		// Said 3, consumed 1, nothing left: gap 2 -- the load lasted less time than said, but that is
		// a POSITIVE gap here because the days-left projection is 0 rather than a shortfall in days;
		// the negative kg is what flags this row.
		t.Errorf("overrun load: left 0 days, gap 3-1-0 = 2: left %s gap %s", str(cpt.DaysLeft), str(cpt.GapDays))
	}
	if len(page.FeedItems) != 1 || page.FeedItems[0].Key != key {
		t.Errorf("feed-item facet lists the one feed in the ledger: %+v", page.FeedItems)
	}
	if len(page.Farms) != 2 || page.Farms[0] != "CBE" || page.Farms[1] != "CPT" {
		t.Errorf("farm facet lists both farms, CBE first: %+v", page.Farms)
	}

	t.Run("ParkScopeFarmFilterPageBoundaryKeepWholeFilterCounts", func(t *testing.T) {
		activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
		scoped, err := repo.StockLoads(ctx, fdiTenant, []uuid.UUID{uuid.MustParse(park2)}, domain.StockLoadsQuery{})
		if err != nil {
			t.Fatalf("scoped: %v", err)
		}
		if scoped.Total != 1 || len(scoped.Rows) != 1 || scoped.Rows[0].FarmLabel != "CPT" {
			t.Fatalf("park scope must narrow to CPT's one load with its own counts: %+v", scoped)
		}
		// The park filter must not disturb FIFO inside the other farm either.
		if scoped.Rows[0].LeftKg != "-10.0" {
			t.Fatalf("CPT overrun unchanged under scope: %+v", scoped.Rows[0])
		}
		activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
		farm, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{FarmLabel: "CBE", Limit: 1, Offset: 0})
		if err != nil {
			t.Fatalf("farm page: %v", err)
		}
		if farm.Total != 2 || len(farm.Rows) != 1 {
			t.Fatalf("page of one must still carry the whole-filter total (2, the finished load aside): %+v", farm)
		}
		// Newest purchase first: the in-transit 20 Aug load.
		if farm.Rows[0].BatchNo != 3 {
			t.Fatalf("ordered newest bought first: %+v", farm.Rows[0])
		}
		activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
		if _, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{Limit: 500}); err == nil {
			t.Fatal("an out-of-range page must be refused, not clamped")
		}
		activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
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

	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{Limit: 100})
	if err != nil {
		t.Fatalf("StockLoads: %v", err)
	}
	rows := map[string]domain.StockLoadRow{}
	for _, row := range page.Rows {
		rows[row.FeedItemKey+"#"+itoa(row.BatchNo)] = row
	}
	// Twelve loads, three of them FINISHED and therefore not served: the queue still ran through
	// them, which is exactly what their successors' figures below prove.
	if len(rows) != 9 {
		t.Fatalf("want the nine loads still in the store, got %d: %+v", len(rows), page.Rows)
	}
	for _, gone := range []string{"overlap#1", "exact#1", "weighed#1"} {
		if _, listed := rows[gone]; listed {
			t.Errorf("%s is finished and must not be listed: %+v", gone, rows[gone])
		}
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
	check("overlap load 2 (arrived 3 Sep, first drawn on the 5th)",
		domain.StockLoadRow{Status: domain.StockLoadInUse, PurchasedKg: "100.0", ConsumedKg: "50.0", LeftKg: "50.0",
			ConsumptionFrom: "2026-09-05", FinishedOn: "", DaysConsumed: 2, DaysLeft: n(2), GapDays: n(-1)},
		rows["overlap#2"])

	// Strict boundary: the day the total lands EXACTLY on load 1's last kilogram is load 1's day
	// only. A `>=` here would hand that day to both loads and double-count it.
	check("exact load 2 (starts clean on the 3rd)",
		domain.StockLoadRow{Status: domain.StockLoadInUse, PurchasedKg: "60.0", ConsumedKg: "30.0", LeftKg: "30.0",
			ConsumptionFrom: "2026-10-03", FinishedOn: "", DaysConsumed: 1, DaysLeft: n(1), GapDays: nil},
		rows["exact#2"])

	// The weighbridge figure, not the invoice, moves the queue: load 1 hands over after 80 kg, so
	// the third day's 40 kg belongs to load 2.
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
	check("backdate load 1 (bought later but arrived first, so it owns the pre-arrival overrun)",
		domain.StockLoadRow{Status: domain.StockLoadInUse, PurchasedKg: "100.0", ConsumedKg: "200.0", LeftKg: "-100.0",
			ConsumptionFrom: "2027-02-01", FinishedOn: "2027-02-02", DaysConsumed: 4, DaysLeft: n(0), GapDays: nil},
		rows["backdate#1"])
	// Batch 2 arrived on the 5th, after every locked day. It cannot have served any of those days, so
	// it stays untouched: the overrun, if any, belongs to the load that was available when the feed
	// went out.
	// ...and its DAYS LEFT is 0, not 2. The store is already 100 kg in deficit, so this load's
	// 100 kg carries the farm back to zero and no further: days left is the RUNWAY up to and
	// including the load, which is the figure the stock card quotes for the whole feed.
	check("backdate load 2 (arrived after every locked day, so it is untouched)",
		domain.StockLoadRow{Status: domain.StockLoadNotStarted, PurchasedKg: "100.0", ConsumedKg: "0.0", LeftKg: "100.0",
			ConsumptionFrom: "", FinishedOn: "", DaysConsumed: 0, DaysLeft: n(0), GapDays: nil},
		rows["backdate#2"])

	// THE GAP. The farm fed 100 kg against a 50 kg load, then the next load did not land for another
	// six days. The overrun stays on the load that was available on those locked days; a later load is
	// not charged for feed it could not have served.
	//
	// The tell is the first load's negative kg left. That is the missing-ledger finding this table
	// exists to expose; the future load remains a future load.
	check("gap load 1 (ran out on the 2nd, said three days)",
		domain.StockLoadRow{Status: domain.StockLoadInUse, PurchasedKg: "50.0", ConsumedKg: "100.0", LeftKg: "-50.0",
			ConsumptionFrom: "2026-11-01", FinishedOn: "2026-11-02", DaysConsumed: 4, DaysLeft: n(0), GapDays: n(-1)},
		rows["gap#1"])
	// Days left is 2, not 4: the 50 kg deficit ahead of it eats half of this load before the
	// store is back at zero, and the card for this feed says two days as well.
	check("gap load 2 (arrived 10 Nov, after every locked day)",
		domain.StockLoadRow{Status: domain.StockLoadNotStarted, PurchasedKg: "100.0", ConsumedKg: "0.0", LeftKg: "100.0",
			ConsumptionFrom: "", FinishedOn: "", DaysConsumed: 0, DaysLeft: n(2), GapDays: nil},
		rows["gap#2"])
}

// MILK IS IN (maintainer instruction, 2026-09-22, replacing the 2026-09-21 exclusion). Milk is
// drawn by preparation batches rather than the ration sheet, which is why it was left out; the
// maintainer asked for it back, and the arithmetic needs no special case because externally
// tracked consumption already reaches this read through the same union the stock cards use. The
// milk load here sits at the same farm as a bulk one so the FIFO of the feeds beside it is proved
// undisturbed.
func TestStockLoadsListsMilkBesideTheBulkFeeds(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	insert := func(label string, batch int64, qty string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status, reached_on, days_of_stock)
VALUES ($1, $2, 'CBE', $3, $4, DATE '2026-08-01', $5::numeric, 40, 1000, 0, DATE '2026-08-01',
        'Navaladi', 'Paid', 'reached', DATE '2026-08-01', 5)`,
			fdiTenant, fdiPark, label, batch, qty); err != nil {
			t.Fatalf("purchase %s: %v", label, err)
		}
	}
	insert("UHT Milk", 1, "300.000")
	insert("Mesha Kids Goat Concentrate", 2, "100.000")

	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{})
	if err != nil {
		t.Fatalf("StockLoads: %v", err)
	}
	if page.Total != 2 || len(page.Rows) != 2 {
		t.Fatalf("both loads are in the store and both are listed: total %d rows %+v", page.Total, page.Rows)
	}
	if len(page.FeedItems) != 2 {
		t.Errorf("the feed filter offers both feeds: %+v", page.FeedItems)
	}
	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	milk, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{FeedItemKey: "uht_milk"})
	if err != nil {
		t.Fatalf("milk filter: %v", err)
	}
	if milk.Total != 1 || len(milk.Rows) != 1 || milk.Rows[0].FeedItemKey != "uht_milk" {
		t.Fatalf("asking for milk by key returns the milk load: %+v", milk)
	}
	// No preparation batch has drawn on it, so there is no rate to divide by: days left is ABSENT,
	// never zero, exactly as it is for an untouched bulk load.
	if milk.Rows[0].DaysLeft != nil || milk.Rows[0].AvgDailyKg != "" {
		t.Errorf("an undrawn milk load has no rate and no days left: %+v", milk.Rows[0])
	}
}

// THE TABLE AND THE CARD ABOVE IT MUST AGREE ON THE RUNWAY (maintainer instruction 2026-09-22:
// "days are not matching with above, it should match").
//
// The card answers a FEED: everything in the store over that feed's daily rate. The table answered
// a LOAD: that load's own kg over the same rate -- so a feed with one load read one day more than
// its card, and a feed with two read neither. Days left on a load is now the RUNWAY to the end of
// that load, which makes the NEWEST load of a feed the card's own figure by construction.
//
// The fixture is the case that made the two disagree on the live farm: a RETIRED split concentrate
// with leftover stock folded into its successor (domain.StockFamilyMerge). The retired feed's loads
// are not listed -- the farm does not buy it any more -- while its kg still count toward the
// runway, which is precisely what the card does with them.
func TestStockLoadsDaysLeftEqualsTheStockCardForTheSameFeed(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	const (
		retiredLabel = "Mesha Kids Goat Concentrate"
		retiredKey   = "mesha_kids_goat_concentrate"
		familyLabel  = "Mesha Kids Concentrate"
		familyKey    = "mesha_kids_concentrate"
	)
	catalog := func(label, status string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, status) VALUES ($1::uuid, $2, $3)
ON CONFLICT (tenant_id, feed_item_key) DO UPDATE SET status = EXCLUDED.status`,
			fdiTenant, label, status); err != nil {
			t.Fatalf("catalog %s: %v", label, err)
		}
	}
	catalog(retiredLabel, "retired")
	catalog(familyLabel, "active")

	const park2 = "fd100000-0000-4000-8000-000000003002"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, park2); err != nil {
		t.Fatalf("seed second park: %v", err)
	}
	purchase := func(park, farm, label string, batch int64, day, qty, delivery string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status, reached_on)
VALUES ($1, $2, $3, $4, $5, $6::date, $7::numeric, 40, 1000, 0, $6::date, 'Navaladi', 'Paid',
        $8, CASE WHEN $8 = 'reached' THEN $6::date END)`,
			fdiTenant, park, farm, label, batch, day, qty, delivery); err != nil {
			t.Fatalf("purchase %s#%d: %v", label, batch, err)
		}
	}
	// 100 kg of the retired feed, then two loads of the successor: one older, one newest.
	purchase(fdiPark, "CBE", retiredLabel, 1, "2026-08-01", "100.000", "reached")
	purchase(fdiPark, "CBE", familyLabel, 2, "2026-08-02", "100.000", "reached")
	purchase(fdiPark, "CBE", familyLabel, 3, "2026-08-03", "200.000", "reached")
	// The OTHER park holds the SAME family at a different size and a different rate, so a runway
	// that fanned out across farms -- or a rate that did -- reads as a wrong number here rather
	// than as a missing row. Its in-transit load proves stock-to-be still holds no place.
	purchase(park2, "CPT", familyLabel, 10, "2026-08-02", "300.000", "reached")
	purchase(park2, "CPT", familyLabel, 11, "2026-08-20", "100.000", "purchased")

	feed := func(park, parkLabel, shed, day, label, key, qty string) {
		t.Helper()
		at := time.Date(2026, 8, 20, 9, 0, 0, 0, biztime.DefaultLocation())
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: park, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: at, Fingerprint: "parity" + park + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + park + ":" + day + ":parity",
			GeneratedBy:    "test",
			Cells: []domain.StoredCell{{
				ParkID: park, ParkLabel: parkLabel, ShedID: shed, ShedLabel: "Castro",
				PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
				RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
				HeadCount: 10, Workflow: domain.WorkflowNormal,
				FeedItemLabel: label, FeedItemKey: key, QuantityKg: kg(qty), SessionTotalKg: qty,
			}},
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		if lock, err := repo.LockIssue(ctx, ports.LockIssueCommand{
			TenantID: fdiTenant, ParkID: park, FeedDay: day,
			Workflow: domain.WorkflowNormal, LockedAt: at,
		}); err != nil || lock.Outcome != "locked" {
			t.Fatalf("lock %s %s = (%v, %v)", parkLabel, day, lock.Outcome, err)
		}
	}
	// One day fed off the retired sacks, the next off the successor: SUBSTITUTION, which is why the
	// rate has to be the family's own kg per day rather than a sum of the members' rates. 50 kg a
	// day, so the family rate is 50 and every figure below is hand-checkable -- and the retired feed
	// KEEPS 50 kg, which is the half that decides whether the table counts what the card counts.
	feed(fdiPark, "CBE", fdiShedA, "2026-08-10", retiredLabel, retiredKey, "50.000")
	feed(fdiPark, "CBE", fdiShedA, "2026-08-11", familyLabel, familyKey, "50.000")
	// CPT eats twice as fast off a bigger load: 200 kg left at 100 kg/day is two days there.
	feed(park2, "CPT", fdiShedB, "2026-08-11", familyLabel, familyKey, "100.000")

	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	cards, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{})
	if err != nil {
		t.Fatalf("StockAnalytics: %v", err)
	}
	var card *domain.StockItem
	for i := range cards.Items {
		if cards.Items[i].FarmLabel == "CBE" && cards.Items[i].FeedItemKey == familyKey {
			card = &cards.Items[i]
		}
		if cards.Items[i].FeedItemKey == retiredKey {
			t.Errorf("a retired feed has no card of its own: %+v", cards.Items[i])
		}
	}
	if card == nil {
		t.Fatalf("the family card must exist: %+v", cards.Items)
	}

	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{FarmLabel: "CBE"})
	if err != nil {
		t.Fatalf("StockLoads: %v", err)
	}
	for _, row := range page.Rows {
		if row.FeedItemKey == retiredKey {
			t.Errorf("the farm does not buy the retired feed any more, so its loads are not listed: %+v", row)
		}
	}
	if len(page.Rows) != 2 {
		t.Fatalf("the two successor loads are the table: %+v", page.Rows)
	}
	// Newest purchase first, and that row IS the card: same rate, same days left.
	newest := page.Rows[0]
	if newest.BatchNo != 3 {
		t.Fatalf("newest bought first: %+v", newest)
	}
	if newest.AvgDailyKg != card.AvgDailyKg {
		t.Errorf("the table divides by the card's own rate: table %q card %q", newest.AvgDailyKg, card.AvgDailyKg)
	}
	days := func(v *int64) string {
		if v == nil {
			return "nil"
		}
		return itoa(*v)
	}
	if days(newest.DaysLeft) != days(card.DaysLeft) {
		t.Fatalf("the newest load carries the card's days left: table %s card %s (card %s kg at %s kg/day)",
			days(newest.DaysLeft), days(card.DaysLeft), card.BalanceKg, card.AvgDailyKg)
	}
	// And the load behind it reads the runway up to ITSELF: 50 kg still on the retired sacks ahead
	// of it plus its own 50 kg, at 50 kg/day -- two days, not the one its own kg alone would give.
	older := page.Rows[1]
	if older.BatchNo != 2 || days(older.DaysLeft) != "2" {
		t.Errorf("the earlier load's days left is the runway to the end of it, 100 kg at 50 kg/day: %+v", older)
	}

	// MultipleDimensions: the same family at the other farm, with its own kg and its own rate. A
	// runway or a rate that ranged over the wrong key set shows up here as CBE's number.
	t.Run("MultipleDimensionsOneToManyAcrossFarms", func(t *testing.T) {
		activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
		cpt, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{FarmLabel: "CPT"})
		if err != nil {
			t.Fatalf("CPT: %v", err)
		}
		var cptCard *domain.StockItem
		for i := range cards.Items {
			if cards.Items[i].FarmLabel == "CPT" && cards.Items[i].FeedItemKey == familyKey {
				cptCard = &cards.Items[i]
			}
		}
		if cptCard == nil {
			t.Fatalf("CPT card must exist: %+v", cards.Items)
		}
		var reached *domain.StockLoadRow
		for i := range cpt.Rows {
			if cpt.Rows[i].BatchNo == 10 {
				reached = &cpt.Rows[i]
			}
		}
		if reached == nil {
			t.Fatalf("CPT's reached load must be listed: %+v", cpt.Rows)
		}
		if reached.AvgDailyKg != cptCard.AvgDailyKg || days(reached.DaysLeft) != days(cptCard.DaysLeft) {
			t.Errorf("CPT's load carries CPT's card, not CBE's: load rate %q days %s / card rate %q days %s",
				reached.AvgDailyKg, days(reached.DaysLeft), cptCard.AvgDailyKg, days(cptCard.DaysLeft))
		}
		if days(reached.DaysLeft) == days(newest.DaysLeft) && reached.AvgDailyKg == newest.AvgDailyKg {
			t.Errorf("the two farms were deliberately given different figures: both read %s days at %s kg/day",
				days(reached.DaysLeft), reached.AvgDailyKg)
		}
	})

	// StatusBuckets: every status the table can serve, and the two it must never serve.
	t.Run("StatusBucketsServeNoFinishedAndNoRetiredLoad", func(t *testing.T) {
		activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
		all, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{Limit: 100})
		if err != nil {
			t.Fatalf("all: %v", err)
		}
		seen := map[domain.StockLoadStatus]int{}
		for _, row := range all.Rows {
			seen[row.Status]++
			if row.Status == domain.StockLoadFinished {
				t.Errorf("a finished load is history and is never served: %+v", row)
			}
			if row.FeedItemKey == retiredKey {
				t.Errorf("a retired feed's load is never served: %+v", row)
			}
		}
		for _, want := range []domain.StockLoadStatus{domain.StockLoadInUse, domain.StockLoadNotStarted, domain.StockLoadInTransit} {
			if seen[want] == 0 {
				t.Errorf("the fixture holds a %s load and the table must serve it: %+v", want, all.Rows)
			}
		}
		if int(all.Total) != len(all.Rows) {
			t.Errorf("whole-filter total must count what it serves: total %d rows %d", all.Total, len(all.Rows))
		}
	})

	// ParkScope: a scoped caller sees one park's loads, and the runway is that park's own.
	t.Run("ParkScopeKeepsEachParksOwnRunway", func(t *testing.T) {
		activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
		scoped, err := repo.StockLoads(ctx, fdiTenant, []uuid.UUID{uuid.MustParse(park2)}, domain.StockLoadsQuery{})
		if err != nil {
			t.Fatalf("scoped: %v", err)
		}
		for _, row := range scoped.Rows {
			if row.FarmLabel != "CPT" {
				t.Fatalf("park scope must serve CPT alone: %+v", row)
			}
			if row.BatchNo == 10 && days(row.DaysLeft) != "2" {
				t.Errorf("CPT's runway is unchanged by the scope: %+v", row)
			}
		}
		if len(scoped.FeedItems) != 1 || scoped.FeedItems[0].Key != familyKey {
			t.Errorf("the facet is scoped too, and never offers the retired feed: %+v", scoped.FeedItems)
		}
	})

	// PageBoundary: a page of one still carries the whole-filter count.
	t.Run("PageBoundaryKeepsWholeFilterCounts", func(t *testing.T) {
		activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
		first, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{FarmLabel: "CBE", Limit: 1})
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		if first.Total != 2 || len(first.Rows) != 1 || first.Rows[0].BatchNo != 3 {
			t.Fatalf("one row, the newest, and the whole-filter total of two: %+v", first)
		}
	})
}

// activatePurchasedFeeds gives every feed the fixture has bought an ACTIVE catalog row, as the
// purchase screen itself requires. Stock cards, the loads table and the low-stock push show only
// active catalog feeds (maintainer decision 2026-09-24), so a fixture that buys a feed without
// cataloguing it would otherwise read an empty store. A row the fixture already set (retired, for
// instance) is left as it is.
func activatePurchasedFeeds(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	// The successors of the transitional split-concentrate fold are active feeds the farm buys
	// today, so a fixture that only bought an old split feed still has an active family card.
	_, _, familyLabels := domain.StockFamilyMergeArrays()
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, status)
SELECT DISTINCT $1::uuid, label, 'active'
FROM (
    SELECT p.feed_item_label AS label FROM feed_purchases p WHERE p.tenant_id = $1::uuid
    UNION
    SELECT unnest($2::text[])
) feeds
ON CONFLICT (tenant_id, feed_item_key) DO NOTHING`, fdiTenant, familyLabels); err != nil {
		t.Fatalf("catalog purchased feeds: %v", err)
	}
}

// THE CARD IS RIGHT, AND THE TABLE READS IT (maintainer decision 2026-09-24, "stock cards are
// correct, not the table"). On the live farm a retired split concentrate was fed MORE than was ever
// bought of it: CPT's card read 1,694.4 kg of Mesha Kids Concentrate while the loads table showed
// 1,979.8 kg left on the successor's load, because the table ran its FIFO per ITEM and so never
// charged the retired feed's shortfall to anything. The feed physically came out of the
// successor's sacks, so the family is ONE queue: the retired load is eaten first and its overrun is
// drawn from the successor's load.
//
// Fixture, hand-checkable: 100 kg of the retired feed then 300 kg of the successor; 150 kg fed off
// the retired feed (50 more than it ever held) and 100 kg of the successor. Card = 400 - 250 = 150.
// The per-item FIFO read 300 - 100 = 200 on the successor's load.
func TestStockLoadsSuccessorLoadCarriesTheCardWhenARetiredFeedWasOverfedParkScopePageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	const (
		retiredLabel = "Mesha Kids Goat Concentrate"
		retiredKey   = "mesha_kids_goat_concentrate"
		familyLabel  = "Mesha Kids Concentrate"
		familyKey    = "mesha_kids_concentrate"
	)
	for label, status := range map[string]string{retiredLabel: "retired", familyLabel: "active"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, status) VALUES ($1::uuid, $2, $3)
ON CONFLICT (tenant_id, feed_item_key) DO UPDATE SET status = EXCLUDED.status`,
			fdiTenant, label, status); err != nil {
			t.Fatalf("catalog %s: %v", label, err)
		}
	}
	purchase := func(label string, batch int64, day, qty string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status, reached_on)
VALUES ($1, $2, 'CBE', $3, $4, $5::date, $6::numeric, 40, 1000, 0, $5::date, 'Navaladi', 'Paid',
        'reached', $5::date)`,
			fdiTenant, fdiPark, label, batch, day, qty); err != nil {
			t.Fatalf("purchase %s#%d: %v", label, batch, err)
		}
	}
	purchase(retiredLabel, 1, "2026-08-01", "100.000")
	purchase(familyLabel, 2, "2026-08-02", "300.000")

	feed := func(day, label, key, qty string) {
		t.Helper()
		at := time.Date(2026, 8, 20, 9, 0, 0, 0, biztime.DefaultLocation())
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: at, Fingerprint: "overfed" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":overfed",
			GeneratedBy:    "test",
			Cells: []domain.StoredCell{{
				ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
				PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
				RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
				HeadCount: 10, Workflow: domain.WorkflowNormal,
				FeedItemLabel: label, FeedItemKey: key, QuantityKg: kg(qty), SessionTotalKg: qty,
			}},
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		if lock, err := repo.LockIssue(ctx, ports.LockIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day,
			Workflow: domain.WorkflowNormal, LockedAt: at,
		}); err != nil || lock.Outcome != "locked" {
			t.Fatalf("lock %s = (%v, %v)", day, lock.Outcome, err)
		}
	}
	feed("2026-08-10", retiredLabel, retiredKey, "75.000")
	feed("2026-08-11", retiredLabel, retiredKey, "75.000")
	feed("2026-08-12", familyLabel, familyKey, "100.000")

	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	cards, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{})
	if err != nil {
		t.Fatalf("StockAnalytics: %v", err)
	}
	var card *domain.StockItem
	for i := range cards.Items {
		if cards.Items[i].FeedItemKey == familyKey {
			card = &cards.Items[i]
		}
	}
	if card == nil || card.BalanceKg != "150.0" {
		t.Fatalf("fixture: the card reads 400 bought - 250 fed = 150.0, got %+v", card)
	}
	var forecast *domain.StockForecastItem
	for i := range cards.Forecast {
		if cards.Forecast[i].FeedItemKey == familyKey {
			forecast = &cards.Forecast[i]
		}
	}
	if forecast == nil || forecast.StockKg != card.BalanceKg {
		t.Errorf("the 7-day table's In stock is the card's balance %s: %+v", card.BalanceKg, forecast)
	}

	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{FarmLabel: "CBE"})
	if err != nil {
		t.Fatalf("StockLoads: %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].BatchNo != 2 {
		t.Fatalf("the successor's load is the whole table; the retired feed is not bought any more: %+v", page.Rows)
	}
	successor := page.Rows[0]
	if successor.LeftKg != card.BalanceKg || successor.ConsumedKg != "150.0" {
		t.Errorf("the successor's load carries the retired feed's overrun: left %s consumed %s, want left %s consumed 150.0",
			successor.LeftKg, successor.ConsumedKg, card.BalanceKg)
	}

	// ParkScope: the family queue is the farm's own; a caller scoped to another park sees none of
	// it, in the loads table or the 7-day table.
	other := []uuid.UUID{uuid.MustParse("11111111-1111-4111-8111-111111111111")}
	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	scoped, err := repo.StockLoads(ctx, fdiTenant, other, domain.StockLoadsQuery{})
	if err != nil {
		t.Fatalf("scoped StockLoads: %v", err)
	}
	if len(scoped.Rows) != 0 || scoped.Total != 0 {
		t.Errorf("another park's scope must not see CBE's loads: %+v", scoped)
	}
	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	scopedStock, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{ParkIDs: other})
	if err != nil {
		t.Fatalf("scoped StockAnalytics: %v", err)
	}
	if len(scopedStock.Forecast) != 0 {
		t.Errorf("another park's scope must not see CBE's 7-day rows: %+v", scopedStock.Forecast)
	}

	// PageBoundary: a page of one row still carries the family arithmetic and the whole-filter
	// total; the retired load that feeds the queue is never counted as a row.
	activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
	one, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{FarmLabel: "CBE", Limit: 1})
	if err != nil {
		t.Fatalf("page of one: %v", err)
	}
	if one.Total != 1 || len(one.Rows) != 1 || one.Rows[0].LeftKg != card.BalanceKg {
		t.Errorf("a page of one keeps the card's kg left and the whole-filter total: %+v", one)
	}
}

// END TO END, through the real write paths on both sides (maintainer questions, 2026-09-21):
// "at what time do we ignore it from the stock", "how are you calculating how many days left",
// and "if I change rows will everything change".
//
// The load is RECORDED through procurement's own CreateFeedPurchase and later CORRECTED through
// its UpdateFeedPurchase; the sheet is issued and locked through feeddirection's own PersistIssue
// and LockIssue. Nothing is hand-inserted, so what this asserts is what the app actually does.
//
// The three answers it pins:
//
//	WHEN     a load starts depleting at the LOCK, never at issue. An issued sheet is a plan and
//	         moves no kilogram; the moment the sheet is locked its kg count against stock.
//	DAYS     days left = floor(kg left / the mean of the THREE most recent locked days). Every
//	         number below is arithmetic anyone can redo by hand.
//	CHANGES  a correction to the load re-reads: the page is served from a cache keyed on a
//	         revision of the ledger and the sheets, so an edit must move the figure on the next
//	         read with no restart and no waiting.
func TestStockLoadsEndToEndFromPurchaseThroughSheetLockToCorrection(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	procurement := procurementpg.NewRepository(pool, 10*time.Second)

	const label = "Mesha Kids Goat Concentrate"
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, status) VALUES ($1::uuid, $2, 'active')
ON CONFLICT (tenant_id, feed_item_key) DO NOTHING`, fdiTenant, label); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	four := 4
	record := func(day, qty string, days *int, idem string) procdomain.FeedPurchase {
		t.Helper()
		amount := 40.0
		kgFloat, _ := strconv.ParseFloat(qty, 64)
		got, err := procurement.CreateFeedPurchase(ctx, fdiTenant, procdomain.FeedPurchaseWrite{
			PurchaseDate: day, FarmLabel: "CBE", FeedItemLabel: label, QuantityKg: kgFloat,
			TotalCost: &amount, Vendor: "Navaladi", PaymentStatus: "Paid",
			DaysOfStock: days, ReachedOn: day,
		}, "", idem)
		if err != nil {
			t.Fatalf("record purchase %s: %v", idem, err)
		}
		return got
	}
	// Issue a day's sheet WITHOUT locking it, and lock it separately, so the two moments can be
	// told apart -- that is the whole point of this test.
	issue := func(day, qty string) {
		t.Helper()
		at := time.Date(2026, 9, 1, 9, 0, 0, 0, biztime.DefaultLocation())
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: at, Fingerprint: "e2e" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":e2e",
			GeneratedBy:    "test",
			Cells: []domain.StoredCell{{
				ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
				PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
				RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
				HeadCount: 10, Workflow: domain.WorkflowNormal,
				FeedItemLabel: label, FeedItemKey: "mesha_kids_goat_concentrate",
				QuantityKg: kg(qty), SessionTotalKg: qty,
			}},
		}); err != nil {
			t.Fatalf("issue %s: %v", day, err)
		}
	}
	lock := func(day string) {
		t.Helper()
		at := time.Date(2026, 9, 1, 15, 30, 0, 0, biztime.DefaultLocation())
		if out, err := repo.LockIssue(ctx, ports.LockIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day,
			Workflow: domain.WorkflowNormal, LockedAt: at,
		}); err != nil || out.Outcome != "locked" {
			t.Fatalf("lock %s = (%v, %v)", day, out.Outcome, err)
		}
	}
	read := func(stage string) domain.StockLoadRow {
		t.Helper()
		activatePurchasedFeeds(t, ctx, pool) // only ACTIVE catalog feeds have stock cards (2026-09-24)
		page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{})
		if err != nil {
			t.Fatalf("%s: StockLoads: %v", stage, err)
		}
		if len(page.Rows) == 0 {
			t.Fatalf("%s: no rows", stage)
		}
		return page.Rows[len(page.Rows)-1] // oldest purchase last; load A throughout
	}
	show := func(r domain.StockLoadRow) string {
		d := func(v *int64) string {
			if v == nil {
				return "nil"
			}
			return itoa(*v)
		}
		return fmt.Sprintf("status=%s bought=%s used=%s left=%s from=%q daysUsed=%d rate=%q daysLeft=%s gap=%s",
			r.Status, r.PurchasedKg, r.ConsumedKg, r.LeftKg, r.ConsumptionFrom, r.DaysConsumed, r.AvgDailyKg, d(r.DaysLeft), d(r.GapDays))
	}

	// STEP 1 -- the load is bought and has arrived. Nothing has been fed, so nothing is used and
	// there is no rate to divide by: days left is ABSENT, not zero, and so is the check.
	loadA := record("2026-09-01", "100.000", &four, "e2e-load-a")
	if got := show(read("after purchase")); got != `status=not_started bought=100.0 used=0.0 left=100.0 from="" daysUsed=0 rate="" daysLeft=nil gap=nil` {
		t.Fatalf("a bought load nobody has fed from yet:\n got %s", got)
	}

	// STEP 2 -- THE SHEET IS ISSUED BUT NOT LOCKED. This is the answer to "at what time".
	// An issued sheet is a plan; it must move NOTHING. Byte for byte the step-1 row.
	issue("2026-09-01", "30.000")
	if got := show(read("after issue, before lock")); got != `status=not_started bought=100.0 used=0.0 left=100.0 from="" daysUsed=0 rate="" daysLeft=nil gap=nil` {
		t.Fatalf("an ISSUED sheet is a plan and must not deplete stock:\n got %s", got)
	}

	// STEP 3 -- THE LOCK. Now the 30 kg count. Rate = 30 (one locked day), left = 70,
	// days left = floor(70 / 30) = 2, check = said 4 - used 1 - left 2 = 1.
	lock("2026-09-01")
	if got := show(read("after lock")); got != `status=in_use bought=100.0 used=30.0 left=70.0 from="2026-09-01" daysUsed=1 rate="30.0" daysLeft=2 gap=1` {
		t.Fatalf("the LOCK is the moment stock moves:\n got %s", got)
	}

	// STEP 4 -- two more locked days at a different rate. The rate is the mean of the three most
	// recent locked days: (30 + 20 + 10) / 3 = 20. Left = 100 - 60 = 40, floor(40/20) = 2,
	// check = 4 - 3 - 2 = -1.
	issue("2026-09-02", "20.000")
	lock("2026-09-02")
	issue("2026-09-03", "10.000")
	lock("2026-09-03")
	if got := show(read("three locked days")); got != `status=in_use bought=100.0 used=60.0 left=40.0 from="2026-09-01" daysUsed=3 rate="20.0" daysLeft=2 gap=-1` {
		t.Fatalf("days left divides by the mean of the three most recent locked days:\n got %s", got)
	}

	// STEP 5 -- a SECOND load arrives. It must not disturb load A at all: same kg, same days,
	// same check. A purchase is not a consumption event.
	record("2026-09-04", "50.000", nil, "e2e-load-b")
	if got := show(read("after the second load")); got != `status=in_use bought=100.0 used=60.0 left=40.0 from="2026-09-01" daysUsed=3 rate="20.0" daysLeft=2 gap=-1` {
		t.Fatalf("buying another load changes nothing about the one being fed:\n got %s", got)
	}

	// STEP 6 -- "IF I CHANGE ROWS WILL IT CHANGE". The buyer corrects his figure from 4 days to 9
	// through the real edit path. The page is served from a revision-keyed cache, so the check
	// must move on the very next read: 9 - 3 - 2 = 4.
	nine := 9
	if _, err := procurement.UpdateFeedPurchase(ctx, fdiTenant, loadA.FeedPurchaseID, procdomain.FeedPurchaseEdit{
		PurchaseDate: "2026-09-01", QuantityKg: 100, Vendor: "Navaladi", DaysOfStock: &nine,
	}, ""); err != nil {
		t.Fatalf("edit days of stock: %v", err)
	}
	if got := show(read("after correcting days said")); got != `status=in_use bought=100.0 used=60.0 left=40.0 from="2026-09-01" daysUsed=3 rate="20.0" daysLeft=2 gap=4` {
		t.Fatalf("a corrected figure must reach the next read, not a stale cached page:\n got %s", got)
	}

	// STEP 7 -- correcting the QUANTITY moves the kilograms and everything derived from them:
	// 70 bought, 60 already fed, 10 left, floor(10/20) = 0 days left, check 9 - 3 - 0 = 6.
	if _, err := procurement.UpdateFeedPurchase(ctx, fdiTenant, loadA.FeedPurchaseID, procdomain.FeedPurchaseEdit{
		PurchaseDate: "2026-09-01", QuantityKg: 70, Vendor: "Navaladi", DaysOfStock: &nine,
	}, ""); err != nil {
		t.Fatalf("edit quantity: %v", err)
	}
	if got := show(read("after correcting the quantity")); got != `status=in_use bought=70.0 used=60.0 left=10.0 from="2026-09-01" daysUsed=3 rate="20.0" daysLeft=0 gap=6` {
		t.Fatalf("a corrected quantity must move the kg and everything derived from them:\n got %s", got)
	}

	// STEP 8 -- CLEARING the figure (nil, not 0) takes the check away rather than reading zero.
	if _, err := procurement.UpdateFeedPurchase(ctx, fdiTenant, loadA.FeedPurchaseID, procdomain.FeedPurchaseEdit{
		PurchaseDate: "2026-09-01", QuantityKg: 70, Vendor: "Navaladi", DaysOfStock: nil,
	}, ""); err != nil {
		t.Fatalf("clear days of stock: %v", err)
	}
	if got := show(read("after clearing days said")); got != `status=in_use bought=70.0 used=60.0 left=10.0 from="2026-09-01" daysUsed=3 rate="20.0" daysLeft=0 gap=nil` {
		t.Fatalf("a cleared figure is absent, never a zero the buyer never stated:\n got %s", got)
	}
}

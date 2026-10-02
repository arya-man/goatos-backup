package domain

import (
	"testing"
	"time"
)

func istDay(t *testing.T, date string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	d, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fact(kind, actor, name, date string, hour int, amount float64) SalesActivityFact {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	d, _ := time.ParseInLocation("2006-01-02", date, loc)
	return SalesActivityFact{
		Kind: kind, ActorID: actor, ActorName: name,
		At: d.Add(time.Duration(hour) * time.Hour), BusinessDate: date,
		Subject: kind + "-" + date, Amount: amount,
	}
}

func TestSalesExecutivePeriodIsBusinessDaysEndingToday(t *testing.T) {
	today := istDay(t, "2026-10-02")
	from, prev, to := SalesExecutivePeriod(today, 7)
	if got := from.Format("2006-01-02"); got != "2026-09-26" {
		t.Fatalf("from = %s, want 2026-09-26 (7 days including today)", got)
	}
	if got := prev.Format("2006-01-02"); got != "2026-09-19" {
		t.Fatalf("prevFrom = %s, want 2026-09-19", got)
	}
	if !to.Equal(today) {
		t.Fatalf("to = %s, want today", to)
	}
}

func TestBuildSalesExecutiveAnalyticsSplitsPeriodsAndFoldsPerPerson(t *testing.T) {
	today := istDay(t, "2026-10-02")
	facts := []SalesActivityFact{
		// The period before: counted in Previous only, never in a person row or a day.
		fact(ActivityVendorAdded, "hemant", "Hemant", "2026-09-20", 10, 0),
		fact(ActivitySaleRecorded, "hemant", "Hemant", "2026-09-25", 10, 5000),
		// The period.
		fact(ActivityVendorAdded, "hemant", "Hemant", "2026-09-26", 10, 0),
		fact(ActivityVendorAdded, "hemant", "Hemant", "2026-10-02", 9, 0),
		fact(ActivityVendorEdited, "hemant", "Hemant", "2026-10-02", 11, 0),
		fact(ActivityMarketCall, "hemant", "Hemant", "2026-10-01", 8, 0),
		fact(ActivityLeadCall, "manohar", "Manohar", "2026-10-01", 12, 0),
		fact(ActivitySaleRecorded, "hemant", "Hemant", "2026-09-30", 15, 120000),
		fact(ActivityPaymentRecorded, "hemant", "Hemant", "2026-09-30", 16, 50000),
		fact(ActivityDealStatus, "manohar", "Manohar", "2026-10-02", 13, 0),
		// Outside both windows: ignored rather than trusted.
		fact(ActivityVendorAdded, "hemant", "Hemant", "2026-09-01", 10, 0),
	}
	out := BuildSalesExecutiveAnalytics(facts, nil, 692, 661, 7, today, SalesExecutivePage{})

	if out.PeriodFrom != "2026-09-26" || out.PeriodTo != "2026-10-02" {
		t.Fatalf("period = %s..%s", out.PeriodFrom, out.PeriodTo)
	}
	if out.TrendGrain != TrendGrainDay || len(out.Daily) != 7 {
		t.Fatalf("trend = %s with %d buckets, want 7 days (one per business day, empty days included)", out.TrendGrain, len(out.Daily))
	}
	c := out.Current
	if c.VendorsAdded != 2 || c.VendorsEdited != 1 || c.Calls() != 2 || c.SalesRecorded != 1 ||
		c.PaymentsRecorded != 1 || c.DealStatusChanges != 1 || c.SalesValue != 120000 || c.PaymentsValue != 50000 {
		t.Fatalf("current counts wrong: %+v", c)
	}
	if c.Total() != 8 {
		t.Fatalf("total = %d, want 8", c.Total())
	}
	if out.Previous.VendorsAdded != 1 || out.Previous.SalesRecorded != 1 || out.Previous.SalesValue != 5000 {
		t.Fatalf("previous counts wrong: %+v", out.Previous)
	}
	// Disjoint buckets: the day series sums back to the period's counts.
	var added, edited, calls, sales int
	for _, d := range out.Daily {
		added += d.VendorsAdded
		edited += d.VendorsEdited
		calls += d.Calls
		sales += d.Sales
	}
	if added != c.VendorsAdded || edited != c.VendorsEdited || calls != c.Calls() || sales != c.SalesRecorded {
		t.Fatalf("daily sums %d/%d/%d/%d disagree with the period counts", added, edited, calls, sales)
	}
	if out.ActivePeople != 2 || out.People[0].ActorID != "hemant" {
		t.Fatalf("people = %+v, want hemant first (busiest)", out.People)
	}
	h := out.People[0]
	if h.Counts.Total() != 6 || h.ActiveDays != 4 || h.LastActivityKind != ActivityVendorEdited {
		t.Fatalf("hemant row wrong: total=%d days=%d last=%s", h.Counts.Total(), h.ActiveDays, h.LastActivityKind)
	}
	if len(out.Recent) != 8 || out.RecentTotal != 8 || out.Recent[0].Kind != ActivityDealStatus {
		t.Fatalf("recent should be the period's activities newest first, got %d starting %s", len(out.Recent), out.Recent[0].Kind)
	}
	if out.RegisterVendors != 692 || out.ImportedVendors != 661 {
		t.Fatalf("register totals not carried: %d/%d", out.RegisterVendors, out.ImportedVendors)
	}
}

func TestTheActivityFeedPagesOverTheWholePeriodWithoutTouchingTheCounts(t *testing.T) {
	today := istDay(t, "2026-10-02")
	facts := make([]SalesActivityFact, 0, 50)
	for i := 0; i < 50; i++ {
		f := fact(ActivityMarketCall, "hemant", "Hemant", "2026-10-02", 0, 0)
		f.At = f.At.Add(time.Duration(i) * time.Minute)
		facts = append(facts, f)
	}
	latest := make([]LatestVendorFact, 0, 25)
	for i := 0; i < 25; i++ {
		latest = append(latest, LatestVendorFact{VendorID: string(rune('a' + i)), AddedAt: today.Add(time.Duration(i) * time.Minute)})
	}
	first := BuildSalesExecutiveAnalytics(facts, latest, 0, 0, 30, today, SalesExecutivePage{})
	if first.RecentTotal != 50 || len(first.Recent) != SalesExecutivePageSize || first.Current.MarketCalls != 50 {
		t.Fatalf("page 1: total=%d rows=%d calls=%d", first.RecentTotal, len(first.Recent), first.Current.MarketCalls)
	}
	if len(first.LatestVendors) != SalesExecutivePageSize || first.LatestVendors[0].VendorID != "y" {
		t.Fatalf("latest vendors not newest-first and page-sized: %d first=%s", len(first.LatestVendors), first.LatestVendors[0].VendorID)
	}
	third := BuildSalesExecutiveAnalytics(facts, nil, 0, 0, 30, today, SalesExecutivePage{ActivityOffset: 40})
	if len(third.Recent) != 10 || third.ActivityOffset != 40 || third.Current.MarketCalls != 50 {
		t.Fatalf("page 3: rows=%d offset=%d calls=%d", len(third.Recent), third.ActivityOffset, third.Current.MarketCalls)
	}
	// Page 3 continues page 1's order: its first row is the 41st newest.
	if !third.Recent[0].At.Equal(facts[9].At) {
		t.Fatalf("page 3 starts at %s, want %s", third.Recent[0].At, facts[9].At)
	}
	beyond := BuildSalesExecutiveAnalytics(facts, nil, 0, 0, 30, today, SalesExecutivePage{ActivityOffset: 200})
	if len(beyond.Recent) != 0 || beyond.RecentTotal != 50 {
		t.Fatalf("past the end: rows=%d total=%d", len(beyond.Recent), beyond.RecentTotal)
	}
	if (SalesExecutivePage{ActivityOffset: -1}).Valid() || (SalesExecutivePage{VendorOffset: MaxSalesExecutiveOffset + 1}).Valid() {
		t.Fatal("out-of-range offsets must be refused")
	}
}

func TestUnattributedActivityCountsButNamesNobody(t *testing.T) {
	today := istDay(t, "2026-10-02")
	out := BuildSalesExecutiveAnalytics([]SalesActivityFact{
		fact(ActivitySaleRecorded, "", "", "2026-10-02", 10, 100),
	}, nil, 0, 0, 7, today, SalesExecutivePage{})
	if out.Current.SalesRecorded != 1 || out.ActivePeople != 0 {
		t.Fatalf("unattributed sale: counted=%d people=%d", out.Current.SalesRecorded, out.ActivePeople)
	}
}

func TestValidSalesExecutiveDays(t *testing.T) {
	for _, d := range []int{7, 30, 90} {
		if !ValidSalesExecutiveDays(d) {
			t.Fatalf("%d should be offered", d)
		}
	}
	for _, d := range []int{0, 1, 31, 365, -7} {
		if ValidSalesExecutiveDays(d) {
			t.Fatalf("%d should be refused", d)
		}
	}
}

func TestLongerPeriodsTrendWeekByWeekAndStillSumToTheHeadline(t *testing.T) {
	today := istDay(t, "2026-10-02")
	facts := []SalesActivityFact{
		fact(ActivityVendorAdded, "hemant", "Hemant", "2026-07-05", 10, 0), // first day of a 90-day period
		fact(ActivityVendorAdded, "hemant", "Hemant", "2026-07-11", 10, 0), // last day of week 1
		fact(ActivityVendorAdded, "hemant", "Hemant", "2026-07-12", 10, 0), // first day of week 2
		fact(ActivitySaleRecorded, "hemant", "Hemant", "2026-10-02", 10, 10),
	}
	out := BuildSalesExecutiveAnalytics(facts, nil, 0, 0, 90, today, SalesExecutivePage{})
	if out.TrendGrain != TrendGrainWeek || len(out.Daily) != 13 {
		t.Fatalf("trend = %s with %d buckets, want 13 weeks", out.TrendGrain, len(out.Daily))
	}
	first, last := out.Daily[0], out.Daily[len(out.Daily)-1]
	if first.Date != "2026-07-05" || first.DateTo != "2026-07-11" || first.VendorsAdded != 2 {
		t.Fatalf("week 1 = %+v", first)
	}
	if out.Daily[1].VendorsAdded != 1 {
		t.Fatalf("week 2 = %+v", out.Daily[1])
	}
	if last.Date != "2026-09-27" || last.DateTo != "2026-10-02" || last.Sales != 1 {
		t.Fatalf("last (short) week = %+v", last)
	}
	added := 0
	for _, b := range out.Daily {
		added += b.VendorsAdded
	}
	if added != out.Current.VendorsAdded {
		t.Fatalf("weekly buckets sum %d, headline %d", added, out.Current.VendorsAdded)
	}
}

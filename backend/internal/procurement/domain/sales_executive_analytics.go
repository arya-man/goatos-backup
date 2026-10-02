package domain

import (
	"sort"
	"time"
)

// SALES EXECUTIVE ANALYTICS (maintainer request 2026-10-02): what the sales desk is DOING, read
// back per person and per day -- vendors added to the register, vendors changed, market and buyer
// calls recorded, sales and payments recorded. Canonical prose:
// docs/decisions/sales-executive-analytics.md.
//
// RECORDED CROSS-MODULE REPORTING READ, the buyer-analytics shape. Procurement owns the vendor
// register and joins OUT, read-only, to the facts that already say who did what: the register's
// own created_by/updated_by stamps, the audit trail the sales ledger writes inside every write
// transaction, and the market survey's recorded_by. Nothing here gates or edits anything.
//
// GRAIN. The input is one ACTIVITY: one vendor added, one vendor edited by one person on one
// business day (several saves of the same vendor by the same person that day are one edit), one
// market city called by one person on one business day (however many prices were typed for it),
// one lead call / sale / payment / deal status change as the audit trail recorded it. Every count
// below is a count of activities, and the buckets are DISJOINT by kind.

// Activity kinds, the wire values the page keys its copy on.
const (
	ActivityVendorAdded     = "vendor_added"
	ActivityVendorEdited    = "vendor_edited"
	ActivityMarketCall      = "market_call"
	ActivityLeadCall        = "lead_call"
	ActivitySaleRecorded    = "sale_recorded"
	ActivityPaymentRecorded = "payment_recorded"
	ActivityDealStatus      = "deal_status"
)

// SalesExecutiveWindowDays are the periods the page offers, in business days ending today.
var SalesExecutiveWindowDays = []int{7, 30, 90}

// SalesExecutiveDefaultDays is the period the page opens on.
const SalesExecutiveDefaultDays = 30

// SalesExecutivePageSize is the page size of the activity feed and of the latest-vendors list.
const SalesExecutivePageSize = 20

// MaxSalesExecutiveOffset is the deepest page offset either list accepts; a deeper one is refused
// rather than clamped, so a page number never shows the wrong rows.
const MaxSalesExecutiveOffset = 10000

// SalesExecutivePage is which page of each paged list the caller is on.
type SalesExecutivePage struct {
	ActivityOffset int
	VendorOffset   int
}

// Valid reports whether both offsets are in range.
func (p SalesExecutivePage) Valid() bool {
	return p.ActivityOffset >= 0 && p.ActivityOffset <= MaxSalesExecutiveOffset &&
		p.VendorOffset >= 0 && p.VendorOffset <= MaxSalesExecutiveOffset
}

// ValidSalesExecutiveDays reports whether days is one of the offered periods.
func ValidSalesExecutiveDays(days int) bool {
	for _, d := range SalesExecutiveWindowDays {
		if d == days {
			return true
		}
	}
	return false
}

// SalesActivityFact is one activity as the repository resolved it.
type SalesActivityFact struct {
	Kind    string
	ActorID string
	// ActorName is "" when the actor id resolves to no person (or to two different names).
	ActorName string
	At        time.Time
	// BusinessDate is the IST business day of At, YYYY-MM-DD.
	BusinessDate string
	SubjectID    string
	// Subject names what the activity was about: the vendor, the city, the buyer.
	Subject string
	// Category is the vendor's record type, or the call / deal status, depending on Kind.
	Category string
	Animals  float64
	// Amount is the sale value or the payment amount, in rupees; 0 for other kinds.
	Amount float64
}

// LatestVendorFact is one of the newest rows in the vendor register.
type LatestVendorFact struct {
	VendorID     string
	BusinessName string
	Category     string
	Place        string
	// AddedByName is "" when the row came from the sheet import or its adder is unknown;
	// AddedByKnown separates the two.
	AddedByName  string
	AddedByKnown bool
	AddedAt      time.Time
}

// SalesExecutiveCounts is one set of activity counts: for the whole period, for one person, or
// for the period before.
type SalesExecutiveCounts struct {
	VendorsAdded      int
	VendorsEdited     int
	MarketCalls       int
	LeadCalls         int
	SalesRecorded     int
	PaymentsRecorded  int
	DealStatusChanges int
	SalesValue        float64
	PaymentsValue     float64
}

// Calls is market calls plus buyer/farmer-group calls.
func (c SalesExecutiveCounts) Calls() int { return c.MarketCalls + c.LeadCalls }

// Total is every activity counted, of every kind.
func (c SalesExecutiveCounts) Total() int {
	return c.VendorsAdded + c.VendorsEdited + c.MarketCalls + c.LeadCalls +
		c.SalesRecorded + c.PaymentsRecorded + c.DealStatusChanges
}

func (c *SalesExecutiveCounts) add(f SalesActivityFact) {
	switch f.Kind {
	case ActivityVendorAdded:
		c.VendorsAdded++
	case ActivityVendorEdited:
		c.VendorsEdited++
	case ActivityMarketCall:
		c.MarketCalls++
	case ActivityLeadCall:
		c.LeadCalls++
	case ActivitySaleRecorded:
		c.SalesRecorded++
		c.SalesValue += f.Amount
	case ActivityPaymentRecorded:
		c.PaymentsRecorded++
		c.PaymentsValue += f.Amount
	case ActivityDealStatus:
		c.DealStatusChanges++
	}
}

// Trend grains: a 7-day period is drawn day by day; a longer one week by week, so the whole
// period fits on one screen instead of opening on its oldest, emptiest days.
const (
	TrendGrainDay  = "day"
	TrendGrainWeek = "week"
)

// SalesExecutiveTrendGrain is the grain the trend is bucketed at for a period of days.
func SalesExecutiveTrendGrain(days int) string {
	if days <= 7 {
		return TrendGrainDay
	}
	return TrendGrainWeek
}

// SalesExecutiveDay is one bucket of the trend chart: a business day, or seven business days
// counted forward from the period's first day (the last bucket may be shorter). Date is the
// bucket's first business day and DateTo its last; they are equal at the day grain.
type SalesExecutiveDay struct {
	Date          string
	DateTo        string
	VendorsAdded  int
	VendorsEdited int
	Calls         int
	Sales         int
}

// SalesExecutivePerson is one person's work in the period.
type SalesExecutivePerson struct {
	ActorID string
	Name    string
	Counts  SalesExecutiveCounts
	// ActiveDays is how many business days of the period this person recorded anything.
	ActiveDays       int
	LastActiveAt     time.Time
	LastActivityKind string
}

// SalesExecutiveAnalytics is the whole page.
type SalesExecutiveAnalytics struct {
	Days       int
	TrendGrain string
	PeriodFrom string
	PeriodTo   string
	Current    SalesExecutiveCounts
	// Previous is the same number of days immediately before PeriodFrom, so the page can say
	// whether the flow is rising or falling.
	Previous     SalesExecutiveCounts
	ActivePeople int
	// RegisterVendors is every row in the vendor register today; ImportedVendors is how many of
	// them carry no adder (the sheet import), so "added by" can only ever explain the rest.
	RegisterVendors int
	ImportedVendors int
	Daily           []SalesExecutiveDay
	People          []SalesExecutivePerson
	// Recent is ONE PAGE of the period's activities, newest first; RecentTotal is how many the
	// whole period holds.
	Recent         []SalesActivityFact
	RecentTotal    int
	ActivityOffset int
	// LatestVendors is ONE PAGE of the register, newest first; RegisterVendors is its total.
	LatestVendors []LatestVendorFact
	VendorOffset  int
	PageSize      int
}

// SalesExecutivePeriod returns the first business date of the period, the first business date of
// the period before it, and today's business date, for a period of days business days ending on
// today (an IST business-day start).
func SalesExecutivePeriod(today time.Time, days int) (from, prevFrom, to time.Time) {
	to = today
	from = today.AddDate(0, 0, -(days - 1))
	prevFrom = from.AddDate(0, 0, -days)
	return from, prevFrom, to
}

// BuildSalesExecutiveAnalytics folds the activities of the period AND the period before it into
// the page. today is the IST business-day start of today; facts outside [prevFrom, today] are
// ignored rather than trusted.
//
// latest is already the requested page of the register (the repository pages it); the activity
// feed is paged here, over every activity of the period, after the totals are taken.
func BuildSalesExecutiveAnalytics(facts []SalesActivityFact, latest []LatestVendorFact, registerVendors, importedVendors, days int, today time.Time, page SalesExecutivePage) SalesExecutiveAnalytics {
	from, prevFrom, to := SalesExecutivePeriod(today, days)
	fromKey, prevKey, toKey := from.Format("2006-01-02"), prevFrom.Format("2006-01-02"), to.Format("2006-01-02")

	out := SalesExecutiveAnalytics{
		Days:            days,
		PeriodFrom:      fromKey,
		PeriodTo:        toKey,
		RegisterVendors: registerVendors,
		ImportedVendors: importedVendors,
		Daily:           make([]SalesExecutiveDay, 0, days),
		People:          []SalesExecutivePerson{},
		Recent:          []SalesActivityFact{},
		LatestVendors:   []LatestVendorFact{},
	}

	// Every business day of the period maps to exactly one bucket, so the buckets stay disjoint
	// and the trend still sums back to the headline at either grain.
	out.TrendGrain = SalesExecutiveTrendGrain(days)
	step := 1
	if out.TrendGrain == TrendGrainWeek {
		step = 7
	}
	dayIndex := make(map[string]int, days)
	for d, n := from, 0; !d.After(to); d, n = d.AddDate(0, 0, 1), n+1 {
		key := d.Format("2006-01-02")
		if n%step == 0 {
			out.Daily = append(out.Daily, SalesExecutiveDay{Date: key})
		}
		bucket := len(out.Daily) - 1
		out.Daily[bucket].DateTo = key
		dayIndex[key] = bucket
	}

	people := map[string]*SalesExecutivePerson{}
	personDays := map[string]map[string]struct{}{}
	current := make([]SalesActivityFact, 0, len(facts))
	for _, f := range facts {
		switch {
		case f.BusinessDate >= fromKey && f.BusinessDate <= toKey:
		case f.BusinessDate >= prevKey && f.BusinessDate < fromKey:
			out.Previous.add(f)
			continue
		default:
			continue
		}
		current = append(current, f)
		out.Current.add(f)

		day := &out.Daily[dayIndex[f.BusinessDate]]
		switch f.Kind {
		case ActivityVendorAdded:
			day.VendorsAdded++
		case ActivityVendorEdited:
			day.VendorsEdited++
		case ActivityMarketCall, ActivityLeadCall:
			day.Calls++
		case ActivitySaleRecorded:
			day.Sales++
		}

		if f.ActorID == "" {
			continue
		}
		p, ok := people[f.ActorID]
		if !ok {
			p = &SalesExecutivePerson{ActorID: f.ActorID, Name: f.ActorName}
			people[f.ActorID] = p
			personDays[f.ActorID] = map[string]struct{}{}
		}
		p.Counts.add(f)
		personDays[f.ActorID][f.BusinessDate] = struct{}{}
		if f.At.After(p.LastActiveAt) {
			p.LastActiveAt = f.At
			p.LastActivityKind = f.Kind
		}
	}

	for id, p := range people {
		p.ActiveDays = len(personDays[id])
		out.People = append(out.People, *p)
	}
	// Busiest first; ties by most recent activity, then name, then id so the order is total.
	sort.Slice(out.People, func(i, j int) bool {
		a, b := out.People[i], out.People[j]
		if a.Counts.Total() != b.Counts.Total() {
			return a.Counts.Total() > b.Counts.Total()
		}
		if !a.LastActiveAt.Equal(b.LastActiveAt) {
			return a.LastActiveAt.After(b.LastActiveAt)
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ActorID < b.ActorID
	})
	out.ActivePeople = len(out.People)

	sort.SliceStable(current, func(i, j int) bool { return current[i].At.After(current[j].At) })
	out.RecentTotal = len(current)
	out.ActivityOffset = page.ActivityOffset
	out.VendorOffset = page.VendorOffset
	out.PageSize = SalesExecutivePageSize
	if page.ActivityOffset < len(current) {
		end := page.ActivityOffset + SalesExecutivePageSize
		if end > len(current) {
			end = len(current)
		}
		out.Recent = append(out.Recent, current[page.ActivityOffset:end]...)
	}

	sorted := append([]LatestVendorFact(nil), latest...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].AddedAt.After(sorted[j].AddedAt) })
	if len(sorted) > SalesExecutivePageSize {
		sorted = sorted[:SalesExecutivePageSize]
	}
	out.LatestVendors = append(out.LatestVendors, sorted...)
	return out
}

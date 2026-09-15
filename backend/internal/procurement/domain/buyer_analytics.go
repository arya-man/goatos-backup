package domain

import (
	"sort"
	"strings"
	"time"
)

// BUYER ANALYTICS (maintainer request 2026-09-15): who the farm sells to, read back per buyer --
// name, phone number, category, how many times they have bought, how many animals, how much
// money, and whether they come back.
//
// RECORDED CROSS-MODULE REPORTING READ, the load-wise shape (docs/decisions/sales-loadwise.md).
// Procurement owns the vendor register (the buyer's phone number, category and place live
// nowhere else) and joins OUT to sales_deals for the deals made to each buyer. It is read-only
// over the ledger and reporting grain only: nothing here gates a sale or edits a vendor, and the
// sales module's own lock (migration 000173: sales reads nothing from herd/procurement) is
// untouched because the dependency points the other way. Canonical prose:
// docs/decisions/sales-buyer-analytics.md.
//
// WHO IS ONE BUYER. A deal recorded in the app carries buyer_vendor_id (migration 000215); the
// sheet-imported history carries only a typed name, and so do two app deals whose vendor row has
// since been deleted. Keying on the vendor id alone would split one man into two rows ("Mahendran"
// on STG: 16 sheet deals and 2 app deals), and keying on the typed name alone would throw away
// the register. So a deal is claimed by a buyer in this order, and the resolution is done ONCE in
// the repository so every figure below ranges over the same identity:
//
//  1. the vendor its buyer_vendor_id names, when that row still exists;
//  2. otherwise the ONE vendor whose business name matches the typed name (whitespace- and
//     case-insensitively). A name matching two vendors is claimed by neither -- the same
//     agree-or-go-bare rule the sex and origin filters use -- because picking one would hand a
//     stranger's phone number to the wrong row;
//  3. otherwise the typed name itself, reported as not in the register so the sales desk can
//     add them.

// BuyerDealFact is one closed deal after the repository has resolved who bought: the input grain
// of BuildBuyerAnalytics. Revenue, animals and product types are the deal's own rollup (its lines
// summed, or its single-product fields when it predates lines), computed by the repository so
// this package repeats none of the ledger's line arithmetic.
type BuyerDealFact struct {
	DealID   string
	SaleDate string // YYYY-MM-DD business date
	Farm     string

	// BuyerKey is the resolved identity: "vendor:<id>" or "name:<normalized name>". Every deal
	// with the same key is the same buyer.
	BuyerKey string
	// VendorID is set when the key resolved to a register row.
	VendorID string
	// BuyerName is the typed name on the deal, the snapshot of what the buyer was called then.
	BuyerName  string
	BuyerPlace string

	// Register facts, present only when VendorID is set. Phone is redacted by the HANDLER for a
	// caller without the vendor register permission; the repository always reads it.
	VendorName  string
	VendorPhone string
	// VendorCategory is the register's record_type (Agent, Butcher, Farmer, ...).
	VendorCategory string
	VendorPlace    string

	Animals      float64
	Revenue      float64
	Outstanding  float64
	ProductTypes []string
}

// BuyerRow is one buyer on the analytics page.
type BuyerRow struct {
	BuyerKey string
	VendorID string
	// InRegister is false for a buyer known only by the name typed on their deals.
	InRegister bool

	BuyerName string
	// Phone is blank when the buyer is not in the register, or when the caller may not see the
	// register (the handler blanks it; see BuyerAnalytics.PhonesVisible).
	Phone    string
	Category string
	Place    string

	// Purchases is the number of closed deals -- "number purchased so far" at deal grain.
	Purchases int
	Animals   float64
	Revenue   float64
	// SharePct is this buyer's share of the whole-filter revenue.
	SharePct float64
	// Outstanding is what the buyer still owes across their deals, never negative per deal.
	Outstanding float64

	FirstSaleDate string
	LastSaleDate  string
	// Repeat is true once the buyer has bought more than once. RepeatPurchases is every purchase
	// after the first.
	Repeat          bool
	RepeatPurchases int
	// AvgDaysBetween is the mean gap between purchases: the span from first to last sale over the
	// gaps between them. Nil for a one-time buyer, and nil (not 0) when every purchase landed on
	// one day, since "buys every 0 days" is not a cadence.
	AvgDaysBetween *int
	// DaysSinceLast is the age of the buyer's latest purchase at asOf. Nil when asOf precedes it.
	DaysSinceLast *int

	ProductTypes []string
}

// BuyerSummary is the whole-filter headline, ranging over every buyer the filter admits, never the
// served page.
type BuyerSummary struct {
	Buyers        int
	RepeatBuyers  int
	OneTimeBuyers int
	// NotInRegister counts buyers known by a typed name only.
	NotInRegister int
	Purchases     int
	Animals       float64
	Revenue       float64
	// RepeatRevenue is the revenue brought by buyers who have bought more than once, and
	// RepeatRevenuePct its share of Revenue (0 when Revenue is 0).
	RepeatRevenue    float64
	RepeatRevenuePct float64
	Outstanding      float64
	PeriodFrom       string
	PeriodTo         string
}

// BuyerAnalytics is the whole page in one read.
type BuyerAnalytics struct {
	Buyers []BuyerRow
	// TotalBuyers is the whole-filter buyer count; Buyers is one page of it.
	TotalBuyers int
	Summary     BuyerSummary
	Limit       int
	Offset      int
}

// Buyer page bounds: the register is a few hundred counterparties at most, and the page is a
// bounded desktop table.
const (
	DefaultBuyerPageSize = 25
	MaxBuyerPageSize     = 100
	MaxBuyerOffset       = 10000
)

// ClampBuyerPageSize resolves a requested page size to a supported one.
func ClampBuyerPageSize(requested int) int {
	switch {
	case requested <= 0:
		return DefaultBuyerPageSize
	case requested > MaxBuyerPageSize:
		return MaxBuyerPageSize
	default:
		return requested
	}
}

// NormalizeBuyerFarmFilter resolves the page's farm query parameter exactly as the sales pages
// do: "" and "all" mean the whole company; CBE or CPT must be exact. ok is false for anything
// else -- the caller rejects rather than silently widening the filter.
func NormalizeBuyerFarmFilter(raw string) (farm string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	switch {
	case trimmed == "" || strings.EqualFold(trimmed, "all"):
		return "", true
	case trimmed == "CBE" || trimmed == "CPT":
		return trimmed, true
	default:
		return "", false
	}
}

// NormalizeBuyerName is the matching key for a typed buyer name: whitespace collapsed, lower
// case. It is the SAME normalization the repository applies in SQL to the register's business
// name, so the two sides fold identically; it is exported so the test can pin that equality.
func NormalizeBuyerName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// BuildBuyerAnalytics groups resolved deal facts into the buyer page: one row per BuyerKey, the
// whole-filter summary, and the requested page of rows ordered by revenue (highest first, then
// name so the order is total).
//
// projection-review: membership=BuyerDealFact rows, one per CLOSED deal in the filter (the
// repository's own predicate: tenant, status = 'Deal Closed', optional farm), each carrying ONE
// BuyerKey resolved before this function runs; group_key=BuyerKey on both sides -- the rows here
// and the summary counts range over the identical map, so Summary.Buyers == TotalBuyers and the
// per-row shares sum to 100% by construction; join_cardinality=none here (no join; the register
// facts ride on each fact 1:1 from the repository's resolution); pagination=Offset/Limit slice
// the SORTED rows only, the summary is computed before the slice; the ratio RepeatRevenuePct has
// numerator (revenue of buyers with Purchases >= 2) and denominator (revenue of every buyer)
// ranging over the same key set, and a zero denominator yields 0 rather than a division.
func BuildBuyerAnalytics(facts []BuyerDealFact, asOf time.Time, limit, offset int) BuyerAnalytics {
	type agg struct {
		row   BuyerRow
		types map[string]struct{}
		// latest is the sale date of the deal whose typed name/place is currently shown for a
		// name-only buyer, so the newest spelling wins.
		latest string
	}
	buyers := map[string]*agg{}
	summary := BuyerSummary{}

	for _, f := range facts {
		a := buyers[f.BuyerKey]
		if a == nil {
			a = &agg{row: BuyerRow{BuyerKey: f.BuyerKey}, types: map[string]struct{}{}}
			buyers[f.BuyerKey] = a
		}
		r := &a.row
		if f.VendorID != "" {
			r.VendorID = f.VendorID
			r.InRegister = true
			r.BuyerName = f.VendorName
			r.Phone = f.VendorPhone
			r.Category = f.VendorCategory
			r.Place = f.VendorPlace
		} else if f.SaleDate >= a.latest {
			// Name-only buyer: the newest deal's spelling and place describe them.
			a.latest = f.SaleDate
			r.BuyerName = f.BuyerName
			r.Place = f.BuyerPlace
		}
		r.Purchases++
		r.Animals += f.Animals
		r.Revenue += f.Revenue
		r.Outstanding += f.Outstanding
		if r.FirstSaleDate == "" || f.SaleDate < r.FirstSaleDate {
			r.FirstSaleDate = f.SaleDate
		}
		if f.SaleDate > r.LastSaleDate {
			r.LastSaleDate = f.SaleDate
		}
		for _, p := range f.ProductTypes {
			if p != "" {
				a.types[p] = struct{}{}
			}
		}

		summary.Purchases++
		summary.Animals += f.Animals
		summary.Revenue += f.Revenue
		summary.Outstanding += f.Outstanding
		if summary.PeriodFrom == "" || f.SaleDate < summary.PeriodFrom {
			summary.PeriodFrom = f.SaleDate
		}
		if f.SaleDate > summary.PeriodTo {
			summary.PeriodTo = f.SaleDate
		}
	}

	today := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, time.UTC)
	rows := make([]BuyerRow, 0, len(buyers))
	for _, a := range buyers {
		r := a.row
		r.ProductTypes = make([]string, 0, len(a.types))
		for p := range a.types {
			r.ProductTypes = append(r.ProductTypes, p)
		}
		sort.Strings(r.ProductTypes)
		if summary.Revenue > 0 {
			r.SharePct = r.Revenue / summary.Revenue * 100
		}
		r.Repeat = r.Purchases >= 2
		if r.Repeat {
			r.RepeatPurchases = r.Purchases - 1
			if first, last, ok := parseDates(r.FirstSaleDate, r.LastSaleDate); ok {
				span := int(last.Sub(first).Hours() / 24)
				if span > 0 {
					avg := span / r.RepeatPurchases
					r.AvgDaysBetween = &avg
				}
			}
		}
		if last, err := time.Parse("2006-01-02", r.LastSaleDate); err == nil && !today.Before(last) {
			days := int(today.Sub(last).Hours() / 24)
			r.DaysSinceLast = &days
		}

		summary.Buyers++
		if r.Repeat {
			summary.RepeatBuyers++
			summary.RepeatRevenue += r.Revenue
		} else {
			summary.OneTimeBuyers++
		}
		if !r.InRegister {
			summary.NotInRegister++
		}
		rows = append(rows, r)
	}
	if summary.Revenue > 0 {
		summary.RepeatRevenuePct = summary.RepeatRevenue / summary.Revenue * 100
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Revenue != rows[j].Revenue {
			return rows[i].Revenue > rows[j].Revenue
		}
		if rows[i].BuyerName != rows[j].BuyerName {
			return rows[i].BuyerName < rows[j].BuyerName
		}
		return rows[i].BuyerKey < rows[j].BuyerKey
	})

	limit = ClampBuyerPageSize(limit)
	if offset < 0 {
		offset = 0
	}
	page := []BuyerRow{}
	if offset < len(rows) {
		end := offset + limit
		if end > len(rows) {
			end = len(rows)
		}
		page = rows[offset:end]
	}
	return BuyerAnalytics{
		Buyers:      page,
		TotalBuyers: len(rows),
		Summary:     summary,
		Limit:       limit,
		Offset:      offset,
	}
}

func parseDates(first, last string) (time.Time, time.Time, bool) {
	f, err := time.Parse("2006-01-02", first)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	l, err := time.Parse("2006-01-02", last)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	return f, l, true
}

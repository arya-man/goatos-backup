// Package domain holds the sales ledger's business types and validation.
//
// Sales is a COMMERCIAL module: it records deals with buyers (live animals and manure), the demand
// pipeline behind them, and the evidence panels that back the numbers. It owns its own tables and
// reads NOTHING from the herd, vaccination, or procurement schemas -- the ledger's tag strings are
// sheet-era labels that resolve to no goat, and the terminal sold exit of a live animal stays on
// its authoritative herd workflow.
package domain

import (
	"fmt"
	"strings"
	"time"
)

// Farms the business operates. These are the CHECK-constrained storage forms.
const (
	FarmCBE = "CBE"
	FarmCPT = "CPT"
)

// Product types a deal can sell. Sheep and Goat are the LIVE types (they carry animal counts and
// live weight); Manure contributes weight and revenue but never animal counts.
const (
	ProductSheep  = "Sheep"
	ProductGoat   = "Goat"
	ProductManure = "Manure"
)

// Deal statuses, exactly the sheet's vocabulary. Only StatusDealClosed counts toward the overview's
// revenue/animals/price aggregates; the other three are pipeline states.
const (
	StatusDealClosed   = "Deal Closed"
	StatusDealFailed   = "Deal Failed"
	StatusInDiscussion = "In Discussion"
	StatusAdvancePaid  = "Advance Paid"
)

// Farms, ProductTypes and Statuses mirror the CHECK constraints on sales_deals.
var (
	Farms        = []string{FarmCBE, FarmCPT}
	ProductTypes = []string{ProductSheep, ProductGoat, ProductManure}
	Statuses     = []string{StatusDealClosed, StatusDealFailed, StatusInDiscussion, StatusAdvancePaid}
)

// IsFarm reports whether raw is one of the two farms, exactly as stored.
func IsFarm(raw string) bool { return raw == FarmCBE || raw == FarmCPT }

// IsProductType reports whether raw is a sellable product type, exactly as stored.
func IsProductType(raw string) bool {
	return raw == ProductSheep || raw == ProductGoat || raw == ProductManure
}

// IsLiveProduct reports whether the product type is a live animal (counts toward animals and
// live weight) rather than manure.
func IsLiveProduct(raw string) bool { return raw == ProductSheep || raw == ProductGoat }

// IsStatus reports whether raw is a recognised deal status, exactly as stored.
func IsStatus(raw string) bool {
	switch raw {
	case StatusDealClosed, StatusDealFailed, StatusInDiscussion, StatusAdvancePaid:
		return true
	default:
		return false
	}
}

// NormalizeFarmFilter resolves the page's farm query parameter. "" and "all" mean the whole
// company; a farm value must be exact. ok is false for anything else -- the caller rejects rather
// than silently widening the filter.
func NormalizeFarmFilter(raw string) (farm string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	switch {
	case trimmed == "" || strings.EqualFold(trimmed, "all"):
		return "", true
	case trimmed == FarmCBE || trimmed == FarmCPT:
		return trimmed, true
	default:
		return "", false
	}
}

// Deal is one row of the sales ledger: one sheet row, or one deal recorded in the app.
type Deal struct {
	DealID   string
	TenantID string

	SaleDate string // YYYY-MM-DD business date
	Farm     string

	SourceSalesID    *int
	SourcePurchaseID *int
	SourceRowNo      *int

	BuyerName  string
	BuyerPlace *string

	ProductType string
	Breed       string

	AnimalCount   *float64
	MaleCount     *float64
	FemaleCount   *float64
	TotalWeightKg *float64

	AdvanceAmount *float64
	SalesValue    float64

	Status   string
	Feedback *string
	Comments *string

	CreatedAt string
	UpdatedAt string
}

// Animals resolves how many animals this deal moved: animal_count when recorded, otherwise the
// male+female split, and always zero for manure -- manure never contributes animal counts.
func (d Deal) Animals() float64 {
	if !IsLiveProduct(d.ProductType) {
		return 0
	}
	if d.AnimalCount != nil {
		return *d.AnimalCount
	}
	total := 0.0
	if d.MaleCount != nil {
		total += *d.MaleCount
	}
	if d.FemaleCount != nil {
		total += *d.FemaleCount
	}
	return total
}

// Month returns the deal's YYYY-MM bucket, derived from sale_date -- a business DATE, never
// timestamp arithmetic.
func (d Deal) Month() string {
	if len(d.SaleDate) < 7 {
		return d.SaleDate
	}
	return d.SaleDate[:7]
}

// Field length caps: generous relative to real data, present to stop a pasted document becoming a
// buyer name.
const (
	maxDealShortField = 160
	maxDealLongField  = 2000
)

// DealWrite is the validated payload for recording a sale. Optional text is a value with ""
// meaning "not set" (stored NULL); optional numbers are pointers so 0 stays distinct from absent.
type DealWrite struct {
	SaleDate      string
	Farm          string
	ProductType   string
	Breed         string
	BuyerName     string
	BuyerPlace    string
	AnimalCount   *float64
	MaleCount     *float64
	FemaleCount   *float64
	TotalWeightKg *float64
	SalesValue    float64
	AdvanceAmount *float64
	Comments      string
}

// ErrDealValidation reports a rejected write with a field-specific, operator-readable reason.
type ErrDealValidation struct {
	Field  string
	Reason string
}

func (e ErrDealValidation) Error() string {
	return fmt.Sprintf("sales deal %s: %s", e.Field, e.Reason)
}

// Normalize collapses whitespace on the short text fields and trims the prose ones. It does NOT
// validate; call Validate after, so the rules apply to the values that will actually be stored.
func (w DealWrite) Normalize() DealWrite {
	out := w
	collapse := func(v string) string { return strings.Join(strings.Fields(v), " ") }
	out.SaleDate = strings.TrimSpace(w.SaleDate)
	out.Farm = strings.TrimSpace(w.Farm)
	out.ProductType = strings.TrimSpace(w.ProductType)
	out.Breed = collapse(w.Breed)
	out.BuyerName = collapse(w.BuyerName)
	out.BuyerPlace = collapse(w.BuyerPlace)
	out.Comments = strings.TrimSpace(w.Comments)
	return out
}

// Validate enforces the enums, required fields and bounds, returning the FIRST failure.
//
// The enums are validate-or-reject, never silently defaulted: a farm or product type the CHECK
// constraint would refuse must fail here with a field-specific message, not be rewritten to a
// value the caller never entered.
func (w DealWrite) Validate() error {
	if w.SaleDate == "" {
		return ErrDealValidation{Field: "sale_date", Reason: "required"}
	}
	if _, err := time.Parse("2006-01-02", w.SaleDate); err != nil {
		return ErrDealValidation{Field: "sale_date", Reason: "must be a date like 2026-08-17"}
	}
	if !IsFarm(w.Farm) {
		return ErrDealValidation{Field: "farm", Reason: "must be CBE or CPT"}
	}
	if !IsProductType(w.ProductType) {
		return ErrDealValidation{Field: "product_type", Reason: "must be Sheep, Goat or Manure"}
	}
	if w.Breed == "" {
		return ErrDealValidation{Field: "breed", Reason: "required"}
	}
	if len(w.Breed) > maxDealShortField {
		return ErrDealValidation{Field: "breed", Reason: "too long"}
	}
	if w.BuyerName == "" {
		return ErrDealValidation{Field: "buyer_name", Reason: "required"}
	}
	if len(w.BuyerName) > maxDealShortField {
		return ErrDealValidation{Field: "buyer_name", Reason: "too long"}
	}
	if len(w.BuyerPlace) > maxDealShortField {
		return ErrDealValidation{Field: "buyer_place", Reason: "too long"}
	}
	if len(w.Comments) > maxDealLongField {
		return ErrDealValidation{Field: "comments", Reason: "too long"}
	}
	if w.SalesValue <= 0 {
		return ErrDealValidation{Field: "sales_value", Reason: "must be more than zero"}
	}
	for field, v := range map[string]*float64{
		"animal_count":    w.AnimalCount,
		"male_count":      w.MaleCount,
		"female_count":    w.FemaleCount,
		"total_weight_kg": w.TotalWeightKg,
		"advance_amount":  w.AdvanceAmount,
	} {
		if v != nil && *v < 0 {
			return ErrDealValidation{Field: field, Reason: "must not be negative"}
		}
	}
	return nil
}

// Deal page bounds. The ledger is an authored commercial record of well under a hundred rows per
// season; the page is a bounded desktop table, never the whole ledger.
const (
	DefaultDealPageSize = 25
	MaxDealPageSize     = 100
	// MaxDealOffset bounds how deep the ledger can be paged; beyond it the caller should filter.
	// Rejected rather than clamped so a page number never shows the wrong rows.
	MaxDealOffset = 10000
)

// ClampDealPageSize resolves a requested page size to a supported one.
func ClampDealPageSize(requested int) int {
	switch {
	case requested <= 0:
		return DefaultDealPageSize
	case requested > MaxDealPageSize:
		return MaxDealPageSize
	default:
		return requested
	}
}

// ---------------------------------------------------------------------------
// Overview -- the whole-page read contract behind GET /sales/overview.
// ---------------------------------------------------------------------------

// Overview is the entire sales page in one read: whole-filter aggregates, never page-local rows.
type Overview struct {
	Summary          Summary
	Monthly          []MonthlyRow
	PriceBands       []PriceBand
	Buyers           []BuyerRow
	BuyerPipeline    BuyerPipeline
	FPOPipeline      FPOPipeline
	TagRoster        TagRoster
	WeightAudit      WeightAuditSummary
	MarketBenchmarks []MarketBenchmark
}

// Summary is the headline block. Only closed deals count; see BuildDealAggregates.
type Summary struct {
	Revenue            float64
	LiveRevenue        float64
	Deals              int
	Animals            float64
	Sheep              float64
	Goats              float64
	LiveWeightKg       float64
	RealizedPricePerKg float64
	ManureKg           float64
	ManureRevenue      float64
	PeriodFrom         string
	PeriodTo           string
}

// MonthlyRow is one month with at least one closed deal.
type MonthlyRow struct {
	Month         string // YYYY-MM, derived from sale_date
	SheepRevenue  float64
	GoatRevenue   float64
	ManureRevenue float64
	SheepCount    float64
	GoatCount     float64
	ManureKg      float64
}

// PriceBand is realized price per kg for one (live product type, breed).
type PriceBand struct {
	ProductType   string
	Breed         string
	Deals         int
	Animals       float64
	WeightKg      float64
	Revenue       float64
	AvgPricePerKg float64
	MinPricePerKg float64
	MaxPricePerKg float64
}

// BuyerRow is one buyer's closed-deal history.
type BuyerRow struct {
	BuyerName    string
	BuyerPlace   string
	ProductTypes []string
	Deals        int
	Animals      float64
	Revenue      float64
	SharePct     float64
}

// StatusCount is one call-status bucket of a pipeline. An empty stored status is reported under
// the "uncontacted" label by the repository, never dropped.
type StatusCount struct {
	Status string
	Count  int
}

// PlaceCount is one place/district bucket of a pipeline.
type PlaceCount struct {
	Place string
	Count int
}

// BuyerPipeline summarises the buyer-lead demand pipeline.
type BuyerPipeline struct {
	Total     int
	Statuses  []StatusCount
	TopPlaces []PlaceCount
}

// FPOPipeline summarises the FPO demand pipeline. Company-wide: the source carries no farm.
type FPOPipeline struct {
	Total     int
	Statuses  []StatusCount
	Districts []PlaceCount
}

// TagTypeCount is one animal-label bucket of the sold-tag roster.
type TagTypeCount struct {
	Label string
	Count int
}

// TagRoster summarises the per-animal tag evidence behind sold deals.
type TagRoster struct {
	Total      int
	SalesCount int // distinct source_sales_id values the tags point back at
	ByType     []TagTypeCount
}

// WeightAuditSummary buckets the video-vs-book weight gaps. The buckets are disjoint on the
// absolute gap: <= 0.3 kg, (0.3, 1] kg, > 1 kg.
type WeightAuditSummary struct {
	Total      int
	Within03Kg int
	Within1Kg  int
	Over1Kg    int
	MaxGapKg   float64
}

// MarketBenchmark is one comparable market quote.
type MarketBenchmark struct {
	Market           *string
	Category         *string
	Breed            string
	Source           *string
	ExFarmRate       *string
	TransportRate    *string
	LandingCostPerKg *float64
	MarketPricePerKg *float64
}

// UncontactedStatusKey is the reported bucket for a lead whose call_status is NULL -- the lead
// exists but nobody has called yet. The backend owns this vocabulary so every surface words the
// bucket identically.
const UncontactedStatusKey = "uncontacted"

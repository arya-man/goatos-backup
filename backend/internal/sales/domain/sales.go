// Package domain holds the sales ledger's business types and validation.
//
// Sales is a COMMERCIAL module: it records deals with buyers (live animals and manure), the demand
// pipeline behind them, and the evidence panels that back the numbers. It owns its own tables and
// reads NOTHING from the herd, vaccination, or procurement schemas -- the ledger's tag strings are
// sheet-era labels that resolve to no goat, and the terminal sold exit of a live animal stays on
// its authoritative herd workflow.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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

// ProductTypes and Statuses mirror the CHECK constraints on sales_deals.
//
// There is deliberately no Farms list. A deal's farm is the CODE of one of the tenant's active
// parks, authored on Configuration > Items & settings > Parks and read through
// platform/parkcatalog, so a park added there can record sales the moment it is saved. A CBE/CPT
// pair here refused every park after the first two.
var (
	ProductTypes = []string{ProductSheep, ProductGoat, ProductManure}
	Statuses     = []string{StatusDealClosed, StatusDealFailed, StatusInDiscussion, StatusAdvancePaid}
)

// ReasonUnknownFarm is the refusal for a farm no active park carries.
const ReasonUnknownFarm = "must be one of your parks"

// MaxSaleDateDaysAhead is how far past today a sale may be dated: a short horizon so a typo
// cannot date a sale into next year, the same 60 days the web drawer caps at.
const MaxSaleDateDaysAhead = 60

// BreedsByProduct is deliberately GONE (migration 000422). A product's variants are now read from
// the farm's own LIVE vocabularies -- an animal product offers the breeds of its species, a feed
// product the active feed catalogue, an `other` product its own name -- through
// ports.SalesRepository.ListProductVariants, and both the web drawer and the phone form render
// that one answer. A map in Go could only ever describe the three products that used to be
// constants, which is the thing being retired.

// StatusTone is the chip tone every surface renders a deal status in (the web's
// sales_deal_statuses group): ok / dng / info / warn.
func StatusTone(status string) string {
	switch status {
	case StatusDealClosed:
		return "ok"
	case StatusDealFailed:
		return "dng"
	case StatusInDiscussion:
		return "info"
	case StatusAdvancePaid:
		return "warn"
	}
	return ""
}

// IsFarm reports whether raw is exactly one of farms, the tenant's active park codes.
func IsFarm(raw string, farms []string) bool {
	for _, f := range farms {
		if f == raw {
			return true
		}
	}
	return false
}

// IsProductType and IsLiveProduct are deliberately GONE (migration 000422). What the farm sells
// is a tenant registry, so "is this a product" is ProductCatalog.Lookup and "is this alive" is the
// line's own stamped kind -- DealLine.IsLive. A package-level predicate over three constants is
// exactly what made a fourth product a deploy, and it must not grow back.

// IsStatus reports whether raw is a recognised deal status, exactly as stored.
func IsStatus(raw string) bool {
	switch raw {
	case StatusDealClosed, StatusDealFailed, StatusInDiscussion, StatusAdvancePaid:
		return true
	default:
		return false
	}
}

// NormalizeFarmFilter resolves the page's farm query parameter against farms, the tenant's active
// park codes. "" and "all" mean the whole company; a farm value must be exact. ok is false for
// anything else -- the caller rejects rather than silently widening the filter.
func NormalizeFarmFilter(raw string, farms []string) (farm string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	switch {
	case trimmed == "" || strings.EqualFold(trimmed, "all"):
		return "", true
	case IsFarm(trimmed, farms):
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
	// BuyerVendorID is the procurement vendor register row this sale was made to, as an OPAQUE
	// reference -- deliberately not a foreign key (migration 000193, mirroring the 000177
	// precedent). Sales still reads no procurement table; the vendor is picked in admin-web and
	// the id arrives on the write. nil for the 2026-08-17 sheet import, which predates the
	// register. BuyerName stays the snapshot of what the buyer was CALLED at the time of sale.
	BuyerVendorID *string

	ProductType string
	Breed       string

	AnimalCount   *float64
	MaleCount     *float64
	FemaleCount   *float64
	TotalWeightKg *float64

	AdvanceAmount *float64
	SalesValue    float64
	// PaymentReceived is the RUNNING TOTAL of money the buyer has handed over: seeded from the
	// sheet's advance_amount by migration 000227, advanced by each recorded receipt inside the
	// same transaction. Nil when nothing was ever received or recorded.
	PaymentReceived *float64

	Status   string
	Feedback *string
	Comments *string

	CreatedAt string
	UpdatedAt string

	// Payments are the receipts recorded against this deal, oldest first. Sheet history has
	// none: its advance_amount predates the receipts ledger.
	Payments []DealPayment

	// Lines are what was sold, in entry order (migration 000296). ProductType, Breed, the counts,
	// weight and SalesValue above are the ROLLUP of these lines: ProductMixed when the lines
	// disagree. Every deal has at least one line; the pre-000296 history was backfilled as one.
	Lines []DealLine
}

// DealPayment is one amount the buyer actually handed over for one deal.
type DealPayment struct {
	PaymentID    string
	DealID       string
	ReceivedOn   string // YYYY-MM-DD business date
	AmountRupees float64
	Note         string
	CreatedAt    string
}

// PaymentBalance is the money the buyer still owes: sales value minus what has been received.
//
// Never negative -- an overpayment reads as a zero balance, not as the farm owing the buyer
// through this ledger. A sheet deal with no recorded value reads zero owed rather than inventing
// a receivable.
func (d Deal) PaymentBalance() float64 {
	received := 0.0
	if d.PaymentReceived != nil {
		received = *d.PaymentReceived
	}
	balance := d.SalesValue - received
	if balance < 0 {
		balance = 0
	}
	return balance
}

// maxDealPaymentNote bounds the free-text note on one receipt.
const maxDealPaymentNote = 300

// DealPaymentWrite is the record-receipt form: one amount received against one deal.
type DealPaymentWrite struct {
	ReceivedOn   string
	AmountRupees float64
	Note         string
}

// Normalize trims the write before validation, so the rules apply to what will be stored.
func (w DealPaymentWrite) Normalize() DealPaymentWrite {
	out := w
	out.ReceivedOn = strings.TrimSpace(w.ReceivedOn)
	out.Note = strings.Join(strings.Fields(w.Note), " ")
	return out
}

// Validate applies the receipt rules. today is the caller's IST business date: money cannot be
// recorded as received on a day that has not happened.
func (w DealPaymentWrite) Validate(today time.Time) error {
	received, err := time.Parse("2006-01-02", w.ReceivedOn)
	if err != nil {
		return ErrDealValidation{Field: "received_on", Reason: "must be a date like 2026-08-17"}
	}
	if received.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
		return ErrDealValidation{Field: "received_on", Reason: "cannot be in the future"}
	}
	if w.AmountRupees <= 0 {
		return ErrDealValidation{Field: "amount_rupees", Reason: "must be more than zero"}
	}
	if len(w.Note) > maxDealPaymentNote {
		return ErrDealValidation{Field: "note", Reason: "too long"}
	}
	return nil
}

// Animals resolves how many animals this deal moved: animal_count when recorded, otherwise the
// male+female split, and always zero for manure -- manure never contributes animal counts.
func (d Deal) Animals() float64 {
	// Line by line: a mixed deal's manure line must not count, and its rollup product is
	// ProductMixed, which the single-product branch below could not classify.
	if len(d.Lines) > 0 {
		total := 0.0
		for _, l := range d.Lines {
			total += l.Animals()
		}
		return total
	}
	if BuiltinKind(d.ProductType) != KindAnimal {
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
	SaleDate string
	Farm     string
	// Lines are what was sold (maintainer decision 2026-09-12): one per product/breed, each with
	// its own counts, weight and value. Normalize fills them from the legacy single-product
	// fields below when a pre-000296 client sends none, and Validate refuses a sale with none.
	Lines []DealLineWrite
	// StockShortfallAcknowledged is the desk having seen what the store thinks it holds and said
	// the sale is right anyway (maintainer decision 2026-09-23). It is only ever true because a
	// person ticked it after being shown the balance; a client that sets it by default turns a
	// confirmation into no confirmation at all.
	StockShortfallAcknowledged bool
	// ProductType, Breed, the counts, TotalWeightKg and SalesValue are the LEGACY single-line
	// body. After Normalize they hold the ROLLUP of Lines (see RollupLines), which is what the
	// deal row stores; a client that sends both lines and these gets its own figures replaced by
	// the rollup, so the deal can never disagree with its lines.
	ProductType string
	Breed       string
	// legacyLines records that Normalize built Lines from the single-product fields, so Validate
	// names the legacy field ("breed", not "lines[1].breed") -- installed phones key their
	// per-field error display on those names.
	legacyLines bool
	BuyerName   string
	BuyerPlace  string
	// BuyerVendorID is REQUIRED on every deal recorded in the app: the farm does not sell to a
	// name typed into a box, it sells to a counterparty in the vendor register. The column is
	// nullable in storage only so the imported sheet history, which predates the register, stays
	// loadable -- see migration 000193.
	BuyerVendorID string
	AnimalCount   *float64
	MaleCount     *float64
	FemaleCount   *float64
	TotalWeightKg *float64
	SalesValue    float64
	AdvanceAmount *float64
	Comments      string
	// Status is OPTIONAL: blank records the sheet's default, Deal Closed. Naming one lets the desk
	// record an EXPECTED sale -- an advance received today for animals leaving on a future date is
	// an Advance Paid deal, not a closed one, and only Deal Closed counts toward revenue.
	Status string
}

// ErrDealFailedIsFinal refuses moving a Deal Failed sale to any other status (maintainer decision
// 2026-09-25). A failed sale's workflow is cancelled and its tagged animals are released back into
// the herd; selling again is a NEW sale, never a revived one.
var ErrDealFailedIsFinal = errors.New("sales: a failed deal is final")

// StatusChangeAllowed reports whether a deal in status `from` may be set to `to`. Deal Failed is
// final: it may only be "set" to itself (a no-op). Every other status may move anywhere.
func StatusChangeAllowed(from, to string) bool {
	return from != StatusDealFailed || to == StatusDealFailed
}

// NextStatuses is the status picker a deal in status `current` offers: every status for a live
// deal, NOTHING for a failed one -- the web and phone status editors hide themselves on an empty
// list rather than offer a change the server refuses.
func NextStatuses(current string) []string {
	if current == StatusDealFailed {
		return []string{}
	}
	return append([]string(nil), Statuses...)
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
// Normalize trims the body and resolves every line's product against the tenant's ACTIVE registry
// (migration 000422), stamping each line with the code, kind and unit it is being sold under.
//
// The catalog is a PARAMETER rather than something this package reads, for the reason the vendor
// register is: the domain owns the rules, never a table. The caller hands it the farm's answer to
// "what do we sell", and the same answer is re-resolved inside the writing transaction so a
// product archived between the form opening and the save landing cannot slip through.
func (w DealWrite) Normalize(cat ProductCatalog) DealWrite {
	out := w
	collapse := func(v string) string { return strings.Join(strings.Fields(v), " ") }
	out.SaleDate = strings.TrimSpace(w.SaleDate)
	out.Farm = strings.TrimSpace(w.Farm)
	out.ProductType = strings.TrimSpace(w.ProductType)
	out.Breed = collapse(w.Breed)
	if len(w.Lines) == 0 {
		out.Lines = out.linesFromLegacy()
		for i := range out.Lines {
			out.Lines[i] = out.Lines[i].normalize(cat)
		}
		out.legacyLines = true
	} else {
		out.Lines = make([]DealLineWrite, 0, len(w.Lines))
		for _, l := range w.Lines {
			out.Lines = append(out.Lines, l.normalize(cat))
		}
	}
	// The deal row is the rollup of its lines, never a figure of its own. Applied only when there
	// are lines to roll up, so an empty body still fails Validate on the fields it named.
	if len(out.Lines) > 0 {
		r := RollupLines(out.Lines)
		out.ProductType, out.Breed = r.ProductType, r.Breed
		out.AnimalCount, out.MaleCount, out.FemaleCount = r.AnimalCount, r.MaleCount, r.FemaleCount
		out.TotalWeightKg, out.SalesValue = r.TotalWeightKg, r.SalesValue
	}
	out.BuyerName = collapse(w.BuyerName)
	out.BuyerPlace = collapse(w.BuyerPlace)
	out.BuyerVendorID = strings.TrimSpace(w.BuyerVendorID)
	out.Comments = strings.TrimSpace(w.Comments)
	out.Status = strings.TrimSpace(w.Status)
	// Canonicalize the two-word statuses case-insensitively, the same way the feed ledger treats
	// its payment words: "advance paid" stores as the sheet's "Advance Paid". An unrecognised
	// value still fails Validate rather than being rewritten -- a silently defaulted status is a
	// deal state nobody entered.
	for _, known := range Statuses {
		if strings.EqualFold(out.Status, known) {
			out.Status = known
			break
		}
	}
	return out
}

// Validate enforces the enums, required fields and bounds, returning the FIRST failure.
//
// The enums are validate-or-reject, never silently defaulted: a farm the CHECK constraint would
// refuse, or a product the farm's registry does not carry, must fail here with a field-specific
// message, not be rewritten to a value the caller never entered.
func (w DealWrite) Validate(cat ProductCatalog, farms []string) error {
	if w.SaleDate == "" {
		return ErrDealValidation{Field: "sale_date", Reason: "required"}
	}
	if _, err := time.Parse("2006-01-02", w.SaleDate); err != nil {
		return ErrDealValidation{Field: "sale_date", Reason: "must be a date like 2026-08-17"}
	}
	if !IsFarm(w.Farm, farms) {
		return ErrDealValidation{Field: "farm", Reason: ReasonUnknownFarm}
	}
	if len(w.Lines) == 0 {
		return ErrDealValidation{Field: "lines", Reason: "add at least one product line"}
	}
	if len(w.Lines) > MaxDealLines {
		return ErrDealValidation{Field: "lines", Reason: fmt.Sprintf("at most %d lines per sale", MaxDealLines)}
	}
	for i, l := range w.Lines {
		lineNo := i + 1
		if w.legacyLines {
			lineNo = 0
		}
		if err := l.validate(lineNo, cat); err != nil {
			return err
		}
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
	// Validate-or-reject, never silently defaulted: a sale with no vendor is refused rather than
	// recorded against nobody. Only the SHAPE is checked here -- the domain must not read the
	// procurement register (the 000173 lock), so that the id names a real vendor is the caller's
	// guarantee, exactly as 000177 validates its sales_deal_id against the deal read.
	if w.BuyerVendorID == "" {
		return ErrDealValidation{Field: "buyer_vendor_id", Reason: "required -- pick the buyer from the vendor register"}
	}
	if _, err := uuid.Parse(w.BuyerVendorID); err != nil {
		return ErrDealValidation{Field: "buyer_vendor_id", Reason: "must be a vendor from the register"}
	}
	if len(w.Comments) > maxDealLongField {
		return ErrDealValidation{Field: "comments", Reason: "too long"}
	}
	if w.SalesValue <= 0 {
		return ErrDealValidation{Field: "sales_value", Reason: "must be more than zero"}
	}
	if w.Status != "" && !IsStatus(w.Status) {
		return ErrDealValidation{Field: "status", Reason: "must be Deal Closed, Deal Failed, In Discussion or Advance Paid"}
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
	// The advance is money the buyer has ALREADY handed over for THIS sale, and it becomes the
	// sale's first receipt. More than the sale is worth is money the ledger cannot explain -- in
	// practice a typo or a second sale's money -- so it is refused rather than recorded.
	if w.AdvanceAmount != nil && *w.AdvanceAmount > w.SalesValue+0.005 {
		return ErrDealValidation{Field: "advance_amount", Reason: "cannot be more than the sale value"}
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
	// SoldWeightBands (maintainer decisions 2026-09-08 and 2026-09-21): every animal sold on a
	// closed deal, by weight -- measured at tagging where it was, spread from the load's own
	// recorded weight where it was not, and from migration 000381's recorded assumption for the
	// seven old animals neither could reach. Whole register, no window: the Sales page carries no
	// date filter, and the maintainer asked for all of it.
	SoldWeightBands SoldWeightBands
	// FarmValuation is the live-herd valuation block behind the "Farm Value under Sales" cards.
	// It is deliberately separate from closed-deal revenue: these animals are current inventory,
	// not sold rows.
	FarmValuation FarmValuation
}

// FarmValuation is a whole-filter valuation of the current live herd. Bucket weights and prices
// mirror the Sales target card from 2026-09-10; animals are filed into exactly one bucket.
type FarmValuation struct {
	TotalValueRupees float64
	TotalMeatKg      float64
	TotalAnimals     int
	ValuedAnimals    int
	ExcludedAnimals  int
	NotValued        []FarmValuationNotValued
	Buckets          []FarmValuationBucket
}

// FarmValuationNotValued names a live-herd slice that is counted in current inventory but not
// priced by the maintainer's valuation formula.
type FarmValuationNotValued struct {
	Label string
	Count int
}

// FarmValuationBucket is one row of the valuation formula.
type FarmValuationBucket struct {
	Bucket         string
	Label          string
	AnimalCount    int
	WeightKg       float64
	PricePerKg     float64
	MeatKg         float64
	ValueRupees    float64
	ActualWeight   bool
	WeighedAnimals int
	// The bucket's animals by recorded sex (maintainer request 2026-09-11, for the fattening
	// and kid cards). The three are DISJOINT and sum to AnimalCount: an animal whose sex is not
	// recorded as male or female counts as missing, never as either.
	MaleCount       int
	FemaleCount     int
	SexMissingCount int
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
	// Feed sold off the store, and anything else the farm sells that is neither alive nor feed
	// (migration 000422). With LiveRevenue and ManureRevenue these are DISJOINT and sum to
	// Revenue; FeedKg is kilograms of feed, which is a quantity sold and not a live weight.
	FeedKg       float64
	FeedRevenue  float64
	OtherKg      float64
	OtherRevenue float64
	PeriodFrom   string
	PeriodTo     string
}

// MonthlyRow is one month with at least one closed deal.
type MonthlyRow struct {
	Revenue       float64
	LiveRevenue   float64
	Animals       float64
	Month         string // YYYY-MM, derived from sale_date
	SheepRevenue  float64
	GoatRevenue   float64
	ManureRevenue float64
	SheepCount    float64
	GoatCount     float64
	ManureKg      float64
	FeedRevenue   float64
	FeedKg        float64
	OtherRevenue  float64
	OtherKg       float64
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

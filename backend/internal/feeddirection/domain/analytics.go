package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Feed Analytics read shapes — the windowed rollup behind /feed-analytics/*.
//
// Everything here aggregates the FROZEN sheet (feed_direction_issue_rows), so the
// numbers are DIRECTED kg — what the sheet told the farm to feed — never measured
// consumption. Copy on every surface must say "directed"; the completions tables
// carry proofs, not weights, so there is no consumed-kg source to aggregate.
//
// Two grain rules carried from the issue-row schema, restated here because every
// consumer of these types depends on them:
//
//   - quantity_kg is NULL IFF BLOCKED. Aggregates SUM resolved cells and skip
//     blocked ones (SQL SUM ignores NULL); nothing may coerce a blocked cell to
//     zero. Blocked counts are deliberately NOT reported on this page
//     (maintainer decision 2026-08-17) — config gaps surface on the Feed
//     Direction screen, not in analytics.
//   - head_count repeats per (session, feed item) CELL of the same pen-grain. A
//     head-day counts each pen-grain ONCE per feed day, so head-day sums collapse
//     cells to the grain (shed, partition, shed_tag, breed) first. BOTH workflows
//     count (maintainer decision 2026-08-19): experiment pens are real animals
//     eating real feed, so directed kg, animals-fed and the whole-herd per-head
//     figure include them. head_count_informational still means the kg was
//     authored absolute, never derived from the head count.

// DirectedAnalyticsQuery bounds one rollup read. Dates are business dates
// (Asia/Kolkata), inclusive on both ends. ParkIDs empty means unrestricted
// (tenant-wide caller); a park-scoped caller's authorized set — or the single
// selected park — arrives here so the rollup can never leak another park's feed.
type DirectedAnalyticsQuery struct {
	ParkIDs  []uuid.UUID
	DateFrom time.Time
	DateTo   time.Time
	// WastageDay selects the single business day the experiment read's per-pen
	// wastage table describes. Zero means the caller's handler default (today,
	// Asia/Kolkata) — wastage is collected live during the feed day. Read only
	// by the experiment analytics; the directed/execution/stock reads ignore it.
	WastageDay time.Time
	// Sections narrows the EXECUTION read to the arms the caller will actually render. Empty means
	// every arm, so an existing caller is unaffected.
	//
	// It exists because the execution payload is six queries and a page can need one array from a
	// second, differently-scoped read: the mismatch table pins a day, the target-vs-actual section
	// pins its own day and park. Fetching the whole payload for each meant three concurrent reads
	// of eighteen queries to use four arrays, and the last query of the third read hit the repo
	// deadline -- the page then showed its "rollup read failed" card while every individual query
	// was fast. Narrowing the fetch is the fix; widening the deadline would only move the failure.
	Sections []ExecutionSection
	// PackingVarianceLimit / PackingVarianceOffset page the mismatch list. Zero limit means
	// DefaultPackingVariancePageSize.
	PackingVarianceLimit  int
	PackingVarianceOffset int
	// PackingVarianceParkLabel and PackingVarianceFeedItemKey narrow the mismatch
	// list before paging. They are display-table filters, not scope controls:
	// tenant and authorized park scope are still enforced by ParkIDs.
	PackingVarianceParkLabel   string
	PackingVarianceFeedItemKey string
}

// Mismatch-list paging. The window is already capped at 92 days and the list carries only bags
// PAST tolerance, so the result set is bounded; the offset cap keeps a hand-typed page number from
// walking a deep scan, and it is REJECTED rather than clamped so a caller asking for page 400 is
// told the page does not exist instead of being handed page 1's rows under page 400's heading.
const (
	DefaultPackingVariancePageSize = 25
	MaxPackingVariancePageSize     = 100
	MaxPackingVarianceOffset       = 5000
)

// ErrPackingVariancePageOutOfRange is returned for a limit or offset outside the bounds above.
var ErrPackingVariancePageOutOfRange = errors.New("feeddirection: packing variance page is out of range")

// NormalisePackingVariancePage validates the requested page and fills the default size. A PRESENT
// but out-of-range value FAILS; only an ABSENT limit takes the default.
func NormalisePackingVariancePage(limit, offset int) (int, int, error) {
	if limit < 0 || limit > MaxPackingVariancePageSize {
		return 0, 0, ErrPackingVariancePageOutOfRange
	}
	if offset < 0 || offset > MaxPackingVarianceOffset {
		return 0, 0, ErrPackingVariancePageOutOfRange
	}
	if limit == 0 {
		limit = DefaultPackingVariancePageSize
	}
	return limit, offset, nil
}

// ExecutionSection names one arm of the execution payload.
type ExecutionSection string

const (
	// ExecutionSectionDays is the per-day status matrix, transport counts and verify latency.
	ExecutionSectionDays ExecutionSection = "days"
	// ExecutionSectionPackingVariance is the intended-vs-entered mismatch list.
	ExecutionSectionPackingVariance ExecutionSection = "packing_variance"
	// ExecutionSectionConsumption is the target-vs-actual shed table and its trend.
	ExecutionSectionConsumption ExecutionSection = "consumption"
)

// ExecutionSections lists every arm, in payload order.
var ExecutionSections = []ExecutionSection{
	ExecutionSectionDays,
	ExecutionSectionPackingVariance,
	ExecutionSectionConsumption,
}

// Wants reports whether the query asked for an arm. An empty selection wants everything.
func (q DirectedAnalyticsQuery) Wants(section ExecutionSection) bool {
	if len(q.Sections) == 0 {
		return true
	}
	for _, s := range q.Sections {
		if s == section {
			return true
		}
	}
	return false
}

// ParseExecutionSection maps a caller's string to an arm. An unknown name is REJECTED rather than
// ignored: silently dropping it would serve a payload missing the array the caller asked for, and
// the client would render an empty table as though the farm had no data.
func ParseExecutionSection(raw string) (ExecutionSection, bool) {
	for _, s := range ExecutionSections {
		if string(s) == raw {
			return s, true
		}
	}
	return "", false
}

// MaxAnalyticsWindowDays caps the window: three months of daily points is the
// widest range the page offers, and the cap keeps the aggregate bounded no
// matter what a caller passes.
const MaxAnalyticsWindowDays = 92

// DirectedDayTotal is one feed day of the normal workflow across every feed item.
type DirectedDayTotal struct {
	FeedDay string
	// DirectedKg is the summed resolved quantity as a decimal string ("0" when
	// every cell that day was blocked or the day authored zero).
	DirectedKg string
	// HeadDays is the number of animals the sheet fed that day: each pen-grain's
	// head_count counted once, experiment pens excluded.
	HeadDays int64
	// PerHeadGrams is DirectedKg×1000 ÷ HeadDays, empty when HeadDays is zero.
	PerHeadGrams string
}

// DirectedDayItem is one (feed day, feed item) of the normal workflow.
type DirectedDayItem struct {
	FeedDay       string
	FeedItemLabel string
	FeedItemKey   string
	DirectedKg    string
	// HeadDays counts the animals in pens whose sheet carried THIS item that day
	// (pen-grain counted once). The per-head figure divides by these heads, not
	// the whole day's, so an item fed to one cohort reads as that cohort's ration.
	HeadDays     int64
	PerHeadGrams string
}

// DirectedAnalytics is the /feed-analytics/directed payload: day totals plus the
// per-item series, both ordered by feed day ascending.
type DirectedAnalytics struct {
	Days  []DirectedDayTotal
	Items []DirectedDayItem
}

// ClampAnalyticsWindow normalises a query window: swaps inverted ends and caps
// the span at MaxAnalyticsWindowDays (keeping the most recent days, because every
// chart on the page anchors to "up to yesterday").
func ClampAnalyticsWindow(from, to time.Time) (time.Time, time.Time) {
	if to.Before(from) {
		from, to = to, from
	}
	if int(to.Sub(from).Hours()/24) >= MaxAnalyticsWindowDays {
		from = to.AddDate(0, 0, -(MaxAnalyticsWindowDays - 1))
	}
	return from, to
}

// ---------------------------------------------------------------------------
// Execution analytics: proof/verdict adherence per day. STATUS COUNTS ONLY —
// completions carry proofs, never kg, so execution can be judged on whether the
// work was proved and verified, not on quantity.
// ---------------------------------------------------------------------------

// ExecutionDay is one business date of completion statuses across the three
// proof-gated stages. Packing and distribution rows bucket by their pen-session
// target_date; transport by its business_date. Latency buckets by the IST date
// the verdict landed.
type ExecutionDay struct {
	Date string
	// Packing pen-session completions by verification outcome.
	PackingVerified int64
	PackingAwaiting int64
	PackingRework   int64
	// Distribution pen-session completions by verification outcome.
	DistributionVerified int64
	DistributionAwaiting int64
	DistributionRework   int64
	// Transport shed tasks by state.
	TransportCompleted       int64
	TransportOpen            int64
	TransportAwaitingVerdict int64
	TransportRework          int64
	// MedianVerifyLatencyMinutes is the median submit→verdict latency of packing
	// and distribution verdicts landing that day; nil when none landed.
	MedianVerifyLatencyMinutes *int64
}

// PackingVarianceToleranceKg is how far the verifier's blind reading may sit from the directed
// quantity before the pen-session-item pops on the leadership execution view (maintainer decision
// 2026-08-21, second same-day decision SUPERSEDING the initial any-mismatch rule): a scale read off
// a video is honest to a couple hundred grams, so differences of 0.2 kg or less are treated as the
// same number. Strictly greater-than: exactly 0.2 kg stays quiet.
const PackingVarianceToleranceKg = 0.2

// PackingVarianceRow is one MISMATCH between what the frozen sheet directed a pen-session to pack
// for one feed item and what the verifier read off the packing video (maintainer decision
// 2026-08-21: blind per-item entry -- she never sees the planned figure, so this comparison lives
// ONLY on the leadership execution view, never on any verifier surface). A row exists only when
// |entered - planned| exceeds PackingVarianceToleranceKg.
type PackingVarianceRow struct {
	FeedDay string
	// PackingDay is FeedDay - 1: the day the bag was actually weighed out. This table is about
	// PACKING, so it is the day the reader recognises; FeedDay stays in the payload because the
	// quantities belong to that feed day's sheet.
	PackingDay string
	ParkLabel  string
	ShedID     string
	ShedLabel  string
	// PartitionLabel is the pen ("2", "Part 3"), empty for an undivided shed.
	PartitionLabel string
	// OperationalLocationDisplay is the oploc-composed shed+pen label, same as every surface.
	OperationalLocationDisplay string
	SessionNo                  int32
	// SessionLabel is the sheet's session name ("Morning"); empty when the frozen sheet row is gone.
	SessionLabel  string
	Workflow      string
	FeedItemKey   string
	FeedItemLabel string
	// BreedLabel and AgeGroup describe the bag's cohort, resolved agree-or-go-bare: a bag whose
	// sheet rows carry more than one breed, or straddle kid and adult, reports MixedCohortLabel
	// rather than naming one, which would be a cohort nobody recorded. Both are empty when the
	// frozen sheet row behind the reading is gone.
	BreedLabel string
	AgeGroup   string
	// PlannedKg is the frozen sheet's summed quantity for this (pen, session, item) as a decimal
	// string; "" when the sheet carried no resolved quantity (blocked cell or missing row) -- blank
	// and zero are never conflated.
	PlannedKg string
	// VerifiedKg is the verifier's entered reading. "0" is a real observation.
	VerifiedKg string
	// VarianceKg is VerifiedKg minus the resolved planned quantity (0 when unresolved), signed.
	VarianceKg string
	// BeyondTolerance marks a bag whose difference exceeds PackingVarianceToleranceKg. Every
	// measured bag is listed now, so this is what separates a real discrepancy from a scale read
	// that is honest to a couple hundred grams.
	BeyondTolerance bool
}

// MixedCohortLabel is what a bag reports when its sheet rows disagree on breed or age group. It
// is a real answer -- the pen holds a mix -- never a missing value.
const MixedCohortLabel = "Mixed"

// FeedConsumptionTrendDay is one day of the packed-vs-given trend under the mismatch table:
// everything the sheet directed that day against everything a verifier measured.
type FeedConsumptionTrendDay struct {
	FeedDay string
	// PackingDay is FeedDay - 1, so the trend's axis matches the table above it.
	PackingDay string
	TargetKg   string
	// ActualKg is EMPTY on a day with no packing readings at all, so the chart draws a GAP rather
	// than a plunge to zero that would read as "the farm fed nothing that day".
	ActualKg     string
	VarianceRows int64
	ComparedRows int64
}

// ExecutionAnalytics is the /feed-analytics/execution payload.
type ExecutionAnalytics struct {
	Days []ExecutionDay
	// PackingVariance lists every intended-vs-entered packing mismatch in the window, newest feed
	// day first. Leadership-only by page contract; the verifier lens never receives this payload.
	PackingVariance []PackingVarianceRow
	// PackingVarianceHasMore reports whether a further page exists beyond the rows returned. The
	// list is a PAGE; every other figure on the screen stays a whole-window aggregate.
	PackingVarianceHasMore bool
	ConsumptionTrend       []FeedConsumptionTrendDay
}

// ---------------------------------------------------------------------------
// Experiment analytics: the experiment pens' authored kg BY FEED ITEM per day
// (maintainer decision 2026-08-19, replacing the earlier per-arm series: the
// farm reads this screen in feed items — masoor, bhusa — never in trial-arm
// labels, and the arms table is retired outright).
// ---------------------------------------------------------------------------

// ExperimentDayItem is one (feed day, feed item) of the experiment workflow.
// Kg is the authored absolute total across every experiment pen that day —
// experiment rations are authored per pen, never multiplied by head count.
type ExperimentDayItem struct {
	FeedDay       string
	FeedItemLabel string
	FeedItemKey   string
	Kg            string
}

// ExperimentWastagePen is ONE experiment pen's leftover-feed state for the
// selected wastage day (Feed Wastage, maintainer decision 2026-08-18). The pen
// list is DERIVED from the day's experiment sheet — the same rows packing and
// the operator worklist read — LEFT-joined to the pen's wastage completion, so
// a pen with no video yet still lists, honestly, as not submitted.
type ExperimentWastagePen struct {
	ShedID string
	// ParkLabel disambiguates the pen under a tenant-wide read: shed names
	// repeat across parks (two Castros), so the pen alone is ambiguous.
	ParkLabel      string
	ShedLabel      string
	PartitionLabel string
	// OperationalLocationDisplay is the backend-composed shed+pen label
	// ("Castro 1", "Godel 2 - Part 1"), built with platform/oploc so every
	// surface renders the pen the same way.
	OperationalLocationDisplay string
	// LifecycleStatus: "" when no video was submitted yet, else the completion's
	// verification-lifecycle bucket (pending_verification | rework | completed).
	LifecycleStatus string
	// WastageKg is the VERIFIER's recorded leftover weight in kg, "" until she
	// records one — "0" is a real measurement (an empty trough), so blank and
	// zero are never conflated.
	WastageKg string
}

// ExperimentAnalytics is the /feed-analytics/experiment payload.
type ExperimentAnalytics struct {
	Items []ExperimentDayItem
	// WastageDay echoes the business date the pens below describe.
	WastageDay string
	// WastagePens is the per-pen leftover-feed table for WastageDay, same park
	// scope as Items. Experiment-only by definition — wastage exists on no
	// other workflow.
	WastagePens []ExperimentWastagePen
}

// ---------------------------------------------------------------------------
// Stock & expenditure analytics, backed by the bootstrapped feed_purchases
// ledger (migration 000173). Stock depletes at SHEET LOCK: balance =
// (purchased − consumed-at-import) − directed kg of LOCKED sheets from the
// bootstrap cutoff onward. Both workflows deplete — experiment feed leaves the
// same store.
// ---------------------------------------------------------------------------

// StockItem is one FARM's current stock position for one feed item. Each farm
// keeps its own physical store (maintainer decision 2026-08-21), so there is
// deliberately no tenant-wide combined balance — a number nobody's store holds.
type StockItem struct {
	FarmLabel     string
	FeedItemLabel string
	FeedItemKey   string
	// BalanceKg may go negative when directed kg overruns the ledger — shown as
	// is, never clamped: a negative balance says the ledger is missing a load.
	BalanceKg string
	// AvgDailyKg averages the item's directed kg over its 3 most recent locked
	// feed days — a short window so a ration-regime change (e.g. animals moving
	// onto a new concentrate) moves days-left immediately, matching the farm's
	// legacy stock sheet (maintainer decision 2026-08-21); empty when the item was never directed.
	AvgDailyKg string
	// DaysLeft is BalanceKg ÷ AvgDailyKg, nil when the item has no recent
	// directed days to divide by.
	DaysLeft      *int64
	LatestBatchNo int64
	// LowStock flags fewer than LowStockDays days left.
	LowStock bool
}

// LowStockDays mirrors the legacy sheet's warning threshold: the RED CARD on the Stock tab, which
// means "nearly out".
const LowStockDays = 5

// LowStockNotifyDays is the DAILY ALERT horizon (maintainer decision 2026-08-24), deliberately
// wider than LowStockDays: leadership is told a week out so a purchase order can still be raised,
// while the card keeps meaning nearly out. Changing one must not silently change the other.
const LowStockNotifyDays = 7

// LowStockFeed is one farm's feed that runs out inside LowStockNotifyDays. Every field the alert
// names is here, because a notification that cannot say WHICH farm, WHICH feed and HOW LONG is the
// abstract-count defect the notification-specificity rule exists to stop.
type LowStockFeed struct {
	// ParkID is empty when the purchase ledger never resolved the farm to a park; the alert then
	// names the farm label only rather than deep-linking somewhere it cannot reach.
	ParkID        string
	FarmLabel     string
	FeedItemLabel string
	FeedItemKey   string
	BalanceKg     string
	AvgDailyKg    string
	DaysLeft      int64
}

// MeshaConcentrateStockKeys is the fixed set of in-house Mesha concentrate
// feeds the per-farm purchase/consumption table covers (maintainer decision
// 2026-08-21: exactly these four, not every ledger item). Keys are
// feed_config_norm outputs of the ledger's feed_item_label values.
var MeshaConcentrateStockKeys = []string{
	"mesha_adult_concentrate_goat",
	"mesha_adult_concentrate_sheep",
	"mesha_kids_goat_concentrate",
	"mesha_kids_sheep_concentrate",
}

// StockFarmItem is one (farm, Mesha concentrate) row of the per-farm
// purchase/consumption table on the Stock tab. Consumption figures come from
// LOCKED GoatOS feed sheets only, so a bootstrapped item's consumption start
// is the ledger cutoff, not the sheet era before it.
type StockFarmItem struct {
	FarmLabel     string
	FeedItemLabel string
	FeedItemKey   string
	// FirstPurchaseDate is the earliest load's purchase date for this farm.
	FirstPurchaseDate string
	// FirstDirectedDay is the first locked feed day the item was directed at
	// this farm; empty when never directed.
	FirstDirectedDay string
	// AvgDailyKg averages the farm's directed kg for the item over its 3 most
	// recent locked feed days (same semantics as StockItem.AvgDailyKg, scoped
	// to the farm); empty when never directed.
	AvgDailyKg string
	// WeeklyRequiredKg is AvgDailyKg multiplied by 7, showing the feed needed
	// for one week at the current farm/item consumption rate.
	WeeklyRequiredKg string
	// Last load (highest purchase_date, then batch_no) details.
	LastLoadBatchNo    int64
	LastLoadDate       string
	LastLoadQuantityKg string
	LastLoadVendor     string
	LastLoadTotalCost  string
	LastLoadPerKgCost  string
	// LedgerStockKg is the canonical current stock from the purchase ledger:
	// purchased minus consumed-at-import minus locked-sheet directed kg.
	LedgerStockKg string
}

// StockForecastDays is the forward window the requirement table answers for:
// the next seven days of feeding, the horizon the farm buys against.
const StockForecastDays = 7

// StockForecastItem is one (farm, feed item) row of the next-7-days
// requirement table (maintainer decision 2026-08-23). It answers the two
// questions leadership asks before a purchase run: how much of this feed does
// this farm need for the coming week at the CURRENT feeding rate, and what
// does that cost.
//
// Its grain is (park, feed item) driven by CONSUMPTION, not by the purchase
// ledger: every feed the farm actually feeds gets a row, including feeds
// GoatOS does not direct through sheets (UHT Milk, feed_external_consumption)
// and feeds with no purchase history at all. That is deliberately wider than
// StockFarmItem's four Mesha concentrates — a requirement table that silently
// omitted a feed would under-order it.
//
// Money is present here by explicit maintainer decision 2026-08-23, which
// supersedes the 2026-08-21 "quantities and timing, not money" scope recorded
// on StockFarmItem FOR THIS TABLE ONLY. StockFarmItem's own columns are
// unchanged.
type StockForecastItem struct {
	FarmLabel     string
	FeedItemLabel string
	FeedItemKey   string
	// AvgDailyKg is the same short 3-locked-day average every other figure on
	// this page uses, so days-left and the requirement move together; empty
	// when the item has no recent consumption to average.
	AvgDailyKg string
	// RequiredKg is AvgDailyKg x StockForecastDays. Empty when AvgDailyKg is.
	RequiredKg string
	// StockKg is the ledger balance for this farm and item, empty when the
	// purchase ledger carries no load for it (a fed-but-never-purchased feed
	// still gets a requirement, just no balance to compare it against).
	StockKg string
	// ShortfallKg is RequiredKg - StockKg floored at zero: what must be bought
	// to feed the week. Empty when either input is.
	ShortfallKg string
	// PerKgCost is the farm's most recent load rate for the item, the same
	// pricing rule the expenditure series uses, read forward with no date
	// bound because this is a forecast. Empty when never purchased at this
	// farm.
	PerKgCost string
	// RequiredCost is RequiredKg x PerKgCost — the week's feed bill at the
	// current rate. Empty without a rate.
	RequiredCost string
}

// ExpenditureDay is one feed day's spend: directed kg priced at each item's
// most recent load rate on or before that day.
type ExpenditureDay struct {
	FeedDay string
	Rupees  string
}

// SpendSummary totals the expenditure over the standing periods leadership
// asks about, independent of the page's chart window. Every bucket ends at
// YESTERDAY (today's sheet is still being executed) and is priced the same way
// as the daily series. Rupee strings, "0" when nothing priced.
type SpendSummary struct {
	// ThisWeek is Monday of the current IST week through yesterday.
	ThisWeek string
	// ThisMonth is the 1st of the current IST month through yesterday.
	ThisMonth string
	// ThreeMonths is the rolling 92 days through yesterday.
	ThreeMonths string
	// ThisYear is Jan 1 of the current IST year through yesterday.
	ThisYear string
}

// StockAnalytics is the /feed-analytics/stock payload.
type StockAnalytics struct {
	Items []StockItem
	// FarmItems is the per-farm Mesha-concentrate purchase/consumption table
	// (MeshaConcentrateStockKeys only), ordered by feed item then farm.
	FarmItems []StockFarmItem
	// Forecast is the next-7-days requirement/cost table at (farm, feed item)
	// grain over every fed feed, ordered by farm then item.
	Forecast    []StockForecastItem
	Expenditure []ExpenditureDay
	Spend       SpendSummary
}

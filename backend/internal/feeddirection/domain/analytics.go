package domain

import (
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

// ExecutionAnalytics is the /feed-analytics/execution payload.
type ExecutionAnalytics struct {
	Days []ExecutionDay
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
	// AvgDailyKg averages the item's directed kg over its 7 most recent locked
	// feed days; empty when the item was never directed.
	AvgDailyKg string
	// DaysLeft is BalanceKg ÷ AvgDailyKg, nil when the item has no recent
	// directed days to divide by.
	DaysLeft      *int64
	LatestBatchNo int64
	// LowStock flags fewer than LowStockDays days left.
	LowStock bool
}

// LowStockDays mirrors the legacy sheet's warning threshold.
const LowStockDays = 5

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
	// AvgDailyKg averages the farm's directed kg for the item over its 7 most
	// recent locked feed days (same semantics as StockItem.AvgDailyKg, scoped
	// to the farm); empty when never directed.
	AvgDailyKg string
	// Last load (highest purchase_date, then batch_no) details. Cost and
	// payment state are deliberately absent (maintainer decision 2026-08-21):
	// this table is about quantities and timing, not money.
	LastLoadBatchNo    int64
	LastLoadDate       string
	LastLoadQuantityKg string
	LastLoadVendor     string
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
	FarmItems   []StockFarmItem
	Expenditure []ExpenditureDay
	Spend       SpendSummary
}

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
//     cells to the grain (shed, partition, shed_tag, breed) first. Experiment rows
//     (head_count_informational) never contribute to head-day or per-head math.

// DirectedAnalyticsQuery bounds one rollup read. Dates are business dates
// (Asia/Kolkata), inclusive on both ends. ParkIDs empty means unrestricted
// (tenant-wide caller); a park-scoped caller's authorized set — or the single
// selected park — arrives here so the rollup can never leak another park's feed.
type DirectedAnalyticsQuery struct {
	ParkIDs  []uuid.UUID
	DateFrom time.Time
	DateTo   time.Time
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

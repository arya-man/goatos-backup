package domain

// HARD-CODED, and deliberately so (maintainer instruction 2026-09-06).
//
// CBE's Concentrate burn rate is pinned at 55 kg/day. The computed rate -- the
// average of the three most recent locked feed days the item appeared on --
// reads 64.9 kg/day, and the maintainer's own figure for what that store
// actually issues is 55. This is not a claim that the computation is broken; it
// is one farm's one feed being answered from the maintainer's knowledge instead
// of from the sheet, until whatever makes the two disagree is found.
//
// Consequences to know before touching it:
//
//   - It overrides the rate for BOTH the stock card and the daily low-stock
//     push, from one table, because a push and a tab quoting different rates for
//     the same feed is the cross-surface disagreement AGENTS.md bans.
//   - The rate it pins is also the rate the card DISPLAYS, so the screen never
//     shows a days-left that its own kg/day cannot reproduce.
//   - It is keyed on (farm, feed) and reaches nothing else: every other farm-feed
//     pair still divides by its own computed rate.
//   - It does NOT touch consumption anywhere else -- not the feed sheet, not the
//     expenditure series, not the per-farm table, not the ledger. Only the
//     divisor behind "days left".
//
// Like the split-concentrate fold beside it, an EMPTY table is the behaviour
// without it, so removing this file and its query parameters is the whole
// revert.
type StockRateOverrideRow struct {
	FarmLabel   string
	FeedItemKey string
	// KgPerDay is decimal text, bound as numeric, so the pinned figure reaches
	// Postgres exactly as written rather than through a float.
	KgPerDay string
}

// StockRateOverrides is the whole list: one farm, one feed.
var StockRateOverrides = []StockRateOverrideRow{
	{FarmLabel: "CBE", FeedItemKey: "concentrate", KgPerDay: "55"},
}

// StockRateOverrideArrays flattens the overrides into the three parallel arrays
// the stock queries bind, built from one row slice so they cannot drift apart.
func StockRateOverrideArrays() (farms, feeds, rates []string) {
	farms = make([]string, 0, len(StockRateOverrides))
	feeds = make([]string, 0, len(StockRateOverrides))
	rates = make([]string, 0, len(StockRateOverrides))
	for _, row := range StockRateOverrides {
		farms = append(farms, row.FarmLabel)
		feeds = append(feeds, row.FeedItemKey)
		rates = append(rates, row.KgPerDay)
	}
	return farms, feeds, rates
}

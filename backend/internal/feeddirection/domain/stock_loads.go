package domain

import "errors"

// PURCHASED VS CONSUMED, PER LOAD (maintainer request 2026-09-19).
//
// The Stock tab answers "how much is in the store" per feed. This read answers, for EVERY load in
// the purchase ledger, what happened to that load: when it was bought, how many days the buyer said
// it would cover (an optional figure entered on the purchase form), how much of it has been fed,
// how much is left, and whether the buyer's figure is holding up.
//
// Consumption is attributed to loads FIFO by arrival date within one (farm, feed item), exactly as
// the Stock tab's "consumption from" already does for the latest load: a load is drawn on only
// after every earlier load of the same feed at the same farm is used up. The kg fed come from the
// LOCKED feed sheets (plus externally-tracked consumption such as milk), so this is DIRECTED feed
// -- the same basis every figure on the page uses.
//
// The check the maintainer asked for is per load:
//
//	gap = days_said - days_consumed - days_left
//
// Zero means the buyer's figure is holding. Positive means it ran out (or will run out) sooner --
// the case the table highlights, because it is the one that leaves animals unfed if nobody
// re-orders in time. Negative means the load is lasting longer than it was bought for. The gap is ABSENT,
// never zero, when the buyer stated no figure or when nothing has been fed recently enough to
// project days left: a check nobody could make is not a check that passed.

// StockLoadStatus names where one load stands in the FIFO queue.
type StockLoadStatus string

const (
	// StockLoadInTransit is a load bought and still on the road: not stock, nothing consumed.
	StockLoadInTransit StockLoadStatus = "in_transit"
	// StockLoadNotStarted is stock in the store that earlier loads are still shielding.
	StockLoadNotStarted StockLoadStatus = "not_started"
	// StockLoadInUse is the load currently being drawn on.
	StockLoadInUse StockLoadStatus = "in_use"
	// StockLoadFinished is a load whose every kilogram has been directed. Finished loads are
	// HISTORY and the table does not list them (maintainer instruction 2026-09-22): it answers
	// what is in the store now. The status is kept because it is what hides them.
	StockLoadFinished StockLoadStatus = "finished"
)

// StockLoadRow is one purchased load with its FIFO consumption position.
type StockLoadRow struct {
	FeedPurchaseID string
	FarmLabel      string
	FeedItemLabel  string
	FeedItemKey    string
	BatchNo        int64
	Vendor         string
	PurchaseDate   string // YYYY-MM-DD business date
	// ReachedOn is the arrival day the load counts as stock from; empty while in transit.
	ReachedOn string
	// ConsumptionFrom is the first locked feed day that drew on this load; empty until then.
	ConsumptionFrom string
	// FinishedOn is the feed day the load's last kilogram was directed; empty while any remains.
	FinishedOn string
	Status     StockLoadStatus

	// Kilograms. PurchasedKg is what the load is worth in the store (received weight once entered,
	// else the buying weight, net of consumption the sheet had already recorded at import).
	// ConsumedKg and LeftKg split it FIFO; LeftKg can be NEGATIVE when the farm fed more than the
	// ledger bought. That load is still the one being fed from, so it reads as in use and the
	// negative figure is left standing rather than given a status of its own.
	PurchasedKg string
	ConsumedKg  string
	LeftKg      string

	// Days. DaysSaid is the buyer's optional figure. DaysConsumed counts the locked feed days that
	// drew on this load. DaysLeft is LeftKg over the feed's recent daily rate -- nil when nothing
	// has been directed recently enough to divide by, 0 once the load is finished. GapDays is the
	// maintainer's check, nil whenever either side of it is unknown.
	DaysSaid     *int64
	DaysConsumed int64
	DaysLeft     *int64
	GapDays      *int64
	// AvgDailyKg is the feed FAMILY's recent burn rate the days-left figure divides by -- the same
	// divisor, pinned overrides and all, the stock card uses; empty when none.
	AvgDailyKg string
}

// StockLoadsPage is one page of loads plus the whole-filter aggregates.
type StockLoadsPage struct {
	Rows   []StockLoadRow
	Total  int64
	Limit  int
	Offset int
	// FeedItems is every feed the ledger holds a load for in the caller's park scope, unnarrowed
	// by the filter, so the feed-item select can name a feed the current filter hides.
	FeedItems []StockLoadFeedItem
	// Farms is every farm label the ledger holds a load for in the park scope, in park-code
	// order (CBE then CPT), likewise unnarrowed, for the farm select.
	Farms []string
}

// StockLoadFeedItem is one feed-item option for the loads table's filter.
type StockLoadFeedItem struct {
	Key   string
	Label string
}

// StockLoadsQuery narrows the loads read.
type StockLoadsQuery struct {
	// FarmLabel is "" for every farm; FeedItemKey is "" for every feed.
	FarmLabel   string
	FeedItemKey string
	Limit       int
	Offset      int
}

// Page bounds for the loads table, the same shape as the packing-mismatch list.
const (
	DefaultStockLoadsPageSize = 25
	MaxStockLoadsPageSize     = 100
	MaxStockLoadsOffset       = 10000
)

// ErrStockLoadsPageOutOfRange is returned for a limit or offset outside the bounds above.
var ErrStockLoadsPageOutOfRange = errors.New("feeddirection: stock loads page is out of range")

// NormaliseStockLoadsPage validates the requested page and fills the default size. A PRESENT but
// out-of-range value FAILS; only an ABSENT limit takes the default.
func NormaliseStockLoadsPage(limit, offset int) (int, int, error) {
	if limit < 0 || limit > MaxStockLoadsPageSize {
		return 0, 0, ErrStockLoadsPageOutOfRange
	}
	if offset < 0 || offset > MaxStockLoadsOffset {
		return 0, 0, ErrStockLoadsPageOutOfRange
	}
	if limit == 0 {
		limit = DefaultStockLoadsPageSize
	}
	return limit, offset, nil
}

// StockLoadGap is the ONE arithmetic behind the check column, kept in Go so the test that pins
// it reads as the rule: nil unless BOTH the buyer's figure and a days-left projection exist.
func StockLoadGap(daysSaid *int64, daysConsumed int64, daysLeft *int64) *int64 {
	if daysSaid == nil || daysLeft == nil {
		return nil
	}
	gap := *daysSaid - daysConsumed - *daysLeft
	return &gap
}

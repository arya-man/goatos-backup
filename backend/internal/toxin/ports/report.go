package ports

// The /feed/toxin report read model. Its grain is the FEED LOAD (one row per
// feed_purchase_id, taken from that load's latest round) — see domain/report.go for why a
// per-task grain would over-count deliveries.

// ReportParams pages the loads table and sizes the analytics window.
type ReportParams struct {
	TenantID string
	// Filter is a domain.ReportFilter key; blank means All.
	Filter string
	Limit  int
	Cursor string
	// WindowDays bounds the summary, the weekly series and the supplier rollup. The loads
	// TABLE is deliberately NOT windowed: a load waiting since before the window is exactly
	// the row a reader needs to see, and dropping it would make the page look finished.
	WindowDays int
}

// ReportLoad is one delivery, described by its latest test round.
type ReportLoad struct {
	FeedPurchaseID string
	TaskID         string
	RoundNo        int
	FarmLabel      string
	FeedItemLabel  string
	Vendor         string
	BatchNo        int
	PurchaseDate   string // business DATE, YYYY-MM-DD
	QuantityKg     float64
	Status         string
	Outcome        string
	Origin         string
	// TestedByName is the resolved workforce NAME of whoever submitted the reading, blank
	// when nobody has yet or the person cannot be resolved. Never a user id — an id on a
	// leadership screen is not an answer to "who tested this".
	TestedByName string
	SubmittedAt  string
	// TurnaroundMinutes is arrival-to-reading for a submitted round; 0 when not submitted.
	TurnaroundMinutes int
	// WaitingDays is how long an untested load has been waiting; 0 once submitted.
	WaitingDays int
}

// ReportSummary is the KPI strip. Whole-window aggregates, never page-local.
type ReportSummary struct {
	LoadsReceived      int
	LoadsTested        int
	NeedsAttention     int
	Waiting            int
	OldestWaitingDays  int
	OldestWaitingLabel string
	FeedTypes          int
	Parks              int
}

// ReportWeek is one bar in the received-vs-tested series.
type ReportWeek struct {
	WeekStart string // business DATE of the Monday, YYYY-MM-DD
	Received  int
	Tested    int
}

// ReportOutcomeMix is the doughnut: how the window's loads read.
type ReportOutcomeMix struct {
	Negative int
	Positive int
	Invalid  int
	Untested int
}

// ReportVendor is one supplier's screening record over the window.
type ReportVendor struct {
	Vendor  string
	Loads   int
	Flagged int
}

// ReportPage is everything the screen renders in one round trip.
type ReportPage struct {
	Loads      []ReportLoad
	NextCursor string
	Summary    ReportSummary
	Weeks      []ReportWeek
	Mix        ReportOutcomeMix
	Vendors    []ReportVendor
	// FilterCounts is whole-tenant per chip, never page-local, so a chip badge cannot
	// advertise a count the chip's own page does not list.
	FilterCounts map[string]int
}

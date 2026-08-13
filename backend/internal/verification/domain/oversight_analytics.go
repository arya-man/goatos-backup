// Package domain: oversight analytics is the CEO/PC-Director-only aggregate view rendered ABOVE
// the /verify queue table, gated by permissions.VerificationOversee (same capability as the
// oversight_filters control -- see docs/decisions/role-scoped-ui-is-capability-gated.md). Every
// field here is a SERVER-COMPUTED aggregate: no client mega-fetch, no page-local recompute. All
// counts/rates are bounded, tenant-scoped aggregate queries over verification_items /
// verification_review_events -- never a per-row or per-verifier fan-out (see
// docs/decisions/scale-anti-patterns.md -> "N+1 fan-out").
package domain

// OversightKPIs is the top KPI strip: CEO-plain numbers, not internal jargon.
type OversightKPIs struct {
	VideosWaiting int
	// OldestPendingAgeHours is nil when the pending queue is empty.
	OldestPendingAgeHours *float64
	// VerdictsPerActiveDayLast7d is verdicts recorded in the last 7 days divided by the number of
	// DISTINCT days in that window that had at least one verdict (never / 7, which understates
	// speed on a day the team was offline).
	VerdictsPerActiveDayLast7d float64
	// EstDaysToClearBacklog is VideosWaiting / VerdictsPerActiveDayLast7d, nil when the review rate
	// is zero (can't estimate a clear date from zero throughput).
	EstDaysToClearBacklog *float64
	// PerModuleMedianReviewLatencyHours is the median (verified_at - captured_at) per module over
	// verdicts recorded in the last 30 days.
	PerModuleMedianReviewLatencyHours []ModuleLatency
	// RejectRateLast30d is rejected / (approved + rejected) over verdicts in the last 30 days, nil
	// when there were no verdicts in that window.
	RejectRateLast30d *float64
}

// ModuleLatency is one module's median review latency (hours between capture and verdict).
type ModuleLatency struct {
	Module string
	// ModuleLabel is the registry's display copy for Module ("Feed"), resolved by the service.
	// verification_items.module holds the SOURCE module code ("feed"), which is config vocabulary
	// and must never reach a leadership screen -- the copy firewall bans raw codes in visible UI,
	// and admin-web's own nav vocabulary is keyed by NavigationModule ("feed_direction"), so a
	// renderer cannot resolve this label on its own. Empty when the registry knows no such module.
	ModuleLabel string
	MedianHours float64
}

// ModulePendingBacklog is one module's open (pending) item count.
type ModulePendingBacklog struct {
	Module string
	// ModuleLabel: see ModuleLatency.ModuleLabel.
	ModuleLabel string
	// NavModule is the queue's own module-filter key for this module ("feed_direction" where Module
	// is "feed"), so the UI can turn a backlog row into the filter that shows exactly those items.
	// Empty when the registry knows no such module.
	NavModule string
	Count     int
}

// VerifierActivity is one verifier's last-14-day activity plus their watch-integrity aggregate.
type VerifierActivity struct {
	VerifierID   string
	VerifierName string
	Verdicts     int
	Approved     int
	Rejected     int
	// BusiestDay is the Asia/Kolkata calendar date (YYYY-MM-DD) with the most verdicts in the
	// window, empty when Verdicts is 0.
	BusiestDay string
	// ItemsTracked/WatchedToEnd/VerdictWithoutPlay are the watch-integrity aggregate, computed from
	// verification_review_events for items this verifier decided in the window.
	ItemsTracked       int
	WatchedToEndCount  int
	VerdictWithoutPlay int
}

// PendingAgeBuckets is the SHAPE of the pending backlog by how long each video has waited.
// OldestPendingAgeHours gives the worst case; this says whether the backlog is one forgotten tail or
// a wall of old work.
//
// The four buckets are DISJOINT and computed in the same statement (and therefore the same snapshot)
// as OversightKPIs.VideosWaiting, so they always sum to it -- a reader can add them up and get the
// headline number back. Age is ELAPSED TIME since capture, not a business-day count: it is the same
// clock OldestPendingAgeHours reports, so the two cannot disagree about what "old" means.
type PendingAgeBuckets struct {
	UpTo1Day         int
	OneToThreeDays   int
	ThreeToSevenDays int
	OverSevenDays    int
}

// Total returns the pending count the buckets partition. Equal to OversightKPIs.VideosWaiting by
// construction (same statement, same snapshot).
func (b PendingAgeBuckets) Total() int {
	return b.UpTo1Day + b.OneToThreeDays + b.ThreeToSevenDays + b.OverSevenDays
}

// DailyVerificationVolume is one Asia/Kolkata business day of flow through the queue: how many
// videos ARRIVED to be reviewed that day, and how many verdicts were recorded.
//
// Arrived-vs-verdicts is what answers "is the backlog getting better or worse" -- a throughput
// number alone cannot, because 16 verdicts a day is progress against 10 arrivals and a losing battle
// against 40. Days with no activity are present with zeroes rather than omitted, so a chart cannot
// silently compress a quiet week into a busy-looking line.
type DailyVerificationVolume struct {
	// BusinessDate is YYYY-MM-DD in Asia/Kolkata.
	BusinessDate string
	Verdicts     int
	Arrived      int
}

// OversightAnalytics is the full GET /verification/oversight-analytics payload.
type OversightAnalytics struct {
	KPIs              OversightKPIs
	PendingAgeBuckets PendingAgeBuckets
	// DailyVolumeLast14d is oldest-first and always covers 14 consecutive business days.
	DailyVolumeLast14d []DailyVerificationVolume
	PendingByModule    []ModulePendingBacklog
	VerifierActivity   []VerifierActivity
}

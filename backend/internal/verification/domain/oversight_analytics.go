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
	Module      string
	MedianHours float64
}

// ModulePendingBacklog is one module's open (pending) item count.
type ModulePendingBacklog struct {
	Module string
	Count  int
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

// OversightAnalytics is the full GET /verification/oversight-analytics payload.
type OversightAnalytics struct {
	KPIs             OversightKPIs
	PendingByModule  []ModulePendingBacklog
	VerifierActivity []VerifierActivity
}

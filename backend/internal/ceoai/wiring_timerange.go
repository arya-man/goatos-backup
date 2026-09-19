package ceoai

// wiring_timerange.go translates the planner's free-form `time_range` param into
// a governed Cube timeDimension bound to a view's IST business-day member. All
// windows are computed in the Goat OS India business calendar (Asia/Kolkata);
// UTC never defines a Goat OS business day. The parser is deterministic and
// bounded: it returns a single inclusive [from,to] date pair (and an optional
// granularity for trends), or ok=false when the phrase cannot be grounded, so a
// time-scoped question is never silently answered with an all-time aggregate.

import (
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/app"
	"github.com/vgoats/goatos/backend/internal/ceoai/cubeclient"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// maxTrendUnits bounds a bare-granularity trend window so a "by day" trend can
// never request an unbounded date range.
const (
	maxTrendDays   = 90
	maxTrendWeeks  = 26
	maxTrendMonths = 24
)

// timeDimensionFor builds the Cube timeDimension for a metric binding from a
// raw time_range string and the current instant. ok=false means "no groundable
// time window" (caller omits the timeDimension). now is injected for testability.
func timeDimensionFor(b metricBinding, timeRange string, now time.Time) (cubeclient.TimeDimension, bool) {
	tr := strings.ToLower(strings.TrimSpace(timeRange))
	if tr == "" || b.timeDim == "" {
		return cubeclient.TimeDimension{}, false
	}
	member := b.view + "." + b.timeDim
	loc := biztime.DefaultLocation()
	today := biztime.BusinessDayStart(now.In(loc))

	from, to, gran, ok := resolveWindow(tr, today)
	if !ok {
		return cubeclient.TimeDimension{}, false
	}
	td := cubeclient.TimeDimension{
		Dimension: member,
		DateRange: []string{fmtDate(from), fmtDate(to)},
	}
	if gran != "" {
		td.Granularity = gran
	}
	return td, true
}

// resolveWindow maps a normalized phrase to an inclusive [from,to] business-day
// pair plus an optional granularity (day|week|month) for trend requests. Trend
// phrases are Cube-specific and resolved here; every other period phrase is
// resolved by the shared app.ResolveWindow so the Cube, API and SQL tiers
// agree on one business calendar (plan v3 D1.2).
func resolveWindow(tr string, today time.Time) (from, to time.Time, gran string, ok bool) {
	// Trend granularity hints: "... trend by month", "monthly trend", or a bare
	// grain word imply a bucketed series over a trailing window.
	if g := trendGranularity(tr); g != "" {
		return trailingTrendWindow(today, g)
	}
	w, ok := app.ResolveWindow(tr, today, today.Location())
	if !ok {
		return time.Time{}, time.Time{}, "", false
	}
	return w.From, w.To, "", true
}

// trendGranularity extracts a granularity when the phrase is a trend/series
// request, else "". A bare grain word ("month") is also treated as a trend.
func trendGranularity(tr string) string {
	grain := ""
	switch {
	case strings.Contains(tr, "by day"), strings.Contains(tr, "daily"), tr == "day":
		grain = "day"
	case strings.Contains(tr, "by week"), strings.Contains(tr, "weekly"), tr == "week":
		grain = "week"
	case strings.Contains(tr, "by month"), strings.Contains(tr, "monthly"), tr == "month":
		grain = "month"
	}
	// A "trend"/"over time" phrase with no explicit grain defaults to month.
	if grain == "" && (strings.Contains(tr, "trend") || strings.Contains(tr, "over time")) {
		grain = "month"
	}
	return grain
}

// trailingTrendWindow returns a bounded trailing window ending today for a grain.
func trailingTrendWindow(today time.Time, gran string) (from, to time.Time, g string, ok bool) {
	switch gran {
	case "day":
		return today.AddDate(0, 0, -(maxTrendDays - 1)), today, "day", true
	case "week":
		return startOfWeek(today).AddDate(0, 0, -7*(maxTrendWeeks-1)), today, "week", true
	case "month":
		return startOfMonth(today).AddDate(0, -(maxTrendMonths - 1), 0), endOfMonth(today), "month", true
	}
	return time.Time{}, time.Time{}, "", false
}

// --- business-calendar boundary helpers (all inputs are day-start in IST) ---

// startOfWeek returns the Monday of t's week (Goat OS weeks are Monday-start).
func startOfWeek(t time.Time) time.Time {
	wd := int(t.Weekday()) // Sunday=0
	delta := wd - 1        // Monday=1 -> 0
	if wd == 0 {
		delta = 6 // Sunday -> back to previous Monday
	}
	return t.AddDate(0, 0, -delta)
}

func startOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, t.Location())
}

func endOfMonth(t time.Time) time.Time {
	return startOfMonth(t).AddDate(0, 1, 0).AddDate(0, 0, -1)
}

func fmtDate(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}

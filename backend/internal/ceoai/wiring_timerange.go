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
	"regexp"
	"strconv"
	"strings"
	"time"

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

var lastNPattern = regexp.MustCompile(`^(?:last|past)\s+(\d{1,3})\s+(day|days|week|weeks|month|months)$`)

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
// pair plus an optional granularity (day|week|month) for trend requests.
func resolveWindow(tr string, today time.Time) (from, to time.Time, gran string, ok bool) {
	// Trend granularity hints: "... trend by month", "monthly trend", or a bare
	// grain word imply a bucketed series over a trailing window.
	if g := trendGranularity(tr); g != "" {
		return trailingTrendWindow(today, g)
	}

	switch tr {
	case "today":
		return today, today, "", true
	case "yesterday":
		y := today.AddDate(0, 0, -1)
		return y, y, "", true
	case "this week", "current week":
		start := startOfWeek(today)
		return start, start.AddDate(0, 0, 6), "", true
	case "last week", "previous week":
		start := startOfWeek(today).AddDate(0, 0, -7)
		return start, start.AddDate(0, 0, 6), "", true
	case "this month", "current month", "mtd", "month to date":
		start := startOfMonth(today)
		return start, endOfMonth(today), "", true
	case "last month", "previous month":
		prev := startOfMonth(today).AddDate(0, 0, -1)
		return startOfMonth(prev), endOfMonth(prev), "", true
	case "this year", "current year", "ytd", "year to date":
		return startOfYear(today), today, "", true
	case "last year", "previous year":
		prevY := startOfYear(today).AddDate(-1, 0, 0)
		return prevY, endOfYear(prevY), "", true
	}

	// "last N days|weeks|months".
	if m := lastNPattern.FindStringSubmatch(tr); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return time.Time{}, time.Time{}, "", false
		}
		switch {
		case strings.HasPrefix(m[2], "day"):
			return today.AddDate(0, 0, -(n - 1)), today, "", true
		case strings.HasPrefix(m[2], "week"):
			return today.AddDate(0, 0, -(7*n - 1)), today, "", true
		case strings.HasPrefix(m[2], "month"):
			return today.AddDate(0, -n, 0).AddDate(0, 0, 1), today, "", true
		}
	}

	// Explicit ISO fixed range or single date.
	if f, t, ok := parseISORange(tr); ok {
		return f, t, "", true
	}
	return time.Time{}, time.Time{}, "", false
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

var isoRangePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s*(?:\.\.|,|\s+to\s+|\s+-\s+)\s*(\d{4}-\d{2}-\d{2})$`)
var isoSingle = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// parseISORange parses "YYYY-MM-DD..YYYY-MM-DD" (or ",", " to ", " - " separators)
// and a single "YYYY-MM-DD" (one-day window). Dates are read in the business
// calendar. from is clamped to <= to.
func parseISORange(tr string) (from, to time.Time, ok bool) {
	loc := biztime.DefaultLocation()
	if m := isoRangePattern.FindStringSubmatch(tr); m != nil {
		f, err1 := time.ParseInLocation("2006-01-02", m[1], loc)
		t, err2 := time.ParseInLocation("2006-01-02", m[2], loc)
		if err1 != nil || err2 != nil {
			return time.Time{}, time.Time{}, false
		}
		if f.After(t) {
			f, t = t, f
		}
		return f, t, true
	}
	if isoSingle.MatchString(tr) {
		d, err := time.ParseInLocation("2006-01-02", tr, loc)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
		return d, d, true
	}
	return time.Time{}, time.Time{}, false
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

func startOfYear(t time.Time) time.Time {
	y, _, _ := t.Date()
	return time.Date(y, time.January, 1, 0, 0, 0, 0, t.Location())
}

func endOfYear(t time.Time) time.Time {
	return startOfYear(t).AddDate(1, 0, 0).AddDate(0, 0, -1)
}

func fmtDate(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}

package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Herd Analytics is the Counts leadership read: what the herd IS RIGHT NOW
// (composition by breed, tag/stage, sex and age band) beside what MOVED it over
// a window of months (births in, deaths and sales out, pen movements within).
//
// It derives no fact of its own that the census does not already carry. Every
// composition figure is the same canonical `goats` population Counts Breakdown
// reports (live animals, merged identities excluded), and every flow figure is
// counted off the canonical row that recorded the event — an animal's own
// origin/exit columns for births and exits, an APPLIED shifting event for a
// movement. Nothing here reads a projection, so the two Counts screens can
// never disagree about the denominator.
//
// TIME GRAIN IS THE INDIA BUSINESS MONTH. Every bucket is `Asia/Kolkata`
// calendar month, never a UTC instant and never a rolling 30-day slice, so a
// figure the farm reads as "August" is the month the farm actually worked.

// HerdAnalyticsSeriesPoint is one composition bar. Key is the raw stored value
// (empty for an unassigned one) and Label is what the source recorded; a client
// renders its own contract copy for the empty bucket, exactly as Counts
// Breakdown does.
type HerdAnalyticsSeriesPoint struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// HerdAnalyticsMonth is one India-calendar month of herd movement.
//
// NetChange is Births minus every exit (Deaths + Sold + OtherExits), so the
// four flow figures and the net always reconcile on the client without it
// re-deriving one of them.
type HerdAnalyticsMonth struct {
	// Month is the IST calendar month key, "2026-08".
	Month string `json:"month"`
	// Label is the farm-readable month, "Aug 2026".
	Label      string `json:"label"`
	Births     int64  `json:"births"`
	Deaths     int64  `json:"deaths"`
	Sold       int64  `json:"sold"`
	OtherExits int64  `json:"other_exits"`
	// Movements is APPLIED shifting events — a movement counts on the day the
	// operator completed it, never when it was raised or approved, because that
	// is the day the animals actually walked.
	Movements int64 `json:"movements"`
	// AnimalsMoved is the head count those movements carried, summed off the
	// per-movement impact rows.
	AnimalsMoved int64 `json:"animals_moved"`
	NetChange    int64 `json:"net_change"`
}

// HerdAnalyticsTotals are whole-window rollups plus the current census. They are
// computed over the FULL window server-side and must never be re-derived from
// the returned months.
type HerdAnalyticsTotals struct {
	// LiveAnimals, Kids and Adults are the census AS OF NOW, not a window figure.
	// Kids + Adults always equals LiveAnimals exactly: an unknown age band counts
	// as an adult rather than falling out of both buckets.
	LiveAnimals int64 `json:"live_animals"`
	Kids        int64 `json:"kids"`
	Adults      int64 `json:"adults"`

	Births       int64 `json:"births"`
	Deaths       int64 `json:"deaths"`
	Sold         int64 `json:"sold"`
	OtherExits   int64 `json:"other_exits"`
	Movements    int64 `json:"movements"`
	AnimalsMoved int64 `json:"animals_moved"`
	NetChange    int64 `json:"net_change"`
}

// HerdAnalytics is the whole page payload: one census composition set and one
// month-by-month flow series, both already scoped to the requested park.
type HerdAnalytics struct {
	// WindowFrom and WindowTo are IST calendar dates bounding the flow series.
	WindowFrom string               `json:"window_from"`
	WindowTo   string               `json:"window_to"`
	Totals     HerdAnalyticsTotals  `json:"totals"`
	Months     []HerdAnalyticsMonth `json:"months"`

	Breed   []HerdAnalyticsSeriesPoint `json:"breed"`
	Stage   []HerdAnalyticsSeriesPoint `json:"stage"`
	Sex     []HerdAnalyticsSeriesPoint `json:"sex"`
	AgeBand []HerdAnalyticsSeriesPoint `json:"age_band"`
	Park    []HerdAnalyticsSeriesPoint `json:"park"`

	GeneratedAt time.Time `json:"generated_at"`
}

// HerdAnalyticsQuery scopes the read.
//
// The window is two inclusive IST business DAYS, matching the shared calendar
// control every other filtered screen uses. Totals cover exactly those days.
//
// The flow SERIES still buckets by business month, because daily births are a
// row of zeroes and ones that says nothing. A window whose edge falls mid-month
// therefore produces a partial first or last column, which the page's own copy
// states rather than leaving a reader to misread it as a collapse in births.
// Empty means the default window.
type HerdAnalyticsQuery struct {
	TenantID string
	ParkID   *string
	// FromDate and ToDate are inclusive "YYYY-MM-DD" bounds.
	FromDate string
	ToDate   string
}

// ---------------------------------------------------------------------------
// Window rules
//
// These are pure calendar rules with no I/O, so they live here rather than in
// either adapter: the HTTP layer validates a caller's window with the SAME
// functions the repository resolves it with, and two hand-rolled copies cannot
// drift on what counts as a day.
// ---------------------------------------------------------------------------

// HerdAnalyticsDateLayout is the wire format of a window bound.
const HerdAnalyticsDateLayout = "2006-01-02"

const (
	// HerdAnalyticsDefaultMonths is the default window: twelve months back from
	// the first of the current month, through today.
	HerdAnalyticsDefaultMonths = 12
	// HerdAnalyticsMaxDays is the widest window the read serves, a little over
	// three years. A longer one is REJECTED rather than silently trimmed — a
	// leader who asked for five years must not be shown three and told nothing.
	HerdAnalyticsMaxDays = 1150
)

// ParseHerdAnalyticsDate parses an inclusive "YYYY-MM-DD" bound into the first
// instant of that IST day.
func ParseHerdAnalyticsDate(raw string) (time.Time, error) {
	parsed, err := time.ParseInLocation(HerdAnalyticsDateLayout, strings.TrimSpace(raw), biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, fmt.Errorf("counts: %q is not a YYYY-MM-DD date", raw)
	}
	return parsed, nil
}

// HerdAnalyticsDefaultWindow returns the default inclusive bounds: the first day
// of the month eleven back, through today.
//
// It starts on a month BOUNDARY rather than exactly 365 days back because the
// flow chart buckets by business month — an arbitrary start day would open the
// default view with a half-empty first column that looks like a collapse in
// births rather than a window edge.
func HerdAnalyticsDefaultWindow(now time.Time) (from string, to string) {
	ist := biztime.DefaultLocation()
	today := now.In(ist)
	firstOfMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, ist)
	return firstOfMonth.AddDate(0, -(HerdAnalyticsDefaultMonths - 1), 0).Format(HerdAnalyticsDateLayout),
		today.Format(HerdAnalyticsDateLayout)
}

// ResolveHerdAnalyticsWindow validates and resolves the inclusive window.
//
// BOTH ABSENT means the default window. Anything else is validated and REJECTED
// on failure — a malformed date, only one of the pair, a reversed range, or a
// span wider than HerdAnalyticsMaxDays. None of those is quietly rewritten: a
// leader who names a window must see that window or an error, never a different
// window under the label they chose.
func ResolveHerdAnalyticsWindow(rawFrom, rawTo string, now time.Time) (string, string, error) {
	from := strings.TrimSpace(rawFrom)
	to := strings.TrimSpace(rawTo)
	if from == "" && to == "" {
		defaultFrom, defaultTo := HerdAnalyticsDefaultWindow(now)
		return defaultFrom, defaultTo, nil
	}
	if from == "" || to == "" {
		return "", "", fmt.Errorf("from and to must be given together as YYYY-MM-DD dates")
	}
	fromDate, err := ParseHerdAnalyticsDate(from)
	if err != nil {
		return "", "", fmt.Errorf("from must be a YYYY-MM-DD date")
	}
	toDate, err := ParseHerdAnalyticsDate(to)
	if err != nil {
		return "", "", fmt.Errorf("to must be a YYYY-MM-DD date")
	}
	if toDate.Before(fromDate) {
		return "", "", fmt.Errorf("to must not be earlier than from")
	}
	if days := int(toDate.Sub(fromDate).Hours()/24) + 1; days > HerdAnalyticsMaxDays {
		return "", "", fmt.Errorf("the window must not be longer than %d days", HerdAnalyticsMaxDays)
	}
	return fromDate.Format(HerdAnalyticsDateLayout), toDate.Format(HerdAnalyticsDateLayout), nil
}

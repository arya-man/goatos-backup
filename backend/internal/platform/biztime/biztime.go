// Package biztime centralizes Goat OS operational calendar handling.
package biztime

import (
	"time"
)

// DefaultTimezone is Goat OS' fixed operational business calendar.
const DefaultTimezone = "Asia/Kolkata"

var defaultLocation = func() *time.Location {
	loc, err := time.LoadLocation(DefaultTimezone)
	if err != nil {
		return time.FixedZone("IST", 5*60*60+30*60)
	}
	return loc
}()

// DefaultLocation returns the default Goat OS business-calendar location.
func DefaultLocation() *time.Location {
	return defaultLocation
}

// Location returns Goat OS' fixed India-only business-calendar location.
func Location(_ string) *time.Location {
	return defaultLocation
}

// BusinessDayStart returns midnight for t's date in the default Goat OS
// operational calendar.
func BusinessDayStart(t time.Time) time.Time {
	return BusinessDayStartIn(t, DefaultTimezone)
}

// BusinessDayStartIn returns midnight for t's date in the Goat OS operational
// calendar. The timezone parameter is retained for API compatibility and is
// ignored.
func BusinessDayStartIn(t time.Time, timezone string) time.Time {
	loc := Location(timezone)
	inLoc := t.In(loc)
	y, m, d := inLoc.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// BusinessDate returns YYYY-MM-DD for t in the default operational calendar.
func BusinessDate(t time.Time) string {
	return BusinessDayStart(t).Format("2006-01-02")
}

// BusinessDateIn returns YYYY-MM-DD for t in the Goat OS operational calendar.
// The timezone parameter is retained for API compatibility and is ignored.
func BusinessDateIn(t time.Time, timezone string) string {
	return BusinessDayStartIn(t, timezone).Format("2006-01-02")
}

// ClampFutureAsOf converts an as-of instant into the Goat OS business calendar and
// caps future values at the server's current business instant. Live operational
// reads may reconstruct historical state, but a stale or hand-edited future
// as_of must not make not-yet-due work look overdue.
func ClampFutureAsOf(asOf, now time.Time) time.Time {
	if asOf.IsZero() {
		return now
	}
	loc := DefaultLocation()
	asOf = asOf.In(loc)
	now = now.In(loc)
	if asOf.After(now) {
		return now
	}
	return asOf
}

// ParseLiveAsOfRFC3339 parses an RFC3339 as_of value for live operational reads.
// Malformed values return the parse error; valid future values are clamped to now.
func ParseLiveAsOfRFC3339(raw string, now time.Time) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return ClampFutureAsOf(parsed, now), nil
}

// FarmDateFormat is how a date is WRITTEN TO A PERSON across Goat OS: dd/mm/yyyy, the form the
// farm's own sheets use (maintainer decision 2026-08-24). It applies to visible copy only --
// notification titles and bodies, screen labels. Structured payload fields (a notification
// context's due_date, an API response, a query parameter) stay ISO YYYY-MM-DD, because clients
// PARSE those and render their own localized string from them.
const FarmDateFormat = "02/01/2006"

// FarmDate writes an instant as dd/mm/yyyy in the operational calendar, for visible copy.
func FarmDate(t time.Time) string {
	return BusinessDayStart(t).Format(FarmDateFormat)
}

// FarmDateFromBusinessDate rewrites an ISO business date (YYYY-MM-DD, as BusinessDate returns and
// as every structured field carries) into the visible dd/mm/yyyy form. A value that is not an ISO
// date is returned UNCHANGED rather than mangled or blanked: a malformed date in copy is a bug to
// see, not one to hide behind an empty string.
func FarmDateFromBusinessDate(businessDate string) string {
	parsed, err := time.ParseInLocation("2006-01-02", businessDate, DefaultLocation())
	if err != nil {
		return businessDate
	}
	return parsed.Format(FarmDateFormat)
}

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

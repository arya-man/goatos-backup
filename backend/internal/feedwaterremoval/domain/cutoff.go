// Package domain holds the feed & water removal evening cutoff rule
// (maintainer decision 2026-09-07: the cutoff is CONFIG, not code).
//
// Animals have feed and water removed the evening BEFORE they are weighed or
// given an in-feed deworming (docs/decisions/feed-water-removal-precondition.md).
// That evening opens at one farm-wide Asia/Kolkata wall-clock time, stored per
// tenant in feed_water_removal_config. Two questions hang off it, and both
// modules (weighing, PC Care) answer them through THIS package so they can
// never disagree about when the evening starts:
//
//   - PLANNING: the earliest date work needing a removal may be planned for at
//     instant now. Strictly before the cutoff that is TOMORROW (tonight's
//     removal can still be staffed); at or after it, the DAY AFTER TOMORROW.
//     Today is never plannable: its removal evening was yesterday.
//   - VISIBILITY: the removal card surfaces on the operator's list from the
//     cutoff on the evening before the work date.
//
// Both are BUSINESS-DAY grained, never now±N hours: the cutoff is compared as
// an IST wall-clock minute-of-day and the answer is a business date.
package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// ErrInvalidCutoff refuses a cutoff that is not an HH:MM wall-clock time.
var ErrInvalidCutoff = errors.New("feedwaterremoval: cutoff must be an HH:MM wall-clock time")

// Cutoff is the removal evening's opening time as an Asia/Kolkata wall-clock
// minute of day. The zero value is INVALID on purpose: 00:00 is a legal cutoff
// but an unset one must never quietly read as midnight, so construct through
// NewCutoff/ParseCutoff and check Valid before use.
type Cutoff struct {
	hour, minute int
	valid        bool
}

// NewCutoff builds a cutoff from a wall-clock hour (0-23) and minute (0-59).
func NewCutoff(hour, minute int) (Cutoff, error) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return Cutoff{}, ErrInvalidCutoff
	}
	return Cutoff{hour: hour, minute: minute, valid: true}, nil
}

// MustCutoff is NewCutoff for tests and fixtures; it panics on a bad value.
func MustCutoff(hour, minute int) Cutoff {
	c, err := NewCutoff(hour, minute)
	if err != nil {
		panic(err)
	}
	return c
}

// ParseCutoff accepts "HH:MM" or "HH:MM:SS" (the Postgres time text form);
// seconds are ignored because the rule is minute-grained.
func ParseCutoff(raw string) (Cutoff, error) {
	var hour, minute, second int
	switch n, _ := fmt.Sscanf(raw, "%d:%d:%d", &hour, &minute, &second); n {
	case 2, 3:
	default:
		return Cutoff{}, ErrInvalidCutoff
	}
	if len(raw) < 5 || raw[2] != ':' {
		return Cutoff{}, ErrInvalidCutoff
	}
	return NewCutoff(hour, minute)
}

// Valid reports whether the cutoff was set. A caller holding an invalid cutoff
// must refuse the operation, never fall back to a literal hour.
func (c Cutoff) Valid() bool { return c.valid }

// Hour and Minute expose the wall-clock parts for callers composing an instant.
func (c Cutoff) Hour() int   { return c.hour }
func (c Cutoff) Minute() int { return c.minute }

// String renders "HH:MM", the wire form served to clients.
func (c Cutoff) String() string { return fmt.Sprintf("%02d:%02d", c.hour, c.minute) }

// SQLTime renders "HH:MM:SS" for a `$n::time` bind, so a query can keep the
// wall-clock comparison in SQL while the value still comes from config.
func (c Cutoff) SQLTime() string { return fmt.Sprintf("%02d:%02d:00", c.hour, c.minute) }

func (c Cutoff) minuteOfDay() int { return c.hour*60 + c.minute }

// ReachedAt reports whether the IST wall clock at instant now is at or past
// the cutoff — the one comparison every rule in this package reduces to.
func (c Cutoff) ReachedAt(now time.Time) bool {
	local := now.In(biztime.DefaultLocation())
	return local.Hour()*60+local.Minute() >= c.minuteOfDay()
}

// EarliestPlannableDate answers the planning question as 00:00 IST of the
// earliest business day: tomorrow strictly before the cutoff, the day after
// tomorrow at or after it. now is the CALLER's clock (the service's injectable
// clock), passed in so the rule is deterministic in tests; the IST conversion
// happens here and nowhere else.
func EarliestPlannableDate(now time.Time, c Cutoff) time.Time {
	local := now.In(biztime.DefaultLocation())
	lead := 1
	if c.ReachedAt(local) {
		lead = 2
	}
	return biztime.BusinessDayStart(local).AddDate(0, 0, lead)
}

// EarliestPlannableBusinessDate is EarliestPlannableDate as a YYYY-MM-DD
// business date, the form the create commands carry.
func EarliestPlannableBusinessDate(now time.Time, c Cutoff) string {
	return EarliestPlannableDate(now, c).Format("2006-01-02")
}

// DateAllowsPlanning reports whether businessDate (a validated YYYY-MM-DD) is
// still plannable at instant now under cutoff c.
func DateAllowsPlanning(businessDate string, now time.Time, c Cutoff) bool {
	return businessDate >= EarliestPlannableBusinessDate(now, c)
}

// VisibleFrom answers the visibility question: the instant the removal card
// for work on businessDate appears, i.e. the cutoff on the evening BEFORE.
func VisibleFrom(businessDate string, c Cutoff) (time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", businessDate, biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, err
	}
	eve := day.AddDate(0, 0, -1)
	return time.Date(eve.Year(), eve.Month(), eve.Day(), c.hour, c.minute, 0, 0, biztime.DefaultLocation()), nil
}

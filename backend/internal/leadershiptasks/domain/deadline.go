package domain

import (
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// DEADLINE AND THE DAY COUNTER (maintainer decision 2026-09-14).
//
// Every task raised from the task form carries a DEADLINE -- a date AND a time, entered by the
// raiser when the task is raised and editable by the raiser while the task is still open for
// work. Beside the task the screen shows ONE BIG NUMBER: how many days the task has taken so
// far, counted in the farm's calendar (IST) from the day it was raised to today. The assign
// date IS the raise date; there is no separate start date. The number is GREEN while the task
// is within its deadline and RED once the deadline instant has passed.
//
// A FINISHED task freezes its clock. Done or cancelled, the number stops at the finish instant
// so it reads how long the task actually took, and the colour records whether it met its
// deadline when it finished -- a task finished late stays red for ever; nothing is rewritten.
//
// A task with NO deadline (raised before this existed, or through the Work Board flag, which
// has no form to ask) shows NO counter and no colour until its raiser sets one. Showing a
// number in no colour would read as a task that is neither on time nor late.
//
// Every value here is composed ONCE, in this file, from the stored row and the server clock,
// and rendered verbatim by admin-web and the phone. Neither client counts days itself.

// Deadline tones. The client maps ok -> green and late -> red; blank means no deadline.
const (
	DeadlineToneOK   = "ok"
	DeadlineToneLate = "late"
)

// deadlineLabelFormat is the visible dd/mm/yyyy hh:mm form, in the operational calendar.
const deadlineLabelFormat = biztime.FarmDateFormat + " 15:04"

// ValidateDeadline checks a raise or edit's deadline against the task's raise instant. The
// form path requires one; a programmatic raise with no form (the Work Board flag) may omit it.
func ValidateDeadline(deadline *time.Time, raisedAt time.Time, required bool) error {
	if deadline == nil {
		if required {
			return ErrDeadlineRequired
		}
		return nil
	}
	if !deadline.After(raisedAt) {
		return ErrDeadlineNotAfterRaise
	}
	return nil
}

// ClockEnd is the instant the day counter reads up to: now while the task is open for work,
// the finish instant once it is done or cancelled.
func ClockEnd(t Task, now time.Time) time.Time {
	switch t.Status {
	case StatusDone:
		if t.DoneAt != nil {
			return *t.DoneAt
		}
	case StatusCancelled:
		if t.CancelledAt != nil {
			return *t.CancelledAt
		}
	}
	return now
}

// DaysTaken is the big number: farm-calendar days from the raise day to the clock end. Raised
// today reads 0. It compares business DAYS, never hours, so the number is the same whatever
// time the screen is opened.
func DaysTaken(t Task, now time.Time) int {
	start := biztime.BusinessDayStart(t.RaisedAt)
	end := biztime.BusinessDayStart(ClockEnd(t, now))
	days := int(end.Sub(start).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// DeadlineTone is the colour of the number: ok (green) while the clock end is on or before the
// deadline, late (red) once it is past. Blank when the task has no deadline.
func DeadlineTone(t Task, now time.Time) string {
	if t.DeadlineAt == nil {
		return ""
	}
	if ClockEnd(t, now).After(*t.DeadlineAt) {
		return DeadlineToneLate
	}
	return DeadlineToneOK
}

// DeadlineLabel is the visible deadline: "15/09/2026 17:00" in IST; blank without one.
func DeadlineLabel(deadline *time.Time) string {
	if deadline == nil {
		return ""
	}
	return deadline.In(biztime.DefaultLocation()).Format(deadlineLabelFormat)
}

// DaysTakenLabel is the number with its unit: "0 days", "1 day", "12 days".
func DaysTakenLabel(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

// DeadlineStateLabel is the sentence under the number, from the task's state: an open task is
// within or past its deadline; a finished one finished within or after it.
func DeadlineStateLabel(t Task, now time.Time) string {
	tone := DeadlineTone(t, now)
	if tone == "" {
		return ""
	}
	finished := t.Status == StatusDone || t.Status == StatusCancelled
	switch {
	case finished && tone == DeadlineToneOK:
		return "Finished within deadline"
	case finished:
		return "Finished after deadline"
	case tone == DeadlineToneOK:
		return "Within deadline"
	default:
		return "Past deadline"
	}
}

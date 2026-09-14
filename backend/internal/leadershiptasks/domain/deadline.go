package domain

import (
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// DEADLINE AND THE COUNTDOWN (maintainer decisions 2026-09-14, the second superseding the first
// on WHAT the number is).
//
// Every task raised from the task form carries a DEADLINE -- a date AND a time, entered by the
// raiser when the task is raised and editable by the raiser while the task is still open for
// work. Beside the task the screen shows ONE BIG NUMBER: how many farm-calendar days (IST) are
// LEFT until the deadline, counted from today. It reads "5 days left", "due today", and once
// the deadline has passed "3 days over". The number is GREEN while more than NearDays (2) days
// remain and RED from 2 days left onward -- and stays red once overdue -- so a task turns red
// as its deadline comes near, not only after it is missed.
//
// The first version counted days TAKEN since the raise; the maintainer replaced it the same
// day with the countdown, because "how long has this run" says nothing about whether the
// deadline is safe, and the colour is what the reader acts on.
//
// A FINISHED task freezes its clock. Done or cancelled, the number is read against the finish
// instant, so it records whether the task made its deadline and by how much ("finished 2 days
// early" / "finished 1 day late"), and nothing is rewritten afterwards.
//
// A task with NO deadline (raised before this existed, or through the Work Board flag, which
// has no form to ask) shows NO counter and no colour until its raiser sets one. Showing a
// number in no colour would read as a task that is neither on time nor late.
//
// Every value here is composed ONCE, in this file, from the stored row and the server clock,
// and rendered verbatim by admin-web and the phone. Neither client counts days itself.

// Deadline tones. The client maps ok -> green and everything else -> red; blank means no
// deadline. near and over are kept apart so a reader of the wire can tell "almost due" from
// "missed" without re-deriving it from the number.
const (
	DeadlineToneOK   = "ok"
	DeadlineToneNear = "near"
	DeadlineToneOver = "over"
)

// NearDays is how many days left turns the countdown red: at 2 days left or fewer.
const NearDays = 2

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

// ClockEnd is the instant the countdown is read from: now while the task is open for work,
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

// IsFinished reports whether the task's clock is frozen.
func IsFinished(t Task) bool { return t.Status == StatusDone || t.Status == StatusCancelled }

// DaysLeft is the big number: farm-calendar days from the clock end's day to the deadline's
// day. Positive means days remaining, 0 means due today, negative means days overdue. It
// compares business DAYS, never hours, so the number is the same whatever time the screen is
// opened. Zero when the task has no deadline (callers check DeadlineAt first).
func DaysLeft(t Task, now time.Time) int {
	if t.DeadlineAt == nil {
		return 0
	}
	from := biztime.BusinessDayStart(ClockEnd(t, now))
	to := biztime.BusinessDayStart(*t.DeadlineAt)
	return int(to.Sub(from).Hours() / 24)
}

// DeadlineTone is the colour of the number: ok (green) while more than NearDays days remain,
// near (red) from NearDays days left down to the deadline instant, over (red) once the clock
// end is past the deadline instant. A FINISHED task is never "near": it either made its
// deadline (ok) or missed it (over) -- there is nothing left to hurry. Blank when the task has
// no deadline.
func DeadlineTone(t Task, now time.Time) string {
	if t.DeadlineAt == nil {
		return ""
	}
	if ClockEnd(t, now).After(*t.DeadlineAt) {
		return DeadlineToneOver
	}
	if !IsFinished(t) && DaysLeft(t, now) <= NearDays {
		return DeadlineToneNear
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

// DaysLeftLabel is the number with its meaning: "5 days left", "1 day left", "Due today",
// "3 days over"; for a finished task "2 days early", "1 day late", "On the day". Blank
// without a deadline.
func DaysLeftLabel(t Task, now time.Time) string {
	if t.DeadlineAt == nil {
		return ""
	}
	days := DaysLeft(t, now)
	if IsFinished(t) {
		switch {
		case days > 0:
			return fmt.Sprintf("%s early", dayWord(days))
		case days < 0:
			return fmt.Sprintf("%s late", dayWord(-days))
		default:
			return "On the day"
		}
	}
	switch {
	case days > 0:
		return fmt.Sprintf("%s left", dayWord(days))
	case days < 0:
		return fmt.Sprintf("%s over", dayWord(-days))
	case DeadlineTone(t, now) == DeadlineToneOver:
		// Same day as the deadline but the hour has passed.
		return "Due today, time passed"
	default:
		return "Due today"
	}
}

func dayWord(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// DeadlineStateLabel is the sentence under the number. Open: "Due in 5 days" / "Due today" /
// "Overdue by 3 days". Finished: "Finished 2 days early" / "Finished on the day" /
// "Finished 1 day late". Blank without a deadline.
func DeadlineStateLabel(t Task, now time.Time) string {
	if t.DeadlineAt == nil {
		return ""
	}
	days := DaysLeft(t, now)
	over := DeadlineTone(t, now) == DeadlineToneOver
	if IsFinished(t) {
		switch {
		case days > 0:
			return fmt.Sprintf("Finished %s early", dayWord(days))
		case days < 0:
			return fmt.Sprintf("Finished %s late", dayWord(-days))
		case over:
			return "Finished on the day, after the deadline"
		default:
			return "Finished on the day"
		}
	}
	switch {
	case days > 0:
		return fmt.Sprintf("Due in %s", dayWord(days))
	case days < 0:
		return fmt.Sprintf("Overdue by %s", dayWord(-days))
	case over:
		return "Due today, time passed"
	default:
		return "Due today"
	}
}

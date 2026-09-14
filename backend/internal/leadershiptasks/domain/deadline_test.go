package domain

import (
	"errors"
	"testing"
	"time"
)

// IST is UTC+05:30; every business day below is named by its IST calendar date.
func ist(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.FixedZone("IST", 5*3600+1800))
}

// The counter is the number of farm-calendar DAYS between the raise day and today -- raised
// today reads 0, raised yesterday evening reads 1 even if fewer than 24 hours passed -- and
// it never depends on the hour the screen is opened.
func TestDaysTakenCountsFarmCalendarDaysFromTheRaiseDay(t *testing.T) {
	task := sample(StatusOpen)
	task.RaisedAt = ist(2026, time.September, 10, 23, 30)
	cases := []struct {
		now  time.Time
		want int
	}{
		{ist(2026, time.September, 10, 23, 45), 0},
		{ist(2026, time.September, 11, 0, 15), 1},
		{ist(2026, time.September, 14, 9, 0), 4},
	}
	for _, c := range cases {
		if got := DaysTaken(task, c.now); got != c.want {
			t.Fatalf("now=%s: days=%d want %d", c.now, got, c.want)
		}
	}
	// A clock skewed before the raise never reads negative.
	if got := DaysTaken(task, ist(2026, time.September, 9, 12, 0)); got != 0 {
		t.Fatalf("pre-raise clock: %d want 0", got)
	}
}

// A finished task freezes its clock: the number reads how long it actually took and stops
// growing, and the colour records whether it met its deadline at the moment it finished.
func TestFinishedTaskFreezesTheClockAtTheFinishInstant(t *testing.T) {
	deadline := ist(2026, time.September, 15, 17, 0)
	raised := ist(2026, time.September, 10, 9, 0)
	later := ist(2026, time.October, 1, 9, 0)

	done := sample(StatusDone)
	done.RaisedAt, done.DeadlineAt = raised, &deadline
	doneAt := ist(2026, time.September, 13, 18, 0)
	done.DoneAt = &doneAt
	if got := DaysTaken(done, later); got != 3 {
		t.Fatalf("done days=%d want 3 (frozen at done_at)", got)
	}
	if got := DeadlineTone(done, later); got != DeadlineToneOK {
		t.Fatalf("done within deadline tone=%q want %q", got, DeadlineToneOK)
	}

	late := sample(StatusDone)
	late.RaisedAt, late.DeadlineAt = raised, &deadline
	lateAt := ist(2026, time.September, 16, 8, 0)
	late.DoneAt = &lateAt
	if got := DeadlineTone(late, later); got != DeadlineToneLate {
		t.Fatalf("done after deadline tone=%q want %q", got, DeadlineToneLate)
	}

	cancelled := sample(StatusCancelled)
	cancelled.RaisedAt, cancelled.DeadlineAt = raised, &deadline
	cancelledAt := ist(2026, time.September, 12, 8, 0)
	cancelled.CancelledAt = &cancelledAt
	if got := DaysTaken(cancelled, later); got != 2 {
		t.Fatalf("cancelled days=%d want 2 (frozen at cancelled_at)", got)
	}
}

// Green while the clock is inside the deadline, red the instant it passes -- the deadline is a
// date AND time, so 17:01 on the deadline day is already late.
func TestDeadlineToneFlipsAtTheDeadlineInstant(t *testing.T) {
	deadline := ist(2026, time.September, 15, 17, 0)
	task := sample(StatusInProgress)
	task.RaisedAt = ist(2026, time.September, 10, 9, 0)
	task.DeadlineAt = &deadline
	if got := DeadlineTone(task, ist(2026, time.September, 15, 17, 0)); got != DeadlineToneOK {
		t.Fatalf("at the deadline: %q want ok", got)
	}
	if got := DeadlineTone(task, ist(2026, time.September, 15, 17, 1)); got != DeadlineToneLate {
		t.Fatalf("a minute past: %q want late", got)
	}
}

// A task raised before deadlines existed carries none: no tone, no counter, and blank labels
// -- the screen shows nothing rather than a number in no colour.
func TestTaskWithoutDeadlineShowsNoCounter(t *testing.T) {
	task := sample(StatusOpen)
	now := ist(2026, time.September, 14, 9, 0)
	if got := DeadlineTone(task, now); got != "" {
		t.Fatalf("tone=%q want blank", got)
	}
	if DeadlineLabel(task.DeadlineAt) != "" || DeadlineStateLabel(task, now) != "" {
		t.Fatalf("labels must be blank without a deadline")
	}
}

func TestDeadlineCopyIsFarmWorded(t *testing.T) {
	deadline := ist(2026, time.September, 15, 17, 0)
	if got := DeadlineLabel(&deadline); got != "15/09/2026 17:00" {
		t.Fatalf("label=%q", got)
	}
	for n, want := range map[int]string{0: "0 days", 1: "1 day", 12: "12 days"} {
		if got := DaysTakenLabel(n); got != want {
			t.Fatalf("%d: %q want %q", n, got, want)
		}
	}
	open := sample(StatusOpen)
	open.RaisedAt, open.DeadlineAt = ist(2026, time.September, 10, 9, 0), &deadline
	if got := DeadlineStateLabel(open, ist(2026, time.September, 14, 9, 0)); got != "Within deadline" {
		t.Fatalf("open within: %q", got)
	}
	if got := DeadlineStateLabel(open, ist(2026, time.September, 16, 9, 0)); got != "Past deadline" {
		t.Fatalf("open late: %q", got)
	}
	done := sample(StatusDone)
	done.RaisedAt, done.DeadlineAt = open.RaisedAt, &deadline
	doneAt := ist(2026, time.September, 16, 9, 0)
	done.DoneAt = &doneAt
	if got := DeadlineStateLabel(done, ist(2026, time.October, 1, 9, 0)); got != "Finished after deadline" {
		t.Fatalf("done late: %q", got)
	}
	doneAt = ist(2026, time.September, 12, 9, 0)
	if got := DeadlineStateLabel(done, ist(2026, time.October, 1, 9, 0)); got != "Finished within deadline" {
		t.Fatalf("done within: %q", got)
	}
}

// The raiser must enter a deadline, and it must be later than the raise -- a deadline already
// behind the task the moment it is raised is a typo, not a plan.
func TestValidateDeadline(t *testing.T) {
	raised := ist(2026, time.September, 14, 9, 0)
	if err := ValidateDeadline(nil, raised, true); !errors.Is(err, ErrDeadlineRequired) {
		t.Fatalf("missing required: %v", err)
	}
	if err := ValidateDeadline(nil, raised, false); err != nil {
		t.Fatalf("missing optional: %v", err)
	}
	past := ist(2026, time.September, 14, 8, 59)
	if err := ValidateDeadline(&past, raised, true); !errors.Is(err, ErrDeadlineNotAfterRaise) {
		t.Fatalf("past: %v", err)
	}
	same := raised
	if err := ValidateDeadline(&same, raised, true); !errors.Is(err, ErrDeadlineNotAfterRaise) {
		t.Fatalf("equal: %v", err)
	}
	future := ist(2026, time.September, 14, 9, 1)
	if err := ValidateDeadline(&future, raised, true); err != nil {
		t.Fatalf("future: %v", err)
	}
}

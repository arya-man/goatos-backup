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

func withDeadline(status string, raised, deadline time.Time) Task {
	t := sample(status)
	t.RaisedAt = raised
	t.DeadlineAt = &deadline
	return t
}

// The countdown is the number of farm-calendar DAYS from today to the deadline day -- due
// tomorrow reads 1 even late tonight, due today reads 0, a day missed reads -1 -- and never
// depends on the hour the screen is opened.
func TestDaysLeftCountsFarmCalendarDaysToTheDeadlineDay(t *testing.T) {
	task := withDeadline(StatusOpen, ist(2026, time.September, 10, 9, 0), ist(2026, time.September, 15, 17, 0))
	cases := []struct {
		now  time.Time
		want int
	}{
		{ist(2026, time.September, 10, 9, 30), 5},
		{ist(2026, time.September, 14, 23, 45), 1},
		{ist(2026, time.September, 15, 0, 15), 0},
		{ist(2026, time.September, 15, 18, 0), 0},
		{ist(2026, time.September, 18, 9, 0), -3},
	}
	for _, c := range cases {
		if got := DaysLeft(task, c.now); got != c.want {
			t.Fatalf("now=%s: days left=%d want %d", c.now, got, c.want)
		}
	}
}

// Green while more than 2 days remain; red from 2 days left onward (near), and red once the
// deadline instant has passed (over) -- the deadline is a date AND time, so 17:01 on the
// deadline day is already over.
func TestDeadlineToneTurnsRedNearTheDeadlineAndStaysRedAfterIt(t *testing.T) {
	task := withDeadline(StatusInProgress, ist(2026, time.September, 10, 9, 0), ist(2026, time.September, 15, 17, 0))
	cases := []struct {
		now  time.Time
		want string
	}{
		{ist(2026, time.September, 12, 9, 0), DeadlineToneOK},   // 3 days left
		{ist(2026, time.September, 13, 9, 0), DeadlineToneNear}, // 2 days left
		{ist(2026, time.September, 15, 17, 0), DeadlineToneNear},
		{ist(2026, time.September, 15, 17, 1), DeadlineToneOver},
		{ist(2026, time.September, 20, 9, 0), DeadlineToneOver},
	}
	for _, c := range cases {
		if got := DeadlineTone(task, c.now); got != c.want {
			t.Fatalf("now=%s: tone=%q want %q", c.now, got, c.want)
		}
	}
}

// A finished task freezes its clock at the finish instant: the number and colour record
// whether it made the deadline and by how much, however much later the screen is opened.
func TestFinishedTaskFreezesTheClockAtTheFinishInstant(t *testing.T) {
	raised, deadline := ist(2026, time.September, 10, 9, 0), ist(2026, time.September, 15, 17, 0)
	later := ist(2026, time.October, 1, 9, 0)

	early := withDeadline(StatusDone, raised, deadline)
	doneAt := ist(2026, time.September, 13, 18, 0)
	early.DoneAt = &doneAt
	if got := DaysLeft(early, later); got != 2 {
		t.Fatalf("done early days=%d want 2 (frozen at done_at)", got)
	}
	if got := DeadlineStateLabel(early, later); got != "Finished 2 days early" {
		t.Fatalf("done early state=%q", got)
	}
	// Finishing 2 days early is inside the "near" window, but a finished task is never red for
	// nearness -- it made its deadline, so it is green.
	if got := DeadlineTone(early, later); got != DeadlineToneOK {
		t.Fatalf("done early tone=%q want ok", got)
	}

	late := withDeadline(StatusDone, raised, deadline)
	lateAt := ist(2026, time.September, 16, 8, 0)
	late.DoneAt = &lateAt
	if got := DeadlineTone(late, later); got != DeadlineToneOver {
		t.Fatalf("done late tone=%q", got)
	}
	if got := DeadlineStateLabel(late, later); got != "Finished 1 day late" {
		t.Fatalf("done late state=%q", got)
	}
	if got := DaysLeftLabel(late, later); got != "1 day late" {
		t.Fatalf("done late number label=%q", got)
	}
	if got := DaysLeftLabel(early, later); got != "2 days early" {
		t.Fatalf("done early number label=%q", got)
	}

	cancelled := withDeadline(StatusCancelled, raised, deadline)
	cancelledAt := ist(2026, time.September, 12, 8, 0)
	cancelled.CancelledAt = &cancelledAt
	if got := DaysLeft(cancelled, later); got != 3 {
		t.Fatalf("cancelled days=%d want 3 (frozen at cancelled_at)", got)
	}
}

// A task raised before deadlines existed carries none: no tone, no counter, blank labels.
func TestTaskWithoutDeadlineShowsNoCounter(t *testing.T) {
	task := sample(StatusOpen)
	now := ist(2026, time.September, 14, 9, 0)
	if DeadlineTone(task, now) != "" || DeadlineLabel(task.DeadlineAt) != "" || DaysLeftLabel(task, now) != "" || DeadlineStateLabel(task, now) != "" {
		t.Fatalf("everything must be blank without a deadline")
	}
}

func TestDeadlineCopyIsFarmWorded(t *testing.T) {
	deadline := ist(2026, time.September, 15, 17, 0)
	if got := DeadlineLabel(&deadline); got != "15/09/2026 17:00" {
		t.Fatalf("label=%q", got)
	}
	open := withDeadline(StatusOpen, ist(2026, time.September, 10, 9, 0), deadline)
	cases := []struct {
		now        time.Time
		wantNumber string
		wantState  string
	}{
		{ist(2026, time.September, 10, 9, 0), "5 days left", "Due in 5 days"},
		{ist(2026, time.September, 14, 9, 0), "1 day left", "Due in 1 day"},
		{ist(2026, time.September, 15, 9, 0), "Due today", "Due today"},
		{ist(2026, time.September, 15, 18, 0), "Due today, time passed", "Due today, time passed"},
		{ist(2026, time.September, 16, 9, 0), "1 day over", "Overdue by 1 day"},
		{ist(2026, time.September, 18, 9, 0), "3 days over", "Overdue by 3 days"},
	}
	for _, c := range cases {
		if got := DaysLeftLabel(open, c.now); got != c.wantNumber {
			t.Fatalf("now=%s: number label=%q want %q", c.now, got, c.wantNumber)
		}
		if got := DeadlineStateLabel(open, c.now); got != c.wantState {
			t.Fatalf("now=%s: state=%q want %q", c.now, got, c.wantState)
		}
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

// Maintainer 2026-09-25: a task FINISHED ON ITS DEADLINE DAY is on time and green, even when the
// hour had passed; only a task finished on a LATER day is late (red). The sentence no longer
// says "after the deadline", so a green badge never contradicts itself.
func TestTaskFinishedOnTheDeadlineDayIsGreen(t *testing.T) {
	raised, deadline := ist(2026, time.September, 10, 9, 0), ist(2026, time.September, 15, 9, 0)
	later := ist(2026, time.October, 1, 9, 0)
	onDay := withDeadline(StatusDone, raised, deadline)
	afterHour := ist(2026, time.September, 15, 11, 43)
	onDay.DoneAt = &afterHour
	if got := DeadlineTone(onDay, later); got != DeadlineToneOK {
		t.Fatalf("done on the deadline day after the hour: tone=%q want ok (green)", got)
	}
	if got := DeadlineStateLabel(onDay, later); got != "Finished on the day" {
		t.Fatalf("state=%q want %q", got, "Finished on the day")
	}
	if got := DaysLeftLabel(onDay, later); got != "On the day" {
		t.Fatalf("number label=%q", got)
	}
	nextDay := withDeadline(StatusDone, raised, deadline)
	dayAfter := ist(2026, time.September, 16, 8, 0)
	nextDay.DoneAt = &dayAfter
	if got := DeadlineTone(nextDay, later); got != DeadlineToneOver {
		t.Fatalf("done the day after: tone=%q want over (red)", got)
	}
}

// Maintainer 2026-09-25: a CANCELLED task shows no countdown at all -- "5 days early" on a task
// nobody finished reads as praise for work that was dropped.
func TestCancelledTaskShowsNoCountdown(t *testing.T) {
	c := withDeadline(StatusCancelled, ist(2026, time.September, 10, 9, 0), ist(2026, time.September, 30, 9, 0))
	at := ist(2026, time.September, 25, 19, 0)
	c.CancelledAt = &at
	now := ist(2026, time.September, 26, 9, 0)
	if ShowsCountdown(c) || DeadlineTone(c, now) != "" || DaysLeftLabel(c, now) != "" || DeadlineStateLabel(c, now) != "" {
		t.Fatalf("a cancelled task must carry no countdown: shows=%v tone=%q number=%q state=%q",
			ShowsCountdown(c), DeadlineTone(c, now), DaysLeftLabel(c, now), DeadlineStateLabel(c, now))
	}
	if !ShowsCountdown(withDeadline(StatusDone, ist(2026, time.September, 10, 9, 0), ist(2026, time.September, 30, 9, 0))) {
		t.Fatal("a done task with a deadline still shows its countdown")
	}
}

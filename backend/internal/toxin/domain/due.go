package domain

import "time"

// The 12-hour start clock (maintainer decision 2026-08-26).
//
// A feed load sits in the store until its strip test clears it, so a task nobody picks up is a
// bag of feed nobody can use. Twelve hours after the load is recorded the task is OVERDUE: the
// card says so, and a reminder goes to the people who run the test.
//
// This is a DURATION from the task's creation, not a business-day deadline. A load recorded at
// 22:00 is overdue at 10:00 the next morning, which is the honest answer — the feed has been
// waiting twelve hours either way. Vaccination's day-grain rule does not apply here and must not
// be copied onto it: that rule is about work PLANNED for a day, and this is an elapsed-time SLA
// on work that arrives whenever a lorry arrives.
//
// No private scheduler and no stored due column: the deadline is derived from created_at wherever
// it is needed, so it cannot drift out of step with the row, and the reminder rides the shared
// operational cadence (see notificationbridge.ToxinOverdueNotifier).
const StartDeadline = 12 * time.Hour

// DueAt is when this round becomes overdue. Zero when the task carries no creation instant.
func DueAt(createdAt time.Time) time.Time {
	if createdAt.IsZero() {
		return time.Time{}
	}
	return createdAt.Add(StartDeadline)
}

// IsOverdue reports whether a round is past its start deadline and still unfinished.
//
// The predicate is "still IN PROGRESS", not "never started": a task somebody opened, filmed two
// steps of and walked away from is exactly as overdue as one nobody touched — the load is still
// uncleared either way. Once the reading is submitted the clock stops, because the remaining wait
// is the reviewer's, and holding an operator's card red for a queue they do not control would
// blame the wrong desk.
func IsOverdue(t Task, createdAt time.Time, now time.Time) bool {
	if t.Status != StatusInProgress {
		return false
	}
	due := DueAt(createdAt)
	return !due.IsZero() && now.After(due)
}

// NotStarted reports that no working step has been completed yet. The REMINDER is narrower than
// the overdue chip and keys on this: a task somebody is visibly working through does not need a
// message telling them to start it.
func NotStarted(completions []StepCompletion) bool {
	return len(completions) == 0
}

// OverdueBy is how long past the deadline a round is, rounded down to whole hours. Used for copy
// ("Overdue by 14 hours"), never for a gate.
func OverdueBy(createdAt time.Time, now time.Time) time.Duration {
	due := DueAt(createdAt)
	if due.IsZero() || !now.After(due) {
		return 0
	}
	return now.Sub(due).Truncate(time.Hour)
}

package domain

import (
	"testing"
	"time"
)

var overdueBase = time.Date(2026, 8, 26, 6, 0, 0, 0, time.UTC)

func inProgress(createdAt time.Time) Task {
	return Task{Status: StatusInProgress, CreatedAt: createdAt.Format(time.RFC3339Nano)}
}

// TestOverdueIsTwelveHoursFromCreation pins the deadline and both edges of it. The boundary
// matters: a task exactly at twelve hours is NOT yet late, so a reminder cannot fire a tick early
// on a load that arrived on time.
func TestOverdueIsTwelveHoursFromCreation(t *testing.T) {
	created := overdueBase
	task := inProgress(created)
	for _, tc := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{"fresh", created.Add(time.Minute), false},
		{"one minute early", created.Add(StartDeadline - time.Minute), false},
		{"exactly at the deadline", created.Add(StartDeadline), false},
		{"one minute late", created.Add(StartDeadline + time.Minute), true},
		{"a day late", created.Add(36 * time.Hour), true},
	} {
		if got := IsOverdue(task, created, tc.now); got != tc.want {
			t.Fatalf("%s: IsOverdue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestTheClockStopsAtSubmit is the rule that keeps the red chip on the right desk. Once the
// reading is in, the remaining wait belongs to the reviewer, and holding an operator's card red
// for a queue they do not control would blame the wrong person.
func TestTheClockStopsAtSubmit(t *testing.T) {
	created := overdueBase
	late := created.Add(48 * time.Hour)
	for _, status := range []string{StatusPendingReview, StatusAccepted, StatusCancelled} {
		task := Task{Status: status, CreatedAt: created.Format(time.RFC3339Nano)}
		if IsOverdue(task, created, late) {
			t.Fatalf("status %q reads overdue two days on; the operator's clock stops at submit", status)
		}
	}
}

// TestOverdueChipOutranksTheStepCountdown: an operator has to see that a load has been sitting
// longer than the farm allows BEFORE they see which step is next.
func TestOverdueChipOutranksTheStepCountdown(t *testing.T) {
	created := overdueBase
	task := inProgress(created)
	fresh := StatusChip(task, nil, created.Add(time.Hour))
	if fresh == "" || fresh == "Overdue" {
		t.Fatalf("a fresh task should read as due/progress, got %q", fresh)
	}
	late := StatusChip(task, nil, created.Add(StartDeadline+3*time.Hour))
	if late != "Overdue by 3 hours" {
		t.Fatalf("late chip = %q, want \"Overdue by 3 hours\"", late)
	}
	// Two days on it reads in days, not a 60-hour number nobody parses at a glance.
	twoDays := StatusChip(task, nil, created.Add(StartDeadline+50*time.Hour))
	if twoDays != "Overdue by 2 days" {
		t.Fatalf("very late chip = %q, want \"Overdue by 2 days\"", twoDays)
	}
}

// TestAcceptedReadsCompleted pins the maintainer's wording (2026-08-26): the tester's card says
// Completed once review is done, not "Reviewed", which left them unsure whether anything was
// still owed of them.
func TestAcceptedReadsCompleted(t *testing.T) {
	accepted := Task{Status: StatusAccepted, CreatedAt: overdueBase.Format(time.RFC3339Nano)}
	if got := StatusChip(accepted, nil, overdueBase.Add(72*time.Hour)); got != "Completed" {
		t.Fatalf("accepted chip = %q, want Completed", got)
	}
	pending := Task{Status: StatusPendingReview, CreatedAt: overdueBase.Format(time.RFC3339Nano)}
	if got := StatusChip(pending, nil, overdueBase.Add(72*time.Hour)); got != "Waiting for review" {
		t.Fatalf("pending chip = %q; Completed must not appear before the verdict", got)
	}
}

// TestNotStartedIsNarrowerThanOverdue: the CARD reddens for any unfinished round at twelve hours,
// but the REMINDER only goes where nobody has begun. Telling someone visibly working through the
// steps to "start" the test is how people learn to ignore notifications.
func TestNotStartedIsNarrowerThanOverdue(t *testing.T) {
	created := overdueBase
	task := inProgress(created)
	late := created.Add(StartDeadline + time.Hour)
	partial := []StepCompletion{{StepNo: 1, CompletedAt: created.Add(time.Minute)}}

	if !IsOverdue(task, created, late) {
		t.Fatal("a part-worked round past the deadline is still overdue on the card")
	}
	if NotStarted(partial) {
		t.Fatal("a round with a completed step is started; it must not be reminded to start")
	}
	if !NotStarted(nil) {
		t.Fatal("a round with no completions has not been started")
	}
}

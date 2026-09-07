package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func openTask() Task {
	return Task{
		TaskID:      "t1",
		ParkName:    "Coimbatore",
		ShedName:    "Castro",
		Partition:   "2",
		PenLabel:    "Castro 2",
		Reasons:     []string{ReasonDeworming, ReasonVaccination},
		SourceDate:  "2026-09-06",
		PlannedDate: "2026-09-07",
		DueDate:     "2026-09-07",
		WorkState:   WorkStateScheduled,
		AssigneeID:  "u-dinakar",
		RowVersion:  1,
	}
}

// TestCopyIsFarmWordedAndBackendOwned pins every string the card renders: the pen and park in
// the title, the reasons in display order with "yesterday" when the work was the day before
// the viewer's today, the chip per state, and the tone token.
func TestCopyIsFarmWordedAndBackendOwned(t *testing.T) {
	task := openTask()
	if got := Title(task); got != "Visit Castro 2 · Coimbatore" {
		t.Fatalf("Title = %q", got)
	}
	if got := ReasonLine(task, "2026-09-07"); got != "Vaccination, deworming yesterday" {
		t.Fatalf("ReasonLine (yesterday) = %q", got)
	}
	if got := ReasonLine(task, "2026-09-09"); got != "Vaccination, deworming on "+biztime.FarmDateFromBusinessDate("2026-09-06") {
		t.Fatalf("ReasonLine (dated) = %q", got)
	}
	if got := StateChip(task, "2026-09-07"); got != "Due today" {
		t.Fatalf("StateChip due today = %q", got)
	}
	delayed := task
	delayed.WorkState = WorkStateDelayed
	delayed.DueDate = "2026-09-09"
	since := "2026-09-07"
	delayed.DelayedSince = &since
	if got := StateChip(delayed, "2026-09-09"); got == "" || got == "Due today" || got[:13] != "Delayed since" {
		t.Fatalf("StateChip delayed = %q", got)
	}
	if StateTone(delayed) != "danger" || StateTone(task) != "info" {
		t.Fatal("tones: delayed must be danger, scheduled info")
	}
	done := task
	done.WorkState = WorkStateCompleted
	at := time.Date(2026, 9, 7, 4, 30, 0, 0, time.UTC)
	done.SubmittedAt = &at
	if StateChip(done, "2026-09-07") != "Done" || StateTone(done) != "success" || DoneLine(done) == "" {
		t.Fatal("completed copy wrong")
	}
	if DoneLine(task) != "" {
		t.Fatal("an open visit has no done line")
	}
}

// TestSubmitRuleIsAssigneeOpenProofAndVersion pins CheckSubmit, the rule the write re-runs
// under the row lock: only the assignee, only while open, only with a video, only on the
// version the screen loaded.
func TestSubmitRuleIsAssigneeOpenProofAndVersion(t *testing.T) {
	task := openTask()
	me := Actor{UserID: "u-dinakar"}
	other := Actor{UserID: "u-chandrakant"}
	if err := CheckSubmit(task, me, "proof-1", 1); err != nil {
		t.Fatalf("assignee submit refused: %v", err)
	}
	if err := CheckSubmit(task, me, "proof-1", 0); err != nil {
		t.Fatalf("zero row_version must mean no fence: %v", err)
	}
	if err := CheckSubmit(task, other, "proof-1", 1); !errors.Is(err, ErrNotAssignee) {
		t.Fatalf("other person = %v, want ErrNotAssignee", err)
	}
	if err := CheckSubmit(task, me, "", 1); !errors.Is(err, ErrProofRequired) {
		t.Fatalf("no proof = %v, want ErrProofRequired", err)
	}
	if err := CheckSubmit(task, me, "proof-1", 2); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale version = %v, want ErrVersionConflict", err)
	}
	done := task
	done.WorkState = WorkStateCompleted
	if err := CheckSubmit(done, me, "proof-1", 1); !errors.Is(err, ErrAlreadyDone) {
		t.Fatalf("done = %v, want ErrAlreadyDone", err)
	}
	canceled := task
	canceled.WorkState = WorkStateCanceled
	if err := CheckSubmit(canceled, me, "proof-1", 1); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled = %v, want ErrCanceled", err)
	}
	if !task.CanSubmit(me) || task.CanSubmit(other) || done.CanSubmit(me) {
		t.Fatal("CanSubmit must agree with CheckSubmit")
	}
}

// TestFiltersAreBackendOwned pins the two chips: To do lists scheduled+delayed, Done lists
// completed, cancelled is never listed, and an unknown key falls back to To do.
func TestFiltersAreBackendOwned(t *testing.T) {
	if FilterKeyOrDefault("garbage") != FilterToDo || FilterKeyOrDefault("done") != FilterDone {
		t.Fatal("filter key normalization")
	}
	counts := map[string]int{WorkStateScheduled: 2, WorkStateDelayed: 1, WorkStateCompleted: 4, WorkStateCanceled: 9}
	if FilterCount(FilterToDo, counts) != 3 || FilterCount(FilterDone, counts) != 4 {
		t.Fatal("filter counts")
	}
	for _, key := range FilterKeys {
		for _, s := range StatesForFilter(key) {
			if s == WorkStateCanceled {
				t.Fatal("cancelled visits must never be listed")
			}
		}
		if FilterLabel(key) == "" || FilterEmptyMessage(key) == "" {
			t.Fatal("every chip carries a label and an empty message")
		}
	}
}

// TestReasonsAreOrderedDedupedAndUnknownsDropped pins the reason vocabulary handling.
func TestReasonsAreOrderedDedupedAndUnknownsDropped(t *testing.T) {
	got := SortReasons([]string{ReasonHairTrimming, "bogus", ReasonVaccination, ReasonVaccination, ReasonDeworming})
	want := []string{ReasonVaccination, ReasonDeworming, ReasonHairTrimming}
	if len(got) != len(want) {
		t.Fatalf("SortReasons = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortReasons = %v, want %v", got, want)
		}
	}
	for _, r := range []string{ReasonVaccination, ReasonDeworming, ReasonAntiProtozoan, ReasonTicksRemoval, ReasonHoofTrimming, ReasonHairTrimming} {
		if ReasonLabel(r) == r || ReasonLabel(r) == "" {
			t.Fatalf("reason %s has no farm label", r)
		}
	}
}

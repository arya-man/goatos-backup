package domain

import (
	"errors"
	"strings"
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
		Status:      StatusOpen,
		VisitorIDs:  []string{"u-dinakar", "u-second"},
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
	if got := StateChip(task, "2026-09-07"); got != "Visit pen today" {
		t.Fatalf("StateChip due today = %q", got)
	}
	delayed := task
	delayed.WorkState = WorkStateDelayed
	delayed.DueDate = "2026-09-09"
	since := "2026-09-07"
	delayed.DelayedSince = &since
	if got := StateChip(delayed, "2026-09-09"); got == "" || got == "Visit pen today" || got[:19] != "Visit delayed since" {
		t.Fatalf("StateChip delayed = %q", got)
	}
	if StateTone(delayed) != "danger" || StateTone(task) != "info" {
		t.Fatal("tones: delayed must be danger, scheduled info")
	}
	// The gate speaks before the clock (maintainer decision 2026-09-12): a submitted visit is
	// "in review" on its open clock, a rejected one says so, and only the verifier's approval
	// reads as verified -- never "Done" on submit.
	at := time.Date(2026, 9, 7, 4, 30, 0, 0, time.UTC)
	submitted := task
	submitted.Status = StatusPendingVerification
	submitted.SubmittedAt = &at
	if StateChip(submitted, "2026-09-07") != "Visit in review" || StateTone(submitted) != "review" || submitted.CanSubmit(Actor{UserID: "u-dinakar"}) {
		t.Fatalf("submitted copy wrong: %q %q", StateChip(submitted, "2026-09-07"), StateTone(submitted))
	}
	rework := submitted
	rework.Status = StatusRework
	rework.ReworkReason = "pen not visible"
	if StateChip(rework, "2026-09-07") != "Visit needs another video" || StateTone(rework) != "danger" || !rework.CanSubmit(Actor{UserID: "u-second"}) {
		t.Fatalf("rework copy wrong: %q", StateChip(rework, "2026-09-07"))
	}
	if got := Instruction(rework); got == Instruction(task) || !containsAll(got, "pen not visible", "record a new video") {
		t.Fatalf("rework instruction must carry the verifier's reason: %q", got)
	}
	done := submitted
	done.WorkState = WorkStateCompleted
	done.Status = StatusCompleted
	done.VerifiedAt = &at
	if StateChip(done, "2026-09-07") != "Visit verified" || StateTone(done) != "success" || DoneLine(done) == "" || !done.IsVerified() {
		t.Fatal("completed copy wrong")
	}
	if DoneLine(task) != "" {
		t.Fatal("an open visit has no done line")
	}
	step := StepFor(done, Actor{UserID: "u-dinakar"}, "2026-09-07")
	if !step.Verified || step.CanSubmit || step.StateChip != "Visit verified" || step.VerifiedAt == nil {
		t.Fatalf("step = %+v", step)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

// TestSubmitRuleIsAssigneeOpenProofAndVersion pins CheckSubmit, the rule the write re-runs
// under the row lock: only a configured visitor (ANY of them -- "if anyone does then enough"),
// only while a recording is owed, only with a video, only on the version the screen loaded.
func TestSubmitRuleIsAssigneeOpenProofAndVersion(t *testing.T) {
	task := openTask()
	me := Actor{UserID: "u-dinakar"}
	second := Actor{UserID: "u-second"}
	other := Actor{UserID: "u-chandrakant"}
	if err := CheckSubmit(task, me, "proof-1", 1); err != nil {
		t.Fatalf("assignee submit refused: %v", err)
	}
	if err := CheckSubmit(task, second, "proof-1", 1); err != nil {
		t.Fatalf("second configured visitor refused: %v", err)
	}
	inReview := task
	inReview.Status = StatusPendingVerification
	if err := CheckSubmit(inReview, me, "proof-1", 1); !errors.Is(err, ErrInReview) {
		t.Fatalf("in review = %v, want ErrInReview", err)
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
	done.Status = StatusCompleted
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

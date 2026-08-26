package http

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

func openRound() ports.TaskRow {
	return ports.TaskRow{
		Task: domain.Task{
			TaskID:        "98000000-0000-4000-8000-000000000001",
			RoundNo:       1,
			Origin:        "purchase",
			FarmLabel:     "CPT",
			FeedItemLabel: "Concentrate",
			Vendor:        "Siddi Srilekha",
			PurchaseDate:  "2026-08-26",
			Status:        domain.StatusInProgress,
		},
	}
}

// TestWatcherSeesNoActionableStep pins the second half of the 2026-08-26 decision. The
// permission half (CEO holds no toxin.execute) lives in the permissions package; this is
// the half the phone reads. A CEO opening the same task must get can_execute=false and a
// step list with NOTHING available, so the card is watch-only and no camera is offered.
//
// Composed payload, not a field-presence check: the defect being prevented is a step that
// renders "available" to someone whose completion the server would then refuse with 403.
func TestWatcherSeesNoActionableStep(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)

	watcher := toTaskDetailPayload(openRound(), now, false)
	if watcher.CanExecute {
		t.Fatal("a caller without toxin.execute must get can_execute=false")
	}
	for _, step := range watcher.Steps {
		if step.State == stepStateAvailable {
			t.Fatalf("step %d is actionable for a watcher; every unfinished step must be locked", step.StepNo)
		}
	}

	// The same row, same clock, for a tester: step 1 IS actionable. Without this half the
	// test would also pass if the payload locked every step for everyone.
	tester := toTaskDetailPayload(openRound(), now, true)
	if !tester.CanExecute {
		t.Fatal("a toxin.execute holder must get can_execute=true")
	}
	first, ok := stepByNo(tester.Steps, 1)
	if !ok {
		t.Fatal("step 1 is missing from the composed payload")
	}
	if first.State != stepStateAvailable {
		t.Fatalf("step 1 state for a tester = %q, want %q", first.State, stepStateAvailable)
	}
}

func stepByNo(steps []stepPayload, no int) (stepPayload, bool) {
	for _, s := range steps {
		if s.StepNo == no {
			return s, true
		}
	}
	return stepPayload{}, false
}

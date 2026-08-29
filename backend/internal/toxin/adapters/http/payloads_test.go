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

// TestFilterChipsAreBackendComposedAndCountWholeTenant pins the list's filter contract on the
// wire (maintainer decision 2026-08-26): three chips in display order, exactly one selected,
// labels owned here rather than in the client, and counts summed over WHOLE-TENANT status
// counts rather than the rows on the current page.
func TestFilterChipsAreBackendComposedAndCountWholeTenant(t *testing.T) {
	// Whole-tenant counts: 12 rounds across four statuses, while a page holds at most 20 rows.
	counts := map[string]int{
		domain.StatusInProgress:    2,
		domain.StatusPendingReview: 1,
		domain.StatusAccepted:      6,
		domain.StatusCancelled:     3,
	}

	chips := toFilterPayloads(domain.FilterPending, counts)

	if got := []string{chips[0].Key, chips[1].Key, chips[2].Key}; got[0] != domain.FilterAll ||
		got[1] != domain.FilterPending || got[2] != domain.FilterCompleted {
		t.Fatalf("chip order = %v, want all, pending, completed", got)
	}
	if chips[0].Label != "All" || chips[1].Label != "Pending" || chips[2].Label != "Completed" {
		t.Fatalf("chip labels = %q/%q/%q; the backend owns this copy", chips[0].Label, chips[1].Label, chips[2].Label)
	}

	selected := 0
	for _, c := range chips {
		if c.Selected {
			selected++
		}
	}
	if selected != 1 || !chips[1].Selected {
		t.Fatalf("want exactly Pending selected, got %d selected: %+v", selected, chips)
	}

	if chips[0].Count != 12 || chips[1].Count != 3 || chips[2].Count != 9 {
		t.Fatalf("counts = %d/%d/%d, want 12/3/9 over the whole tenant", chips[0].Count, chips[1].Count, chips[2].Count)
	}
}

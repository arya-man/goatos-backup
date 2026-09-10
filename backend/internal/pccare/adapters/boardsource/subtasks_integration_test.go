package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

func subtaskQuery(sourceID string) ports.SubtaskQuery {
	return ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, SourceID: sourceID, Limit: 10}
}

func states(steps []domain.Step) string {
	out := ""
	for _, s := range steps {
		out += string(s.State) + " "
	}
	return out
}

// TestPCCareSubtasksAreScannedAnimalsOnADatabaseRoundTrip asserts the OUTPUT of the per-animal
// drill: the scanned tag verbatim, the scan -> proof -> submit -> verify chain by what the
// animal carries, the scanner's name, the task itself when nothing is scanned, and nothing
// for a canceled task or the other park.
func TestPCCareSubtasksAreScannedAnimalsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	// Three animals scanned into the deworming task: two filmed, one not.
	page, err := src.ListSubtasks(ctx, subtaskQuery(tScanning))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Subtasks) != 3 || page.NextCursor != "" {
		t.Fatalf("scanning task %+v", page)
	}
	byName := map[string]domain.Subtask{}
	for _, st := range page.Subtasks {
		byName[st.Name] = st
		if st.WorkState != domain.WorkStateInProgress || st.Lane != domain.LaneInProgress || st.Owner.Name != "Dinakar" || st.Owner.UserID != bsOperator {
			t.Fatalf("scanned animal %+v", st)
		}
	}
	if a, ok := byName["tag-a"]; !ok || states(a.Steps) != "done done todo locked " {
		t.Fatalf("filmed animal %+v", byName["tag-a"])
	}
	if c, ok := byName["tag-c"]; !ok || states(c.Steps) != "done todo locked locked " {
		t.Fatalf("unfilmed animal %+v", byName["tag-c"])
	}

	// Submitted: the animal awaits the verdict.
	page, err = src.ListSubtasks(ctx, subtaskQuery(tSubmitted))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].Name != "tag-d" || page.Subtasks[0].WorkState != domain.WorkStateVerificationPending || states(page.Subtasks[0].Steps) != "done done done in_review " {
		t.Fatalf("submitted animal %+v", page)
	}

	// Nothing scanned: the task itself, by its category label, to do.
	page, err = src.ListSubtasks(ctx, subtaskQuery(tUntouched))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("a live task never drills into nothing: %+v", page)
	}
	whole := page.Subtasks[0]
	if whole.Name != "Hoof Trimming" || whole.Subtitle != "No animal scanned yet" || whole.WorkState != domain.WorkStateDue || states(whole.Steps) != "todo locked locked locked " {
		t.Fatalf("untouched task %+v", whole)
	}
	// Sent back, delayed and verified tasks with no scans: the task itself in that state.
	page, err = src.ListSubtasks(ctx, subtaskQuery(tRework))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || !page.Subtasks[0].NeedsAttention || page.Subtasks[0].WorkState != domain.WorkStateRejected || page.Subtasks[0].Steps[3].State != domain.StepRework {
		t.Fatalf("rework task %+v", page)
	}
	page, err = src.ListSubtasks(ctx, subtaskQuery(tDelayed))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || !page.Subtasks[0].NeedsAttention || page.Subtasks[0].WorkState != domain.WorkStateOverdue || page.Subtasks[0].Name != "Hair Trimming" {
		t.Fatalf("delayed task %+v", page)
	}
	page, err = src.ListSubtasks(ctx, subtaskQuery(tVerified))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].WorkState != domain.WorkStateCompleted || states(page.Subtasks[0].Steps) != "done done done done " {
		t.Fatalf("verified task %+v", page)
	}
	for name, q := range map[string]ports.SubtaskQuery{
		"canceled":   subtaskQuery(tCanceled),
		"other park": subtaskQuery(tOtherPark),
		"tomorrow":   subtaskQuery(tTomorrow),
	} {
		page, err := src.ListSubtasks(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 0 || len(page.Subtasks) != 0 {
			t.Fatalf("%s must drill into nothing: %+v", name, page)
		}
	}
}

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

// withoutVisit drops the "Pen visit" unit every pen task carries (2026-09-12), so the animal
// assertions below read the animal grain alone; the visit unit is asserted on its own.
func withoutVisit(page domain.SubtaskPage) domain.SubtaskPage {
	out := domain.SubtaskPage{Total: page.Total - 1, NextCursor: page.NextCursor}
	for _, st := range page.Subtasks {
		if st.Name == "Pen visit" {
			continue
		}
		out.Subtasks = append(out.Subtasks, st)
	}
	return out
}

func visitUnit(t *testing.T, page domain.SubtaskPage) domain.Subtask {
	t.Helper()
	for _, st := range page.Subtasks {
		if st.Name == "Pen visit" {
			return st
		}
	}
	t.Fatalf("every pen task drills into a Pen visit unit; got %+v", page)
	return domain.Subtask{}
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

	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Href != "" {
			t.Fatalf("PC Care has no web page yet; %s must carry no href, got %q", r.SourceID, r.Href)
		}
	}
	// Three animals scanned into the deworming task: two filmed, one not -- plus the pen
	// visit still to come, as the task's last unit.
	full, err := src.ListSubtasks(ctx, subtaskQuery(tScanning))
	if err != nil {
		t.Fatal(err)
	}
	if full.Total != 4 || len(full.Subtasks) != 4 || full.NextCursor != "" {
		t.Fatalf("scanning task %+v", full)
	}
	if v := visitUnit(t, full); v.WorkState != domain.WorkStateDue || v.Subtitle != "The day after" || states(v.Steps) != "todo locked " {
		t.Fatalf("visit unit before the visit exists %+v", v)
	}
	page := withoutVisit(full)
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
	page = withoutVisit(page)
	if len(page.Subtasks) != 1 || page.Subtasks[0].Name != "tag-d" || page.Subtasks[0].WorkState != domain.WorkStateVerificationPending || states(page.Subtasks[0].Steps) != "done done done in_review " {
		t.Fatalf("submitted animal %+v", page)
	}

	// Nothing scanned: the task itself, by its category label, to do.
	page, err = src.ListSubtasks(ctx, subtaskQuery(tUntouched))
	if err != nil {
		t.Fatal(err)
	}
	page = withoutVisit(page)
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
	page = withoutVisit(page)
	if len(page.Subtasks) != 1 || !page.Subtasks[0].NeedsAttention || page.Subtasks[0].WorkState != domain.WorkStateRejected || page.Subtasks[0].Steps[3].State != domain.StepRework {
		t.Fatalf("rework task %+v", page)
	}
	page, err = src.ListSubtasks(ctx, subtaskQuery(tDelayed))
	if err != nil {
		t.Fatal(err)
	}
	page = withoutVisit(page)
	if len(page.Subtasks) != 1 || !page.Subtasks[0].NeedsAttention || page.Subtasks[0].WorkState != domain.WorkStateOverdue || page.Subtasks[0].Name != "Hair Trimming" {
		t.Fatalf("delayed task %+v", page)
	}
	page, err = src.ListSubtasks(ctx, subtaskQuery(tVerified))
	if err != nil {
		t.Fatal(err)
	}
	page = withoutVisit(page)
	if len(page.Subtasks) != 1 || page.Subtasks[0].WorkState != domain.WorkStateCompleted || states(page.Subtasks[0].Steps) != "done done done done " {
		t.Fatalf("verified task %+v", page)
	}
	// The visit unit carries the visit row's own state once it exists: recorded, with the
	// verifier, named by the visitor, and it sorts after the done animal (worst first).
	full, err = src.ListSubtasks(ctx, subtaskQuery(tVisitReview))
	if err != nil {
		t.Fatal(err)
	}
	if full.Total != 2 || len(full.Subtasks) != 2 {
		t.Fatalf("visit-in-review task %+v", full)
	}
	if v := visitUnit(t, full); v.WorkState != domain.WorkStateVerificationPending || states(v.Steps) != "done in_review " || v.Owner.Name != "Dinakar" || v.Subtitle != "Due 11/09/2026" {
		t.Fatalf("visit unit in review %+v", v)
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

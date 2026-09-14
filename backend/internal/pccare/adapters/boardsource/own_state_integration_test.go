package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// TestPCCareBoardOneToManyParkScopePaginationStatusMatrixReadsTheTasksOwnState pins the
// 2026-09-14 reshape: the PC Care row reads the task's OWN status and clock and nothing of the
// pen visit (which rows on its own under Tasks). Status matrix over the seeded tasks (open,
// submitted, verified, sent back, delayed, closed); one-to-many -- a task with two assignees is
// one row; park scope -- the other park's task never appears; pagination -- a page of one
// walks every row once.
func TestPCCareBoardOneToManyParkScopePaginationStatusMatrixReadsTheTasksOwnState(t *testing.T) {
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
	got := byTask(rows)
	if _, leaked := got[tOtherPark]; leaked {
		t.Fatal("the other park's task leaked into this park's board")
	}
	if _, listed := got[tCanceled]; listed {
		t.Fatal("a canceled task is not work")
	}
	want := map[string]domain.WorkState{
		tScanning: domain.WorkStateInProgress, tUntouched: domain.WorkStateDue,
		tSubmitted: domain.WorkStateVerificationPending, tVerified: domain.WorkStateCompleted,
		tRework: domain.WorkStateRejected, tDelayed: domain.WorkStateOverdue, tClosed: domain.WorkStateCompleted,
	}
	for id, state := range want {
		r, ok := got[id]
		if !ok || r.WorkState != state {
			t.Fatalf("%s: ok=%v state=%s, want %s", id, ok, r.WorkState, state)
		}
		if r.Module != domain.ModulePCCare {
			t.Fatalf("%s rows under %s", id, r.Module)
		}
	}
	// One-to-many: two assignees, one row, "+1" on the owner.
	if got[tScanning].Owner.Name != "Dinakar +1" {
		t.Fatalf("two-assignee owner %q", got[tScanning].Owner.Name)
	}
	// The verified task's subtitle is its own animals only: no pen-visit line (2026-09-14).
	if got[tVerified].Subtitle != "" && got[tVerified].Subtitle != "1 animal" {
		t.Fatalf("verified subtitle %q carries something beyond the task's own work", got[tVerified].Subtitle)
	}

	seen := map[string]bool{}
	after := ""
	for i := 0; i < 20; i++ {
		q := query("")
		q.Limit = 1
		q.AfterSourceID = after
		page, err := src.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if page[0].SourceID <= after || seen[page[0].SourceID] {
			t.Fatalf("keyset broken at %s after %s", page[0].SourceID, after)
		}
		seen[page[0].SourceID] = true
		after = page[0].SourceID
	}
	if len(seen) != len(rows) {
		t.Fatalf("keyset walk saw %d rows, list has %d", len(seen), len(rows))
	}
}

package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// TestPenVisitBoardOneToManyParkScopePaginationStatusMatrix pins the 2026-09-14 reshape on a
// real database: ONE source under the Tasks module for every visit whatever raised it, so (a)
// one-to-many -- a park with two configured visitors still rows each visit exactly once, and a
// pen both vaccinated and dewormed is one row; (b) park scope -- the other park's visit never
// appears and the visitor lens keeps every row of the park; (c) pagination -- a page of one
// walks every row once under a monotonic cursor; (d) the status matrix -- open, delayed, in
// review, sent back and verified each land in exactly one board state, and the counts agree
// with the rows.
func TestPenVisitBoardOneToManyParkScopePaginationStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	// (a) One-to-many: two configured visitors, six visits, six rows -- every one under Tasks.
	rows, err := src.ListRows(ctx, query(""))
	if err != nil || len(rows) != 6 {
		t.Fatalf("rows = %d err %v, want 6 (one per visit, never one per visitor)", len(rows), err)
	}
	for _, r := range rows {
		if r.Module != domain.ModuleTasks {
			t.Fatalf("%s rows under %s, want tasks", r.SourceID, r.Module)
		}
	}
	got := byID(rows)
	if _, dup := got[vOtherPark]; dup {
		t.Fatal("the other park's visit leaked into this park's board")
	}

	// (b) Park scope: the visitor lens keeps the park's rows; an unconfigured person sees none.
	if mine, err := src.ListRows(ctx, query(bsDinakar)); err != nil || len(mine) != 6 {
		t.Fatalf("configured visitor lens = %d err %v, want 6", len(mine), err)
	}
	if none, err := src.ListRows(ctx, query(bsOther)); err != nil || len(none) != 0 {
		t.Fatalf("unconfigured person lens = %d err %v, want 0", len(none), err)
	}

	// (d) Status matrix: each fixture state lands in exactly one board state.
	want := map[string]domain.WorkState{
		vOwed: domain.WorkStateDue, vVaccOnly: domain.WorkStateDue, vLate: domain.WorkStateOverdue,
		vInReview: domain.WorkStateVerificationPending, vSentBack: domain.WorkStateRejected, vVerified: domain.WorkStateCompleted,
	}
	for id, state := range want {
		if got[id].WorkState != state {
			t.Fatalf("%s state = %s, want %s", id, got[id].WorkState, state)
		}
	}
	counts, err := src.CountByState(ctx, query(""))
	if err != nil || sum(counts) != 6 || counts[domain.WorkStateDue] != 2 {
		t.Fatalf("counts %+v err %v", counts, err)
	}

	// (c) Pagination: a page of one reaches every row exactly once.
	seen := map[string]bool{}
	after := ""
	for i := 0; i < 10; i++ {
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
	if len(seen) != 6 {
		t.Fatalf("keyset walk saw %d rows, want 6", len(seen))
	}
}

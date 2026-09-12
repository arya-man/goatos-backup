package boardsource

import (
	"context"
	"testing"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// TestFeedActivitySubtasksListPensWorstFirst: opening the transport card drills into its four
// pens, worst first -- the sent-back pen (needs attention), then the owed pen, the in-review
// pen, and the approved pen -- each carrying its own state and film/verify steps.
func TestFeedActivitySubtasksListPensWorstFirst(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5000000000)

	page, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, SourceID: "transport", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || len(page.Subtasks) != 4 {
		t.Fatalf("transport has four pens, got total=%d rows=%d", page.Total, len(page.Subtasks))
	}
	type want struct {
		name  string
		state domain.WorkState
		lane  domain.Lane
		attn  bool
	}
	wants := []want{
		{"Mandela 1", domain.WorkStateRejected, domain.LaneInProgress, true}, // sent back -> worst
		{"Godel 1", domain.WorkStateDue, domain.LaneToDo, false},             // owed
		{"Castro", domain.WorkStateVerificationPending, domain.LaneInReview, false},
		{"Yashoda 2", domain.WorkStateCompleted, domain.LaneDone, false},
	}
	for i, w := range wants {
		s := page.Subtasks[i]
		if s.Name != w.name || s.WorkState != w.state || s.Lane != w.lane || s.NeedsAttention != w.attn {
			t.Errorf("pen %d: name=%q state=%s lane=%s attn=%v, want %+v", i, s.Name, s.WorkState, s.Lane, s.NeedsAttention, w)
		}
		if len(s.Steps) != 2 {
			t.Errorf("pen %d: want film+verify steps, got %d", i, len(s.Steps))
		}
	}

	// Keyset: paging AFTER the first pen returns exactly the remaining three, in order.
	afterFirst, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, SourceID: "transport", AfterKey: page.Subtasks[0].Key, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if afterFirst.Total != 4 || len(afterFirst.Subtasks) != 3 {
		t.Fatalf("after the first pen: total=%d rows=%d, want 4/3", afterFirst.Total, len(afterFirst.Subtasks))
	}
	if afterFirst.Subtasks[0].Name != page.Subtasks[1].Name {
		t.Fatalf("keyset skipped or repeated: got %q, want %q", afterFirst.Subtasks[0].Name, page.Subtasks[1].Name)
	}

	// An unknown activity key resolves to an empty page, never an error.
	empty, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, SourceID: "not-an-activity", Limit: 10})
	if err != nil {
		t.Fatalf("unknown activity should be empty, not error: %v", err)
	}
	if empty.Total != 0 || len(empty.Subtasks) != 0 {
		t.Fatalf("unknown activity page %+v", empty)
	}
}
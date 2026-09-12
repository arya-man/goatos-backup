package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// fakeSource is an in-memory source: rows keyed by source_id, returned in order after the
// keyset boundary. It records every call so a test can assert which sources a page touched.
type fakeSource struct {
	module   domain.Module
	kind     string
	rows     []domain.Row
	calls    []ports.SourceQuery
	failWith error // when set, ListRows and CountByState return it (a slow/broken source)

	subtasks     []domain.Subtask
	subtaskCalls []ports.SubtaskQuery
}

func (f *fakeSource) Module() domain.Module { return f.module }
func (f *fakeSource) SourceType() string    { return f.kind }
func (f *fakeSource) ListRows(_ context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	f.calls = append(f.calls, q)
	out := []domain.Row{}
	for _, r := range f.rows {
		if q.AfterSourceID != "" && r.SourceID <= q.AfterSourceID {
			continue
		}
		if q.OwnerUserID != "" && r.Owner.UserID != q.OwnerUserID {
			continue
		}
		out = append(out, r)
		if len(out) == q.Limit {
			break
		}
	}
	return out, nil
}
func (f *fakeSource) CountByState(_ context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	out := map[domain.WorkState]int{}
	for _, r := range f.rows {
		if q.OwnerUserID != "" && r.Owner.UserID != q.OwnerUserID {
			continue
		}
		out[r.WorkState]++
	}
	return out, nil
}

// ListSubtasks serves the fake's subtasks (set by a test), recording the query.
func (f *fakeSource) ListSubtasks(_ context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	f.subtaskCalls = append(f.subtaskCalls, q)
	out := []domain.Subtask{}
	for _, st := range f.subtasks {
		if q.AfterKey != "" && st.Key <= q.AfterKey {
			continue
		}
		out = append(out, st)
	}
	page := domain.SubtaskPage{Subtasks: out, Total: len(f.subtasks)}
	if len(out) > q.Limit {
		page.Subtasks = out[:q.Limit]
		page.NextCursor = out[q.Limit-1].Key
	}
	return page, nil
}

func mk(module domain.Module, kind string, n int, state domain.WorkState, owner string) *fakeSource {
	f := &fakeSource{module: module, kind: kind}
	for i := 1; i <= n; i++ {
		f.rows = append(f.rows, domain.Row{
			Module: module, SourceType: kind, SourceID: fmt.Sprintf("%s-%03d", kind, i),
			WorkState: state, Owner: domain.Owner{UserID: owner},
		}.Finalize())
	}
	sort.Slice(f.rows, func(i, j int) bool { return f.rows[i].SourceID < f.rows[j].SourceID })
	return f
}

func baseQuery() domain.Query {
	return domain.Query{TenantID: "t", ParkID: "p", BusinessDate: "2026-09-10", Limit: 5}
}

func keys(rows []domain.Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.RowKey)
	}
	return out
}

// TestListWalksSourcesInBoardOrderWithOneGlobalCursor pins the keyset contract: a page
// touches the source the cursor names and the ones after it, never earlier ones, and the
// concatenation of pages equals the full board with no gaps and no duplicates.
func TestListWalksSourcesInBoardOrderWithOneGlobalCursor(t *testing.T) {
	// Registered out of order on purpose; the service must sort into board order.
	weighing := mk(domain.ModuleWeighing, "bucket", 3, domain.WorkStateScheduled, "u1")
	feed := mk(domain.ModuleFeed, "transport", 4, domain.WorkStateDue, "u1")
	verify := mk(domain.ModuleVerification, "item", 6, domain.WorkStateVerificationPending, "u2")
	svc := NewService(weighing, verify, feed)

	all := []string{}
	q := baseQuery()
	pages := 0
	for {
		page, err := svc.List(context.Background(), q)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		pages++
		all = append(all, keys(page.Rows)...)
		if page.NextCursor == "" {
			break
		}
		c, err := domain.ParseCursor(page.NextCursor)
		if err != nil {
			t.Fatalf("cursor: %v", err)
		}
		q.Cursor = c
		if pages > 10 {
			t.Fatal("cursor never terminated")
		}
	}
	want := []string{}
	for _, s := range []*fakeSource{feed, weighing, verify} {
		want = append(want, keys(s.rows)...)
	}
	if strings.Join(all, ",") != strings.Join(want, ",") {
		t.Fatalf("board order/gaps wrong\n got %v\nwant %v", all, want)
	}
	if pages != 3 {
		t.Fatalf("13 rows at 5 per page should be 3 pages, got %d", pages)
	}
	// Page 2 started inside feed (after transport-004? no: feed has 4, page 1 took 4 feed + 1
	// weighing). So page 2's cursor names weighing/bucket-001 and feed must NOT be re-read.
	feedCalls := len(feed.calls)
	if feedCalls != 1 {
		t.Fatalf("feed should be read on page 1 only, was read %d times", feedCalls)
	}
}

// TestListCursorNamesSourceNotJustID: two sources can share an id space; the cursor carries
// module and source type so the walk resumes in the right one.
func TestListCursorNamesSourceNotJustID(t *testing.T) {
	a := mk(domain.ModuleCounts, "approval", 2, domain.WorkStateDue, "")
	b := mk(domain.ModuleCounts, "shifting", 2, domain.WorkStateDue, "")
	// Same ids in both sources.
	for i := range b.rows {
		b.rows[i].SourceID = a.rows[i].SourceID
		b.rows[i] = b.rows[i].Finalize()
	}
	svc := NewService(b, a)
	q := baseQuery()
	q.Limit = 3
	p1, err := svc.List(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := domain.ParseCursor(p1.NextCursor)
	if c.Module != domain.ModuleCounts || c.SourceType != "shifting" {
		t.Fatalf("cursor should name counts/shifting, got %+v", c)
	}
	q.Cursor = c
	p2, err := svc.List(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Rows)+len(p2.Rows) != 4 {
		t.Fatalf("expected 4 rows across two pages, got %d + %d", len(p1.Rows), len(p2.Rows))
	}
}

// TestListModuleFilterSkipsSourcesEntirely: a filtered-out source is never called.
func TestListModuleFilterSkipsSourcesEntirely(t *testing.T) {
	feed := mk(domain.ModuleFeed, "transport", 2, domain.WorkStateDue, "")
	health := mk(domain.ModuleHealth, "session", 2, domain.WorkStateDue, "")
	svc := NewService(feed, health)
	q := baseQuery()
	q.Modules = []domain.Module{domain.ModuleHealth}
	page, err := svc.List(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.calls) != 0 {
		t.Fatal("feed must not be read when filtered out")
	}
	if len(page.Rows) != 2 || page.Rows[0].Module != domain.ModuleHealth {
		t.Fatalf("expected the two health rows, got %v", keys(page.Rows))
	}
}

// TestListOwnerScopeReachesEverySource: the operator lens is applied by every source.
func TestListOwnerScopeReachesEverySource(t *testing.T) {
	feed := mk(domain.ModuleFeed, "transport", 3, domain.WorkStateDue, "u1")
	feed.rows[1].Owner.UserID = "u2"
	svc := NewService(feed)
	q := baseQuery()
	q.OwnerUserID = "u2"
	page, err := svc.List(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.Rows[0].Owner.UserID != "u2" {
		t.Fatalf("expected only u2's row, got %v", keys(page.Rows))
	}
	if feed.calls[0].OwnerUserID != "u2" {
		t.Fatal("owner scope must be pushed down to the source, not filtered after")
	}
}

// TestSummaryIsWholeFilterAndLaneDerived: lane counts come from the states, across every
// source in scope, independent of page size.
func TestSummaryIsWholeFilterAndLaneDerived(t *testing.T) {
	feed := mk(domain.ModuleFeed, "transport", 7, domain.WorkStateCompleted, "")
	feed.rows[0].WorkState = domain.WorkStateOverdue
	weighing := mk(domain.ModuleWeighing, "bucket", 2, domain.WorkStateVerificationPending, "")
	svc := NewService(feed, weighing)
	q := baseQuery()
	q.Limit = 1 // page size must not matter
	sum, err := svc.Summary(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 9 {
		t.Fatalf("total %d", sum.Total)
	}
	if sum.ByLane[domain.LaneDone] != 6 || sum.ByLane[domain.LaneInReview] != 2 || sum.ByLane[domain.LaneToDo] != 1 {
		t.Fatalf("lanes %+v", sum.ByLane)
	}
	if sum.Attention != 1 {
		t.Fatalf("attention %d", sum.Attention)
	}
	if sum.ByModule[domain.ModuleFeed] != 7 || sum.ByModule[domain.ModuleWeighing] != 2 {
		t.Fatalf("by module %+v", sum.ByModule)
	}
	if sum.ByLane[domain.LaneInProgress] != 0 {
		t.Fatal("every lane must be present, even at zero")
	}
}

// TestInvalidCursorIsRefusedNotIgnored: a cursor naming an unregistered source is an error,
// never silently the first page (which would re-serve rows the client already has).
func TestInvalidCursorIsRefusedNotIgnored(t *testing.T) {
	svc := NewService(mk(domain.ModuleFeed, "transport", 1, domain.WorkStateDue, ""))
	q := baseQuery()
	q.Cursor = domain.Cursor{Module: domain.ModuleHealth, SourceType: "session", SourceID: "x"}
	if _, err := svc.List(context.Background(), q); err == nil {
		t.Fatal("expected ErrInvalidCursor")
	}
}

// TestVisibleModulesFollowsModulePermissions: a Feed Director's permission set opens feed
// and only feed; the CEO's opens everything; an operator's opens what it executes.
func TestVisibleModulesFollowsModulePermissions(t *testing.T) {
	feedDirector := VisibleModules([]string{permissions.FeedDirectionRead, permissions.WorkBoardRead, permissions.WorkBoardOversee})
	if len(feedDirector) != 1 || feedDirector[0] != domain.ModuleFeed {
		t.Fatalf("feed director sees %v", feedDirector)
	}
	none := VisibleModules([]string{permissions.WorkBoardRead})
	if len(none) != 0 {
		t.Fatalf("work_board.read alone must open no module, got %v", none)
	}
	everything := VisibleModules(VisibilityPermissions())
	if len(everything) != len(domain.Modules()) {
		t.Fatalf("all visibility permissions should open every module, got %v", everything)
	}
	// Intersection keeps request order out of it: board order wins.
	got := IntersectModules([]domain.Module{domain.ModuleWeighing, domain.ModuleFeed}, everything)
	if len(got) != 2 || got[0] != domain.ModuleWeighing {
		t.Fatalf("intersect keeps the requested modules the caller may see: %v", got)
	}
	if len(IntersectModules([]domain.Module{domain.ModuleHealth}, feedDirector)) != 0 {
		t.Fatal("a request for an invisible module resolves to nothing, not to everything")
	}
}

// TestFindRowStaysInsideTheCallersBoard: a key resolves only on the board the query
// describes. A module outside the caller's visibility, a source the board does not have, or
// an id the source does not hold all answer not-found; a bad key is refused, not swallowed;
// and the walk pages the ONE named source in keyset order until the key matches.
func TestFindRowStaysInsideTheCallersBoard(t *testing.T) {
	weighing := mk(domain.ModuleWeighing, "weighing_work_item", 250, domain.WorkStateDue, "u1")
	feed := mk(domain.ModuleFeed, "feed_transport_task", 3, domain.WorkStateDue, "u2")
	svc := NewService(feed, weighing)

	q := domain.Query{TenantID: "t", ParkID: "p", BusinessDate: "2026-09-10", Modules: []domain.Module{domain.ModuleWeighing, domain.ModuleFeed}}
	row, found, err := svc.FindRow(context.Background(), q, "weighing|weighing_work_item|weighing_work_item-237")
	if err != nil || !found || row.SourceID != "weighing_work_item-237" {
		t.Fatalf("expected row 237, got found=%v row=%+v err=%v", found, row, err)
	}
	if len(feed.calls) != 0 {
		t.Fatal("only the named source may be read")
	}
	if len(weighing.calls) != 3 || weighing.calls[1].AfterSourceID != "weighing_work_item-100" {
		t.Fatalf("expected a keyset walk of the named source, got %d calls: %+v", len(weighing.calls), weighing.calls)
	}

	// The feed director's board holds feed only: the same weighing key is not there.
	director := domain.Query{TenantID: "t", ParkID: "p", BusinessDate: "2026-09-10", Modules: []domain.Module{domain.ModuleFeed}}
	if _, found, err := svc.FindRow(context.Background(), director, "weighing|weighing_work_item|weighing_work_item-237"); err != nil || found {
		t.Fatalf("a module outside the caller's visibility must not resolve: found=%v err=%v", found, err)
	}
	if _, found, err := svc.FindRow(context.Background(), q, "feed|feed_transport_task|feed_transport_task-009"); err != nil || found {
		t.Fatalf("an id the source does not hold must not resolve: found=%v err=%v", found, err)
	}
	if _, found, err := svc.FindRow(context.Background(), q, "health|health_treatment_session|h1"); err != nil || found {
		t.Fatalf("a source the board does not have must not resolve: found=%v err=%v", found, err)
	}
	if _, _, err := svc.FindRow(context.Background(), q, "not-a-key"); !errors.Is(err, domain.ErrInvalidRowKey) {
		t.Fatalf("a bad key is refused, got %v", err)
	}
}

// TestListSubtasksResolvesTheRowsSourceAndBoundsThePage: the key names the source, the
// module must be visible on the caller's board, and the page size is clamped to [10, 50].
func TestListSubtasksResolvesTheRowsSourceAndBoundsThePage(t *testing.T) {
	feed := mk(domain.ModuleFeed, "feed_transport_task", 1, domain.WorkStateDue, "u1")
	weighing := mk(domain.ModuleWeighing, "weighing_work_item", 1, domain.WorkStateDue, "u1")
	for i := 0; i < 12; i++ {
		weighing.subtasks = append(weighing.subtasks, domain.Subtask{
			Key: domain.SubtaskKey(domain.RankToDo, fmt.Sprintf("obs-%02d", i)), Name: fmt.Sprintf("tag-%02d", i),
			WorkState: domain.WorkStateDue, Steps: []domain.Step{{Name: "Scan", State: domain.StepTodo}},
		}.Finalize())
	}
	svc := NewService(feed, weighing)

	page, err := svc.ListSubtasks(context.Background(), baseQuery(), "weighing|weighing_work_item|weighing_work_item-001", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(weighing.subtaskCalls) != 1 || len(feed.subtaskCalls) != 0 {
		t.Fatalf("exactly the named source is asked: weighing=%d feed=%d", len(weighing.subtaskCalls), len(feed.subtaskCalls))
	}
	got := weighing.subtaskCalls[0]
	if got.SourceID != "weighing_work_item-001" || got.TenantID != "t" || got.ParkID != "p" || got.BusinessDate != "2026-09-10" {
		t.Fatalf("query %+v", got)
	}
	owned := baseQuery()
	owned.OwnerUserID = "u1"
	if _, err := svc.ListSubtasks(context.Background(), owned, "weighing|weighing_work_item|weighing_work_item-001", "", 0); err != nil {
		t.Fatal(err)
	}
	if got := weighing.subtaskCalls[len(weighing.subtaskCalls)-1]; got.OwnerUserID != "u1" {
		t.Fatalf("subtask query lost owner lens: %+v", got)
	}
	if got.Limit != domain.DefaultSubtaskLimit {
		t.Fatalf("a zero limit is the default page of %d, got %d", domain.DefaultSubtaskLimit, got.Limit)
	}
	if len(page.Subtasks) != 10 || page.Total != 12 || page.NextCursor != page.Subtasks[9].Key {
		t.Fatalf("page: %d subtasks, total %d, next %q", len(page.Subtasks), page.Total, page.NextCursor)
	}
	// The cursor is handed to the source verbatim; the next page starts after it.
	page2, err := svc.ListSubtasks(context.Background(), baseQuery(), "weighing|weighing_work_item|weighing_work_item-001", page.NextCursor, 999)
	if err != nil {
		t.Fatal(err)
	}
	if weighing.subtaskCalls[2].AfterKey != page.NextCursor || weighing.subtaskCalls[2].Limit != domain.MaxSubtaskLimit {
		t.Fatalf("second call %+v", weighing.subtaskCalls[2])
	}
	if len(page2.Subtasks) != 2 || page2.NextCursor != "" {
		t.Fatalf("second page: %d subtasks, next %q", len(page2.Subtasks), page2.NextCursor)
	}

	// A module outside the caller's board is not found, and the source is never asked.
	beforeInvisible := len(weighing.subtaskCalls)
	q := baseQuery()
	q.Modules = []domain.Module{domain.ModuleFeed}
	if _, err := svc.ListSubtasks(context.Background(), q, "weighing|weighing_work_item|weighing_work_item-001", "", 0); !errors.Is(err, ErrRowNotFound) {
		t.Fatalf("invisible module must be not found, got %v", err)
	}
	if len(weighing.subtaskCalls) != beforeInvisible {
		t.Fatalf("an invisible module's source must not be asked, calls=%d", len(weighing.subtaskCalls))
	}
	// An unregistered source is not found; a malformed key and a malformed cursor are refused.
	if _, err := svc.ListSubtasks(context.Background(), baseQuery(), "toxin|toxin_task|x", "", 0); !errors.Is(err, ErrRowNotFound) {
		t.Fatalf("unregistered source: %v", err)
	}
	if _, err := svc.ListSubtasks(context.Background(), baseQuery(), "garbage", "", 0); !errors.Is(err, domain.ErrInvalidRowKey) {
		t.Fatalf("bad key: %v", err)
	}
	if _, err := svc.ListSubtasks(context.Background(), baseQuery(), "weighing|weighing_work_item|weighing_work_item-001", "nope", 0); !errors.Is(err, domain.ErrInvalidSubtaskCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
}

// TestSubtaskKeysSortWorstFirst pins the rank table the sources' SQL mirrors.
func TestSubtaskKeysSortWorstFirst(t *testing.T) {
	cases := []struct {
		state     domain.WorkState
		attention bool
		want      int
	}{
		{domain.WorkStateRejected, true, domain.RankNeedsAttention},
		{domain.WorkStateDue, false, domain.RankToDo},
		{domain.WorkStateScheduled, false, domain.RankToDo},
		{domain.WorkStateInProgress, false, domain.RankInProgress},
		{domain.WorkStateVerificationPending, false, domain.RankInReview},
		{domain.WorkStateCompleted, false, domain.RankDone},
	}
	prev := ""
	for _, c := range cases {
		key := domain.SubtaskKey(domain.RankFor(c.state, c.attention), "x")
		if domain.RankFor(c.state, c.attention) != c.want {
			t.Errorf("%s/%v: rank %d want %d", c.state, c.attention, domain.RankFor(c.state, c.attention), c.want)
		}
		if key < prev {
			t.Errorf("keys must sort worst first: %q before %q", prev, key)
		}
		prev = key
		rank, id, err := domain.ParseSubtaskKey(key)
		if err != nil || rank != c.want || id != "x" {
			t.Errorf("round trip %q: %d %q %v", key, rank, id, err)
		}
	}
	for _, bad := range []string{"x", "9:x", "1:", ":x"} {
		if _, _, err := domain.ParseSubtaskKey(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if domain.BoundSubtaskLimit(3) != domain.DefaultSubtaskLimit || domain.BoundSubtaskLimit(500) != domain.MaxSubtaskLimit || domain.BoundSubtaskLimit(25) != 25 {
		t.Error("limit bounds")
	}
}

// TestFindRowRefusesEverythingWhenTheBoardHasNoModules pins the service half of the same
// rule: a query carrying NoModules resolves no row at all, whatever key is asked for.
func TestFindRowRefusesEverythingWhenTheBoardHasNoModules(t *testing.T) {
	weighing := mk(domain.ModuleWeighing, "weighing_work_item", 3, domain.WorkStateDue, "u1")
	svc := NewService(weighing)
	q := domain.Query{TenantID: "t", ParkID: "p", BusinessDate: "2026-09-10", NoModules: true}
	if _, found, err := svc.FindRow(context.Background(), q, "weighing|weighing_work_item|weighing_work_item-002"); err != nil || found {
		t.Fatalf("a board with no modules must resolve nothing: found=%v err=%v", found, err)
	}
	if len(weighing.calls) != 0 {
		t.Fatal("no source may be read for a board with no modules")
	}
	if got := StrictIntersect(nil, []domain.Module{domain.ModuleFeed}); len(got) != 0 {
		t.Fatalf("StrictIntersect must never read empty as all, got %v", got)
	}
}

// TestOneBrokenSourceDegradesInsteadOfBlankingTheBoard: a single source that errors (the heavy
// process-integrity read timing out) must never fail the whole board. List serves every healthy
// source's rows and names the broken module in Degraded; Summary sums the healthy sources and
// names the broken one too. Neither returns an error.
func TestOneBrokenSourceDegradesInsteadOfBlankingTheBoard(t *testing.T) {
	weighing := mk(domain.ModuleWeighing, "bucket", 2, domain.WorkStateDue, "u1")
	feed := mk(domain.ModuleFeed, "activity", 3, domain.WorkStateVerificationPending, "u1")
	broken := &fakeSource{module: domain.ModuleVaccination, kind: "drive", failWith: context.DeadlineExceeded}
	svc := NewService(weighing, feed, broken)

	q := baseQuery()
	q.Limit = 25
	page, err := svc.List(context.Background(), q)
	if err != nil {
		t.Fatalf("the board must not fail for one broken source: %v", err)
	}
	if len(page.Rows) != 5 {
		t.Fatalf("healthy sources still serve their rows: got %d, want 5", len(page.Rows))
	}
	if len(page.Degraded) != 1 || page.Degraded[0] != domain.ModuleVaccination {
		t.Fatalf("the broken module must be reported degraded: %v", page.Degraded)
	}

	sum, err := svc.Summary(context.Background(), baseQuery())
	if err != nil {
		t.Fatalf("summary must not fail for one broken source: %v", err)
	}
	if sum.Total != 5 || sum.ByModule[domain.ModuleWeighing] != 2 || sum.ByModule[domain.ModuleFeed] != 3 {
		t.Fatalf("summary sums the healthy sources: total=%d byModule=%v", sum.Total, sum.ByModule)
	}
	if len(sum.Degraded) != 1 || sum.Degraded[0] != domain.ModuleVaccination {
		t.Fatalf("summary must report the broken module degraded: %v", sum.Degraded)
	}
}

// TestBadCursorStillFailsTheRead: a client-supplied bad cursor is a 400, never degraded away.
func TestBadCursorStillFailsTheRead(t *testing.T) {
	// A source whose ListRows returns ErrInvalidCursor (a garbage keyset id) must propagate.
	bad := &fakeSource{module: domain.ModuleWeighing, kind: "bucket", failWith: domain.ErrInvalidCursor}
	svc := NewService(bad)
	if _, err := svc.List(context.Background(), baseQuery()); err == nil {
		t.Fatal("an invalid cursor must fail the read, not degrade")
	}
}

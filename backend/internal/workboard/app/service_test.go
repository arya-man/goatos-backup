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
	module domain.Module
	kind   string
	rows   []domain.Row
	calls  []ports.SourceQuery
}

func (f *fakeSource) Module() domain.Module { return f.module }
func (f *fakeSource) SourceType() string    { return f.kind }
func (f *fakeSource) ListRows(_ context.Context, q ports.SourceQuery) ([]domain.Row, error) {
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
	out := map[domain.WorkState]int{}
	for _, r := range f.rows {
		if q.OwnerUserID != "" && r.Owner.UserID != q.OwnerUserID {
			continue
		}
		out[r.WorkState]++
	}
	return out, nil
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

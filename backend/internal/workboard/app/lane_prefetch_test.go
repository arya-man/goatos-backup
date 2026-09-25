package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

type prefetchSource struct {
	*fakeSource
	lists        atomic.Int32
	countErr     error
	failFirstRow bool
	empty        bool
	countWait    <-chan struct{}
	rowStarted   chan struct{}
	once         sync.Once
	active, max  *atomic.Int32
}

func (f *prefetchSource) enter() func() {
	if f.active == nil {
		return func() {}
	}
	n := f.active.Add(1)
	for {
		old := f.max.Load()
		if n <= old || f.max.CompareAndSwap(old, n) {
			break
		}
	}
	return func() { f.active.Add(-1) }
}
func (f *prefetchSource) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	defer f.enter()()
	if f.countWait != nil {
		select {
		case <-f.countWait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.active != nil {
		time.Sleep(5 * time.Millisecond)
	}
	if f.countErr != nil {
		return nil, f.countErr
	}
	if f.empty {
		return map[domain.WorkState]int{}, nil
	}
	return map[domain.WorkState]int{domain.WorkStateDue: len(f.rows)}, nil
}
func (f *prefetchSource) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	defer f.enter()()
	n := f.lists.Add(1)
	if f.rowStarted != nil {
		f.once.Do(func() { close(f.rowStarted) })
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.failFirstRow && n == 1 {
		return nil, errors.New("transient row failure")
	}
	if f.active != nil {
		time.Sleep(5 * time.Millisecond)
	}
	rows := []domain.Row{}
	for _, r := range f.rows {
		if q.AfterSourceID != "" && r.SourceID <= q.AfterSourceID {
			continue
		}
		rows = append(rows, r)
		if len(rows) == q.Limit {
			break
		}
	}
	return rows, nil
}
func prefetchIntent() (domain.Query, []domain.Query) {
	q := baseQuery()
	lane := q
	lane.WorkStates = domain.StatesInLane(domain.LaneToDo)
	return q, []domain.Query{lane}
}

func TestPagePrefetchOverlapsSlowSummaryAndKeepsExactOrderedCursor(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	fast := &prefetchSource{fakeSource: mk(domain.ModuleFeed, "fast", 4, domain.WorkStateDue, ""), rowStarted: make(chan struct{})}
	slow := &prefetchSource{fakeSource: mk(domain.ModuleVaccination, "slow", 4, domain.WorkStateDue, ""), countWait: release}
	svc := NewService(slow, fast)
	q, intents := prefetchIntent()
	ctx := ports.WithRequestReadMemo(context.Background())
	done := make(chan error, 1)
	go func() { _, err := svc.Summary(WithPageLanePrefetch(ctx, intents), q); done <- err }()
	select {
	case <-fast.rowStarted:
	case <-time.After(time.Second):
		t.Fatal("fast rows did not start while slow summary blocked")
	}
	select {
	case <-done:
		t.Fatal("summary returned before slow source")
	default:
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(ctx, intents[0])
	if err != nil {
		t.Fatal(err)
	}
	if fast.lists.Load() != 1 || slow.lists.Load() != 1 {
		t.Fatal("normal lane duplicated prefetched read")
	}
	control, err := svc.List(context.Background(), intents[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page, control) || len(page.Rows) != 5 || page.NextCursor == "" {
		t.Fatalf("order/cursor changed: %+v versus %+v", page, control)
	}
}

func TestPagePrefetchHoldsSummaryConcurrencyBudget(t *testing.T) {
	var active, peak atomic.Int32
	sources := []ports.Source{}
	for i := 0; i < 18; i++ {
		sources = append(sources, &prefetchSource{fakeSource: mk(domain.ModuleFeed, fmt.Sprint(i), 1, domain.WorkStateDue, ""), active: &active, max: &peak})
	}
	q, intents := prefetchIntent()
	svc := NewService(sources...)
	_, err := svc.Summary(WithPageLanePrefetch(ports.WithRequestReadMemo(context.Background()), intents), q)
	if err != nil || peak.Load() > maxSummarySourceConcurrency || peak.Load() < 2 {
		t.Fatalf("peak=%d error=%v", peak.Load(), err)
	}
}

func TestPagePrefetchSkipsEmptyDegradedCursorAndVocabularyAndRetriesRows(t *testing.T) {
	for _, mode := range []string{"empty", "degraded", "cursor", "vocabulary", "row_error", "canceled", "no_modules", "other_scope"} {
		t.Run(mode, func(t *testing.T) {
			src := &prefetchSource{fakeSource: mk(domain.ModuleFeed, "feed", 2, domain.WorkStateDue, "")}
			svc := NewService(src)
			q, intents := prefetchIntent()
			ctx := ports.WithRequestReadMemo(context.Background())
			switch mode {
			case "empty":
				src.empty = true
			case "degraded":
				src.countErr = errors.New("count unavailable")
			case "cursor":
				intents[0].Cursor = domain.Cursor{Module: domain.ModuleFeed, SourceType: "feed", SourceID: "feed-001"}
			case "row_error":
				src.failFirstRow = true
			case "no_modules":
				q.NoModules = true
			case "other_scope":
				intents[0].ParkID = "elsewhere"
			}
			summaryCtx := WithPageLanePrefetch(ctx, intents)
			if mode == "vocabulary" {
				summaryCtx = ctx
			}
			if mode == "canceled" {
				var cancel context.CancelFunc
				summaryCtx, cancel = context.WithCancel(summaryCtx)
				cancel()
			}
			sum, _ := svc.Summary(summaryCtx, q)
			if mode == "row_error" && len(sum.Degraded) != 0 {
				t.Fatal("row error degraded valid summary")
			}
			expected := int32(0)
			if mode == "row_error" {
				expected = 1
			}
			if src.lists.Load() != expected {
				t.Fatalf("unexpected prefetch %d", src.lists.Load())
			}
			if mode == "row_error" {
				page, err := svc.List(ctx, intents[0])
				if err != nil || len(page.Rows) != 2 || len(page.Degraded) != 0 || src.lists.Load() != 2 {
					t.Fatalf("failed prefetch prevented recovery: %+v %v", page, err)
				}
			}
		})
	}
}

func TestPageSourceRowsMemoUsesExactScopeLimitStateCursorAndService(t *testing.T) {
	src := &prefetchSource{fakeSource: mk(domain.ModuleFeed, "feed", 3, domain.WorkStateDue, "")}
	svc := NewService(src)
	ctx := ports.WithRequestReadMemo(context.Background())
	q := ports.SourceQuery{TenantID: "t", ParkID: "p", BusinessDate: "2026-09-10", Limit: 2}
	if _, err := svc.sourceRows(ctx, 0, src, q); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tenant", "park", "day", "owner", "state", "limit", "cursor", "index", "service"} {
		other := q
		index := 0
		target := svc
		switch field {
		case "tenant":
			other.TenantID = "other"
		case "park":
			other.ParkID = "other"
		case "day":
			other.BusinessDate = "2026-09-11"
		case "owner":
			other.OwnerUserID = "other"
		case "state":
			other.WorkStates = []domain.WorkState{domain.WorkStateCompleted}
		case "limit":
			other.Limit = 3
		case "cursor":
			other.AfterSourceID = "feed-001"
		case "index":
			index = 1
		case "service":
			target = NewService(src)
		}
		before := src.lists.Load()
		if _, err := target.sourceRows(ctx, index, src, other); err != nil {
			t.Fatal(err)
		}
		if src.lists.Load() != before+1 {
			t.Fatalf("%s reused another query", field)
		}
	}
}

func TestPagePrefetchNormalizesLimitAndFreshRequest(t *testing.T) {
	for _, limit := range []int{0, domain.MaxLimit + 100} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			src := &prefetchSource{fakeSource: mk(domain.ModuleFeed, "feed", 3, domain.WorkStateDue, "")}
			svc := NewService(src)
			q, intents := prefetchIntent()
			q.Limit = limit
			intents[0].Limit = limit
			ctx := ports.WithRequestReadMemo(context.Background())
			if _, err := svc.Summary(WithPageLanePrefetch(ctx, intents), q); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.List(ctx, intents[0]); err != nil {
				t.Fatal(err)
			}
			if src.lists.Load() != 1 {
				t.Fatal("normalized lane did not reuse prefetch")
			}
			src.rows = src.rows[:1]
			fresh, err := svc.List(ports.WithRequestReadMemo(context.Background()), intents[0])
			if err != nil || len(fresh.Rows) != 1 {
				t.Fatalf("next request missed mutation: %+v %v", fresh, err)
			}
			if src.lists.Load() != 2 {
				t.Fatal("next request retained previous rows")
			}
		})
	}
}

func TestPagePrefetchFilteredModuleKeepsRegistryIndex(t *testing.T) {
	first := &prefetchSource{fakeSource: mk(domain.ModuleFeed, "feed", 2, domain.WorkStateDue, "")}
	selected := &prefetchSource{fakeSource: mk(domain.ModuleVerification, "verify", 2, domain.WorkStateDue, "")}
	svc := NewService(selected, first)
	q, intents := prefetchIntent()
	q.Modules = []domain.Module{domain.ModuleVerification}
	intents[0].Modules = q.Modules
	ctx := ports.WithRequestReadMemo(context.Background())
	if _, err := svc.Summary(WithPageLanePrefetch(ctx, intents), q); err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(ctx, intents[0])
	if err != nil || len(page.Rows) != 2 || first.lists.Load() != 0 || selected.lists.Load() != 1 {
		t.Fatalf("filtered source index drift: %+v %v first=%d selected=%d", page, err, first.lists.Load(), selected.lists.Load())
	}
}

// A source the summary of THIS request counted empty for a lane is not read for that lane: an
// engine-workflow source beside populated approvals under the same module used to cost one
// round trip per empty source on every page (TestWorkBoardPageRoundTripsIncludingAuth). A
// populated sibling is still read, a later page still reads, and a lane asking for a state
// the summary never counted still reads.
func TestLaneReadSkipsASourceTheSummaryCountedEmpty(t *testing.T) {
	populated := &prefetchSource{fakeSource: mk(domain.ModuleCounts, "approval", 2, domain.WorkStateDue, "")}
	empty := &prefetchSource{fakeSource: mk(domain.ModuleCounts, "workflow", 0, domain.WorkStateDue, ""), empty: true}
	svc := NewService(populated, empty)
	q, intents := prefetchIntent()
	ctx := ports.WithRequestReadMemo(context.Background())
	if _, err := svc.Summary(WithPageLanePrefetch(ctx, intents), q); err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(ctx, intents[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 2 || populated.lists.Load() != 1 {
		t.Fatalf("the populated source must still serve its rows once: %d rows, %d reads", len(page.Rows), populated.lists.Load())
	}
	if empty.lists.Load() != 0 {
		t.Fatalf("a source counted empty was read %d times", empty.lists.Load())
	}

	// Outside a request that counted it, the source is read as before.
	if _, err := svc.List(context.Background(), intents[0]); err != nil {
		t.Fatal(err)
	}
	if empty.lists.Load() != 1 {
		t.Fatalf("without the summary's counts the source must be read: %d", empty.lists.Load())
	}

	// A summary narrowed to other states says nothing about this lane: read it.
	narrowed := q
	narrowed.WorkStates = []domain.WorkState{domain.WorkStateCompleted}
	ctx2 := ports.WithRequestReadMemo(context.Background())
	if _, err := svc.Summary(ctx2, narrowed); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.List(ctx2, intents[0]); err != nil {
		t.Fatal(err)
	}
	if empty.lists.Load() != 2 {
		t.Fatalf("a lane state the summary never counted must be read: %d", empty.lists.Load())
	}
}

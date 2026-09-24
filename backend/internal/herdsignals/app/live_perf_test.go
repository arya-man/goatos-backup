package app

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

func manyMappedTags(n int) *fakeRepo {
	now := time.Now().UTC()
	pen := "30000000-0000-4000-8000-000000000001"
	repo := &fakeRepo{resolvedTags: map[string]string{}, goats: map[string]ports.GoatData{}}
	for i := 0; i < n; i++ {
		motion := int64(i)
		temp := 37.0
		tagID := fmt.Sprintf("A%05d", i)
		goatID := fmt.Sprintf("10000000-0000-4000-8000-%012d", i)
		repo.livePages = append(repo.livePages, domain.TagLatest{
			TagID: tagID, LastSeenAt: now, PatternState: "inactive", MappingState: "mapped",
			MovementState: "quiet", MotionDelta: &motion, TagTemperatureC: &temp,
		})
		repo.resolvedTags[tagID] = goatID
		repo.goats[goatID] = ports.GoatData{DisplayID: tagID, ShedID: &pen}
	}
	return repo
}

// A limit=1 summary read must enrich only its page: no 5k-page cohort walk, no whole-herd
// identifier resolve.
func TestListLiveNonRiskEnrichesOnlyRequestedPage(t *testing.T) {
	repo := manyMappedTags(300)
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	resp, err := svc.ListLive(context.Background(), actor, nil, nil, nil, nil, nil, nil, nil, nil, "", 1, domain.LiveSort{})
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(resp.Items))
	}
	for _, l := range repo.listLimits {
		if l > 1 {
			t.Fatalf("ListTagsLatest called with limit %d; the non-risk path must not walk the cohort (limits=%v)", l, repo.listLimits)
		}
	}
	if repo.resolvedValueCnt > 2 {
		t.Fatalf("resolved %d identifier values for a 1-row page; only the page may be enriched", repo.resolvedValueCnt)
	}
	if resp.Items[0].GroupMotionDeltaPct == nil {
		t.Fatalf("page row lost its pen-group comparison")
	}
}

// F5: a cold risk_state read (brand-new service, empty caches) is an indexed page read of the
// persisted classification: no cohort walk, and enrichment only for the page.
func TestListLiveColdRiskReadDoesNotWalkCohort(t *testing.T) {
	repo := manyMappedTags(300)
	if _, err := NewService(repo).RecomputeRisk(context.Background(), "tenant-1"); err != nil {
		t.Fatal(err)
	}
	repo.listLimits, repo.resolvedValueCnt = nil, 0
	svc := NewService(repo)
	risk := "attention"
	resp, err := svc.ListLive(context.Background(), domain.Actor{TenantID: "tenant-1", UserID: "u"}, nil, nil, nil, nil, nil, nil, &risk, nil, "", 5, domain.LiveSort{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 5 || resp.Summary.TagsSeen != 300 {
		t.Fatalf("risk page = %d items / summary %d, want 5 / 300", len(resp.Items), resp.Summary.TagsSeen)
	}
	for _, l := range repo.listLimits {
		if l > 5 {
			t.Fatalf("cold risk read listed with limit %d (cohort walk); limits=%v", l, repo.listLimits)
		}
	}
	if repo.resolvedValueCnt > 10 {
		t.Fatalf("cold risk read resolved %d identifiers, want page-only (<=10)", repo.resolvedValueCnt)
	}
}

// Invalidated entries are a miss for plain reads; an entry merely past its TTL (inside the
// grace) is served stale while ONE background refresh runs.
func TestLiveCohortCacheStaleWhileRevalidate(t *testing.T) {
	c := newLiveCohortCache[[]domain.LiveItem]()
	var mu sync.Mutex
	now := time.Unix(1000, 0)
	c.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	var calls int
	compute := func(context.Context) ([]domain.LiveItem, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		return []domain.LiveItem{{TagID: fmt.Sprint(n)}}, nil
	}
	getCalls := func() int { mu.Lock(); defer mu.Unlock(); return calls }
	ctx := context.Background()
	key := liveCohortKey("t1")
	if got, _ := c.get(ctx, key, false, compute); got[0].TagID != "1" {
		t.Fatalf("first load = %v", got)
	}
	c.invalidate("t1")
	advance(time.Second)
	if got, _ := c.get(ctx, key, false, compute); got[0].TagID != "2" {
		t.Fatalf("read after invalidation = %v, want recomputed 2", got)
	}
	advance(liveCohortTTL + time.Second) // age-expired, not invalidated, inside grace
	if got, _ := c.get(ctx, key, false, compute); got[0].TagID != "2" {
		t.Fatalf("age-expired read must be served stale immediately, got %v", got)
	}
	deadline := time.Now().Add(time.Second)
	for getCalls() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond)
	if got, _ := c.get(ctx, key, false, compute); got[0].TagID != "3" || getCalls() != 3 {
		t.Fatalf("after background refresh got %v calls %d, want 3 / 3", got, getCalls())
	}
	advance(liveCohortTTL + liveCohortStaleGrace + time.Second) // past grace: a miss
	if got, _ := c.get(ctx, key, false, compute); got[0].TagID != "4" {
		t.Fatalf("past-grace read = %v, want synchronous recompute 4", got)
	}
}

// The whole-filter summary aggregate is shared across reads of one (tenant, filter) until a
// NOTIFY invalidates it; movement_state/cursor/limit never split the key.
func TestListLiveSummaryAggregateIsSharedUntilNotify(t *testing.T) {
	repo := manyMappedTags(10)
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	ctx := context.Background()
	moving := "moving"
	for _, mv := range []*string{nil, &moving, nil} {
		resp, err := svc.ListLive(ctx, actor, nil, nil, mv, nil, nil, nil, nil, nil, "", 3, domain.LiveSort{})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Summary.TagsSeen != 10 {
			t.Fatalf("summary tags_seen = %d, want whole-filter 10", resp.Summary.TagsSeen)
		}
	}
	if repo.summaryCalls != 1 {
		t.Fatalf("summary aggregates = %d for 3 reads, want 1", repo.summaryCalls)
	}
	svc.InvalidateLive("tenant-1")
	if _, err := svc.ListLive(domain.WithFreshLiveRead(ctx), actor, nil, nil, nil, nil, nil, nil, nil, nil, "", 3, domain.LiveSort{}); err != nil {
		t.Fatal(err)
	}
	if repo.summaryCalls != 2 {
		t.Fatalf("fresh read after NOTIFY: summary aggregates = %d, want 2", repo.summaryCalls)
	}
}

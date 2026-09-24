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

// Risk-filtered reads share one cohort computation until a NOTIFY invalidates it; a fresh read
// (stream hub) after the NOTIFY recomputes, a plain read is served stale-while-revalidate.
func TestListLiveRiskCohortIsCachedAndInvalidatedByNotify(t *testing.T) {
	repo := manyMappedTags(20)
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	risk := "attention"
	ctx := context.Background()
	walks := func() int {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		n := 0
		for _, l := range repo.listLimits {
			if l == liveSignalCohortPageSize {
				n++
			}
		}
		return n
	}
	for i := 0; i < 5; i++ {
		if _, err := svc.ListLive(ctx, actor, nil, nil, nil, nil, nil, nil, &risk, nil, "", 5, domain.LiveSort{}); err != nil {
			t.Fatalf("ListLive: %v", err)
		}
	}
	if got := walks(); got != 1 {
		t.Fatalf("cohort walks = %d after 5 reads, want 1", got)
	}
	svc.InvalidateLive("other-tenant")
	if _, err := svc.ListLive(domain.WithFreshLiveRead(ctx), actor, nil, nil, nil, nil, nil, nil, &risk, nil, "", 5, domain.LiveSort{}); err != nil {
		t.Fatal(err)
	}
	if got := walks(); got != 1 {
		t.Fatalf("another tenant's NOTIFY recomputed this tenant: walks = %d", got)
	}
	svc.InvalidateLive("tenant-1")
	resp, err := svc.ListLive(domain.WithFreshLiveRead(ctx), actor, nil, nil, nil, nil, nil, nil, &risk, nil, "", 5, domain.LiveSort{})
	if err != nil {
		t.Fatal(err)
	}
	if got := walks(); got != 2 {
		t.Fatalf("fresh read after NOTIFY walks = %d, want 2", got)
	}
	if len(resp.Items) != 5 || resp.Summary.TagsSeen != 20 {
		t.Fatalf("risk page = %d items, summary %d; want 5 / 20", len(resp.Items), resp.Summary.TagsSeen)
	}
}

// A plain (non-stream) read of an invalidated but recently loaded cohort is served from cache
// without a walk; once past the refresh floor it is served stale while ONE background walk runs.
func TestLiveCohortCacheStaleWhileRevalidate(t *testing.T) {
	c := newLiveCohortCache[[]domain.LiveItem]()
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	var calls int
	var mu sync.Mutex
	compute := func(context.Context) ([]domain.LiveItem, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		return []domain.LiveItem{{TagID: fmt.Sprint(n)}}, nil
	}
	ctx := context.Background()
	key := liveCohortKey("t1")
	if got, _ := c.get(ctx, key, false, compute); got[0].TagID != "1" {
		t.Fatalf("first load = %v", got)
	}
	c.invalidate("t1")
	now = now.Add(time.Second)
	if got, _ := c.get(ctx, key, false, compute); got[0].TagID != "1" || calls != 1 {
		t.Fatalf("stale read inside refresh floor: got %v calls %d, want cached 1 / 1", got, calls)
	}
	now = now.Add(liveCohortMinRefresh)
	if got, _ := c.get(ctx, key, false, compute); got[0].TagID != "1" {
		t.Fatalf("stale read must be served immediately, got %v", got)
	}
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		n := calls
		mu.Unlock()
		if n == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond)
	if got, _ := c.get(ctx, key, false, compute); got[0].TagID != "2" || calls != 2 {
		t.Fatalf("after background refresh got %v calls %d, want 2 / 2", got, calls)
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

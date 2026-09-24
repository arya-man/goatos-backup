package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

type syncRecorder struct {
	mu sync.Mutex
	*httptest.ResponseRecorder
}

func (s *syncRecorder) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ResponseRecorder.Write(b)
}

func (s *syncRecorder) snapshots() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Count(s.Body.String(), "event: snapshot\n")
}

// Five viewers of the same tenant+query and a NOTIFY every 20ms for 1s (a 2s MQTT cadence
// compressed 100x) must cost one read per coalescing window, not one per viewer per NOTIFY, and
// every viewer must still receive the recomputed snapshots.
func TestLiveHubCoalescesRecomputesAcrossViewersAndNotifies(t *testing.T) {
	svc := &fakeService{liveResp: domain.LiveResponse{Summary: domain.Summary{TagsSeen: 7}}}
	source := &fakeLiveNotificationSource{}
	h := NewHandler(svc).WithLiveNotifications(context.Background(), source)
	h.liveHub.interval = 200 * time.Millisecond

	const viewers = 5
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	recs := make([]*syncRecorder, viewers)
	for i := 0; i < viewers; i++ {
		recs[i] = &syncRecorder{ResponseRecorder: httptest.NewRecorder()}
		base := authedRequest("GET", "/herd-signals/live/stream?limit=1", nil)
		viewerCtx, stopViewer := context.WithCancel(base.Context())
		go func() { <-ctx.Done(); stopViewer() }()
		req := base.WithContext(viewerCtx)
		wg.Add(1)
		go func(rec *syncRecorder) {
			defer wg.Done()
			h.StreamLive(rec, req)
		}(recs[i])
	}
	deadline := time.Now().Add(2 * time.Second)
	for svc.liveCallCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	initial := svc.liveCallCount()

	stop := time.Now().Add(time.Second)
	for time.Now().Before(stop) {
		source.publish("00000000-0000-4000-8000-000000000001")
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // let the trailing edge land
	recomputes := svc.liveCallCount() - initial
	cancel()
	wg.Wait()

	// 1s of NOTIFYs at a 200ms window: leading edge + at most one per window (+1 trailing).
	if recomputes < 2 || recomputes > 7 {
		t.Fatalf("recomputes = %d for 5 viewers x ~50 NOTIFYs, want 2..7 (one per window, shared)", recomputes)
	}
	if initial > viewers {
		t.Fatalf("initial snapshot reads = %d, want <= %d", initial, viewers)
	}
	for i, rec := range recs {
		if got := rec.snapshots(); got < 2 {
			t.Fatalf("viewer %d received %d snapshots, want the initial plus coalesced recomputes", i, got)
		}
	}
}

// A NOTIFY for another tenant must not recompute this tenant's feed.
func TestLiveHubIsTenantScoped(t *testing.T) {
	hub := newLiveStreamHub(time.Millisecond, nil)
	var mu sync.Mutex
	calls := 0
	_, unsub := hub.subscribe("t1", "t1\x00", func(context.Context) ([]byte, bool) {
		mu.Lock()
		calls++
		mu.Unlock()
		return []byte("x"), true
	})
	defer unsub()
	hub.publish("t2")
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Fatalf("other-tenant NOTIFY recomputed feed %d times", calls)
	}
}

// F2: a new viewer's first snapshot must not be served from a stale-while-revalidate cache
// entry: nothing would ever push the refreshed result to it.
func TestStreamLiveInitialSnapshotIsFreshRead(t *testing.T) {
	svc := &fakeService{liveResp: domain.LiveResponse{}}
	h := NewHandler(svc)
	base := authedRequest("GET", "/herd-signals/live/stream?limit=3", nil)
	ctx, cancel := context.WithCancel(base.Context())
	done := make(chan struct{})
	go func() { h.StreamLive(httptest.NewRecorder(), base.WithContext(ctx)); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for svc.liveCallCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.liveFresh) == 0 || !svc.liveFresh[0] {
		t.Fatalf("initial stream snapshot fresh flags = %v, want a WithFreshLiveRead context", svc.liveFresh)
	}
}

// F4: a failed recompute (snapshot_error frame) must not become the feed's reusable frame.
func TestLiveHubDoesNotCacheErrorFrame(t *testing.T) {
	hub := newLiveStreamHub(time.Hour, nil)
	ch, unsub := hub.subscribe("t1", "k", func(context.Context) ([]byte, bool) {
		return []byte("event: snapshot_error\n\n"), false
	})
	defer unsub()
	hub.publish("t1")
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("error frame not delivered")
	}
	if f := hub.recentFrame("k"); f != nil {
		t.Fatalf("recentFrame = %q, want nil after a failed compute", f)
	}
}

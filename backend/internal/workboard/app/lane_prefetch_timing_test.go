package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

func TestPrefetchReadTimingRecordsActualReadNotMemoHits(t *testing.T) {
	src := &prefetchSource{fakeSource: mk(domain.ModuleFeed, "timed", 1, domain.WorkStateDue, "")}
	svc := NewService(src)
	q, intents := prefetchIntent()
	var mu sync.Mutex
	observed := map[string]int{}
	ctx := WithTiming(ports.WithRequestReadMemo(context.Background()), func(name string, elapsed time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		observed[name]++
		if elapsed < 0 {
			t.Errorf("negative elapsed duration: %v", elapsed)
		}
	})
	if _, err := svc.Summary(WithPageLanePrefetch(ctx, intents), q); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.List(ctx, intents[0]); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if got := observed["source_read_feed_timed_todo"]; got != 1 {
		t.Fatalf("actual prefetched read timing count = %d, want 1; timings: %v", got, observed)
	}
	if src.lists.Load() != 1 {
		t.Fatalf("source reads = %d, want 1", src.lists.Load())
	}
}

package readcache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The cache is bounded by approximate BYTES, not only entry count: a Weight Demographics answer is
// ~135 KB of JSON (~0.6 MB on the heap) on the OCI clone, so 512 of them would be ~300 MB.
func TestByteBoundEvictsLeastRecentlyUsedByApproximateSize(t *testing.T) {
	c := New(Options{Name: "bytes", MaxEntries: 100, MaxBytes: 2400, Sizer: func(v any) int64 { return int64(len(v.(string))) }})
	c.SetCoherent(true)
	for i := 0; i < 10; i++ {
		v := strings.Repeat("x", 300)
		Load(context.Background(), c, key("t1", fmt.Sprint(i)), func(context.Context) (string, error) { return v, nil })
	}
	st := c.Stats()
	if st.Bytes > 2400 || st.Entries != 8 || st.LRUEvictions != 2 {
		t.Fatalf("stats = %+v, want <= 2400 bytes in 8 entries after 2 byte-bound evictions", st)
	}
	// The most recent entries survive.
	if v, _ := Load(context.Background(), c, key("t1", "9"), func(context.Context) (string, error) { return "reloaded", nil }); v == "reloaded" {
		t.Fatal("the newest entry was evicted")
	}
	if v, _ := Load(context.Background(), c, key("t1", "0"), func(context.Context) (string, error) { return "reloaded", nil }); v != "reloaded" {
		t.Fatal("the oldest entry survived the byte bound")
	}
}

func TestOversizedValueIsServedButNotCached(t *testing.T) {
	c := New(Options{Name: "bytes", MaxBytes: 800, Sizer: func(v any) int64 { return int64(len(v.(string))) }})
	big := strings.Repeat("y", 500) // > MaxBytes/8
	if v, _ := Load(context.Background(), c, key("t1", "big"), func(context.Context) (string, error) { return big, nil }); v != big {
		t.Fatal("oversized value not served")
	}
	if c.Stats().Entries != 0 || c.Stats().TooLarge != 1 {
		t.Fatalf("stats = %+v, want the oversized value uncached", c.Stats())
	}
}

func TestEvictionReleasesBytes(t *testing.T) {
	c := New(Options{Name: "bytes", MaxBytes: 10000, Sizer: func(v any) int64 { return int64(len(v.(string))) }})
	Load(context.Background(), c, key("t1", "a", "p1"), func(context.Context) (string, error) { return strings.Repeat("z", 100), nil })
	Load(context.Background(), c, key("t1", "b", "p2"), func(context.Context) (string, error) { return strings.Repeat("z", 50), nil })
	c.Evict(context.Background(), "t1", "p1")
	if b := c.Stats().Bytes; b != 50 {
		t.Fatalf("bytes after scoped evict = %d, want 50", b)
	}
	c.EvictAll(context.Background())
	if b := c.Stats().Bytes; b != 0 {
		t.Fatalf("bytes after evict-all = %d", b)
	}
}

func TestDefaultSizerApproximatesHeapFromJSON(t *testing.T) {
	type report struct{ Rows []int }
	small := DefaultSizer(report{Rows: make([]int, 10)})
	large := DefaultSizer(report{Rows: make([]int, 10000)})
	if large <= small*100 {
		t.Fatalf("sizer does not scale with payload: small=%d large=%d", small, large)
	}
	if DefaultSizer(make(chan int)) <= 0 {
		t.Fatal("an unmarshalable value must still get a positive, conservative size")
	}
}

// A half-open TCP session must not keep the cache "coherent": the liveness ping is bounded, and
// its failure is a disconnect.
func TestHealthPingIsBoundedAndFailureDisconnects(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	l := &Listener{pingTimeout: 50 * time.Millisecond}
	start := time.Now()
	err := l.ping(context.Background(), func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-blocked:
			return nil
		}
	})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung ping err = %v, want deadline exceeded", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("ping was not bounded")
	}
}

// Warm-up starts only after the first successful LISTEN (so the connect-time evict-all cannot
// wipe what it loads), once, after a per-instance jitter.
func TestWarmupRunsOnceAfterFirstConnect(t *testing.T) {
	c := New(Options{Name: "warm"})
	runs := make(chan struct{}, 4)
	l := &Listener{caches: []*Cache{c}}
	l.OnFirstConnect(func(ctx context.Context) {
		c.StartWarmup(ctx, nil, time.Millisecond, time.Second, func(context.Context) error {
			runs <- struct{}{}
			return nil
		})
	})
	Load(context.Background(), c, key("t1", "pre"), func(context.Context) (int, error) { return 1, nil })
	l.connected(context.Background())
	l.connected(context.Background()) // a reconnect: evicts again, does not re-run the hook
	select {
	case <-runs:
	case <-time.After(time.Second):
		t.Fatal("warm-up never ran after the first connect")
	}
	select {
	case <-runs:
		t.Fatal("warm-up ran twice")
	case <-time.After(100 * time.Millisecond):
	}
	if !c.Coherent() {
		t.Fatal("connect did not mark the cache coherent")
	}
}

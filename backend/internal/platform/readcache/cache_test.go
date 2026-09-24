package readcache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func newTestCache(t *testing.T, coherent bool) (*Cache, *clock) {
	t.Helper()
	clk := &clock{now: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)}
	c := New(Options{Name: "test", MaxEntries: 4, FreshTTL: time.Minute, StaleGrace: 5 * time.Minute, DegradedTTL: 30 * time.Second, Now: clk.Now})
	c.SetCoherent(coherent)
	return c, clk
}

func key(tenant string, params string, parks ...string) Key {
	return Key{Tenant: tenant, Parks: parks, Params: params}
}

func TestSingleFlightRunsOneLoadForConcurrentIdenticalMisses(t *testing.T) {
	c, _ := newTestCache(t, true)
	var loads atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := Load(context.Background(), c, key("t1", "shed", "p1"), func(context.Context) (string, error) {
				loads.Add(1)
				<-release
				return "answer", nil
			})
			if err != nil {
				t.Error(err)
			}
			results[i] = v
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := loads.Load(); n != 1 {
		t.Fatalf("20 identical concurrent misses ran %d loads, want 1", n)
	}
	for _, r := range results {
		if r != "answer" {
			t.Fatalf("waiter got %q", r)
		}
	}
	st := c.Stats()
	if st.Misses != 1 || st.Shared != 19 {
		t.Fatalf("stats = %+v, want 1 miss + 19 shared", st)
	}
}

func TestCancelledFirstCallerDoesNotPoisonWaiters(t *testing.T) {
	c, _ := newTestCache(t, true)
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() {
		_, err := Load(ctx, c, key("t1", "k"), func(loadCtx context.Context) (int, error) {
			<-release
			return 7, loadCtx.Err()
		})
		errs <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller err = %v", err)
	}
	done := make(chan int, 1)
	go func() {
		v, _ := Load(context.Background(), c, key("t1", "k"), func(context.Context) (int, error) { return -1, nil })
		done <- v
	}()
	close(release)
	if v := <-done; v != 7 {
		t.Fatalf("waiter got %d, want the detached load's 7", v)
	}
}

func TestStaleWhileRevalidateServesStaleAndRefreshesOnce(t *testing.T) {
	c, clk := newTestCache(t, true)
	var loads atomic.Int32
	release := make(chan struct{})
	load := func(context.Context) (int, error) {
		n := loads.Add(1)
		if n > 1 {
			<-release
		}
		return int(n), nil
	}
	if v, _ := Load(context.Background(), c, key("t1", "k"), load); v != 1 {
		t.Fatalf("first load = %d", v)
	}
	clk.Add(2 * time.Minute) // past FreshTTL, inside grace
	for i := 0; i < 10; i++ {
		v, err := Load(context.Background(), c, key("t1", "k"), load)
		if err != nil || v != 1 {
			t.Fatalf("stale read %d = %d, %v; want the stale 1 without blocking", i, v, err)
		}
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for c.Stats().Refreshes == 0 || loads.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("background refresh never ran")
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitFor(t, func() bool { v, _ := Load(context.Background(), c, key("t1", "k"), load); return v == 2 })
	if n := loads.Load(); n != 2 {
		t.Fatalf("10 stale reads triggered %d loads, want exactly 1 refresh", n-1)
	}
}

func TestPastGraceIsAMissNotStale(t *testing.T) {
	c, clk := newTestCache(t, true)
	var loads atomic.Int32
	load := func(context.Context) (int32, error) { return loads.Add(1), nil }
	Load(context.Background(), c, key("t1", "k"), load)
	clk.Add(7 * time.Minute)
	if v, _ := Load(context.Background(), c, key("t1", "k"), load); v != 2 {
		t.Fatalf("read past grace = %d, want a blocking reload (2)", v)
	}
}

func TestIncoherentCacheNeverServesStaleAndExpiresAtDegradedTTL(t *testing.T) {
	c, clk := newTestCache(t, false)
	var loads atomic.Int32
	load := func(context.Context) (int32, error) { return loads.Add(1), nil }
	Load(context.Background(), c, key("t1", "k"), load)
	clk.Add(20 * time.Second)
	if v, _ := Load(context.Background(), c, key("t1", "k"), load); v != 1 {
		t.Fatalf("inside degraded TTL = %d, want hit", v)
	}
	clk.Add(15 * time.Second) // 35 s: past 30 s degraded TTL
	if v, _ := Load(context.Background(), c, key("t1", "k"), load); v != 2 {
		t.Fatalf("sibling instance without eviction feed served a %d-old entry", 35)
	}
	if c.Stats().Stale != 0 {
		t.Fatal("incoherent cache served stale")
	}
}

func TestEvictOnWriteIsScopedToTenantAndPark(t *testing.T) {
	c, _ := newTestCache(t, true)
	c.opts.MaxEntries = 100
	var loads atomic.Int32
	load := func(context.Context) (int32, error) { return loads.Add(1), nil }
	keys := map[string]Key{
		"t1-p1":  key("t1", "shed", "p1"),
		"t1-p2":  key("t1", "shed", "p2"),
		"t1-all": key("t1", "growth"),
		"t2-p1":  key("t2", "shed", "p1"),
	}
	first := map[string]int32{}
	for name, k := range keys {
		first[name], _ = Load(context.Background(), c, k, load)
	}
	if n := c.Evict(context.Background(), "t1", "p1"); n != 2 {
		t.Fatalf("evict t1/p1 removed %d entries, want t1-p1 and the tenant-wide t1 entry", n)
	}
	for name, k := range keys {
		v, _ := Load(context.Background(), c, k, load)
		evicted := name == "t1-p1" || name == "t1-all"
		if evicted && v == first[name] {
			t.Fatalf("%s survived a write to its park", name)
		}
		if !evicted && v != first[name] {
			t.Fatalf("%s was evicted by a write to another park/tenant", name)
		}
	}
	c.Evict(context.Background(), "t1")
	if v, _ := Load(context.Background(), c, keys["t1-p2"], load); v == first["t1-p2"] {
		t.Fatal("tenant-wide eviction left a t1 entry")
	}
	if v, _ := Load(context.Background(), c, keys["t2-p1"], load); v != first["t2-p1"] {
		t.Fatal("tenant-wide eviction of t1 touched t2")
	}
}

func TestLoadStartedBeforeWriteDoesNotRepopulateAfterEviction(t *testing.T) {
	c, _ := newTestCache(t, true)
	release := make(chan struct{})
	started := make(chan struct{})
	go Load(context.Background(), c, key("t1", "k", "p1"), func(context.Context) (string, error) {
		close(started)
		<-release
		return "pre-write", nil
	})
	<-started
	c.Evict(context.Background(), "t1", "p1") // the write commits while the read is in flight
	// A request after the write must not join the pre-write flight.
	got := make(chan string, 1)
	go func() {
		v, _ := Load(context.Background(), c, key("t1", "k", "p1"), func(context.Context) (string, error) { return "post-write", nil })
		got <- v
	}()
	if v := <-got; v != "post-write" {
		t.Fatalf("post-write request got %q", v)
	}
	close(release)
	time.Sleep(20 * time.Millisecond)
	v, _ := Load(context.Background(), c, key("t1", "k", "p1"), func(context.Context) (string, error) { return "reload", nil })
	if v == "pre-write" {
		t.Fatal("a load that started before the write repopulated the cache")
	}
}

func TestNoCrossTenantLeakageForIdenticalParams(t *testing.T) {
	c, _ := newTestCache(t, true)
	for _, tenant := range []string{"t1", "t2"} {
		tenant := tenant
		v, _ := Load(context.Background(), c, key(tenant, "same-params", "p1"), func(context.Context) (string, error) { return "data-of-" + tenant, nil })
		if v != "data-of-"+tenant {
			t.Fatalf("tenant %s got %q", tenant, v)
		}
	}
	v, _ := Load(context.Background(), c, key("t2", "same-params", "p1"), func(context.Context) (string, error) { return "reload", nil })
	if v != "data-of-t2" {
		t.Fatalf("t2 cached read = %q", v)
	}
	if key("t1", "x", "b", "a").String() != key("t1", "x", "a", "b", "a").String() {
		t.Fatal("park order/duplicates must normalize to one key")
	}
}

func TestLRUBoundsEntries(t *testing.T) {
	c, _ := newTestCache(t, true)
	for i := 0; i < 10; i++ {
		Load(context.Background(), c, key("t1", fmt.Sprint(i)), func(context.Context) (int, error) { return i, nil })
	}
	st := c.Stats()
	if st.Entries != 4 || st.LRUEvictions != 6 {
		t.Fatalf("stats = %+v, want 4 entries and 6 LRU evictions", st)
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	c, _ := newTestCache(t, true)
	boom := errors.New("boom")
	if _, err := Load(context.Background(), c, key("t1", "k"), func(context.Context) (int, error) { return 0, boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if v, _ := Load(context.Background(), c, key("t1", "k"), func(context.Context) (int, error) { return 5, nil }); v != 5 {
		t.Fatal("an error was cached")
	}
}

func TestTypeMismatchIsAnError(t *testing.T) {
	c, _ := newTestCache(t, true)
	Load(context.Background(), c, key("t1", "k"), func(context.Context) (int, error) { return 5, nil })
	if _, err := Load(context.Background(), c, key("t1", "k"), func(context.Context) (string, error) { return "", nil }); !errors.Is(err, ErrUnexpectedType) {
		t.Fatalf("err = %v", err)
	}
}

func TestListenerPayloadEvictsScopedAndBadPayloadEvictsAll(t *testing.T) {
	c, _ := newTestCache(t, true)
	c.opts.MaxEntries = 100
	for _, k := range []Key{key("t1", "a", "p1"), key("t1", "a", "p2"), key("t2", "a", "p1")} {
		Load(context.Background(), c, k, func(context.Context) (int, error) { return 1, nil })
	}
	l := &Listener{caches: []*Cache{c}}
	l.apply(context.Background(), `{"tenant_id":"t1","park_ids":["p2"]}`)
	if c.Stats().Entries != 2 {
		t.Fatalf("scoped payload left %d entries, want 2", c.Stats().Entries)
	}
	l.apply(context.Background(), `not json`)
	if c.Stats().Entries != 0 {
		t.Fatal("an unparseable payload must evict everything")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never became true")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestListenStatementNamesTheNotifyChannel(t *testing.T) {
	src, err := os.ReadFile("listener.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `"LISTEN `+NotifyChannel+`"`) {
		t.Fatal("the LISTEN literal must name NotifyChannel")
	}
}

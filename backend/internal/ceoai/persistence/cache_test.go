package persistence

import (
	"testing"
	"time"
)

func TestNormalizeQuestionCollapsesAndTrims(t *testing.T) {
	cases := map[string]string{
		"How many goats?":        "how many goats",
		"  how many   goats  ":   "how many goats",
		"HOW MANY GOATS!!":       "how many goats",
		"how many goats.":        "how many goats",
		"Count by shed, please;": "count by shed, please",
	}
	for in, want := range cases {
		if got := NormalizeQuestion(in); got != want {
			t.Errorf("NormalizeQuestion(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestHashQuestionStableAcrossEquivalentForms(t *testing.T) {
	a := HashQuestion("How many goats?")
	b := HashQuestion("  how many   GOATS ")
	if a != b {
		t.Fatalf("equivalent questions hashed differently: %s vs %s", a, b)
	}
	if a == HashQuestion("how many sheep") {
		t.Fatal("different questions must not share a hash")
	}
}

func TestCacheHitMissAndStale(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{t: now}
	c := NewResponseCache(CacheConfig{TTL: time.Minute, Capacity: 8, nowFn: clock.Now})

	if _, ok := c.Get("t1", "how many goats", "2026-07-22"); ok {
		t.Fatal("expected miss on empty cache")
	}

	c.Set("t1", "how many goats", "2026-07-22", CachedAnswer{Answer: "2567 goats", Source: "cube"})
	got, ok := c.Get("t1", "How many goats?", "2026-07-22")
	if !ok {
		t.Fatal("expected hit for equivalent normalized question")
	}
	if got.Answer != "2567 goats" || got.Source != "cube" {
		t.Fatalf("unexpected cached value: %+v", got)
	}
	if got.StoredAt != now {
		t.Fatalf("StoredAt=%v, want freshness stamp %v", got.StoredAt, now)
	}

	// Advance past the TTL: the entry is stale and must miss, and be evicted on read.
	clock.t = now.Add(time.Minute + time.Second)
	if _, ok := c.Get("t1", "how many goats", "2026-07-22"); ok {
		t.Fatal("expected stale miss after TTL")
	}
	if c.Len() != 0 {
		t.Fatalf("stale entry not evicted on read: len=%d", c.Len())
	}
}

func TestCacheNeverCrossesTenants(t *testing.T) {
	c := NewResponseCache(CacheConfig{TTL: time.Minute, Capacity: 8})
	c.Set("tenantA", "how many goats", "2026-07-22", CachedAnswer{Answer: "A-answer"})

	if _, ok := c.Get("tenantB", "how many goats", "2026-07-22"); ok {
		t.Fatal("tenant B must not read tenant A's cached answer")
	}
	got, ok := c.Get("tenantA", "how many goats", "2026-07-22")
	if !ok || got.Answer != "A-answer" {
		t.Fatalf("tenant A own read failed: ok=%v val=%+v", ok, got)
	}
}

func TestCacheAsOfBucketSeparatesDays(t *testing.T) {
	c := NewResponseCache(CacheConfig{TTL: time.Hour, Capacity: 8})
	c.Set("t1", "what is due today", "2026-07-22", CachedAnswer{Answer: "day1"})
	if _, ok := c.Get("t1", "what is due today", "2026-07-23"); ok {
		t.Fatal("a new business day must be a cache miss, not a stale carry-over")
	}
}

func TestCacheLRUEviction(t *testing.T) {
	c := NewResponseCache(CacheConfig{TTL: time.Hour, Capacity: 2})
	c.Set("t1", "q1", "d", CachedAnswer{Answer: "1"})
	c.Set("t1", "q2", "d", CachedAnswer{Answer: "2"})
	// Touch q1 so q2 becomes the least-recently-used.
	if _, ok := c.Get("t1", "q1", "d"); !ok {
		t.Fatal("q1 should be present")
	}
	c.Set("t1", "q3", "d", CachedAnswer{Answer: "3"}) // evicts q2.
	if _, ok := c.Get("t1", "q2", "d"); ok {
		t.Fatal("q2 should have been LRU-evicted")
	}
	if _, ok := c.Get("t1", "q1", "d"); !ok {
		t.Fatal("q1 should have survived (recently used)")
	}
	if _, ok := c.Get("t1", "q3", "d"); !ok {
		t.Fatal("q3 should be present")
	}
}

func TestCacheInvalidateScopedToTenant(t *testing.T) {
	c := NewResponseCache(CacheConfig{TTL: time.Hour, Capacity: 8})
	c.Set("t1", "q", "d", CachedAnswer{Answer: "1"})
	c.Set("t2", "q", "d", CachedAnswer{Answer: "2"})
	c.Invalidate("t1")
	if _, ok := c.Get("t1", "q", "d"); ok {
		t.Fatal("t1 entry should be invalidated")
	}
	if _, ok := c.Get("t2", "q", "d"); !ok {
		t.Fatal("t2 entry must survive invalidation of t1")
	}
}

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time { return f.t }

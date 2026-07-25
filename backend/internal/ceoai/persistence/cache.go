package persistence

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CachedAnswer is a previously composed leadership answer plus the provenance the UI
// needs to render it truthfully on a cache hit. StoredAt is the freshness stamp: the UI
// shows "as of <StoredAt>" so a cached number never masquerades as live.
type CachedAnswer struct {
	Answer    string
	Source    string
	Mode      string
	Citations []byte // raw JSON citations array, opaque to the cache.
	StoredAt  time.Time
}

// cacheKey scopes every entry to (tenant, normalized-question, as-of bucket). The tenant
// is the FIRST component so a lookup can never cross tenants: a different tenant hashes
// to a different key even for a byte-identical question.
type cacheKey struct {
	tenantID     string
	questionHash string
	asOfBucket   string
}

type cacheEntry struct {
	key       cacheKey
	value     CachedAnswer
	expiresAt time.Time
	elem      *list.Element // position in the LRU recency list.
}

// ResponseCache is an in-process TTL + LRU response cache. It is safe for concurrent
// use. It deliberately holds no cross-tenant state: keys embed the tenant id and there
// is no wildcard read path. A Redis adapter can later implement the same Get/Set shape
// for multi-instance deployments; this in-process version is the single-instance default.
type ResponseCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	capacity int
	now      func() time.Time
	entries  map[cacheKey]*cacheEntry
	lru      *list.List // front = most recently used.
}

// CacheConfig configures a ResponseCache. Zero values fall back to safe defaults.
type CacheConfig struct {
	TTL      time.Duration // entry lifetime; default 5m.
	Capacity int           // max live entries before LRU eviction; default 1024.
	nowFn    func() time.Time
}

const (
	defaultCacheTTL      = 5 * time.Minute
	defaultCacheCapacity = 1024
)

// NewResponseCache builds a ready-to-use cache.
func NewResponseCache(cfg CacheConfig) *ResponseCache {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	capacity := cfg.Capacity
	if capacity <= 0 {
		capacity = defaultCacheCapacity
	}
	nowFn := cfg.nowFn
	if nowFn == nil {
		nowFn = time.Now
	}
	return &ResponseCache{
		ttl:      ttl,
		capacity: capacity,
		now:      nowFn,
		entries:  make(map[cacheKey]*cacheEntry, capacity),
		lru:      list.New(),
	}
}

// Get returns a live cached answer for (tenant, question, asOf) or hit=false on miss or
// stale. A stale entry is evicted on read so it is never served twice. asOf buckets the
// business day (e.g. an IST date) so "today" answers roll over cleanly at day boundary.
func (c *ResponseCache) Get(tenantID, question, asOf string) (CachedAnswer, bool) {
	if strings.TrimSpace(tenantID) == "" {
		return CachedAnswer{}, false
	}
	key := c.keyFor(tenantID, question, asOf)

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return CachedAnswer{}, false
	}
	if !c.now().Before(entry.expiresAt) {
		// Stale: drop it so a later Get is a clean miss rather than a second stale hit.
		c.removeLocked(entry)
		return CachedAnswer{}, false
	}
	c.lru.MoveToFront(entry.elem)
	return entry.value, true
}

// Set stores an answer under (tenant, question, asOf) with the configured TTL and stamps
// StoredAt. An empty tenant is a no-op (defensive: a keyless answer is never cached).
func (c *ResponseCache) Set(tenantID, question, asOf string, value CachedAnswer) {
	if strings.TrimSpace(tenantID) == "" {
		return
	}
	key := c.keyFor(tenantID, question, asOf)
	now := c.now()
	value.StoredAt = now

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.entries[key]; ok {
		existing.value = value
		existing.expiresAt = now.Add(c.ttl)
		c.lru.MoveToFront(existing.elem)
		return
	}
	entry := &cacheEntry{key: key, value: value, expiresAt: now.Add(c.ttl)}
	entry.elem = c.lru.PushFront(entry)
	c.entries[key] = entry
	c.evictLocked()
}

// Invalidate drops every entry for a tenant (e.g. on a day rollover or config change for
// that tenant). It never touches another tenant's entries.
func (c *ResponseCache) Invalidate(tenantID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if key.tenantID == tenantID {
			c.removeLocked(entry)
		}
	}
}

// Len reports the number of live entries (used by tests and metrics).
func (c *ResponseCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func (c *ResponseCache) keyFor(tenantID, question, asOf string) cacheKey {
	return cacheKey{
		tenantID:     tenantID,
		questionHash: HashQuestion(question),
		asOfBucket:   strings.TrimSpace(asOf),
	}
}

func (c *ResponseCache) evictLocked() {
	for len(c.entries) > c.capacity {
		back := c.lru.Back()
		if back == nil {
			return
		}
		c.removeLocked(back.Value.(*cacheEntry))
	}
}

func (c *ResponseCache) removeLocked(entry *cacheEntry) {
	c.lru.Remove(entry.elem)
	delete(c.entries, entry.key)
}

var whitespaceRun = regexp.MustCompile(`\s+`)

// HashQuestion normalizes a leadership question and returns a stable hex digest. The
// normalization (lowercase, collapse whitespace, trim trailing punctuation) makes
// "How many goats?" and "how many   goats" share a cache slot without conflating
// genuinely different questions. It is exported so callers can log/compare the hash.
func HashQuestion(question string) string {
	norm := NormalizeQuestion(question)
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// NormalizeQuestion canonicalizes question text for cache-key stability.
func NormalizeQuestion(question string) string {
	q := strings.ToLower(strings.TrimSpace(question))
	q = whitespaceRun.ReplaceAllString(q, " ")
	q = strings.TrimRight(q, " ?.!,;:")
	return q
}

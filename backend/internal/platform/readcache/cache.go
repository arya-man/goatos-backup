// Package readcache is the ONE in-process read cache for heavy, tenant+park scoped analytics
// reads (weighing Weights/ADG screens, Growth Director FCR). It replaces the two hand-rolled
// map+epoch caches that lived in weighing/adapters/postgres/growth.go and
// growthdirector/adapters/postgres/repository.go; do not add a third.
//
// Contract:
//
//   - Keyed by tenant + park set + normalized request params (Key). The tenant is always part of
//     the key, so one tenant can never be served another tenant's answer.
//   - Single flight on a miss: N concurrent identical requests run ONE load. The load runs on a
//     context detached from the first caller's cancellation, so a client that disconnects does
//     not poison the other waiters with context.Canceled.
//   - Stale-while-revalidate: once an entry passes FreshTTL it is still served for StaleGrace
//     while exactly one bounded background refresh re-loads it. SWR is only enabled while the
//     cache is COHERENT (the cross-instance eviction feed is connected, see Listener). Without
//     the feed, entries live DegradedTTL and are never served stale -- the old 30 s behaviour.
//   - Event-driven eviction: a committed write evicts only the affected tenant (and park set, when
//     the writer knows it). Eviction also bumps a per-tenant generation, so a load that STARTED
//     before the write can never repopulate the cache with pre-write data after it.
//   - Bounded LRU (MaxEntries), hit/miss/stale/evict counters (Stats + OpenTelemetry).
package readcache

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Key identifies one cached read. Parks empty means the read spans the whole tenant (it is
// evicted by any write in that tenant).
type Key struct {
	Tenant string
	Parks  []string
	Params string
}

func (k Key) normalizedParks() []string {
	parks := make([]string, 0, len(k.Parks))
	seen := map[string]struct{}{}
	for _, p := range k.Parks {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		parks = append(parks, p)
	}
	sort.Strings(parks)
	return parks
}

// String is the canonical cache key: tenant, sorted de-duplicated parks, params.
func (k Key) String() string {
	return strings.TrimSpace(k.Tenant) + "|" + strings.Join(k.normalizedParks(), ",") + "|" + k.Params
}

type Options struct {
	Name               string
	MaxEntries         int
	FreshTTL           time.Duration
	StaleGrace         time.Duration
	DegradedTTL        time.Duration
	RefreshConcurrency int
	Now                func() time.Time
}

// DefaultOptions: 60 s fresh + 5 min stale grace while coherent; 30 s and no stale serving when
// the eviction feed is down. 512 entries bounds memory to a few MB of report structs.
func DefaultOptions(name string) Options {
	return Options{
		Name:               name,
		MaxEntries:         512,
		FreshTTL:           60 * time.Second,
		StaleGrace:         5 * time.Minute,
		DegradedTTL:        30 * time.Second,
		RefreshConcurrency: 2,
	}
}

// Stats is a snapshot of the cache counters.
type Stats struct {
	Hits, Misses, Shared, Stale, Refreshes, RefreshErrors, Evictions, LRUEvictions, DroppedStores int64
	Entries                                                                                       int
	Coherent                                                                                      bool
}

type entry struct {
	key        string
	tenant     string
	parks      map[string]struct{}
	value      any
	storedAt   time.Time
	coherent   bool
	refreshing bool
}

type flight struct {
	tenant string
	parks  map[string]struct{}
	done   chan struct{}
	value  any
	err    error
}

type Cache struct {
	opts Options

	mu        sync.Mutex
	lru       *list.List
	entries   map[string]*list.Element
	flights   map[string]*flight
	tenantGen map[string]uint64
	globalGen uint64

	coherent     atomic.Bool
	refreshSlots chan struct{}
	warmOnce     sync.Once

	hits, misses, shared, stale, refreshes, refreshErrors, evictions, lruEvictions, droppedStores atomic.Int64
}

func New(opts Options) *Cache {
	def := DefaultOptions(opts.Name)
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = def.MaxEntries
	}
	if opts.FreshTTL <= 0 {
		opts.FreshTTL = def.FreshTTL
	}
	if opts.StaleGrace < 0 {
		opts.StaleGrace = 0
	}
	if opts.DegradedTTL <= 0 {
		opts.DegradedTTL = def.DegradedTTL
	}
	if opts.RefreshConcurrency <= 0 {
		opts.RefreshConcurrency = def.RefreshConcurrency
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Cache{
		opts:         opts,
		lru:          list.New(),
		entries:      map[string]*list.Element{},
		flights:      map[string]*flight{},
		tenantGen:    map[string]uint64{},
		refreshSlots: make(chan struct{}, opts.RefreshConcurrency),
	}
}

// SetCoherent records whether the cross-instance eviction feed is live. Only a coherent cache
// may serve stale entries or keep them past DegradedTTL.
func (c *Cache) SetCoherent(v bool) {
	if c == nil {
		return
	}
	c.coherent.Store(v)
}

func (c *Cache) Coherent() bool { return c != nil && c.coherent.Load() }

// Name is Options.Name; an eviction payload that names caches is matched against it.
func (c *Cache) Name() string {
	if c == nil {
		return ""
	}
	return c.opts.Name
}

// ErrUnexpectedType is returned when a cached value is not the type the caller asked for, which
// means two reads share a key prefix -- a key bug, never served silently.
var ErrUnexpectedType = errors.New("readcache: cached value has unexpected type")

// Load returns the cached value for key or runs load exactly once across concurrent callers.
// A nil cache simply runs load.
func Load[V any](ctx context.Context, c *Cache, key Key, load func(context.Context) (V, error)) (V, error) {
	var zero V
	if c == nil {
		return load(ctx)
	}
	anyLoad := func(ctx context.Context) (any, error) { return load(ctx) }
	value, err := c.get(ctx, key, anyLoad)
	if err != nil {
		return zero, err
	}
	out, ok := value.(V)
	if !ok {
		return zero, fmt.Errorf("%w: %s wants %T, got %T", ErrUnexpectedType, c.opts.Name, zero, value)
	}
	return out, nil
}

func (c *Cache) get(ctx context.Context, key Key, load func(context.Context) (any, error)) (any, error) {
	ks := key.String()
	tenant := strings.TrimSpace(key.Tenant)
	now := c.opts.Now()
	coherent := c.coherent.Load()

	c.mu.Lock()
	if el, ok := c.entries[ks]; ok {
		e := el.Value.(*entry)
		age := now.Sub(e.storedAt)
		freshFor := c.opts.DegradedTTL
		if coherent && e.coherent {
			freshFor = c.opts.FreshTTL
		}
		if age < freshFor {
			c.lru.MoveToFront(el)
			c.mu.Unlock()
			c.hits.Add(1)
			record(ctx, c.opts.Name, "hit")
			return e.value, nil
		}
		if coherent && e.coherent && age < c.opts.FreshTTL+c.opts.StaleGrace {
			c.lru.MoveToFront(el)
			value := e.value
			if !e.refreshing {
				select {
				case c.refreshSlots <- struct{}{}:
					e.refreshing = true
					gen := c.genLocked(tenant)
					go c.refresh(context.WithoutCancel(ctx), ks, key, gen, load)
				default:
					// Refresh capacity is busy: serve stale now, a later hit retries.
				}
			}
			c.mu.Unlock()
			c.stale.Add(1)
			record(ctx, c.opts.Name, "stale")
			return value, nil
		}
	}
	if f, ok := c.flights[ks]; ok {
		c.mu.Unlock()
		c.shared.Add(1)
		record(ctx, c.opts.Name, "shared")
		return waitFlight(ctx, f)
	}
	f := &flight{tenant: tenant, parks: parkSet(key), done: make(chan struct{})}
	c.flights[ks] = f
	gen := c.genLocked(tenant)
	c.mu.Unlock()
	c.misses.Add(1)
	record(ctx, c.opts.Name, "miss")

	go func(loadCtx context.Context) {
		value, err := safeLoad(loadCtx, load)
		c.finish(ks, key, f, gen, value, err)
	}(context.WithoutCancel(ctx))
	return waitFlight(ctx, f)
}

func safeLoad(ctx context.Context, load func(context.Context) (any, error)) (value any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("readcache: load panicked: %v", r)
		}
	}()
	return load(ctx)
}

func waitFlight(ctx context.Context, f *flight) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		return f.value, f.err
	}
}

func (c *Cache) refresh(ctx context.Context, ks string, key Key, gen uint64, load func(context.Context) (any, error)) {
	defer func() { <-c.refreshSlots }()
	c.refreshes.Add(1)
	record(ctx, c.opts.Name, "refresh")
	value, err := safeLoad(ctx, load)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[ks]; ok {
		el.Value.(*entry).refreshing = false
	}
	if err != nil {
		c.refreshErrors.Add(1)
		return
	}
	c.storeLocked(ks, key, gen, value)
}

func (c *Cache) finish(ks string, key Key, f *flight, gen uint64, value any, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.storeLocked(ks, key, gen, value)
	}
	f.value, f.err = value, err
	close(f.done)
	if c.flights[ks] == f {
		delete(c.flights, ks)
	}
}

// genLocked is the generation a load captures when it starts; any eviction touching the tenant
// (or EvictAll) changes it, and a load that finishes under a changed generation is not stored.
func (c *Cache) genLocked(tenant string) uint64 {
	return c.globalGen<<32 + c.tenantGen[tenant]
}

func (c *Cache) storeLocked(ks string, key Key, gen uint64, value any) {
	tenant := strings.TrimSpace(key.Tenant)
	if c.genLocked(tenant) != gen {
		c.droppedStores.Add(1)
		return
	}
	now := c.opts.Now()
	if el, ok := c.entries[ks]; ok {
		e := el.Value.(*entry)
		e.value, e.storedAt, e.coherent, e.refreshing = value, now, c.coherent.Load(), false
		c.lru.MoveToFront(el)
		return
	}
	e := &entry{key: ks, tenant: tenant, parks: parkSet(key), value: value, storedAt: now, coherent: c.coherent.Load()}
	c.entries[ks] = c.lru.PushFront(e)
	for c.lru.Len() > c.opts.MaxEntries {
		oldest := c.lru.Back()
		c.lru.Remove(oldest)
		delete(c.entries, oldest.Value.(*entry).key)
		c.lruEvictions.Add(1)
	}
}

func parkSet(key Key) map[string]struct{} {
	parks := key.normalizedParks()
	if len(parks) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(parks))
	for _, p := range parks {
		out[p] = struct{}{}
	}
	return out
}

// scopeMatches: an empty eviction park list, or an entry/flight that spans the whole tenant, or
// any shared park, is affected.
func scopeMatches(entryParks map[string]struct{}, evictParks []string) bool {
	if len(evictParks) == 0 || len(entryParks) == 0 {
		return true
	}
	for _, p := range evictParks {
		if _, ok := entryParks[p]; ok {
			return true
		}
	}
	return false
}

// Evict drops every entry of tenant whose park set intersects parkIDs (all of the tenant's
// entries when parkIDs is empty). Other tenants are untouched. In-flight loads for the tenant
// are detached so a request arriving after the write starts a fresh load instead of joining a
// pre-write one, and their results are not stored.
func (c *Cache) Evict(ctx context.Context, tenantID string, parkIDs ...string) int {
	if c == nil {
		return 0
	}
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		return c.EvictAll(ctx)
	}
	parks := Key{Parks: parkIDs}.normalizedParks()
	c.mu.Lock()
	c.tenantGen[tenant]++
	n := 0
	for ks, el := range c.entries {
		e := el.Value.(*entry)
		if e.tenant == tenant && scopeMatches(e.parks, parks) {
			c.lru.Remove(el)
			delete(c.entries, ks)
			n++
		}
	}
	for ks, f := range c.flights {
		if f.tenant == tenant && scopeMatches(f.parks, parks) {
			delete(c.flights, ks)
		}
	}
	c.mu.Unlock()
	c.evictions.Add(int64(n))
	recordEvict(ctx, c.opts.Name, "scoped", n)
	return n
}

// EvictAll drops everything; used when the eviction feed (re)connects and may have missed
// notifications.
func (c *Cache) EvictAll(ctx context.Context) int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	c.globalGen++
	n := len(c.entries)
	c.entries = map[string]*list.Element{}
	c.lru.Init()
	c.flights = map[string]*flight{}
	c.mu.Unlock()
	c.evictions.Add(int64(n))
	recordEvict(ctx, c.opts.Name, "all", n)
	return n
}

func (c *Cache) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	return Stats{
		Hits: c.hits.Load(), Misses: c.misses.Load(), Shared: c.shared.Load(), Stale: c.stale.Load(),
		Refreshes: c.refreshes.Load(), RefreshErrors: c.refreshErrors.Load(), Evictions: c.evictions.Load(),
		LRUEvictions: c.lruEvictions.Load(), DroppedStores: c.droppedStores.Load(), Entries: n, Coherent: c.coherent.Load(),
	}
}

package app

import (
	"context"
	"strings"
	"sync"
	"time"
)

// liveCohortCache is the per-instance, per-(tenant, filter) snapshot cache for the two
// whole-cohort live reads: the enriched, risk-scored cohort behind the risk_state filter, and the
// per-pen comparison medians behind every page.
//
// The risk filter genuinely needs the whole filtered cohort enriched (risk_state is scored from
// per-tag 24h baselines and pen medians, then filtered and paged in memory), and that walk costs
// seconds at the 50k envelope. Before this cache every request AND every SSE viewer on every
// NOTIFY (~2s) re-ran it. Now:
//   - a fresh entry (< liveCohortTTL, not invalidated) is served as-is;
//   - an entry invalidated by a NOTIFY or past its TTL but younger than liveCohortMaxStale is
//     served immediately while ONE background refresh runs (stale-while-revalidate), unless the
//     caller asked for a fresh read (the stream hub after a NOTIFY), which waits on the refresh;
//   - concurrent misses for one key share a single computation (single-flight).
const (
	liveCohortTTL = 12 * time.Second
	// liveCohortMinRefresh stops a NOTIFY every ~2s from turning stale-while-revalidate into a
	// background cohort walk every ~2s: a stale entry younger than this is served without refresh.
	liveCohortMinRefresh = 5 * time.Second
	liveCohortMaxStale   = 60 * time.Second
	liveCohortMaxEntries = 64
	liveCohortComputeCap = 5 * time.Second
)

type liveCohortEntry[T any] struct {
	items    T
	loaded   bool
	loadedAt time.Time
	stale    bool
	gen      uint64 // bumped by every invalidation
	inflight *liveCohortFlight[T]
}

type liveCohortFlight[T any] struct {
	done  chan struct{}
	items T
	err   error
}

type liveCohortCache[T any] struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]*liveCohortEntry[T]
}

func newLiveCohortCache[T any]() *liveCohortCache[T] {
	return &liveCohortCache[T]{now: time.Now, entries: make(map[string]*liveCohortEntry[T])}
}

func liveCohortKey(tenantID string, parts ...*string) string {
	var b strings.Builder
	b.WriteString(tenantID)
	b.WriteByte(0) // invalidate matches on this tenant prefix
	for _, p := range parts {
		b.WriteByte(1)
		if p != nil {
			b.WriteString(*p)
		}
	}
	return b.String()
}

// get returns the cohort for key, computing it at most once concurrently. Returned slices are
// shared and MUST be treated as read-only by callers.
func (c *liveCohortCache[T]) get(ctx context.Context, key string, requireFresh bool, compute func(context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	now := c.now()
	e := c.entries[key]
	if e != nil && e.loaded {
		age := now.Sub(e.loadedAt)
		fresh := !e.stale && age < liveCohortTTL
		if fresh {
			items := e.items
			c.mu.Unlock()
			return items, nil
		}
		if !requireFresh && age < liveCohortMaxStale {
			items := e.items
			if e.inflight == nil && age >= liveCohortMinRefresh {
				c.startFlightLocked(key, e, compute)
			}
			c.mu.Unlock()
			return items, nil
		}
	}
	if e == nil {
		c.evictLocked()
		e = &liveCohortEntry[T]{}
		c.entries[key] = e
	}
	flight := e.inflight
	if flight == nil {
		flight = c.startFlightLocked(key, e, compute)
	}
	c.mu.Unlock()

	select {
	case <-flight.done:
		return flight.items, flight.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// startFlightLocked runs compute detached from any one caller's context (so a disconnecting
// viewer cannot cancel the result other viewers are waiting on), bounded by liveCohortComputeCap.
func (c *liveCohortCache[T]) startFlightLocked(key string, e *liveCohortEntry[T], compute func(context.Context) (T, error)) *liveCohortFlight[T] {
	flight := &liveCohortFlight[T]{done: make(chan struct{})}
	e.inflight = flight
	startedAt := c.now()
	startGen := e.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), liveCohortComputeCap)
		defer cancel()
		items, err := compute(ctx)
		c.mu.Lock()
		flight.items, flight.err = items, err
		if cur := c.entries[key]; cur == e {
			e.inflight = nil
			if err == nil {
				e.items = items
				e.loaded = true
				e.loadedAt = startedAt
				// A NOTIFY that landed while this computed may not be reflected in it: stay stale.
				e.stale = e.gen != startGen
			}
		}
		c.mu.Unlock()
		close(flight.done)
	}()
	return flight
}

// invalidate marks every entry of tenantID stale (a NOTIFY for that tenant).
func (c *liveCohortCache[T]) invalidate(tenantID string) {
	prefix := tenantID + "\x00"
	c.mu.Lock()
	for k, e := range c.entries {
		if strings.HasPrefix(k, prefix) {
			e.stale = true
			e.gen++
		}
	}
	c.mu.Unlock()
}

func (c *liveCohortCache[T]) invalidateAll() {
	c.mu.Lock()
	for _, e := range c.entries {
		e.stale = true
		e.gen++
	}
	c.mu.Unlock()
}

func (c *liveCohortCache[T]) evictLocked() {
	if len(c.entries) < liveCohortMaxEntries {
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, e := range c.entries {
		if e.inflight != nil {
			continue
		}
		if oldestKey == "" || e.loadedAt.Before(oldest) {
			oldestKey, oldest = k, e.loadedAt
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

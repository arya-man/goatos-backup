// Package memcache is an in-process TTL/LRU cache adapter for ceoai answers.
// Keyed by (tenant, normalized question, as_of bucket) — the key is built by
// the orchestrator, so this adapter never crosses tenants. A Redis adapter can
// drop in behind the same ports.Cache interface for multi-instance deploys.
package memcache

import (
	"container/list"
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

type entry struct {
	key       string
	answer    domain.Answer
	expiresAt time.Time
}

// Cache is a bounded TTL+LRU cache implementing ports.Cache.
type Cache struct {
	mu       sync.Mutex
	ttl      time.Duration
	capacity int
	ll       *list.List
	items    map[string]*list.Element
	now      func() time.Time
}

// New builds a cache with the given TTL and max entries.
func New(ttl time.Duration, capacity int) *Cache {
	if capacity <= 0 {
		capacity = 256
	}
	return &Cache{
		ttl: ttl, capacity: capacity, ll: list.New(),
		items: map[string]*list.Element{}, now: time.Now,
	}
}

// Get returns a non-expired cached answer.
func (c *Cache) Get(key string) (domain.Answer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return domain.Answer{}, false
	}
	e := el.Value.(*entry)
	if c.now().After(e.expiresAt) {
		c.removeElement(el)
		return domain.Answer{}, false
	}
	c.ll.MoveToFront(el)
	return e.answer, true
}

// Set stores an answer, evicting the LRU entry past capacity.
func (c *Cache) Set(key string, ans domain.Answer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		e.answer = ans
		e.expiresAt = c.now().Add(c.ttl)
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&entry{key: key, answer: ans, expiresAt: c.now().Add(c.ttl)})
	c.items[key] = el
	for c.ll.Len() > c.capacity {
		c.removeElement(c.ll.Back())
	}
}

func (c *Cache) removeElement(el *list.Element) {
	if el == nil {
		return
	}
	c.ll.Remove(el)
	delete(c.items, el.Value.(*entry).key)
}

package app

import (
	"container/list"
	"context"
	"sync"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// InMemoryMemory is the default bounded, in-process session memory of resolved
// entities (park/shed/metric scope) keyed by tenant + conversation. It exists so
// pronoun / elliptical follow-ups within one conversation ("and yesterday?",
// "why is that one behind?") can bind the scope resolved on the prior turn.
//
// Root cause it fixes: without a wired MemoryStore the orchestrator recalled
// nothing, so every follow-up planned from scratch and dropped the park/shed
// the previous turn had established. This is process-local (a single backend
// serves a conversation) and strictly tenant-scoped: the key embeds the tenant
// id so one tenant can never recall another's scope. Entries are capped per
// conversation (last N turns) and the conversation set is LRU-capped so a
// long-lived process cannot grow unbounded.
type InMemoryMemory struct {
	mu        sync.Mutex
	perConvo  int // max resolved-entity snapshots kept per conversation
	maxConvos int // LRU cap on distinct conversations
	entries   map[string][]domain.ResolvedEntities
	lru       *list.List               // front = most-recently used key
	lruByKey  map[string]*list.Element // key -> element for O(1) touch
}

// NewInMemoryMemory builds the default session memory. perConvo bounds snapshots
// per conversation; maxConvos bounds distinct conversations (LRU eviction).
func NewInMemoryMemory(perConvo, maxConvos int) *InMemoryMemory {
	if perConvo <= 0 {
		perConvo = 6
	}
	if maxConvos <= 0 {
		maxConvos = 4096
	}
	return &InMemoryMemory{
		perConvo:  perConvo,
		maxConvos: maxConvos,
		entries:   make(map[string][]domain.ResolvedEntities),
		lru:       list.New(),
		lruByKey:  make(map[string]*list.Element),
	}
}

func memKey(actor domain.Actor, conversationID string) string {
	return actor.TenantID + "|" + conversationID
}

// Recall returns the resolved-entity snapshots for the conversation, oldest to
// newest (the orchestrator/prompt reads the last one for the current scope).
func (m *InMemoryMemory) Recall(_ context.Context, actor domain.Actor, conversationID string) ([]domain.ResolvedEntities, error) {
	if conversationID == "" {
		return nil, nil
	}
	key := memKey(actor, conversationID)
	m.mu.Lock()
	defer m.mu.Unlock()
	snaps := m.entries[key]
	if len(snaps) == 0 {
		return nil, nil
	}
	m.touchLocked(key)
	out := make([]domain.ResolvedEntities, len(snaps))
	copy(out, snaps)
	return out, nil
}

// Remember appends the newly resolved scope for the conversation. Empty scope
// (no park/shed/metric resolved) is not stored so a scopeless turn cannot wipe a
// prior park binding a later follow-up still needs.
func (m *InMemoryMemory) Remember(_ context.Context, actor domain.Actor, conversationID string, ent domain.ResolvedEntities) error {
	if conversationID == "" {
		return nil
	}
	if ent.ParkLabel == "" && ent.ShedLabel == "" && ent.Metric == "" && ent.IntentClass == "" {
		return nil
	}
	key := memKey(actor, conversationID)
	m.mu.Lock()
	defer m.mu.Unlock()

	snaps := append(m.entries[key], ent)
	if len(snaps) > m.perConvo {
		snaps = snaps[len(snaps)-m.perConvo:]
	}
	m.entries[key] = snaps
	m.touchLocked(key)
	m.evictLocked()
	return nil
}

// touchLocked marks key most-recently used; caller holds the lock.
func (m *InMemoryMemory) touchLocked(key string) {
	if el, ok := m.lruByKey[key]; ok {
		m.lru.MoveToFront(el)
		return
	}
	m.lruByKey[key] = m.lru.PushFront(key)
}

// evictLocked drops the least-recently used conversations past the cap.
func (m *InMemoryMemory) evictLocked() {
	for m.lru.Len() > m.maxConvos {
		back := m.lru.Back()
		if back == nil {
			return
		}
		key, _ := back.Value.(string)
		m.lru.Remove(back)
		delete(m.lruByKey, key)
		delete(m.entries, key)
	}
}

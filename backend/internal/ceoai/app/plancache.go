package app

// plancache.go: a short-lived, shape-keyed cache of the PLAN — never of the
// data.
//
// The same question asked twice replanned from scratch, paying the planner's
// Vertex round trip (the single largest fixed cost of an ask) to produce the
// same routing it produced a minute ago. This caches WHICH READS ANSWER THIS
// QUESTION and nothing else: every cached plan is executed again against the
// live database, so a leader asking twice still sees today's numbers. Nothing
// here can make an answer stale — only the choice of read is reused.
//
// The key carries the TENANT and the USER, and that is load-bearing rather
// than tidy. The conversation id is client-supplied and only proven to belong
// to the caller later, so a key without the user let a same-tenant colleague
// replay another user's conversation id and receive their cached answer — the
// cross-user replay the answer cache's own key was fixed for. A plan is a
// smaller disclosure than an answer, but it still carries the park/shed
// filters the other user's question resolved to, so it is keyed the same way
// and the entry re-checks the actor's tenant on the way out.

import (
	"strings"
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// planCacheTTL is deliberately short. The plan depends on the schema cards and
// the tool catalog, which a deploy can change under a running process, and on
// the resolved window, which turns over at the business-day boundary the key
// already carries.
const (
	planCacheTTL     = 10 * time.Minute
	planCacheEntries = 256
)

type planCacheEntry struct {
	tenantID  string
	plan      domain.Plan
	expiresAt time.Time
}

// planCache is a bounded, in-process TTL cache of model plans.
type planCache struct {
	mu      sync.Mutex
	entries map[string]planCacheEntry
	ttl     time.Duration
	max     int
	now     func() time.Time
}

func newPlanCache(ttl time.Duration, max int, now func() time.Time) *planCache {
	if ttl <= 0 {
		ttl = planCacheTTL
	}
	if max <= 0 {
		max = planCacheEntries
	}
	if now == nil {
		now = time.Now
	}
	return &planCache{entries: map[string]planCacheEntry{}, ttl: ttl, max: max, now: now}
}

// get returns a live plan for this key, but only to an actor of the tenant it
// was stored for. The tenant re-check is defence in depth: the key already
// starts with the tenant id, so a hit for another tenant means the key was
// built wrong, and the honest response to that is a miss.
func (c *planCache) get(key string, actor domain.Actor) (domain.Plan, bool) {
	if c == nil {
		return domain.Plan{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return domain.Plan{}, false
	}
	if c.now().After(e.expiresAt) {
		delete(c.entries, key)
		return domain.Plan{}, false
	}
	if e.tenantID == "" || e.tenantID != actor.TenantID {
		return domain.Plan{}, false
	}
	return clonePlan(e.plan), true
}

func (c *planCache) set(key string, actor domain.Actor, plan domain.Plan) {
	if c == nil || key == "" || actor.TenantID == "" || len(plan.SubQuestions) == 0 || plan.Refusal != "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		// Bounded memory: drop whatever has expired, and if nothing has,
		// drop one entry rather than grow without limit.
		now := c.now()
		for k, e := range c.entries {
			if now.After(e.expiresAt) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.max {
			for k := range c.entries {
				delete(c.entries, k)
				break
			}
		}
	}
	c.entries[key] = planCacheEntry{tenantID: actor.TenantID, plan: clonePlan(plan), expiresAt: c.now().Add(c.ttl)}
}

// clonePlan deep-copies a plan's sub-questions and their param maps. The
// executor WRITES into those maps (the as-of day, the resolved window, the
// repaired SQL), so a shared map would hand a later ask another day's literals
// — a stale window bound into a cached plan is exactly the "cached data"
// failure this cache exists to avoid.
func clonePlan(p domain.Plan) domain.Plan {
	out := domain.Plan{Refusal: p.Refusal}
	if p.SubQuestions == nil {
		return out
	}
	out.SubQuestions = make([]domain.SubQuestion, 0, len(p.SubQuestions))
	for _, s := range p.SubQuestions {
		c := s
		if s.Params != nil {
			c.Params = make(map[string]any, len(s.Params))
			for k, v := range s.Params {
				c.Params[k] = v
			}
		}
		if s.Declared.Dimensions != nil {
			c.Declared.Dimensions = append([]string(nil), s.Declared.Dimensions...)
		}
		if s.Declared.MeasureTerms != nil {
			c.Declared.MeasureTerms = append([]string(nil), s.Declared.MeasureTerms...)
		}
		out.SubQuestions = append(out.SubQuestions, c)
	}
	return out
}

// planCacheKey is the SHAPE of the ask: tenant, user, conversation, business
// day and the normalised question. It deliberately mirrors the answer cache's
// key rather than being cleverer about it — a plan resolved with a prior
// turn's park/shed in scope belongs to that conversation, and the business day
// is what makes "yesterday" mean the same thing on a hit as on the miss that
// stored it.
func (a *Assistant) planCacheKey(q domain.Question) string {
	day := q.AsOf.In(biztime.DefaultLocation()).Format("2006-01-02")
	norm := strings.ToLower(strings.Join(strings.Fields(q.Text), " "))
	norm = strings.TrimRight(norm, " ?.!")
	conversation := strings.TrimSpace(q.ConversationID)
	if conversation == "" {
		conversation = "no-conversation"
	}
	user := strings.TrimSpace(q.Actor.UserID)
	if user == "" {
		user = "no-user"
	}
	return "plan|" + q.Actor.TenantID + "|" + user + "|" + conversation + "|" + day + "|" + norm
}

package app

import (
	"context"
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// InMemoryBudget is a per-tenant/day + per-user/day token/cost cap. It survives
// only in-process; the persistence adapter provides the durable variant. Never
// silently drops requests: over-budget returns a friendly message upstream.
type InMemoryBudget struct {
	mu             sync.Mutex
	day            string
	perTenantToken map[string]int
	perUserToken   map[string]int
	tenantCap      int
	userCap        int
	now            func() time.Time
}

// NewInMemoryBudget builds a budget with per-tenant/day and per-user/day token
// caps (input+output combined).
func NewInMemoryBudget(tenantCap, userCap int) *InMemoryBudget {
	return &InMemoryBudget{
		perTenantToken: map[string]int{},
		perUserToken:   map[string]int{},
		tenantCap:      tenantCap,
		userCap:        userCap,
		now:            time.Now,
	}
}

func (b *InMemoryBudget) rollDay() {
	d := b.now().In(time.UTC).Format("2006-01-02")
	if d != b.day {
		b.day = d
		b.perTenantToken = map[string]int{}
		b.perUserToken = map[string]int{}
	}
}

// Reserve reports whether the actor is within budget for another request.
func (b *InMemoryBudget) Reserve(_ context.Context, actor domain.Actor) (bool, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollDay()
	if b.tenantCap > 0 && b.perTenantToken[actor.TenantID] >= b.tenantCap {
		return false, "Your organization has reached today's assistant usage limit. Please try again tomorrow."
	}
	if b.userCap > 0 && b.perUserToken[actor.UserID] >= b.userCap {
		return false, "You've reached today's assistant usage limit. Please try again tomorrow."
	}
	return true, ""
}

// Record accrues token usage after a request completes.
func (b *InMemoryBudget) Record(_ context.Context, actor domain.Actor, inputTokens, outputTokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollDay()
	total := inputTokens + outputTokens
	b.perTenantToken[actor.TenantID] += total
	b.perUserToken[actor.UserID] += total
}

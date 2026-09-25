package app

// D0 tenant-isolation proofs for the in-process answer cache and the session
// memory (plan-v3 D0, "Cache" and "Memory / resume" rows).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

func actorFor(tenant, user string) domain.Actor {
	return domain.Actor{TenantID: tenant, UserID: user, Role: permissions.RoleCEOInternal}
}

// TestCacheKeyTenantFirst pins the key layout tenant | user | conversation |
// IST day | normalized text: the tenant is the FIRST component, so no prefix scan,
// namespace collision or byte-identical question can ever cross tenants, and a
// different tenant with the same conversation id, day and text hashes apart.
func TestCacheKeyTenantFirst(t *testing.T) {
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Registry: NewRegistry(nil, nil, nil)})
	asOf := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	qA := domain.Question{Actor: actorFor("tenant-A", "u1"), ConversationID: "conv-1", Text: "How many  goats?", AsOf: asOf}
	qB := domain.Question{Actor: actorFor("tenant-B", "u1"), ConversationID: "conv-1", Text: "How many  goats?", AsOf: asOf}

	keyA, keyB := a.cacheKey(qA), a.cacheKey(qB)
	if !strings.HasPrefix(keyA, "tenant-A|") || !strings.HasPrefix(keyB, "tenant-B|") {
		t.Fatalf("tenant must be the first key component: %q / %q", keyA, keyB)
	}
	if keyA == keyB {
		t.Fatalf("same question in two tenants must not share a cache key: %q", keyA)
	}
	parts := strings.SplitN(keyA, "|", 5)
	if len(parts) != 5 || parts[0] != "tenant-A" || parts[1] != "u1" || parts[2] != "conv-1" || parts[3] != "2026-09-19" || parts[4] != "how many goats?" {
		t.Fatalf("key layout must be tenant|user|conversation|IST day|normalized text, got %q", keyA)
	}
	// Same tenant, same conversation id, different user: the conversation id is
	// client-supplied and only proven to be the caller's later, so the key must
	// already differ here.
	if a.cacheKey(domain.Question{Actor: actorFor("tenant-A", "u2"), ConversationID: "conv-1", Text: "How many  goats?", AsOf: asOf}) == keyA {
		t.Fatal("same-tenant different user must not share a cache key")
	}
	// Whitespace/case normalisation never touches the tenant component.
	if a.cacheKey(domain.Question{Actor: actorFor("TENANT-A", "u1"), ConversationID: "conv-1", Text: "how many goats?", AsOf: asOf}) == keyA {
		t.Fatal("tenant id must be compared verbatim, not normalised")
	}
}

// TestCacheCrossTenantMiss runs the real Ask path twice with a byte-identical
// question: tenant A warms the cache, tenant B must MISS (planner runs again)
// and B's answer must never be A's cached body; A's second ask is a hit.
func TestCacheCrossTenantMiss(t *testing.T) {
	metrics := &fakeMetrics{specs: []ports.MetricSpec{{Name: "active_animals"}},
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "tenant-A", Label: "Active animals", Value: "2567"}}}}
	reg := NewRegistry(metrics, nil, nil)
	cache := &countingCache{m: map[string]domain.Answer{}}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteCube},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics, Cache: cache})
	asOf := time.Now()

	ansA, err := a.Ask(context.Background(), domain.Question{Actor: actorFor("tenant-A", "ceo-A"), Text: "total animals", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 || len(cache.m) != 1 {
		t.Fatalf("first ask must plan once and store once: calls=%d entries=%d", prov.calls, len(cache.m))
	}

	// Tenant B, same text, same day: MISS.
	metrics.result = domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "tenant-B", Label: "Active animals", Value: "41"}}}
	ansB, err := a.Ask(context.Background(), domain.Question{Actor: actorFor("tenant-B", "ceo-B"), Text: "total animals", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if prov.calls != 2 {
		t.Fatalf("tenant B must miss the cache and plan again, planner calls=%d", prov.calls)
	}
	if len(cache.m) != 2 {
		t.Fatalf("tenant B's answer must be stored under its own key, entries=%d", len(cache.m))
	}
	if ansB.Answer == ansA.Answer || strings.Contains(ansB.Answer, "2567") {
		t.Fatalf("tenant B received tenant A's cached answer: %q", ansB.Answer)
	}
	for k := range cache.m {
		if !strings.HasPrefix(k, "tenant-A|") && !strings.HasPrefix(k, "tenant-B|") {
			t.Fatalf("cache key without tenant prefix: %q", k)
		}
	}

	// Tenant A again: HIT (planner not called), body unchanged.
	prov.calls = 0
	again, err := a.Ask(context.Background(), domain.Question{Actor: actorFor("tenant-A", "ceo-A"), Text: "total animals", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if prov.calls != 0 || again.Answer != ansA.Answer {
		t.Fatalf("tenant A second ask must be a hit with the same body: calls=%d", prov.calls)
	}
}

// TestMemoryRecallTenantScoped: resolved-entity memory (park/shed/metric scope
// for follow-ups) is keyed by (tenant, user, conversation). A conversation id
// remembered for tenant A is invisible to tenant B, and to another user of the
// same tenant, even with the identical conversation id.
func TestMemoryRecallTenantScoped(t *testing.T) {
	ctx := context.Background()
	mem := NewInMemoryMemory(4, 16)
	convID := "conv-shared-id"
	if err := mem.Remember(ctx, actorFor("tenant-A", "ceo-A"), convID, domain.ResolvedEntities{ParkLabel: "Coimbatore", Metric: "active_animals"}); err != nil {
		t.Fatal(err)
	}

	got, err := mem.Recall(ctx, actorFor("tenant-A", "ceo-A"), convID)
	if err != nil || len(got) != 1 || got[0].ParkLabel != "Coimbatore" {
		t.Fatalf("owner recall: %+v err=%v", got, err)
	}
	if got, _ := mem.Recall(ctx, actorFor("tenant-B", "ceo-B"), convID); len(got) != 0 {
		t.Fatalf("tenant B must not recall tenant A's scope: %+v", got)
	}
	if got, _ := mem.Recall(ctx, actorFor("tenant-B", "ceo-A"), convID); len(got) != 0 {
		t.Fatalf("same user id in another tenant must not recall tenant A's scope: %+v", got)
	}
	if got, _ := mem.Recall(ctx, actorFor("tenant-A", "cxo-A2"), convID); len(got) != 0 {
		t.Fatalf("another user of tenant A must not recall ceo-A's conversation scope: %+v", got)
	}

	// Tenant B remembering under the same conversation id never overwrites A.
	if err := mem.Remember(ctx, actorFor("tenant-B", "ceo-B"), convID, domain.ResolvedEntities{ParkLabel: "Northwind Park"}); err != nil {
		t.Fatal(err)
	}
	got, _ = mem.Recall(ctx, actorFor("tenant-A", "ceo-A"), convID)
	if len(got) != 1 || got[0].ParkLabel != "Coimbatore" {
		t.Fatalf("tenant A's memory must be untouched by tenant B: %+v", got)
	}

	// End to end through Ask: the orchestrator recalls with the SESSION actor,
	// so a follow-up from tenant B carrying A's conversation id plans with no
	// remembered scope.
	prov := &fakeProvider{}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: NewRegistry(nil, nil, nil), Memory: mem})
	_, _ = a.Ask(ctx, domain.Question{Actor: actorFor("tenant-B", "ceo-B"), ConversationID: "conv-only-A"})
	_ = mem.Remember(ctx, actorFor("tenant-A", "ceo-A"), "conv-only-A", domain.ResolvedEntities{ParkLabel: "Coimbatore"})
	recalled := &recallSpyProvider{}
	a = NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: recalled, Registry: NewRegistry(nil, nil, nil), Memory: mem})
	_, _ = a.Ask(ctx, domain.Question{Actor: actorFor("tenant-B", "ceo-B"), ConversationID: "conv-only-A", Text: "and yesterday?"})
	if len(recalled.mem) != 0 {
		t.Fatalf("tenant B's plan received tenant A's remembered scope: %+v", recalled.mem)
	}
	_, _ = a.Ask(ctx, domain.Question{Actor: actorFor("tenant-A", "ceo-A"), ConversationID: "conv-only-A", Text: "and yesterday?"})
	if len(recalled.mem) != 1 || recalled.mem[0].ParkLabel != "Coimbatore" {
		t.Fatalf("tenant A's plan must receive its own remembered scope: %+v", recalled.mem)
	}
}

// recallSpyProvider records the memory snapshots the orchestrator hands to Plan.
type recallSpyProvider struct {
	mem []domain.ResolvedEntities
}

func (p *recallSpyProvider) Plan(_ context.Context, _ domain.Question, mem []domain.ResolvedEntities, _ []ports.ToolSpec) (domain.Plan, error) {
	p.mem = mem
	return domain.Plan{}, nil
}
func (p *recallSpyProvider) PlannedByModel() bool { return false }

// TestCacheSameTenantCrossUserMiss: user A warms a conversation-scoped
// follow-up; user B in the SAME tenant replays A's conversation id, text and
// day and must MISS (planner runs again) — never A's cached body.
func TestCacheSameTenantCrossUserMiss(t *testing.T) {
	metrics := &fakeMetrics{specs: []ports.MetricSpec{{Name: "active_animals"}},
		result: domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "tenant-A", Label: "Active animals", Value: "2567"}}}}
	reg := NewRegistry(metrics, nil, nil)
	cache := &countingCache{m: map[string]domain.Answer{}}
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteCube},
	}}}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics, Cache: cache})
	asOf := time.Now()

	ansA, err := a.Ask(context.Background(), domain.Question{Actor: actorFor("tenant-A", "ceo-A"), ConversationID: "conv-A", Text: "and yesterday?", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("first ask must plan once, got %d", prov.calls)
	}
	metrics.result = domain.ToolResult{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "tenant-A", Label: "Active animals", Value: "99"}}}
	ansB, err := a.Ask(context.Background(), domain.Question{Actor: actorFor("tenant-A", "ceo-B"), ConversationID: "conv-A", Text: "and yesterday?", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if prov.calls != 2 {
		t.Fatalf("same-tenant different user must MISS the cache (planner calls=%d)", prov.calls)
	}
	if ansB.Answer == ansA.Answer || strings.Contains(ansB.Answer, "2567") {
		t.Fatalf("user B received user A's cached answer: %q", ansB.Answer)
	}
}

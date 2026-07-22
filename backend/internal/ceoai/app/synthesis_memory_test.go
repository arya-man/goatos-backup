package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

func TestSynthesizeLeadScopedBreakdown(t *testing.T) {
	results := []domain.ToolResult{{
		Surface: "Cube · active_animals",
		Facts: []domain.Fact{
			{Label: "Active animals", Value: "972", Scope: "goat"},
			{Label: "Active animals", Value: "336", Scope: "sheep"},
		},
	}}
	lead := synthesizeLead(results)
	if !strings.Contains(lead, "goat 972") || !strings.Contains(lead, "sheep 336") {
		t.Fatalf("lead must name each scoped figure, got %q", lead)
	}
}

func TestSynthesizeLeadMergesSameLabelAcrossResults(t *testing.T) {
	// The "goats vs sheep" planner shape: two species-FILTERED sub-queries, each
	// its own result with one scoped fact. The lead must name BOTH groups.
	results := []domain.ToolResult{
		{Surface: "Cube · active_animals", Facts: []domain.Fact{{Label: "Active animals", Value: "972", Scope: "goat"}}},
		{Surface: "Cube · active_animals", Facts: []domain.Fact{{Label: "Active animals", Value: "336", Scope: "sheep"}}},
	}
	lead := synthesizeLead(results)
	if !strings.Contains(lead, "goat 972") || !strings.Contains(lead, "sheep 336") {
		t.Fatalf("lead must merge same-label figures across results, got %q", lead)
	}
}

func TestSynthesizeLeadKeepsUnlikeMetricsSeparate(t *testing.T) {
	// Active vs Total are different metrics; the lead must not fold 59 under
	// "Active animals".
	results := []domain.ToolResult{
		{Facts: []domain.Fact{{Label: "Active animals", Value: "58"}}},
		{Facts: []domain.Fact{{Label: "Total animals", Value: "59"}}},
	}
	lead := synthesizeLead(results)
	if !strings.Contains(lead, "58") || strings.Contains(lead, "59") {
		t.Fatalf("lead must stay on the first metric only, got %q", lead)
	}
}

func TestSynthesizeLeadSingleValue(t *testing.T) {
	results := []domain.ToolResult{{Facts: []domain.Fact{{Label: "Active animals", Value: "58"}}}}
	if lead := synthesizeLead(results); !strings.Contains(lead, "58") {
		t.Fatalf("single-value lead must restate the figure, got %q", lead)
	}
}

func TestSynthesizeLeadNoNumericFacts(t *testing.T) {
	results := []domain.ToolResult{{Facts: []domain.Fact{{Label: "note", Value: "n/a"}}}}
	if lead := synthesizeLead(results); lead != "" {
		t.Fatalf("no numeric facts => no lead, got %q", lead)
	}
}

func TestInMemoryMemoryRecallByConversation(t *testing.T) {
	m := NewInMemoryMemory(0, 0)
	actor := domain.Actor{TenantID: "t1", UserID: "u1"}
	ctx := context.Background()
	if err := m.Remember(ctx, actor, "c1", domain.ResolvedEntities{ShedLabel: "Castro 1", Metric: "active_animals"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Recall(ctx, actor, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ShedLabel != "Castro 1" {
		t.Fatalf("expected Castro 1 recalled, got %+v", got)
	}
	// Cross-conversation isolation.
	if other, _ := m.Recall(ctx, actor, "c2"); len(other) != 0 {
		t.Fatalf("c2 must not see c1 scope, got %+v", other)
	}
	// Cross-tenant isolation.
	if xt, _ := m.Recall(ctx, domain.Actor{TenantID: "t2", UserID: "u1"}, "c1"); len(xt) != 0 {
		t.Fatalf("tenant t2 must not recall t1 scope, got %+v", xt)
	}
}

func TestInMemoryMemorySkipsEmptyScope(t *testing.T) {
	m := NewInMemoryMemory(0, 0)
	actor := domain.Actor{TenantID: "t1", UserID: "u1"}
	ctx := context.Background()
	_ = m.Remember(ctx, actor, "c1", domain.ResolvedEntities{ShedLabel: "Castro 1"})
	_ = m.Remember(ctx, actor, "c1", domain.ResolvedEntities{}) // scopeless follow-up
	got, _ := m.Recall(ctx, actor, "c1")
	if len(got) != 1 || got[len(got)-1].ShedLabel != "Castro 1" {
		t.Fatalf("scopeless turn must not wipe prior park, got %+v", got)
	}
}

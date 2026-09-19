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
			{TenantID: "t1", Label: "Active animals", Value: "972", Scope: "goat"},
			{TenantID: "t1", Label: "Active animals", Value: "336", Scope: "sheep"},
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
		{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "t1", Label: "Active animals", Value: "972", Scope: "goat"}}},
		{Surface: "Cube · active_animals", Facts: []domain.Fact{{TenantID: "t1", Label: "Active animals", Value: "336", Scope: "sheep"}}},
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
		{Facts: []domain.Fact{{TenantID: "t1", Label: "Active animals", Value: "58"}}},
		{Facts: []domain.Fact{{TenantID: "t1", Label: "Total animals", Value: "59"}}},
	}
	lead := synthesizeLead(results)
	if !strings.Contains(lead, "58") || strings.Contains(lead, "59") {
		t.Fatalf("lead must stay on the first metric only, got %q", lead)
	}
}

func TestSynthesizeLeadSingleValue(t *testing.T) {
	results := []domain.ToolResult{{Facts: []domain.Fact{{TenantID: "t1", Label: "Active animals", Value: "58"}}}}
	if lead := synthesizeLead(results); !strings.Contains(lead, "58") {
		t.Fatalf("single-value lead must restate the figure, got %q", lead)
	}
}

func TestSynthesizeLeadNoNumericFacts(t *testing.T) {
	results := []domain.ToolResult{{Facts: []domain.Fact{{TenantID: "t1", Label: "note", Value: "n/a"}}}}
	if lead := synthesizeLead(results); lead != "" {
		t.Fatalf("no numeric facts => no lead, got %q", lead)
	}
}

func TestComposerRendersSalesAsConversationNotFactDump(t *testing.T) {
	body, _, _ := composer{}.compose([]domain.ToolResult{{
		Surface: "Mesha read API · Sales overview",
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Sold animals this month in 2026-09", Value: "114"},
			{TenantID: "t1", Label: "Sold sheep this month in 2026-09", Value: "0"},
			{TenantID: "t1", Label: "Sold goats this month in 2026-09", Value: "114"},
			{TenantID: "t1", Label: "Sales revenue this month in 2026-09", Value: "1221067"},
		},
	}})
	for _, want := range []string{"For 2026-09", "114 animals were sold", "114 goats", "0 sheep", "1221067"} {
		if !strings.Contains(body, want) {
			t.Fatalf("answer missing %q: %q", want, body)
		}
	}
	if strings.Contains(body, "In short") || strings.Count(body, "114") > 3 {
		t.Fatalf("sales answer should not be a repeated metric dump, got %q", body)
	}
}

func TestComposerRendersSingleMetricBreakdownInOneSentence(t *testing.T) {
	body, _, _ := composer{}.compose([]domain.ToolResult{{
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Active animals", Value: "185", Scope: "goat"},
			{TenantID: "t1", Label: "Active animals", Value: "531", Scope: "sheep"},
		},
	}})
	if body != "Active animals: goat 185, sheep 531." {
		t.Fatalf("unexpected concise breakdown: %q", body)
	}
}

func TestComposerRendersOperationalCountsAsHumanSentences(t *testing.T) {
	body, _, _ := composer{}.compose([]domain.ToolResult{{
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Farm-born animals", Value: "172", Scope: "Coimbatore"},
			{TenantID: "t1", Label: "Farm-born animals", Value: "76", Scope: "Channapatna"},
		},
	}})
	if body != "Farm-born active animals: Coimbatore 172, Channapatna 76." {
		t.Fatalf("unexpected farm-born prose: %q", body)
	}

	body, _, _ = composer{}.compose([]domain.ToolResult{{
		Facts: []domain.Fact{{TenantID: "t1", Label: "Cause established deaths", Value: "0", Scope: "Coimbatore"}},
	}})
	if body != "Deaths with an established cause: Coimbatore 0." {
		t.Fatalf("unexpected mortality prose: %q", body)
	}
}

func TestComposerRendersFeedWeightBandReconciliation(t *testing.T) {
	body, _, _ := composer{}.compose([]domain.ToolResult{{
		ToolName: "feed_weight_band_summary",
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Matched animals", Value: "462", Scope: "on farm"},
			{TenantID: "t1", Label: "Matched animals including exited", Value: "506", Scope: "include exited"},
			{TenantID: "t1", Label: "Animals weighed in period", Value: "515", Scope: "general tab total"},
			{TenantID: "t1", Label: "Not shown feed rows", Value: "120", Scope: "on farm"},
			{TenantID: "t1", Label: "Exited in period", Value: "65", Scope: "55 weighed, 10 no weighing"},
			{TenantID: "t1", Label: "Feed sheet", Value: "2026-09-19"},
		},
	}})
	for _, want := range []string{"462 matched animals on farm", "506 when exited animals are included", "515 animals weighed in the General tab", "120 feed rows not shown"} {
		if !strings.Contains(body, want) {
			t.Fatalf("answer missing %q: %s", want, body)
		}
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

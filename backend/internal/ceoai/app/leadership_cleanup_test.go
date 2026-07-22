package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// TestPreferActiveCensus proves a plain headcount/species question is rewritten
// from the all-time total_animals metric to the living-herd active_animals
// metric, while an explicit all-time request is left untouched.
func TestPreferActiveCensus(t *testing.T) {
	cases := []struct {
		name string
		text string
		in   string
		want string
	}{
		{"species headcount", "How many goats vs sheep do we have?", "total_animals", "active_animals"},
		{"how many animals", "how many animals do we have", "total_animals", "active_animals"},
		{"explicit all-time keeps total", "how many animals all-time including dead", "total_animals", "total_animals"},
		{"non-census metric untouched", "vaccinations overdue", "vaccination_overdue", "vaccination_overdue"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			subs := []domain.SubQuestion{{ToolName: c.in, Route: domain.RouteCube}}
			preferActiveCensus(c.text, subs)
			if subs[0].ToolName != c.want {
				t.Fatalf("preferActiveCensus(%q, %q) => %q, want %q", c.text, c.in, subs[0].ToolName, c.want)
			}
		})
	}
}

// TestEnsureUtilizationForOverload proves overload/capacity questions always get
// an operator utilization sub-question (so over-capacity framing is possible),
// including the partial-stem phrasings a trailing word boundary would miss, and
// that non-overload questions and already-present utilization are untouched.
func TestEnsureUtilizationForOverload(t *testing.T) {
	hasUtil := func(subs []domain.SubQuestion) bool {
		for _, s := range subs {
			if s.ToolName == "operator_vaccination_utilization" {
				return true
			}
		}
		return false
	}
	overloadPhrases := []string{
		"Which operators are overloaded or at capacity?",
		"Who is over capacity right now?",
		"which operators are overloaded",
		"who is overstretched",
		"who is maxed out",
	}
	for _, p := range overloadPhrases {
		got := ensureUtilizationForOverload(p, []domain.SubQuestion{{ToolName: "operator_vaccination_load", Route: domain.RouteCube}})
		if !hasUtil(got) {
			t.Fatalf("ensureUtilizationForOverload(%q) did not add utilization sub-question", p)
		}
		last := got[len(got)-1]
		if last.Route != domain.RouteCube || last.Params["group_by"] != "operator_label" {
			t.Fatalf("appended util sub malformed for %q: %+v", p, last)
		}
	}

	// Non-overload question: no utilization added.
	none := ensureUtilizationForOverload("how many animals do we have", []domain.SubQuestion{{ToolName: "active_animals"}})
	if hasUtil(none) {
		t.Fatalf("non-overload question should not add utilization")
	}

	// Already has utilization: no duplicate appended.
	already := ensureUtilizationForOverload("who is overloaded",
		[]domain.SubQuestion{{ToolName: "operator_vaccination_utilization", Route: domain.RouteCube}})
	if len(already) != 1 {
		t.Fatalf("utilization already present must not be duplicated, got %d subs", len(already))
	}
}

// TestSynthesizeOverloadLead proves the composer names exactly the operators
// whose utilization is >= 100%, worst-first, with the grounded percent, and
// states everyone is within capacity when none are over.
func TestSynthesizeOverloadLead(t *testing.T) {
	over := []domain.ToolResult{{
		Route: domain.RouteCube,
		Facts: []domain.Fact{
			{Label: operatorUtilizationLabel, Value: "130%", Scope: "Fixture Staff 012"},
			{Label: operatorUtilizationLabel, Value: "155%", Scope: "Fixture Staff 013"},
			{Label: operatorUtilizationLabel, Value: "40%", Scope: "Fixture Staff 016"},
		},
	}}
	lead := synthesizeOverloadLead(over)
	if !strings.Contains(lead, "over capacity") {
		t.Fatalf("expected over-capacity framing, got %q", lead)
	}
	// Worst-first: 013 (155%) named before 012 (130%).
	i13 := strings.Index(lead, "Fixture Staff 013")
	i12 := strings.Index(lead, "Fixture Staff 012")
	if i13 < 0 || i12 < 0 || i13 > i12 {
		t.Fatalf("expected 013 before 012 (worst-first), got %q", lead)
	}
	if !strings.Contains(lead, "155%") || !strings.Contains(lead, "130%") {
		t.Fatalf("expected grounded percents 155%%/130%%, got %q", lead)
	}
	if strings.Contains(lead, "Fixture Staff 016") {
		t.Fatalf("under-capacity operator 016 must not be named as over capacity: %q", lead)
	}

	// All within capacity.
	under := []domain.ToolResult{{Facts: []domain.Fact{{Label: operatorUtilizationLabel, Value: "40%", Scope: "Fixture Staff 016"}}}}
	if lead := synthesizeOverloadLead(under); !strings.Contains(lead, "within capacity") {
		t.Fatalf("expected within-capacity lead, got %q", lead)
	}

	// No utilization facts => empty (normal lead handles it).
	if lead := synthesizeOverloadLead([]domain.ToolResult{{Facts: []domain.Fact{{Label: "Active animals", Value: "972"}}}}); lead != "" {
		t.Fatalf("expected empty lead when no utilization facts, got %q", lead)
	}
}

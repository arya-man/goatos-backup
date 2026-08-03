package adminuibridge

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

type stubCategories struct {
	defs []domain.CategoryDefinition
}

func (s stubCategories) Categories() []domain.CategoryDefinition { return s.defs }

// Categories collapse into drawer modules by NavigationModule, so the three feed gates
// (distribution/packing/transport) become ONE Feed module with three pages rather than three
// modules -- the same grouping the mobile drawer uses.
func TestVerifierNavModulesGroupsCategoriesByNavigationModule(t *testing.T) {
	source := New(stubCategories{defs: []domain.CategoryDefinition{
		{Vertical: "feed", Module: "feed", Category: "feed_distribution",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
			PageKey: "feed_distribution", PageLabel: "Feed Distribution", PageOrder: 1},
		{Vertical: "feed", Module: "feed", Category: "feed_packing",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
			PageKey: "feed_packing", PageLabel: "Feed Packing", PageOrder: 2},
		{Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_shed_proof",
			NavigationModule: "vaccination", NavigationModuleLabel: "Vaccination",
			PageKey: "vaccination", PageLabel: "Vaccination", PageOrder: 1},
	}})

	modules := source.VerifierNavModules()
	if len(modules) != 2 {
		t.Fatalf("modules = %#v, want 2 (Feed, Vaccination)", modules)
	}
	byKey := map[string]int{}
	for _, module := range modules {
		byKey[module.Key] = len(module.Pages)
	}
	if byKey["feed_direction"] != 2 {
		t.Fatalf("feed_direction pages = %d, want 2", byKey["feed_direction"])
	}
	if byKey["vaccination"] != 1 {
		t.Fatalf("vaccination pages = %d, want 1", byKey["vaccination"])
	}
}

// A category with no navigation metadata is a producer that never declared where its evidence
// belongs. Guessing a module for it would surface a queue under a module the maintainer never
// approved, so it is skipped.
func TestVerifierNavModulesSkipsCategoriesWithoutNavigationMetadata(t *testing.T) {
	source := New(stubCategories{defs: []domain.CategoryDefinition{
		{Vertical: "counts", Module: "counts", Category: "unregistered_evidence"},
		{Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_shed_proof",
			NavigationModule: "vaccination", NavigationModuleLabel: "Vaccination",
			PageKey: "vaccination", PageLabel: "Vaccination", PageOrder: 1},
	}})

	modules := source.VerifierNavModules()
	if len(modules) != 1 || modules[0].Key != "vaccination" {
		t.Fatalf("modules = %#v, want only vaccination", modules)
	}
}

func TestVerifierNavModulesHandlesNilSource(t *testing.T) {
	if got := New(nil).VerifierNavModules(); got != nil {
		t.Fatalf("nil source modules = %#v, want nil", got)
	}
	var source *Source
	if got := source.VerifierNavModules(); got != nil {
		t.Fatalf("nil receiver modules = %#v, want nil", got)
	}
}

package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestWebDefinedTabsLandInTheirModuleBarBeforeYou pins where a web-defined phone tab goes
// (docs/decisions/simple-task-phone-tabs.md): into its module's bar, ahead of "You"; into the
// Routines module when this person is not served its module; nowhere when neither is served;
// and visible_navigation follows the active module so it keeps equalling that module's bar.
func TestWebDefinedTabsLandInTheirModuleBarBeforeYou(t *testing.T) {
	pc := []domain.BootstrapNavigationItem{{Key: "pc_deworming", Href: "/pc/deworming"}, {Key: "you", Href: "/you"}}
	modules := []domain.BootstrapModule{
		{Key: "pc_care", Status: moduleStatusAvailable, NavItems: append([]domain.BootstrapNavigationItem(nil), pc...)},
		{Key: "pen_routines", Status: moduleStatusAvailable, NavItems: []domain.BootstrapNavigationItem{{Key: "pen_routines", Href: "/pen-routines"}}},
	}
	tabs := []ModuleTab{
		{ModuleKey: "pc_care", Key: "fumigation_wash", Label: "Wash", Href: "/pen-routines/tab/fumigation_wash", Icon: "fumigation"},
		{ModuleKey: "feed_direction", Key: "trough", Label: "Trough", Href: "/pen-routines/tab/trough", Icon: "water"},
		// The same tab twice never doubles the item.
		{ModuleKey: "pc_care", Key: "fumigation_wash", Label: "Wash", Href: "/pen-routines/tab/fumigation_wash", Icon: "fumigation"},
	}
	visible := placeModuleTabs(tabs, pc, modules)

	gotPC := modules[0].NavItems
	if len(gotPC) != 3 || gotPC[1].Href != "/pen-routines/tab/fumigation_wash" || gotPC[1].Key != "routine_tab_fumigation_wash" || gotPC[1].Icon != "fumigation" || gotPC[2].Key != "you" {
		t.Fatalf("pc bar = %+v", gotPC)
	}
	if len(visible) != 3 || visible[1].Href != gotPC[1].Href {
		t.Fatalf("visible_navigation did not follow the active module: %+v", visible)
	}
	gotRoutines := modules[1].NavItems
	if len(gotRoutines) != 2 || gotRoutines[1].Href != "/pen-routines/tab/trough" {
		t.Fatalf("a tab whose module is not served must fall back to Routines: %+v", gotRoutines)
	}

	// Neither its module nor Routines served: the tab is skipped, nothing else moves.
	only := []domain.BootstrapModule{{Key: "weighing", Status: moduleStatusAvailable, NavItems: []domain.BootstrapNavigationItem{{Key: "weighing", Href: "/weighing"}}}}
	placeModuleTabs(tabs, only[0].NavItems, only)
	if len(only[0].NavItems) != 1 {
		t.Fatalf("a tab with no module to sit in must be skipped: %+v", only[0].NavItems)
	}
}

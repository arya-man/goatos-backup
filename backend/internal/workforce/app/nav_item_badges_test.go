package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// fakeTabBadges answers the module badge AND the per-tab counts, the shape the Tasks module's
// composed source (penvisits/app.ModuleBadges over leadershiptasks/app.Service) has.
type fakeTabBadges struct {
	module map[string]int
	items  map[string]int
	asked  []string
}

func (f *fakeTabBadges) ModuleBadgeCounts(context.Context, string, string, []string) (map[string]int, error) {
	return f.module, nil
}

func (f *fakeTabBadges) NavItemBadgeCounts(_ context.Context, _, _ string, hrefs []string) (map[string]int, error) {
	f.asked = append(f.asked, hrefs...)
	return f.items, nil
}

// moduleOnlyBadges implements just the module half, the pre-2026-09-07 contract.
type moduleOnlyBadges struct{ module map[string]int }

func (m moduleOnlyBadges) ModuleBadgeCounts(context.Context, string, string, []string) (map[string]int, error) {
	return m.module, nil
}

// TestTasksBarItemsCarryTheirOwnBadges pins the 2026-09-07 decision: a module with two tabs
// carries two counts, each on ITS OWN bar item (unseen asks on Raised by me, pens owed on For
// me), while the module badge -- the drawer's number -- is their sum. Before this, the one
// module number landed on the first tab, so a park head with two pens owed saw a "2" on the
// tab that had nothing in it.
//
// Mutation-tested when written: dropping the NavItemBadgeSource assertion in applyModuleBadges
// leaves every item at 0 and turns this red.
func TestTasksBarItemsCarryTheirOwnBadges(t *testing.T) {
	src := &fakeTabBadges{
		module: map[string]int{"leadership_tasks": 3},
		items:  map[string]int{"/leadership-tasks": 1, "/pen-visits": 2},
	}
	svc := NewService(nil).WithModuleBadges(src)
	modules := []domain.BootstrapModule{{
		Key: "leadership_tasks",
		NavItems: []domain.BootstrapNavigationItem{
			{Key: "leadership_tasks", Href: "/leadership-tasks"},
			{Key: "pen_visits", Href: "/pen-visits"},
		},
	}, {
		Key:      "weighing",
		NavItems: []domain.BootstrapNavigationItem{{Key: "weighing", Href: "/weighing"}},
	}}
	svc.applyModuleBadges(context.Background(), "tenant", "user", modules)

	if modules[0].BadgeCount != 3 {
		t.Fatalf("module badge = %d, want the drawer's sum 3", modules[0].BadgeCount)
	}
	if got := modules[0].NavItems[0].BadgeCount; got != 1 {
		t.Fatalf("Raised by me badge = %d, want 1", got)
	}
	if got := modules[0].NavItems[1].BadgeCount; got != 2 {
		t.Fatalf("For me badge = %d, want 2", got)
	}
	if modules[1].BadgeCount != 0 || modules[1].NavItems[0].BadgeCount != 0 {
		t.Fatalf("a module the source never named must carry no badge: %+v", modules[1])
	}
	// Only badged modules' tabs are asked for, so an un-badged module costs no read.
	for _, h := range src.asked {
		if h == "/weighing" {
			t.Fatal("asked for a tab count on a module with no module badge")
		}
	}
}

// TestModuleOnlyBadgeSourceStillBadgesTheModule keeps the older contract working: a source that
// answers only the module half leaves every tab at 0, and the phone puts the module's number on
// its landing tab.
func TestModuleOnlyBadgeSourceStillBadgesTheModule(t *testing.T) {
	svc := NewService(nil).WithModuleBadges(moduleOnlyBadges{module: map[string]int{"leadership_tasks": 2}})
	modules := []domain.BootstrapModule{{
		Key:      "leadership_tasks",
		NavItems: []domain.BootstrapNavigationItem{{Key: "leadership_tasks", Href: "/leadership-tasks"}},
	}}
	svc.applyModuleBadges(context.Background(), "tenant", "user", modules)
	if modules[0].BadgeCount != 2 || modules[0].NavItems[0].BadgeCount != 0 {
		t.Fatalf("module-only source: got %+v", modules[0])
	}
}

package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestTasksServesNoBottomBarWhileKeepingItsDrawerRow pins the 2026-09-05 maintainer decision:
// the Tasks module (director -> CXO ask desk) shows NO bottom bar on the phone. Its bar held a
// single "Tasks" tab under a screen already titled Tasks -- a switcher with nothing to switch
// to, spending a permanent strip of a phone screen on where the reader already is.
//
// The two halves are asserted TOGETHER because the obvious implementation breaks the second:
// a module with no permitted contribution is dropped from the drawer entirely by
// modulesForScope, so deleting the nav item would delete the module. It keeps its declared
// contribution, its drawer row and its landing href; only the served bar list is empty.
func TestTasksServesNoBottomBarWhileKeepingItsDrawerRow(t *testing.T) {
	const en = localization.DefaultTag
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)}

	var tasks *domain.BootstrapModule
	var withBar int
	for _, m := range modulesFor(grants, nil, en) {
		mod := m
		if mod.Key == "leadership_tasks" {
			tasks = &mod
			continue
		}
		if mod.Status == moduleStatusAvailable && len(mod.NavItems) > 0 {
			withBar++
		}
	}
	if tasks == nil {
		t.Fatal("the Tasks module must still reach the drawer; a module with no bar is not a deleted module")
	}
	if len(tasks.NavItems) != 0 {
		t.Fatalf("Tasks served %d bar destinations, want none: %+v", len(tasks.NavItems), tasks.NavItems)
	}
	// The landing href must survive on its own, not fall back to items[0] (which no longer
	// exists): this is the route the drawer row opens.
	if tasks.Href != "/leadership-tasks" {
		t.Fatalf("Tasks landing href = %q, want /leadership-tasks", tasks.Href)
	}
	if tasks.Label != "Tasks" {
		t.Fatalf("Tasks label = %q, want Tasks", tasks.Label)
	}
	// ...and this is scoped to Tasks alone, not a global "no bars" regression. The same CEO's
	// other modules keep theirs.
	if withBar == 0 {
		t.Fatal("no other module served a bottom bar; the no-bar rule leaked past Tasks")
	}
}

// TestOnlyTasksLosesItsBottomBar is the maintainer's own question, asserted rather than promised:
// across every principal the app serves, EXACTLY ONE module is served with no bar destinations,
// and it is Tasks. Every other module a principal can open still gets its bar.
//
// This is the leak test for the rule. The bar is drawn by the client whenever the backend sends
// destinations, so "which modules lose their bar" is decided here and nowhere else -- a second
// noBottomBar: true, or a client rule keyed on the NUMBER of tabs, would turn this red.
func TestOnlyTasksLosesItsBottomBar(t *testing.T) {
	const en = localization.DefaultTag
	roles := map[string]string{
		"ceo_internal":         permissions.RoleCEOInternal,
		"pc_director":          permissions.RolePCDirector,
		"growth_director":      permissions.RoleGrowthDirector,
		"feed_director":        permissions.RoleFeedDirector,
		"health_director":      permissions.RoleHealthDirector,
		"breeding_director":    permissions.RoleBreedingDirector,
		"procurement_director": permissions.RoleProcurementDirector,
		"park_head":            permissions.RoleParkHead,
		"operator":             permissions.RoleOperator,
		"verifier":             permissions.RoleVerifier,
	}
	for name, role := range roles {
		t.Run(name, func(t *testing.T) {
			grants := []domain.GrantSummary{grantWithRole(role)}
			barless := make([]string, 0, 2)
			withBar := 0
			for _, m := range modulesFor(grants, nil, en) {
				if m.Status != moduleStatusAvailable {
					continue // a "soon" row is a roadmap advert, never an enterable module
				}
				if len(m.NavItems) == 0 {
					barless = append(barless, m.Key)
					continue
				}
				withBar++
			}
			for _, key := range barless {
				if key != "leadership_tasks" {
					t.Fatalf("%s lost its bottom bar; only leadership_tasks may (barless=%v)", key, barless)
				}
			}
			if withBar == 0 && len(barless) > 0 {
				t.Fatalf("%s got no module with a bar at all: %v", name, barless)
			}
		})
	}
}

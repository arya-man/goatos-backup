package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// MAINTAINER RULING 2026-08-03 (stated three times): "You" belongs in the NAVIGATION
// DRAWER for anyone with 2+ features, not in every feature's bottom bar.
//
// The rule, and the ONE place it is decided (applyProfileEntryPlacement, called from
// Bootstrap): a principal whose composed drawer holds >=2 available modules gets
// nav_chrome "expanded", and for THEM the profile entry is removed from
// visible_navigation and from every module's nav_items -- the drawer footer carries it
// once. A principal with exactly one module gets "minimal" chrome, has no drawer, and
// therefore KEEPS "You" in the bottom bar: that is their only route to /you.
//
// This applies UNIFORMLY. There is deliberately no verifier exception: a verifier who
// verifies vaccination AND weighing has a drawer, so his You lives there like everyone
// else's. The previous verifier carve-out in Bootstrap is exactly what shipped the
// duplicate the maintainer objected to.
//
// "You must ALWAYS be reachable" is the second half of the rule and is asserted here as
// an invariant over every principal shape, not as a per-case expectation: either the bar
// carries it (minimal) or the drawer does (expanded). A principal with neither is the
// regression that shipped once already.

// countProfileEntries reports how many nav items point at the account destination.
// It matches on BOTH the stable key and the href so that renaming one and not the other
// cannot silently satisfy the assertion.
func countProfileEntries(items []domain.BootstrapNavigationItem) int {
	n := 0
	for _, item := range items {
		if item.Key == navItemKeyYou || item.Href == "/you" {
			n++
		}
	}
	return n
}

func bootstrapFor(t *testing.T, grants []domain.GrantSummary, grantedModules []string) *domain.BootstrapResponse {
	t.Helper()
	svc := NewService(&fakeRepo{
		profile:        profile("active"),
		grants:         grants,
		grantedModules: grantedModules,
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-profile-entry")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	return got
}

func countAvailableDrawerModules(modules []domain.BootstrapModule) int {
	n := 0
	for _, m := range modules {
		if m.Status == moduleStatusAvailable {
			n++
		}
	}
	return n
}

func TestProfileEntryPlacementFollowsModuleCount(t *testing.T) {
	cases := []struct {
		name           string
		grants         []domain.GrantSummary
		grantedModules []string
		wantMultiple   bool // >=2 available modules -> drawer owns "You"
	}{
		{
			name:         "ceo holds several modules",
			grants:       []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)},
			wantMultiple: true,
		},
		{
			name:           "verifier verifies vaccination and weighing",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			grantedModules: []string{"pc.vaccination", "weighing"},
			wantMultiple:   true,
		},
		{
			name:         "verifier with no named duty is scoped to every built feature",
			grants:       []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			wantMultiple: true,
		},
		{
			name:           "verifier verifies vaccination only",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			grantedModules: []string{"vaccination"},
			wantMultiple:   false,
		},
		{
			name:           "operator holds one module",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			grantedModules: []string{"vaccination"},
			wantMultiple:   false,
		},
		{
			name:         "park head is preventive-care only",
			grants:       []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)},
			wantMultiple: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bootstrapFor(t, tc.grants, tc.grantedModules)

			available := countAvailableDrawerModules(got.Modules)
			if tc.wantMultiple != (available >= 2) {
				t.Fatalf("available drawer modules=%d (wantMultiple=%v); modules=%#v", available, tc.wantMultiple, got.Modules)
			}
			wantChrome := domain.NavChromeMinimal
			if tc.wantMultiple {
				wantChrome = domain.NavChromeExpanded
			}
			if got.NavChrome != wantChrome {
				t.Fatalf("NavChrome=%q want %q (available modules=%d)", got.NavChrome, wantChrome, available)
			}

			barCount := countProfileEntries(got.VisibleNavigation)
			if tc.wantMultiple {
				// (1) A principal with 2+ features must not carry You in the served bar.
				if barCount != 0 {
					t.Fatalf("2+ module principal carries %d You item(s) in visible_navigation; it belongs in the drawer. nav=%#v", barCount, got.VisibleNavigation)
				}
				// (3) ...and switching modules must never surface a second one: every
				// module's own bar is You-free too, so no drawer selection can produce one.
				for _, m := range got.Modules {
					if n := countProfileEntries(m.NavItems); n != 0 {
						t.Fatalf("module %q bar carries %d You item(s); switching to it would show You twice (drawer + bar). items=%#v", m.Key, n, m.NavItems)
					}
				}
			} else {
				// (2) A single-module principal has no drawer, so the bar is the ONLY
				// route to /you and must carry it exactly once.
				if barCount != 1 {
					t.Fatalf("single-module principal has %d You item(s) in visible_navigation, want exactly 1 (no drawer to reach it from). nav=%#v", barCount, got.VisibleNavigation)
				}
				for _, m := range got.Modules {
					if m.Status != moduleStatusAvailable {
						continue
					}
					if n := countProfileEntries(m.NavItems); n != 1 {
						t.Fatalf("single-module principal's module %q bar has %d You item(s), want exactly 1. items=%#v", m.Key, n, m.NavItems)
					}
				}
			}

			// THE REACHABILITY INVARIANT, checked for every shape: You is never
			// unreachable. Either the bar carries it, or chrome is expanded and the
			// drawer does (GoatOsShell.kt DrawerFooter renders the account row for
			// every expanded-chrome principal).
			reachable := barCount > 0 || got.NavChrome == domain.NavChromeExpanded
			if !reachable {
				t.Fatalf("no route to /you at all: chrome=%q nav=%#v", got.NavChrome, got.VisibleNavigation)
			}
		})
	}
}

// TestProfileEntryPlacementIsLoadBearing is the mutation guard. It calls the placement
// rule DIRECTLY on a fixed payload so a regression cannot hide behind whichever principal
// shapes happen to be enumerated above: break applyProfileEntryPlacement (drop the strip,
// re-add a per-role exception, key it off something other than the composed module count)
// and this fails on its own.
func TestProfileEntryPlacementIsLoadBearing(t *testing.T) {
	bar := []domain.BootstrapNavigationItem{
		{Key: "verify", Label: "Verify", Href: "/verify?module=vaccination"},
		{Key: "alerts", Label: "Alerts", Href: "/verify/alerts?category=vaccination_proof"},
		{Key: navItemKeyYou, Label: "You", Href: "/you"},
	}
	modules := []domain.BootstrapModule{
		{Key: "verify_vaccination", Status: moduleStatusAvailable, NavItems: append([]domain.BootstrapNavigationItem(nil), bar...)},
		{Key: "verify_weighing", Status: moduleStatusAvailable, NavItems: []domain.BootstrapNavigationItem{
			{Key: "verify", Label: "Verify", Href: "/verify?module=weighing"},
			{Key: navItemKeyYou, Label: "You", Href: "/you"},
		}},
	}

	t.Run("expanded chrome moves You out of every bar", func(t *testing.T) {
		gotBar, gotModules := applyProfileEntryPlacement(domain.NavChromeExpanded, append([]domain.BootstrapNavigationItem(nil), bar...), cloneModules(modules))
		if n := countProfileEntries(gotBar); n != 0 {
			t.Fatalf("expanded chrome left %d You item(s) in the served bar: %#v", n, gotBar)
		}
		for _, m := range gotModules {
			if n := countProfileEntries(m.NavItems); n != 0 {
				t.Fatalf("expanded chrome left %d You item(s) in module %q: %#v", n, m.Key, m.NavItems)
			}
		}
		// The strip must remove ONLY the profile entry.
		if len(gotBar) != 2 || gotBar[0].Key != "verify" || gotBar[1].Key != "alerts" {
			t.Fatalf("expanded chrome damaged the rest of the bar: %#v", gotBar)
		}
	})

	t.Run("minimal chrome keeps You reachable", func(t *testing.T) {
		gotBar, gotModules := applyProfileEntryPlacement(domain.NavChromeMinimal, append([]domain.BootstrapNavigationItem(nil), bar...), cloneModules(modules))
		if n := countProfileEntries(gotBar); n != 1 {
			t.Fatalf("minimal chrome must keep exactly one You in the bar (no drawer exists); got %d: %#v", n, gotBar)
		}
		if n := countProfileEntries(gotModules[0].NavItems); n != 1 {
			t.Fatalf("minimal chrome must keep You in the module bar; got %d: %#v", n, gotModules[0].NavItems)
		}
	})
}

func cloneModules(in []domain.BootstrapModule) []domain.BootstrapModule {
	out := make([]domain.BootstrapModule, 0, len(in))
	for _, m := range in {
		m.NavItems = append([]domain.BootstrapNavigationItem(nil), m.NavItems...)
		out = append(out, m)
	}
	return out
}

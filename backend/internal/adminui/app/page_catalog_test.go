package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// The page catalog in permissions/capability_pages.go is what the People screen ticks.
// These two tests keep it honest against the REAL contract compiled here: a nav leaf
// missing from the catalog would be unwithholdable (nobody could take it away), and a page
// contract whose route no module owns could never be narrowed -- both are silent, and both
// are the kind of drift that made the hand-coded lenses necessary in the first place.

func TestEveryNavLeafIsATickablePage(t *testing.T) {
	catalog := make(map[string]permissions.ModulePage, 32)
	for _, p := range permissions.ModulePages() {
		catalog[p.Key] = p
	}
	seen := make(map[string]struct{}, len(catalog))
	visit := func(id, label, href string) {
		page, ok := catalog[id]
		if !ok {
			t.Errorf("nav item %q (%s) has no row in permissions.ModulePages(); nobody could withhold it", id, href)
			return
		}
		if page.Href != href {
			t.Errorf("nav item %q href is %q but the catalog says %q", id, href, page.Href)
		}
		if page.Label != label {
			t.Errorf("nav item %q label is %q but the catalog says %q; the tick must read as the sidebar reads", id, label, page.Label)
		}
		seen[id] = struct{}{}
	}
	nav := navigation()
	for _, item := range nav.Primary {
		visit(item.ID, item.Label, item.Href)
	}
	for _, group := range nav.Groups {
		for _, leaf := range group.Leaves {
			visit(leaf.ID, leaf.Label, leaf.Href)
		}
	}
	for key := range catalog {
		if _, ok := seen[key]; !ok {
			t.Errorf("catalog page %q is not a nav leaf any more; a stale tick grants a screen that no longer exists", key)
		}
	}
}

func TestEveryPageContractRouteIsOwnedByAModule(t *testing.T) {
	for _, page := range pages() {
		module, owned := permissions.ModuleOwningRoute(page.Href)
		if !owned {
			t.Errorf("page contract %q (%s) is owned by no module; it can never be narrowed or withheld", page.RouteID, page.Href)
			continue
		}
		if _, known := permissions.LookupModuleCapability(module); !known {
			t.Errorf("page contract %q resolves to unknown module %q", page.RouteID, module)
		}
	}
}

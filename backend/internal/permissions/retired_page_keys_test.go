package permissions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A page rename must (1) alias the old key here so stored ticks keep resolving, and (2) ship
// a remap migration so the stored data says the new name. Both halves are pinned: an alias
// pointing at a dead or itself-retired page is a typo that would lose the screen again, and
// an alias with no migration leaves the database lying about what people can open.
func TestRetiredPageKeysResolveToLivePages(t *testing.T) {
	migrations, err := filepath.Glob(filepath.Join("..", "..", "migrations", "postgres", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for old, replacement := range RetiredPageKeys {
		if _, live := modulePageIndex[old]; live {
			t.Fatalf("%q is listed as retired but is still a live page", old)
		}
		if _, retired := RetiredPageKeys[replacement]; retired {
			t.Fatalf("%q -> %q points at another retired key; point at the live page", old, replacement)
		}
		if _, live := modulePageIndex[replacement]; !live {
			t.Fatalf("%q -> %q points at a page that does not exist", old, replacement)
		}
		if CanonicalPageKey(old) != replacement {
			t.Fatalf("CanonicalPageKey(%q) = %q, want %q", old, CanonicalPageKey(old), replacement)
		}
		found := false
		for _, m := range migrations {
			body, err := os.ReadFile(m)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "'"+old+"'") && strings.Contains(string(body), "'"+replacement+"'") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("retired page key %q has no remap migration naming it and %q", old, replacement)
		}
	}
}

// A stored tick list carrying the retired key still opens the replacement screen.
func TestStaleWeightsTickStillOpensADGAnalytics(t *testing.T) {
	access := PageAccessForAssignments([]ModuleAssignment{{
		Module: "weighing", Surface: SurfaceWeb, Capabilities: []string{LevelView, LevelConfigure},
		Pages: []string{"weighing-weights", "weighing-sops"},
	}, {Module: "config", Surface: SurfaceWeb, Capabilities: []string{LevelView}}})
	if _, ok := access.Pages["weighing-analytics"]; !ok {
		t.Fatalf("a stored weighing-weights tick must resolve to weighing-analytics; got %v", access.Pages)
	}
}

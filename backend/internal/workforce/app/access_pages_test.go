package app

import (
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Page ticks are the mechanism that retired the hand-coded admin-web lenses
// (maintainer decision 2026-08-27), so the write path has to refuse the two shapes
// that would silently widen access: a page from another module, and a granted
// module with nothing ticked (which resolves to EVERY page at read time).

func TestPageTicksAreValidatedAgainstTheModulesOwnScreens(t *testing.T) {
	feed, _ := permissions.LookupModuleCapability("feed_direction")

	got, err := validatedPages(feed, []string{"feed-sops", "feed-analytics"})
	if err != nil {
		t.Fatalf("valid pages refused: %v", err)
	}
	// Sidebar order, not request order.
	if len(got) != 2 || got[0] != "feed-analytics" || got[1] != "feed-sops" {
		t.Fatalf("pages came back %v; want sidebar order [feed-analytics feed-sops]", got)
	}

	if _, err := validatedPages(feed, []string{"people"}); !errors.Is(err, ErrInvalidAccessRequest) {
		t.Fatal("a page from another module was accepted onto Feed")
	}
	if _, err := validatedPages(feed, []string{"feed-analytics", "not-a-page"}); !errors.Is(err, ErrInvalidAccessRequest) {
		t.Fatal("an unknown page key was accepted")
	}
	// Granting the module with no screen ticked resolves to EVERY screen at read time,
	// which is the opposite of what the admin did. Refuse it rather than widen.
	if _, err := validatedPages(feed, nil); !errors.Is(err, ErrInvalidAccessRequest) {
		t.Fatal("a granted module with no page ticked was accepted; it would resolve to every page")
	}

	// A module with no admin-web screen of its own takes no ticks and must not refuse.
	toxin, _ := permissions.LookupModuleCapability("toxin")
	if pages, err := validatedPages(toxin, nil); err != nil || len(pages) != 0 {
		t.Fatalf("a page-less module refused an empty tick list: %v %v", pages, err)
	}
}

func TestSavedPageTicksRideOnlyTheWebRow(t *testing.T) {
	rows := []domain.AccessModuleWrite{{
		ModuleKey: "feed_direction",
		Web:       []string{permissions.LevelView},
		Mobile:    []string{permissions.LevelView},
		Pages:     []string{"feed-analytics"},
	}}
	out, err := validatedAssignments(rows)
	if err != nil {
		t.Fatalf("save refused: %v", err)
	}
	for _, a := range out {
		switch a.Surface {
		case permissions.SurfaceWeb:
			if len(a.Pages) != 1 || a.Pages[0] != "feed-analytics" {
				t.Errorf("web row carries pages %v; want [feed-analytics]", a.Pages)
			}
		case permissions.SurfaceMobile:
			if len(a.Pages) != 0 {
				t.Errorf("mobile row carries pages %v; the phone builds its own navigation", a.Pages)
			}
		}
	}
}

// TestTheEditorShowsExplicitTicksForAModuleStoredWithNone pins the read half of
// "empty means every page": the SCREEN must never open with a held module showing
// no ticks, or an admin saving it back would refuse (see the write test above) or
// read as a removal nobody made.
func TestTheEditorShowsExplicitTicksForAModuleStoredWithNone(t *testing.T) {
	rows := moduleRows([]permissions.ModuleAssignment{
		{Module: "feed_direction", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelView}},
	})
	for _, row := range rows {
		if row.ModuleKey != "feed_direction" {
			continue
		}
		if len(row.Pages) != 3 {
			t.Fatalf("Feed offers %d tickable screens; want 3", len(row.Pages))
		}
		if len(row.GrantedPagesWeb) != 3 {
			t.Fatalf("a module stored with no page list rendered %v ticked; want all three", row.GrantedPagesWeb)
		}
		return
	}
	t.Fatal("feed_direction is missing from the editor rows")
}

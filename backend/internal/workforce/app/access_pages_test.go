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

	got, err := validatedPages(feed, []string{permissions.LevelConfigure}, []string{"feed-sops", "feed-analytics"}, []string{permissions.SOPRead})
	if err != nil {
		t.Fatalf("valid pages refused: %v", err)
	}
	// Sidebar order, not request order.
	if len(got) != 2 || got[0] != "feed-analytics" || got[1] != "feed-sops" {
		t.Fatalf("pages came back %v; want sidebar order [feed-analytics feed-sops]", got)
	}

	if _, err := validatedPages(feed, []string{permissions.LevelConfigure}, []string{"people"}, nil); !errors.Is(err, ErrInvalidAccessRequest) {
		t.Fatal("a page from another module was accepted onto Feed")
	}
	if _, err := validatedPages(feed, []string{permissions.LevelConfigure}, []string{"feed-analytics", "not-a-page"}, nil); !errors.Is(err, ErrInvalidAccessRequest) {
		t.Fatal("an unknown page key was accepted")
	}
	// Granting the module with no screen ticked resolves to EVERY screen at read time,
	// which is the opposite of what the admin did. Refuse it rather than widen.
	if _, err := validatedPages(feed, []string{permissions.LevelConfigure}, nil, nil); !errors.Is(err, ErrInvalidAccessRequest) {
		t.Fatal("a granted module with no page ticked was accepted; it would resolve to every page")
	}

	// A screen's authority can come from ANOTHER module: Feed SOP is grouped under Feed but
	// needs sop.read, which lives in Protocols & SOPs. Without that held elsewhere it is not
	// tickable -- and the CEO, who holds it, keeps the screen.
	if _, err := validatedPages(feed, []string{permissions.LevelConfigure}, []string{"feed-sops"}, nil); !errors.Is(err, ErrInvalidAccessRequest) {
		t.Fatal("Feed SOP was tickable without sop.read held anywhere")
	}

	// A capability too low to open a screen may not tick it, and the refusal says which.
	// Health Config is the worked case: it declares health.config.read, which `view` on the
	// Health module does not produce.
	if _, err := validatedPages(health0(t), []string{permissions.LevelView}, []string{"health-config"}, nil); !errors.Is(err, ErrInvalidAccessRequest) {
		t.Fatal("Health Config was tickable at `view`, which does not open it")
	}
	// ...and a module whose capabilities open NONE of its screens stores an empty list
	// rather than refusing. This is the round-trip the persona sweep caught: the editor's
	// own payload for such a person was rejected by its own save.
	if pages, err := validatedPages(health0(t), []string{permissions.LevelView}, nil, nil); err != nil || len(pages) != 0 {
		t.Fatalf("a module whose capabilities open no screen refused an empty list: %v %v", pages, err)
	}

	// A module with no admin-web screen of its own takes no ticks and must not refuse.
	toxin, _ := permissions.LookupModuleCapability("toxin")
	if pages, err := validatedPages(toxin, []string{permissions.LevelView}, nil, nil); err != nil || len(pages) != 0 {
		t.Fatalf("a page-less module refused an empty tick list: %v %v", pages, err)
	}
}

func TestSavedPageTicksRideOnlyTheWebRow(t *testing.T) {
	rows := []domain.AccessModuleWrite{{
		ModuleKey: "feed_direction",
		Web:       []string{permissions.LevelConfigure},
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
	// Feed at `view` opens Feed Analytics alone: Feed Config needs feed_config.read (which
	// only `configure` produces) and Feed SOP needs sop.read from Protocols & SOPs. The
	// editor must offer exactly the one, or it would show ticks the save then refuses.
	find := func(rows []domain.AccessModuleRow, key string) domain.AccessModuleRow {
		for _, r := range rows {
			if r.ModuleKey == key {
				return r
			}
		}
		t.Fatalf("%s is missing from the editor rows", key)
		return domain.AccessModuleRow{}
	}
	row := find(moduleRows([]permissions.ModuleAssignment{
		{Module: "feed_direction", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelView}},
	}), "feed_direction")
	if len(row.Pages) != 1 || row.Pages[0].PageKey != "feed-analytics" {
		t.Fatalf("Feed at view offers %v; want Feed Analytics alone", row.Pages)
	}
	if len(row.GrantedPagesWeb) != 1 {
		t.Fatalf("a module stored with no page list rendered %v ticked; want the one it can open", row.GrantedPagesWeb)
	}

	// Add Protocols & SOPs and Feed Config authority, and all three appear -- the proof that
	// a screen's authority can come from another module.
	row = find(moduleRows([]permissions.ModuleAssignment{
		{Module: "feed_direction", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelConfigure}},
		{Module: "config", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelView}},
	}), "feed_direction")
	if len(row.Pages) != 3 {
		t.Fatalf("Feed at configure with Protocols & SOPs offers %v; want all three screens", row.Pages)
	}
}

func health0(t *testing.T) permissions.ModuleCapability {
	t.Helper()
	m, ok := permissions.LookupModuleCapability("aas_health")
	if !ok {
		t.Fatal("the Health module is missing from the catalog")
	}
	return m
}

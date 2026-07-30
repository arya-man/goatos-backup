package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

func moduleKeySet(modules []domain.BootstrapModule) map[string]string {
	out := make(map[string]string, len(modules))
	for _, m := range modules {
		out[m.Key] = m.Status
	}
	return out
}

// TestLeadershipDrawerCompositionPerRole locks the role -> drawer matrix
// (maintainer decision 2026-07-25, docs/decisions/role-module-nav-composition.md):
//   - CEO/CXO: Vaccination + Weighing + Counts + Feed(soon) + Breeding(soon), never the
//     removed synthetic leadership module or Verification. Expanded drawer.
//   - Park Head: Vaccination only, NO Counts/Feed/Weighing/Breeding.
//   - PC Director: Vaccination only, no Counts/Feed/Weighing/Breeding.
//   - Growth Director: Weighing only, no Vaccination/Counts/Feed/Breeding.
//   - Verifier: Verification only. Operator: department modules, never leadership.
func TestLeadershipDrawerCompositionPerRole(t *testing.T) {
	const en = localization.DefaultTag

	t.Run("ceo sees vaccination+weighing+counts+soon, no synthetic leadership module, no verification", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)}
		modules := modulesFor(grants, nil, en)
		keys := moduleKeySet(modules)

		if keys["vaccination"] != moduleStatusAvailable {
			t.Fatalf("CEO must have the Vaccination module; got %v", keys)
		}
		if keys["weighing"] != moduleStatusAvailable {
			t.Fatalf("CEO must have the Weighing module; got %v", keys)
		}
		if keys["counts"] != moduleStatusAvailable {
			t.Fatalf("CEO must see Counts as available; got %v", keys)
		}
		if keys["feed_direction"] != moduleStatusSoon {
			t.Fatalf("CEO must see Feed as soon; got %v", keys)
		}
		if keys["breeding"] != moduleStatusSoon {
			t.Fatalf("CEO must see Breeding as soon; got %v", keys)
		}
		if _, ok := keys["leadership"]; ok {
			t.Fatalf("CEO must NOT see the removed leadership overview module; got %v", keys)
		}
		if _, ok := keys["verification"]; ok {
			t.Fatalf("CEO must NOT see Verification (verifier-only); got %v", keys)
		}
		// The vaccination drawer row lands on the shared vaccination area.
		for _, m := range modules {
			if m.Key == "vaccination" {
				if m.Label != "Vaccination" {
					t.Fatalf("vaccination row label = %q, want Vaccination", m.Label)
				}
				if m.Href != "/vaccination" {
					t.Fatalf("vaccination row lands on %q, want /vaccination", m.Href)
				}
				wantItems := []domain.BootstrapNavigationItem{
					{Key: "overview", Label: "Overview", Href: "/vaccination"},
					{Key: "calendar", Label: "Calendar", Href: "/calendar"},
					{Key: "videos", Label: "Videos", Href: "/verify/action"},
					{Key: "alerts", Label: "Alerts", Href: "/alerts"},
					{Key: "you", Label: "You", Href: "/you"},
				}
				if len(m.NavItems) != len(wantItems) {
					t.Fatalf("CEO vaccination bar=%+v want %+v", m.NavItems, wantItems)
				}
				for _, it := range m.NavItems {
					if it.Key == "vaccination" || it.Key == "weighing" || it.Href == "/leadership" {
						t.Fatalf("CEO vaccination bar must not contain operator Drives, Weighing, or /leadership; got %+v", m.NavItems)
					}
				}
				for i := range wantItems {
					if m.NavItems[i] != wantItems[i] {
						t.Fatalf("CEO vaccination bar[%d]=%+v want %+v", i, m.NavItems[i], wantItems[i])
					}
				}
			}
		}
		if got := navChromeFor(grants, modules); got != domain.NavChromeExpanded {
			t.Fatalf("CEO chrome = %q, want expanded", got)
		}
	})

	t.Run("pc-leader pc_director sees vaccination only", func(t *testing.T) {
		role := permissions.RolePCDirector
		grants := []domain.GrantSummary{grantWithRole(role)}
		// Even with extra department modules, the PC Director's drawer is
		// preventive-care only.
		modules := modulesFor(grants, []string{"vaccination", "weighing", "counts"}, en)
		keys := moduleKeySet(modules)

		if _, ok := keys["vaccination"]; !ok {
			t.Fatalf("%s must have the Vaccination module; got %v", role, keys)
		}
		for _, banned := range []string{"weighing", "counts", "feed_direction", "breeding", "leadership", "verification"} {
			if _, ok := keys[banned]; ok {
				t.Fatalf("%s must NOT see %q; got %v", role, banned, keys)
			}
		}
		if got := navChromeFor(grants, modules); got != domain.NavChromeMinimal {
			t.Fatalf("%s chrome = %q, want minimal", role, got)
		}
	})

	t.Run("growth director sees weighing only", func(t *testing.T) {
		role := permissions.RoleGrowthDirector
		grants := []domain.GrantSummary{grantWithRole(role)}
		modules := modulesFor(grants, []string{"vaccination", "weighing", "counts"}, en)
		keys := moduleKeySet(modules)

		if _, ok := keys["weighing"]; !ok {
			t.Fatalf("%s must have the Weighing module; got %v", role, keys)
		}
		for _, banned := range []string{"vaccination", "counts", "feed_direction", "breeding", "leadership", "verification"} {
			if _, ok := keys[banned]; ok {
				t.Fatalf("%s must NOT see %q; got %v", role, banned, keys)
			}
		}
		if got := navChromeFor(grants, modules); got != domain.NavChromeMinimal {
			t.Fatalf("%s chrome = %q, want minimal", role, got)
		}
	})

	// Park Head is preventive-care scoped. Feed is still roadmap/soon and not part
	// of the park-head drawer until it becomes a built park-ops surface.
	t.Run("park_head sees vaccination only", func(t *testing.T) {
		role := permissions.RoleParkHead
		grants := []domain.GrantSummary{grantWithRole(role)}
		modules := modulesFor(grants, []string{"vaccination", "weighing", "counts", "feed_direction"}, en)
		keys := moduleKeySet(modules)

		if keys["vaccination"] != moduleStatusAvailable {
			t.Fatalf("park_head must have the Vaccination module; got %v", keys)
		}
		for _, banned := range []string{"weighing", "counts", "feed_direction", "breeding", "leadership", "verification"} {
			if _, ok := keys[banned]; ok {
				t.Fatalf("park_head must NOT see %q; got %v", banned, keys)
			}
		}
		if got := navChromeFor(grants, modules); got != domain.NavChromeMinimal {
			t.Fatalf("park_head chrome = %q, want minimal", got)
		}
	})

	t.Run("verifier sees verification only", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
		modules := modulesFor(grants, []string{"vaccination"}, en)
		keys := moduleKeySet(modules)
		if _, ok := keys["verification"]; !ok {
			t.Fatalf("verifier must see Verification; got %v", keys)
		}
		if _, ok := keys["leadership"]; ok {
			t.Fatalf("verifier must NOT see leadership; got %v", keys)
		}
		verify := modules[0]
		wantItems := []domain.BootstrapNavigationItem{
			{Key: "verify", Label: "Verify", Href: "/verify"},
			{Key: "you", Label: "You", Href: "/you"},
		}
		if len(verify.NavItems) != len(wantItems) {
			t.Fatalf("verifier nav items = %+v want %+v", verify.NavItems, wantItems)
		}
		for i := range wantItems {
			if verify.NavItems[i] != wantItems[i] {
				t.Fatalf("verifier nav item[%d]=%+v want %+v", i, verify.NavItems[i], wantItems[i])
			}
		}
	})

	t.Run("pc director with verifier permission keeps director vaccination module only", func(t *testing.T) {
		grants := []domain.GrantSummary{
			grantWithRole(permissions.RolePCDirector),
			grantWithRole(permissions.RoleVerifier),
		}
		modules := modulesFor(grants, []string{"vaccination", "weighing"}, en)
		keys := moduleKeySet(modules)
		if _, ok := keys["vaccination"]; !ok {
			t.Fatalf("hybrid director must keep Vaccination; got %v", keys)
		}
		if _, ok := keys["weighing"]; !ok {
			// no-op
		} else {
			t.Fatalf("hybrid pc director must NOT get Weighing; got %v", keys)
		}
		if _, ok := keys["verification"]; ok {
			t.Fatalf("hybrid director must NOT collapse into verifier-only module; got %v", keys)
		}
	})

	t.Run("operator keeps department modules, never leadership", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleOperator)}
		keys := moduleKeySet(modulesFor(grants, []string{"vaccination", "weighing", "counts"}, en))
		if _, ok := keys["vaccination"]; !ok {
			t.Fatalf("operator must keep the vaccination module; got %v", keys)
		}
		if _, ok := keys["weighing"]; !ok {
			t.Fatalf("operator must keep the weighing module; got %v", keys)
		}
		if _, ok := keys["leadership"]; ok {
			t.Fatalf("operator must NOT get the leadership module; got %v", keys)
		}
		if _, ok := keys["verification"]; ok {
			t.Fatalf("operator must NOT get verification; got %v", keys)
		}
	})
}

func TestBootstrapCopyCatalogCoversSupportedLocales(t *testing.T) {
	english := bootstrapLabels[localization.DefaultTag]
	if len(english) == 0 {
		t.Fatal("English bootstrap copy catalog is empty")
	}
	for _, tag := range localization.SupportedTags() {
		labels := bootstrapLabels[tag]
		if len(labels) == 0 {
			t.Fatalf("missing bootstrap copy catalog for locale %q", tag)
		}
		for key := range english {
			if labels[key] == "" {
				t.Fatalf("missing bootstrap copy key %q for locale %q", key, tag)
			}
		}
	}
}

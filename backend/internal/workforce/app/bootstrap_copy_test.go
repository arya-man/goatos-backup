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
					{Key: "alerts", Label: "Vaccination alerts", Href: "/alerts"},
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

// TestAlertsNavLabelNamesTheVaccinationScope locks the honest naming of the /alerts
// destination. That screen is fed by ONE upstream — the vaccination control-tower
// summary — so it can only ever show vaccination alerts. It is nevertheless
// contributed to the Weighing bottom bar as well, where a bare "Alerts" promises a
// weighing operator that a weighing alert is reachable there. It is not: a weighing
// push exists only as a system-tray entry, and swiping it away loses it.
//
// Until a module-scoped notification history exists, the label must say which
// module's alerts these are, in every language, so the tab does not over-promise.
func TestAlertsNavLabelNamesTheVaccinationScope(t *testing.T) {
	wantByLocale := map[string]string{
		"en": "Vaccination alerts",
		"hi": "टीकाकरण अलर्ट",
		"kn": "ಲಸಿಕೆ ಎಚ್ಚರಿಕೆಗಳು",
		"te": "టీకా అలర్ట్లు",
	}
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)}
	for locale, want := range wantByLocale {
		modules := modulesFor(grants, nil, locale)
		seen := 0
		for _, m := range modules {
			for _, item := range m.NavItems {
				if item.Href != "/alerts" {
					continue
				}
				seen++
				if item.Label != want {
					t.Fatalf("locale %s module %s: /alerts label = %q, want %q", locale, m.Key, item.Label, want)
				}
			}
		}
		if seen == 0 {
			t.Fatalf("locale %s: no /alerts nav item found to check", locale)
		}
	}
}

// TestNewDirectorRolesGetTheirOwnModuleOffer pins the /app/bootstrap hole these two roles were
// shipped with: both are leadership principals, so visibleNavigationFor takes the leadership
// branch, and with no entry in leadershipModuleKeys that branch resolved zero keys and returned
// an EMPTY nav plus an EMPTY drawer -- a role that can log in and reach nothing.
//
// It also pins the OFF-feature boundary the offer must not cross: health_director is offered the
// Counts module but holds NO counts.read, so no Counts nav item may appear for him. Granting
// counts.read is what would switch the feature on (AGENTS.md).
func TestNewDirectorRolesGetTheirOwnModuleOffer(t *testing.T) {
	const en = localization.DefaultTag

	t.Run("feed director is offered feed, never another director's module", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleFeedDirector)}
		keys := leadershipModuleKeys(grants)
		if len(keys) == 0 {
			t.Fatal("feed_director resolves ZERO leadership module keys; bootstrap returns an empty nav")
		}
		if keys[0] != "feed_direction" {
			t.Fatalf("feed_director module keys = %v, want feed_direction first", keys)
		}
		for _, key := range keys {
			switch key {
			case "vaccination", "weighing", "counts":
				t.Fatalf("feed_director offered %q, which belongs to another director", key)
			}
		}
		// Feed is still a declared roadmap module, so the drawer row is the disabled "Soon" row
		// (TestFeedModuleRoleMatrix pins that Feed contributes no bottom-bar items yet).
		modules := modulesFor(grants, nil, en)
		if status := moduleKeySet(modules)["feed_direction"]; status != moduleStatusSoon {
			t.Fatalf("feed_director feed row status = %q, want %q", status, moduleStatusSoon)
		}
	})

	t.Run("health director is offered counts but counts stays OFF without counts.read", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleHealthDirector)}
		keys := leadershipModuleKeys(grants)
		if len(keys) == 0 {
			t.Fatal("health_director resolves ZERO leadership module keys; bootstrap returns an empty nav")
		}
		if keys[0] != "counts" {
			t.Fatalf("health_director module keys = %v, want counts first", keys)
		}
		for _, key := range keys {
			switch key {
			case "vaccination", "weighing", "feed_direction":
				t.Fatalf("health_director offered %q, which belongs to another director", key)
			}
		}
		if permissions.RoleHasPermission(permissions.RoleHealthDirector, permissions.CountsRead) {
			t.Fatal("health_director holds counts.read; that switches the OFF Counts feature on")
		}
		// Offer without access: every Counts nav item is permission-gated, so none renders and
		// the Counts drawer row does not appear at all.
		for _, item := range visibleNavigationFor(grants, nil, en) {
			if item.Key == "counts" || item.Href == "/counts" {
				t.Fatalf("Counts nav item %q rendered for health_director without counts.read", item.Key)
			}
		}
		if _, ok := moduleKeySet(modulesFor(grants, nil, en))["counts"]; ok {
			t.Fatal("Counts drawer row rendered for health_director without counts.read")
		}
	})
}

// TestMultiModuleVerifierDrawer locks the verifier nav composition for ≥2 verify duties.
// A verifier with VerificationReview permission and verify duty on multiple features
// gets a drawer with one entry per feature, following the CEO/leadership pattern.
func TestMultiModuleVerifierDrawer(t *testing.T) {
	const en = localization.DefaultTag

	t.Run("multi-module verifier (vaccination+weighing) gets expanded drawer with feature-scoped modules", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
		grantedModules := []string{"pc.vaccination", "weighing"}
		modules := modulesFor(grants, grantedModules, en)
		keys := moduleKeySet(modules)

		// Should have two modules: one for each feature the verifier has a verify duty on
		if len(modules) != 2 {
			t.Fatalf("multi-module verifier should see 2 modules; got %d: %v", len(modules), keys)
		}

		// Check vaccination module
		vaccModule := modules[0]
		if vaccModule.Key != "verify_vaccination" {
			t.Fatalf("first module key = %q, want verify_vaccination", vaccModule.Key)
		}
		if vaccModule.Label != "Vaccination" {
			t.Fatalf("vaccination module label = %q, want Vaccination", vaccModule.Label)
		}
		if vaccModule.Status != moduleStatusAvailable {
			t.Fatalf("vaccination module status = %q, want available", vaccModule.Status)
		}
		// The module href carries the feature so the drawer's tap scopes the verify queue to
		// THAT feature; plain "/verify" made every drawer entry land on vaccination.
		if vaccModule.Href != "/verify?module=vaccination" {
			t.Fatalf("vaccination module landing href = %q, want /verify", vaccModule.Href)
		}

		// Check vaccination nav items: verify + alerts + you
		if len(vaccModule.NavItems) != 3 {
			t.Fatalf("vaccination nav items count = %d, want 3; got %v", len(vaccModule.NavItems), vaccModule.NavItems)
		}
		if vaccModule.NavItems[0].Key != "verify" || vaccModule.NavItems[0].Label != "Verify" {
			t.Fatalf("vaccination bar item 0 = %+v, want {verify, Verify}", vaccModule.NavItems[0])
		}
		if vaccModule.NavItems[1].Key != "alerts" || vaccModule.NavItems[1].Label != "Vaccination alerts" {
			t.Fatalf("vaccination bar item 1 = %+v, want {alerts, Vaccination alerts}", vaccModule.NavItems[1])
		}
		if vaccModule.NavItems[2].Key != "you" || vaccModule.NavItems[2].Label != "You" {
			t.Fatalf("vaccination bar item 2 = %+v, want {you, You}", vaccModule.NavItems[2])
		}

		// Check weighing module
		weighModule := modules[1]
		if weighModule.Key != "verify_weighing" {
			t.Fatalf("second module key = %q, want verify_weighing", weighModule.Key)
		}
		if weighModule.Label != "Weighing" {
			t.Fatalf("weighing module label = %q, want Weighing", weighModule.Label)
		}
		if len(weighModule.NavItems) != 3 {
			t.Fatalf("weighing nav items count = %d, want 3; got %v", len(weighModule.NavItems), weighModule.NavItems)
		}
	})

	t.Run("single-module verifier gets minimal chrome with generic verification module", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
		grantedModules := []string{"pc.vaccination"}
		modules := modulesFor(grants, grantedModules, en)
		keys := moduleKeySet(modules)

		// Single module verifier should still use the generic "verification" module
		// (handled by candidateModuleKeys returning ["verification"] when len(grantedModules) <= 1)
		if keys["verification"] != moduleStatusAvailable {
			t.Fatalf("single-module verifier should see generic verification module; got %v", keys)
		}
		if len(modules) != 1 {
			t.Fatalf("single-module verifier should have 1 module; got %d", len(modules))
		}
	})

	t.Run("verifier with no modules gets generic verification module", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
		grantedModules := []string{} // No verify duties
		modules := modulesFor(grants, grantedModules, en)
		keys := moduleKeySet(modules)

		if keys["verification"] != moduleStatusAvailable {
			t.Fatalf("verifier with no duties should see generic verification; got %v", keys)
		}
	})
}

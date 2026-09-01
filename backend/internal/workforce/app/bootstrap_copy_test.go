package app

import (
	"strings"
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
//   - CEO/CXO: Vaccination + Weighing + Counts + Feed + Breeding(soon), never the
//     removed synthetic leadership module or Verification. Expanded drawer.
//   - Park Head: Vaccination + Weighing + Health, NO Counts/Feed/Breeding.
//   - PC Director: Vaccination + Weighing + Health, no Counts/Feed/Breeding.
//   - Growth Director: Weighing only, no Vaccination/Counts/Feed/Breeding.
//   - Verifier: five evidence modules, never one synthetic Verification module.
//     Operator: department modules, never leadership.
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
		if keys["feed_direction"] != moduleStatusAvailable {
			t.Fatalf("CEO must see Feed as available; got %v", keys)
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
				// MAINTAINER DECISION 2026-08-06: leadership and verifier are SEPARATE SURFACES on
				// SEPARATE ROUTES. Leadership videos nav points to /vaccination/videos (leadership-owned),
				// NEVER to /verify (verifier-owned). Verifier bottom bar is [Verify, Alerts];
				// "You" lives in the drawer, and the alerts tab label never names the
				// feature (the href's category still scopes it).
				// Leadership/registry modules are NOT the verifier bar: they legitimately
				// keep their own "you" entry. Only the generic "Alerts" label is shared.
				wantItems := []domain.BootstrapNavigationItem{
					{Key: "calendar", Label: "Calendar", Href: "/calendar"},
					{Key: "videos", Label: "Videos", Href: "/vaccination/videos"},
					{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
					{Key: "you", Label: "You", Href: "/you"},
				}
				if len(m.NavItems) != len(wantItems) {
					t.Fatalf("CEO vaccination bar=%+v want %+v", m.NavItems, wantItems)
				}
				for _, it := range m.NavItems {
					if it.Key == "overview" || it.Key == "weighing" || it.Href == "/leadership" {
						t.Fatalf("CEO vaccination bar must not contain duplicate Overview, Weighing, or /leadership; got %+v", m.NavItems)
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
		for _, required := range []string{"weighing", "aas_health"} {
			if _, ok := keys[required]; !ok {
				t.Fatalf("%s must have the %s module; got %v", role, required, keys)
			}
		}
		for _, banned := range []string{"counts", "feed_direction", "breeding", "leadership", "verification"} {
			if _, ok := keys[banned]; ok {
				t.Fatalf("%s must NOT see %q; got %v", role, banned, keys)
			}
		}
		var vaccination *domain.BootstrapModule
		for i := range modules {
			if modules[i].Key == "vaccination" {
				vaccination = &modules[i]
				break
			}
		}
		if vaccination == nil {
			t.Fatalf("%s vaccination module missing; got %+v", role, modules)
		}
		wantVaccinationItems := []domain.BootstrapNavigationItem{
			{Key: "vaccination", Label: "Stock", Href: "/pc/vaccine-stock"},
			{Key: "videos", Label: "Videos", Href: "/vaccination/videos"},
			{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
			{Key: "you", Label: "You", Href: "/you"},
		}
		if len(vaccination.NavItems) != len(wantVaccinationItems) {
			t.Fatalf("%s vaccination bar=%+v want %+v", role, vaccination.NavItems, wantVaccinationItems)
		}
		for i := range wantVaccinationItems {
			if vaccination.NavItems[i] != wantVaccinationItems[i] {
				t.Fatalf("%s vaccination bar[%d]=%+v want %+v", role, i, vaccination.NavItems[i], wantVaccinationItems[i])
			}
		}
		if got := navChromeFor(grants, modules); got != domain.NavChromeExpanded {
			t.Fatalf("%s chrome = %q, want expanded", role, got)
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
		if got := navChromeFor(grants, modules); got != domain.NavChromeExpanded {
			t.Fatalf("%s chrome = %q, want expanded (weighing + baseline clock)", role, got)
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
		for _, required := range []string{"weighing", "aas_health"} {
			if _, ok := keys[required]; !ok {
				t.Fatalf("park_head must have the %s module; got %v", required, keys)
			}
		}
		for _, banned := range []string{"counts", "feed_direction", "breeding", "leadership", "verification"} {
			if _, ok := keys[banned]; ok {
				t.Fatalf("park_head must NOT see %q; got %v", banned, keys)
			}
		}
		if got := navChromeFor(grants, modules); got != domain.NavChromeExpanded {
			t.Fatalf("park_head chrome = %q, want expanded", got)
		}
	})

	t.Run("verifier with a named feature duty sees that feature's module, never generic verification", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
		modules := modulesFor(grants, []string{"vaccination"}, en)
		keys := moduleKeySet(modules)
		if _, ok := keys["verification"]; ok {
			t.Fatalf("verifier with a named feature duty must NOT get the generic merged verification module; got %v", keys)
		}
		if _, ok := keys["leadership"]; ok {
			t.Fatalf("verifier must NOT see leadership; got %v", keys)
		}
		if len(modules) != 2 || modules[1].Key != "clock" {
			t.Fatalf("single-feature verifier should see [verify module, clock]; got %d: %v", len(modules), keys)
		}
		verify := modules[0]
		if verify.Key != "verify_vaccination" {
			t.Fatalf("verifier module key = %q, want verify_vaccination", verify.Key)
		}
		// Per-module bar keeps its own Alerts tab (binding ruling: "Alerts are NOT one
		// merged tab"), with the alerts category matching what the vaccination
		// verification-bridge actually writes (vaccination_proof).
		//
		// MAINTAINER DECISION 2026-08-03: the verifier bar is [Verify, Alerts, You].
		// "You" carries shared_key "you" so it dedupes across modules like the leadership
		// entries -- the objection was the per-feature REPETITION, not its presence. The
		// alerts tab label never names the feature; the href's category still scopes it.
		wantItems := []domain.BootstrapNavigationItem{
			{Key: "verify", Label: "Verify", Href: "/verify?module=vaccination&category=vaccination_proof"},
			{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
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
			t.Fatalf("hybrid pc director must keep Weighing; got %v", keys)
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

// TestAlertsNavLabelIsGenericInEveryLocale locks the alerts tab label.
//
// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts]; "You"
// lives in the drawer, and the alerts tab label never names the feature (the href's
// category still scopes it).
//
// This test previously asserted the OPPOSITE (per-feature labels "Vaccination
// alerts" / "Weighing alerts" / ... from the now-deleted alertsLabelKeyForFeature
// resolver and the now-deleted "nav.alerts.<feature>" keys). It is repointed, not
// deleted, because the coverage that still matters is that the single generic
// "nav.alerts" key resolves in ALL FOUR locales -- a missing translation would ship
// an English tab title to a Kannada operator. Feature scoping is asserted where it
// actually lives now: the href category, see TestBootstrapAlertsPerModule.
func TestAlertsNavLabelIsGenericInEveryLocale(t *testing.T) {
	wantByLocale := map[string]string{
		"en": "Alerts",
		"hi": "अलर्ट",
		"kn": "ಎಚ್ಚರಿಕೆಗಳು",
		"te": "అలర్ట్లు",
	}
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)}
	for locale, want := range wantByLocale {
		modules := modulesFor(grants, nil, locale)
		seen := 0
		for _, m := range modules {
			for _, item := range m.NavItems {
				// Match the alerts item by KEY, not by a hardcoded href: the feeds are
				// feature-scoped ("/vaccination/alerts", "/weighing/alerts"), and the
				// label under test is the shared "Alerts" string in each locale.
				if item.Key != "alerts" && item.Key != "weighing_alerts" {
					continue
				}
				seen++
				if item.Label != want {
					t.Fatalf("locale %s module %s: alerts label = %q, want %q", locale, m.Key, item.Label, want)
				}
			}
		}
		if seen == 0 {
			t.Fatalf("locale %s: no alerts nav item found to check", locale)
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
		// Feed is BUILT in this tree (moduleNavRegistry marks feed_direction available), so the
		// Feed Director gets a real bottom bar, not the "Soon" roadmap row main's registry had.
		if status := moduleKeySet(modules)["feed_direction"]; status != moduleStatusAvailable {
			t.Fatalf("feed_director feed row status = %q, want %q", status, moduleStatusAvailable)
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

// TestHerdOperationsIsOfferedPerPersonNotPerDirectorJob pins the 2026-08-07 maintainer decision:
// Chandrakant and Dinakar get the Herd Operations (Counts) capture module ON TOP OF their existing
// director access, and they get it because THEY hold counts.write on their own grant row (a tenant
// `operator` grant layered on the director job), never because of the director job itself.
//
// The negative half is the point of the test. A bare pc_director / growth_director -- a future
// holder of either job with no personal operator grant -- must still resolve NO counts module, or
// the per-person grant has silently become a per-job one and the one-module-one-director
// segregation lock is reversed.
func TestHerdOperationsIsOfferedPerPersonNotPerDirectorJob(t *testing.T) {
	const en = localization.DefaultTag

	hasKey := func(keys []string, want string) bool {
		for _, k := range keys {
			if k == want {
				return true
			}
		}
		return false
	}

	// The two named people: director job + the personal tenant `operator` grant that carries
	// counts.write. This is exactly what user_scope_grants holds for them in STG today.
	perPerson := map[string][]domain.GrantSummary{
		"Chandrakant (pc_director + operator)": {
			grantWithRole(permissions.RolePCDirector),
			grantWithRole(permissions.RoleOperator),
		},
		"Dinakar (growth_director + pc_director + operator)": {
			grantWithRole(permissions.RoleGrowthDirector),
			grantWithRole(permissions.RolePCDirector),
			grantWithRole(permissions.RoleOperator),
		},
	}
	for name, grants := range perPerson {
		t.Run(name+" is offered Herd Operations", func(t *testing.T) {
			if !permissions.RoleHasPermission(permissions.RoleOperator, permissions.CountsWrite) {
				t.Fatal("operator no longer carries counts.write; this grant no longer confers capture")
			}
			if keys := leadershipModuleKeys(grants); !hasKey(keys, "counts") {
				t.Fatalf("leadership module keys = %v, want counts offered", keys)
			}
			// The offer must actually RENDER -- these people hold counts.write, so unlike
			// health_director the Counts drawer row and its capture tabs are real.
			if _, ok := moduleKeySet(modulesFor(grants, nil, en))["counts"]; !ok {
				t.Fatal("Counts module did not render for a principal holding counts.write")
			}
			// ...and it is ADDITIVE: their existing director access is untouched.
			keys := moduleKeySet(modulesFor(grants, nil, en))
			for _, want := range []string{"vaccination", "weighing"} {
				if _, ok := keys[want]; !ok {
					t.Fatalf("module %q disappeared; Counts must be added on top, not swapped in", want)
				}
			}
		})
	}

	// park_head is the trap this offer must not fall into. It holds counts.write ON THE ROLE, so
	// keying the offer on the permission instead of the grant silently hands the capture module to
	// every park head -- caught by TestCountsModuleRoleMatrix when exactly that was tried. Asserted
	// here too so the reason travels with the offer it constrains.
	t.Run("park_head holds counts.write on the role and is still NOT offered Herd Operations", func(t *testing.T) {
		if !permissions.RoleHasPermission(permissions.RoleParkHead, permissions.CountsWrite) {
			t.Skip("park_head no longer holds counts.write; this trap no longer exists")
		}
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)}
		if keys := leadershipModuleKeys(grants); hasKey(keys, "counts") {
			t.Fatalf("park_head offered counts (keys=%v); the offer is keyed on a role-wide permission, not the per-person grant", keys)
		}
		if _, ok := moduleKeySet(modulesFor(grants, nil, en))["counts"]; ok {
			t.Fatal("Counts module rendered for a bare park_head")
		}
	})

	// The negative half: the JOB alone confers nothing.
	for _, role := range []string{permissions.RolePCDirector, permissions.RoleGrowthDirector} {
		t.Run("bare "+role+" is NOT offered Herd Operations", func(t *testing.T) {
			grants := []domain.GrantSummary{grantWithRole(role)}
			if permissions.RoleHasPermission(role, permissions.CountsWrite) {
				t.Fatalf("%s now holds counts.write on the ROLE; that hands Counts to every future holder of the job", role)
			}
			if keys := leadershipModuleKeys(grants); hasKey(keys, "counts") {
				t.Fatalf("bare %s offered counts (keys=%v); the per-person grant became per-job", role, keys)
			}
			if _, ok := moduleKeySet(modulesFor(grants, nil, en))["counts"]; ok {
				t.Fatalf("Counts module rendered for a bare %s", role)
			}
		})
	}
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

		// One module per verify duty, plus the baseline Clock module every
		// principal carries (maintainer decision 2026-08-28).
		if len(modules) != 3 || modules[2].Key != "clock" {
			t.Fatalf("multi-module verifier should see [2 verify modules, clock]; got %d: %v", len(modules), keys)
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
		if vaccModule.Href != "/verify?module=vaccination&category=vaccination_proof" {
			t.Fatalf("vaccination module landing href = %q, want /verify", vaccModule.Href)
		}

		// Check vaccination nav items: verify + alerts + you.
		//
		// MAINTAINER DECISION 2026-08-03: the verifier bar is [Verify, Alerts, You].
		// "You" carries shared_key "you" so it dedupes across modules like the leadership
		// entries -- the objection was the per-feature REPETITION, not its presence. The
		// alerts tab label never names the feature; the href's category still scopes it.
		wantVaccItems := []domain.BootstrapNavigationItem{
			{Key: "verify", Label: "Verify", Href: "/verify?module=vaccination&category=vaccination_proof"},
			{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
			{Key: "you", Label: "You", Href: "/you"},
		}
		if len(vaccModule.NavItems) != len(wantVaccItems) {
			t.Fatalf("vaccination nav items = %+v, want %+v", vaccModule.NavItems, wantVaccItems)
		}
		for i := range wantVaccItems {
			if vaccModule.NavItems[i] != wantVaccItems[i] {
				t.Fatalf("vaccination bar item[%d] = %+v, want %+v", i, vaccModule.NavItems[i], wantVaccItems[i])
			}
		}

		// Check weighing module. Same bar shape, SAME generic "Alerts" label, but a
		// DIFFERENT href category -- that category is the load-bearing part of the
		// scoping: a mismatch renders a permanently-empty 200 tab.
		weighModule := modules[1]
		if weighModule.Key != "verify_weighing" {
			t.Fatalf("second module key = %q, want verify_weighing", weighModule.Key)
		}
		if weighModule.Label != "Weighing" {
			t.Fatalf("weighing module label = %q, want Weighing", weighModule.Label)
		}
		wantWeighItems := []domain.BootstrapNavigationItem{
			{Key: "verify", Label: "Verify", Href: "/verify?module=weighing&category=weighing_proof"},
			{Key: "alerts", Label: "Alerts", Href: "/weighing/alerts"},
			{Key: "you", Label: "You", Href: "/you"},
		}
		if len(weighModule.NavItems) != len(wantWeighItems) {
			t.Fatalf("weighing nav items = %+v, want %+v", weighModule.NavItems, wantWeighItems)
		}
		for i := range wantWeighItems {
			if weighModule.NavItems[i] != wantWeighItems[i] {
				t.Fatalf("weighing bar item[%d] = %+v, want %+v", i, weighModule.NavItems[i], wantWeighItems[i])
			}
		}

		// The two features must NOT collapse onto one alerts feed: identical label,
		// different category.
		if vaccModule.NavItems[1].Label != weighModule.NavItems[1].Label {
			t.Fatalf("alerts label must be identical across features; got %q vs %q", vaccModule.NavItems[1].Label, weighModule.NavItems[1].Label)
		}
		if vaccModule.NavItems[1].Href == weighModule.NavItems[1].Href {
			t.Fatalf("alerts href must stay feature-scoped; both features got %q", vaccModule.NavItems[1].Href)
		}

		// THE NO-REPETITION PROPERTY, at the COMPOSITION layer.
		//
		// Read the layering before reading the numbers below. modulesFor and
		// visibleNavigationFor compose a module's OWN bar and still carry the "you"
		// contribution; the placement rule runs AFTER them, in Bootstrap
		// (applyProfileEntryPlacement), and is what decides whether the served payload
		// keeps that entry or hands it to the drawer. So "exactly one You" here is the
		// pre-placement composition invariant -- no module may contribute You twice, and
		// the served bar is one module's bar rather than a merged one -- NOT a claim that
		// this verifier's phone shows You in the bottom bar. He holds two features, so it
		// is moved to the drawer; that is asserted on the SERVED payload immediately
		// after this block, and across every principal shape in
		// TestProfileEntryPlacementFollowsModuleCount.
		//
		// Note also, precisely: verificationModuleForFeature builds its NavItems by hand
		// and never calls composeNavigationFromModules, the ONLY reader of shared_key, so
		// `shared_key: "you"` on that contribution is inert here -- it is not what keeps
		// You single. The composition stays single because the bar is MODULE-SCOPED, and
		// the served payload stays single because of the placement rule.
		nav := visibleNavigationFor(grants, grantedModules, en)
		youCount := 0
		seenKeys := make(map[string]bool, len(nav))
		for _, item := range nav {
			if item.Key == "you" {
				youCount++
			}
			if seenKeys[item.Key] {
				t.Fatalf("multi-module verifier served bar repeats key %q; got %+v", item.Key, nav)
			}
			seenKeys[item.Key] = true
		}
		if youCount != 1 {
			t.Fatalf("multi-module verifier served bar carries %d You items, want exactly 1 (a second feature must add a drawer row, never a second You); got %+v", youCount, nav)
		}
		for _, m := range modules {
			// The baseline Clock module deliberately carries no You (the
			// approvals precedent: it is never held alone).
			if m.Key == "clock" {
				continue
			}
			perModule := 0
			for _, item := range m.NavItems {
				if item.Key == "you" {
					perModule++
				}
			}
			if perModule != 1 {
				t.Fatalf("module %q carries %d You items, want exactly 1; got %+v", m.Key, perModule, m.NavItems)
			}
		}

		// ...and now the SERVED payload, which is what the phone actually renders. Two
		// features means a drawer, and the drawer owns You: it must be gone from the
		// served bar and from BOTH module bars, so no drawer selection can bring it back.
		// Without this, the composition assertions above read as an endorsement of a You
		// tab per verify feature -- the exact thing the maintainer rejected three times.
		served := bootstrapFor(t, grants, grantedModules)
		if served.NavChrome != domain.NavChromeExpanded {
			t.Fatalf("two-feature verifier NavChrome=%q want %q (he must have a drawer to hold You)", served.NavChrome, domain.NavChromeExpanded)
		}
		if n := countProfileEntries(served.VisibleNavigation); n != 0 {
			t.Fatalf("two-feature verifier served bar still carries %d You item(s); it belongs in the drawer: %+v", n, served.VisibleNavigation)
		}
		for _, m := range served.Modules {
			if n := countProfileEntries(m.NavItems); n != 0 {
				t.Fatalf("two-feature verifier module %q still carries %d You item(s); switching to it would show You twice: %+v", m.Key, n, m.NavItems)
			}
		}
	})

	t.Run("single-module verifier gets minimal chrome with a feature-scoped module, never generic verification", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
		grantedModules := []string{"pc.vaccination"}
		modules := modulesFor(grants, grantedModules, en)
		keys := moduleKeySet(modules)

		// The generic merged "verification" module never appears for a verifier -- per
		// the binding ruling, Alerts is never a merged/un-scoped tab, so a single-feature
		// verifier gets the SAME feature-scoped [Verify, Alerts] module a
		// multi-feature verifier gets for each of their features (chrome collapses to
		// minimal below the 2-module threshold, but the module itself is still
		// feature-scoped).
		if _, ok := keys["verification"]; ok {
			t.Fatalf("single-module verifier must NOT see generic verification module; got %v", keys)
		}
		if len(modules) != 2 || modules[1].Key != "clock" {
			t.Fatalf("single-module verifier should have [verify module, clock]; got %d: %v", len(modules), keys)
		}
		if modules[0].Key != "verify_vaccination" {
			t.Fatalf("single-module verifier module key = %q, want verify_vaccination", modules[0].Key)
		}
		// MAINTAINER DECISION 2026-08-03: the verifier bar is [Verify, Alerts, You].
		// "You" carries shared_key "you" so it dedupes across modules like the leadership
		// entries -- the objection was the per-feature REPETITION, not its presence. The
		// alerts tab label never names the feature; the href's category still scopes it.
		wantItems := []domain.BootstrapNavigationItem{
			{Key: "verify", Label: "Verify", Href: "/verify?module=vaccination&category=vaccination_proof"},
			{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
			{Key: "you", Label: "You", Href: "/you"},
		}
		if len(modules[0].NavItems) != len(wantItems) {
			t.Fatalf("nav items = %+v want %+v", modules[0].NavItems, wantItems)
		}
		for i := range wantItems {
			if modules[0].NavItems[i] != wantItems[i] {
				t.Fatalf("nav item[%d]=%+v want %+v", i, modules[0].NavItems[i], wantItems[i])
			}
		}
	})

	// TestBootstrapAlertsPerModule / grantedModules == ["verification"] below cover the
	// department-level-grant case (no feature named at all) that this suite previously
	// missed -- the exact shape of the real defect (a verifier with only a
	// department_module_grants.module_key = "verification" row) reproduced against the
	// live QA DB on 2026-08-02: grant_count=1, ListGrantedModuleKeys returns
	// ["verification"], and the bar rendered [Verify, You] with no Alerts at all.
	t.Run("verifier with no duties is scoped to every built feature, never generic verification", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
		grantedModules := []string{} // No verify duties
		modules := modulesFor(grants, grantedModules, en)
		keys := moduleKeySet(modules)

		if _, ok := keys["verification"]; ok {
			t.Fatalf("verifier with no duties must NOT see generic verification; got %v", keys)
		}
		wantKeys := map[string]bool{
			"verify_vaccination": true, "verify_weighing": true, "verify_counts": true,
			"verify_feed_direction": true, "verify_aas_health": true, "verify_milk": true,
			"verify_pc_care": true, "clock": true,
		}
		if len(keys) != len(wantKeys) {
			t.Fatalf("verifier with no duties should see one module per built feature; got %v", keys)
		}
		for k := range wantKeys {
			if _, ok := keys[k]; !ok {
				t.Fatalf("verifier with no duties missing module %q; got %v", k, keys)
			}
		}
	})
}

// TestBootstrapAlertsPerModule reproduces the department-level "verification" grant
// case found live on 2026-08-02 (Jyothi, user 90000000-0000-4000-8000-000000000104):
// department_module_grants.module_key = "verification" with no position_module_duties
// rows, so ListGrantedModuleKeys returns ["verification"] -- a literal module key that
// names no feature. Before this fix, candidateModuleKeys treated any grantedModules with
// len <= 1 as "single/no duties" and fell back to the generic, un-scoped "verification"
// registry module, so the served bar was [Verify, You] with no Alerts item at all,
// contradicting the binding ruling ("Per-module bottom bar = [Verify, Alerts] ...
// Alerts are NOT one merged tab").
func TestBootstrapAlertsPerModule(t *testing.T) {
	const en = localization.DefaultTag
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
	grantedModules := []string{"verification"}

	modules := modulesFor(grants, grantedModules, en)
	if len(modules) == 0 {
		t.Fatalf("verifier with only a department-level verification grant must still get per-module bars; got none")
	}

	wantCategories := map[string]string{
		"verify_vaccination":    "vaccination_proof",
		"verify_weighing":       "weighing_proof",
		"verify_counts":         "shifting_move",     // NOT "counts_proof" -- see verificationCategoryForFeature.
		"verify_feed_direction": "feed_distribution", // NOT "feed_direction_proof" -- see verificationCategoryForFeature.
		"verify_aas_health":     "health_adults",     // NOT "aas_health_proof" -- see verificationCategoryForFeature.
		"verify_milk":           "milk_preparation",  // NOT "milk_proof" -- see verificationCategoryForFeature.
		"verify_pc_care":        "pc_deworming",      // NOT "pc_care_proof" -- see verificationCategoryForFeature.
	}
	// MAINTAINER DECISION 2026-08-03: the verifier bar is [Verify, Alerts, You]. "You"
	// carries shared_key "you" so it dedupes across modules like the leadership entries
	// -- the objection was the per-feature REPETITION, not its presence. The alerts tab
	// label never names the feature; the href's category still scopes it. This test is
	// where that scoping is proven: all three features must produce the IDENTICAL label
	// "Alerts" but three DIFFERENT href categories. A category mismatch renders a
	// permanently-empty 200 tab, so the category assertion below must never be relaxed
	// just because the label stopped varying.
	const wantAlertsLabel = "Alerts"
	seenHrefs := make(map[string]string, len(modules))
	seen := make(map[string]bool, len(modules))
	for _, m := range modules {
		// The baseline Clock module rides along for every principal
		// (2026-08-28); it carries no verify/alerts tabs to assert here.
		if m.Key == "clock" {
			continue
		}
		seen[m.Key] = true
		wantCategory, known := wantCategories[m.Key]
		if !known {
			t.Fatalf("unexpected verifier module %q; got modules %v", m.Key, moduleKeySet(modules))
		}
		var verify, alerts, you *domain.BootstrapNavigationItem
		youCount := 0
		for i := range m.NavItems {
			switch m.NavItems[i].Key {
			case "verify":
				verify = &m.NavItems[i]
			case "alerts":
				alerts = &m.NavItems[i]
			case "you":
				you = &m.NavItems[i]
				youCount++
			}
		}
		if verify == nil {
			t.Fatalf("module %q missing Verify item; got %+v", m.Key, m.NavItems)
		}
		if alerts == nil {
			t.Fatalf("module %q missing Alerts item -- per-module bottom bar must be [Verify, Alerts, You], never [Verify] alone; got %+v", m.Key, m.NavItems)
		}
		if you == nil {
			t.Fatalf("module %q missing You item -- the verifier must be able to reach their profile; got %+v", m.Key, m.NavItems)
		}
		if youCount != 1 {
			t.Fatalf("module %q carries %d You items, want exactly 1 (shared_key %q dedupes it); got %+v", m.Key, youCount, "you", m.NavItems)
		}
		if you.Label != "You" || you.Href != "/you" {
			t.Fatalf("module %q You item = %+v, want {Key:you Label:You Href:/you}", m.Key, *you)
		}
		if len(m.NavItems) != 3 {
			t.Fatalf("module %q bar = %+v, want exactly [Verify, Alerts, You]", m.Key, m.NavItems)
		}
		if m.NavItems[2].Key != "you" {
			t.Fatalf("module %q bar must end with You (priority 100); got %+v", m.Key, m.NavItems)
		}
		if alerts.Label != wantAlertsLabel {
			t.Fatalf("module %q alerts label = %q, want the generic %q (the tab must not name the feature the verifier is already inside)", m.Key, alerts.Label, wantAlertsLabel)
		}
		wantHref := wantVerifierAlertsHref(m.Key, wantCategory)
		if alerts.Href != wantHref {
			t.Fatalf("module %q alerts href = %q, want %q (category must match what the feature's verification-bridge writes to verification_items.category, not just the module name)", m.Key, alerts.Href, wantHref)
		}
		if other, dup := seenHrefs[alerts.Href]; dup {
			t.Fatalf("modules %q and %q share alerts href %q -- the generic label must not have collapsed the feature scoping", other, m.Key, alerts.Href)
		}
		seenHrefs[alerts.Href] = m.Key
	}
	if len(seenHrefs) != len(wantCategories) {
		t.Fatalf("expected one distinct alerts category per feature; got %v", seenHrefs)
	}
	for key := range wantCategories {
		if !seen[key] {
			t.Fatalf("expected verifier module %q missing; got %v", key, moduleKeySet(modules))
		}
	}

	// visible_navigation (the served bottom bar before any drawer switch) must match the
	// first module's bar exactly, not the generic registry "verification" entry.
	nav := visibleNavigationFor(grants, grantedModules, en)
	if len(nav) != len(modules[0].NavItems) {
		t.Fatalf("visible_navigation = %+v, want modules[0].NavItems = %+v", nav, modules[0].NavItems)
	}
	for i := range nav {
		if nav[i] != modules[0].NavItems[i] {
			t.Fatalf("visible_navigation[%d] = %+v, want %+v", i, nav[i], modules[0].NavItems[i])
		}
	}
	foundAlerts := false
	for _, item := range nav {
		if item.Key == "alerts" {
			foundAlerts = true
		}
	}
	if !foundAlerts {
		t.Fatalf("visible_navigation must include Alerts for a verifier; got %+v", nav)
	}
}

// TestVerifierDrawerHasNoDuplicateOrNamelessModules reproduces what Jyothi's phone actually
// showed on 2026-08-07: a nameless row at the top of the drawer, then "Feed" twice and
// "Vaccination" twice -- four rows a verifier cannot tell apart or identify.
//
// The grantedModules below are her REAL rows, read from STG. They come from a UNION of two
// tables that spell the same module differently:
//
//	department_module_grants.module_key   position_module_duties.module_code
//	  counts                                counts          <- same string, collapses
//	  weighing                              weighing        <- same string, collapses
//	  vaccination                           pc.vaccination  <- DIFFERENT, both survive
//	  feed_direction                        feed.direction  <- DIFFERENT, both survive
//	  milk                                  aas_health
//
// normalizeModuleFeatureKey maps pc.vaccination -> vaccination and feed.direction ->
// feed_direction, but it runs at RENDER time, after both dedupe passes (SQL UNION and
// verifierFeatureKeys' seen[key]) have already compared raw strings. Dedupe is simply
// ordered before normalization.
func TestVerifierDrawerHasNoDuplicateOrNamelessModules(t *testing.T) {
	const en = localization.DefaultTag
	jyothiGrantedModules := []string{
		"aas_health", "counts", "counts", "feed_direction", "feed.direction",
		"milk", "pc.vaccination", "vaccination", "weighing", "weighing",
	}
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}

	t.Run("one module per feature, whichever way the two tables spell it", func(t *testing.T) {
		modules := modulesFor(grants, jyothiGrantedModules, en)
		count := map[string]int{}
		for _, m := range modules {
			count[m.Key]++
		}
		for key, n := range count {
			if n > 1 {
				t.Errorf("module %q emitted %d times; the drawer shows %d identical rows", key, n, n)
			}
		}
		// Named explicitly: these are the two pairs that actually shipped.
		for _, key := range []string{"verify_vaccination", "verify_feed_direction"} {
			if count[key] != 1 {
				t.Errorf("%s emitted %d times, want exactly 1", key, count[key])
			}
		}
	})

	t.Run("every module row has a name", func(t *testing.T) {
		for _, m := range modulesFor(grants, jyothiGrantedModules, en) {
			if strings.TrimSpace(m.Label) == "" {
				t.Errorf("module %q rendered with an EMPTY label; the drawer shows a nameless row", m.Key)
			}
		}
	})
}

// TestEveryVerifierModuleLabelResolvesInTheCopyCatalog is the guard that makes the blank-row
// class of defect loud instead of silent. localizedBootstrapLabel returns "" for a key the
// catalog does not contain, so a module whose labelKey is absent ships a nameless row and
// nothing fails -- which is exactly how verify_aas_health reached a real phone.
//
// verificationModuleForFeature derives its labelKey from a four-entry map with a
// "module."+normalized fallback. That fallback is a GUESS: it happens to be right for milk
// ("module.milk" exists) and wrong for aas_health (the catalog spells it "module.health").
// This asserts the guess is correct for every feature the drawer can build, in every locale.
func TestEveryVerifierModuleLabelResolvesInTheCopyCatalog(t *testing.T) {
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
	features := []string{"aas_health", "counts", "feed_direction", "milk", "vaccination", "weighing", "breeding"}

	for locale := range bootstrapLabels {
		for _, feature := range features {
			m := verificationModuleForFeature(feature, grants, locale)
			if strings.TrimSpace(m.Label) == "" {
				t.Errorf("feature %q has no label in locale %q (module key %q); localizedBootstrapLabel returned \"\" for a missing catalog key",
					feature, locale, m.Key)
			}
			for _, item := range m.NavItems {
				if strings.TrimSpace(item.Label) == "" {
					t.Errorf("feature %q nav item %q has no label in locale %q", feature, item.Key, locale)
				}
			}
		}
	}
}

func TestVaccinationModuleHasOneLandingNavItem(t *testing.T) {
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleOperator)}
	modules := modulesFor(grants, []string{"vaccination"}, localization.DefaultTag)
	var vaccination *domain.BootstrapModule
	for i := range modules {
		if modules[i].Key == "vaccination" {
			vaccination = &modules[i]
			break
		}
	}
	if vaccination == nil {
		t.Fatalf("vaccination module missing from %+v", modules)
	}

	seenHref := map[string]domain.BootstrapNavigationItem{}
	for _, item := range vaccination.NavItems {
		if previous, ok := seenHref[item.Href]; ok {
			t.Fatalf("duplicate href %q in vaccination bar: first=%+v duplicate=%+v all=%+v",
				item.Href, previous, item, vaccination.NavItems)
		}
		seenHref[item.Href] = item
	}
	if got := seenHref["/vaccination"]; got.Key != "vaccination" || got.Label != "Drive" {
		t.Fatalf("/vaccination nav item=%+v want key=vaccination label=Drive", got)
	}
}

// wantVerifierAlertsHref mirrors verifierAlertsHref: a module with its OWN lifecycle feed points
// there, everything else keeps the pending-queue href.
func wantVerifierAlertsHref(feature, category string) string {
	switch strings.TrimPrefix(feature, "verify_") {
	case "vaccination":
		return "/vaccination/alerts"
	case "weighing":
		return "/weighing/alerts"
	default:
		return "/verify/alerts?category=" + category
	}
}

// TestVerifierNoDutyFallbackCoversEveryBuiltVerifiableModule pins builtVerifiableFeatures against
// the module registry, in both directions, so the list cannot silently go stale again.
//
// The defect this guards (found 2026-08-29): feed_direction, aas_health, and milk were all
// moduleStatusAvailable with registered verification categories, but builtVerifiableFeatures still
// carried only the original four features -- so a verifier holding only a coarse department-level
// "verification" grant (no named duty) silently never saw Health, Feed, or Milk evidence. Nothing
// erred anywhere; the evidence just never reached her.
func TestVerifierNoDutyFallbackCoversEveryBuiltVerifiableModule(t *testing.T) {
	// Modules that are deliberately NOT verifiable: they produce no verification items, so a
	// verifier fallback entry for them would open a queue that answers 400 unknown_category
	// forever. Adding a module here instead of builtVerifiableFeatures is a recorded decision,
	// not a default.
	notVerifiable := map[string]string{
		"approvals": "capture-approval queue; approving is not evidence review and enqueues nothing",
		"clock":     "attendance clock-in/out; no proof video, no verification category",
		"toxin":     "strip-test module with its own CEO/CXO review routes; not a verificationcatalog producer",
	}

	listed := map[string]bool{}
	for _, key := range builtVerifiableFeatures {
		listed[key] = true

		def, ok := moduleNavRegistry[key]
		if !ok {
			t.Fatalf("builtVerifiableFeatures entry %q is not in moduleNavRegistry", key)
		}
		if def.status != moduleStatusAvailable {
			t.Fatalf("builtVerifiableFeatures entry %q has status %q; only built (available) modules are verifier-scoped", key, def.status)
		}
	}

	for key, def := range moduleNavRegistry {
		if def.status != moduleStatusAvailable {
			continue
		}
		if _, exempt := notVerifiable[key]; exempt {
			if listed[key] {
				t.Fatalf("module %q is both in builtVerifiableFeatures and in the notVerifiable exemption; pick one", key)
			}
			continue
		}
		if !listed[key] {
			t.Fatalf("available module %q is missing from builtVerifiableFeatures: a no-duty verifier would silently never see its evidence. Add it in drawer priority order, or record it in this test's notVerifiable map with a reason", key)
		}
	}
}

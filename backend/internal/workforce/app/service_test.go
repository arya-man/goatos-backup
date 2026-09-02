package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	testTenant   = "00000000-0000-4000-8000-000000000001"
	testActor    = "90000000-0000-4000-8000-000000000001"
	testOperator = "91000000-0000-4000-8000-000000000001"
	testDevice   = "92000000-0000-4000-8000-000000000001"
)

func TestBootstrapDeniesMissingProfile(t *testing.T) {
	svc := NewService(&fakeRepo{profileErr: ports.ErrNotFound})
	_, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	assertAppCode(t, err, "operator_profile_missing")
}

func TestBootstrapDeniesInactiveProfile(t *testing.T) {
	svc := NewService(&fakeRepo{profile: profile("inactive"), grants: []domain.GrantSummary{grant()}})
	_, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	assertAppCode(t, err, "operator_profile_inactive")
}

func TestBootstrapDeniesMissingGrant(t *testing.T) {
	svc := NewService(&fakeRepo{profile: profile("active")})
	_, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	assertAppCode(t, err, "operator_grant_missing")
}

// TestBootstrapDeniesAdministrativelyRevokedDevice: a device revoked via the admin/security path
// (workforce RevokeDevice, which stamps metadata["revocation_reason"]) must stay locked out and
// must NEVER be silently reactivated by Bootstrap -- see isAdministrativelyRevoked.
func TestBootstrapDeniesAdministrativelyRevokedDevice(t *testing.T) {
	revoked := device("revoked")
	revoked.Metadata = map[string]any{"revocation_reason": "lost phone"}
	repo := &fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grant()},
		device:  revoked,
	}
	svc := NewService(repo)
	_, err := svc.Bootstrap(context.Background(), testTenant, testActor, testDevice, "", "trace-1")
	assertAppCode(t, err, "device_revoked")
	if len(repo.registerDeviceCalls) != 0 {
		t.Fatalf("RegisterDevice called %d times, want 0 (administrative revocation must never self-heal)", len(repo.registerDeviceCalls))
	}
}

// TestBootstrapSelfHealsPushSuppressedDevice is the P0 regression test: a device left non-active
// by a push-delivery side effect (SuppressInvalidRecipient, metadata["fcm_invalidated_reason"], NO
// revocation_reason) must self-heal on Bootstrap via the same RegisterDevice upsert path a fresh
// install uses, WITHOUT requiring the app to clear data / reinstall.
func TestBootstrapSelfHealsPushSuppressedDevice(t *testing.T) {
	suppressed := device("revoked")
	suppressed.Metadata = map[string]any{"fcm_invalidated_reason": "FCM: UNREGISTERED"}
	reactivated := device("active")
	repo := &fakeRepo{
		profile:              profile("active"),
		grants:               []domain.GrantSummary{grant()},
		device:               suppressed,
		registerDeviceResult: reactivated,
	}
	svc := NewService(repo)
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, testDevice, "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v, want self-heal to succeed", err)
	}
	if got.DeviceState.Status != "active" {
		t.Fatalf("device state=%#v, want active after self-heal", got.DeviceState)
	}
	if len(repo.registerDeviceCalls) != 1 {
		t.Fatalf("RegisterDevice called %d times, want 1 (self-heal must reactivate via the register path)", len(repo.registerDeviceCalls))
	}
	call := repo.registerDeviceCalls[0]
	if call.TenantID != testTenant || call.ActorID != testActor {
		t.Fatalf("RegisterDevice scoped to tenant=%q actor=%q, want tenant=%q actor=%q (must reactivate only the authenticated owner's device)", call.TenantID, call.ActorID, testTenant, testActor)
	}
	if call.Body.AppInstallID != suppressed.AppInstallID {
		t.Fatalf("RegisterDevice app_install_id=%q, want %q (must re-key onto the same device row)", call.Body.AppInstallID, suppressed.AppInstallID)
	}
}

func TestBootstrapAllowsFreshUnregisteredDevice(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile:   profile("active"),
		grants:    []domain.GrantSummary{grant()},
		deviceErr: ports.ErrNotFound,
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, testDevice, "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	if got.DeviceState.Device == nil {
		t.Fatalf("device state missing device: %#v", got.DeviceState)
	}
	if got.DeviceState.Status != "not_registered" || got.DeviceState.Device.Status != "not_registered" {
		t.Fatalf("device state=%#v, want not_registered", got.DeviceState)
	}
}

func TestBootstrapAllowsActiveProfileGrantCapabilityDevice(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grant()},
		caps: []domain.CapabilityAssignment{
			{CapabilityCode: "movement.execute", Status: "active"},
			{CapabilityCode: "media.video_capture", Status: "active"},
		},
		device: device("active"),
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, testDevice, "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	if got.Actor.ActorID != testActor || got.OperatorProfile.OperatorID != testOperator {
		t.Fatalf("unexpected bootstrap identity: %#v", got.Actor)
	}
	if got.DeviceState.Device == nil || got.DeviceState.Device.Status != "active" {
		t.Fatalf("device state=%#v", got.DeviceState)
	}
	if !got.FeatureFlags["proof_capture"] {
		t.Fatal("proof_capture should be enabled from media.video_capture capability")
	}
	if len(got.TaskQueueDescriptors) < 2 {
		t.Fatalf("expected movement queue descriptor, got %#v", got.TaskQueueDescriptors)
	}
}

func TestBootstrapPermissionDerivedExecutionFlags(t *testing.T) {
	tests := []struct {
		name                   string
		role                   string
		modules                []string
		wantVaccinationExecute bool
		wantWeighingExecute    bool
		wantWeighingOversee    bool
		wantVideoControls      bool
	}{
		{name: "operator executes vaccination and weighing when both modules are granted", role: permissions.RoleOperator, modules: []string{"vaccination", "weighing"}, wantVaccinationExecute: true, wantWeighingExecute: true, wantWeighingOversee: false, wantVideoControls: false},
		{name: "operator executes only vaccination when only vaccination module is granted", role: permissions.RoleOperator, modules: []string{"vaccination"}, wantVaccinationExecute: true, wantWeighingExecute: false, wantWeighingOversee: false, wantVideoControls: false},
		{name: "operator executes only weighing when only weighing module is granted", role: permissions.RoleOperator, modules: []string{"weighing"}, wantVaccinationExecute: false, wantWeighingExecute: true, wantWeighingOversee: false, wantVideoControls: false},
		{name: "pc director executes vaccination only", role: permissions.RolePCDirector, modules: []string{"vaccination", "weighing"}, wantVaccinationExecute: true, wantWeighingExecute: false, wantWeighingOversee: false, wantVideoControls: true},
		// The Operators surface is the growth director's alone. The CEO plans (weighing.plan) and
		// lands on the flat all-tasks list, so a second someone-else's-work tab is redundant there.
		{name: "growth director executes and oversees weighing", role: permissions.RoleGrowthDirector, modules: []string{"vaccination", "weighing"}, wantVaccinationExecute: false, wantWeighingExecute: true, wantWeighingOversee: true, wantVideoControls: true},
		// Leadership modules come from the leadership TIER (leadershipModuleKeys), not from
		// department_module_grants, so a growth director keeps weighing even when the granted
		// module list says otherwise. The flag still tracks the permission.
		{name: "growth director keeps weighing from leadership tier regardless of granted modules", role: permissions.RoleGrowthDirector, modules: []string{"vaccination"}, wantVaccinationExecute: false, wantWeighingExecute: true, wantWeighingOversee: true, wantVideoControls: true},
		// The CEO plans and oversees weighing but holds neither TaskExecute nor WeighingExecute:
		// a planner must never reach a scan surface.
		{name: "ceo plans weighing but neither executes nor oversees operators", role: permissions.RoleCEOInternal, modules: []string{"vaccination", "weighing"}, wantVaccinationExecute: false, wantWeighingExecute: false, wantWeighingOversee: false, wantVideoControls: true},
		{name: "verifier is display and review only", role: permissions.RoleVerifier, wantVaccinationExecute: false, wantWeighingExecute: false, wantWeighingOversee: false, wantVideoControls: false},
		{name: "park head sees operational nav without field execution", role: permissions.RoleParkHead, wantVaccinationExecute: false, wantWeighingExecute: false, wantWeighingOversee: false, wantVideoControls: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(&fakeRepo{
				profile:        profile("active"),
				grants:         []domain.GrantSummary{grantWithRole(tc.role)},
				grantedModules: tc.modules,
			})
			got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
			if err != nil {
				t.Fatalf("Bootstrap() error=%v", err)
			}
			if got.FeatureFlags["vaccination_execute"] != tc.wantVaccinationExecute {
				t.Fatalf("vaccination_execute=%v want %v", got.FeatureFlags["vaccination_execute"], tc.wantVaccinationExecute)
			}
			if got.FeatureFlags["weighing_execute"] != tc.wantWeighingExecute {
				t.Fatalf("weighing_execute=%v want %v", got.FeatureFlags["weighing_execute"], tc.wantWeighingExecute)
			}
			if got.FeatureFlags["weighing_oversee_operators"] != tc.wantWeighingOversee {
				t.Fatalf("weighing_oversee_operators=%v want %v", got.FeatureFlags["weighing_oversee_operators"], tc.wantWeighingOversee)
			}
			if got.FeatureFlags["verification_video_controls"] != tc.wantVideoControls {
				t.Fatalf("verification_video_controls=%v want %v", got.FeatureFlags["verification_video_controls"], tc.wantVideoControls)
			}
		})
	}
}

func TestBootstrapProtocolAdherenceCardRoleGate(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{role: permissions.RoleCEOInternal, want: true},
		{role: permissions.RolePCDirector, want: true},
		{role: permissions.RoleOperator, want: false},
		{role: permissions.RoleParkHead, want: false},
		{role: permissions.RoleVerifier, want: false},
		{role: permissions.RoleGrowthDirector, want: false},
		{role: permissions.RoleFeedDirector, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.role, func(t *testing.T) {
			svc := NewService(&fakeRepo{
				profile: profile("active"),
				grants:  []domain.GrantSummary{grantWithRole(tc.role)},
			})
			got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
			if err != nil {
				t.Fatalf("Bootstrap() error=%v", err)
			}
			if got.FeatureFlags["protocol_adherence_card"] != tc.want {
				t.Fatalf("protocol_adherence_card=%v want %v", got.FeatureFlags["protocol_adherence_card"], tc.want)
			}
		})
	}
}

func TestBootstrapPopulatesOperatorNavAndChrome(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile:        profile("active"),
		grants:         []domain.GrantSummary{grant()},
		grantedModules: []string{"vaccination"},
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	wantNav := []domain.BootstrapNavigationItem{
		{Key: "vaccination", Label: "Drive", Href: "/vaccination"},
		// Vaccine-stock capture tab (maintainer decision 2026-09-02): the park's own
		// vaccination operators record the fridge proof, so the operator bar carries Stock.
		{Key: "vaccine_stock", Label: "Stock", Href: "/pc/vaccine-stock"},
		// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). This is a leadership/registry bar, so
		// it legitimately KEEPS its "you" entry -- only the label went generic.
		{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
		// The baseline Clock module (maintainer decision 2026-08-28: everyone
		// clocks in) gives every principal >=2 modules, so chrome is expanded
		// and "You" lives in the drawer footer per the 2026-08-03 placement rule.
	}
	if len(got.VisibleNavigation) != len(wantNav) {
		t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, wantNav)
	}
	for i := range wantNav {
		if got.VisibleNavigation[i] != wantNav[i] {
			t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], wantNav[i])
		}
	}
	if got.NavChrome != domain.NavChromeExpanded {
		t.Fatalf("NavChrome=%q want %q (vaccination + baseline clock)", got.NavChrome, domain.NavChromeExpanded)
	}
}

func TestBootstrapLocalizesBackendOwnedLabels(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile:        profile("active"),
		grants:         []domain.GrantSummary{grant()},
		grantedModules: []string{"vaccination"},
		caps:           []domain.CapabilityAssignment{{CapabilityCode: "movement.execute", Status: "active"}},
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "hi", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	wantNav := []domain.BootstrapNavigationItem{
		{Key: "vaccination", Label: "ड्राइव", Href: "/vaccination"},
		// Vaccine-stock capture tab (maintainer decision 2026-09-02), localized like its peers.
		{Key: "vaccine_stock", Label: "स्टॉक", Href: "/pc/vaccine-stock"},
		// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). The Hindi label went generic with it.
		{Key: "alerts", Label: "अलर्ट", Href: "/vaccination/alerts"},
	}
	if len(got.VisibleNavigation) != len(wantNav) {
		t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, wantNav)
	}
	for i := range wantNav {
		if got.VisibleNavigation[i] != wantNav[i] {
			t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], wantNav[i])
		}
	}
	if got.TaskQueueDescriptors[0].Label != "सौंपा गया काम" {
		t.Fatalf("assigned queue label=%q", got.TaskQueueDescriptors[0].Label)
	}
	if got.TaskQueueDescriptors[1].Label != "शिफ्टिंग" {
		t.Fatalf("shifting queue label=%q", got.TaskQueueDescriptors[1].Label)
	}
}

// TestBootstrapLeadershipGetsFixedNav pins that a preventive-care leader
// (park_head) lands in the shared Vaccination area. The standalone
// leadership/overview screen is intentionally absent.
func TestBootstrapLeadershipGetsFixedNav(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)},
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	wantNav := []domain.BootstrapNavigationItem{
		{Key: "calendar", Label: "Calendar", Href: "/calendar"},
		{Key: "videos", Label: "Videos", Href: "/vaccination/videos"},
		// MAINTAINER DECISION 2026-08-06: leadership and verifier are SEPARATE SURFACES on
		// SEPARATE ROUTES. Leadership videos nav points to /vaccination/videos (leadership-owned),
		// NEVER to /verify (verifier-owned). Verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). This is a leadership/registry bar, so
		// it legitimately KEEPS its "you" entry -- only the label went generic.
		{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
		// No "you" here: preventive-care leadership holds vaccination + weighing + health, so
		// navChrome is expanded and applyProfileEntryPlacement moves the account entry into the
		// drawer footer -- exactly once, instead of once per module bar.
	}
	if len(got.VisibleNavigation) != len(wantNav) {
		t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, wantNav)
	}
	for i := range wantNav {
		if got.VisibleNavigation[i] != wantNav[i] {
			t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], wantNav[i])
		}
	}
	if got.NavChrome != domain.NavChromeExpanded {
		t.Fatalf("NavChrome=%q want %q (park_head holds the three preventive-care verticals)", got.NavChrome, domain.NavChromeExpanded)
	}
}

// TestBootstrapVerifierGetsStandaloneVerificationNav is the end-to-end regression guard
// for the "Alerts silently missing" defect found live on 2026-08-02: a verifier with no
// module-specific grant (no position_module_duties row, no department feature grant --
// exactly a bare RoleVerifier grant, as ListGrantedModuleKeys returns for a principal
// whose only department grant is the generic "verification" key) must still get a
// feature-scoped [Verify, Alerts] bar per built feature, never a bare [Verify] with
// no Alerts tab.
func TestBootstrapVerifierGetsStandaloneVerificationNav(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	// No named duties -> scoped to every built feature; the active/default bar is the
	// first feature (vaccination, drawer priority 1).
	//
	// MAINTAINER RULING 2026-08-03: this verifier holds THREE features, so he has a
	// drawer, and "You" lives in that drawer -- not in this bar, and not repeated in each
	// feature's bar. The served bar is therefore [Verify, Alerts] exactly. You was not
	// deleted, it was MOVED (applyProfileEntryPlacement); that the >=2-module principal
	// still has exactly one route to /you is asserted in
	// TestProfileEntryPlacementFollowsModuleCount. A verifier with ONE feature has no
	// drawer and keeps You on the bar -- see
	// TestBootstrapSingleFeatureVerifierGetsFeatureScopedAlerts, which still expects it.
	//
	// The alerts tab label never names the feature; the href's category still scopes it.
	want := []domain.BootstrapNavigationItem{
		{Key: "verify", Label: "Verify", Href: "/verify?module=vaccination&category=vaccination_proof"},
		{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
	}
	if len(got.VisibleNavigation) != len(want) {
		t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, want)
	}
	for i := range want {
		if got.VisibleNavigation[i] != want[i] {
			t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], want[i])
		}
	}
	// >=2 built features -> expanded drawer (Vaccination / Weighing / Counts / You / Sign
	// out), same shape as the CEO app per the binding maintainer ruling.
	if got.NavChrome != domain.NavChromeExpanded {
		t.Fatalf("NavChrome=%q want %q", got.NavChrome, domain.NavChromeExpanded)
	}
	foundAlerts := false
	for _, m := range got.Modules {
		for _, item := range m.NavItems {
			if item.Key == "alerts" {
				foundAlerts = true
			}
		}
	}
	if !foundAlerts {
		t.Fatalf("no module in the drawer carries an Alerts item; modules=%#v", got.Modules)
	}
}

// TestBootstrapSingleFeatureVerifierGetsFeatureScopedAlerts covers the common real-world
// shape: a verifier with exactly one verify duty gets a minimal-chrome bar scoped to
// THAT feature, with a correctly-categorized Alerts item -- never the generic merged
// "verification" module the pre-fix code fell back to for len(grantedModules) <= 1.
//
// MAINTAINER DECISION 2026-08-03: the verifier bar is [Verify, Alerts, You]. "You"
// carries shared_key "you" so it dedupes across modules like the leadership entries --
// the objection was the per-feature REPETITION, not its presence. The alerts tab label
// never names the feature; the href's category still scopes it.
//
// The scoping this test is named for did NOT move to the label -- it lives in the href
// category, and that is what is asserted per feature below: three single-duty verifiers
// get the IDENTICAL tab label "Alerts" but three DIFFERENT categories. Note counts maps
// to "shifting_move", not "counts_proof" (see verificationCategoryForFeature); a wrong
// category renders a permanently-empty 200 tab, so these values must never be relaxed.
func TestBootstrapSingleFeatureVerifierGetsFeatureScopedAlerts(t *testing.T) {
	const wantAlertsLabel = "Alerts"

	cases := []struct {
		feature      string
		wantModule   string
		wantCategory string
	}{
		{feature: "weighing", wantModule: "verify_weighing", wantCategory: "weighing_proof"},
		{feature: "vaccination", wantModule: "verify_vaccination", wantCategory: "vaccination_proof"},
		{feature: "counts", wantModule: "verify_counts", wantCategory: "shifting_move"},
	}

	seenCategories := make(map[string]string, len(cases))
	for _, tc := range cases {
		t.Run(tc.feature, func(t *testing.T) {
			svc := NewService(&fakeRepo{
				profile:        profile("active"),
				grants:         []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
				grantedModules: []string{tc.feature},
			})
			got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
			if err != nil {
				t.Fatalf("Bootstrap() error=%v", err)
			}
			want := []domain.BootstrapNavigationItem{
				{Key: "verify", Label: "Verify", Href: "/verify?module=" + tc.feature + "&category=" + tc.wantCategory},
				{Key: "alerts", Label: wantAlertsLabel, Href: wantVerifierAlertsHref(tc.feature, tc.wantCategory)},
				// You moved to the drawer: the baseline Clock module makes even a
				// single-feature verifier a two-module principal (2026-08-28).
			}
			if len(got.VisibleNavigation) != len(want) {
				t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, want)
			}
			for i := range want {
				if got.VisibleNavigation[i] != want[i] {
					t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], want[i])
				}
			}
			if got.NavChrome != domain.NavChromeExpanded {
				t.Fatalf("NavChrome=%q want %q (verify feature + baseline clock)", got.NavChrome, domain.NavChromeExpanded)
			}
			if len(got.Modules) != 2 || got.Modules[0].Key != tc.wantModule || got.Modules[1].Key != "clock" {
				t.Fatalf("Modules=%#v want [%s clock]", got.Modules, tc.wantModule)
			}
			if prev, dup := seenCategories[tc.wantCategory]; dup {
				t.Fatalf("features %q and %q share alerts category %q -- the generic label must not have collapsed the feature scoping", prev, tc.feature, tc.wantCategory)
			}
			seenCategories[tc.wantCategory] = tc.feature
		})
	}

	if len(seenCategories) != len(cases) {
		t.Fatalf("expected one distinct alerts category per feature; got %v", seenCategories)
	}
}

// TestBootstrapOperatorGetsFixedNav is a regression guard: an operator-role
// principal gets the field execution queue, not the leadership calendar.
func TestBootstrapOperatorGetsFixedNav(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile:        profile("active"),
		grants:         []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
		grantedModules: []string{"vaccination"},
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	wantNav := []domain.BootstrapNavigationItem{
		{Key: "vaccination", Label: "Drive", Href: "/vaccination"},
		// Vaccine-stock capture tab (maintainer decision 2026-09-02): the park's own
		// vaccination operators record the fridge proof, so the operator bar carries Stock.
		{Key: "vaccine_stock", Label: "Stock", Href: "/pc/vaccine-stock"},
		// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). This is a leadership/registry bar, so
		// it legitimately KEEPS its "you" entry -- only the label went generic.
		{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
		// The baseline Clock module (maintainer decision 2026-08-28: everyone
		// clocks in) gives every principal >=2 modules, so chrome is expanded
		// and "You" lives in the drawer footer per the 2026-08-03 placement rule.
	}
	if len(got.VisibleNavigation) != len(wantNav) {
		t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, wantNav)
	}
	for i := range wantNav {
		if got.VisibleNavigation[i] != wantNav[i] {
			t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], wantNav[i])
		}
	}
	if got.NavChrome != domain.NavChromeExpanded {
		t.Fatalf("NavChrome=%q want %q (vaccination + baseline clock)", got.NavChrome, domain.NavChromeExpanded)
	}
}

// TestIsLeadershipPrincipal covers every valid workforce grant role (see
// validRole in service.go): ceo_internal, park_head, pc_director, and
// growth_director are leadership tiers. Verifier owns the standalone
// verification app; operator owns field execution. Neither is leadership
// navigation.
func TestIsLeadershipPrincipal(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{role: permissions.RoleCEOInternal, want: true},
		{role: permissions.RolePCDirector, want: true},
		{role: permissions.RoleGrowthDirector, want: true},
		{role: permissions.RoleParkHead, want: true},
		{role: permissions.RoleVerifier, want: false},
		{role: permissions.RoleOperator, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.role, func(t *testing.T) {
			got := isLeadershipPrincipal([]domain.GrantSummary{grantWithRole(tc.role)})
			if got != tc.want {
				t.Fatalf("isLeadershipPrincipal(role=%q)=%v want %v", tc.role, got, tc.want)
			}
		})
	}
	t.Run("no grants", func(t *testing.T) {
		if isLeadershipPrincipal(nil) {
			t.Fatal("isLeadershipPrincipal(nil) = true, want false")
		}
	})
	t.Run("mixed grants any-leadership wins", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleOperator), grantWithRole(permissions.RoleParkHead)}
		if !isLeadershipPrincipal(grants) {
			t.Fatal("isLeadershipPrincipal with a leadership grant among others = false, want true")
		}
	})
}

// TestVisibleNavigationFor pins that the removed thing is the synthetic
// Leadership module/route, not the CEO's Vaccination module tab. CEO opens
// Vaccination from the drawer and sees Calendar/Alerts/You; operators
// keep the field execution bar.
func TestVisibleNavigationFor(t *testing.T) {
	ceoVaccinationWant := []domain.BootstrapNavigationItem{
		{Key: "calendar", Label: "Calendar", Href: "/calendar"},
		{Key: "videos", Label: "Videos", Href: "/vaccination/videos"},
		// MAINTAINER DECISION 2026-08-06: leadership and verifier are SEPARATE SURFACES on
		// SEPARATE ROUTES. Leadership videos nav points to /vaccination/videos (leadership-owned),
		// NEVER to /verify (verifier-owned). Verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). This is a leadership/registry bar, so
		// it legitimately KEEPS its "you" entry -- only the label went generic.
		{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
		{Key: "you", Label: "You", Href: "/you"},
	}
	tests := []struct {
		name    string
		grants  []domain.GrantSummary
		modules []string
		want    []domain.BootstrapNavigationItem
	}{
		{
			name:    "ceo vaccination module shows calendar alerts you",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)},
			modules: []string{"vaccination", "counts"},
			want:    ceoVaccinationWant,
		},
		{
			name:    "operator",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"vaccination"},
			want: []domain.BootstrapNavigationItem{
				{Key: "vaccination", Label: "Drive", Href: "/vaccination"},
				// Vaccine-stock capture tab (maintainer decision 2026-09-02): operators film
				// the fridge, the PC Director judges — so the operator bar carries Stock.
				{Key: "vaccine_stock", Label: "Stock", Href: "/pc/vaccine-stock"},
				// MAINTAINER DECISION 2026-08-03: the verifier bar is [Verify, Alerts, You].
				// "You" carries shared_key "you" so it dedupes across modules like the
				// leadership entries -- the objection was the per-feature REPETITION, not its
				// presence. The alerts tab label never names the feature; the href's category
				// still scopes it. This registry bar always carried its own "you" entry.
				{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
				{Key: "you", Label: "You", Href: "/you"},
			},
		},
		{
			// No duties named -> scoped to every built feature; the default/active bar
			// is the first (vaccination), with a correctly-categorized Alerts item.
			//
			// MAINTAINER DECISION 2026-08-03: the verifier bar is [Verify, Alerts, You].
			// "You" carries shared_key "you" so it dedupes across modules like the
			// leadership entries -- the objection was the per-feature REPETITION, not
			// its presence. The alerts tab label never names the feature; the href's
			// category still scopes it.
			name:    "verifier with no duties",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			modules: nil,
			want: []domain.BootstrapNavigationItem{
				{Key: "verify", Label: "Verify", Href: "/verify?module=vaccination&category=vaccination_proof"},
				{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
				{Key: "you", Label: "You", Href: "/you"},
			},
		},
		{
			// Same generic "Alerts" label as the vaccination case above, but a
			// different href category (counts maps to shifting_move, NOT counts_proof)
			// -- the label went generic, the SCOPING did not.
			//
			// MAINTAINER DECISION 2026-08-03: the verifier bar is [Verify, Alerts, You].
			// "You" carries shared_key "you" so it dedupes across modules like the
			// leadership entries -- the objection was the per-feature REPETITION, not
			// its presence. The alerts tab label never names the feature; the href's
			// category still scopes it.
			name:    "single-feature verifier",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			modules: []string{"counts"},
			want: []domain.BootstrapNavigationItem{
				{Key: "verify", Label: "Verify", Href: "/verify?module=counts&category=shifting_move"},
				{Key: "alerts", Label: "Alerts", Href: "/verify/alerts?category=shifting_move"},
				{Key: "you", Label: "You", Href: "/you"},
			},
		},
		{
			name: "pc director with verifier permission keeps director vaccination bar",
			grants: []domain.GrantSummary{
				grantWithRole(permissions.RolePCDirector),
				grantWithRole(permissions.RoleVerifier),
			},
			modules: []string{"vaccination"},
			want: []domain.BootstrapNavigationItem{
				{Key: "vaccination", Label: "Stock", Href: "/pc/vaccine-stock"},
				{Key: "videos", Label: "Videos", Href: "/vaccination/videos"},
				// MAINTAINER DECISION 2026-08-06: leadership and verifier are SEPARATE SURFACES.
				// Leadership videos nav points to /vaccination/videos (leadership-owned),
				// NEVER to /verify (verifier-owned). The verifier bar is [Verify, Alerts, You].
				// "You" carries shared_key "you" so it dedupes across modules like the
				// leadership entries -- the objection was the per-feature REPETITION, not its
				// presence. The alerts tab label never names the feature; the href's category
				// still scopes it. This registry bar always carried its own "you" entry.
				{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
				{Key: "you", Label: "You", Href: "/you"},
			},
		},
		{
			// The bar is module-scoped: a two-module operator gets the ACTIVE (lowest
			// priority) module's bar, not a 6-item union of both modules.
			name:    "operator with vaccination+counts gets the active module bar only",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"counts", "vaccination"},
			want: []domain.BootstrapNavigationItem{
				{Key: "vaccination", Label: "Drive", Href: "/vaccination"},
				// Vaccine-stock capture tab (maintainer decision 2026-09-02): operators film
				// the fridge, the PC Director judges — so the operator bar carries Stock.
				{Key: "vaccine_stock", Label: "Stock", Href: "/pc/vaccine-stock"},
				// MAINTAINER DECISION 2026-08-03: the verifier bar is [Verify, Alerts, You].
				// "You" carries shared_key "you" so it dedupes across modules like the
				// leadership entries -- the objection was the per-feature REPETITION, not its
				// presence. The alerts tab label never names the feature; the href's category
				// still scopes it. This registry bar always carried its own "you" entry.
				{Key: "alerts", Label: "Alerts", Href: "/vaccination/alerts"},
				{Key: "you", Label: "You", Href: "/you"},
			},
		},
		{
			// The phone Counts module is capture-only for EVERY role: the tenant-wide census
			// read page was removed from mobile (maintainer decision 2026-07-30) and lives on
			// admin-web. Counts also contributes no Approval tab (moved to admin-web,
			// maintainer decision 2026-07-21), so this bar is exactly the capture tabs.
			name:    "counts operator gets the capture pages; there is no census tab",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"counts"},
			want: []domain.BootstrapNavigationItem{
				{Key: "birth", Label: "Birth", Href: "/counts/birth"},
				{Key: "death", Label: "Death", Href: "/counts/death"},
				{Key: "shifting", Label: "Shifting", Href: "/counts/shifting"},
			},
		},
		{
			// Milk Prep and Milk Feeding moved OUT of Counts into their own drawer module
			// (maintainer decision 2026-07-31). The routes are unchanged; only the grouping
			// moved, so a department granted counts is also granted milk (migration 000064)
			// and the operator still reaches both pages.
			// Colostrum joined the bar on 2026-08-06 (docs/decisions/colostrum-milk-module.md) as the
			// newborn end of the same daily milk round. It carries the SAME CountsWrite grant as its
			// siblings — it renders existing birth-workflow feed tasks under a day-scoped lens, so it
			// widens no authority — and Milk Prep keeps the landing slot (priority 1).
			name:    "milk operator gets the three daily milk pages",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"milk"},
			want: []domain.BootstrapNavigationItem{
				{Key: "milk_preparation", Label: "Milk Prep", Href: "/counts/milk-preparation"},
				{Key: "milk_feeding", Label: "Milk Feeding", Href: "/counts/milk-feeding"},
				{Key: "colostrum", Label: "Colostrum", Href: "/counts/colostrum"},
			},
		},
		{
			// No grants means no nav, not an implicit vaccination default.
			// Baseline clock (2026-08-28, decision D2): a person no module owns
			// still clocks in, so the FLOOR of every bar is My Clock — never
			// blank. Work modules are still earned by a department grant.
			name:    "operator with no module grants gets the baseline clock bar",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: nil,
			want:    []domain.BootstrapNavigationItem{{Key: "clock", Label: "My Clock", Href: "/clock"}},
		},
		{
			name:    "unknown module key contributes nothing beyond the clock floor",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"not_a_real_module"},
			want:    []domain.BootstrapNavigationItem{{Key: "clock", Label: "My Clock", Href: "/clock"}},
		},
		{
			// A "soon" module is a roadmap row, never a servable bar; the
			// baseline clock floor is what renders instead.
			name:    "soon module is not servable",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"breeding"},
			want:    []domain.BootstrapNavigationItem{{Key: "clock", Label: "My Clock", Href: "/clock"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := visibleNavigationFor(tc.grants, tc.modules, "")
			if len(got) != len(tc.want) {
				t.Fatalf("visibleNavigationFor()=%#v want %#v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("item[%d]=%#v want %#v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestNavChromeFor verifies chrome follows the composed drawer for operators and
// leadership alike: any non-verifier principal with >=2 available modules (CEO:
// vaccination + counts; OPERATOR: vaccination + counts + feed) gets the expanded
// drawer; a single-module principal (PC Director, or an operator with one module)
// gets minimal chrome (bottom bar only). Chrome is derived from the composed
// modules, so cases pass the real modulesFor() output.
func TestNavChromeFor(t *testing.T) {
	tests := []struct {
		name           string
		grants         []domain.GrantSummary
		grantedModules []string
		want           string
	}{
		{
			name:   "ceo with vaccination+counts expands",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)},
			want:   domain.NavChromeExpanded,
		},
		{
			// PC leadership is scoped to three preventive-care verticals (vaccination,
			// weighing, health), so it earns the module switcher. Counts/Feed are still
			// filtered out by leadershipModuleKeys regardless of what is granted.
			name:           "pc director with preventive care verticals expands",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RolePCDirector)},
			grantedModules: []string{"vaccination", "weighing", "counts"},
			want:           domain.NavChromeExpanded,
		},
		{
			name:           "park head with preventive care verticals expands",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)},
			grantedModules: []string{"vaccination", "weighing", "counts", "feed_direction"},
			want:           domain.NavChromeExpanded,
		},
		{
			name:           "operator gets the drawer once baseline clock joins",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			grantedModules: []string{"vaccination"},
			want:           domain.NavChromeExpanded,
		},
		{
			// Operator drawer rollout ENABLED (maintainer decision 2026-07-27): an
			// operator with >=2 available modules gets the module-switcher drawer,
			// same as leadership, so they can reach counts/feed alongside vaccination.
			name:           "operator with two modules expands",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			grantedModules: []string{"vaccination", "counts"},
			want:           domain.NavChromeExpanded,
		},
		{
			name:           "operator with vaccination + counts + feed expands",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			grantedModules: []string{"vaccination", "counts", "feed_direction"},
			want:           domain.NavChromeExpanded,
		},
		{
			name:           "operator with vaccination + weighing expands",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			grantedModules: []string{"vaccination", "weighing"},
			want:           domain.NavChromeExpanded,
		},
		{
			// No verify duties/module grant at all -> scoped to every built feature
			// (verifierFeatureKeys fallback), so >=2 modules -> expanded drawer, same as
			// a multi-feature verifier.
			name:   "verifier with no duties expands to every built feature",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			want:   domain.NavChromeExpanded,
		},
		{
			name:           "single-feature verifier gets the drawer once baseline clock joins",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			grantedModules: []string{"vaccination"},
			want:           domain.NavChromeExpanded,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			modules := modulesFor(tc.grants, tc.grantedModules, "")
			if got := navChromeFor(tc.grants, modules); got != tc.want {
				t.Fatalf("navChromeFor()=%q want %q", got, tc.want)
			}
		})
	}
}

// TestBootstrapNavComposition verifies that navigation is composed from granted modules,
// not hardcoded per-role. This test proves:
// 1. Operator with single module (vaccination) gets bottom-bar-only nav (minimal chrome)
// 2. Operator drawer chrome expands once the operator holds >=2 modules (rollout enabled)
// 3. Leadership principals get separate module bars after Overview removal
// 4. Nav items are deduplicated by shared_key
func TestBootstrapNavComposition(t *testing.T) {
	operatorGrants := []domain.GrantSummary{grantWithRole(permissions.RoleOperator)}
	leadershipGrants := []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)}

	t.Run("operator with single work module gets the drawer via baseline clock", func(t *testing.T) {
		chrome := navChromeFor(operatorGrants, modulesFor(operatorGrants, []string{"vaccination"}, ""))
		if chrome != domain.NavChromeExpanded {
			t.Fatalf("navChromeFor(operator)=%q want %q (vaccination + baseline clock)", chrome, domain.NavChromeExpanded)
		}
	})

	t.Run("operator vaccination module gets only vaccination tabs", func(t *testing.T) {
		nav := visibleNavigationFor(operatorGrants, []string{"vaccination"}, "")
		if len(nav) != 4 {
			t.Fatalf("operator nav length=%d want 4 (drives + stock + alerts + you)", len(nav))
		}
		if nav[0].Key != "vaccination" || nav[0].Href != "/vaccination" {
			t.Fatalf("first nav item=%#v want shed-first vaccination root at /vaccination", nav[0])
		}
		// Vaccine-stock capture tab (maintainer decision 2026-09-02): the park's operators
		// record the fridge proof; the PC Director's bar swaps tab #1 to Stock instead.
		if nav[1].Key != "vaccine_stock" || nav[1].Href != "/pc/vaccine-stock" {
			t.Fatalf("second nav item=%#v want the operator Stock tab at /pc/vaccine-stock", nav[1])
		}
		if nav[2].Key != "alerts" || nav[2].Href != "/vaccination/alerts" {
			t.Fatalf("third nav item=%#v want alerts inside the Vaccination module bar", nav[2])
		}
		if nav[3].Key != "you" || nav[3].Href != "/you" {
			t.Fatalf("last nav item=%#v want the composed You tab at /you", nav[3])
		}
		for _, item := range nav {
			if item.Key == "calendar" || item.Href == "/calendar" || item.Key == "weighing" || item.Href == "/weighing" {
				t.Fatalf("operator vaccination nav must not include Calendar or Weighing; got %#v", nav)
			}
		}
	})

	t.Run("operator weighing module gets weighing tabs and expanded sidebar", func(t *testing.T) {
		modules := modulesFor(operatorGrants, []string{"vaccination", "weighing"}, "")
		if chrome := navChromeFor(operatorGrants, modules); chrome != domain.NavChromeExpanded {
			t.Fatalf("navChromeFor(operator vaccination+weighing)=%q want expanded", chrome)
		}
		var weighing *domain.BootstrapModule
		for i := range modules {
			if modules[i].Key == "weighing" {
				weighing = &modules[i]
			}
		}
		if weighing == nil {
			t.Fatalf("operator must receive separate weighing module; modules=%#v", modules)
		}
		// An operator executes and nothing else: one work list, no planner list, no
		// oversight -- plus weighing's OWN alerts feed.
		want := []domain.BootstrapNavigationItem{
			{Key: "weighing", Label: "My work", Href: "/weighing"},
			// /weighing/alerts, NOT the vaccination process-integrity feed at /alerts. The
			// old cross-module item was removed because a weighing operator holds no
			// vaccination permission and it 403'd on open. This one is gated on weighing
			// capabilities and has real rows behind it (the weighing lifecycle
			// notifications already routed to this operator). The label is just "Alerts":
			// the tab never names the feature, the href carries the scoping.
			//
			// It also ends the degenerate single-tab bar this operator used to get -- a
			// switcher with nothing to switch to.
			{Key: "weighing_alerts", Label: "Alerts", Href: "/weighing/alerts"},
			{Key: "you", Label: "You", Href: "/you"},
		}
		if len(weighing.NavItems) != len(want) {
			t.Fatalf("weighing nav=%#v want %#v", weighing.NavItems, want)
		}
		for i := range want {
			if weighing.NavItems[i] != want[i] {
				t.Fatalf("weighing nav[%d]=%#v want %#v", i, weighing.NavItems[i], want[i])
			}
		}
	})

	// The three weighing surfaces are separate destinations, so the bar each principal gets is
	// decided by the capabilities they hold -- never by a per-role nav template.
	t.Run("weighing surfaces are composed per capability", func(t *testing.T) {
		weighingModule := func(grants []domain.GrantSummary) domain.BootstrapModule {
			t.Helper()
			for _, m := range modulesFor(grants, []string{"vaccination", "weighing"}, "") {
				if m.Key == "weighing" {
					return m
				}
			}
			t.Fatalf("no weighing module for grants=%#v", grants)
			return domain.BootstrapModule{}
		}
		hrefs := func(m domain.BootstrapModule) []string {
			out := make([]string, 0, len(m.NavItems))
			for _, item := range m.NavItems {
				out = append(out, item.Href)
			}
			return out
		}
		contains := func(list []string, want string) bool {
			for _, got := range list {
				if got == want {
					return true
				}
			}
			return false
		}

		// The CEO plans. He must not get an executable work list, and because "My work" is gated
		// away his landing falls through to the flat all-tasks list rather than an empty page.
		ceo := weighingModule([]domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)})
		if ceo.Href != "/weighing/tasks" {
			t.Fatalf("ceo weighing landing=%q want the flat all-tasks list", ceo.Href)
		}
		if got := hrefs(ceo); contains(got, "/weighing") || contains(got, "/weighing/operators") {
			t.Fatalf("ceo weighing nav=%v must not offer a work list or the operators surface", got)
		}
		if !contains(hrefs(ceo), "/weighing/tasks") {
			t.Fatalf("ceo weighing nav=%v want the planner list", hrefs(ceo))
		}
		// Maintainer ruling 2026-08-28: Videos, Weights and Growth are retired from the mobile
		// weighing bar for EVERY principal. The CEO/planner bar is Tasks + Alerts (+You); the
		// Weights/Growth read-outs stay admin-web-only and evidence review lives in the
		// verifier/leadership surfaces.
		for _, retired := range []string{"/weighing/videos", "/weighing/weights", "/weighing/growth"} {
			if contains(hrefs(ceo), retired) {
				t.Fatalf("ceo weighing nav=%v must not offer the retired mobile page %q", hrefs(ceo), retired)
			}
		}

		// The growth director executes his own sheds and oversees other people's, but does not plan.
		director := weighingModule([]domain.GrantSummary{grantWithRole(permissions.RoleGrowthDirector)})
		if director.Href != "/weighing" {
			t.Fatalf("growth director weighing landing=%q want his own work list", director.Href)
		}
		got := hrefs(director)
		if !contains(got, "/weighing") || !contains(got, "/weighing/operators") {
			t.Fatalf("growth director weighing nav=%v want both his work list and the operators surface", got)
		}
		if contains(got, "/weighing/tasks") {
			t.Fatalf("growth director weighing nav=%v must not offer the planner list", got)
		}
		// Same 2026-08-28 retirement for the Growth Director: My work / Operators / Alerts only.
		for _, retired := range []string{"/weighing/videos", "/weighing/weights", "/weighing/growth"} {
			if contains(got, retired) {
				t.Fatalf("growth director weighing nav=%v must not offer the retired mobile page %q", got, retired)
			}
		}
	})

	t.Run("preventive care leader keeps field vaccination bar without weighing tab", func(t *testing.T) {
		nav := visibleNavigationFor(leadershipGrants, nil, "")
		if len(nav) != 4 {
			t.Fatalf("pc leader nav length=%d want 4 (calendar + videos + alerts + you)", len(nav))
		}
		if nav[0].Key != "calendar" || nav[1].Key != "videos" || nav[2].Key != "alerts" || nav[3].Key != "you" {
			t.Fatalf("pc leader nav should have calendar, videos, alerts, you; got %v", []string{nav[0].Key, nav[1].Key, nav[2].Key, nav[3].Key})
		}
	})

	t.Run("pc director gets the preventive care verticals", func(t *testing.T) {
		directorGrants := []domain.GrantSummary{grantWithRole(permissions.RolePCDirector)}
		modules := modulesFor(directorGrants, nil, "")
		if chrome := navChromeFor(directorGrants, modules); chrome != domain.NavChromeExpanded {
			t.Fatalf("pc director chrome=%q want expanded", chrome)
		}
		keys := moduleKeySet(modules)
		for _, want := range []string{"vaccination", "weighing", "aas_health"} {
			if keys[want] != moduleStatusAvailable {
				t.Fatalf("pc director must get %s module; got %v", want, keys)
			}
		}
		// Counts and Feed are not preventive-care surfaces and stay out, which is the half of
		// the rule the union merge did NOT relax.
		for _, unwanted := range []string{"counts", "feed_direction"} {
			if _, ok := keys[unwanted]; ok {
				t.Fatalf("pc director must not get %s module; got %v", unwanted, keys)
			}
		}
	})

	t.Run("growth director gets weighing module only", func(t *testing.T) {
		directorGrants := []domain.GrantSummary{grantWithRole(permissions.RoleGrowthDirector)}
		modules := modulesFor(directorGrants, nil, "")
		if chrome := navChromeFor(directorGrants, modules); chrome != domain.NavChromeExpanded {
			t.Fatalf("growth director chrome=%q want expanded (weighing + baseline clock)", chrome)
		}
		keys := moduleKeySet(modules)
		if keys["weighing"] != moduleStatusAvailable {
			t.Fatalf("growth director must get weighing module; got %v", keys)
		}
		if _, ok := keys["vaccination"]; ok {
			t.Fatalf("growth director must not get vaccination module; got %v", keys)
		}
	})

	t.Run("ceo (multi-module leader) gets expanded nav chrome", func(t *testing.T) {
		ceoGrants := []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)}
		chrome := navChromeFor(ceoGrants, modulesFor(ceoGrants, nil, ""))
		if chrome != domain.NavChromeExpanded {
			t.Fatalf("navChromeFor(ceo)=%q want %q", chrome, domain.NavChromeExpanded)
		}
	})

	t.Run("park head keeps preventive care verticals even when extra modules are granted", func(t *testing.T) {
		modules := modulesFor(leadershipGrants, []string{"vaccination", "weighing", "counts", "feed_direction"}, "")
		if chrome := navChromeFor(leadershipGrants, modules); chrome != domain.NavChromeExpanded {
			t.Fatalf("navChromeFor(park_head)=%q want %q", chrome, domain.NavChromeExpanded)
		}
		// The grant list offers Counts and Feed; leadershipModuleKeys still filters them out.
		keys := moduleKeySet(modules)
		for _, unwanted := range []string{"counts", "feed_direction"} {
			if _, ok := keys[unwanted]; ok {
				t.Fatalf("park head must not get %s module; got %v", unwanted, keys)
			}
		}
	})
}

func assertAppCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error=%v, want app error", err)
	}
	if appErr.Code != code {
		t.Fatalf("code=%q want %q", appErr.Code, code)
	}
}

func profile(status string) domain.OperatorProfile {
	return domain.OperatorProfile{
		OperatorID:      testOperator,
		UserID:          stringPtr(testActor),
		DisplayCode:     "OP-001",
		DisplayName:     "Synthetic Operator",
		Status:          status,
		PrimaryRoleHint: "operator",
		RowVersion:      1,
	}
}

func grant() domain.GrantSummary {
	return domain.GrantSummary{
		GrantID:   "93000000-0000-4000-8000-000000000001",
		UserID:    testActor,
		Role:      "operator",
		ScopeType: "tenant",
		ScopeID:   testTenant,
		Status:    "active",
	}
}

// grantWithRole builds an active tenant-scoped grant with the given role,
// for exercising isLeadershipPrincipal/visibleNavigationFor/navChromeFor
// across every workforce grant role.
func grantWithRole(role string) domain.GrantSummary {
	g := grant()
	g.Role = role
	return g
}

func device(status string) domain.DeviceSummary {
	return domain.DeviceSummary{
		DeviceID:     testDevice,
		OperatorID:   testOperator,
		Platform:     "android",
		AppInstallID: "install-1",
		AppVersion:   "0.1.0",
		Status:       status,
		RowVersion:   1,
	}
}

func stringPtr(value string) *string {
	return &value
}

type fakeRepo struct {
	profile        domain.OperatorProfile
	profileErr     error
	grants         []domain.GrantSummary
	caps           []domain.CapabilityAssignment
	grantedModules []string
	// personMobileModules are the phone modules the person is TICKED for. Empty means no
	// stored rows, and the service then falls back to grantedModules above.
	personMobileModules []string
	// personAssignments are the person's stored access rows. The nav filter resolves what
	// they may do from these instead of from the retired role map.
	personAssignments []permissions.ModuleAssignment
	device            domain.DeviceSummary
	deviceErr         error

	// registerDeviceResult/registerDeviceErr let tests control what the reactivation self-heal
	// path (reactivateRecoverableDevice -> repo.RegisterDevice) observes. registerDeviceCalls
	// records every invocation so tests can assert the self-heal path was (or was not) taken, and
	// with which tenant/actor -- reactivation must always be scoped to the authenticated caller.
	registerDeviceResult domain.DeviceSummary
	registerDeviceErr    error
	registerDeviceCalls  []ports.RegisterDeviceCommand
}

func (f *fakeRepo) GetMemberForActor(context.Context, string, string) (domain.OperatorProfile, error) {
	if f.profileErr != nil {
		return domain.OperatorProfile{}, f.profileErr
	}
	return f.profile, nil
}

func (f *fakeRepo) ListActiveGrantsForActor(context.Context, string, string) ([]domain.GrantSummary, error) {
	return f.grants, nil
}

func (f *fakeRepo) ListCapabilities(context.Context, string, string) ([]domain.CapabilityAssignment, error) {
	return f.caps, nil
}

func (f *fakeRepo) ListPersonAssignments(context.Context, string, string) ([]permissions.ModuleAssignment, error) {
	return f.personAssignments, nil
}

func (f *fakeRepo) ListGrantedModuleKeys(context.Context, string, string) ([]string, error) {
	return f.grantedModules, nil
}

func (f *fakeRepo) GetDeviceForActor(context.Context, string, string, string) (domain.DeviceSummary, error) {
	if f.deviceErr != nil {
		return domain.DeviceSummary{}, f.deviceErr
	}
	return f.device, nil
}

func (f *fakeRepo) ListOperators(context.Context, ports.ListOperatorsParams) ([]domain.OperatorProfile, error) {
	return nil, ports.ErrNotFound
}
func (f *fakeRepo) CreateOperator(context.Context, ports.CreateOperatorCommand) (domain.OperatorProfile, error) {
	return domain.OperatorProfile{}, ports.ErrNotFound
}
func (f *fakeRepo) GetOperator(context.Context, string, string) (domain.OperatorProfile, error) {
	return domain.OperatorProfile{}, ports.ErrNotFound
}
func (f *fakeRepo) UpdateOperator(context.Context, ports.UpdateOperatorCommand) (domain.OperatorProfile, error) {
	return domain.OperatorProfile{}, ports.ErrNotFound
}
func (f *fakeRepo) SetOperatorStatus(context.Context, ports.StatusCommand) (domain.OperatorProfile, error) {
	return domain.OperatorProfile{}, ports.ErrNotFound
}
func (f *fakeRepo) ListGrants(context.Context, string, string) ([]domain.GrantSummary, error) {
	return nil, ports.ErrNotFound
}
func (f *fakeRepo) CreateGrant(context.Context, ports.CreateGrantCommand) (domain.GrantSummary, error) {
	return domain.GrantSummary{}, ports.ErrNotFound
}
func (f *fakeRepo) AssignCapability(context.Context, ports.CapabilityCommand) (domain.CapabilityAssignment, error) {
	return domain.CapabilityAssignment{}, ports.ErrNotFound
}
func (f *fakeRepo) RemoveCapability(context.Context, ports.RemoveCapabilityCommand) error {
	return ports.ErrNotFound
}
func (f *fakeRepo) ListDevices(context.Context, string, string) ([]domain.DeviceSummary, error) {
	return nil, ports.ErrNotFound
}
func (f *fakeRepo) RevokeDevice(context.Context, ports.RevokeDeviceCommand) (domain.DeviceSummary, error) {
	return domain.DeviceSummary{}, ports.ErrNotFound
}
func (f *fakeRepo) ListSourceCandidates(context.Context, ports.ListSourceCandidatesParams) ([]domain.SourceCandidate, error) {
	return nil, ports.ErrNotFound
}
func (f *fakeRepo) MapSourceCandidate(context.Context, ports.MapSourceCandidateCommand) (domain.SourceCandidate, error) {
	return domain.SourceCandidate{}, ports.ErrNotFound
}
func (f *fakeRepo) RejectSourceCandidate(context.Context, ports.RejectSourceCandidateCommand) (domain.SourceCandidate, error) {
	return domain.SourceCandidate{}, ports.ErrNotFound
}
func (f *fakeRepo) RegisterDevice(_ context.Context, cmd ports.RegisterDeviceCommand) (domain.DeviceSummary, error) {
	f.registerDeviceCalls = append(f.registerDeviceCalls, cmd)
	if f.registerDeviceErr != nil {
		return domain.DeviceSummary{}, f.registerDeviceErr
	}
	if f.registerDeviceResult.DeviceID != "" || f.registerDeviceResult.Status != "" {
		return f.registerDeviceResult, nil
	}
	return f.device, nil
}
func (f *fakeRepo) HeartbeatDevice(context.Context, ports.HeartbeatDeviceCommand) (domain.DeviceSummary, error) {
	return f.device, nil
}
func (f *fakeRepo) DeregisterDevice(context.Context, ports.DeregisterDeviceCommand) (domain.DeviceSummary, error) {
	return f.device, nil
}

// TestCountsModuleRoleMatrix pins the maintainer-approved Counts access matrix
// (2026-07-18; the mobile Approval TAB was removed from Counts 2026-07-21 and has not returned —
// approvals came back to the phone on 2026-08-05 as their OWN module, never as a Counts tab;
// preventive-care leaders excluded from the Counts drawer 2026-07-24).
// The Counts module exposes capture pages on the phone; who sees which is decided by permission
// (counts.read / counts.write), never by a per-role nav template.
//
// The mobile Counts bar carries capture tabs only: EVERY role that holds the module gets the
// same [birth, death, shifting, milk prep, milk feeding] bar. The census read page was removed
// from mobile (maintainer decision 2026-07-30) and approvals ride their own module. Permanent RFID
// assignment is the final action inside each kid's Birth workflow, not a separate Counts page.
// Birth and Death were recombined into one work-list tab
// (docs/decisions/birth-death-workflows.md).
//
// Whether the Counts MODULE appears in the drawer at all is decided upstream by the
// drawer matrix: operators get it via their department grant, CEO gets it via the
// leadership tier, and preventive-care leaders (PC Director, Park Head) do NOT —
// their drawer is the vaccination home only (maintainer decision 2026-07-24).
//
//	role          | census | birth/death | shifting | approval | module in drawer
//	operator      |   -    |      x      |    x     |    -     | yes
//	park_head     |   -    |      -      |    -     |    -     | NO  (preventive-care leader)
//	ceo_internal  |   -    |      x      |    x     |    -     | yes
//	pc_director   |   -    |      -      |    -     |    -     | NO  (preventive-care leader)
//	verifier      | review |    review   |  review  |    -     | evidence lens
func TestCountsModuleRoleMatrix(t *testing.T) {
	tests := []struct {
		role      string
		wantItems []string // nav item keys inside the counts module, nil => module absent
	}{
		{permissions.RoleOperator, []string{"birth", "death", "shifting"}},
		{permissions.RoleParkHead, nil},
		{permissions.RoleCEOInternal, []string{"birth", "death", "shifting"}},
		{permissions.RolePCDirector, nil},
		// A standalone verifier does NOT get registry modules at all: modulesFor composes
		// per-feature verification modules ("verify_counts", "verify_feed_direction", ...),
		// each with its own [Verify, Alerts, You] bar. Nothing keyed "counts"/"feed_direction".
		{permissions.RoleVerifier, nil},
	}
	for _, tc := range tests {
		t.Run(tc.role, func(t *testing.T) {
			grants := []domain.GrantSummary{grantWithRole(tc.role)}
			modules := modulesFor(grants, []string{"vaccination", "counts"}, "")

			var counts *domain.BootstrapModule
			for i := range modules {
				if modules[i].Key == "counts" {
					counts = &modules[i]
				}
			}
			if tc.wantItems == nil {
				if counts != nil {
					t.Fatalf("%s must NOT see the counts module; got items %#v", tc.role, counts.NavItems)
				}
				return
			}
			if counts == nil {
				t.Fatalf("%s must see the counts module; modules=%#v", tc.role, modules)
			}
			got := make([]string, 0, len(counts.NavItems))
			for _, item := range counts.NavItems {
				got = append(got, item.Key)
			}
			if len(got) != len(tc.wantItems) {
				t.Fatalf("counts nav items=%v want %v", got, tc.wantItems)
			}
			for i := range tc.wantItems {
				if got[i] != tc.wantItems[i] {
					t.Fatalf("counts nav items=%v want %v", got, tc.wantItems)
				}
			}
			// Landing must be a page this principal can actually open — never a
			// gated-away route that would 403 on arrival.
			if !navItemsContainHref(counts.NavItems, counts.Href) {
				t.Fatalf("%s counts landing href=%q is not among its permitted items %v", tc.role, counts.Href, got)
			}
		})
	}
}

// TestFeedModuleRoleMatrix pins who sees the built mobile Feed module and which tabs
// each job may execute. The backend composes these items from permissions.
func TestFeedModuleRoleMatrix(t *testing.T) {
	tests := []struct {
		role      string
		wantItems []string // nil => module absent
	}{
		{permissions.RoleCEOInternal, []string{"feed_direction", "feed_packing", "feed_transport", "feed_wastage"}},
		{permissions.RoleParkHead, nil},
		{permissions.RoleKey(permissions.TierDirector, permissions.VerticalFeed), []string{"feed_direction"}},
		{permissions.RoleKey(permissions.TierHead, permissions.VerticalFeed), []string{"feed_direction"}},
		{permissions.RoleKey(permissions.TierManager, permissions.VerticalFeed), nil},
		{permissions.RoleOperator, []string{"feed_direction", "feed_packing", "feed_transport", "feed_wastage"}},
		// A standalone verifier does NOT get registry modules at all: modulesFor composes
		// per-feature verification modules ("verify_counts", "verify_feed_direction", ...),
		// each with its own [Verify, Alerts, You] bar. Nothing keyed "counts"/"feed_direction".
		{permissions.RoleVerifier, nil},
	}
	for _, tc := range tests {
		t.Run(tc.role, func(t *testing.T) {
			grants := []domain.GrantSummary{grantWithRole(tc.role)}
			modules := modulesFor(grants, []string{"vaccination", "feed_direction"}, "")

			var feed *domain.BootstrapModule
			for i := range modules {
				if modules[i].Key == "feed_direction" {
					feed = &modules[i]
				}
			}
			if tc.wantItems == nil {
				if feed != nil {
					t.Fatalf("%s must NOT see the feed module; got items %#v", tc.role, feed.NavItems)
				}
				return
			}
			if feed == nil {
				t.Fatalf("%s must see the feed module; modules=%#v", tc.role, modules)
			}
			if feed.Status != moduleStatusAvailable {
				t.Fatalf("%s feed status=%q want available", tc.role, feed.Status)
			}
			got := make([]string, 0, len(feed.NavItems))
			for _, item := range feed.NavItems {
				got = append(got, item.Key)
			}
			if len(got) != len(tc.wantItems) {
				t.Fatalf("feed nav items=%v want %v", got, tc.wantItems)
			}
			for i := range tc.wantItems {
				if got[i] != tc.wantItems[i] {
					t.Fatalf("feed nav items=%v want %v", got, tc.wantItems)
				}
			}
			if !navItemsContainHref(feed.NavItems, feed.Href) {
				t.Fatalf("%s feed landing href=%q is not among its permitted items %v", tc.role, feed.Href, got)
			}
		})
	}
}

// TestCountsModuleBarIsCaptureOnlyAndOmitsYouTab pins that the mobile Counts module contributes
// only capture/decision tabs and no trailing global action tab. Counts never
// contributes the global "You" tab that vaccination and leadership do.
//
// This is the backend authority for how many tabs an operator sees: the Android shell renders the
// composed bar verbatim. If "you" leaked in, Counts would grow a tab it does not own.
func TestCountsModuleBarIsCaptureOnlyAndOmitsYouTab(t *testing.T) {
	countsBar := func(role string) []string {
		grants := []domain.GrantSummary{grantWithRole(role)}
		items := composeNavigationFromModules([]string{"counts"}, grants, "")
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item.Key)
		}
		return out
	}
	equal := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// The headline requirement: an operator's Counts bar is the capture tabs (no approval/census).
	if got := countsBar(permissions.RoleOperator); !equal(got, []string{"birth", "death", "shifting"}) {
		t.Fatalf("operator counts bar=%v want exactly [birth death shifting]", got)
	}

	// A park head no longer gets an approval tab — its Counts bar is the same capture tabs.
	if got := countsBar(permissions.RoleParkHead); !equal(got, []string{"birth", "death", "shifting"}) {
		t.Fatalf("park_head counts bar=%v want [birth death shifting] (no approval on mobile)", got)
	}

	// No role gets a global "You" tab from Counts on mobile.
	for _, role := range []string{
		permissions.RoleOperator, permissions.RoleParkHead,
		permissions.RoleCEOInternal, permissions.RoleCEOInternal,
	} {
		for _, key := range countsBar(role) {
			if key == "you" {
				t.Fatalf("%s counts bar contains a %q tab; Counts is capture-only on mobile", role, key)
			}
		}
	}

	// ...while Vaccination still emits it, so the profile surface stays reachable.
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)}
	items := composeNavigationFromModules([]string{"vaccination"}, grants, "")
	found := false
	for _, item := range items {
		if item.Key == "you" && item.Href == "/you" {
			found = true
		}
	}
	if !found {
		t.Fatalf("vaccination must contribute the You tab at /you; got %#v", items)
	}
}

// TestVisibleNavigationIsEarnedByAModuleGrant pins the nav-composition contract behind P1-NAV: nav
// is EARNED by holding a department module grant. A non-leadership operator whose department has a
// module composes that module's bottom bar; a department with NO module composes an EMPTY bar -- and
// that empty bar is CORRECT, not a bug (a department that does no operational work should not get a
// phantom module invented for it). The actual P1-NAV bug was a CRASH (a missing
// department_module_grants table), fixed by migration 000008, not a blank-bar bug to be papered over
// with universal grants.
func TestVisibleNavigationIsEarnedByAModuleGrant(t *testing.T) {
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleOperator)}

	// A module-less department composes ONLY the baseline clock bar (2026-08-28,
	// decision D2: everyone clocks in). No WORK module is invented for it — the
	// P1-NAV rule stands for operational modules; attendance is the one floor.
	nav0 := visibleNavigationFor(grants, nil, "en")
	if len(nav0) != 1 || nav0[0].Key != "clock" {
		t.Fatalf("nav with no granted module=%v, want exactly the baseline clock bar", nav0)
	}

	// A department that DOES hold a module (here Counts) composes that module's non-empty bottom bar.
	nav := visibleNavigationFor(grants, []string{"counts"}, "en")
	if len(nav) == 0 {
		t.Fatal("nav for an operator whose department holds the Counts module is empty -- a granted module must render its bar")
	}
}

// TestWeighingAlertsTabReachesEveryWeighingSeat answers the maintainer's 2026-08-03
// observation directly: "there is no alerts surface anywhere -- not for CEO, not
// director, not operator."
//
// The weighing alerts item carries NO requiredPermission on purpose. Everyone with a
// weighing job has a stake in the module's lifecycle feed, and the three seats hold
// three DIFFERENT capability sets -- the operator holds execute, the CEO holds
// plan+monitor but not execute, and the Growth Director intentionally holds both
// execute and monitor. Gating the tab on any single weighing capability would drop it
// from at least one of them; the endpoint behind it is the OR gate that does the real
// authorization, and the rows a caller sees are already scoped to them by the routing
// that produced them.
func TestWeighingAlertsTabReachesEveryWeighingSeat(t *testing.T) {
	for _, tc := range []struct {
		name string
		role string
	}{
		{"operator", permissions.RoleOperator},
		{"growth director", permissions.RoleGrowthDirector},
		{"CEO", permissions.RoleCEOInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grants := []domain.GrantSummary{grantWithRole(tc.role)}
			modules := modulesFor(grants, []string{"weighing"}, "")

			var weighing *domain.BootstrapModule
			for i := range modules {
				if modules[i].Key == "weighing" {
					weighing = &modules[i]
				}
			}
			if weighing == nil {
				t.Fatalf("%s received no weighing module at all; modules=%#v", tc.name, modules)
			}

			var alerts *domain.BootstrapNavigationItem
			for i := range weighing.NavItems {
				if weighing.NavItems[i].Key == "weighing_alerts" {
					alerts = &weighing.NavItems[i]
				}
			}
			if alerts == nil {
				t.Fatalf("%s has NO weighing alerts tab; nav=%#v", tc.name, weighing.NavItems)
			}
			// The tab never names the feature -- the href carries the scoping.
			if alerts.Label != "Alerts" {
				t.Fatalf("%s alerts label = %q, want exactly \"Alerts\"", tc.name, alerts.Label)
			}
			// It must be WEIGHING's feed, never the vaccination process-integrity feed.
			if alerts.Href != "/weighing/alerts" {
				t.Fatalf("%s alerts href = %q, want /weighing/alerts (never /alerts, the vaccination feed)", tc.name, alerts.Href)
			}
			// A module bar must have something to switch between; a single tab is a
			// switcher with nothing to switch to.
			if len(weighing.NavItems) < 2 {
				t.Fatalf("%s weighing bar has %d tab(s): %#v", tc.name, len(weighing.NavItems), weighing.NavItems)
			}
		})
	}
}

// The live Feed Director must see every page of the module they own (maintainer decision
// 2026-08-05). This role was absent from TestFeedModuleRoleMatrix above, which is why the
// gap survived: the matrix pinned the CEO, the operator, the park head and the DORMANT
// org-role scaffolding (director_feed/head_feed), but never the flat feed_director that
// AGENTS.md lists as the live persona.
func TestFeedDirectorSeesEveryFeedPage(t *testing.T) {
	grants := []domain.GrantSummary{grantWithRole(permissions.RoleFeedDirector)}
	modules := modulesFor(grants, []string{"feed_direction"}, "")

	var feed *domain.BootstrapModule
	for i := range modules {
		if modules[i].Key == "feed_direction" {
			feed = &modules[i]
		}
	}
	if feed == nil {
		t.Fatalf("feed_director must see the feed module; modules=%#v", modules)
	}
	got := make([]string, 0, len(feed.NavItems))
	for _, item := range feed.NavItems {
		got = append(got, item.Key)
	}
	want := []string{"feed_direction", "feed_packing", "feed_transport", "feed_wastage"}
	if len(got) != len(want) {
		t.Fatalf("feed_director feed tabs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("feed_director feed tabs = %v, want %v", got, want)
		}
	}
}

// A nav gate that disagrees with the route behind it is a defect in whichever direction it
// leans: too strict hides a page the principal may open, too loose renders a tab that 403s
// on arrival. Both shipped here at once -- feed_director held feed_direction.read and was
// hidden from the dispatch sheet, while director_feed held only the vaccination
// protocol.read and saw a tab it could not open.
//
// Asserting the gate EQUALS the backing route's permission is what keeps the two in step;
// a role-by-role expectation only pins the roles someone remembered to list.
func TestFeedNavGatesEqualTheirBackingRoutePermissions(t *testing.T) {
	backing := map[string]struct {
		method string
		path   string
	}{
		"feed_direction": {"GET", "/feed-direction/preview"},
		"feed_packing":   {"GET", "/feed-packing/worklist"},
		"feed_transport": {"GET", "/feed-transport/tasks"},
		"feed_wastage":   {"GET", "/feed-wastage/worklist"},
	}
	for _, item := range moduleNavRegistry["feed_direction"].contributions {
		route, ok := backing[item.key]
		if !ok {
			t.Fatalf("feed nav item %q has no declared backing route in this test; add one", item.key)
		}
		matched, found := permissions.Match(route.method, route.path)
		if !found {
			t.Fatalf("%s %s is not a registered route", route.method, route.path)
		}
		if len(matched.Permissions) != 1 || matched.Permissions[0] != item.requiredPermission {
			t.Errorf("feed nav item %q gates on %q but its route %s %s requires %v",
				item.key, item.requiredPermission, route.method, route.path, matched.Permissions)
		}
	}
}

// TestApprovalsModuleIsPerPersonAndLeavesCountsCaptureOnly pins the mobile Approvals module
// (maintainer decision 2026-08-05, SUPERSEDING the 2026-07-21 "approvals live on admin-web only"
// decision).
//
// The decision has two halves and this test exists because the SECOND half is the one a later
// author will break by accident:
//
//  1. Approvals is back on the phone, as its OWN module.
//  2. It is granted PER PERSON, via permissions.RoleCountsApprover held ALONGSIDE a job role --
//     never by widening pc_director / growth_director themselves. The whole point is that a
//     FUTURE PC Director inherits no approval authority by holding the job.
//
// So the assertions below are deliberately paired: the same job role must see the module WITH the
// per-person grant and must NOT see it without one. A change that "simplifies" this by moving
// counts.approve_* onto pc_director/growth_director passes half of this test and fails the other
// half, which is exactly the signal wanted.
func TestApprovalsModuleIsPerPersonAndLeavesCountsCaptureOnly(t *testing.T) {
	moduleKeys := func(grants []domain.GrantSummary) []string {
		modules := modulesFor(grants, []string{"vaccination", "counts", "weighing"}, "")
		out := make([]string, 0, len(modules))
		for _, m := range modules {
			out = append(out, m.Key)
		}
		return out
	}
	has := func(keys []string, want string) bool {
		for _, k := range keys {
			if k == want {
				return true
			}
		}
		return false
	}

	// The job role ALONE never carries approvals. This is the half that keeps the
	// one-module-one-director segregation lock intact for every future holder of these jobs.
	for _, role := range []string{permissions.RolePCDirector, permissions.RoleGrowthDirector} {
		grants := []domain.GrantSummary{grantWithRole(role)}
		if keys := moduleKeys(grants); has(keys, "approvals") {
			t.Fatalf("%s alone must NOT see the approvals module (per-person grant required); got %v", role, keys)
		}
	}

	// The SAME job role plus the per-person authority grant does carry it. Two grant rows on one
	// principal is the real shape: permissions OR across every role the caller holds.
	for _, role := range []string{permissions.RolePCDirector, permissions.RoleGrowthDirector} {
		grants := []domain.GrantSummary{
			grantWithRole(role),
			grantWithRole(permissions.RoleCountsApprover),
		}
		keys := moduleKeys(grants)
		if !has(keys, "approvals") {
			t.Fatalf("%s + counts_approver must see the approvals module; got %v", role, keys)
		}
		// The module must land on a page this principal can actually open, and carry exactly the
		// one queue tab -- no "you", which belongs in the drawer for a 2+-module principal.
		var approvals *domain.BootstrapModule
		modules := modulesFor(grants, []string{"vaccination", "counts", "weighing"}, "")
		for i := range modules {
			if modules[i].Key == "approvals" {
				approvals = &modules[i]
			}
		}
		if approvals == nil {
			t.Fatalf("%s + counts_approver: approvals module missing from %v", role, keys)
		}
		if len(approvals.NavItems) != 1 || approvals.NavItems[0].Key != "approvals" {
			t.Fatalf("%s + counts_approver approvals bar=%#v want exactly one 'approvals' item", role, approvals.NavItems)
		}
		if !navItemsContainHref(approvals.NavItems, approvals.Href) {
			t.Fatalf("%s + counts_approver approvals landing href=%q is not among its items", role, approvals.Href)
		}
	}

	// The CEO tier reaches approvals through ceo_internal's own counts.approve_access, with no
	// extra grant. This is the case the permission-keyed offer exists to cover: leadershipModuleKeys
	// used to return early for ceo_internal, which would have hidden the module from the one
	// principal who most obviously owns it.
	ceo := []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)}
	if keys := moduleKeys(ceo); !has(keys, "approvals") {
		t.Fatalf("ceo_internal must see the approvals module without a per-person grant; got %v", keys)
	}

	// Half 2 of the decision: Counts does NOT regain an approval tab. Approving is not capturing.
	// An approver holding no CountsWrite gets the queue and no capture tabs; the operator keeps
	// capture tabs and no queue.
	countsBar := func(grants []domain.GrantSummary) []string {
		items := composeNavigationFromModules([]string{"counts"}, grants, "")
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item.Key)
		}
		return out
	}
	approverOnly := []domain.GrantSummary{
		grantWithRole(permissions.RolePCDirector),
		grantWithRole(permissions.RoleCountsApprover),
	}
	for _, key := range countsBar(approverOnly) {
		if key == "approval" || key == "approvals" {
			t.Fatalf("counts bar regained an approval tab (%q); approvals is its own module", key)
		}
	}
	if got := countsBar([]domain.GrantSummary{grantWithRole(permissions.RoleOperator)}); len(got) != 3 {
		t.Fatalf("operator counts bar=%v want the 3 capture tabs, unchanged by the approvals module", got)
	}
}

// TestCountsApproverRoleCarriesOnlyApprovalAuthority pins that the per-person role is exactly three
// permissions and confers no way in on its own.
//
// The role is handed to named individuals, so every permission added to it silently widens what
// that hand-off carries for everyone already holding it. In particular it must NOT carry
// AppBootstrap/AdminWebBootstrap: a grant of this role alone has to be inert, so the authority can
// only ever ride on a principal who already has a job and a way to log in.
func TestCountsApproverRoleCarriesOnlyApprovalAuthority(t *testing.T) {
	want := []string{
		permissions.CountsApproveAccess,
		permissions.CountsApproveLifecycle,
		permissions.CountsApproveShifting,
	}
	for _, p := range want {
		if !permissions.RoleHasPermission(permissions.RoleCountsApprover, p) {
			t.Errorf("counts_approver must hold %q", p)
		}
	}
	for _, p := range []string{
		permissions.AppBootstrap, permissions.AdminWebBootstrap,
		permissions.CountsRead, permissions.CountsWrite,
		permissions.GoatRead, permissions.TaskRead, permissions.CalendarRead,
		permissions.VerificationReview, permissions.VerificationAct,
	} {
		if permissions.RoleHasPermission(permissions.RoleCountsApprover, p) {
			t.Errorf("counts_approver must NOT hold %q -- it is an authority grant, not a job", p)
		}
	}

	// And the job roles it rides on stay clean, which is the invariant the whole design rests on.
	for _, role := range []string{permissions.RolePCDirector, permissions.RoleGrowthDirector} {
		for _, p := range want {
			if permissions.RoleHasPermission(role, p) {
				t.Errorf("%s must NOT hold %q; approvals are granted per person via counts_approver", role, p)
			}
		}
	}
}

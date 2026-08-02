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

func TestBootstrapDeniesRevokedDevice(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grant()},
		device:  device("revoked"),
	})
	_, err := svc.Bootstrap(context.Background(), testTenant, testActor, testDevice, "", "trace-1")
	assertAppCode(t, err, "device_revoked")
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
		{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
		// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). This is a leadership/registry bar, so
		// it legitimately KEEPS its "you" entry -- only the label went generic.
		{Key: "alerts", Label: "Alerts", Href: "/alerts"},
		// Backend-composed profile tab: the client no longer appends one.
		{Key: "you", Label: "You", Href: "/you"},
	}
	if len(got.VisibleNavigation) != len(wantNav) {
		t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, wantNav)
	}
	for i := range wantNav {
		if got.VisibleNavigation[i] != wantNav[i] {
			t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], wantNav[i])
		}
	}
	if got.NavChrome != domain.NavChromeMinimal {
		t.Fatalf("NavChrome=%q want %q", got.NavChrome, domain.NavChromeMinimal)
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
		// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). The Hindi label went generic with it.
		{Key: "alerts", Label: "अलर्ट", Href: "/alerts"},
		{Key: "you", Label: "आप", Href: "/you"},
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
		{Key: "videos", Label: "Videos", Href: "/verify/action"},
		// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). This is a leadership/registry bar, so
		// it legitimately KEEPS its "you" entry -- only the label went generic.
		{Key: "alerts", Label: "Alerts", Href: "/alerts"},
		// Backend-composed profile tab: the client no longer appends one.
		{Key: "you", Label: "You", Href: "/you"},
	}
	if len(got.VisibleNavigation) != len(wantNav) {
		t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, wantNav)
	}
	for i := range wantNav {
		if got.VisibleNavigation[i] != wantNav[i] {
			t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], wantNav[i])
		}
	}
	if got.NavChrome != domain.NavChromeMinimal {
		t.Fatalf("NavChrome=%q want %q (park_head is vaccination-only)", got.NavChrome, domain.NavChromeMinimal)
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
	// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts]; "You"
	// lives in the drawer, and the alerts tab label never names the feature (the href's
	// category still scopes it).
	want := []domain.BootstrapNavigationItem{
		{Key: "verify", Label: "Verify", Href: "/verify?module=vaccination"},
		{Key: "alerts", Label: "Alerts", Href: "/verify/alerts?category=vaccination_proof"},
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
// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts]; "You" lives
// in the drawer, and the alerts tab label never names the feature (the href's category
// still scopes it).
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
				{Key: "verify", Label: "Verify", Href: "/verify?module=" + tc.feature},
				{Key: "alerts", Label: wantAlertsLabel, Href: "/verify/alerts?category=" + tc.wantCategory},
			}
			if len(got.VisibleNavigation) != len(want) {
				t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, want)
			}
			for i := range want {
				if got.VisibleNavigation[i] != want[i] {
					t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], want[i])
				}
			}
			if got.NavChrome != domain.NavChromeMinimal {
				t.Fatalf("NavChrome=%q want %q (single feature -> no drawer)", got.NavChrome, domain.NavChromeMinimal)
			}
			if len(got.Modules) != 1 || got.Modules[0].Key != tc.wantModule {
				t.Fatalf("Modules=%#v want single %s module", got.Modules, tc.wantModule)
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
		{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
		// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). This is a leadership/registry bar, so
		// it legitimately KEEPS its "you" entry -- only the label went generic.
		{Key: "alerts", Label: "Alerts", Href: "/alerts"},
		// Backend-composed profile tab: the client no longer appends one.
		{Key: "you", Label: "You", Href: "/you"},
	}
	if len(got.VisibleNavigation) != len(wantNav) {
		t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, wantNav)
	}
	for i := range wantNav {
		if got.VisibleNavigation[i] != wantNav[i] {
			t.Fatalf("VisibleNavigation[%d]=%#v want %#v", i, got.VisibleNavigation[i], wantNav[i])
		}
	}
	if got.NavChrome != domain.NavChromeMinimal {
		t.Fatalf("NavChrome=%q want %q", got.NavChrome, domain.NavChromeMinimal)
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
// Leadership module/route, not the CEO's Vaccination overview tab. CEO opens
// Vaccination from the drawer and sees Overview/Calendar/Alerts/You; operators
// keep the field execution bar.
func TestVisibleNavigationFor(t *testing.T) {
	ceoVaccinationWant := []domain.BootstrapNavigationItem{
		{Key: "overview", Label: "Overview", Href: "/vaccination"},
		{Key: "calendar", Label: "Calendar", Href: "/calendar"},
		{Key: "videos", Label: "Videos", Href: "/verify/action"},
		// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
		// "You" lives in the drawer, and the alerts tab label never names the feature
		// (the href's category still scopes it). This is a leadership/registry bar, so
		// it legitimately KEEPS its "you" entry -- only the label went generic.
		{Key: "alerts", Label: "Alerts", Href: "/alerts"},
		{Key: "you", Label: "You", Href: "/you"},
	}
	tests := []struct {
		name    string
		grants  []domain.GrantSummary
		modules []string
		want    []domain.BootstrapNavigationItem
	}{
		{
			name:    "ceo vaccination module shows overview calendar alerts you",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)},
			modules: []string{"vaccination", "counts"},
			want:    ceoVaccinationWant,
		},
		{
			name:    "operator",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"vaccination"},
			want: []domain.BootstrapNavigationItem{
				{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
				// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
				// "You" lives in the drawer, and the alerts tab label never names the feature
				// (the href's category still scopes it). This is a leadership/registry bar, so
				// it legitimately KEEPS its "you" entry -- only the label went generic.
				{Key: "alerts", Label: "Alerts", Href: "/alerts"},
				{Key: "you", Label: "You", Href: "/you"},
			},
		},
		{
			// No duties named -> scoped to every built feature; the default/active bar
			// is the first (vaccination), with a correctly-categorized Alerts item.
			//
			// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
			// "You" lives in the drawer, and the alerts tab label never names the
			// feature (the href's category still scopes it).
			name:    "verifier with no duties",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			modules: nil,
			want: []domain.BootstrapNavigationItem{
				{Key: "verify", Label: "Verify", Href: "/verify?module=vaccination"},
				{Key: "alerts", Label: "Alerts", Href: "/verify/alerts?category=vaccination_proof"},
			},
		},
		{
			// Same generic "Alerts" label as the vaccination case above, but a
			// different href category (counts maps to shifting_move, NOT counts_proof)
			// -- the label went generic, the SCOPING did not.
			//
			// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
			// "You" lives in the drawer, and the alerts tab label never names the
			// feature (the href's category still scopes it).
			name:    "single-feature verifier",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			modules: []string{"counts"},
			want: []domain.BootstrapNavigationItem{
				{Key: "verify", Label: "Verify", Href: "/verify?module=counts"},
				{Key: "alerts", Label: "Alerts", Href: "/verify/alerts?category=shifting_move"},
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
				{Key: "calendar", Label: "Calendar", Href: "/calendar"},
				{Key: "videos", Label: "Videos", Href: "/verify/action"},
				// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
				// "You" lives in the drawer, and the alerts tab label never names the feature
				// (the href's category still scopes it). This is a leadership/registry bar, so
				// it legitimately KEEPS its "you" entry -- only the label went generic.
				{Key: "alerts", Label: "Alerts", Href: "/alerts"},
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
				{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
				// MAINTAINER DECISION 2026-08-03: verifier bottom bar is [Verify, Alerts];
				// "You" lives in the drawer, and the alerts tab label never names the feature
				// (the href's category still scopes it). This is a leadership/registry bar, so
				// it legitimately KEEPS its "you" entry -- only the label went generic.
				{Key: "alerts", Label: "Alerts", Href: "/alerts"},
				{Key: "you", Label: "You", Href: "/you"},
			},
		},
		{
			// An Operator captures ground events but does not get the tenant-wide census
			// page: counts.read is Admin/CEO-only (maintainer decision 2026-07-18).
			// Counts contributes Approval in the trailing slot other modules give to You,
			// and an Operator holds neither approval permission -- so their Counts bar is
			// exactly these two capture tabs (maintainer decision 2026-07-19).
			name:    "counts-only operator gets the capture pages, not the census",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"counts"},
			want: []domain.BootstrapNavigationItem{
				{Key: "birth_death", Label: "Birth/Death", Href: "/counts/birth-death"},
				{Key: "shifting", Label: "Shifting", Href: "/counts/shifting"},
			},
		},
		{
			// No grants means no nav, not an implicit vaccination default.
			name:    "operator with no module grants gets empty nav",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: nil,
			want:    []domain.BootstrapNavigationItem{},
		},
		{
			name:    "unknown module key contributes nothing",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"not_a_real_module"},
			want:    []domain.BootstrapNavigationItem{},
		},
		{
			// A "soon" module is a roadmap row, never a servable bar.
			name:    "soon module is not servable",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"breeding"},
			want:    []domain.BootstrapNavigationItem{},
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
			name:           "pc director with preventive care verticals stays minimal",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RolePCDirector)},
			grantedModules: []string{"vaccination", "weighing", "counts"},
			want:           domain.NavChromeMinimal,
		},
		{
			name:           "park head with vaccination + weighing + feed stays minimal",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)},
			grantedModules: []string{"vaccination", "weighing", "counts", "feed_direction"},
			want:           domain.NavChromeMinimal,
		},
		{
			name:           "operator minimal",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			grantedModules: []string{"vaccination"},
			want:           domain.NavChromeMinimal,
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
			name:           "single-feature verifier minimal",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			grantedModules: []string{"vaccination"},
			want:           domain.NavChromeMinimal,
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

	t.Run("operator with single module gets minimal nav chrome", func(t *testing.T) {
		chrome := navChromeFor(operatorGrants, modulesFor(operatorGrants, []string{"vaccination"}, ""))
		if chrome != domain.NavChromeMinimal {
			t.Fatalf("navChromeFor(operator)=%q want %q (single module = minimal)", chrome, domain.NavChromeMinimal)
		}
	})

	t.Run("operator vaccination module gets only vaccination tabs", func(t *testing.T) {
		nav := visibleNavigationFor(operatorGrants, []string{"vaccination"}, "")
		if len(nav) != 3 {
			t.Fatalf("operator nav length=%d want 3 (drives + alerts + you)", len(nav))
		}
		if nav[0].Key != "vaccination" || nav[0].Href != "/vaccination" {
			t.Fatalf("first nav item=%#v want shed-first vaccination root at /vaccination", nav[0])
		}
		if nav[1].Key != "alerts" || nav[1].Href != "/alerts" {
			t.Fatalf("second nav item=%#v want alerts inside the Vaccination module bar", nav[1])
		}
		if nav[2].Key != "you" || nav[2].Href != "/you" {
			t.Fatalf("last nav item=%#v want the composed You tab at /you", nav[2])
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
		// An operator executes and nothing else: one work list, no planner list, no oversight.
		want := []domain.BootstrapNavigationItem{
			{Key: "weighing", Label: "My work", Href: "/weighing"},
			// No alerts tab: /alerts is the VACCINATION process-integrity feed, and a weighing
			// operator holds no vaccination permission, so carrying it here gave them a
			// permanently-empty cross-module tab that 403s on open.
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

	t.Run("pc director gets vaccination module only", func(t *testing.T) {
		directorGrants := []domain.GrantSummary{grantWithRole(permissions.RolePCDirector)}
		modules := modulesFor(directorGrants, nil, "")
		if chrome := navChromeFor(directorGrants, modules); chrome != domain.NavChromeMinimal {
			t.Fatalf("pc director chrome=%q want minimal", chrome)
		}
		keys := moduleKeySet(modules)
		if keys["vaccination"] != moduleStatusAvailable {
			t.Fatalf("pc director must get vaccination module; got %v", keys)
		}
		if _, ok := keys["weighing"]; ok {
			t.Fatalf("pc director must not get weighing module; got %v", keys)
		}
	})

	t.Run("growth director gets weighing module only", func(t *testing.T) {
		directorGrants := []domain.GrantSummary{grantWithRole(permissions.RoleGrowthDirector)}
		modules := modulesFor(directorGrants, nil, "")
		if chrome := navChromeFor(directorGrants, modules); chrome != domain.NavChromeMinimal {
			t.Fatalf("growth director chrome=%q want minimal", chrome)
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

	t.Run("park head stays vaccination-only even when extra modules are granted", func(t *testing.T) {
		chrome := navChromeFor(leadershipGrants, modulesFor(leadershipGrants, []string{"vaccination", "weighing", "counts", "feed_direction"}, ""))
		if chrome != domain.NavChromeMinimal {
			t.Fatalf("navChromeFor(park_head)=%q want %q", chrome, domain.NavChromeMinimal)
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
	device         domain.DeviceSummary
	deviceErr      error
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
func (f *fakeRepo) RegisterDevice(context.Context, ports.RegisterDeviceCommand) (domain.DeviceSummary, error) {
	return f.device, nil
}
func (f *fakeRepo) HeartbeatDevice(context.Context, ports.HeartbeatDeviceCommand) (domain.DeviceSummary, error) {
	return f.device, nil
}
func (f *fakeRepo) DeregisterDevice(context.Context, ports.DeregisterDeviceCommand) (domain.DeviceSummary, error) {
	return f.device, nil
}

// TestCountsModuleRoleMatrix pins the maintainer-approved Counts access matrix
// (2026-07-18; the mobile Approval tab was REMOVED 2026-07-21 — approvals moved to admin-web only;
// preventive-care leaders excluded from the Counts drawer 2026-07-24).
// The Counts module exposes capture pages on the phone; who sees which is decided by permission
// (counts.read / counts.write), never by a per-role nav template.
//
// The mobile Counts bar carries capture tabs; an Operator gets
// [birth_death, shifting], and Admin/CEO additionally get census and approval. Permanent RFID
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
//	ceo_internal  |   x    |      x      |    x     |    x     | yes
//	pc_director   |   -    |      -      |    -     |    -     | NO  (preventive-care leader)
//	verifier      |   -    |      -      |    -     |    -     | NO
func TestCountsModuleRoleMatrix(t *testing.T) {
	tests := []struct {
		role      string
		wantItems []string // nav item keys inside the counts module, nil => module absent
	}{
		{permissions.RoleOperator, []string{"birth_death", "shifting"}},
		{permissions.RoleParkHead, nil},
		{permissions.RoleCEOInternal, []string{"counts", "birth_death", "shifting", "approval"}},
		{permissions.RolePCDirector, nil},
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

// TestFeedModuleRoleMatrix pins the current Feed state: feed_direction is a
// declared roadmap drawer row, not a built mobile Feed module yet. It may appear
// as disabled "Soon" for principals that are allowed to see roadmap modules, but
// it contributes no servable bottom-bar items until the Feed surface is built.
func TestFeedModuleRoleMatrix(t *testing.T) {
	tests := []struct {
		role       string
		wantStatus string   // empty => module absent
		wantItems  []string // nav item keys inside the feed module
	}{
		{permissions.RoleCEOInternal, moduleStatusSoon, []string{}},
		{permissions.RoleParkHead, "", nil},
		{permissions.RoleKey(permissions.TierDirector, permissions.VerticalFeed), moduleStatusSoon, []string{}},
		{permissions.RoleKey(permissions.TierHead, permissions.VerticalFeed), moduleStatusSoon, []string{}},
		{permissions.RoleKey(permissions.TierManager, permissions.VerticalFeed), moduleStatusSoon, []string{}},
		{permissions.RoleOperator, moduleStatusSoon, []string{}},
		{permissions.RoleVerifier, "", nil},
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
			if tc.wantStatus == "" {
				if feed != nil {
					t.Fatalf("%s must NOT see the feed module; got items %#v", tc.role, feed.NavItems)
				}
				return
			}
			if feed == nil {
				t.Fatalf("%s must see the feed module; modules=%#v", tc.role, modules)
			}
			if feed.Status != tc.wantStatus {
				t.Fatalf("%s feed status=%q want %q", tc.role, feed.Status, tc.wantStatus)
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
			// Built modules must land on a page this principal can actually open.
			if feed.Status == moduleStatusAvailable && !navItemsContainHref(feed.NavItems, feed.Href) {
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
	if got := countsBar(permissions.RoleOperator); !equal(got, []string{"birth_death", "shifting"}) {
		t.Fatalf("operator counts bar=%v want exactly [birth_death shifting]", got)
	}

	// A park head no longer gets an approval tab — its Counts bar is the same capture tabs.
	if got := countsBar(permissions.RoleParkHead); !equal(got, []string{"birth_death", "shifting"}) {
		t.Fatalf("park_head counts bar=%v want [birth_death shifting] (no approval on mobile)", got)
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

	// A module-less department composes an EMPTY bar. This is the intended outcome for departments
	// (procurement, growth, infra, milk, sales) that hold no operational module -- not a defect.
	if nav := visibleNavigationFor(grants, nil, "en"); len(nav) != 0 {
		t.Fatalf("nav with no granted module=%v, want empty (a module-less department correctly gets a blank bar)", nav)
	}

	// A department that DOES hold a module (here Counts) composes that module's non-empty bottom bar.
	nav := visibleNavigationFor(grants, []string{"counts"}, "en")
	if len(nav) == 0 {
		t.Fatal("nav for an operator whose department holds the Counts module is empty -- a granted module must render its bar")
	}
}

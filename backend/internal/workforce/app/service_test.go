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
		{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
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
		t.Fatalf("NavChrome=%q want %q (park_head has a single module)", got.NavChrome, domain.NavChromeMinimal)
	}
}

func TestBootstrapVerifierGetsStandaloneVerificationNav(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	want := []domain.BootstrapNavigationItem{
		{Key: "verify", Label: "Verify", Href: "/verify"},
		{Key: "you", Label: "You", Href: "/you"},
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
		t.Fatalf("NavChrome=%q want %q", got.NavChrome, domain.NavChromeMinimal)
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
// validRole in service.go): ceo_internal, park_head, and pc_director are
// leadership tiers. Verifier owns the standalone verification app; operator
// owns field execution. Neither is leadership navigation.
func TestIsLeadershipPrincipal(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{role: permissions.RoleCEOInternal, want: true},
		{role: permissions.RoleCEOInternal, want: true},
		{role: permissions.RolePCDirector, want: true},
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
				{Key: "alerts", Label: "Alerts", Href: "/alerts"},
				{Key: "you", Label: "You", Href: "/you"},
			},
		},
		{
			name:    "verifier",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			modules: nil,
			want: []domain.BootstrapNavigationItem{
				{Key: "verify", Label: "Verify", Href: "/verify"},
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
			name:    "counts-only operator gets the two capture pages, not the census",
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

// TestNavChromeFor verifies chrome follows the composed drawer: a leadership
// principal with >=2 available modules (CEO: vaccination + counts) gets the
// expanded drawer; a single-module leader (PC Director / Park Head) and every
// field operator get minimal chrome (bottom bar only). Chrome is derived from
// the composed modules, so cases pass the real modulesFor() output.
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
			name:           "pc director single module stays minimal",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RolePCDirector)},
			grantedModules: []string{"vaccination", "counts"},
			want:           domain.NavChromeMinimal,
		},
		{
			name:           "park head single module stays minimal",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)},
			grantedModules: []string{"vaccination", "counts"},
			want:           domain.NavChromeMinimal,
		},
		{
			name:           "operator minimal",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			grantedModules: []string{"vaccination"},
			want:           domain.NavChromeMinimal,
		},
		{
			// Operator drawer rollout is disabled for now, even with multiple modules.
			name:           "operator with two modules stays minimal",
			grants:         []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			grantedModules: []string{"vaccination", "counts"},
			want:           domain.NavChromeMinimal,
		},
		{
			name:   "verifier minimal",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			want:   domain.NavChromeMinimal,
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
// 2. Operator drawer chrome is disabled while module-specific bars still compose
// 3. Leadership principals use the shared Vaccination module after Overview removal
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

	t.Run("operator with single vaccination module gets shed queue, alerts, and you", func(t *testing.T) {
		nav := visibleNavigationFor(operatorGrants, []string{"vaccination"}, "")
		if len(nav) != 3 {
			t.Fatalf("operator nav length=%d want 3 (drives + alerts + you)", len(nav))
		}
		if nav[0].Key != "vaccination" || nav[0].Href != "/vaccination" {
			t.Fatalf("first nav item=%#v want shed-first vaccination root at /vaccination", nav[0])
		}
		for _, item := range nav {
			if item.Key == "calendar" || item.Href == "/calendar" {
				t.Fatalf("operator vaccination nav must not include Calendar/week/month/history; got %#v", nav)
			}
		}
		// "You" is a BACKEND contribution now, not client-static chrome the shell appends.
		// The client renders the composed bar verbatim, so if this item stops being emitted the
		// profile/settings surface silently disappears from the bottom bar.
		if nav[2].Key != "you" || nav[2].Href != "/you" {
			t.Fatalf("last nav item=%#v want the composed You tab at /you", nav[2])
		}
		// Verify shared nav items are not duplicated (dedupe by shared_key)
		seen := make(map[string]int)
		for _, item := range nav {
			seen[item.Key]++
			if seen[item.Key] > 1 {
				t.Fatalf("nav item %q appears %d times (should be deduplicated)", item.Key, seen[item.Key])
			}
		}
	})

	t.Run("preventive care leader keeps field vaccination bar", func(t *testing.T) {
		nav := visibleNavigationFor(leadershipGrants, nil, "")
		if len(nav) != 3 {
			t.Fatalf("pc leader nav length=%d want 3 (drives + alerts + you)", len(nav))
		}
		if nav[0].Key != "vaccination" || nav[0].Href != "/vaccination" {
			t.Fatalf("first nav item=%#v want operator-style Drives at /vaccination", nav[0])
		}
		if nav[1].Key != "alerts" || nav[2].Key != "you" {
			t.Fatalf("pc leader nav should have alerts and you after drives; got %v", []string{nav[1].Key, nav[2].Key})
		}
	})

	t.Run("ceo (multi-module leader) gets expanded nav chrome", func(t *testing.T) {
		ceoGrants := []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)}
		chrome := navChromeFor(ceoGrants, modulesFor(ceoGrants, nil, ""))
		if chrome != domain.NavChromeExpanded {
			t.Fatalf("navChromeFor(ceo)=%q want %q", chrome, domain.NavChromeExpanded)
		}
	})

	t.Run("pc leader (single-module) gets minimal nav chrome", func(t *testing.T) {
		chrome := navChromeFor(leadershipGrants, modulesFor(leadershipGrants, []string{"vaccination", "counts"}, ""))
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
// (2026-07-18, extended 2026-07-19 with the Approval tab). The Counts module exposes four
// pages; who sees which is decided by permission (counts.read / counts.write /
// counts.approve_access), never by a per-role nav template.
//
// The Approval tab occupies the TRAILING bar slot that other modules give to "You", so a
// principal with no approval permission gets a strictly shorter bar rather than a disabled
// row — most importantly, an Operator's Counts bar is exactly [birth_death, shifting].
//
// Counts item composition is still permission-gated (counts.read / counts.write /
// counts.approve_access). Whether the Counts MODULE appears in the drawer at all is
// decided upstream by the drawer matrix: operators get it via their department
// grant, CEO gets it via the leadership tier, and preventive-care leaders
// (PC Director, Park Head) do NOT — their drawer is the vaccination home only
// (maintainer decision 2026-07-24).
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

// TestCountsModuleReplacesYouTabWithApproval pins the maintainer decision (2026-07-19) that the
// Counts module owns its trailing bottom-bar slot: it contributes the Approval queue there, and
// deliberately does NOT contribute the global "You" tab that Vaccination does.
//
// This is the backend half of removing the client's hardcoded trailing tab. The Android shell now
// renders the composed bar verbatim with nothing appended, so these assertions are what actually
// decide how many tabs an operator sees. If "you" ever leaked into the counts contributions, the
// Counts module would silently grow a fifth tab; if "approval" were ungated, an operator would be
// shown a queue they cannot act on and whose route 403s.
func TestCountsModuleReplacesYouTabWithApproval(t *testing.T) {
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

	// The headline requirement: an operator's Counts bar is exactly two tabs.
	if got := countsBar(permissions.RoleOperator); !equal(got, []string{"birth_death", "shifting"}) {
		t.Fatalf("operator counts bar=%v want exactly [birth_death shifting]", got)
	}

	// An approver gets the same two capture tabs plus Approval in the trailing slot.
	if got := countsBar(permissions.RoleParkHead); !equal(got, []string{"birth_death", "shifting", "approval"}) {
		t.Fatalf("park_head counts bar=%v want [birth_death shifting approval]", got)
	}

	// No role gets a "You" tab from Counts -- it is not a counts contribution at all.
	for _, role := range []string{
		permissions.RoleOperator, permissions.RoleParkHead,
		permissions.RoleCEOInternal, permissions.RoleCEOInternal,
	} {
		for _, key := range countsBar(role) {
			if key == "you" {
				t.Fatalf("%s counts bar contains a 'you' tab; Counts contributes Approval in that slot", role)
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

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
		{Key: "calendar", Label: "Calendar", Href: "/calendar"},
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
		{Key: "calendar", Label: "कैलेंडर", Href: "/calendar"},
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

// TestBootstrapLeadershipGetsFixedNav pins the leadership
// loop: a leadership-tier grant (park_head here, but the role set below
// covers every leadership tier) gets the fixed Calendar/Overview/Alerts nav
// and expanded chrome.
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
		{Key: "leadership", Label: "Overview", Href: "/leadership"},
		{Key: "calendar", Label: "Calendar", Href: "/calendar"},
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
	if got.NavChrome != domain.NavChromeExpanded {
		t.Fatalf("NavChrome=%q want %q", got.NavChrome, domain.NavChromeExpanded)
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
	want := []domain.BootstrapNavigationItem{{Key: "verify", Label: "Verify", Href: "/verify"}}
	if len(got.VisibleNavigation) != len(want) || got.VisibleNavigation[0] != want[0] {
		t.Fatalf("VisibleNavigation=%#v want %#v", got.VisibleNavigation, want)
	}
	if got.NavChrome != domain.NavChromeMinimal {
		t.Fatalf("NavChrome=%q want %q", got.NavChrome, domain.NavChromeMinimal)
	}
}

// TestBootstrapOperatorGetsFixedNav is a regression guard: an operator-role
// principal keeps the field-operator nav untouched by the leadership branch.
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
		{Key: "calendar", Label: "Calendar", Href: "/calendar"},
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

// TestVisibleNavigationFor pins that leadership always gets the fixed
// Calendar/Overview/Alerts nav, and operator gets the fixed field nav.
func TestVisibleNavigationFor(t *testing.T) {
	leadershipWant := []domain.BootstrapNavigationItem{
		{Key: "leadership", Label: "Overview", Href: "/leadership"},
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
			name:    "leadership ignores module grants",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)},
			modules: []string{"vaccination", "counts"},
			want:    leadershipWant,
		},
		{
			name:    "operator",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"vaccination"},
			want: []domain.BootstrapNavigationItem{
				{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
				{Key: "calendar", Label: "Calendar", Href: "/calendar"},
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
				{Key: "calendar", Label: "Calendar", Href: "/calendar"},
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

// TestNavChromeFor verifies that leadership principals get expanded chrome
// (module drawer) while field operators get minimal chrome (bottom bar only).
func TestNavChromeFor(t *testing.T) {
	tests := []struct {
		name    string
		grants  []domain.GrantSummary
		modules []string
		want    string
	}{
		{
			name:    "leadership expanded",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)},
			modules: nil,
			want:    domain.NavChromeExpanded,
		},
		{
			name:    "operator minimal",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"vaccination"},
			want:    domain.NavChromeMinimal,
		},
		{
			// Two granted modules earn the drawer without any leadership role.
			name:    "operator with two modules expanded",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"vaccination", "counts"},
			want:    domain.NavChromeExpanded,
		},
		{
			// A disabled roadmap row must not promote a single-module operator.
			name:    "soon module does not earn the drawer",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"vaccination", "breeding"},
			want:    domain.NavChromeMinimal,
		},
		{
			name:    "duplicate grants do not earn the drawer",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			modules: []string{"vaccination", "vaccination"},
			want:    domain.NavChromeMinimal,
		},
		{
			name:    "ceo expanded",
			grants:  []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)},
			modules: nil,
			want:    domain.NavChromeExpanded,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := navChromeFor(tc.grants, tc.modules); got != tc.want {
				t.Fatalf("navChromeFor()=%q want %q", got, tc.want)
			}
		})
	}
}

// TestBootstrapNavComposition verifies that navigation is composed from granted modules,
// not hardcoded per-role. This test proves:
// 1. Operator with single module (vaccination) gets bottom-bar-only nav (minimal chrome)
// 2. Operator with multiple modules would get sidebar nav (expanded chrome)
// 3. Leadership principals get overview + shared cross-module nav (expanded chrome)
// 4. Nav items are deduplicated by shared_key (e.g., calendar appears once)
func TestBootstrapNavComposition(t *testing.T) {
	operatorGrants := []domain.GrantSummary{grantWithRole(permissions.RoleOperator)}
	leadershipGrants := []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)}

	t.Run("operator with single module gets minimal nav chrome", func(t *testing.T) {
		chrome := navChromeFor(operatorGrants, []string{"vaccination"})
		if chrome != domain.NavChromeMinimal {
			t.Fatalf("navChromeFor(operator)=%q want %q (single module = minimal)", chrome, domain.NavChromeMinimal)
		}
	})

	t.Run("operator with single module gets vaccination + shared nav", func(t *testing.T) {
		nav := visibleNavigationFor(operatorGrants, []string{"vaccination"}, "")
		if len(nav) != 4 {
			t.Fatalf("operator nav length=%d want 4 (vaccination + calendar + alerts + you)", len(nav))
		}
		// "You" is a BACKEND contribution now, not client-static chrome the shell appends.
		// The client renders the composed bar verbatim, so if this item stops being emitted the
		// profile/settings surface silently disappears from the bottom bar.
		if nav[3].Key != "you" || nav[3].Href != "/you" {
			t.Fatalf("last nav item=%#v want the composed You tab at /you", nav[3])
		}
		// Verify calendar, alerts, and you are not duplicated (dedupe by shared_key)
		seen := make(map[string]int)
		for _, item := range nav {
			seen[item.Key]++
			if seen[item.Key] > 1 {
				t.Fatalf("nav item %q appears %d times (should be deduplicated)", item.Key, seen[item.Key])
			}
		}
	})

	t.Run("leadership principal gets overview + shared nav", func(t *testing.T) {
		nav := visibleNavigationFor(leadershipGrants, nil, "")
		if len(nav) != 4 {
			t.Fatalf("leadership nav length=%d want 4 (overview + calendar + alerts + you)", len(nav))
		}
		if nav[0].Key != "leadership" {
			t.Fatalf("first nav item key=%q want 'leadership'", nav[0].Key)
		}
		if nav[1].Key != "calendar" || nav[2].Key != "alerts" || nav[3].Key != "you" {
			t.Fatalf("leadership nav should have calendar, alerts, and you; got %v", []string{nav[1].Key, nav[2].Key, nav[3].Key})
		}
	})

	t.Run("leadership gets expanded nav chrome", func(t *testing.T) {
		chrome := navChromeFor(leadershipGrants, nil)
		if chrome != domain.NavChromeExpanded {
			t.Fatalf("navChromeFor(leadership)=%q want %q", chrome, domain.NavChromeExpanded)
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
// (2026-07-18; the mobile Approval tab was REMOVED 2026-07-21 — approvals moved to admin-web only).
// The Counts module exposes capture pages on the phone; who sees which is decided by permission
// (counts.read / counts.write), never by a per-role nav template.
//
// The mobile Counts bar no longer carries an approval tab for anyone: an Operator/Park Head gets
// exactly [birth_death, shifting], and Admin/CEO additionally get the census page.
//
//	role          | census | birth/death | shifting | module in drawer
//	operator      |   -    |      x      |    x     | yes
//	park_head     |   -    |      x      |    x     | yes
//	ceo_internal  |   x    |      x      |    x     | yes
//	pc_director   |   -    |      -      |    -     | NO
//	verifier      |   -    |      -      |    -     | NO
func TestCountsModuleRoleMatrix(t *testing.T) {
	tests := []struct {
		role      string
		wantItems []string // nav item keys inside the counts module, "" => module absent
	}{
		{permissions.RoleOperator, []string{"birth_death", "shifting"}},
		{permissions.RoleParkHead, []string{"birth_death", "shifting"}},
		{permissions.RoleCEOInternal, []string{"counts", "birth_death", "shifting"}},
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

// TestFeedModuleRoleMatrix pins who sees the mobile Feed module and which of its two tabs. The
// module is the Feed vertical on the phone: Feed Direction (the generated sheet, gated ProtocolRead)
// and Feed Packing (the bag worklist, gated FeedPackingRead). The two tabs gate on DIFFERENT
// authorities on purpose, so the matrix is not "all or nothing":
//
//   - CEO/park head hold both authorities, so they get both tabs (via the drawer — they default to
//     the leadership bar and switch to Feed).
//   - an org Head/Director tier holds ProtocolRead but not FeedPackingRead, so they get Feed
//     Direction only — they may read the sheet but not draw the bags.
//   - an operator holds neither, so the module is fully gated away and never appears.
//
// The Android shell renders this composed bar verbatim; this is the backend authority for it.
func TestFeedModuleRoleMatrix(t *testing.T) {
	tests := []struct {
		role      string
		wantItems []string // nav item keys inside the feed module, nil => module absent
	}{
		{permissions.RoleCEOInternal, []string{"feed_direction", "feed_packing"}},
		{permissions.RoleParkHead, []string{"feed_direction", "feed_packing"}},
		{permissions.RoleKey(permissions.TierDirector, permissions.VerticalFeed), []string{"feed_direction"}},
		{permissions.RoleKey(permissions.TierHead, permissions.VerticalFeed), []string{"feed_direction"}},
		{permissions.RoleKey(permissions.TierManager, permissions.VerticalFeed), nil},
		{permissions.RoleOperator, nil},
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
			// Landing must be a page this principal can actually open.
			if !navItemsContainHref(feed.NavItems, feed.Href) {
				t.Fatalf("%s feed landing href=%q is not among its permitted items %v", tc.role, feed.Href, got)
			}
		})
	}
}

// TestCountsModuleBarIsCaptureOnlyAndOmitsYouTab pins that the mobile Counts module contributes
// only capture tabs and no trailing action tab. The Approval queue was REMOVED from mobile
// (maintainer decision 2026-07-21) — approve/reject is admin-web only — and Counts never
// contributed the global "You" tab that vaccination and leadership do.
//
// This is the backend authority for how many tabs an operator sees: the Android shell renders the
// composed bar verbatim. If "approval" ever re-appeared here, the phone would show a queue that no
// longer has a mobile route; if "you" leaked in, Counts would grow a tab it does not own.
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

	// The headline requirement: an operator's Counts bar is exactly two capture tabs.
	if got := countsBar(permissions.RoleOperator); !equal(got, []string{"birth_death", "shifting"}) {
		t.Fatalf("operator counts bar=%v want exactly [birth_death shifting]", got)
	}

	// A park head no longer gets an approval tab — its Counts bar is the same two capture tabs.
	if got := countsBar(permissions.RoleParkHead); !equal(got, []string{"birth_death", "shifting"}) {
		t.Fatalf("park_head counts bar=%v want [birth_death shifting] (no approval on mobile)", got)
	}

	// No role gets an "approval" or a "You" tab from Counts on mobile.
	for _, role := range []string{
		permissions.RoleOperator, permissions.RoleParkHead,
		permissions.RoleCEOInternal, permissions.RoleCEOInternal,
	} {
		for _, key := range countsBar(role) {
			if key == "you" || key == "approval" {
				t.Fatalf("%s counts bar contains a %q tab; Counts is capture-only on mobile", role, key)
			}
		}
	}

	// ...while the modules that DO own it still emit it, so the profile surface stays reachable.
	for _, module := range []string{"vaccination", "leadership"} {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)}
		items := composeNavigationFromModules([]string{module}, grants, "")
		found := false
		for _, item := range items {
			if item.Key == "you" && item.Href == "/you" {
				found = true
			}
		}
		if !found {
			t.Fatalf("module %q must contribute the You tab at /you; got %#v", module, items)
		}
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

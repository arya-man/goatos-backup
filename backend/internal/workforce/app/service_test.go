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
		profile: profile("active"),
		grants:  []domain.GrantSummary{grant()},
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	wantNav := []domain.BootstrapNavigationItem{
		{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
		{Key: "calendar", Label: "Calendar", Href: "/calendar"},
		{Key: "alerts", Label: "Alerts", Href: "/alerts"},
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
		profile: profile("active"),
		grants:  []domain.GrantSummary{grant()},
		caps:    []domain.CapabilityAssignment{{CapabilityCode: "movement.execute", Status: "active"}},
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "hi", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	wantNav := []domain.BootstrapNavigationItem{
		{Key: "vaccination", Label: "ड्राइव", Href: "/vaccination"},
		{Key: "calendar", Label: "कैलेंडर", Href: "/calendar"},
		{Key: "alerts", Label: "अलर्ट", Href: "/alerts"},
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
		profile: profile("active"),
		grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
	})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	wantNav := []domain.BootstrapNavigationItem{
		{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
		{Key: "calendar", Label: "Calendar", Href: "/calendar"},
		{Key: "alerts", Label: "Alerts", Href: "/alerts"},
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
// validRole in service.go): admin, park_head, pc_director, and ceo_internal
// are leadership tiers. Verifier owns the standalone verification app; operator
// owns field execution. Neither is leadership navigation.
func TestIsLeadershipPrincipal(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{role: permissions.RoleAdmin, want: true},
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
	}
	tests := []struct {
		name   string
		grants []domain.GrantSummary
		want   []domain.BootstrapNavigationItem
	}{
		{
			name:   "leadership",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)},
			want:   leadershipWant,
		},
		{
			name:   "operator",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			want: []domain.BootstrapNavigationItem{
				{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
				{Key: "calendar", Label: "Calendar", Href: "/calendar"},
				{Key: "alerts", Label: "Alerts", Href: "/alerts"},
			},
		},
		{
			name:   "verifier",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
			want: []domain.BootstrapNavigationItem{
				{Key: "verify", Label: "Verify", Href: "/verify"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := visibleNavigationFor(tc.grants, "")
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
		name   string
		grants []domain.GrantSummary
		want   string
	}{
		{
			name:   "leadership expanded",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)},
			want:   domain.NavChromeExpanded,
		},
		{
			name:   "operator minimal",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			want:   domain.NavChromeMinimal,
		},
		{
			name:   "ceo expanded",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)},
			want:   domain.NavChromeExpanded,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := navChromeFor(tc.grants); got != tc.want {
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
		chrome := navChromeFor(operatorGrants)
		if chrome != domain.NavChromeMinimal {
			t.Fatalf("navChromeFor(operator)=%q want %q (single module = minimal)", chrome, domain.NavChromeMinimal)
		}
	})

	t.Run("operator with single module gets vaccination + shared nav", func(t *testing.T) {
		nav := visibleNavigationFor(operatorGrants, "")
		if len(nav) != 3 {
			t.Fatalf("operator nav length=%d want 3 (vaccination + calendar + alerts)", len(nav))
		}
		// Verify calendar and alerts are not duplicated (dedupe by shared_key)
		keys := []string{nav[0].Key, nav[1].Key, nav[2].Key}
		seen := make(map[string]int)
		for _, k := range keys {
			seen[k]++
			if seen[k] > 1 {
				t.Fatalf("nav item %q appears %d times (should be deduplicated)", k, seen[k])
			}
		}
	})

	t.Run("leadership principal gets overview + shared nav", func(t *testing.T) {
		nav := visibleNavigationFor(leadershipGrants, "")
		if len(nav) != 3 {
			t.Fatalf("leadership nav length=%d want 3 (overview + calendar + alerts)", len(nav))
		}
		if nav[0].Key != "leadership" {
			t.Fatalf("first nav item key=%q want 'leadership'", nav[0].Key)
		}
		if nav[1].Key != "calendar" || nav[2].Key != "alerts" {
			t.Fatalf("leadership nav should have calendar and alerts; got %v", []string{nav[1].Key, nav[2].Key})
		}
	})

	t.Run("leadership gets expanded nav chrome", func(t *testing.T) {
		chrome := navChromeFor(leadershipGrants)
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
	profile    domain.OperatorProfile
	profileErr error
	grants     []domain.GrantSummary
	caps       []domain.CapabilityAssignment
	device     domain.DeviceSummary
	deviceErr  error
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

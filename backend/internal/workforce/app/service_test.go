package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// stubOwnership is a no-DB fake of permissions.ModuleOwnershipSource for unit
// tests: it returns a fixed owned-module set (and optional error).
type stubOwnership struct {
	mods []permissions.OwnedModule
	err  error
}

func (s stubOwnership) ListActiveModuleGrantsForActor(context.Context, string, string) ([]permissions.OwnedModule, error) {
	return s.mods, s.err
}

const (
	testTenant   = "00000000-0000-4000-8000-000000000001"
	testActor    = "90000000-0000-4000-8000-000000000001"
	testOperator = "91000000-0000-4000-8000-000000000001"
	testDevice   = "92000000-0000-4000-8000-000000000001"
)

func TestBootstrapDeniesMissingProfile(t *testing.T) {
	svc := NewService(&fakeRepo{profileErr: ports.ErrNotFound}, stubOwnership{})
	_, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "trace-1")
	assertAppCode(t, err, "operator_profile_missing")
}

func TestBootstrapDeniesInactiveProfile(t *testing.T) {
	svc := NewService(&fakeRepo{profile: profile("inactive"), grants: []domain.GrantSummary{grant()}}, stubOwnership{})
	_, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "trace-1")
	assertAppCode(t, err, "operator_profile_inactive")
}

func TestBootstrapDeniesMissingGrant(t *testing.T) {
	svc := NewService(&fakeRepo{profile: profile("active")}, stubOwnership{})
	_, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "trace-1")
	assertAppCode(t, err, "operator_grant_missing")
}

func TestBootstrapDeniesRevokedDevice(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grant()},
		device:  device("revoked"),
	}, stubOwnership{})
	_, err := svc.Bootstrap(context.Background(), testTenant, testActor, testDevice, "trace-1")
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
	}, stubOwnership{})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, testDevice, "trace-1")
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

func TestBootstrapPopulatesModuleDrivenNavAndChrome(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grant()},
	}, stubOwnership{mods: []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}}})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "trace-1")
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
	if len(got.OwnedModules) != 1 || got.OwnedModules[0].Module != "pc.vaccination" || got.OwnedModules[0].Vertical != "pc" {
		t.Fatalf("OwnedModules=%#v want single pc.vaccination", got.OwnedModules)
	}
}

// TestBootstrapLeadershipGetsFixedNavRegardlessOfModules pins the leadership
// loop: a leadership-tier grant (park_head here, but the role set below
// covers every leadership tier) gets the fixed Calendar/Overview/Alerts nav
// and minimal chrome even though it owns zero registry-mapped modules (so
// the module-driven path alone would otherwise leave leadership with no
// reachable nav item at all).
func TestBootstrapLeadershipGetsFixedNavRegardlessOfModules(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)},
	}, stubOwnership{})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "trace-1")
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
	if got.NavChrome != domain.NavChromeMinimal {
		t.Fatalf("NavChrome=%q want %q", got.NavChrome, domain.NavChromeMinimal)
	}
}

// TestBootstrapOperatorKeepsModuleDrivenNav is a regression guard: an
// operator-role principal must keep the existing module-driven nav
// (vaccination-only today) untouched by the new leadership branch.
func TestBootstrapOperatorKeepsModuleDrivenNav(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
	}, stubOwnership{mods: []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}}})
	got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "trace-1")
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

func TestBootstrapOwnershipError(t *testing.T) {
	svc := NewService(&fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grant()},
	}, stubOwnership{err: errors.New("ownership read failed")})
	if _, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "trace-1"); err == nil {
		t.Fatal("expected ownership error to propagate")
	}
}

func TestNavigationForModules(t *testing.T) {
	tests := []struct {
		name string
		mods []permissions.OwnedModule
		want []domain.BootstrapNavigationItem
	}{
		{name: "none", mods: nil, want: []domain.BootstrapNavigationItem{}},
		{
			name: "only vaccination",
			mods: []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}},
			want: []domain.BootstrapNavigationItem{{Key: "vaccination", Label: "Vaccination", Href: "/vaccination"}},
		},
		{
			// admin.config is not registry-mapped (not yet built) -> dropped.
			name: "vaccination plus unmapped module",
			mods: []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}, {Vertical: "admin", Module: "admin.config"}},
			want: []domain.BootstrapNavigationItem{{Key: "vaccination", Label: "Vaccination", Href: "/vaccination"}},
		},
		{
			name: "duplicate vaccination deduped",
			mods: []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}, {Vertical: "pc", Module: "pc.vaccination"}},
			want: []domain.BootstrapNavigationItem{{Key: "vaccination", Label: "Vaccination", Href: "/vaccination"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := navigationForModules(tc.mods)
			if len(got) != len(tc.want) {
				t.Fatalf("navigationForModules(%#v)=%#v want %#v", tc.mods, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("item[%d]=%#v want %#v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestNavChromeForModules pins the threshold to registry-mapped modules only.
// With pc.vaccination as the single currently-registered module, the reachable
// states are minimal (0 or 1 mapped, and dedup of the same module). "expanded"
// (>=2 distinct mapped modules) becomes reachable honestly once a second module
// is added to moduleNavRegistry; we do not fake a second built module here.
func TestNavChromeForModules(t *testing.T) {
	tests := []struct {
		name string
		mods []permissions.OwnedModule
		want string
	}{
		{name: "zero mapped", mods: nil, want: domain.NavChromeMinimal},
		{name: "unmapped only", mods: []permissions.OwnedModule{{Vertical: "admin", Module: "admin.config"}}, want: domain.NavChromeMinimal},
		{name: "one mapped", mods: []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}}, want: domain.NavChromeMinimal},
		{
			name: "same mapped twice deduped",
			mods: []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}, {Vertical: "pc", Module: "pc.vaccination"}},
			want: domain.NavChromeMinimal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := navChromeForModules(tc.mods); got != tc.want {
				t.Fatalf("navChromeForModules(%#v)=%q want %q", tc.mods, got, tc.want)
			}
		})
	}
}

// TestIsLeadershipPrincipal covers every valid workforce grant role (see
// validRole in service.go): admin, park_head, pc_director, verifier, and
// ceo_internal are leadership tiers (mirrors roleLensForRole in
// internal/adminui/app/compiler.go); operator is the only non-leadership
// role.
func TestIsLeadershipPrincipal(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{role: permissions.RoleAdmin, want: true},
		{role: permissions.RoleCEOInternal, want: true},
		{role: permissions.RolePCDirector, want: true},
		{role: permissions.RoleParkHead, want: true},
		{role: permissions.RoleVerifier, want: true},
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
// Calendar/Overview/Alerts nav regardless of owned modules, and operator
// keeps falling through to navigationForModules.
func TestVisibleNavigationFor(t *testing.T) {
	leadershipWant := []domain.BootstrapNavigationItem{
		{Key: "leadership", Label: "Overview", Href: "/leadership"},
		{Key: "calendar", Label: "Calendar", Href: "/calendar"},
		{Key: "alerts", Label: "Alerts", Href: "/alerts"},
	}
	tests := []struct {
		name   string
		grants []domain.GrantSummary
		mods   []permissions.OwnedModule
		want   []domain.BootstrapNavigationItem
	}{
		{
			name:   "leadership with no owned modules",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)},
			mods:   nil,
			want:   leadershipWant,
		},
		{
			name:   "leadership even with vaccination module owned",
			grants: []domain.GrantSummary{grantWithRole(permissions.RolePCDirector)},
			mods:   []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}},
			want:   leadershipWant,
		},
		{
			name:   "operator owning vaccination gets the fixed operator nav",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			mods:   []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}},
			want: []domain.BootstrapNavigationItem{
				{Key: "vaccination", Label: "Drives", Href: "/vaccination"},
				{Key: "calendar", Label: "Calendar", Href: "/calendar"},
				{Key: "alerts", Label: "Alerts", Href: "/alerts"},
			},
		},
		{
			name:   "operator with no owned modules gets empty nav",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			mods:   nil,
			want:   []domain.BootstrapNavigationItem{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := visibleNavigationFor(tc.grants, tc.mods)
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

// TestNavChromeFor pins leadership to minimal chrome even when it owns 2+
// modules (a hypothetical future case), and keeps the module-count-driven
// chrome for operators.
func TestNavChromeFor(t *testing.T) {
	tests := []struct {
		name   string
		grants []domain.GrantSummary
		mods   []permissions.OwnedModule
		want   string
	}{
		{
			name:   "leadership always minimal",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)},
			mods:   nil,
			want:   domain.NavChromeMinimal,
		},
		{
			name:   "operator single module minimal",
			grants: []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
			mods:   []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}},
			want:   domain.NavChromeMinimal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := navChromeFor(tc.grants, tc.mods); got != tc.want {
				t.Fatalf("navChromeFor()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestToDomainOwnedModules(t *testing.T) {
	in := []permissions.OwnedModule{{Vertical: "pc", Module: "pc.vaccination"}, {Vertical: "admin", Module: "admin.config"}}
	got := toDomainOwnedModules(in)
	if len(got) != len(in) {
		t.Fatalf("toDomainOwnedModules len=%d want %d", len(got), len(in))
	}
	for i := range in {
		if got[i].Vertical != in[i].Vertical || got[i].Module != in[i].Module {
			t.Fatalf("mapped[%d]=%#v want %#v", i, got[i], in[i])
		}
	}
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

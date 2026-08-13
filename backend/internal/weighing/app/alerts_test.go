package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

const (
	alertsTenant = "00000000-0000-0000-0000-000000000001"
	alertsParkA  = "00000000-0000-4000-8000-000000000201"
	alertsParkB  = "00000000-0000-4000-8000-000000000202"
)

// recordingAlertsRepo captures exactly what the service asked the repository for.
// The scoping decision IS the behaviour under test: the SQL filters on the
// member id and park list this fake records, so asserting on them asserts on who
// the caller can see.
type recordingAlertsRepo struct {
	fakeRepo
	calls []recordedAlertsCall
	page  domain.AlertPage
}

type recordedAlertsCall struct {
	tenantID       string
	memberOrUserID string
	tenantWide     bool
	parkIDs        []string
	cursor         string
	limit          int
}

// ListParks satisfies ports.Repository. This fake covers the alerts path only, so the park list
// is empty rather than fabricated -- an alerts assertion must never depend on invented parks.
func (r *recordingAlertsRepo) ListParks(context.Context, string) ([]domain.WeighingPark, error) {
	return nil, nil
}

func (r *recordingAlertsRepo) ListAlerts(
	_ context.Context,
	tenantID, memberOrUserID string,
	tenantWide bool,
	parkIDs []string,
	cursor string,
	limit int,
) (domain.AlertPage, error) {
	r.calls = append(r.calls, recordedAlertsCall{
		tenantID: tenantID, memberOrUserID: memberOrUserID,
		tenantWide: tenantWide, parkIDs: parkIDs, cursor: cursor, limit: limit,
	})
	return r.page, nil
}

func alertsContext(grants ...permissions.ActiveGrant) context.Context {
	ctx := httpmiddleware.WithAuthGrants(context.Background(), grants)
	return httpmiddleware.WithTenantID(ctx, alertsTenant)
}

// TestListAlertsOperatorReadsOnlyTheirOwnFeed is the core audience guarantee.
// The feed is the READ of routing that already happened, so the only identity
// the repository may be given is the CALLER'S OWN. If the service ever passed
// something else -- a shed's assignee, a park roster, a request parameter -- an
// operator could read another operator's alerts.
func TestListAlertsOperatorReadsOnlyTheirOwnFeed(t *testing.T) {
	repo := &recordingAlertsRepo{}
	service := NewService(repo)
	ctx := alertsContext(permissions.ActiveGrant{
		Role: permissions.RoleOperator, ScopeType: "park", ScopeID: alertsParkA,
	})

	if _, err := service.ListAlerts(ctx, domain.Actor{
		TenantID: alertsTenant,
		UserID:   "operator-pramod",
		Roles:    []string{permissions.RoleOperator},
	}, "", 0); err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}

	if len(repo.calls) != 1 {
		t.Fatalf("repo calls = %d, want 1", len(repo.calls))
	}
	call := repo.calls[0]
	if call.memberOrUserID != "operator-pramod" {
		t.Fatalf("member id = %q, want the caller's own id; another operator's feed would be readable", call.memberOrUserID)
	}
	if call.tenantID != alertsTenant {
		t.Fatalf("tenant = %q, want %q", call.tenantID, alertsTenant)
	}
	// A park-scoped operator must NEVER be read tenant-wide.
	if call.tenantWide {
		t.Fatal("park-scoped operator was read tenant-wide; that exposes every park's weighing alerts")
	}
	if len(call.parkIDs) != 1 || call.parkIDs[0] != alertsParkA {
		t.Fatalf("park scope = %v, want only [%s]", call.parkIDs, alertsParkA)
	}
}

// TestListAlertsParkScopedCallerNeverSeesAnotherPark pins the defence-in-depth
// park narrowing: a seat granted on park A must not be handed park B's rows even
// if a future producer routes too broadly.
func TestListAlertsParkScopedCallerNeverSeesAnotherPark(t *testing.T) {
	repo := &recordingAlertsRepo{}
	service := NewService(repo)
	ctx := alertsContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: alertsParkA,
	})

	if _, err := service.ListAlerts(ctx, domain.Actor{
		TenantID: alertsTenant, UserID: "director-park-a",
		Roles: []string{permissions.RoleGrowthDirector},
	}, "", 0); err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}

	call := repo.calls[0]
	if call.tenantWide {
		t.Fatal("park-scoped director was read tenant-wide")
	}
	for _, parkID := range call.parkIDs {
		if parkID == alertsParkB {
			t.Fatalf("park scope leaked %s to a director granted only on %s", alertsParkB, alertsParkA)
		}
	}
}

// TestListAlertsGrowthDirectorHoldsBothCapabilities guards the intentional
// double grant. The Growth Director holds BOTH weighing.execute and
// weighing.monitor; an AND gate over the three weighing capabilities would deny
// them (they hold no weighing.plan), and treating "also an executor" as
// disqualifying would cost them the upstream view.
func TestListAlertsGrowthDirectorHoldsBothCapabilities(t *testing.T) {
	if !permissions.RoleHasPermission(permissions.RoleGrowthDirector, permissions.WeighingExecute) ||
		!permissions.RoleHasPermission(permissions.RoleGrowthDirector, permissions.WeighingMonitor) {
		t.Fatal("growth_director no longer holds BOTH weighing.execute and weighing.monitor; the alerts gate assumed it does")
	}

	repo := &recordingAlertsRepo{}
	service := NewService(repo)
	ctx := alertsContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: alertsTenant,
	})

	if _, err := service.ListAlerts(ctx, domain.Actor{
		TenantID: alertsTenant, UserID: "director-dinakar",
		Roles: []string{permissions.RoleGrowthDirector},
	}, "", 0); err != nil {
		t.Fatalf("growth director denied the weighing alerts feed: %v", err)
	}
	if !repo.calls[0].tenantWide {
		t.Fatal("tenant-wide growth director was park-narrowed; they legitimately span parks")
	}
}

// TestListAlertsCEOReadsTenantWide covers the seat that holds plan+monitor but
// NOT execute -- the exact combination an AND gate would reject.
func TestListAlertsCEOReadsTenantWide(t *testing.T) {
	if permissions.RoleHasPermission(permissions.RoleCEOInternal, permissions.WeighingExecute) {
		t.Fatal("ceo_internal now holds weighing.execute; the CEO must never reach a scan surface")
	}

	repo := &recordingAlertsRepo{}
	service := NewService(repo)
	ctx := alertsContext(permissions.ActiveGrant{
		Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: alertsTenant,
	})

	if _, err := service.ListAlerts(ctx, domain.Actor{
		TenantID: alertsTenant, UserID: "ceo", Roles: []string{permissions.RoleCEOInternal},
	}, "", 0); err != nil {
		t.Fatalf("CEO denied the weighing alerts feed: %v", err)
	}
	if !repo.calls[0].tenantWide {
		t.Fatal("tenant-wide CEO was park-narrowed")
	}
}

// TestListAlertsDeniesNonWeighingRole proves the gate is weighing-scoped. A
// health director has real, tenant-wide authority elsewhere and must still be
// refused: alerts are scoped by feature AND role.
func TestListAlertsDeniesNonWeighingRole(t *testing.T) {
	repo := &recordingAlertsRepo{}
	service := NewService(repo)
	ctx := alertsContext(permissions.ActiveGrant{
		Role: permissions.RoleHealthDirector, ScopeType: "tenant", ScopeID: alertsTenant,
	})

	_, err := service.ListAlerts(ctx, domain.Actor{
		TenantID: alertsTenant, UserID: "health-director",
		Roles: []string{permissions.RoleHealthDirector},
	}, "", 0)
	if !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden for a role with no weighing capability", err)
	}
	if len(repo.calls) != 0 {
		t.Fatal("repository was queried for a caller with no weighing capability")
	}
}

// TestListAlertsGateNeverRequiresVaccination is the regression guard for the
// defect that deleted the previous tab: the weighing feed must not be reachable
// only by someone who also holds the vaccination reads.
func TestListAlertsGateNeverRequiresVaccination(t *testing.T) {
	if permissions.RoleHasPermission(permissions.RoleOperator, permissions.ObligationRead) ||
		permissions.RoleHasPermission(permissions.RoleOperator, permissions.VaccinationRead) {
		t.Skip("operator now holds the vaccination reads; this guard can no longer distinguish the gates")
	}

	repo := &recordingAlertsRepo{}
	service := NewService(repo)
	ctx := alertsContext(permissions.ActiveGrant{
		Role: permissions.RoleOperator, ScopeType: "park", ScopeID: alertsParkA,
	})

	// An operator holds NEITHER ObligationRead nor VaccinationRead. If the gate
	// ever grows a vaccination requirement, this call starts returning 403 --
	// exactly the failure that made the old tab permanently empty.
	if _, err := service.ListAlerts(ctx, domain.Actor{
		TenantID: alertsTenant, UserID: "operator-pramod",
		Roles: []string{permissions.RoleOperator},
	}, "", 0); err != nil {
		t.Fatalf("weighing operator denied the WEIGHING alerts feed: %v -- the gate has picked up a non-weighing requirement", err)
	}
}

// TestListAlertsClampsPageSize keeps the feed inside the mobile list budget: a
// client asking for an unbounded page must be clamped, not obeyed.
func TestListAlertsClampsPageSize(t *testing.T) {
	repo := &recordingAlertsRepo{}
	service := NewService(repo)
	ctx := alertsContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: alertsTenant,
	})
	actor := domain.Actor{
		TenantID: alertsTenant, UserID: "director-dinakar",
		Roles: []string{permissions.RoleGrowthDirector},
	}

	if _, err := service.ListAlerts(ctx, actor, "", 100_000); err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if got := repo.calls[0].limit; got != domain.MaxAlertPageSize {
		t.Fatalf("limit = %d, want clamp to %d", got, domain.MaxAlertPageSize)
	}

	if _, err := service.ListAlerts(ctx, actor, "", 0); err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if got := repo.calls[1].limit; got != domain.AlertPageSize {
		t.Fatalf("default limit = %d, want %d", got, domain.AlertPageSize)
	}
	if domain.AlertPageSize > 20 {
		t.Fatalf("default page %d exceeds the ~20-rows-per-screen mobile list rule", domain.AlertPageSize)
	}
}

// TestListAlertsCopyIsBackendOwned pins that the phone is handed its title and
// empty-state sentence. If these ever empty out, the renderer would have to
// invent copy, which is exactly what the contract forbids.
func TestListAlertsCopyIsBackendOwned(t *testing.T) {
	repo := &recordingAlertsRepo{page: domain.AlertPage{
		Title: domain.AlertFeedTitle, EmptyMessage: domain.AlertFeedEmptyMessage,
	}}
	service := NewService(repo)
	ctx := alertsContext(permissions.ActiveGrant{
		Role: permissions.RoleOperator, ScopeType: "park", ScopeID: alertsParkA,
	})

	page, err := service.ListAlerts(ctx, domain.Actor{
		TenantID: alertsTenant, UserID: "operator-pramod",
		Roles: []string{permissions.RoleOperator},
	}, "", 0)
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if page.Title == "" || page.EmptyMessage == "" {
		t.Fatal("title/empty-state copy is empty; the renderer would have to hardcode it")
	}
}

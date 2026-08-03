package http

import (
	"context"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const (
	conformanceTenant = "40000000-0000-4000-8000-0000000000f1"
	conformancePark   = "40000000-0000-4000-8000-0000000000f2"
	conformanceOther  = "40000000-0000-4000-8000-0000000000f3"
)

// The park-authority set must be at least as wide as the ROUTE GATE it sits behind.
// When it was narrower, the route admitted a role and the park resolver then denied it
// every park -- a blanket 403 park_scope_required on a screen the role is entitled to,
// with nothing in the route table to explain it. This regression only ever hit roles
// nobody had a test for: the module's tests covered Operator/ParkHead/CEO, and it was
// Verifier and the vaccination manager tiers (which hold VaccinationRead/ObligationRead
// but neither TaskExecute, VaccinationOverseeExecution nor VaccinationCampaign) that lost
// their own park's execution list.
//
// The assertion is derived from the route table rather than restated by hand, so widening
// or narrowing either side without the other fails here.
func TestParkAuthoritiesCoverEveryRoleTheReadRoutesAdmit(t *testing.T) {
	readRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/vaccination/execution"},
		{"GET", "/vaccination/schedule"},
		{"GET", "/vaccination/command"},
		{"GET", "/vaccination/sheds"},
		{"GET", "/vaccination/drive-assignments"},
	}
	roles := []string{
		permissions.RoleVerifier,
		permissions.RoleParkHead,
		permissions.RoleOperator,
		permissions.RoleCEOInternal,
		permissions.RoleKey(permissions.TierManager, permissions.VerticalPreventiveCare),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalPreventiveCare),
		permissions.RoleKey(permissions.TierHead, permissions.VerticalPreventiveCare),
		permissions.RoleKey(permissions.TierDirector, permissions.VerticalPreventiveCare),
	}

	for _, rt := range readRoutes {
		route, ok := permissions.Match(rt.method, rt.path)
		if !ok {
			t.Fatalf("route %s %s is not in the protected route table", rt.method, rt.path)
		}
		for _, role := range roles {
			if !permissions.AuthorizeRoute(route, []string{role}) {
				// The route gate itself rejects this role; the park resolver is never reached.
				continue
			}
			t.Run(rt.path+"/"+role, func(t *testing.T) {
				ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
					{Role: role, ScopeType: "park", ScopeID: conformancePark},
				})
				got := authorizedParkFilterVaccinationExecution(ctx, conformanceTenant)
				if !reflect.DeepEqual(got, []string{conformancePark}) {
					t.Fatalf("park-scoped %s is admitted by %s %s but resolves to %#v; it must see its OWN park, not a 403",
						role, rt.method, rt.path, got)
				}
			})
		}
	}
}

// Widening the authority set to match the route gate must NOT re-open the cross-park hole:
// it is still each GRANT's own role that has to carry a vaccination authority, so a grant
// for an unrelated role in another park cannot ride along with a vaccination grant.
func TestParkAuthoritiesStillBindEachGrantsRoleToItsOwnScope(t *testing.T) {
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: conformanceOther},
		{Role: permissions.RoleVerifier, ScopeType: "park", ScopeID: conformancePark},
	})
	got := authorizedParkFilterVaccinationExecution(ctx, conformanceTenant)
	if !reflect.DeepEqual(got, []string{conformancePark}) {
		t.Fatalf("authorized parks = %#v, want only %q; a weighing-only grant must not lend its park to vaccination", got, conformancePark)
	}
}

// A tenant-wide Park Head holds VaccinationOverseeExecution but NOT VaccinationCampaign.
// The handler's tenant-wide test used to accept ONLY VaccinationCampaign, so this actor was
// treated as park-scoped with zero parks. Both halves of the authority now come from one
// definition, so a tenant-wide vaccination role is unrestricted (nil) everywhere.
func TestTenantWideOversightRoleIsUnrestricted(t *testing.T) {
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleParkHead, ScopeType: "tenant", ScopeID: conformanceTenant},
	})
	if got := authorizedParkFilterVaccinationExecution(ctx, conformanceTenant); got != nil {
		t.Fatalf("authorized parks = %#v, want nil (unrestricted) for a tenant-wide oversight role", got)
	}
}

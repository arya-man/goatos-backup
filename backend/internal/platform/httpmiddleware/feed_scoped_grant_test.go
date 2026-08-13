package httpmiddleware

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// THE 2026-08-08 FEED 403s.
//
// A park-scoped operator could open Feed Direction but got 403 on the packing worklist, the
// transport list and the distribution completion -- 13 denials and zero successes on STG, logged
// with roles:"" even though the grant was active and RoleOperator genuinely holds
// FeedPackingRead / FeedTransportRead / FeedDirectionComplete. routeRoles dropped the park grant
// before AuthorizeRoute ever ran, so no permission change could have fixed it. Only tenant-scoped
// principals (Chandrakant, CEO) worked, which was incidental rather than intended.
//
// The three routes now clamp park_id to the caller's own grant in their handlers, which is the
// precondition for being admitted here.
func TestParkScopedOperatorKeepsItsGrantOnTheFeedFieldRoutes(t *testing.T) {
	parkGrant := permissions.ActiveGrant{
		Role: permissions.RoleOperator, ScopeType: "park",
		ScopeID: "86000000-0000-4000-8000-000000000701",
	}
	const tenant = "86000000-0000-4000-8000-000000000001"

	for _, tc := range []struct{ method, path string }{
		{"GET", "/feed-packing/worklist"},
		{"GET", "/feed-transport/tasks"},
		{"POST", "/feed-direction/distribution/complete"},
		{"POST", "/feed-direction/packing/complete"},
		{"GET", "/feed-direction/preview"}, // already admitted; guards against a regression
	} {
		route, ok := permissions.Match(tc.method, tc.path)
		if !ok {
			t.Fatalf("%s %s is not a registered route", tc.method, tc.path)
		}
		roles := routeRoles(route, []permissions.ActiveGrant{parkGrant}, tenant)
		if len(roles) != 1 || roles[0] != permissions.RoleOperator {
			t.Fatalf("%s %s resolved roles=%v; a park-scoped operator must keep its grant here, "+
				"otherwise the route 403s with roles:\"\" no matter what permissions the role holds",
				tc.method, tc.path, roles)
		}
		if !permissions.AuthorizeRoute(route, roles) {
			t.Fatalf("%s %s: operator holds the route's permission but was not authorized", tc.method, tc.path)
		}
	}
}

// Widening the allowlist must NOT become a general park-grant amnesty. An admin/config route that
// does no park clamping still requires a tenant-wide grant -- otherwise any park operator could
// author the ration grid.
func TestScopedGrantAdmissionStaysNarrow(t *testing.T) {
	parkGrant := permissions.ActiveGrant{
		Role: permissions.RoleOperator, ScopeType: "park",
		ScopeID: "86000000-0000-4000-8000-000000000701",
	}
	const tenant = "86000000-0000-4000-8000-000000000001"

	for _, tc := range []struct{ method, path string }{
		{"GET", "/feed-config/ration-rates"},
		{"GET", "/admin-web/bootstrap"},
	} {
		route, ok := permissions.Match(tc.method, tc.path)
		if !ok {
			continue // not registered in this build; nothing to assert
		}
		if roles := routeRoles(route, []permissions.ActiveGrant{parkGrant}, tenant); len(roles) != 0 {
			t.Fatalf("%s %s admitted a PARK grant (%v); only the clamped field routes may do that",
				tc.method, tc.path, roles)
		}
	}
}

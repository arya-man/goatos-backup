package permissions

import "testing"

// Route coherence for the Weighing FCR tab read and the sale-price vocabulary it prices gain at.
// Both render on the Weights screen, so both are reachable by exactly the actors who can read that
// screen and by nobody else.
func TestAdminGrowthFCRRoutesAreMonitorGated(t *testing.T) {
	for _, tc := range []struct{ pattern, op string }{
		{"/growth-director/fcr", "adminGetGrowthDirectorFCR"},
		{"/growth-director/sale-prices", "adminGetGrowthSalePrices"},
	} {
		route, ok := Match("GET", tc.pattern)
		if !ok {
			t.Fatalf("%s is not a registered protected route", tc.pattern)
		}
		if route.OperationID != tc.op {
			t.Fatalf("%s resolved to %q, want %s", tc.pattern, route.OperationID, tc.op)
		}
		for _, role := range []string{RoleGrowthDirector, RoleCEOInternal} {
			if !AuthorizeRoute(route, []string{role}) {
				t.Errorf("%s must reach %s; the Weights screen renders the FCR tab for them", role, tc.pattern)
			}
		}
		for _, role := range []string{RoleOperator, RoleVerifier} {
			if AuthorizeRoute(route, []string{role}) {
				t.Errorf("%s must not reach %s", role, tc.pattern)
			}
		}
	}
}

// The Assumptions drawer (maintainer decision 2026-09-19): every Weights reader may READ the
// figures; only weighing.assumptions.write may CHANGE them. The CEO holds it on the role; the
// growth_director does NOT (a /people Configure tick grants it per person, which the
// capability-parity test proves); operators and verifiers reach neither.
func TestGrowthAssumptionsWriteIsItsOwnCapability(t *testing.T) {
	read, ok := Match("GET", "/growth-director/assumptions")
	if !ok || read.OperationID != "adminGetGrowthAssumptions" {
		t.Fatalf("GET /growth-director/assumptions = %+v, %v", read, ok)
	}
	write, ok := Match("PUT", "/growth-director/assumptions")
	if !ok || write.OperationID != "adminPutGrowthAssumptions" {
		t.Fatalf("PUT /growth-director/assumptions = %+v, %v", write, ok)
	}
	if !AuthorizeRoute(read, []string{RoleGrowthDirector}) || !AuthorizeRoute(read, []string{RoleCEOInternal}) {
		t.Fatalf("readers of the Weights screen must read the assumptions")
	}
	if !AuthorizeRoute(write, []string{RoleCEOInternal}) {
		t.Fatalf("the CEO must hold the write on the role")
	}
	if AuthorizeRoute(write, []string{RoleGrowthDirector}) {
		t.Fatalf("growth_director must NOT hold the write on the role: it is granted per person by a Configure tick")
	}
	for _, role := range []string{RoleOperator, RoleVerifier} {
		if AuthorizeRoute(read, []string{role}) || AuthorizeRoute(write, []string{role}) {
			t.Errorf("%s must reach neither assumptions route", role)
		}
	}
	held := PermissionsForAssignments([]ModuleAssignment{{Module: "weighing", Surface: SurfaceWeb, Capabilities: []string{LevelConfigure}}})
	if allowed, decidable := AuthorizePermissionSet(write, held); !decidable || !allowed {
		t.Fatalf("a Configure tick on weighing must carry the write: %v %v", allowed, decidable)
	}
}

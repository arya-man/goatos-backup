package permissions

import "testing"

// TestSalesRolePermissions pins who may read and write the sales module.
//
// CEO/CxO holds both by the founder/builder visibility invariant. The procurement manager runs
// the BUYING desk (vendor register) and deliberately does NOT get the selling side; the sales
// vertical's own tiers do. Operators and park heads hold ProcurementRead for intake screens and
// must not inherit revenue/buyer visibility from that.
func TestSalesRolePermissions(t *testing.T) {
	if !RolesAuthorize([]string{RoleCEOInternal}, []string{SalesRead, SalesWrite}, false) {
		t.Fatal("CEO/CXO should read and write sales")
	}
	for _, role := range []string{RoleOperator, RoleParkHead, RolePCDirector, RoleGrowthDirector, RoleVerifier, RoleProcurementManager} {
		if RolesAuthorize([]string{role}, []string{SalesRead}, false) {
			t.Fatalf("%s must not read sales", role)
		}
		if RolesAuthorize([]string{role}, []string{SalesWrite}, false) {
			t.Fatalf("%s must not write sales", role)
		}
	}
}

// TestSalesVerticalTiersHoldTheModule pins the org-role composition: the sales vertical's
// director/head/manager run the desk and record sales; the assistant manager reads the board
// only. No other vertical's tiers gain a sales permission from their tier.
func TestSalesVerticalTiersHoldTheModule(t *testing.T) {
	for _, role := range []string{
		RoleKey(TierDirector, VerticalSales),
		RoleKey(TierHead, VerticalSales),
		RoleKey(TierManager, VerticalSales),
	} {
		if !RolesAuthorize([]string{role}, []string{SalesRead, SalesWrite}, false) {
			t.Fatalf("%s should read and write sales", role)
		}
	}
	am := RoleKey(TierAssistantManager, VerticalSales)
	if !RolesAuthorize([]string{am}, []string{SalesRead}, false) {
		t.Fatalf("%s should read sales", am)
	}
	if RolesAuthorize([]string{am}, []string{SalesWrite}, false) {
		t.Fatalf("%s must not write the ledger", am)
	}
	// The one-module-one-director split: another vertical's director gets nothing.
	for _, role := range []string{
		RoleKey(TierDirector, VerticalFeed),
		RoleKey(TierDirector, VerticalProcurement),
		RoleKey(TierManager, VerticalHealth),
	} {
		if RolesAuthorize([]string{role}, []string{SalesRead}, false) {
			t.Fatalf("%s must not gain sales from its tier", role)
		}
	}
}

// TestSalesRoutesAreGatedOnTheDedicatedPermissions pins the route table entries, so the mux
// registration and the permission table cannot drift apart on who gates what.
func TestSalesRoutesAreGatedOnTheDedicatedPermissions(t *testing.T) {
	want := map[string][]string{
		"GET /sales/overview": {SalesRead},
		"GET /sales/deals":    {SalesRead},
		"POST /sales/deals":   {SalesWrite},
	}
	found := map[string]bool{}
	for _, route := range ProtectedRoutes() {
		key := route.Method + " " + route.Pattern
		expected, ok := want[key]
		if !ok {
			continue
		}
		found[key] = true
		if len(route.Permissions) != len(expected) || route.Permissions[0] != expected[0] {
			t.Fatalf("%s gated on %v want %v", key, route.Permissions, expected)
		}
	}
	for key := range want {
		if !found[key] {
			t.Fatalf("route %s missing from the permission table", key)
		}
	}
}

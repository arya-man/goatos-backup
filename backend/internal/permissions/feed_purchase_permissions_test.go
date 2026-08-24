package permissions

import "testing"

// TestFeedPurchaseRolePermissions pins who may read and record feed purchases.
//
// CEO/CxO holds both by the founder/builder visibility invariant. The procurement desk BUYS, so it
// holds both. The Feed Director owns what the farm feeds and is accountable for the stock cards
// these loads are counted from, so it READS -- and deliberately does not write, the same read/write
// split that keeps that role out of FeedDirectionComplete.
//
// The operator and park_head rows are the leak this pins: both hold ProcurementRead for the
// source-entry screens they work, and the purchase ledger carries supplier prices and payment
// state. Gating this module on ProcurementRead would hand it to both.
func TestFeedPurchaseRolePermissions(t *testing.T) {
	for _, role := range []string{RoleCEOInternal, RoleProcurementDirector, RoleProcurementManager} {
		if !RolesAuthorize([]string{role}, []string{FeedPurchaseRead, FeedPurchaseWrite}, false) {
			t.Fatalf("%s should read and record feed purchases", role)
		}
	}

	if !RolesAuthorize([]string{RoleFeedDirector}, []string{FeedPurchaseRead}, false) {
		t.Fatal("the feed director should read the purchase ledger the stock cards are built from")
	}
	if RolesAuthorize([]string{RoleFeedDirector}, []string{FeedPurchaseWrite}, false) {
		t.Fatal("the feed director must not record purchases: buying is the procurement desk's job")
	}

	for _, role := range []string{RoleOperator, RoleParkHead, RolePCDirector, RoleGrowthDirector, RoleVerifier} {
		if !RolesAuthorize([]string{role}, []string{ProcurementRead}, false) && (role == RoleOperator || role == RoleParkHead) {
			t.Fatalf("%s is expected to hold ProcurementRead -- otherwise this test proves nothing", role)
		}
		if RolesAuthorize([]string{role}, []string{FeedPurchaseRead}, false) {
			t.Fatalf("%s must not read the feed purchase ledger", role)
		}
		if RolesAuthorize([]string{role}, []string{FeedPurchaseWrite}, false) {
			t.Fatalf("%s must not record feed purchases", role)
		}
	}
}

// TestFeedPurchaseRoutesAreGatedOnTheDedicatedPermissions pins the route table entries, so the mux
// registration and the permission table cannot drift apart on who gates what. The options route is
// a GET serving the ENTRY FORM's vocabulary and rides the READ permission: a principal who may see
// the ledger may see which feeds and farms it is keyed by.
func TestFeedPurchaseRoutesAreGatedOnTheDedicatedPermissions(t *testing.T) {
	want := map[string][]string{
		"GET /procurement/feed-purchases":        {FeedPurchaseRead},
		"POST /procurement/feed-purchases":       {FeedPurchaseWrite},
		"GET /procurement/feed-purchase-options": {FeedPurchaseRead},
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

package permissions

import "testing"

// TestProcurementDeskReachesThePhoneAndTheProofHandshake pins the 2026-09-03 maintainer decision
// behind the Vendors phone module: the procurement director and manager can open the app, upload
// a vendor voice note through the proof handshake, and play one back -- while a role with no
// vendor authority still cannot use vendor write as a way into the proof routes.
func TestProcurementDeskReachesThePhoneAndTheProofHandshake(t *testing.T) {
	for _, role := range []string{RoleProcurementDirector, RoleProcurementManager, RoleCEOInternal} {
		if !RoleHasPermission(role, AppBootstrap) {
			t.Fatalf("%s must hold app.bootstrap for the Vendors phone module", role)
		}
		for _, target := range []struct{ method, path string }{
			{"POST", "/app/proofs/uploads"},
			{"PUT", "/app/proofs/98000000-0000-4000-8000-000000000001/upload"},
			{"POST", "/app/proofs/98000000-0000-4000-8000-000000000001/complete"},
			{"GET", "/app/proofs/98000000-0000-4000-8000-000000000001/download"},
		} {
			route, ok := Match(target.method, target.path)
			if !ok {
				t.Fatalf("%s %s is not registered", target.method, target.path)
			}
			authorized := (len(route.Permissions) > 0 && RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly)) ||
				RolesAuthorizeAny([]string{role}, route.AnyPermissions)
			if !authorized {
				t.Fatalf("%s must authorize %s %s for the vendor voice note", role, target.method, target.path)
			}
		}
	}
	// feed_director reads feed purchases on the web and holds no vendor authority: the phone
	// Vendors module does not open for it through this change. It DOES reach the proof
	// handshake -- through leadership_tasks.raise (task attachments, 2026-09-04), never through
	// vendor.write -- so the assertion is on the lever, not the route.
	if RoleHasPermission(RoleFeedDirector, VendorWrite) || RoleHasPermission(RoleFeedDirector, VendorRead) {
		t.Fatal("feed_director must not gain vendor authority through the voice-note widening")
	}
}

package permissions

import "testing"

// The in-app notification centre is a SELF-SCOPED surface: both routes serve the caller
// their own notifications, filtered inside the query by the caller's own resolved
// workforce_member_id, and neither route has any parameter that names another person.
//
// It is therefore gated exactly like the other self-scoped app routes (/app/me,
// /app/config, the caller's own leave requests): on AppBootstrap, which every authenticated
// app principal holds. The mistake this test exists to prevent is gating the bell on a
// module or leadership permission -- an operator who is pushed a rework notification and
// then 403s trying to read it is precisely the defect the notification centre exists to fix.
func TestNotificationCentreRoutesRideAppBootstrapNotALeadershipPermission(t *testing.T) {
	targets := []struct{ method, path string }{
		{"GET", "/app/notifications"},
		{"POST", "/app/notifications/read"},
	}
	for _, target := range targets {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered in the route table -- it would serve UNGATED", target.method, target.path)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != AppBootstrap {
			t.Errorf("%s %s must be gated on exactly [%s], got Permissions=%v AnyPermissions=%v",
				target.method, target.path, AppBootstrap, route.Permissions, route.AnyPermissions)
		}
		if route.AdminOnly {
			t.Errorf("%s %s must not be admin-only: every worker reads their own notifications", target.method, target.path)
		}
		// The rank and file must reach their own bell. An operator and a verifier hold no
		// leadership permission at all, so if either is refused the gate is wrong.
		for _, role := range []string{RoleOperator, RoleVerifier, RoleParkHead, RoleCEOInternal, RolePCDirector} {
			if !RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
				t.Errorf("%s must authorize %s %s -- anyone who can be pushed a notification must be able to read it", role, target.method, target.path)
			}
		}
		// And it must NOT have been widened to an unauthenticated/permissionless caller.
		if RolesAuthorize([]string{}, route.Permissions, route.AdminOnly) {
			t.Errorf("%s %s must not authorize a caller holding no role at all", target.method, target.path)
		}
		for _, leadership := range []string{LeadershipTasksRead, LeadershipTasksRaise, LeadershipTasksAct} {
			for _, held := range route.Permissions {
				if held == leadership {
					t.Errorf("%s %s must not ride %s: these are the caller's OWN notifications, not a leadership surface", target.method, target.path, leadership)
				}
			}
		}
	}
}

// The two patterns are distinct routes, not one prefix: the read collection and the
// mark-as-read write must never resolve to each other's entry.
func TestNotificationCentreReadWriteRoutesAreDistinctEntries(t *testing.T) {
	list, _ := Match("GET", "/app/notifications")
	read, _ := Match("POST", "/app/notifications/read")
	if list.OperationID != "listAppNotifications" {
		t.Errorf("GET /app/notifications resolved to %q", list.OperationID)
	}
	if read.OperationID != "markAppNotificationsRead" {
		t.Errorf("POST /app/notifications/read resolved to %q", read.OperationID)
	}
	// A GET of the write path, and a POST of the collection, are not routes this module
	// serves; if either ever matches, a pattern above grew a wildcard it should not have.
	if route, ok := Match("GET", "/app/notifications/read"); ok && route.OperationID == "listAppNotifications" {
		t.Error("GET /app/notifications must not match the mark-as-read path")
	}
}

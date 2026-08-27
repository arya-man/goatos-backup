package permissions

import "testing"

// AuthorizePermissionSet decides a route from a principal's RESOLVED permissions
// instead of their roles (per-person access, maintainer decision 2026-08-24). It is
// the seam every authenticated request now passes through, so its agreement with the
// role path is the property that matters: the route rules did not change, only the
// source of the answer.

func TestPermissionSetAgreesWithTheRolePathOnEveryRoute(t *testing.T) {
	// For each real route and each registered role, the permission-set decision must
	// equal the role decision when the "permission set" IS that role's permissions.
	// A disagreement means the two paths read the same route rule differently, which
	// is how one surface silently grants what the other refuses.
	for _, route := range ProtectedRoutes() {
		for role, perms := range rolePermissions {
			held := make([]string, 0, len(perms))
			for p := range perms {
				held = append(held, p)
			}
			viaRole := AuthorizeRoute(route, []string{role})
			viaSet, decidable := AuthorizePermissionSet(route, held)
			if !decidable {
				// AdminOnly routes are deliberately not decidable from a permission set;
				// they short-circuit to a product-admin ROLE check.
				if !route.AdminOnly {
					t.Errorf("%s: not decidable but route is not AdminOnly", route.OperationID)
				}
				continue
			}
			if viaSet != viaRole {
				t.Errorf("%s / %s: permission set says %v, role path says %v",
					route.OperationID, role, viaSet, viaRole)
			}
		}
	}
}

func TestPermissionSetRefusesWhatIsNotHeld(t *testing.T) {
	route := Route{OperationID: "t", Permissions: []string{OperatorsManageCapability}}

	if allowed, _ := AuthorizePermissionSet(route, []string{OperatorsRead}); allowed {
		t.Fatal("granted a route whose permission the principal does not hold")
	}
	if allowed, _ := AuthorizePermissionSet(route, []string{OperatorsRead, OperatorsManageCapability}); !allowed {
		t.Fatal("refused a route whose permission the principal holds")
	}
	// An empty set grants nothing. This is the shape a person with no access rows
	// resolves to, and the middleware treats it as "fall back to the role path" rather
	// than as a denial -- but the function itself must still refuse.
	if allowed, _ := AuthorizePermissionSet(route, nil); allowed {
		t.Fatal("an empty permission set granted a route")
	}
}

func TestPermissionSetAndsRequiredAndOrsAny(t *testing.T) {
	both := Route{OperationID: "and", Permissions: []string{OperatorsRead, RosterRead}}
	if allowed, _ := AuthorizePermissionSet(both, []string{OperatorsRead}); allowed {
		t.Fatal("Permissions must be ANDed; holding one of two granted the route")
	}
	if allowed, _ := AuthorizePermissionSet(both, []string{OperatorsRead, RosterRead}); !allowed {
		t.Fatal("holding both required permissions did not grant the route")
	}

	either := Route{OperationID: "or", AnyPermissions: []string{CountsRead, CountsWrite}}
	if allowed, _ := AuthorizePermissionSet(either, []string{CountsWrite}); !allowed {
		t.Fatal("AnyPermissions must be ORed; holding one of two refused the route")
	}
	if allowed, _ := AuthorizePermissionSet(either, []string{GoatRead}); allowed {
		t.Fatal("holding neither of the AnyPermissions granted the route")
	}
}

// TestAdminOnlyIsNeverDecidedFromAPermissionSet pins the one case the permission path
// must refuse to answer. AdminOnly ignores the permission lists entirely and checks a
// product-admin ROLE, so answering it from a permission set would 403 every admin-only
// route for everyone -- silently, and only for principals who have access rows.
func TestAdminOnlyIsNeverDecidedFromAPermissionSet(t *testing.T) {
	route := Route{OperationID: "admin", Permissions: []string{OperatorsRead}, AdminOnly: true}
	allowed, decidable := AuthorizePermissionSet(route, []string{OperatorsRead})
	if decidable {
		t.Fatal("an AdminOnly route reported itself decidable from a permission set")
	}
	if allowed {
		t.Fatal("an AdminOnly route was granted from a permission set")
	}
}

// TestAccessEditorSplitsReadFromWrite is the concrete authority this rewrite adds:
// opening the editor is the directory's own read, changing it is a separate authority
// that can grant every other permission in the catalog, including itself.
func TestAccessEditorSplitsReadFromWrite(t *testing.T) {
	var read, write Route
	for _, route := range ProtectedRoutes() {
		switch route.OperationID {
		case "getWorkforcePersonAccess":
			read = route
		case "saveWorkforcePersonAccess":
			write = route
		}
	}
	if read.OperationID == "" || write.OperationID == "" {
		t.Fatal("the access routes are not registered; the editor would be unreachable")
	}

	viewer := []string{OperatorsRead}
	if allowed, _ := AuthorizePermissionSet(read, viewer); !allowed {
		t.Error("a principal holding operators.read cannot open the access editor")
	}
	if allowed, _ := AuthorizePermissionSet(write, viewer); allowed {
		t.Error("operators.read alone could CHANGE access -- the write must need its own authority")
	}
	// Creating a person is deliberately NOT enough to change what everyone may do.
	if allowed, _ := AuthorizePermissionSet(write, []string{OperatorsRead, OperatorsWrite}); allowed {
		t.Error("operators.write (create a person) could change access; it must require operators.manage_capability")
	}
	if allowed, _ := AuthorizePermissionSet(write, []string{OperatorsManageCapability}); !allowed {
		t.Error("operators.manage_capability cannot change access")
	}
}

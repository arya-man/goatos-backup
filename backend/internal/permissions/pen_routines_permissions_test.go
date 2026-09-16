package permissions

import "testing"

// TestPenRoutinesExecuteIsParkHeadsAndDirectorsNeverCEO pins the 2026-09-16 decision
// (docs/decisions/pen-routines.md): pen_routines.execute -- the phone module and its routes --
// is held by park_head and the six director roles (the pen-visit precedent); ceo_internal holds
// read + configure and NOT execute, because the CXO desk writes the rule and does not walk
// pens (the toxin rule). Operator, verifier and the per-person roles hold nothing.
//
// Mutation-tested when written: granting PenRoutinesExecute to RoleCEOInternal, and removing it
// from RoleParkHead, each turn this red.
func TestPenRoutinesExecuteIsParkHeadsAndDirectorsNeverCEO(t *testing.T) {
	for _, role := range []string{RoleParkHead, RolePCDirector, RoleGrowthDirector, RoleFeedDirector, RoleHealthDirector, RoleBreedingDirector, RoleProcurementDirector} {
		if !RoleHasPermission(role, PenRoutinesExecute) {
			t.Errorf("%s must hold pen_routines.execute", role)
		}
		if !RoleHasPermission(role, PenRoutinesRead) {
			t.Errorf("%s must hold pen_routines.read", role)
		}
	}
	for _, role := range []string{RoleCEOInternal, RoleOperator, RoleVerifier, RoleCountsApprover, RoleToxinTester, RoleProcurementManager} {
		if RoleHasPermission(role, PenRoutinesExecute) {
			t.Errorf("%s must NOT hold pen_routines.execute", role)
		}
	}
	if !RoleHasPermission(RoleCEOInternal, PenRoutinesRead) || !RoleHasPermission(RoleCEOInternal, PenRoutinesConfigure) {
		t.Error("ceo_internal must hold pen_routines.read and pen_routines.configure")
	}
	for _, role := range []string{RoleParkHead, RolePCDirector, RoleOperator, RoleVerifier} {
		if RoleHasPermission(role, PenRoutinesConfigure) {
			t.Errorf("%s must NOT hold pen_routines.configure", role)
		}
	}
	// The module's ticks: Do carries execute, View and Configure do not.
	for _, tc := range []struct {
		level   string
		execute bool
		config  bool
	}{{LevelView, false, false}, {LevelDo, true, false}, {LevelConfigure, false, true}} {
		perms := PermissionsForAssignments([]ModuleAssignment{{Surface: SurfaceMobile, Module: "pen_routines", Capabilities: []string{tc.level}}})
		gotExecute, gotConfig := false, false
		for _, p := range perms {
			if p == PenRoutinesExecute {
				gotExecute = true
			}
			if p == PenRoutinesConfigure {
				gotConfig = true
			}
		}
		if gotExecute != tc.execute || gotConfig != tc.config {
			t.Errorf("pen_routines tick %s carries execute=%v configure=%v, want %v/%v", tc.level, gotExecute, gotConfig, tc.execute, tc.config)
		}
	}
}

// TestPenRoutineRoutesAreGated pins the route table: every phone route rides
// pen_routines.execute alone (a park head passes, a CXO and an operator are refused), every
// admin read rides pen_routines.read, every admin write rides pen_routines.configure (the CXO
// passes, a park head is refused), and the proof upload handshake ORs execute in so a
// routine's captures can finish uploading.
func TestPenRoutineRoutesAreGated(t *testing.T) {
	const id = "98000000-0000-4000-8000-000000000003"
	for _, target := range []struct{ method, path string }{
		{"GET", "/app/pen-routines"},
		{"GET", "/app/pen-routines/" + id},
		{"POST", "/app/pen-routines/" + id + "/presence"},
		{"POST", "/app/pen-routines/" + id + "/submit"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if len(route.AnyPermissions) != 0 || len(route.Permissions) != 1 || route.Permissions[0] != PenRoutinesExecute {
			t.Errorf("%s %s must ride pen_routines.execute alone, got all=%v any=%v", target.method, target.path, route.Permissions, route.AnyPermissions)
		}
		if !RolesAuthorize([]string{RoleParkHead}, route.Permissions, false) {
			t.Errorf("a park head must be authorized for %s %s", target.method, target.path)
		}
		for _, role := range []string{RoleOperator, RoleCEOInternal, RoleVerifier} {
			if RolesAuthorize([]string{role}, route.Permissions, false) {
				t.Errorf("%s must NOT authorize %s %s", role, target.method, target.path)
			}
		}
	}
	for _, target := range []struct{ method, path string }{
		{"GET", "/admin/pen-routines"},
		{"GET", "/admin/pen-routines/catalog"},
		{"GET", "/admin/pen-routines/tasks"},
		{"GET", "/admin/pen-routines/" + id},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != PenRoutinesRead {
			t.Errorf("%s %s must ride pen_routines.read, got %v", target.method, target.path, route.Permissions)
		}
		if !RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, false) {
			t.Errorf("the CXO must read %s %s", target.method, target.path)
		}
	}
	for _, target := range []struct{ method, path string }{
		{"POST", "/admin/pen-routines"},
		{"PUT", "/admin/pen-routines/" + id},
		{"POST", "/admin/pen-routines/" + id + "/status"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != PenRoutinesConfigure {
			t.Errorf("%s %s must ride pen_routines.configure, got %v", target.method, target.path, route.Permissions)
		}
		if !RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, false) {
			t.Errorf("the CXO must be authorized for %s %s", target.method, target.path)
		}
		for _, role := range []string{RoleParkHead, RolePCDirector, RoleOperator, RoleVerifier} {
			if RolesAuthorize([]string{role}, route.Permissions, false) {
				t.Errorf("%s must NOT authorize %s %s", role, target.method, target.path)
			}
		}
	}
	for _, target := range []struct{ method, path string }{
		{"POST", "/app/proofs/uploads"},
		{"POST", "/app/proofs/" + id + "/complete"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		found := false
		for _, p := range route.AnyPermissions {
			if p == PenRoutinesExecute {
				found = true
			}
		}
		if !found {
			t.Errorf("%s %s must OR in pen_routines.execute so a routine's captures can finish uploading", target.method, target.path)
		}
	}
}

package permissions

import "testing"

// TestPenVisitsExecuteIsDirectorsNeverCEO pins the 2026-09-07 maintainer decision: the Tasks
// module's "For me" tab (pen visits the kernel owes a park head) rides pen_visits.execute, held
// by the director roles -- the same jobs that raise tasks, and the jobs a park's visits are
// configured against (CBE -> Dinakar, CPT -> Chandrakant) -- and by nobody else. The CXO desk
// answers asks, it does not walk pens; an operator, park head, verifier and the per-person
// roles hold nothing.
//
// Mutation-tested when written: granting PenVisitsExecute to RoleCEOInternal, and removing it
// from RoleGrowthDirector, each turn this red.
func TestPenVisitsExecuteIsDirectorsNeverCEO(t *testing.T) {
	for _, role := range []string{RolePCDirector, RoleGrowthDirector, RoleFeedDirector, RoleHealthDirector, RoleBreedingDirector, RoleProcurementDirector} {
		if !RoleHasPermission(role, PenVisitsExecute) {
			t.Errorf("%s must hold pen_visits.execute", role)
		}
	}
	for _, role := range []string{RoleCEOInternal, RoleOperator, RoleParkHead, RoleVerifier, RoleCountsApprover, RoleToxinTester, RoleProcurementManager} {
		if RoleHasPermission(role, PenVisitsExecute) {
			t.Errorf("%s must NOT hold pen_visits.execute", role)
		}
	}
	// The module's Do tick carries it, so a person ticked Do on Tasks from /people gets the tab
	// with no code change; View (a CXO) and Oversee (an assignable CXO) do not.
	for _, tc := range []struct {
		level string
		want  bool
	}{{LevelView, false}, {LevelDo, true}, {LevelOversee, false}} {
		perms := PermissionsForAssignments([]ModuleAssignment{{Surface: SurfaceMobile, Module: "leadership_tasks", Capabilities: []string{tc.level}}})
		got := false
		for _, p := range perms {
			if p == PenVisitsExecute {
				got = true
			}
		}
		if got != tc.want {
			t.Errorf("leadership_tasks tick %s carries pen_visits.execute = %v, want %v", tc.level, got, tc.want)
		}
	}
}

// TestPenVisitRoutesAreGatedOnPenVisitsExecute pins the route table: every pen-visit route,
// and the proof upload handshake the visit video needs, admits a director and refuses a CXO
// and an operator.
func TestPenVisitRoutesAreGatedOnPenVisitsExecute(t *testing.T) {
	const id = "98000000-0000-4000-8000-000000000002"
	for _, target := range []struct{ method, path string }{
		{"GET", "/app/pen-visits"},
		{"GET", "/app/pen-visits/" + id},
		{"POST", "/app/pen-visits/" + id + "/submit"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if !RolesAuthorize([]string{RoleGrowthDirector}, route.Permissions, route.AdminOnly) {
			t.Errorf("growth_director must authorize %s %s", target.method, target.path)
		}
		for _, role := range []string{RoleCEOInternal, RoleOperator, RoleParkHead, RoleVerifier} {
			if RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
				t.Errorf("%s must NOT authorize %s %s", role, target.method, target.path)
			}
		}
	}
	// A director who holds ONLY pen_visits.execute on a hypothetical narrower grant must still
	// finish a proof upload: the upload routes OR the permission in, the toxin.execute lever.
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
			if p == PenVisitsExecute {
				found = true
			}
		}
		if !found {
			t.Errorf("%s %s must OR in pen_visits.execute so the visit video can finish uploading", target.method, target.path)
		}
	}
}

package permissions

import "testing"

// TestBreedingDirectorPlansTrimmingOnly pins the maintainer decision of 2026-09-04: the
// Breeding Director's desk plans HOOF and HAIR TRIMMING through the category-scoped
// pc_care.plan_trimming, and holds neither the whole-module pc_care.plan (deworming / ticks
// removal stay CEO-planned) nor pc_care.execute (a planner must not film the work they
// planned). Three edges, each deliberate:
//
//  1. breeding_director holds exactly the permissions the desk needs and no execute.
//  2. pc_care.plan still lives on ceo_internal ALONE -- the carve-out did not widen it.
//  3. The planner routes admit the role at the route table; the category refusal is the
//     service's (pccare/app TestCreateTaskHonoursTheTrimmingPlannerCarveOut), because a
//     route cannot see a body.
func TestBreedingDirectorPlansTrimmingOnly(t *testing.T) {
	perms := rolePermissions[RoleBreedingDirector]
	for _, want := range []string{PCCarePlanTrimming, PCCareMonitor, AppBootstrap, AdminWebBootstrap, LocationsRead, OperatorsRead, RosterRead} {
		if _, ok := perms[want]; !ok {
			t.Errorf("breeding_director must hold %s", want)
		}
	}
	for _, mustNot := range []string{PCCarePlan, PCCareExecute, PCCareStockApprove, PCCareOverseeOperators} {
		if _, ok := perms[mustNot]; ok {
			t.Errorf("breeding_director must NOT hold %s", mustNot)
		}
	}
	if len(perms) != 7 {
		t.Errorf("breeding_director carries %d permissions, want exactly 7 -- widen this test deliberately, never by accident", len(perms))
	}

	// The whole-module plan is still the CEO's alone.
	for _, role := range []string{RoleBreedingDirector, RolePCDirector, RoleGrowthDirector, RoleFeedDirector, RoleHealthDirector, RoleParkHead, RoleOperator, RoleVerifier, RoleCountsApprover, RoleToxinTester} {
		if RoleHasPermission(role, PCCarePlan) {
			t.Errorf("%s must not hold pc_care.plan -- whole-module planning is CEO-only", role)
		}
	}
	if !RoleHasPermission(RoleCEOInternal, PCCarePlan) {
		t.Fatal("ceo_internal must keep pc_care.plan")
	}
	// And the trimming carve-out is the Breeding Director's alone: no other job inherits it.
	for _, role := range []string{RolePCDirector, RoleGrowthDirector, RoleFeedDirector, RoleHealthDirector, RoleParkHead, RoleOperator, RoleVerifier, RoleCountsApprover, RoleToxinTester, RoleProcurementDirector} {
		if RoleHasPermission(role, PCCarePlanTrimming) {
			t.Errorf("%s must not hold pc_care.plan_trimming -- the carve-out is the Breeding Director's desk", role)
		}
	}

	// The planner routes really resolve for the role, and the execute routes really refuse it.
	for _, target := range []struct{ method, path string }{
		{"GET", "/app/pc-care/planner/catalog"},
		{"GET", "/app/pc-care/planner/parks/98000000-0000-4000-8000-000000000001/sheds"},
		{"POST", "/app/pc-care/tasks"},
		{"POST", "/app/pc-care/tasks/98000000-0000-4000-8000-000000000001/cancel"},
		{"GET", "/app/pc-care/tasks"},
		{"GET", "/app/pc-care/tasks/98000000-0000-4000-8000-000000000001"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if !AuthorizeRoute(route, []string{RoleBreedingDirector}) {
			t.Errorf("breeding_director must reach %s %s", target.method, target.path)
		}
	}
	for _, target := range []struct{ method, path string }{
		{"GET", "/app/pc-care/worklist"},
		{"POST", "/app/pc-care/tasks/98000000-0000-4000-8000-000000000001/animals"},
		{"POST", "/app/pc-care/tasks/98000000-0000-4000-8000-000000000001/submit"},
		{"POST", "/app/pc-care/tasks/98000000-0000-4000-8000-000000000001/stock-verdict"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if AuthorizeRoute(route, []string{RoleBreedingDirector}) {
			t.Errorf("breeding_director must NOT reach %s %s -- the desk plans, it does not execute or judge", target.method, target.path)
		}
	}
}

// TestBreedingDirectorIsAGrantableJobRole pins the shape: a real job key (IsKnownRole, so
// the seeder and the Add Person form accept it) whose per-person access reproduces its
// permissions -- the whole-role parity test covers the diff; this one pins that the
// pc_trimming catalog row is the thing that carries the carve-out, so a human can tick it
// for one person without handing them pc_care at Configure.
func TestBreedingDirectorIsAGrantableJobRole(t *testing.T) {
	if !IsKnownRole(RoleBreedingDirector) {
		t.Fatal("breeding_director must be a known role")
	}
	assignments, ok := AssignmentsForRole(RoleBreedingDirector)
	if !ok {
		t.Fatal("breeding_director has no backfill mapping")
	}
	held := map[string]struct{}{}
	for _, p := range PermissionsForAssignmentsWithBaseline(assignments) {
		held[p] = struct{}{}
	}
	if _, ok := held[PCCarePlanTrimming]; !ok {
		t.Fatal("the breeding_director access rows must resolve pc_care.plan_trimming")
	}
	if _, ok := held[PCCarePlan]; ok {
		t.Fatal("the breeding_director access rows must NOT resolve pc_care.plan")
	}
	// The carve-out lives on pc_trimming, not on pc_care: ticking pc_care at Configure is the
	// whole module, and that is exactly what this row exists to avoid.
	onlyTrimming := PermissionsForAssignments([]ModuleAssignment{assign("pc_trimming", SurfaceMobile, LevelConfigure)})
	sawTrimming, sawPlan := false, false
	for _, p := range onlyTrimming {
		sawTrimming = sawTrimming || p == PCCarePlanTrimming
		sawPlan = sawPlan || p == PCCarePlan
	}
	if !sawTrimming || sawPlan {
		t.Fatalf("pc_trimming@configure resolved %v; want pc_care.plan_trimming and never pc_care.plan", onlyTrimming)
	}
}

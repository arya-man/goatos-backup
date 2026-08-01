package permissions

import "testing"

// projection-review: role-permission composition, not an aggregate query.
//   producer unique key: rolePermissions[<role key>] -- one map entry per role string.
//   consumer match key:  the same <role key>, read by RoleHasPermission.
//   multiplicity:        1:1. Exactly one effective permission set per role; a second
//                        assignment to the same key is an OVERWRITE, which is the
//                        defect class this file pins (see TestInitMustNotSilently...).

// plannerRoutes are the weighing planner surfaces the Growth Director owns. These are
// looked up through permissions.Match, the same lookup the HTTP auth middleware performs,
// so a route rename or pattern change fails here instead of silently skipping the test.
var plannerRoutes = []struct {
	method string
	path   string
}{
	{"POST", "/weighing/campaigns"},
	{"PUT", "/weighing/campaigns/80000000-0000-4000-8000-000000000001"},
	{"POST", "/weighing/campaigns/80000000-0000-4000-8000-000000000001/publish"},
	{"GET", "/app/weighing/planner/catalog"},
	{"GET", "/app/weighing/planner/parks/86000000-0000-4000-8000-000000000701/buckets"},
}

// Planning a weighing task is CEO-only (maintainer decision 2026-08-01). Every planner
// route -- the three writes and the two reads that feed the create wizard -- belongs to
// ceo_internal alone. The Growth Director monitors weighing across both parks and oversees
// the operators through his own surfaces; he does not raise the task.
//
// It calls AuthorizeRoute, which is the exact predicate
// httpmiddleware.AuthMiddleware.Wrap evaluates -- not a re-spelled copy of it.
func TestWeighingPlannerRoutesAreCEOOnly(t *testing.T) {
	for _, rt := range plannerRoutes {
		route, ok := Match(rt.method, rt.path)
		if !ok {
			t.Fatalf("%s %s is not a registered protected route", rt.method, rt.path)
		}
		if AuthorizeRoute(route, []string{RoleGrowthDirector}) {
			t.Errorf("growth_director must NOT be authorized for %s (%s %s); planning is CEO-only", route.OperationID, rt.method, rt.path)
		}
		if !AuthorizeRoute(route, []string{RoleCEOInternal}) {
			t.Errorf("ceo_internal must stay authorized for %s (%s %s)", route.OperationID, rt.method, rt.path)
		}
	}
}

// TestWeighingPlannerReadsBelongToThePlanner pins who may reach the create wizard's park
// picker and per-park shed page. Planning is CEO-only (maintainer decision 2026-08-01), so
// these reads are WeighingPlan and nothing else -- a monitor reaches the same parks and
// sheds through the task list and the Operators surface, which carry their own gates.
func TestWeighingPlannerReadsBelongToThePlanner(t *testing.T) {
	monitorOnly := "test_weighing_monitor_only"
	rolePermissions[monitorOnly] = map[string]struct{}{WeighingMonitor: {}}
	planOnly := "test_weighing_plan_only"
	rolePermissions[planOnly] = map[string]struct{}{WeighingPlan: {}}
	t.Cleanup(func() {
		delete(rolePermissions, monitorOnly)
		delete(rolePermissions, planOnly)
	})

	for _, rt := range []struct{ method, path string }{
		{"GET", "/app/weighing/planner/catalog"},
		{"GET", "/app/weighing/planner/parks/86000000-0000-4000-8000-000000000701/buckets"},
	} {
		route, ok := Match(rt.method, rt.path)
		if !ok {
			t.Fatalf("%s %s is not a registered protected route", rt.method, rt.path)
		}
		if AuthorizeRoute(route, []string{monitorOnly}) {
			t.Errorf("monitor-only actor must NOT reach %s; it is a planning surface", route.OperationID)
		}
		if !AuthorizeRoute(route, []string{planOnly}) {
			t.Errorf("plan-only actor must read %s", route.OperationID)
		}
		if AuthorizeRoute(route, []string{RoleVerifier}) {
			t.Errorf("verifier must not read %s", route.OperationID)
		}
	}
}

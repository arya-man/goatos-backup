package permissions

import "testing"

// Route coherence for the two reads the phone was missing: the single-task read behind a
// notification deep link, and the park vocabulary an oversight actor may call.
//
// Both go through Match + AuthorizeRoute, which is exactly the predicate
// httpmiddleware.AuthMiddleware.Wrap evaluates -- not a re-spelled copy of it -- so a pattern
// rename or a permission edit fails here instead of silently skipping.

// TestAppWeighingSingleTaskReadIsRegisteredForEveryWeighingSurface pins the single-task read to
// AppBootstrap, the same gate the bucket page it drills into carries.
//
// The gate is deliberately NOT one of the weighing permissions: the SERVICE applies the
// assignee/planner split and resolves the task's own park before answering, so naming (say)
// WeighingMonitor on the route would 403 the assignee the push was sent to.
func TestAppWeighingSingleTaskReadIsRegisteredForEveryWeighingSurface(t *testing.T) {
	route, ok := Match("GET", "/app/weighing/campaigns/80000000-0000-4000-8000-000000000001")
	if !ok {
		t.Fatalf("the single-task read is not a registered protected route")
	}
	if route.OperationID != "appGetWeighingCampaign" {
		t.Fatalf("single-task read resolved to %q; the campaign list or the bucket page is shadowing it", route.OperationID)
	}
	// The three weighing surfaces all reach a task detail, and an assignee reaching their own
	// work is the case a tighter route permission would break.
	for _, role := range []string{RoleOperator, RoleGrowthDirector, RoleCEOInternal} {
		if !AuthorizeRoute(route, []string{role}) {
			t.Errorf("%s must reach the single-task read; a push naming their task would 404 at the middleware", role)
		}
	}
}

// TestAppWeighingParksIsReadableWithoutThePlannerGrant is the whole reason the park vocabulary is
// a separate endpoint. The planner catalog is WeighingPlan, which is CEO-only, so a Growth
// Director (monitor + oversee_operators, never plan) had no park list they could read.
//
// It also pins the OR: naming all three permissions in the ANDed Permissions field would 403
// every actor, because no role holds the whole set.
func TestAppWeighingParksIsReadableWithoutThePlannerGrant(t *testing.T) {
	route, ok := Match("GET", "/app/weighing/parks")
	if !ok {
		t.Fatalf("the weighing park vocabulary is not a registered protected route")
	}
	if len(route.Permissions) != 0 {
		t.Fatalf("park vocabulary uses ANDed Permissions %v; no role holds monitor+plan+oversee, so every caller would 403", route.Permissions)
	}
	if !AuthorizeRoute(route, []string{RoleGrowthDirector}) {
		t.Fatalf("growth_director must read the weighing park vocabulary; that is the actor whose park chips 403'd on the planner catalog")
	}
	if !AuthorizeRoute(route, []string{RoleCEOInternal}) {
		t.Fatalf("ceo_internal must read the weighing park vocabulary")
	}

	// A monitor-only actor -- no plan, no oversight -- still filters the leadership surfaces by
	// park, so the vocabulary must not be tied to the two richer roles above.
	monitorOnly := "test_weighing_park_monitor_only"
	rolePermissions[monitorOnly] = map[string]struct{}{WeighingMonitor: {}}
	overseeOnly := "test_weighing_park_oversee_only"
	rolePermissions[overseeOnly] = map[string]struct{}{WeighingOverseeOperators: {}}
	t.Cleanup(func() {
		delete(rolePermissions, monitorOnly)
		delete(rolePermissions, overseeOnly)
	})
	if !AuthorizeRoute(route, []string{monitorOnly}) {
		t.Errorf("a monitor-only actor must read the weighing park vocabulary")
	}
	if !AuthorizeRoute(route, []string{overseeOnly}) {
		t.Errorf("an oversight-only actor must read the weighing park vocabulary")
	}

	// Not a public list. Someone with no weighing authority at all learns no park names here.
	if AuthorizeRoute(route, []string{RoleVerifier}) {
		t.Errorf("verifier must not read the weighing park vocabulary; they filter no weighing surface")
	}
}

// TestAppWeighingParksIsNotShadowedByTheCampaignIdPattern guards the one routing hazard these two
// additions create together: "/app/weighing/parks" and "/app/weighing/campaigns/{campaign_id}" are
// siblings, and a pattern that let the literal segment be read as a campaign id would send the
// park read through the task read's gate.
func TestAppWeighingParksIsNotShadowedByTheCampaignIdPattern(t *testing.T) {
	route, ok := Match("GET", "/app/weighing/parks")
	if !ok {
		t.Fatalf("the weighing park vocabulary is not a registered protected route")
	}
	if route.OperationID != "appListWeighingParks" {
		t.Fatalf("GET /app/weighing/parks resolved to %q, want appListWeighingParks", route.OperationID)
	}
}

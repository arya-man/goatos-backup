package permissions

import "testing"

// Route coherence for the Growth Director widgets read. Match + AuthorizeRoute
// is exactly the predicate httpmiddleware.AuthMiddleware.Wrap evaluates — not a
// re-spelled copy of it — so a pattern rename or a permission edit fails here
// instead of silently skipping.
func TestAdminGrowthDirectorWeightsRouteIsMonitorGated(t *testing.T) {
	route, ok := Match("GET", "/growth-director/weights")
	if !ok {
		t.Fatalf("the growth director weights read is not a registered protected route")
	}
	if route.OperationID != "adminGetGrowthDirectorWeights" {
		t.Fatalf("growth director weights resolved to %q, want adminGetGrowthDirectorWeights", route.OperationID)
	}

	// The section renders on the Weights screen, so it must be reachable by
	// exactly the actors who can read that screen: the Growth Director and the
	// CEO, both of whom hold WeighingMonitor.
	for _, role := range []string{RoleGrowthDirector, RoleCEOInternal} {
		if !AuthorizeRoute(route, []string{role}) {
			t.Errorf("%s must reach the growth director weights read; the Weights screen renders this section for them", role)
		}
	}

	// Not a public read: an operator executes weighing but monitors nothing,
	// and the verifier reviews evidence, not herd growth.
	for _, role := range []string{RoleOperator, RoleVerifier} {
		if AuthorizeRoute(route, []string{role}) {
			t.Errorf("%s must not reach the growth director weights read", role)
		}
	}
}

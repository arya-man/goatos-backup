package permissions

import "testing"

// The field operator who SPOTS a sick animal must be able to raise the case from the phone
// (maintainer decision 2026-07-30). This is the regression for the shipped defect: the phone
// showed an operator the ＋ Add-case button while POST /app/health/cases required
// health.diagnose — a permission no operator holds — so every sick-goat report came back 403
// and the outbox retried it forever under a "Retrying sync" row.
func TestOperatorMayRaiseHealthCase(t *testing.T) {
	route, ok := Match("POST", "/app/health/cases")
	if !ok {
		t.Fatal("POST /app/health/cases must stay a protected route")
	}
	if len(route.Permissions) != 1 || route.Permissions[0] != HealthReport {
		t.Fatalf("open-case route permissions = %v, want [%s]", route.Permissions, HealthReport)
	}

	for _, role := range []string{RoleOperator, RolePCDirector, RoleCEOInternal} {
		if !RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
			t.Fatalf("%s must be able to raise a sick-goat report", role)
		}
	}

	// Raising is not diagnosing: the clinical course authority stays with the PC Director tier.
	if RolesAuthorize([]string{RoleOperator}, []string{HealthDiagnose}, false) {
		t.Fatal("operator must not hold health.diagnose")
	}
	// A verifier reviews evidence and never captures it (separation of duty).
	if RolesAuthorize([]string{RoleVerifier}, []string{HealthReport}, false) {
		t.Fatal("verifier must not raise health cases")
	}
}

package permissions

import "testing"

// The health SOP engine is ADVISORY, and this file is where that stops being a
// design intention and becomes something the code enforces.
//
// The whole guarantee is that the person who FILLS THE FORM is not the person
// who AUTHORISES THE TREATMENT. That holds only because submitting and
// confirming are separate routes carrying separate permissions, and because no
// field role holds both. A test that checked only the route table would miss
// half of it; a test that checked only the role matrix would miss the other.

func routeFor(t *testing.T, operationID string) Route {
	t.Helper()
	for _, r := range protectedRoutes {
		if r.OperationID == operationID {
			return r
		}
	}
	t.Fatalf("route %q is not registered; an unregistered route answers 403 at runtime", operationID)
	return Route{}
}

func hasPermission(t *testing.T, role, permission string) bool {
	t.Helper()
	perms, ok := rolePermissions[role]
	if !ok {
		t.Fatalf("role %q is not in the permission matrix", role)
	}
	_, held := perms[permission]
	return held
}

// The three routes carry the three different permissions. Collapsing any two
// would let one actor both observe and authorise.
func TestDiagnosisRoutesCarryTheSeparatedPermissions(t *testing.T) {
	cases := []struct {
		operationID string
		method      string
		pattern     string
		permission  string
	}{
		{"submitAppHealthObservation", "POST", "/app/health/observations", HealthReport},
		{"getAppHealthObservation", "GET", "/app/health/observations/{health_diagnosis_run_id}", HealthRead},
		{"confirmAppHealthDiagnosis", "POST", "/app/health/observations/{health_diagnosis_run_id}/confirm", HealthDiagnose},
	}
	for _, tc := range cases {
		t.Run(tc.operationID, func(t *testing.T) {
			route := routeFor(t, tc.operationID)
			if route.Method != tc.method || route.Pattern != tc.pattern {
				t.Errorf("route = %s %s, want %s %s", route.Method, route.Pattern, tc.method, tc.pattern)
			}
			if len(route.Permissions) != 1 || route.Permissions[0] != tc.permission {
				t.Errorf("permissions = %v, want exactly [%s]", route.Permissions, tc.permission)
			}
		})
	}
}

// THE separation of duty. An operator observes and executes; they must never be
// able to authorise the treatment their own form proposed.
func TestOperatorCanObserveButNeverConfirm(t *testing.T) {
	if !hasPermission(t, RoleOperator, HealthReport) {
		t.Error("an operator must be able to raise a sick-goat report")
	}
	if !hasPermission(t, RoleOperator, HealthExecute) {
		t.Error("an operator must be able to record a treatment session")
	}
	if hasPermission(t, RoleOperator, HealthDiagnose) {
		t.Fatal("an operator must NEVER confirm a diagnosis: the form-filler would be authorising " +
			"their own proposal, which removes the second check the engine exists to provide")
	}
}

// The Health Director is the confirmer (maintainer decision 2026-08-14), and
// needs the read to see what they are confirming.
func TestHealthDirectorConfirms(t *testing.T) {
	if !hasPermission(t, RoleHealthDirector, HealthDiagnose) {
		t.Fatal("health_director must hold health.diagnose -- confirming a diagnosis is the role's defining job")
	}
	if !hasPermission(t, RoleHealthDirector, HealthRead) {
		t.Fatal("health.diagnose is unusable without health.read: the confirmer cannot see the queue")
	}
	// Deliberately withheld: the desk that judges the work must not also record
	// having performed it.
	if hasPermission(t, RoleHealthDirector, HealthExecute) {
		t.Error("health_director must not hold health.execute -- the manager treats, the Director judges")
	}
	// The role authors the rulebook the diagnosis is made against.
	if !hasPermission(t, RoleHealthDirector, HealthConfigWrite) {
		t.Error("health_director authors the treatment rulebook")
	}
}

// A verifier checks captured work and must not be able to rewrite or authorise
// the standard that work is judged against.
func TestVerifierHoldsNoDiagnosisAuthority(t *testing.T) {
	for _, permission := range []string{HealthDiagnose, HealthConfigWrite} {
		if hasPermission(t, RoleVerifier, permission) {
			t.Errorf("a verifier must not hold %s -- separation of duty", permission)
		}
	}
}

// A park head runs a park's execution; they read health work but do not diagnose.
func TestParkHeadDoesNotDiagnose(t *testing.T) {
	if hasPermission(t, RoleParkHead, HealthDiagnose) {
		t.Error("park_head runs execution and must not confirm diagnoses")
	}
}

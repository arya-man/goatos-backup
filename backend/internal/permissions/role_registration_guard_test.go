package permissions

import "testing"

// TestInitMustNotSilentlyDropADeclaredPermission is the guard against the RECURRENCE, not
// just against today's symptom. registerRole is the only way a role permission set may be
// installed, and it panics on a duplicate key -- so a future init() that re-declares a
// flat role (which is how WeighingOverseeOperators and then WeighingPlan were both
// silently voided) fails loudly at process start instead of shipping an inert permission.
func TestInitMustNotSilentlyDropADeclaredPermission(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("re-registering an existing role must panic, not silently overwrite it")
		}
	}()
	registerRole(RoleGrowthDirector, map[string]struct{}{WeighingMonitor: {}})
}

// TestEveryFlatRoleIsDeclaredExactlyOnce is the static half of the same guard: it proves
// no source file assigns rolePermissions[<role>] outside registerRole, which is what made
// the override invisible to every existing test.
func TestEveryFlatRoleIsDeclaredExactlyOnce(t *testing.T) {
	if len(registeredRoleOrigins) == 0 {
		t.Fatal("registeredRoleOrigins is empty; registerRole is not being used")
	}
	for role := range rolePermissions {
		if _, ok := registeredRoleOrigins[role]; !ok {
			t.Errorf("role %q was installed into rolePermissions without registerRole", role)
		}
	}
}

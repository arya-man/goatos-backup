package permissions

import "testing"

func TestWeighingRolePermissions(t *testing.T) {
	if !RolesAuthorize([]string{RoleCEOInternal}, []string{WeighingPlan, WeighingMonitor}, false) {
		t.Fatal("CEO/CXO should plan and monitor weighing")
	}
	if RolesAuthorize([]string{RolePCDirector}, []string{WeighingPlan}, false) {
		t.Fatal("pc director must not plan weighing")
	}
	if RolesAuthorize([]string{RolePCDirector}, []string{WeighingMonitor}, false) {
		t.Fatal("pc director must not monitor weighing")
	}
	if RolesAuthorize([]string{RolePCDirector}, []string{WeighingExecute}, false) {
		t.Fatal("pc director must not execute weighing")
	}
	if RolesAuthorize([]string{RoleGrowthDirector}, []string{WeighingPlan}, false) {
		t.Fatal("growth director must not plan weighing")
	}
	if !RolesAuthorize([]string{RoleGrowthDirector}, []string{WeighingMonitor}, false) {
		t.Fatal("growth director should monitor weighing")
	}
	if !RolesAuthorize([]string{RoleOperator}, []string{WeighingExecute}, false) {
		t.Fatal("operator should execute weighing")
	}
	if !RolesAuthorize([]string{RoleGrowthDirector}, []string{WeighingExecute}, false) {
		t.Fatal("growth director should execute weighing")
	}
}

// TestWeighingOverseeOperatorsIsGrowthDirectorOnly pins the read-only Operators surface to the
// one role that owns it. It also guards the composition trap that made the grant inert: the
// RoleGrowthDirector entry in permissions.go is REPLACED by the override in
// permissions_orgrole.go, so a permission added only to the literal never reaches the effective
// role set and no other test noticed.
func TestWeighingOverseeOperatorsIsGrowthDirectorOnly(t *testing.T) {
	if !RolesAuthorize([]string{RoleGrowthDirector}, []string{WeighingOverseeOperators}, false) {
		t.Fatal("growth director should oversee weighing operators")
	}
	for _, role := range []string{RoleCEOInternal, RolePCDirector, RoleOperator, RoleParkHead, RoleVerifier} {
		if RolesAuthorize([]string{role}, []string{WeighingOverseeOperators}, false) {
			t.Fatalf("%s must not oversee weighing operators", role)
		}
	}
}

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

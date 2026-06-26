package permissions

import "testing"

func TestRolePermissionMatrix(t *testing.T) {
	tests := []struct {
		role       string
		permission string
		want       bool
	}{
		{RoleOperator, GoatRead, true},
		{RoleCEOInternal, GoatWriteIdentity, true},
		{RoleAdmin, OperatorsManageCapability, true},
		{RoleParkHead, OperatorsManageRoster, true},
		{RoleOperator, AppBootstrap, true},
		{RoleOperator, OperatorsRead, false},
		{RoleVerifier, OperatorsManageCapability, false},
		{RoleAdmin, SOPPublish, true},
		{RoleParkHead, TaskAssign, true},
		{RoleParkHead, VaccinationRead, true},
		{RoleParkHead, ProcurementReview, true},
		{RoleOperator, ProcurementWrite, true},
		{RoleVerifier, ProcurementWrite, false},
		{RoleOperator, TaskExecute, true},
		{RoleOperator, SOPWrite, false},
		{RoleVerifier, TaskVerify, true},
	}
	for _, tt := range tests {
		t.Run(tt.role+"/"+tt.permission, func(t *testing.T) {
			if got := RoleHasPermission(tt.role, tt.permission); got != tt.want {
				t.Fatalf("RoleHasPermission()=%v want %v", got, tt.want)
			}
		})
	}
}

func TestRouteRegistryCoversImplementedProtectedRoutes(t *testing.T) {
	implemented := []struct {
		method string
		path   string
	}{
		{"GET", "/goats/search"},
		{"GET", "/goats/10000000-0000-4000-8000-000000000001"},
		{"GET", "/goats/10000000-0000-4000-8000-000000000001/timeline"},
		{"GET", "/identifiers/rfid/RFID-SYNTHETIC-001/resolve"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers/30000000-0000-4000-8000-000000000001/retire"},
		{"GET", "/admin/locations"},
		{"POST", "/admin/locations"},
		{"GET", "/admin/locations/54000000-0000-4000-8000-000000000001"},
		{"PATCH", "/admin/locations/54000000-0000-4000-8000-000000000001"},
		{"DELETE", "/admin/locations/54000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/locations/54000000-0000-4000-8000-000000000001/retire"},
		{"GET", "/admin/locations/54000000-0000-4000-8000-000000000001/children"},
		{"GET", "/admin/locations/54000000-0000-4000-8000-000000000001/usage"},
		{"GET", "/admin/locations/54000000-0000-4000-8000-000000000001/aliases"},
		{"POST", "/admin/locations/54000000-0000-4000-8000-000000000001/aliases"},
		{"PATCH", "/admin/locations/54000000-0000-4000-8000-000000000001/aliases/55000000-0000-4000-8000-000000000001"},
		{"DELETE", "/admin/locations/54000000-0000-4000-8000-000000000001/aliases/55000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/locations/54000000-0000-4000-8000-000000000001/aliases/55000000-0000-4000-8000-000000000001/retire"},
		{"GET", "/admin/locations/54000000-0000-4000-8000-000000000001/capacity"},
		{"POST", "/admin/locations/54000000-0000-4000-8000-000000000001/capacity"},
		{"PATCH", "/admin/locations/54000000-0000-4000-8000-000000000001/capacity/56000000-0000-4000-8000-000000000001"},
		{"DELETE", "/admin/locations/54000000-0000-4000-8000-000000000001/capacity/56000000-0000-4000-8000-000000000001"},
		{"GET", "/admin/location-review-items"},
		{"POST", "/admin/location-review-items"},
		{"POST", "/admin/location-review-items/57000000-0000-4000-8000-000000000001/resolve"},
		{"GET", "/admin/operators"},
		{"POST", "/admin/operators"},
		{"GET", "/admin/operators/90000000-0000-4000-8000-000000000001"},
		{"PATCH", "/admin/operators/90000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/operators/90000000-0000-4000-8000-000000000001/activate"},
		{"POST", "/admin/operators/90000000-0000-4000-8000-000000000001/deactivate"},
		{"GET", "/admin/operators/90000000-0000-4000-8000-000000000001/grants"},
		{"POST", "/admin/operators/90000000-0000-4000-8000-000000000001/grants"},
		{"POST", "/admin/operators/90000000-0000-4000-8000-000000000001/capabilities"},
		{"DELETE", "/admin/operators/90000000-0000-4000-8000-000000000001/capabilities/91000000-0000-4000-8000-000000000001"},
		{"GET", "/admin/operators/90000000-0000-4000-8000-000000000001/devices"},
		{"POST", "/admin/operators/90000000-0000-4000-8000-000000000001/devices/92000000-0000-4000-8000-000000000001/revoke"},
		{"GET", "/app/me"},
		{"GET", "/app/bootstrap"},
		{"POST", "/app/devices/register"},
		{"POST", "/app/devices/92000000-0000-4000-8000-000000000001/heartbeat"},
		{"GET", "/admin/sops"},
		{"POST", "/admin/sops"},
		{"GET", "/admin/sops/61000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/sops/61000000-0000-4000-8000-000000000001/versions"},
		{"GET", "/admin/sops/61000000-0000-4000-8000-000000000001/versions/62000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/sops/61000000-0000-4000-8000-000000000001/versions/62000000-0000-4000-8000-000000000001/dry-run"},
		{"POST", "/admin/sops/61000000-0000-4000-8000-000000000001/versions/62000000-0000-4000-8000-000000000001/publish"},
		{"POST", "/admin/sops/61000000-0000-4000-8000-000000000001/versions/62000000-0000-4000-8000-000000000001/retire"},
		{"GET", "/admin/tasks"},
		{"POST", "/admin/tasks"},
		{"GET", "/admin/tasks/63000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/tasks/63000000-0000-4000-8000-000000000001/assign"},
		{"POST", "/admin/tasks/63000000-0000-4000-8000-000000000001/verify"},
		{"POST", "/admin/tasks/63000000-0000-4000-8000-000000000001/rework"},
		{"GET", "/admin/tasks/submission-fanouts/failed"},
		{"GET", "/app/tasks"},
		{"GET", "/app/tasks/63000000-0000-4000-8000-000000000001"},
		{"GET", "/app/sop-versions/62000000-0000-4000-8000-000000000001"},
		{"POST", "/app/tasks/63000000-0000-4000-8000-000000000001/submissions"},
		{"GET", "/procurement/source-entry/loads"},
		{"POST", "/procurement/source-entry/loads"},
		{"GET", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001"},
		{"POST", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001/goats"},
		{"POST", "/procurement/source-entry/goats/10000000-0000-4000-8000-000000000001/hf-vaccination-evidence"},
		{"POST", "/procurement/source-entry/hf-vaccination-evidence/12000000-0000-4000-8000-000000000001/review"},
		{"POST", "/procurement/source-entry/goats/10000000-0000-4000-8000-000000000001/source-health"},
		{"POST", "/procurement/source-entry/goats/10000000-0000-4000-8000-000000000001/pre-dispatch-decision"},
		{"POST", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001/dispatch"},
		{"POST", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001/arrival-review"},
		{"POST", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001/accept-intake"},
		{"GET", "/protocols"},
		{"GET", "/protocols/animal-stages"},
		{"POST", "/protocols"},
		{"POST", "/protocols/64000000-0000-4000-8000-000000000001/versions"},
		{"POST", "/protocols/versions/65000000-0000-4000-8000-000000000001/rules"},
		{"GET", "/protocols/versions/65000000-0000-4000-8000-000000000001"},
		{"POST", "/protocols/versions/65000000-0000-4000-8000-000000000001/publish"},
		{"POST", "/protocols/vaccination/impact-preview"},
		{"GET", "/action-center/obligations"},
		{"GET", "/vaccination/action-center"},
		{"GET", "/vaccination/adherence"},
		{"GET", "/control-tower/vaccination"},
		{"GET", "/vaccination/workflows/batch:66000000-0000-4000-8000-000000000001:rule:65000000-0000-4000-8000-000000000001:shed:55000000-0000-4000-8000-000000000001"},
		{"GET", "/vaccination/operations"},
		{"GET", "/vaccination/execution"},
		{"GET", "/vaccination/execution/sheds/55000000-0000-4000-8000-000000000001"},
		{"GET", "/vaccination/verification-queue"},
		{"POST", "/vaccination/completions/aa000000-0000-4000-8000-000000000001/accept"},
		{"POST", "/vaccination/completions/aa000000-0000-4000-8000-000000000001/reject"},
		{"GET", "/goats/10000000-0000-4000-8000-000000000001/passport"},
	}
	for _, route := range implemented {
		if _, ok := Match(route.method, route.path); !ok {
			t.Fatalf("implemented route not registered: %s %s", route.method, route.path)
		}
	}
}

func TestVaccinationBackendRouteSmokeAvoidsRouteNotRegistered(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/control-tower/vaccination"},
		{"GET", "/vaccination/action-center"},
		{"GET", "/vaccination/adherence"},
		{"GET", "/vaccination/workflows/batch:66000000-0000-4000-8000-000000000001:rule:65000000-0000-4000-8000-000000000001:shed:55000000-0000-4000-8000-000000000001"},
		{"GET", "/vaccination/operations"},
		{"GET", "/vaccination/execution"},
	}
	for _, route := range routes {
		if _, ok := Match(route.method, route.path); !ok {
			t.Fatalf("vaccination backend route smoke failed: %s %s is not registered", route.method, route.path)
		}
	}
}

func TestProcurementBackendRouteSmokeAvoidsRouteNotRegistered(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/procurement/source-entry/loads"},
		{"POST", "/procurement/source-entry/loads"},
		{"GET", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001"},
		{"POST", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001/goats"},
		{"POST", "/procurement/source-entry/goats/10000000-0000-4000-8000-000000000001/hf-vaccination-evidence"},
		{"POST", "/procurement/source-entry/hf-vaccination-evidence/12000000-0000-4000-8000-000000000001/review"},
		{"POST", "/procurement/source-entry/goats/10000000-0000-4000-8000-000000000001/source-health"},
		{"POST", "/procurement/source-entry/goats/10000000-0000-4000-8000-000000000001/pre-dispatch-decision"},
		{"POST", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001/dispatch"},
		{"POST", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001/arrival-review"},
		{"POST", "/procurement/source-entry/loads/ac000000-0000-4000-8000-000000000001/accept-intake"},
	}
	for _, route := range routes {
		if _, ok := Match(route.method, route.path); !ok {
			t.Fatalf("procurement backend route smoke failed: %s %s is not registered", route.method, route.path)
		}
	}
}

func TestRouteRegistryFailsClosedForUnknownRoute(t *testing.T) {
	if _, ok := Match("GET", "/admin/not-registered"); ok {
		t.Fatal("unknown route matched")
	}
}

func TestMultipleActiveGrantRolesUnionPermissions(t *testing.T) {
	if RolesAuthorize([]string{RoleOperator}, []string{TaskVerify}, false) {
		t.Fatal("operator alone should not verify tasks")
	}
	if !RolesAuthorize([]string{RoleOperator, RoleVerifier}, []string{TaskVerify}, false) {
		t.Fatal("operator+verifier should authorize verifier-only task verification")
	}
}

package permissions

import "testing"

func TestRolePermissionMatrix(t *testing.T) {
	tests := []struct {
		role       string
		permission string
		want       bool
	}{
		{RoleAdmin, ImportRunManage, true},
		{RoleVerifier, ImportRunView, true},
		{RoleVerifier, ImportRunManage, false},
		{RoleParkHead, CorrectionCreate, true},
		{RoleParkHead, GoatReviewIdentity, false},
		{RoleOperator, GoatRead, true},
		{RoleOperator, AnalyticsIdentityRead, false},
		{RoleCEOInternal, GoatViewDirtyData, true},
		{RoleCEOInternal, GoatWriteIdentity, true},
		{RoleCEOInternal, ImportRunManage, true},
		{RoleCEOInternal, ImportRunView, true},
		{RoleAdmin, OperatorsManageCapability, true},
		{RoleParkHead, OperatorsManageRoster, true},
		{RoleOperator, AppBootstrap, true},
		{RoleOperator, OperatorsRead, false},
		{RoleVerifier, OperatorsManageCapability, false},
		{RoleAdmin, SOPPublish, true},
		{RoleParkHead, TaskAssign, true},
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
		{"GET", "/identity/correction-requests"},
		{"POST", "/identity/correction-requests"},
		{"GET", "/admin/identity/conflicts"},
		{"GET", "/admin/identity/conflicts/20000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/identity/conflicts/20000000-0000-4000-8000-000000000001/resolve"},
		{"GET", "/admin/import-runs"},
		{"GET", "/admin/import-runs/70000000-0000-4000-8000-000000000001"},
		{"GET", "/admin/import-runs/70000000-0000-4000-8000-000000000001/rows"},
		{"POST", "/admin/import-runs"},
		{"GET", "/admin/legacy-sync/sources"},
		{"GET", "/admin/legacy-sync/status"},
		{"GET", "/admin/legacy-sync/runs"},
		{"POST", "/admin/legacy-sync/runs"},
		{"GET", "/admin/legacy-sync/runs/70000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/legacy-sync/runs/70000000-0000-4000-8000-000000000001/cancel"},
		{"GET", "/admin/identity/candidates"},
		{"POST", "/admin/identity/candidates/80000000-0000-4000-8000-000000000001/approve"},
		{"POST", "/admin/identity/candidates/80000000-0000-4000-8000-000000000001/reject"},
		{"POST", "/admin/goats"},
		{"PATCH", "/admin/goats/10000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers/30000000-0000-4000-8000-000000000001/retire"},
		{"GET", "/admin/identity/correction-requests"},
		{"POST", "/admin/identity/correction-requests/40000000-0000-4000-8000-000000000001/resolve"},
		{"GET", "/analytics/identity/counts"},
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
		{"GET", "/admin/operator-source-candidates"},
		{"POST", "/admin/operator-source-candidates/93000000-0000-4000-8000-000000000001/map"},
		{"POST", "/admin/operator-source-candidates/93000000-0000-4000-8000-000000000001/reject"},
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
		{"GET", "/app/tasks"},
		{"GET", "/app/tasks/63000000-0000-4000-8000-000000000001"},
		{"GET", "/app/sop-versions/62000000-0000-4000-8000-000000000001"},
		{"POST", "/app/tasks/63000000-0000-4000-8000-000000000001/submissions"},
	}
	for _, route := range implemented {
		if _, ok := Match(route.method, route.path); !ok {
			t.Fatalf("implemented route not registered: %s %s", route.method, route.path)
		}
	}
}

func TestRouteRegistryFailsClosedForUnknownRoute(t *testing.T) {
	if _, ok := Match("GET", "/admin/not-registered"); ok {
		t.Fatal("unknown route matched")
	}
}

func TestCreateImportRunIsProductAdminOnly(t *testing.T) {
	route, ok := Match("POST", "/admin/import-runs")
	if !ok {
		t.Fatal("create import run route missing")
	}
	if !route.AdminOnly || !RolesAuthorize([]string{RoleAdmin}, route.Permissions, route.AdminOnly) {
		t.Fatalf("admin route not admin-authorized: %#v", route)
	}
	if !RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, route.AdminOnly) {
		t.Fatal("ceo_internal should be authorized as Goat OS product admin")
	}
	if RolesAuthorize([]string{RoleVerifier}, route.Permissions, route.AdminOnly) {
		t.Fatal("verifier authorized for product-admin-only import management")
	}
}

func TestCreateLegacySyncRunIsProductAdminOnly(t *testing.T) {
	route, ok := Match("POST", "/admin/legacy-sync/runs")
	if !ok {
		t.Fatal("create legacy sync run route missing")
	}
	if !route.AdminOnly || !RolesAuthorize([]string{RoleAdmin}, route.Permissions, route.AdminOnly) {
		t.Fatalf("admin route not admin-authorized: %#v", route)
	}
	if !RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, route.AdminOnly) {
		t.Fatal("ceo_internal should be authorized as Goat OS product admin")
	}
	if RolesAuthorize([]string{RoleVerifier}, route.Permissions, route.AdminOnly) {
		t.Fatal("verifier authorized for product-admin-only legacy sync management")
	}
}

func TestMultipleActiveGrantRolesUnionPermissions(t *testing.T) {
	if RolesAuthorize([]string{RoleOperator}, []string{GoatReviewIdentity}, false) {
		t.Fatal("operator alone should not review identity")
	}
	if !RolesAuthorize([]string{RoleOperator, RoleVerifier}, []string{GoatReviewIdentity}, false) {
		t.Fatal("operator+verifier should authorize verifier-only identity review")
	}
}

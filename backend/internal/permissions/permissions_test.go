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
		{RoleCEOInternal, GoatWriteIdentity, false},
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
		{"GET", "/admin/import-runs/70000000-0000-4000-8000-000000000001"},
		{"GET", "/admin/import-runs/70000000-0000-4000-8000-000000000001/rows"},
		{"POST", "/admin/import-runs"},
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

func TestCreateImportRunIsAdminOnly(t *testing.T) {
	route, ok := Match("POST", "/admin/import-runs")
	if !ok {
		t.Fatal("create import run route missing")
	}
	if !route.AdminOnly || !RolesAuthorize([]string{RoleAdmin}, route.Permissions, route.AdminOnly) {
		t.Fatalf("admin route not admin-authorized: %#v", route)
	}
	if RolesAuthorize([]string{RoleVerifier}, route.Permissions, route.AdminOnly) {
		t.Fatal("verifier authorized for admin-only import management")
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

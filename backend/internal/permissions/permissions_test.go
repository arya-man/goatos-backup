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
		{RoleCEOInternal, GoatWriteHealth, true},
		{RoleAdmin, GoatWriteHealth, true},
		{RolePCDirector, GoatWriteHealth, true},
		{RoleVerifier, GoatWriteHealth, false},
		{RoleVerifier, GoatWriteIdentity, true},
		{RoleParkHead, GoatWriteHealth, false},
		{RoleAdmin, OperatorsManageCapability, true},
		{RoleParkHead, OperatorsManageRoster, true},
		{RoleOperator, AppBootstrap, true},
		{RoleOperator, OperatorsRead, false},
		{RoleVerifier, OperatorsManageCapability, false},
		{RoleAdmin, SOPPublish, true},
		{RoleParkHead, TaskAssign, true},
		{RoleParkHead, VaccinationRead, true},
		{RolePCDirector, VaccinationVerify, true},
		{RolePCDirector, CalendarAction, true},
		{RolePCDirector, ProcurementWrite, false},
		{RolePCDirector, ProcurementReview, false},
		{RoleParkHead, ProcurementReview, true},
		{RoleOperator, ProcurementWrite, true},
		{RoleVerifier, ProcurementWrite, false},
		{RoleOperator, TaskExecute, true},
		{RoleOperator, SOPWrite, false},
		{RoleVerifier, TaskVerify, true},
		{RoleAdmin, CalendarAction, true},
		{RoleCEOInternal, CalendarAction, true},
		{RoleParkHead, CalendarRead, true},
		{RoleParkHead, CalendarAction, true},
		{RoleVerifier, CalendarRead, true},
		{RoleVerifier, CalendarAction, false},
		{RoleOperator, CalendarRead, false},
		{RoleAdmin, AdminWebBootstrap, true},
		{RoleCEOInternal, AdminWebBootstrap, true},
		{RoleAdmin, OperationsRepair, true},
		{RoleCEOInternal, OperationsRepair, true},
		{RolePCDirector, OperationsRepair, false},
		{RoleParkHead, OperationsRepair, false},
		{RolePCDirector, AdminWebBootstrap, true},
		{RoleVerifier, AdminWebBootstrap, false},
		{RoleParkHead, AdminWebBootstrap, false},
		{RoleOperator, AdminWebBootstrap, false},
	}
	for _, tt := range tests {
		t.Run(tt.role+"/"+tt.permission, func(t *testing.T) {
			if got := RoleHasPermission(tt.role, tt.permission); got != tt.want {
				t.Fatalf("RoleHasPermission()=%v want %v", got, tt.want)
			}
		})
	}
}

func TestHealthGoatRouteUsesDedicatedHealthPermission(t *testing.T) {
	route, ok := Match("POST", "/admin/goats/10000000-0000-4000-8000-000000000001/health")
	if !ok {
		t.Fatal("healthGoat route is not registered")
	}
	if route.OperationID != "healthGoat" {
		t.Fatalf("operation_id=%q, want healthGoat", route.OperationID)
	}
	if len(route.Permissions) != 1 || route.Permissions[0] != GoatWriteHealth {
		t.Fatalf("healthGoat permissions=%v, want [%s]", route.Permissions, GoatWriteHealth)
	}
	if RolesAuthorize([]string{RoleVerifier}, route.Permissions, route.AdminOnly) {
		t.Fatal("verifier must not authorize direct health mutation")
	}
	if !RolesAuthorize([]string{RolePCDirector}, route.Permissions, route.AdminOnly) {
		t.Fatal("pc_director should authorize direct health mutation")
	}
}

func TestCriticalDeathExitRouteUsesDedicatedHealthPermission(t *testing.T) {
	route, ok := Match("POST", "/admin/goats/10000000-0000-4000-8000-000000000001/critical-death-exit")
	if !ok {
		t.Fatal("criticalDeathExitGoat route is not registered")
	}
	if route.OperationID != "criticalDeathExitGoat" {
		t.Fatalf("operation_id=%q, want criticalDeathExitGoat", route.OperationID)
	}
	if len(route.Permissions) != 1 || route.Permissions[0] != GoatWriteHealth {
		t.Fatalf("criticalDeathExitGoat permissions=%v, want [%s]", route.Permissions, GoatWriteHealth)
	}
	if RolesAuthorize([]string{RoleVerifier}, route.Permissions, route.AdminOnly) {
		t.Fatal("verifier must not authorize critical death exit")
	}
	if !RolesAuthorize([]string{RolePCDirector}, route.Permissions, route.AdminOnly) {
		t.Fatal("pc_director should authorize critical death exit")
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
		{"GET", "/identifiers/animal_identifier_1/AID-SYNTHETIC-001/resolve"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers/30000000-0000-4000-8000-000000000001/retire"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/move"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/exit"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/critical-death-exit"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/health"},
		{"POST", "/admin/goats/10000000-0000-4000-8000-000000000001/reproductive"},
		{"GET", "/operations/audit"},
		{"GET", "/operations/audit/summary"},
		{"GET", "/operations/kernel-health"},
		{"GET", "/operations/dlq"},
		{"POST", "/operations/dlq/replay"},
		{"POST", "/operations/dlq/discard"},
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
		{"GET", "/admin-web/bootstrap"},
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
		{"POST", "/vaccination/manual-campaigns"},
		{"GET", "/action-center/obligations"},
		{"GET", "/vaccination/action-center"},
		{"GET", "/vaccination/adherence"},
		{"GET", "/control-tower/vaccination"},
		{"GET", "/workflows/feed_projection_exception:77000000-0000-4000-8000-000000000001"},
		{"GET", "/vaccination/workflows/batch:66000000-0000-4000-8000-000000000001:rule:65000000-0000-4000-8000-000000000001:shed:55000000-0000-4000-8000-000000000001"},
		{"GET", "/vaccination/operations"},
		{"GET", "/vaccination/execution"},
		{"GET", "/vaccination/execution/sheds/55000000-0000-4000-8000-000000000001"},
		{"GET", "/feed-direction/readiness"},
		{"GET", "/feed-direction/generation-preview"},
		{"GET", "/calendar/vaccination/events"},
		{"GET", "/calendar/vaccination/events/obligation:86000000-0000-4000-8000-000000001001"},
		{"GET", "/calendar/vaccination/events/batch:86000000-0000-4000-8000-000000001002:rule:86000000-0000-4000-8000-000000000503:shed:86000000-0000-4000-8000-000000000101/targets"},
		{"GET", "/calendar/vaccination/events/obligation:86000000-0000-4000-8000-000000001001/history"},
		{"POST", "/calendar/vaccination/events/obligation:86000000-0000-4000-8000-000000001001/nudge"},
		{"POST", "/calendar/vaccination/events/obligation:86000000-0000-4000-8000-000000001001/snooze"},
		{"POST", "/calendar/vaccination/events/obligation:86000000-0000-4000-8000-000000001001/escalation/acknowledge"},
		{"POST", "/calendar/vaccination/events/obligation:86000000-0000-4000-8000-000000001001/escalation/resolve"},
		{"GET", "/vaccination/verification-queue"},
		{"GET", "/goats/10000000-0000-4000-8000-000000000001/passport"},
	}
	for _, route := range implemented {
		if _, ok := Match(route.method, route.path); !ok {
			t.Fatalf("implemented route not registered: %s %s", route.method, route.path)
		}
	}
}

func TestCalendarBackendRouteSmokeAvoidsRouteNotRegistered(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/calendar/vaccination/events"},
		{"GET", "/calendar/vaccination/events/batch:86000000-0000-4000-8000-000000001002:rule:86000000-0000-4000-8000-000000000503:shed:86000000-0000-4000-8000-000000000101"},
		{"GET", "/calendar/vaccination/events/batch:86000000-0000-4000-8000-000000001002:rule:86000000-0000-4000-8000-000000000503:shed:86000000-0000-4000-8000-000000000101/targets"},
		{"GET", "/calendar/vaccination/events/calendar:86000000-0000-4000-8000-000000001003/history"},
		{"POST", "/calendar/vaccination/events/calendar:86000000-0000-4000-8000-000000001003/nudge"},
		{"POST", "/calendar/vaccination/events/calendar:86000000-0000-4000-8000-000000001003/snooze"},
		{"POST", "/calendar/vaccination/events/calendar:86000000-0000-4000-8000-000000001003/escalation/acknowledge"},
		{"POST", "/calendar/vaccination/events/calendar:86000000-0000-4000-8000-000000001003/escalation/resolve"},
	}
	for _, route := range routes {
		if _, ok := Match(route.method, route.path); !ok {
			t.Fatalf("calendar backend route smoke failed: %s %s is not registered", route.method, route.path)
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

func TestFeedDirectionBackendRouteSmokeAvoidsRouteNotRegistered(t *testing.T) {
	for _, item := range []struct {
		path        string
		operationID string
	}{
		{"/feed-direction/readiness", "getFeedDirectionReadiness"},
		{"/feed-direction/generation-preview", "getFeedDirectionGenerationPreview"},
	} {
		route, ok := Match("GET", item.path)
		if !ok {
			t.Fatalf("feed direction route is not registered: %s", item.path)
		}
		if route.OperationID != item.operationID {
			t.Fatalf("operation_id=%q, want %s", route.OperationID, item.operationID)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != ProtocolRead {
			t.Fatalf("permissions=%v, want [%s]", route.Permissions, ProtocolRead)
		}
		if RolesAuthorize([]string{RoleOperator}, route.Permissions, route.AdminOnly) {
			t.Fatalf("operator must not authorize Feed Direction read route: %s", item.path)
		}
		if !RolesAuthorize([]string{RoleParkHead}, route.Permissions, route.AdminOnly) {
			t.Fatalf("park head should authorize Feed Direction read route via protocol.read: %s", item.path)
		}
	}

	workflowRoute, ok := Match("GET", "/workflows/feed_projection_exception:77000000-0000-4000-8000-000000000001")
	if !ok {
		t.Fatal("top-level Feed Direction workflow route is not registered")
	}
	if workflowRoute.OperationID != "getWorkflowDrilldown" {
		t.Fatalf("operation_id=%q, want getWorkflowDrilldown", workflowRoute.OperationID)
	}
	if len(workflowRoute.Permissions) != 1 || workflowRoute.Permissions[0] != ObligationRead {
		t.Fatalf("permissions=%v, want [%s]", workflowRoute.Permissions, ObligationRead)
	}
	if !RolesAuthorize([]string{RoleParkHead}, workflowRoute.Permissions, workflowRoute.AdminOnly) {
		t.Fatal("park head should authorize top-level workflow route via obligation.read")
	}

	for _, path := range []string{
		"/feed-direction/counts-projection/exceptions/77000000-0000-4000-8000-000000000001/resolve",
		"/feed-direction/counts-projection/exceptions/77000000-0000-4000-8000-000000000001/dismiss",
	} {
		route, ok := Match("POST", path)
		if !ok {
			t.Fatalf("feed direction counts exception action route is not registered: %s", path)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != ProtocolWrite {
			t.Fatalf("permissions=%v, want [%s]", route.Permissions, ProtocolWrite)
		}
		if RolesAuthorize([]string{RoleOperator}, route.Permissions, route.AdminOnly) {
			t.Fatalf("operator must not authorize Feed Direction counts exception action: %s", path)
		}
		if !RolesAuthorize([]string{RoleAdmin}, route.Permissions, route.AdminOnly) {
			t.Fatalf("admin should authorize Feed Direction counts exception action: %s", path)
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

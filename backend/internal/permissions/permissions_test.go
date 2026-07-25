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
		{RolePCDirector, GoatWriteHealth, true},
		{RoleVerifier, GoatWriteHealth, false},
		{RoleVerifier, GoatWriteIdentity, true},
		{RoleParkHead, GoatWriteHealth, false},
		{RoleCEOInternal, OperatorsManageCapability, true},
		{RoleParkHead, OperatorsManageRoster, true},
		{RoleOperator, AppBootstrap, true},
		{RoleVerifier, AppBootstrap, true},
		{RoleOperator, OperatorsRead, false},
		{RoleVerifier, OperatorsManageCapability, false},
		{RoleCEOInternal, SOPPublish, true},
		{RoleParkHead, TaskAssign, true},
		{RoleParkHead, VaccinationRead, true},
		{RoleCEOInternal, VaccinationOverviewRead, true},
		{RoleOperator, VaccinationOverviewRead, false},
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
		{RoleCEOInternal, CalendarAction, true},
		{RoleParkHead, CalendarRead, true},
		{RoleParkHead, CalendarAction, true},
		{RoleVerifier, CalendarRead, true},
		{RoleVerifier, CalendarAction, false},
		{RoleOperator, CalendarRead, true},
		{RoleCEOInternal, AdminWebBootstrap, true},
		{RoleCEOInternal, OperationsRepair, true},
		{RolePCDirector, OperationsRepair, false},
		{RoleParkHead, OperationsRepair, false},
		{RolePCDirector, AdminWebBootstrap, true},
		{RoleVerifier, AdminWebBootstrap, false},
		{RoleParkHead, AdminWebBootstrap, false},
		{RoleOperator, AdminWebBootstrap, false},
		{RoleVerifier, VerificationReview, true},
		{RoleCEOInternal, VerificationReview, true},
		{RoleOperator, VerificationReview, false},
		{RoleParkHead, VerificationReview, false},
		{RolePCDirector, VerificationReview, false},
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
		{"GET", "/herd-register/summary"},
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
		{"POST", "/app/tasks/63000000-0000-4000-8000-000000000001/scan-captures"},
		{"POST", "/app/tasks/63000000-0000-4000-8000-000000000001/scan-attempts"},
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
		{"GET", "/vaccination/action-center/counts"},
		{"GET", "/vaccination/adherence"},
		{"GET", "/control-tower/vaccination"},
		{"GET", "/workflows/feed_projection_exception:77000000-0000-4000-8000-000000000001"},
		{"GET", "/vaccination/workflows/batch:66000000-0000-4000-8000-000000000001:rule:65000000-0000-4000-8000-000000000001:shed:55000000-0000-4000-8000-000000000001"},
		{"GET", "/vaccination/operations"},
		{"GET", "/vaccination/execution"},
		{"GET", "/vaccination/execution/sheds/55000000-0000-4000-8000-000000000001"},
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
		{"GET", "/admin/roster/positions"},
		{"POST", "/admin/roster/positions"},
		{"GET", "/admin/roster/leave"},
		{"POST", "/admin/roster/leave"},
		{"GET", "/admin/roster/leave/87000000-0000-4000-8000-000000000001"},
		{"POST", "/admin/roster/leave/87000000-0000-4000-8000-000000000001/approve"},
		{"POST", "/admin/roster/leave/87000000-0000-4000-8000-000000000001/resolve-coverage"},
		{"GET", "/admin/roster/vaccination-owner"},
		{"GET", "/verification/queue"},
		{"POST", "/verification/items/98000000-0000-4000-8000-000000000001/verdict"},
		{"POST", "/verification/vaccination-batches/98000000-0000-4000-8000-000000000001/close"},
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
		{"GET", "/vaccination/action-center/counts"},
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

// TestAppVaccinationExecutionRoutesAuthorizeOperator guards against
// regressing the app-tier vaccination execution routes back onto admin-tier
// permissions RoleOperator lacks (P1 fix mirroring commit 6c8962be's /app/roster/*
// precedent): field operators must be able to scan a shed roster and reschedule
// an obligation from the mobile app, while the admin /vaccination/* routes stay
// admin-gated.
func TestAppVaccinationExecutionRoutesAuthorizeOperator(t *testing.T) {
	for _, item := range []struct {
		method      string
		path        string
		operationID string
	}{
		{"GET", "/app/vaccination/execution", "appListVaccinationExecution"},
		{"GET", "/app/vaccination/execution/sheds/55000000-0000-4000-8000-000000000001", "appGetVaccinationExecutionShedDrilldown"},
		{"GET", "/app/vaccination/execution/sheds/55000000-0000-4000-8000-000000000001/roster", "appScanRoster"},
		{"POST", "/app/vaccination/obligations/86000000-0000-4000-8000-000000001001/reschedule", "appRescheduleObligation"},
	} {
		route, ok := Match(item.method, item.path)
		if !ok {
			t.Fatalf("app vaccination route is not registered: %s %s", item.method, item.path)
		}
		if route.OperationID != item.operationID {
			t.Fatalf("operation_id=%q, want %s", route.OperationID, item.operationID)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != AppBootstrap {
			t.Fatalf("permissions=%v, want [%s] so any authenticated app user (operators + leadership) can execute", route.Permissions, AppBootstrap)
		}
		if !RolesAuthorize([]string{RoleOperator}, route.Permissions, route.AdminOnly) {
			t.Fatalf("operator must authorize app vaccination execution route: %s", item.path)
		}
	}

	// The admin /vaccination/* routes must stay on their existing admin-tier
	// perms -- this fix must not broaden them.
	adminRoute, ok := Match("GET", "/vaccination/execution/sheds/55000000-0000-4000-8000-000000000001")
	if !ok {
		t.Fatal("admin vaccination execution shed route is not registered")
	}
	if RolesAuthorize([]string{RoleOperator}, adminRoute.Permissions, adminRoute.AdminOnly) {
		t.Fatal("operator must NOT authorize the admin vaccination execution shed route")
	}
}

func TestFeedDirectionBackendRouteSmokeAvoidsRouteNotRegistered(t *testing.T) {
	for _, item := range []struct {
		path        string
		operationID string
	}{
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
		// Maintainer decision 2026-07-22: operators now hold protocol.read, so they DO authorize the
		// Feed Direction read surface (the Feed vertical is visible to the operator on the phone).
		if !RolesAuthorize([]string{RoleOperator}, route.Permissions, route.AdminOnly) {
			t.Fatalf("operator should authorize Feed Direction read route via protocol.read: %s", item.path)
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
		if !RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, route.AdminOnly) {
			t.Fatalf("ceo_internal should authorize Feed Direction counts exception action: %s", path)
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

// TestVerificationQueueAndVerdictRoutesAreRegistered guards the generic Verification vertical's two
// new routes (context/architecture/verification-module-design.md) and their exact permission gate.
func TestVerificationQueueAndVerdictRoutesAreRegistered(t *testing.T) {
	for _, item := range []struct {
		method      string
		path        string
		operationID string
		permission  string
	}{
		{"GET", "/verification/queue", "listVerificationQueue", VerificationReview},
		{"POST", "/verification/items/98000000-0000-4000-8000-000000000001/verdict", "recordVerificationVerdict", VerificationReview},
		{"GET", "/verification/action-queue", "listVerificationActionQueue", VerificationAct},
		{"POST", "/verification/items/98000000-0000-4000-8000-000000000001/close", "closeVerificationItem", VerificationAct},
		{"POST", "/verification/submissions/98000000-0000-4000-8000-000000000001/close", "closeVerificationSubmission", VerificationAct},
		{"POST", "/verification/vaccination-batches/98000000-0000-4000-8000-000000000001/close", "closeVaccinationBatch", VerificationAct},
	} {
		route, ok := Match(item.method, item.path)
		if !ok {
			t.Fatalf("verification route is not registered: %s %s", item.method, item.path)
		}
		if route.OperationID != item.operationID {
			t.Fatalf("operation_id=%q, want %s", route.OperationID, item.operationID)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != item.permission {
			t.Fatalf("permissions=%v, want [%s]", route.Permissions, item.permission)
		}
	}
}

// TestVerificationSeparationOfDuty is the mandatory separation-of-duty gate from
// context/architecture/org-role-model.md: capture (Operator/Manager) != verify (Verifier) != act
// (Head/Director). An operator holding only capture permissions must NEVER be able to review/verdict
// a verification item; the Verifier role must.
func TestVerificationSeparationOfDuty(t *testing.T) {
	route, ok := Match("POST", "/verification/items/98000000-0000-4000-8000-000000000001/verdict")
	if !ok {
		t.Fatal("recordVerificationVerdict route is not registered")
	}
	if RolesAuthorize([]string{RoleOperator}, route.Permissions, route.AdminOnly) {
		t.Fatal("operator (capture role) must NOT authorize verification verdict — separation of duty violated")
	}
	if RolesAuthorize([]string{RoleParkHead}, route.Permissions, route.AdminOnly) {
		t.Fatal("park head (act role) must NOT authorize verification verdict — separation of duty violated")
	}
	if RolesAuthorize([]string{RolePCDirector}, route.Permissions, route.AdminOnly) {
		t.Fatal("pc_director (act role) must NOT authorize verification verdict — separation of duty violated")
	}
	if !RolesAuthorize([]string{RoleVerifier}, route.Permissions, route.AdminOnly) {
		t.Fatal("verifier must authorize verification verdict")
	}
	if !RoleHasPermission(RoleVerifier, VerificationReview) {
		t.Fatal("verifier role must hold verification.review")
	}
	if RoleHasPermission(RoleOperator, VerificationReview) {
		t.Fatal("operator role must not hold verification.review")
	}
}

// TestAppCountsWriteRoutesAllowOperatorsWithoutAdminGoatGrants pins the maintainer decision that
// field operators may record shifting, birth, AND death from the mobile app, and pins the reason it
// is a dedicated permission: the operator role must gain exactly that surface, not the admin
// /admin/goats/* write surface.
func TestAppCountsWriteRoutesAllowOperatorsWithoutAdminGoatGrants(t *testing.T) {
	for _, pattern := range []string{
		"/app/counts/shifting-events",
		"/app/counts/birth-events",
		"/app/counts/death-events",
	} {
		t.Run(pattern, func(t *testing.T) {
			route, ok := Match("POST", pattern)
			if !ok {
				t.Fatalf("%s is not registered", pattern)
			}
			if len(route.Permissions) != 1 || route.Permissions[0] != CountsWrite {
				t.Fatalf("permissions=%v, want [%s]", route.Permissions, CountsWrite)
			}
			if !RolesAuthorize([]string{RoleOperator}, route.Permissions, route.AdminOnly) {
				t.Fatalf("operator must authorize %s", pattern)
			}
			for _, role := range []string{RoleCEOInternal, RoleParkHead} {
				if !RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
					t.Fatalf("%s must authorize %s", role, pattern)
				}
			}
			// Maintainer decision 2026-07-18: the Counts module is scoped to ground
			// capture (Operator, Park Head) plus full CEO/CXO oversight. PC Director
			// and Verifier are excluded from Counts entirely — they do not see the
			// module in nav, and must not reach its write routes either.
			for _, role := range []string{RolePCDirector, RoleVerifier} {
				if RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
					t.Fatalf("%s must not authorize %s", role, pattern)
				}
			}
		})
	}

	// The widened surface must not have leaked into the admin goat write grants.
	if RoleHasPermission(RoleOperator, GoatWriteIdentity) || RoleHasPermission(RoleOperator, GoatWriteHealth) {
		t.Fatal("operator must not gain admin goat write permissions from the Counts app write surface")
	}
	if RolesAuthorize([]string{RoleOperator}, []string{GoatWriteHealth}, false) {
		t.Fatal("operator must still be denied the admin critical-death-exit route")
	}
}

// TestAppCountsShiftingDestinationsIsReachableByOperators pins the gate on the shifting destination
// catalog.
//
// The regression this guards is specific and would be invisible until a real operator tried to file
// a movement: the catalog is a READ of locations rows, so the obvious instinct is to gate it on the
// admin-tier LocationsRead. RolesAuthorize ANDs a route's permissions, and RoleOperator does not
// hold LocationsRead -- so doing that would leave an operator able to SUBMIT a shifting event but
// unable to load the list of destinations they are allowed to submit, which reads on the phone as
// an empty dropdown rather than as a permission error.
func TestAppCountsShiftingDestinationsIsReachableByOperators(t *testing.T) {
	const pattern = "/app/counts/shifting/destinations"

	route, ok := Match("GET", pattern)
	if !ok {
		t.Fatalf("%s is not registered", pattern)
	}
	if len(route.Permissions) != 1 || route.Permissions[0] != CountsWrite {
		t.Fatalf("permissions=%v, want [%s]", route.Permissions, CountsWrite)
	}
	// The load-bearing assertion: an operator holding CountsWrite and NOT LocationsRead authorizes.
	if RoleHasPermission(RoleOperator, LocationsRead) {
		t.Fatal("test premise broken: RoleOperator now holds LocationsRead, so this no longer proves the gate is CountsWrite-only")
	}
	if !RolesAuthorize([]string{RoleOperator}, route.Permissions, route.AdminOnly) {
		t.Fatalf("operator must authorize %s without LocationsRead", pattern)
	}
	for _, role := range []string{RoleCEOInternal, RoleParkHead} {
		if !RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
			t.Fatalf("%s must authorize %s", role, pattern)
		}
	}
	// The catalog exposes the same surface the write routes do, so it inherits their exclusions:
	// roles kept out of Counts entirely must not reach it either.
	for _, role := range []string{RolePCDirector, RoleVerifier} {
		if RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
			t.Fatalf("%s must not authorize %s", role, pattern)
		}
	}
}

// TestGroundCaptureTiersHoldCountsWrite pins CountsWrite onto the composite org roles that perform
// ground capture, and off the tiers that act on verified work instead of capturing it.
func TestGroundCaptureTiersHoldCountsWrite(t *testing.T) {
	for _, tier := range []Tier{TierAssistantManager, TierManager} {
		role := RoleKey(tier, VerticalHealth)
		if !RoleHasPermission(role, CountsWrite) {
			t.Fatalf("%q should hold CountsWrite (ground capture tier)", role)
		}
	}
	for _, tier := range []Tier{TierHead, TierDirector} {
		role := RoleKey(tier, VerticalHealth)
		if RoleHasPermission(role, CountsWrite) {
			t.Fatalf("%q must NOT hold CountsWrite (capture is ground-only)", role)
		}
	}
}

// TestFeedConfigRoutesAreRegisteredWithDedicatedPermissions pins the authored feed-configuration
// surface onto its OWN permissions.
//
// The regression this blocks is reusing ProtocolRead/ProtocolWrite (the feed-DIRECTION gate) for
// these routes. Direction is today's operational output; this is the standing rule that produced it.
// A park head who may look at this morning's feed sheet must not thereby be able to read -- let
// alone rewrite -- the tenant-wide ration grid the whole farm is fed from.
func TestFeedConfigRoutesAreRegisteredWithDedicatedPermissions(t *testing.T) {
	reads := []struct {
		path        string
		operationID string
	}{
		{"/feed-config/ration-rates", "listFeedConfigRationRates"},
		{"/feed-config/ration-groups", "listFeedConfigRationGroups"},
		{"/feed-config/shed-tags", "listFeedConfigShedTags"},
		{"/feed-config/feed-items", "listFeedConfigFeedItems"},
		{"/feed-config/session-templates", "listFeedConfigSessionTemplates"},
		{"/feed-config/schedule", "listFeedConfigSchedule"},
		{"/feed-config/shed-factors", "listFeedConfigShedFactors"},
	}
	for _, item := range reads {
		route, ok := Match("GET", item.path)
		if !ok {
			t.Fatalf("feed config read route is not registered: %s", item.path)
		}
		if route.OperationID != item.operationID {
			t.Fatalf("operation_id=%q, want %s", route.OperationID, item.operationID)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != FeedConfigRead {
			t.Fatalf("%s permissions=%v, want [%s]", item.path, route.Permissions, FeedConfigRead)
		}
	}

	writes := []struct {
		path        string
		operationID string
	}{
		{"/feed-config/ration-rates", "upsertFeedConfigRationRate"},
		{"/feed-config/shed-factors", "upsertFeedConfigShedFactor"},
		{"/feed-config/schedule", "upsertFeedConfigSchedule"},
	}
	for _, item := range writes {
		route, ok := Match("POST", item.path)
		if !ok {
			t.Fatalf("feed config write route is not registered: POST %s", item.path)
		}
		if route.OperationID != item.operationID {
			t.Fatalf("operation_id=%q, want %s", route.OperationID, item.operationID)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != FeedConfigWrite {
			t.Fatalf("POST %s permissions=%v, want [%s]", item.path, route.Permissions, FeedConfigWrite)
		}
	}
}

// TestFeedConfigWriteIsAdminAndCEOOnly pins WHO may author the ration grid.
//
// A ration rate is a standing feeding instruction for every animal matching its key, and an
// incorrect one produces no alert at all -- just thinner animals a month later. So authoring sits
// with the tier that owns farm economics, never with the ground roles that execute feeding, and
// never with the verifier (who must not rewrite the standard captured work is judged against).
func TestFeedConfigWriteIsAdminAndCEOOnly(t *testing.T) {
	writeRoute, ok := Match("POST", "/feed-config/ration-rates")
	if !ok {
		t.Fatal("feed config write route is not registered")
	}
	readRoute, ok := Match("GET", "/feed-config/ration-rates")
	if !ok {
		t.Fatal("feed config read route is not registered")
	}

	// The founder/builder visibility invariant: ceo_internal must never be locked out of a built
	// visible module.
	if !RolesAuthorize([]string{RoleCEOInternal}, readRoute.Permissions, readRoute.AdminOnly) {
		t.Fatalf("%s must authorize the feed config read route", RoleCEOInternal)
	}
	if !RolesAuthorize([]string{RoleCEOInternal}, writeRoute.Permissions, writeRoute.AdminOnly) {
		t.Fatalf("%s must authorize the feed config write route", RoleCEOInternal)
	}

	for _, role := range []string{RoleOperator, RoleParkHead, RoleVerifier, RolePCDirector} {
		if RolesAuthorize([]string{role}, writeRoute.Permissions, writeRoute.AdminOnly) {
			t.Fatalf("%s must NOT be able to author feed configuration", role)
		}
		if RolesAuthorize([]string{role}, readRoute.Permissions, readRoute.AdminOnly) {
			t.Fatalf("%s must NOT be able to read the authored feed configuration grid", role)
		}
	}
}

// TestFeedConfigPermissionsAreSeparateFromFeedDirection proves the two surfaces did not collapse
// into one authority. Holding the feed-direction gate must not grant feed-config access.
func TestFeedConfigPermissionsAreSeparateFromFeedDirection(t *testing.T) {
	if FeedConfigRead == ProtocolRead || FeedConfigWrite == ProtocolWrite {
		t.Fatal("feed config permissions must be distinct from the feed direction (protocol) permissions")
	}
	directionRoute, ok := Match("GET", "/feed-direction/generation-preview")
	if !ok {
		t.Fatal("feed direction route is not registered")
	}
	// RoleParkHead holds ProtocolRead and can therefore see today's direction preview...
	if !RolesAuthorize([]string{RoleParkHead}, directionRoute.Permissions, directionRoute.AdminOnly) {
		t.Fatal("park head should still authorize the feed direction preview")
	}
	// ...but must not thereby reach the authored grid behind it.
	if RoleHasPermission(RoleParkHead, FeedConfigRead) || RoleHasPermission(RoleParkHead, FeedConfigWrite) {
		t.Fatal("park head must not hold feed config permissions via the feed direction grant")
	}
}

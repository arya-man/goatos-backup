package permissions

import "testing"

// TestWeighingExecutorCanUploadProof pins the authorization symmetry that a weighing
// executor's work depends on: recording a weighing observation and attaching its
// MANDATORY video proof are one indivisible act, so any role authorized for
// appRecordWeighingShedObservation / appRecordWeighingAnimalObservation
// (permissions.WeighingExecute) must also be authorized for the proof-upload
// handshake those writes require.
//
// Live defect this pins (2026-08-03, phone-QA): a Growth Director (weighing.execute,
// deliberately NOT task.execute) opened lump-sum weighing, entered a valid total weight
// and animal count, recorded the group video — and POST /app/proofs/uploads answered 403
// permission_denied on every attempt. No proof could ever reach SYNCED, so the Submit gate
// (WeighingScreen.canRecordShedPartition, which requires at least one SYNCED proof) stayed
// permanently disabled. The role could do the weighing but could never prove it.
//
// The proof routes were gated on task.execute alone. task.execute is the OPERATOR's
// general task-execution grant and deliberately carries vaccination task execution with it,
// so widening growth_director to hold it would over-grant. The proof handshake is instead
// an either/or surface: task.execute OR weighing.execute.
func TestWeighingExecutorCanUploadProof(t *testing.T) {
	proofWriteOperations := []string{
		"createProofUpload",
		"uploadProofLocal",
		"completeProofUpload",
		"deleteUnattachedProofUpload",
	}
	// Every role that may execute weighing must be able to complete the proof handshake.
	weighingExecutors := []string{RoleOperator, RoleGrowthDirector}

	byOperation := map[string]Route{}
	for _, route := range ProtectedRoutes() {
		byOperation[route.OperationID] = route
	}

	for _, operation := range proofWriteOperations {
		route, ok := byOperation[operation]
		if !ok {
			t.Fatalf("proof route %s is not registered in protectedRoutes", operation)
		}
		for _, role := range weighingExecutors {
			if !RoleHasPermission(role, WeighingExecute) {
				t.Fatalf("test premise broken: %s must hold weighing.execute", role)
			}
			if !AuthorizeRoute(route, []string{role}) {
				t.Errorf(
					"%s holds weighing.execute but is NOT authorized for %s (%s %s): "+
						"the weighing write is allowed while its mandatory proof upload is denied, "+
						"so the operator can never submit",
					role, operation, route.Method, route.Pattern,
				)
			}
		}
	}
}

func TestFeedExecutorCanUploadProof(t *testing.T) {
	proofWriteOperations := []string{
		"createProofUpload",
		"uploadProofLocal",
		"completeProofUpload",
		"deleteUnattachedProofUpload",
	}

	byOperation := map[string]Route{}
	for _, route := range ProtectedRoutes() {
		byOperation[route.OperationID] = route
	}

	for _, operation := range proofWriteOperations {
		route, ok := byOperation[operation]
		if !ok {
			t.Fatalf("proof route %s is not registered in protectedRoutes", operation)
		}
		if !RoleHasPermission(RoleParkHead, FeedDirectionComplete) {
			t.Fatal("test premise broken: park_head must hold feed_direction.complete")
		}
		if !AuthorizeRoute(route, []string{RoleParkHead}) {
			t.Errorf("park_head holds feed_direction.complete but is not authorized for %s", operation)
		}
	}
}

func TestAppAnalyticsRouteIsRegisteredForAuthenticatedAppClients(t *testing.T) {
	route, ok := Match("POST", "/app/analytics/events")
	if !ok {
		t.Fatal("POST /app/analytics/events must be a protected route")
	}
	if !AuthorizeRoute(route, []string{RoleOperator}) {
		t.Fatal("operator app clients must be authorized to mirror analytics events")
	}
}

// TestWeighingExecuteIsNotTaskExecuteEscalation is the privilege-escalation guard on the fix
// above, and it must pass BOTH before and after it. The tempting one-line "fix" was to add
// TaskExecute to growth_director; TaskExecute is the OPERATOR's broad task-execution grant and
// carries SOP submission and scan capture for every vertical (vaccination included) with it,
// which this role is explicitly asserted not to have (see permissions_test.go). Widening the
// proof ROUTES to accept weighing.execute must not widen anything else: holding
// weighing.execute alone still authorizes no task submission and no vaccination surface.
//
// Note what did NOT change: permissions_test.go's `{RoleGrowthDirector, TaskExecute, false}`
// stays false and stays correct. The defect was never that the role was missing a permission —
// it was that a route required the wrong one.
func TestWeighingExecuteIsNotTaskExecuteEscalation(t *testing.T) {
	if RoleHasPermission(RoleGrowthDirector, TaskExecute) {
		t.Fatal("growth_director must NOT hold task.execute: it carries every vertical's task execution")
	}
	// SOP task execution stays closed: these are the routes an operator uses to execute a
	// vaccination task, and they remain gated on task.execute alone.
	for _, operation := range []string{"submitAppTask", "recordAppScanCapture", "recordAppScanAttempt"} {
		var route Route
		found := false
		for _, candidate := range ProtectedRoutes() {
			if candidate.OperationID == operation {
				route, found = candidate, true
				break
			}
		}
		if !found {
			t.Fatalf("route %s is not registered in protectedRoutes", operation)
		}
		if AuthorizeRoute(route, []string{RoleGrowthDirector}) {
			t.Errorf("growth_director must NOT be authorized for %s: weighing execution is not task execution", operation)
		}
	}
	// And the vaccination surface itself stays closed.
	for _, permission := range []string{VaccinationRead, VaccinationCampaign} {
		if RoleHasPermission(RoleGrowthDirector, permission) {
			t.Errorf("growth_director must NOT hold %s", permission)
		}
	}
}

// TestProofUploadStaysClosedToNonExecutors guards the widening above from becoming a
// blanket grant: broadening the proof handshake to weighing.execute must not open it to
// roles that execute no work at all.
func TestProofUploadStaysClosedToNonExecutors(t *testing.T) {
	route, ok := Match("POST", "/app/proofs/uploads")
	if !ok {
		t.Fatal("POST /app/proofs/uploads must be a protected route")
	}
	// pc_director is deliberately absent: it already holds task.execute today, so it could
	// always create proof uploads. The guard is on roles that execute NEITHER kind of work.
	for _, role := range []string{RoleFeedDirector, RoleHealthDirector} {
		if RoleHasPermission(role, TaskExecute) || RoleHasPermission(role, WeighingExecute) || RoleHasPermission(role, FeedDirectionComplete) {
			t.Fatalf("test premise broken: %s must hold no proof-producing execute permission", role)
		}
		if AuthorizeRoute(route, []string{role}) {
			t.Errorf("%s executes no weighing or task work and must not create proof uploads", role)
		}
	}
}

package permissions

import "testing"

// Leadership Tasks (maintainer decision 2026-09-04): directors ASK, the CXO desk ANSWERS.
// Three edges, each deliberate and each mutation-tested when written:
//
//  1. RAISE belongs to the director roles that carry a phone (pc, growth, feed, health)
//     and NOT to ceo_internal -- the desk does not send itself asks.
//  2. ACT (be assigned, change status) belongs to ceo_internal ALONE.
//  3. Nobody below leadership -- operator, park head, verifier, the per-person roles --
//     reaches the module at all.
func TestLeadershipTasksRaiseIsDirectorsAndActIsCEO(t *testing.T) {
	directors := []string{RolePCDirector, RoleGrowthDirector, RoleFeedDirector, RoleHealthDirector, RoleBreedingDirector, RoleProcurementDirector}
	for _, role := range directors {
		if !RoleHasPermission(role, LeadershipTasksRead) || !RoleHasPermission(role, LeadershipTasksRaise) {
			t.Errorf("%s must read and raise leadership tasks", role)
		}
		if RoleHasPermission(role, LeadershipTasksAct) {
			t.Errorf("%s must NOT act on leadership tasks -- the CXO desk is the audience", role)
		}
	}
	if !RoleHasPermission(RoleCEOInternal, LeadershipTasksRead) || !RoleHasPermission(RoleCEOInternal, LeadershipTasksAct) {
		t.Error("ceo_internal must read and act on leadership tasks")
	}
	if RoleHasPermission(RoleCEOInternal, LeadershipTasksRaise) {
		t.Error("ceo_internal must NOT raise leadership tasks -- directors ask the desk, not the reverse")
	}
	for _, role := range []string{RoleOperator, RoleParkHead, RoleVerifier, RoleCountsApprover, RoleToxinTester, RoleProcurementManager} {
		for _, perm := range []string{LeadershipTasksRead, LeadershipTasksRaise, LeadershipTasksAct} {
			if RoleHasPermission(role, perm) {
				t.Errorf("%s must not hold %s", role, perm)
			}
		}
	}
}

// TestLeadershipTaskRoutesAreGatedOnTheDedicatedPermissions pins the route table: the
// raise/edit routes and the assignee picker refuse a CXO, the list/detail/status/seen
// routes accept both parties (the domain rule decides who may move a task), and an
// operator reaches none of them.
func TestLeadershipTaskRoutesAreGatedOnTheDedicatedPermissions(t *testing.T) {
	const id = "98000000-0000-4000-8000-000000000001"
	raiseOnly := []struct{ method, path string }{
		{"POST", "/app/leadership-tasks"},
		{"POST", "/app/leadership-tasks/" + id + "/edit"},
		{"GET", "/app/leadership-tasks/assignees"},
	}
	bothParties := []struct{ method, path string }{
		{"GET", "/app/leadership-tasks"},
		{"GET", "/app/leadership-tasks/" + id},
		{"POST", "/app/leadership-tasks/" + id + "/status"},
		{"POST", "/app/leadership-tasks/" + id + "/seen"},
	}
	for _, target := range raiseOnly {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if !RolesAuthorize([]string{RoleFeedDirector}, route.Permissions, route.AdminOnly) {
			t.Errorf("feed_director must authorize %s %s", target.method, target.path)
		}
		if RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, route.AdminOnly) {
			t.Errorf("ceo_internal must NOT authorize %s %s", target.method, target.path)
		}
	}
	for _, target := range bothParties {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		for _, role := range []string{RoleFeedDirector, RolePCDirector, RoleCEOInternal} {
			if !RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
				t.Errorf("%s must authorize %s %s", role, target.method, target.path)
			}
		}
		if RolesAuthorize([]string{RoleOperator}, route.Permissions, route.AdminOnly) {
			t.Errorf("operator must NOT authorize %s %s", target.method, target.path)
		}
	}
	// The assignee picker must not be reachable on read alone: a guessed URL from a CXO's
	// phone would list the leadership desk to someone with no reason to see it there.
	picker, _ := Match("GET", "/app/leadership-tasks/assignees")
	if RolesAuthorize([]string{RoleCEOInternal}, picker.Permissions, picker.AdminOnly) {
		t.Error("the assignee picker must ride leadership_tasks.raise, not read")
	}
}

// TestLeadershipTaskPartiesReachAttachmentMedia pins the download lever: every role that
// can open a task can also fetch the attachment it points at, and every raiser can finish
// the upload handshake. A director who could attach a file nobody can open, or a CXO who
// sees a paperclip that 403s, is the exact defect TestToxinReviewerReachesProofMedia
// recorded for toxin.
func TestLeadershipTaskPartiesReachAttachmentMedia(t *testing.T) {
	download, ok := Match("GET", "/app/proofs/98000000-0000-4000-8000-000000000001/download")
	if !ok {
		t.Fatal("the proof download route is not registered")
	}
	readers := 0
	for role := range rolePermissions {
		if !RoleHasPermission(role, LeadershipTasksRead) {
			continue
		}
		readers++
		if !AuthorizeRoute(download, []string{role}) {
			t.Errorf("%s can open a leadership task but cannot download its attachments", role)
		}
	}
	if readers == 0 {
		t.Fatal("no role holds leadership_tasks.read; this test would pass vacuously")
	}
	for _, target := range []struct{ method, path string }{
		{"POST", "/app/proofs/uploads"},
		{"PUT", "/app/proofs/98000000-0000-4000-8000-000000000001/upload"},
		{"POST", "/app/proofs/98000000-0000-4000-8000-000000000001/complete"},
		{"DELETE", "/app/proofs/98000000-0000-4000-8000-000000000001"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		for role := range rolePermissions {
			if RoleHasPermission(role, LeadershipTasksRaise) && !AuthorizeRoute(route, []string{role}) {
				t.Errorf("%s raises leadership tasks but cannot %s %s", role, target.method, target.path)
			}
		}
	}
}

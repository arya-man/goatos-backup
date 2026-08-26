package permissions

import "testing"

// TestToxinVerdictIsCEOOnly pins the maintainer decision (2026-08-25) that the toxin
// accept/reject belongs to CEO/CXO ALONE. Three edges, each deliberate:
//
//  1. The tenant VERIFIER must not reach toxin work at all — this module is an approval
//     gate outside the generic Verification vertical precisely so the 2026-08-03 verifier
//     verdict-exclusivity lock stays untouched.
//  2. The TESTER (toxin_tester, and the park_head/director jobs its holders carry) must
//     not review their own test.
//  3. ceo_internal is the ONLY authorizer.
func TestToxinVerdictIsCEOOnly(t *testing.T) {
	for _, target := range []struct{ method, path string }{
		{"POST", "/toxin/tasks/98000000-0000-4000-8000-000000000001/verdict"},
		{"GET", "/toxin/review"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		for _, role := range []string{RoleVerifier, RoleToxinTester, RoleParkHead, RolePCDirector, RoleGrowthDirector, RoleFeedDirector, RoleHealthDirector, RoleOperator, RoleCountsApprover} {
			if RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
				t.Fatalf("%s must NOT authorize %s %s — toxin review is CEO/CXO only", role, target.method, target.path)
			}
		}
		if !RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, route.AdminOnly) {
			t.Fatalf("ceo_internal must authorize %s %s", target.method, target.path)
		}
	}
	// The other side of the wall: toxin never rides the verifier's permission, and the
	// verifier never gains a toxin permission.
	if RoleHasPermission(RoleVerifier, ToxinRead) || RoleHasPermission(RoleVerifier, ToxinExecute) || RoleHasPermission(RoleVerifier, ToxinVerdict) {
		t.Fatal("verifier must hold no toxin permission — the verifier never sees toxin work")
	}
	if RoleHasPermission(RoleCEOInternal, VerificationVerdict) {
		t.Fatal("granting toxin must not have re-opened verification.verdict to the CEO — that lock stands")
	}
}

// TestToxinTesterCarriesOnlyTestingAuthority pins the per-person role's shape: read +
// execute and NOTHING else, so a grant of toxin_tester alone reaches exactly the toxin
// tester surface (rendered through the holder's real job role, which owns bootstrap).
func TestToxinTesterCarriesOnlyTestingAuthority(t *testing.T) {
	perms := rolePermissions[RoleToxinTester]
	if len(perms) != 2 {
		t.Fatalf("toxin_tester carries %d permissions, want exactly toxin.read + toxin.execute", len(perms))
	}
	for _, want := range []string{ToxinRead, ToxinExecute} {
		if _, ok := perms[want]; !ok {
			t.Fatalf("toxin_tester must hold %s", want)
		}
	}
	// The tester's step-work routes really resolve for the role.
	for _, target := range []struct{ method, path string }{
		{"GET", "/app/toxin/tasks"},
		{"GET", "/app/toxin/tasks/98000000-0000-4000-8000-000000000001"},
		{"POST", "/app/toxin/tasks/98000000-0000-4000-8000-000000000001/steps/3/complete"},
		{"POST", "/app/toxin/tasks/98000000-0000-4000-8000-000000000001/submit"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if !RolesAuthorize([]string{RoleToxinTester}, route.Permissions, route.AdminOnly) {
			t.Fatalf("toxin_tester must authorize %s %s", target.method, target.path)
		}
	}
	// A bare park_head/director job inherits no tester surface — the authority is per
	// person, never per job (the counts_approver precedent).
	listRoute, _ := Match("GET", "/app/toxin/tasks")
	for _, role := range []string{RoleParkHead, RolePCDirector, RoleGrowthDirector, RoleFeedDirector, RoleHealthDirector, RoleOperator, RoleVerifier} {
		if RolesAuthorize([]string{role}, listRoute.Permissions, listRoute.AdminOnly) {
			t.Fatalf("%s must not reach the toxin task list by job role alone", role)
		}
	}
}

// TestToxinExecuteReachesProofUploads pins the either/or proof-route lever: a
// toxin_tester who is not also a general operator must still complete the proof
// handshake, or every step video is permanently unsubmittable (the exact weighing
// phone-QA 2026-08-03 failure shape recorded on createProofUpload).
func TestToxinExecuteReachesProofUploads(t *testing.T) {
	for _, target := range []struct{ method, path string }{
		{"POST", "/app/proofs/uploads"},
		{"GET", "/app/proofs/uploads"},
		{"PUT", "/app/proofs/98000000-0000-4000-8000-000000000001/upload"},
		{"POST", "/app/proofs/98000000-0000-4000-8000-000000000001/complete"},
		{"DELETE", "/app/proofs/98000000-0000-4000-8000-000000000001"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if !RolesAuthorize([]string{RoleToxinTester}, append(append([]string{}, route.Permissions...), route.AnyPermissions...), route.AdminOnly) &&
			!anyAuthorizes(RoleToxinTester, route.AnyPermissions) {
			t.Fatalf("toxin_tester must reach %s %s through the either/or proof lever", target.method, target.path)
		}
	}
}

func anyAuthorizes(role string, anyPerms []string) bool {
	for _, p := range anyPerms {
		if RoleHasPermission(role, p) {
			return true
		}
	}
	return false
}

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

// TestToxinExecuteIsTesterOnlyAndNeverCEO pins the maintainer decision (2026-08-26) that
// splits WATCHING from DOING: CEO/CXO sees every toxin task and casts the verdict, but
// never runs a step. The defect this replaces shipped and was caught on the phone — a CEO
// principal opened a task card and could film step 1, which would have let the same person
// produce the evidence and then accept it.
//
// Read is kept deliberately (the module must stay visible to leadership, and the phone
// card renders read-only from it); execute is the half that is withheld.
func TestToxinExecuteIsTesterOnlyAndNeverCEO(t *testing.T) {
	if RoleHasPermission(RoleCEOInternal, ToxinExecute) {
		t.Fatal("ceo_internal must NOT hold toxin.execute — leadership watches and judges, the named testers run the test")
	}
	if !RoleHasPermission(RoleCEOInternal, ToxinRead) || !RoleHasPermission(RoleCEOInternal, ToxinVerdict) {
		t.Fatal("ceo_internal must keep toxin.read (the module stays visible) and toxin.verdict (the accept/reject)")
	}
	// The step-work routes refuse a CEO for real, not just in the permission map.
	for _, target := range []struct{ method, path string }{
		{"POST", "/app/toxin/tasks/98000000-0000-4000-8000-000000000001/steps/1/complete"},
		{"POST", "/app/toxin/tasks/98000000-0000-4000-8000-000000000001/submit"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, route.AdminOnly) {
			t.Fatalf("ceo_internal must NOT authorize %s %s", target.method, target.path)
		}
	}
	// ...while the read routes still resolve, so the module does not vanish for leadership.
	for _, target := range []struct{ method, path string }{
		{"GET", "/app/toxin/tasks"},
		{"GET", "/app/toxin/tasks/98000000-0000-4000-8000-000000000001"},
	} {
		route, ok := Match(target.method, target.path)
		if !ok {
			t.Fatalf("%s %s is not registered", target.method, target.path)
		}
		if !RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, route.AdminOnly) {
			t.Fatalf("ceo_internal must still authorize %s %s", target.method, target.path)
		}
	}
}

// TestToxinReviewerReachesProofMedia answers a review finding raised against PR #115 and
// pins the answer so it is not re-raised from the same reading.
//
// The finding: "downloadProof is gated on TaskRead only, and the toxin CEO gets
// ToxinRead/ToxinVerdict, not TaskRead — so the reviewer sees the drawer but every strip
// photo 403s." It does not reproduce. The toxin block in RoleCEOInternal is an ADDITION to
// that role's set, not the whole of it: ceo_internal is the founder-visibility role and
// already carries TaskRead among ~80 permissions, so downloadProof authorizes.
//
// Rather than restate that in prose, this asserts the property the finding was really
// about — EVERY principal who can cast a toxin verdict can also fetch the media that
// verdict is about. Adding a ToxinVerdict holder that lacks TaskRead turns this red, which
// is the case the finding imagined and the one worth catching.
//
// The tester is deliberately NOT asserted here: toxin_tester holds no TaskRead and cannot
// call downloadProof. That is latent, not broken — no toxin screen resolves a server proof
// URL (the phone renders its own local capture off the durable proof slot and only ever
// writes proof_ref). If a tester surface ever needs to show a previous attempt's photo,
// widen the route with ToxinRead the way the upload routes already OR in ToxinExecute.
func TestToxinReviewerReachesProofMedia(t *testing.T) {
	route, ok := Match("GET", "/app/proofs/98000000-0000-4000-8000-000000000001/download")
	if !ok {
		t.Fatal("the proof download route is not registered")
	}
	reviewers := 0
	for role := range rolePermissions {
		if !RoleHasPermission(role, ToxinVerdict) {
			continue
		}
		reviewers++
		if !AuthorizeRoute(route, []string{role}) {
			t.Fatalf("%s casts the toxin verdict but cannot download the proof it is judging", role)
		}
	}
	if reviewers == 0 {
		t.Fatal("no role holds toxin.verdict; this test would pass vacuously")
	}
}

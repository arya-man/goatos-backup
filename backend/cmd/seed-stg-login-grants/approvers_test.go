package main

import (
	"os"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// A role key that does not exist in rolePermissions authorizes NOTHING: the grant row inserts fine
// and every permission check on it returns false, so the person appears seeded and can do nothing.
// This catches a typo'd or retired constant at test time instead of on the phone.
func TestPerPersonGrantRolesAreKnownGrantableRoles(t *testing.T) {
	for _, person := range perPersonGrants {
		if len(person.roles) == 0 {
			t.Errorf("%s has no roles; the entry grants nothing at all", person.email)
		}
		for _, role := range person.roles {
			if !permissions.IsKnownRole(role) {
				t.Errorf("%s: role %q is not a known grantable role; the grant would authorize nothing", person.email, role)
			}
		}
	}
}

// Per the STG login contract, a PENDING row alone is not seed-complete: admin-web SSO does not
// reliably trigger the runtime claim path on a fresh seed. So every grantee must have a way to
// resolve a user_id — a committed Firebase UID in stgLoginAccounts, or a roster display name to
// look the existing row up by. Without either, seedPerPersonGrants can only ever stage the grant
// and the person silently has no authority until they happen to sign in.
func TestPerPersonGranteesCanResolveAnActiveUserID(t *testing.T) {
	uidBacked := make(map[string]bool, len(stgLoginAccounts))
	for _, acct := range stgLoginAccounts {
		if strings.TrimSpace(acct.FirebaseUID) != "" {
			uidBacked[strings.ToLower(strings.TrimSpace(acct.Email))] = true
		}
	}
	for _, person := range perPersonGrants {
		email := strings.ToLower(strings.TrimSpace(person.email))
		if uidBacked[email] {
			continue
		}
		if strings.TrimSpace(person.rosterDisplayName) == "" {
			t.Errorf("%s has no committed Firebase UID and no rosterDisplayName; their grants can only ever be PENDING", person.email)
		}
	}
}

// The whole point of this list (maintainer decisions 2026-08-05 and 2026-08-07): the authority
// follows the NAMED PERSON, never the job. If a later change "simplifies" it by moving these
// permissions onto pc_director or growth_director, every future holder of those jobs inherits them
// and this test goes red.
//
// Mutation check performed when written: adding CountsApproveAccess to RolePCDirector's map fails
// this test, and so do TestApprovalsModuleIsPerPersonAndLeavesCountsCaptureOnly and
// TestDirectorHoldsNoOtherModulesCapabilities in internal/permissions.
func TestPerPersonAuthorityDidNotLeakOntoTheJobRoles(t *testing.T) {
	jobRoles := []string{
		permissions.RolePCDirector,
		permissions.RoleGrowthDirector,
		permissions.RoleFeedDirector,
		permissions.RoleHealthDirector,
		permissions.RoleBreedingDirector,
		permissions.RoleParkHead,
	}
	perPersonOnly := []string{
		permissions.CountsApproveAccess,
		permissions.CountsApproveLifecycle,
		permissions.CountsApproveShifting,
	}
	for _, role := range jobRoles {
		for _, permission := range perPersonOnly {
			if permissions.RoleHasPermission(role, permission) {
				t.Errorf("%s holds %s: counts authority is granted per person, not per job", role, permission)
			}
		}
	}
}

// Both named people must actually carry the approval role — this file IS the STG answer to "who can
// approve births, deaths and shifting", so an entry that lists other roles but drops this one is a
// silent removal of authority.
func TestNamedApproversCarryTheApprovalRole(t *testing.T) {
	want := map[string]bool{
		"chandrakanth119527@gmail.com": false,
		"babureddy315@gmail.com":       false,
	}
	for _, person := range perPersonGrants {
		email := strings.ToLower(strings.TrimSpace(person.email))
		if _, tracked := want[email]; !tracked {
			continue
		}
		for _, role := range person.roles {
			if role == permissions.RoleCountsApprover {
				want[email] = true
			}
		}
	}
	for email, found := range want {
		if !found {
			t.Errorf("%s must carry %s; they are a named counts approver", email, permissions.RoleCountsApprover)
		}
	}
}

// A tenant-scoped operator grant is the exception, not the norm (AGENTS.md operator-scope
// invariant), so it must be visibly justified in the source rather than passing silently. The
// machine twin of this check is tools/agent-hooks/check-stg-operator-scope.mjs.
func TestTenantScopedOperatorGrantsAreAnnotated(t *testing.T) {
	raw, err := os.ReadFile("approvers.go")
	if err != nil {
		t.Fatalf("read approvers.go: %v", err)
	}
	operatorGrants := 0
	for _, person := range perPersonGrants {
		for _, role := range person.roles {
			if role == permissions.RoleOperator {
				operatorGrants++
			}
		}
	}
	justifications := strings.Count(string(raw), "stg-operator-scope: tenant approved")
	if justifications < operatorGrants {
		t.Fatalf("%d tenant-scoped operator grant(s) but only %d \"stg-operator-scope: tenant approved\" justification(s) in approvers.go; each one must say why it is not park-scoped",
			operatorGrants, justifications)
	}
}

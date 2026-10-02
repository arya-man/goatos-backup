package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/parkscope"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestAddPersonGrantsEveryDesignationThatIsARole: the Add Person role list is the designation
// catalog, so every designation whose code is a role this backend knows must be grantable, at
// the right scope. Until 2026-10-02 the write path was a closed map of fifteen roles and refused
// Sales Director, Sales Manager, Procurement Director / Manager and HR -- all catalog rows.
func TestAddPersonGrantsEveryDesignationThatIsARole(t *testing.T) {
	want := map[string]string{
		permissions.RoleKey(permissions.TierDirector, permissions.VerticalSales): "tenant",
		permissions.RoleKey(permissions.TierManager, permissions.VerticalSales):  "park",
		permissions.RoleProcurementDirector:                                      "tenant",
		permissions.RoleProcurementManager:                                       "park",
		permissions.RoleHR:                                                       "tenant",
		permissions.RoleBreedingDirector:                                         "tenant",
		permissions.RoleParkHead:                                                 "park",
		permissions.RoleVerifier:                                                 "tenant",
		permissions.RoleKey(permissions.TierManager, permissions.VerticalFeed):   "park",
	}
	for role, scope := range want {
		spec, ok := personRoleSpecFor(role)
		if !ok {
			t.Errorf("%s is a designation and a known role, but Add Person refuses it", role)
			continue
		}
		if spec.ScopeType != scope {
			t.Errorf("%s is granted at %s scope, want %s", role, spec.ScopeType, scope)
		}
		if !validRoleHint(spec.RoleHint) {
			t.Errorf("%s stamps primary_role_hint %q, which the column CHECK refuses", role, spec.RoleHint)
		}
	}
	// The founder/builder cohort is seed-owned; operator is retired (000394).
	for _, role := range []string{permissions.RoleCEOInternal, permissions.RoleOperator, "not_a_role"} {
		if _, ok := personRoleSpecFor(role); ok {
			t.Errorf("%s must never be grantable from the Add Person form", role)
		}
	}
}

// TestEveryFarmWideRoleIsTenantOnlyInParkScope: a role the form grants at tenant scope must be
// one the park-scope writer treats as farm-wide, or the create is refused with "a person with
// only park roles must be limited to parks". Breeding Director shipped in exactly that state.
func TestEveryFarmWideRoleIsTenantOnlyInParkScope(t *testing.T) {
	for _, role := range permissions.KnownRoles() {
		spec, ok := personRoleSpecFor(role)
		if !ok {
			continue
		}
		if (spec.ScopeType == "tenant") != parkscope.TenantOnlyRoles[role] {
			t.Errorf("%s: Add Person scope %s, but park-scope tenant-only=%v -- the create would be refused", role, spec.ScopeType, parkscope.TenantOnlyRoles[role])
		}
	}
}

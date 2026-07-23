package http

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// R50-019: Mixed-grant handler test — hasTenantWidePermission must not
// escape scope. It checks if the actor has the requested permission at the
// tenant scope (ScopeType="tenant", ScopeID=tenantID).
func TestHasTenantWidePermissionLogic(t *testing.T) {
	const testTenant = "tenant-001"
	const testRole = "verifier" // Verifier role has verification.review permission

	tests := []struct {
		name           string
		grants         []permissions.ActiveGrant
		requestedPerm  string
		expectedResult bool
		desc           string
	}{
		{
			name: "tenant_wide_grant",
			grants: []permissions.ActiveGrant{
				{
					Role:      testRole,
					ScopeType: "tenant",
					ScopeID:   testTenant,
				},
			},
			requestedPerm:  permissions.VerificationReview,
			expectedResult: true,
			desc:           "tenant-wide grant with matching role should return true",
		},
		{
			name: "park_scoped_grant_only",
			grants: []permissions.ActiveGrant{
				{
					Role:      testRole,
					ScopeType: "park",
					ScopeID:   "park-001",
				},
			},
			requestedPerm:  permissions.VerificationReview,
			expectedResult: false,
			desc:           "park-scoped grant should NOT satisfy tenant-wide check",
		},
		{
			name:   "no_grants",
			grants: []permissions.ActiveGrant{
				// No grants
			},
			requestedPerm:  permissions.VerificationReview,
			expectedResult: false,
			desc:           "no grants should return false",
		},
		{
			name: "tenant_wide_grant_different_tenant",
			grants: []permissions.ActiveGrant{
				{
					Role:      testRole,
					ScopeType: "tenant",
					ScopeID:   "different-tenant", // Different tenant
				},
			},
			requestedPerm:  permissions.VerificationReview,
			expectedResult: false,
			desc:           "tenant-wide grant for different tenant should return false",
		},
		{
			name: "mixed_grant_tenant_wide_and_park",
			grants: []permissions.ActiveGrant{
				// Tenant-wide grant
				{
					Role:      testRole,
					ScopeType: "tenant",
					ScopeID:   testTenant,
				},
				// Park-scoped grant (redundant but present)
				{
					Role:      testRole,
					ScopeType: "park",
					ScopeID:   "park-001",
				},
			},
			requestedPerm:  permissions.VerificationReview,
			expectedResult: true,
			desc:           "mixed grants with tenant-wide should return true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call hasTenantWidePermission with the test grants
			// Note: This assumes ScopeIDsForPermission correctly filters by role/permission
			result := hasTenantWidePermission(tt.grants, testTenant, tt.requestedPerm)
			if result != tt.expectedResult {
				t.Errorf("hasTenantWidePermission returned %v, expected %v. Test: %s", result, tt.expectedResult, tt.desc)
			}
		})
	}
}

// R50-019 edge case: empty ScopeID should not match
func TestHasTenantWidePermissionEmptyScopeID(t *testing.T) {
	const testTenant = "tenant-001"

	grants := []permissions.ActiveGrant{
		{
			Role:      "verifier",
			ScopeType: "tenant",
			ScopeID:   "", // Empty scope ID
		},
	}

	result := hasTenantWidePermission(grants, testTenant, permissions.VerificationReview)
	if result {
		t.Errorf("empty ScopeID should not match tenant: got %v, expected false", result)
	}
}

// Verify that hasTenantWidePermission uses the correct scope filtering
func TestHasTenantWidePermissionUsesCorrectScopeFilter(t *testing.T) {
	const testTenant = "tenant-001"

	grants := []permissions.ActiveGrant{
		// Park grant for the tenant (should not be returned by ScopeIDsForPermission with scopeType="tenant")
		{
			Role:      "verifier",
			ScopeType: "park",
			ScopeID:   testTenant, // Reusing tenant ID as park ID (edge case)
		},
	}

	// Even though ScopeID matches the tenant, the ScopeType="park" means it's not a tenant-wide grant
	result := hasTenantWidePermission(grants, testTenant, permissions.VerificationReview)
	if result {
		t.Errorf("park-scoped grant should not match tenant-wide check: got %v, expected false", result)
	}
}

// R50-019 EXPLOIT TEST: Mixed-grant permission escape. Actor with a tenant-wide grant for a
// DIFFERENT permission plus a park-only grant for the REQUESTED permission must NOT escape to
// tenant scope. The hasTenantWidePermission check filters by the requested permission; it must
// not accept a tenant-wide grant for any other permission.
func TestHasTenantWidePermissionMixedGrantExploitBlocked(t *testing.T) {
	const testTenant = "tenant-001"
	const testPark = "park-001"

	// Exploit attempt: actor has tenant-wide grant for PermissionA but only park-scoped grant for PermissionB
	grants := []permissions.ActiveGrant{
		// Tenant-wide grant for a DIFFERENT permission (operator has TaskExecute, not VerificationReview)
		{
			Role:      permissions.RoleOperator,
			ScopeType: "tenant",
			ScopeID:   testTenant,
		},
		// Park-scoped grant for the REQUESTED permission (verifier has VerificationReview)
		{
			Role:      permissions.RoleVerifier,
			ScopeType: "park",
			ScopeID:   testPark,
		},
	}

	// The exploit attempt: try to use the tenant-wide operator grant to satisfy a VerificationReview check.
	// This MUST fail because ScopeIDsForPermission filters by the requested permission,
	// and the operator role does NOT have VerificationReview.
	result := hasTenantWidePermission(grants, testTenant, permissions.VerificationReview)
	if result {
		t.Errorf("mixed-grant exploit: tenant-wide grant for different permission (operator/TaskExecute) must NOT satisfy VerificationReview check; got %v, expected false", result)
	}

	// Verify that a genuine tenant-wide grant for the SAME permission DOES work
	grants2 := []permissions.ActiveGrant{
		{
			Role:      permissions.RoleVerifier,
			ScopeType: "tenant",
			ScopeID:   testTenant,
		},
	}
	result2 := hasTenantWidePermission(grants2, testTenant, permissions.VerificationReview)
	if !result2 {
		t.Errorf("legitimate tenant-wide grant must satisfy check: got %v, expected true", result2)
	}
}

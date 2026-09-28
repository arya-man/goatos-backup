package permissions

import "testing"

// Maintainer decision 2026-09-28: the FEED VERIFICATION panel (plan beside the verifier's reading)
// is for the CXO and the verifier ONLY -- no director, park head or operator. Every other role must
// fail this, so a later widening has to edit this list on purpose.
func TestFeedPackingLogIsVerifierAndCXOOnly(t *testing.T) {
	allowed := map[string]bool{RoleVerifier: true, RoleCEOInternal: true}
	for role, perms := range rolePermissions {
		_, holds := perms[VerificationFeedPackingLog]
		if holds != allowed[role] {
			t.Fatalf("role %q holds %s = %v; only the verifier and ceo_internal may", role, VerificationFeedPackingLog, holds)
		}
	}
	for role := range allowed {
		if _, ok := rolePermissions[role][VerificationFeedPackingLog]; !ok {
			t.Fatalf("role %q is missing %s", role, VerificationFeedPackingLog)
		}
	}
}

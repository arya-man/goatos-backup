package postgres

import (
	"context"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// The in-query park filter is defence in depth BEHIND the HTTP gate, and defence in depth
// only defends if it answers the same question. It used to answer a different one: this
// adapter's tenant-wide test accepted VaccinationRead while the handler's accepted
// VaccinationCampaign, and the two park sets differed too. Disagreement here is invisible --
// an actor the gate allows gets zero rows instead of an error, which reads as "there is no
// work" rather than "you were denied". A tenant-wide Park Head (Oversee, no Campaign) hit
// exactly that.
//
// Both sides now resolve through domain.HasTenantWideAuthority / domain.AuthorizedParks, so
// this asserts the filter against that one definition for the role shapes that used to
// disagree.
func TestAuthorizedParkFilterAgreesWithTheSharedAuthorityDefinition(t *testing.T) {
	const tenantID = "40000000-0000-4000-8000-0000000000a1"
	const parkA = "40000000-0000-4000-8000-0000000000a2"
	const parkB = "40000000-0000-4000-8000-0000000000a3"

	tests := []struct {
		name   string
		grants []permissions.ActiveGrant
		want   []string
	}{
		{
			name:   "tenant-wide oversight role (Oversee, no Campaign) is unrestricted",
			grants: []permissions.ActiveGrant{{Role: permissions.RoleParkHead, ScopeType: "tenant", ScopeID: tenantID}},
			want:   nil,
		},
		{
			name:   "park-scoped verifier reads its own park",
			grants: []permissions.ActiveGrant{{Role: permissions.RoleVerifier, ScopeType: "park", ScopeID: parkA}},
			want:   []string{parkA},
		},
		{
			name: "park-scoped vaccination manager reads its own park",
			grants: []permissions.ActiveGrant{
				{Role: permissions.RoleKey(permissions.TierManager, permissions.VerticalPreventiveCare), ScopeType: "park", ScopeID: parkB},
			},
			want: []string{parkB},
		},
		{
			name: "unrelated weighing grant does not lend its park",
			grants: []permissions.ActiveGrant{
				{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: parkA},
				{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkB},
			},
			want: []string{parkB},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := httpmiddleware.WithAuthGrants(context.Background(), tt.grants)
			got := authorizedParkFilter(ctx, tenantID)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("authorizedParkFilter() = %#v, want %#v", got, tt.want)
			}

			// And the same answer computed straight from the shared definition: if these
			// two ever diverge the gate and the filter have drifted apart again.
			var viaDomain []string
			if !domain.HasTenantWideAuthority(tt.grants, tenantID) {
				viaDomain = domain.AuthorizedParks(tt.grants)
			}
			if !reflect.DeepEqual(got, viaDomain) {
				t.Fatalf("filter = %#v but the shared authority definition says %#v", got, viaDomain)
			}
		})
	}
}

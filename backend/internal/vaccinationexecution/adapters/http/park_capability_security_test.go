package http

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// TestAuthorizedParkFilterVaccinationExecutionDeniesUnrelatedParkGrant proves fix #4: a
// grant scoped to Park A carrying a role with NO vaccination-execution-relevant capability
// (TaskExecute / VaccinationOverseeExecution) must NOT appear in the authorized park set,
// even though the actor holds a SEPARATE, capability-carrying grant scoped to Park B. The old
// AuthorizedParkIDs-based filter collected both parks regardless of role, letting an
// unrelated grant in Park A ride along with the capability-carrying grant in Park B.
func TestAuthorizedParkFilterVaccinationExecutionDeniesUnrelatedParkGrant(t *testing.T) {
	const parkA = "40000000-0000-4000-8000-00000000000a"
	const parkB = "40000000-0000-4000-8000-00000000000b"
	const tenantID = "40000000-0000-4000-8000-000000000001"

	tests := []struct {
		name   string
		grants []permissions.ActiveGrant
		want   []string
	}{
		{
			name: "unrelated weighing-only role in park A is excluded; oversight role in park B is included",
			grants: []permissions.ActiveGrant{
				{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: parkA},
				{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: parkB},
			},
			want: []string{parkB},
		},
		{
			name: "unrelated weighing-only role in park A is excluded; operator execution role in park B is included",
			grants: []permissions.ActiveGrant{
				{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: parkA},
				{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkB},
			},
			want: []string{parkB},
		},
		{
			name: "actor with only the unrelated grant is authorized nowhere (fail closed, not unrestricted)",
			grants: []permissions.ActiveGrant{
				{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: parkA},
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := httpmiddleware.WithAuthGrants(context.Background(), tt.grants)
			got := authorizedParkFilterVaccinationExecution(ctx, tenantID)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("authorizedParkFilterVaccinationExecution() = %#v, want %#v (PRIVILEGE ESCALATION if the unrelated park leaked in)", got, tt.want)
			}
		})
	}
}

// TestAuthorizedParkFilterVaccinationExecutionUnrelatedTenantWideGrantIsNotUnrestricted
// proves the tenant-wide half of the same defect class: a tenant-wide grant whose role has
// NO vaccination authority (growth_director, weighing-only) must not be treated as
// unrestricted access -- only the park-scoped, capability-carrying grant should be
// authorized.
func TestAuthorizedParkFilterVaccinationExecutionUnrelatedTenantWideGrantIsNotUnrestricted(t *testing.T) {
	const parkA = "40000000-0000-4000-8000-00000000001a"
	const tenantID = "40000000-0000-4000-8000-000000000002"

	grants := []permissions.ActiveGrant{
		{Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: tenantID},
		{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkA},
	}
	ctx := httpmiddleware.WithAuthGrants(context.Background(), grants)
	got := authorizedParkFilterVaccinationExecution(ctx, tenantID)
	want := []string{parkA}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("authorizedParkFilterVaccinationExecution() = %#v, want %#v (BUG: unrelated tenant-wide grant treated as unrestricted -> nil)", got, want)
	}
}

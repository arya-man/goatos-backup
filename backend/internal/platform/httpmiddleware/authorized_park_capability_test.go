package httpmiddleware

import (
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestAuthorizedParkIDsForCapabilityBindsGrantRoleToItsOwnScope proves the P0
// privilege-escalation fix: AuthorizedParkIDs (the old, capability-blind helper) collects
// the scope of EVERY park grant regardless of what that grant's role can do, so an actor
// with an unrelated grant in Park A plus a capability-carrying grant in Park B was treated
// as authorized for that capability in BOTH parks. AuthorizedParkIDsForCapability must keep
// each grant's role bound to its own scope: only the park where the CAPABILITY-CARRYING
// grant lives may be returned.
func TestAuthorizedParkIDsForCapabilityBindsGrantRoleToItsOwnScope(t *testing.T) {
	const parkA = "20000000-0000-4000-8000-00000000000a"
	const parkB = "20000000-0000-4000-8000-00000000000b"

	// RoleParkHead carries VaccinationOverseeExecution; RoleOperator does not carry it
	// (RoleOperator carries TaskExecute/WeighingExecute instead -- see permissions.go).
	grants := []permissions.ActiveGrant{
		{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkA},
		{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: parkB},
	}

	tests := []struct {
		name       string
		capability string
		want       []string
	}{
		{
			name:       "capability-carrying grant's own park is authorized",
			capability: permissions.VaccinationOverseeExecution,
			want:       []string{parkB},
		},
		{
			name:       "unrelated grant's park is NOT authorized for a capability it does not carry",
			capability: permissions.VaccinationOverseeExecution,
			want:       []string{parkB}, // parkA must NOT appear even though the actor has SOME grant there
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AuthorizedParkIDsForCapability(grants, tt.capability)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("AuthorizedParkIDsForCapability() = %#v, want %#v (BUG: capability-blind park collection leaked parkA)", got, tt.want)
			}
			for _, id := range got {
				if id == parkA {
					t.Fatalf("PRIVILEGE ESCALATION: parkA leaked into capability-scoped result %#v", got)
				}
			}
		})
	}

	// Sanity: TaskExecute (which RoleOperator DOES carry) correctly resolves to parkA, and
	// still excludes parkB (RoleParkHead does not carry TaskExecute).
	got := AuthorizedParkIDsForCapability(grants, permissions.TaskExecute)
	want := []string{parkA}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AuthorizedParkIDsForCapability(TaskExecute) = %#v, want %#v", got, want)
	}

	// Contrast: the OLD capability-blind AuthorizedParkIDs returns BOTH parks for ANY
	// capability question, which is exactly the bug this fix closes.
	blind := AuthorizedParkIDs(grants)
	wantBlind := []string{parkA, parkB}
	if !reflect.DeepEqual(blind, wantBlind) {
		t.Fatalf("AuthorizedParkIDs() = %#v, want %#v", blind, wantBlind)
	}
}

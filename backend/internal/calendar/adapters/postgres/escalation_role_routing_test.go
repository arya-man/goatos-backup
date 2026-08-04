package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// A granted CalendarAction that cannot action anything is the declared-but-inert class this
// change exists to close. actorCanActionEscalation refuses any role escalationRoleRank does not
// know, so an unranked director holds the permission and is still refused on every escalation.
func TestEveryDirectorRoleIsRankedForEscalationAction(t *testing.T) {
	const tenantID = "11111111-1111-4111-8111-111111111111"
	directors := []string{
		permissions.RolePCDirector,
		permissions.RoleGrowthDirector,
		permissions.RoleFeedDirector,
		permissions.RoleHealthDirector,
	}
	for _, role := range directors {
		if !permissions.RoleHasPermission(role, permissions.CalendarAction) {
			t.Fatalf("%s does not hold calendar.action; this test's premise is stale", role)
		}
		rank, ok := escalationRoleRank(role)
		if !ok {
			t.Errorf("%s has no escalation rank; its granted calendar.action is inert", role)
			continue
		}
		if rank <= 20 {
			t.Errorf("%s ranks %d, below park head; a director must outrank the park level", role, rank)
		}
		// End to end through the actual gate, against a park-level escalation.
		grants := []ports.ActorGrant{{Role: role, ScopeType: "tenant", ScopeID: tenantID}}
		if !actorCanActionEscalation(grants, tenantID, actionTarget{ParkID: "park-1"}, permissions.RoleParkHead) {
			t.Errorf("%s cannot action a park-head escalation despite holding calendar.action", role)
		}
	}
}

// L3 must reach the director who OWNS the module, never a blanket pc_director. Vaccination is
// pinned so today's live routing cannot regress.
func TestEscalationDirectorRoleFollowsTheModule(t *testing.T) {
	cases := map[string]string{
		"vaccination_dose_due":              permissions.RolePCDirector,
		"vaccination_proof_verification":    permissions.RolePCDirector,
		"vaccination_config_activation_rev": permissions.RolePCDirector,
		"weighing_session_due":              permissions.RoleGrowthDirector,
		"feed_direction_due":                permissions.RoleFeedDirector,
		"counts_shifting_due":               permissions.RoleHealthDirector,
	}
	for eventType, want := range cases {
		if got := escalationRole(3, escalationTarget{EventType: eventType}); got != want {
			t.Errorf("escalationRole(3, %s) = %s, want %s", eventType, got, want)
		}
	}
	// An unowned module must NOT quietly land on the PC Director.
	if got := escalationRole(3, escalationTarget{EventType: "breeding_check_due"}); got == permissions.RolePCDirector {
		t.Error("an unowned module escalated to the PC Director; unowned must over-escalate instead")
	}
	if got := escalationRole(4, escalationTarget{EventType: "vaccination_dose_due"}); got != permissions.RoleCEOInternal {
		t.Errorf("L4 = %s, want ceo_internal", got)
	}
}

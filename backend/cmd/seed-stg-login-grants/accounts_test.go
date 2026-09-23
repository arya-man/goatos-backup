package main

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

func TestParkStaffAccountsDeclareParkScope(t *testing.T) {
	for _, acct := range stgLoginAccounts {
		if acct.Role != permissions.RoleOperator {
			continue
		}
		if strings.TrimSpace(acct.ParkCode) == "" {
			t.Fatalf("%s <%s> role=%s must declare ParkCode; operators must not seed tenant scope", acct.DisplayName, acct.Email, acct.Role)
		}
	}
}

func TestLeadershipAndDirectorAccountsRemainTenantScoped(t *testing.T) {
	for _, acct := range stgLoginAccounts {
		if acct.Role != permissions.RoleCEOInternal && acct.Role != permissions.RolePCDirector {
			continue
		}
		if strings.TrimSpace(acct.ParkCode) != "" {
			t.Fatalf("%s <%s> role=%s must not be narrowed to ParkCode=%q", acct.DisplayName, acct.Email, acct.Role, acct.ParkCode)
		}
	}
}

func TestAuthProfileGroundTiersKeepOperatorHintUntilAPKShips(t *testing.T) {
	for _, role := range []string{
		permissions.RoleKey(permissions.TierManager, permissions.VerticalFeed),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalHealth),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalCleaning),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalFarming),
	} {
		hint, grade := authProfileRoleHintAndGrade(role)
		if hint != "operator" || grade != "manager" {
			t.Fatalf("authProfileRoleHintAndGrade(%q)=%q/%v, want operator/manager until installed APK feed gates move to capabilities", role, hint, grade)
		}
	}
	for _, role := range []string{
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalFeed),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalHealth),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalCleaning),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalFarming),
	} {
		hint, grade := authProfileRoleHintAndGrade(role)
		if hint != "operator" || grade != "assistant_manager" {
			t.Fatalf("authProfileRoleHintAndGrade(%q)=%q/%v, want operator/assistant_manager until installed APK feed gates move to capabilities", role, hint, grade)
		}
	}
}

// Migration 000394 narrows a pending ground-manager invite onto the person's
// park, and REFUSES when one person resolves to more than one park for the same
// role -- a pending invite carries one scope, so guessing would silently drop a
// park. The seed roster must never author that shape in the first place: one
// Account carries one ParkCode, so the only way to produce it is to list the
// same person under the same role twice with different parks.
func TestSeedRosterNeverGivesOnePersonTwoParksForTheSameRole(t *testing.T) {
	type personRole struct{ person, role string }
	parks := map[personRole]string{}
	for _, acct := range stgLoginAccounts {
		person := strings.ToLower(strings.TrimSpace(acct.RosterDisplayNameMatch))
		if person == "" {
			person = strings.ToLower(strings.TrimSpace(acct.Email))
		}
		park := strings.TrimSpace(acct.ParkCode)
		if park == "" {
			continue
		}
		key := personRole{person: person, role: acct.Role}
		if seen, ok := parks[key]; ok && seen != park {
			t.Fatalf("%s holds role=%s in two parks (%s and %s); migration 000394 refuses a pending invite that resolves to more than one park, so the seed roster must not author one",
				acct.DisplayName, acct.Role, seen, park)
		}
		parks[key] = park
	}
}

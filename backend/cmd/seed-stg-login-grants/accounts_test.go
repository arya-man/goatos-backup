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

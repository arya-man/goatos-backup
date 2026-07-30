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

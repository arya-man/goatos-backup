package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// The auto-created auth profile is named after the PERSON (email local-part),
// never after the role: the literal "CEO/CXO" placeholder produced N identical
// unidentifiable rows in the People/HRMS directory (2026-08-22).
func TestPendingEmailGrantDisplayNameIsThePersonNotTheRole(t *testing.T) {
	cases := map[string]string{
		"ravi@mesha.sg":      "Ravi",
		"manohar.k@mesha.sg": "Manohar K",
		"jyothi_pvg@x.com":   "Jyothi Pvg",
	}
	for email, want := range cases {
		if got := pendingEmailGrantDisplayName(email, "ceo_internal"); got != want {
			t.Fatalf("pendingEmailGrantDisplayName(%q) = %q, want %q", email, got, want)
		}
	}
	if got := pendingEmailGrantDisplayName("", "ceo_internal"); got != "CEO/CXO" {
		t.Fatalf("empty email fallback = %q, want CEO/CXO", got)
	}
	if got := pendingEmailGrantDisplayName("", "operator"); got != "Granted user" {
		t.Fatalf("empty email non-leadership fallback = %q", got)
	}
}

func TestPendingEmailGrantGroundTiersKeepOperatorHintUntilAPKShips(t *testing.T) {
	for _, role := range []string{
		permissions.RoleKey(permissions.TierManager, permissions.VerticalFeed),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalHealth),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalCleaning),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalFarming),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalFeed),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalHealth),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalCleaning),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalFarming),
	} {
		if got := pendingEmailGrantRoleHint(role); got != "operator" {
			t.Fatalf("pendingEmailGrantRoleHint(%q)=%q, want operator until installed APK feed gates move to capabilities", role, got)
		}
	}
}

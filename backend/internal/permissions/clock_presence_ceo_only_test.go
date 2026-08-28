package permissions

import "testing"

// TestClockPresenceReadIsCEOOnly pins the maintainer's 2026-08-28 ask: the
// clock module's Team presence board (and the admin-web People clock tab
// behind the same permission) belongs to the CEO/CXO ALONE among ROLES. Any
// other person who needs it gets it as a per-person tick on their own row,
// never by widening a job role here — the toxin-verdict shape, one module over.
func TestClockPresenceReadIsCEOOnly(t *testing.T) {
	for role, perms := range rolePermissions {
		_, holds := perms[ClockPresenceRead]
		if role == RoleCEOInternal {
			if !holds {
				t.Fatalf("ceo_internal must hold %s (founder/CXO presence oversight)", ClockPresenceRead)
			}
			continue
		}
		if holds {
			t.Fatalf("role %q holds %s; the Team presence board is CEO/CXO-only — grant individuals per-person ticks instead of widening a job role", role, ClockPresenceRead)
		}
	}
}

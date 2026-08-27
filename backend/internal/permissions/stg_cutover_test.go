package permissions

import (
	"bufio"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestSTGCutoverLosesNoPermission answers the deployment question directly, against the REAL
// roster rather than synthetic roles: after the backfill, does anybody lose an action they
// can take today?
//
// capability_parity_test.go proves it per ROLE. This proves it per PERSON, which is a
// different question: STG carries people wearing up to five roles at once, and the union of
// five mappings is not something a per-role test can check.
//
// The roster is a read-only export (display_name|roles|department_modules per line); the test
// skips when it is absent, so it never blocks CI. Regenerate before a deploy:
//
//	psql "$STG" -tAc "SELECT wm.display_name, ... " > roster.txt
//	GOATOS_ROSTER_EXPORT=roster.txt go test ./internal/permissions/ -run TestSTGCutover
func TestSTGCutoverLosesNoPermission(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("GOATOS_ROSTER_EXPORT"))
	if path == "" {
		t.Skip("set GOATOS_ROSTER_EXPORT to a roster export to run the cutover simulation")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("roster export: %v", err)
	}
	defer f.Close()

	people, losses := 0, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Split(strings.TrimSpace(sc.Text()), "|")
		if len(parts) < 3 {
			continue
		}
		name, roleStr, deptStr := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if roleStr == "(none)" {
			continue
		}
		people++
		roles := strings.Split(roleStr, "+")
		_ = deptStr

		before := map[string]struct{}{}
		for _, role := range roles {
			for perm := range rolePermissions[role] {
				before[perm] = struct{}{}
			}
		}
		assignments := FillDefaultPages(
			NarrowForRetiredLenses(roles, AssignmentsForRoles(roles)))
		after := map[string]struct{}{}
		for _, perm := range PermissionsForAssignmentsWithBaseline(assignments) {
			after[perm] = struct{}{}
		}

		missing := []string{}
		for perm := range before {
			if _, ok := after[perm]; !ok {
				missing = append(missing, perm)
			}
		}
		sort.Strings(missing)
		for _, perm := range missing {
			// An accepted loss is one the parity test already records for that role. Anything
			// else is a person losing an action they can take today.
			if _, accepted := acceptedPersonLosses[perm]; accepted {
				continue
			}
			t.Errorf("STG %s (%s) LOSES %q", name, roleStr, perm)
			losses++
		}
	}
	t.Logf("simulated %d people from %s: %d unaccepted permission losses", people, path, losses)
}

// acceptedPersonLosses are permissions a person may lose at cutover BY DECISION, each one
// recorded rather than discovered.
//
// All three belong to ONE person, the Procurement Director, and all three follow directly
// from the 2026-08-21 decision that he sees only Procurement and Feed on the web. Under the
// retired lens he still HELD them -- the lens hid screens, it did not remove authority -- so
// an API call would have worked even though no screen existed. Expressing that decision as
// ticks removes the modules, and the permissions go with them.
//
// They are listed here rather than silently tolerated because each is a real narrowing and
// the maintainer should decide it, not discover it. Preserving them is possible but not
// free: it means giving him the Calendar and Verify MODULES, and those own sidebar pages,
// so both screens would return to the sidebar the 2026-08-21 decision cleared. The three
// reads that cost no screen (sop.read, protocol.read, goat.read) are already preserved that
// way -- Protocols & SOPs, Herd Register and Parks & Sheds own no sidebar page at all.
var acceptedPersonLosses = map[string]struct{}{
	// No Calendar screen exists for him, so nothing he can open uses these.
	"calendar.read":   {},
	"calendar.action": {},
	// No Verify screen either. Acting on a verification item is the verifier's job and the
	// module director's, and his workspace has neither surface.
	"verification.act": {},
}

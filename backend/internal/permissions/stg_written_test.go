package permissions

import (
	"bufio"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestSTGWrittenRowsLoseNoPermission verifies the rows the backfill ACTUALLY WROTE, read back
// from the live database, rather than the rows it was predicted to write.
//
// The simulation ran before the write and against a roster export. This runs after, against
// the real stored capabilities, and is the check that answers "did the cutover cost anyone an
// action" from the data now serving requests.
func TestSTGWrittenRowsLoseNoPermission(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("GOATOS_WRITTEN_EXPORT"))
	if path == "" {
		t.Skip("set GOATOS_WRITTEN_EXPORT to a written-rows export")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	defer f.Close()

	people, losses := 0, 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		parts := strings.SplitN(strings.TrimSpace(sc.Text()), "|", 3)
		if len(parts) < 3 {
			continue
		}
		name, roleStr, rowStr := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if roleStr == "(none)" || rowStr == "" {
			continue
		}
		people++
		roles := strings.Split(roleStr, "+")

		assignments := []ModuleAssignment{}
		for _, row := range strings.Split(rowStr, ";") {
			f := strings.Split(row, ":")
			if len(f) != 3 || f[2] == "" {
				continue
			}
			assignments = append(assignments, ModuleAssignment{
				Surface: f[0], Module: f[1], Capabilities: strings.Split(f[2], "+"),
			})
		}

		before := map[string]struct{}{}
		for _, role := range roles {
			for perm := range rolePermissions[role] {
				before[perm] = struct{}{}
			}
		}
		after := map[string]struct{}{}
		for _, perm := range PermissionsForAssignmentsWithBaseline(assignments) {
			after[perm] = struct{}{}
		}

		missing := []string{}
		for perm := range before {
			if _, ok := after[perm]; !ok {
				if _, accepted := acceptedPersonLosses[perm]; accepted {
					continue
				}
				missing = append(missing, perm)
			}
		}
		sort.Strings(missing)
		for _, perm := range missing {
			t.Errorf("STG %s (%s) LOST %q in the rows that were actually written", name, roleStr, perm)
			losses++
		}
	}
	t.Logf("verified %d people from the LIVE rows: %d unaccepted losses", people, losses)
}

package app

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEveryGrantableRoleHintIsAcceptedByTheColumnCheck closes PR #181 review finding
// PC-181-001 and keeps it closed: the Add Person form stamps grantablePersonRoles[role].RoleHint
// onto workforce_members.primary_role_hint, and the LATEST migration that redefines
// workforce_members_role_hint_check is the list the database will actually accept. A role added
// to the form without extending that CHECK passes app validation and fails at the INSERT --
// which is exactly how breeding_director shipped in the first cut. This test parses the
// migrations so the two lists cannot drift again without a red build.
func TestEveryGrantableRoleHintIsAcceptedByTheColumnCheck(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "migrations", "postgres")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	// The last migration (by number) whose UP section re-adds the constraint wins.
	addRe := regexp.MustCompile(`(?s)ADD CONSTRAINT workforce_members_role_hint_check\s+CHECK\s*\(\s*primary_role_hint = ANY \(ARRAY\[(.*?)\]\)`)
	var accepted map[string]struct{}
	var source string
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		up := string(raw)
		if i := strings.Index(up, "-- +goose Down"); i >= 0 {
			up = up[:i]
		}
		m := addRe.FindStringSubmatch(up)
		if m == nil {
			continue
		}
		accepted = map[string]struct{}{}
		for _, q := range regexp.MustCompile(`'([a-z_]+)'::text`).FindAllStringSubmatch(m[1], -1) {
			accepted[q[1]] = struct{}{}
		}
		source = name
	}
	if accepted == nil {
		t.Fatal("no migration re-adds workforce_members_role_hint_check; the parser or the schema moved")
	}
	for role, spec := range grantablePersonRoles {
		if _, ok := accepted[spec.RoleHint]; !ok {
			t.Errorf("role %s stamps primary_role_hint=%q, which %s's workforce_members_role_hint_check does not accept -- Add Person would validate and then fail at the INSERT", role, spec.RoleHint, source)
		}
	}
}

package app

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/parkscope"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
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
		// The legacy operator create/update path pre-flights the same hint through
		// validRoleHint (PC-181-003): a hint the form and the DB accept must not 400 there.
		if !validRoleHint(spec.RoleHint) {
			t.Errorf("role %s stamps primary_role_hint=%q, which validRoleHint (the CreateOperator/UpdateOperator pre-flight) rejects", role, spec.RoleHint)
		}
	}
	// And validRoleHint must be EXACTLY the DB list, both ways: a hint it accepts that the
	// CHECK refuses turns a 400 into a check_violation; one it refuses that the CHECK allows
	// is this finding again.
	for hint := range accepted {
		if !validRoleHint(hint) {
			t.Errorf("validRoleHint rejects %q, which %s's workforce_members_role_hint_check accepts", hint, source)
		}
	}
	for _, hint := range []string{"operator", "park_head", "pc_director", "growth_director", "feed_director", "health_director", "breeding_director", "verifier", "supervisor", "cxo", "other", "procurement_director", "counts_approver", "toxin_tester", ""} {
		if _, inDB := accepted[hint]; validRoleHint(hint) != inDB {
			t.Errorf("validRoleHint(%q)=%v but the DB CHECK says %v", hint, validRoleHint(hint), inDB)
		}
	}
}

// TestLegacyOperatorPathsAcceptTheBreedingDirectorHint drives the legacy service paths the
// review named (PC-181-003): CreateOperator's body validation and UpdateOperator's hint
// pre-flight must not answer invalid_primary_role_hint for a hint the column accepts.
func TestLegacyOperatorPathsAcceptTheBreedingDirectorHint(t *testing.T) {
	if err := validateCreateOperator(domain.CreateOperatorRequest{
		DisplayCode:     "BD-1",
		DisplayName:     "Breeding Director",
		Status:          "active",
		PrimaryRoleHint: "breeding_director",
	}); err != nil {
		t.Fatalf("CreateOperator body with primary_role_hint=breeding_director: %v", err)
	}
	if err := validateCreateOperator(domain.CreateOperatorRequest{
		DisplayCode:     "X-1",
		DisplayName:     "Nobody",
		Status:          "active",
		PrimaryRoleHint: "counts_approver",
	}); err == nil {
		t.Fatal("a per-person authority is never anyone's primary_role_hint; the pre-flight must still refuse it")
	}
	// UpdateOperator runs the same validRoleHint on a present hint (service.go, UpdateOperator).
	if !validRoleHint("breeding_director") {
		t.Fatal("UpdateOperator's hint pre-flight must accept breeding_director")
	}
}

// TestEveryGrantableRoleIsAcceptedByTheGrantPreflight: CreateGrant pre-flights the role
// through validRole. A role the Add Person form can grant, or that the park-scope
// derivation knows how to preserve tenant-wide, must also be grantable to an existing
// person, or the write paths disagree about which roles exist.
func TestEveryGrantableRoleIsAcceptedByTheGrantPreflight(t *testing.T) {
	for role := range grantablePersonRoles {
		if !validRole(role) {
			t.Errorf("role %s is grantable from Add Person but CreateGrant's validRole refuses it", role)
		}
	}
	for role := range parkscope.TenantOnlyRoles {
		if !validRole(role) {
			t.Errorf("role %s is tenant-only in park-scope derivation but CreateGrant's validRole refuses it", role)
		}
	}
}

package permissions

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// groundTierSeedMigration is the migration that introduces the ground-tier manager roles
// (maintainer decision 2026-09-23). These tests parse it rather than restating its contents,
// so the seeded rows and the Go vocabulary cannot drift apart without a red build -- the same
// shape workforce/app.TestEveryGrantableRoleHintIsAcceptedByTheColumnCheck uses for the
// primary_role_hint CHECK.
const groundTierSeedMigration = "000393_ground_tier_roles_cleaning_and_farming.sql"

func readGroundTierSeedUpSection(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "postgres", groundTierSeedMigration))
	if err != nil {
		t.Fatalf("read %s: %v", groundTierSeedMigration, err)
	}
	body := string(raw)
	if down := strings.Index(body, "-- +goose Down"); down >= 0 {
		body = body[:down]
	}
	return body
}

// TestSeededDesignationsResolveToAssignments is the one that matters operationally.
//
// backend/cmd/backfill-person-access HARD-FAILS on any active designation_catalog row with no
// Go mapping ("designation %q has no assignment mapping; either map it in capability_backfill.go
// or retire the catalog row"). So a designation seeded here that AssignmentsForRole cannot
// resolve does not fail at deploy -- it fails later, when somebody runs the backfill, with the
// new people already created. Every code the migration seeds must resolve now.
func TestSeededDesignationsResolveToAssignments(t *testing.T) {
	up := readGroundTierSeedUpSection(t)

	block := regexp.MustCompile(`(?s)INSERT INTO public\.designation_catalog.*?;`).FindString(up)
	if block == "" {
		t.Fatalf("%s seeds no designation_catalog rows; this test is guarding nothing", groundTierSeedMigration)
	}
	codes := regexp.MustCompile(`\(\s*'([a-z0-9_]+)'\s*,`).FindAllStringSubmatch(block, -1)
	if len(codes) == 0 {
		t.Fatalf("parsed no designation codes out of %s", groundTierSeedMigration)
	}

	for _, m := range codes {
		code := m[1]
		if _, ok := AssignmentsForRole(code); !ok {
			t.Errorf("designation %q is seeded active but AssignmentsForRole cannot resolve it; backfill-person-access will hard-fail on it", code)
		}
	}
}

// TestSeededRoleKeysAreGrantable proves the other half: a role_key row exists in
// org_role_catalog precisely so a grant INSERT can reference it (user_scope_grants_role_fk and
// auth_pending_email_grants_role_fk are both FKs to it). A key seeded into the catalog that the
// Go side does not recognise is a row nothing can ever use.
func TestSeededRoleKeysAreGrantable(t *testing.T) {
	up := readGroundTierSeedUpSection(t)

	block := regexp.MustCompile(`(?s)INSERT INTO public\.org_role_catalog.*?;`).FindString(up)
	if block == "" {
		t.Fatalf("%s seeds no org_role_catalog rows; this test is guarding nothing", groundTierSeedMigration)
	}
	keys := regexp.MustCompile(`\(\s*'([a-z0-9_]+)'\s*,\s*'(?:director|head|manager|am)'`).FindAllStringSubmatch(block, -1)
	if len(keys) == 0 {
		t.Fatalf("parsed no role keys out of %s", groundTierSeedMigration)
	}

	for _, m := range keys {
		key := m[1]
		if !IsOrgRoleKey(key) {
			t.Errorf("org_role_catalog seeds %q, which ParseRoleKey does not recognise as a tier x vertical key", key)
		}
		if !IsKnownRole(key) {
			t.Errorf("org_role_catalog seeds %q, which IsKnownRole rejects; a grant naming it would pass the FK and fail authorization", key)
		}
	}
}

// TestSeededVerticalsMatchTheGoVocabulary keeps org_verticals and AllVerticals in step. The FK
// org_role_catalog_vertical_code_fkey means a vertical named in Go but absent from the table
// makes every role key built on it ungrantable, and the reverse leaves orphan catalog rows.
func TestSeededVerticalsMatchTheGoVocabulary(t *testing.T) {
	up := readGroundTierSeedUpSection(t)

	block := regexp.MustCompile(`(?s)INSERT INTO public\.org_verticals.*?;`).FindString(up)
	if block == "" {
		t.Fatalf("%s seeds no org_verticals rows; this test is guarding nothing", groundTierSeedMigration)
	}
	seeded := regexp.MustCompile(`\(\s*'([a-z0-9_]+)'\s*,`).FindAllStringSubmatch(block, -1)
	if len(seeded) == 0 {
		t.Fatalf("parsed no verticals out of %s", groundTierSeedMigration)
	}

	known := make(map[string]struct{}, len(AllVerticals))
	for _, v := range AllVerticals {
		known[string(v)] = struct{}{}
	}
	for _, m := range seeded {
		if _, ok := known[m[1]]; !ok {
			t.Errorf("org_verticals seeds %q but AllVerticals does not carry it; role keys on that vertical cannot be composed", m[1])
		}
	}
}

// TestCleaningAndFarmingCarryNoModuleOfTheirOwn pins the decision that makes these two
// verticals cheap and safe: their people get Clock In / Out, which is baseline for every
// principal and needs no tick, and nothing else by default. If someone later gives them a
// module in verticalModule, that is a real product decision and should not arrive silently
// through a refactor.
func TestCleaningAndFarmingCarryNoModuleOfTheirOwn(t *testing.T) {
	for _, vertical := range []Vertical{VerticalCleaning, VerticalFarming} {
		for _, tier := range AllTiers {
			if got := verticalModule(tier, vertical); len(got) != 0 {
				t.Errorf("verticalModule(%s, %s) returned %d module row(s), want none: attendance is baseline and anything more is a per-person HRMS tick", tier, vertical, len(got))
			}
		}
	}
}

// TestClockIsTickableWithoutGrantingThePresenceBoard is the reason the clock module gained a
// View level. Opening the phone for somebody requires at least one module row on the mobile
// surface (PermissionsForAssignments derives the surface bootstrap from exactly that), and
// before View existed the only assignable clock level was Oversee -- so giving a cleaning
// manager attendance also gave them the cross-person Team presence board.
func TestClockIsTickableWithoutGrantingThePresenceBoard(t *testing.T) {
	row := ModuleAssignment{Module: "clock", Surface: SurfaceMobile, Capabilities: []string{LevelView}}
	got := PermissionsForAssignmentsWithBaseline([]ModuleAssignment{row})

	held := make(map[string]struct{}, len(got))
	for _, p := range got {
		held[p] = struct{}{}
	}
	if _, ok := held[AppBootstrap]; !ok {
		t.Errorf("clock at view does not admit the mobile surface: %v", got)
	}
	if _, ok := held[ClockPresenceRead]; ok {
		t.Errorf("clock at view leaked %s: attendance alone must not carry the cross-person presence board", ClockPresenceRead)
	}
}

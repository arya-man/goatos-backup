package postgres

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// PEN TYPES MUST SURVIVE A RESEED (maintainer instruction 2026-09-26: "on initial deployment or
// seeding nothing should break; we already have a mapping for pen types, that mapping should be
// present"). Migration 000428 keeps every pen's type on an existing database, but a reseed creates
// pens AFTER migrations run, so the farm's classification comes back only if seed closeout applies
// fixtures/pen-types/pen-type-map.sql. These pin both halves: closeout still runs the map, and the
// map names only pen types the same file (and migration 000428) creates -- a code nobody seeds
// would violate shed_partitions_pen_type_fk and stop the whole closeout.
func TestSeedCloseoutAppliesThePenTypeMap(t *testing.T) {
	script, err := os.ReadFile("../../../tools/dev/seed-closeout.sh")
	if err != nil {
		t.Fatalf("read seed-closeout.sh: %v", err)
	}
	s := string(script)
	if !strings.Contains(s, "fixtures/pen-types/pen-type-map.sql") {
		t.Fatal("seed closeout no longer names the pen type map; a reseed would leave every pen unclassified")
	}
	call := regexp.MustCompile(`(?m)^apply_pen_type_map$`)
	if !call.MatchString(s) {
		t.Fatal("seed closeout defines apply_pen_type_map but never calls it")
	}
}

func TestPenTypeMapNamesOnlySeededPenTypes(t *testing.T) {
	raw, err := os.ReadFile("../../../fixtures/pen-types/pen-type-map.sql")
	if err != nil {
		t.Fatalf("read pen-type-map.sql: %v", err)
	}
	text := string(raw)
	seeded := map[string]bool{}
	for _, m := range regexp.MustCompile(`\('([a-z_]+)', '[^']+', \d+\)`).FindAllStringSubmatch(text, -1) {
		seeded[m[1]] = true
	}
	if !seeded["elevated"] || !seeded["non_elevated"] {
		t.Fatalf("the map must create the pen types it uses; seeded = %v", seeded)
	}
	rows := regexp.MustCompile(`\('([A-Z]+)', '([^']+)', '([^']+)', '([a-z_]+)'\)`).FindAllStringSubmatch(text, -1)
	if len(rows) < 100 {
		t.Fatalf("the map carries %d pens; the farm's classification is ~130 rows", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if !seeded[r[4]] {
			t.Fatalf("pen %s %s part %s uses pen type %q, which the map does not create", r[1], r[2], r[3], r[4])
		}
		key := r[1] + "|" + r[2] + "|" + r[3]
		if seen[key] {
			t.Fatalf("pen %s is listed twice", key)
		}
		seen[key] = true
	}
	if !strings.Contains(text, "AND sp.shed_type IS NULL") {
		t.Fatal("the map must fill only unclassified pens; it would otherwise overwrite a type the farm set on screen")
	}
}

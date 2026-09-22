package postgres

import (
	"os"
	"strings"
	"testing"
)

// 000389 moves pen type down a grain -- off the BUILDING (shed_profiles.shed_type, written by
// 000385) and onto the PEN THE FARM WORKS (shed_partitions.shed_type). Four properties decide
// whether that move loses or invents a classification, and each is asserted against the migration
// text because the alternative is discovering it on the farm's own data after the deploy.
//
//	OneToMany   one building holds many partitions, so the carry-down is deliberately 1:N -- every
//	            pen of a classified building inherits it. The join that feeds it is keyed on
//	            shed_profiles' PRIMARY KEY, so the many side is the partitions and the one side
//	            cannot multiply them.
//	PageBoundary the UPDATE is unpaginated on purpose: a bounded per-tenant catalog, and a partial
//	            pass would leave half the farm classified and half not, with nothing to say which.
//	ParkScope   tenant_id is carried on every join and on the predicate, so one tenant's pens can
//	            never be typed from another's profile row.
//	StatusBuckets a pen that ALREADY carries a type keeps it (shed_type IS NULL), so the migration
//	            is safe to run after someone has started classifying by hand, and a building with
//	            no type writes nothing rather than writing NULL over a real answer.
func TestPenTypeMoveOneToManyAliasRowsPageBoundaryParkScopeStatusBuckets(t *testing.T) {
	raw, err := os.ReadFile("000389_pen_type_moves_to_the_partition.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(raw)
	downAt := strings.Index(sql, "-- +goose Down")
	if downAt < 0 {
		t.Fatal("migration has no Down section")
	}
	up, down := sql[:downAt], sql[downAt:]

	for _, want := range []string{
		// The carry-down, and the two spellings of one pen it has to reconcile.
		"COALESCE(alias_profile.shed_type, own_profile.shed_type)",
		"alias_profile.location_id = p.alias_location_id",
		// StatusBuckets: a hand-classified pen is never overwritten.
		"AND sp.shed_type IS NULL",
		"AND src.shed_type IS NOT NULL",
		// ParkScope: the tenant is on the predicate, not assumed.
		"WHERE sp.tenant_id = src.tenant_id",
		// The old column is retired in the SAME migration: two columns holding one farm fact is
		// the disagreement this move exists to prevent.
		"DROP COLUMN IF EXISTS shed_type",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("the Up migration no longer contains %q", want)
		}
	}
	if !strings.Contains(up, "projection-review: membership=") {
		t.Fatal("the carry-down has no projection-review marker")
	}

	// The Down path is LOSSY and must stay honest about it: a building whose pens disagree gets no
	// answer rather than one of theirs. Losing the HAVING would silently pick min() -- alphabetical
	// order deciding how a farm's pens are classified.
	for _, want := range []string{
		"HAVING count(DISTINCT shed_type) = 1",
		"GROUP BY tenant_id, shed_id",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("the Down migration no longer contains %q", want)
		}
	}
	if strings.Contains(up, "gandhi") || strings.Contains(up, "mandela") || strings.Contains(up, "elevate|") {
		t.Fatal("the migration re-derives a pen type from names; it must CARRY the configured value, never recompute it")
	}
}

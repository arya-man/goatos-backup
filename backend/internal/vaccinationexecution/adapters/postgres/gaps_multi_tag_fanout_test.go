package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// THE SAME FAN-OUT, IN THE SIBLING QUERY (de4e099b6 fixed the drawer; this is the gaps list).
//
// goat_identifiers is unique per (goat_id, identifier_type) only for the PRIMARY active row, so
// one goat may hold several ACTIVE NON-PRIMARY rows of a type -- the validation database already
// does, which is what de4e099b6 found. That commit replaced the plain joins in the Closed, No Dose
// read with scalar LATERALs. vaccinationGapsSQL was left on the plain joins, and its own header
// still says only "a goat with no active tag of a type yields NULL" -- it never considered more
// than one.
//
// Nothing caught it because the gaps fixture gives NO animal any identifier at all, so aid1 and
// aid2 are always NULL there and the multi-tag case cannot arise in it. A fixture that can only
// hold the happy case cannot fail on the unhappy one.
//
// The cost here is worse than a repeated row. This is a KEYSET-PAGINATED scan: a fanned-out animal
// spends two of the LIMIT's slots, so a caller asking for one page of gaps silently gets fewer
// DISTINCT animals than it asked for, and the operator's list of animals missing a date of birth
// is short by however many of them carry a spare tag.
func TestVaccinationGapsCountsAMultiTagAnimalOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationGaps(t, ctx, pool)
	// The no-DOB animal gains a PRIMARY and a second ACTIVE non-primary tag of the SAME type.
	execProjectionSQL(t, ctx, pool, "primary tag 1",
		`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
		   scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
		 VALUES ($1, $2, 'animal_identifier_1', 'GAP-PRIMARY', 'gap-primary', 'tenant', true, 'active', now(), 'v1')`,
		testTenant, testGapsGoatNoDOB)
	execProjectionSQL(t, ctx, pool, "second active non-primary tag 1",
		`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
		   scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
		 VALUES ($1, $2, 'animal_identifier_1', 'GAP-SECONDARY', 'gap-secondary', 'tenant', false, 'active', now(), 'v1')`,
		testTenant, testGapsGoatNoDOB)

	repo := NewRepository(pool, 5*time.Second)
	rows, err := repo.VaccinationGaps(ctx, domain.GapsQuery{TenantID: testTenant, Limit: 10})
	if err != nil {
		t.Fatalf("VaccinationGaps() error = %v", err)
	}
	occurrences := 0
	var seen domain.GapProjectionRow
	for _, row := range rows {
		if row.GoatID == testGapsGoatNoDOB {
			occurrences++
			seen = row
		}
	}
	if occurrences != 1 {
		t.Fatalf("animal with two active animal_identifier_1 rows appears %d times, want 1 -- a plain "+
			"join on goat_identifiers fans the animal out and, on this keyset-paginated scan, spends "+
			"two of the LIMIT's slots so distinct gapped animals fall off the page", occurrences)
	}
	// The PRIMARY row must win, and it must win STABLY: picking whichever row the join happened to
	// reach would make the operator's card show a different tag on successive reads.
	if seen.AnimalIdentifier1 == nil || *seen.AnimalIdentifier1 != "GAP-PRIMARY" {
		got := "<nil>"
		if seen.AnimalIdentifier1 != nil {
			got = *seen.AnimalIdentifier1
		}
		t.Fatalf("tag 1 = %q, want the PRIMARY active identifier %q -- the non-primary row must not win", got, "GAP-PRIMARY")
	}
}

// The header's own stated case, kept beside the new one so a future change cannot satisfy the test
// above by dropping the join altogether: an animal with NO active tag of a type still yields NULL
// and still appears.
func TestVaccinationGapsStillListsAnAnimalWithNoTagAtAll(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVaccinationGaps(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	rows, err := repo.VaccinationGaps(ctx, domain.GapsQuery{TenantID: testTenant, Limit: 10})
	if err != nil {
		t.Fatalf("VaccinationGaps() error = %v", err)
	}
	for _, row := range rows {
		if row.GoatID != testGapsGoatNoBreed {
			continue
		}
		if row.AnimalIdentifier1 != nil || row.AnimalIdentifier2 != nil {
			t.Fatalf("an animal with no active tag must read NULL on both slots, got %v / %v", row.AnimalIdentifier1, row.AnimalIdentifier2)
		}
		return
	}
	t.Fatal("the untagged gapped animal must still be listed")
}

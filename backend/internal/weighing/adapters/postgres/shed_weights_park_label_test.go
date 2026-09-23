package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// THE FILTER AND THE ROWS MUST CARRY THE SAME PARK LABEL (5e7a3635b).
//
// The farm calls its parks CBE and CPT, so the Weights read labels a park by its location_code
// when it has one and falls back to the full name when it does not. Both halves of the screen are
// built from that rule: the rows, and the park vocabulary the filter chips are drawn from.
//
// 5e7a3635b's own words for why the vocabulary is read in the Weights repository rather than
// through the shared ListParks helper: that helper is also the mobile planner's and returns the
// full name, so "the two would then disagree on screen, with the dropdown saying Coimbatore and
// every row saying CBE". Nothing asserted it. The test that shipped lives in the app layer and is
// served by a fake that mirrors the repository's rule in Go, so it proves the fake; the only
// Postgres test near it matches the ORDER BY as SOURCE TEXT, which pins the ordering and says
// nothing about the label; and the existing integration tests assert ParkName != "" and no more.
//
// The park here is the seeded one, code CBE and name Coimbatore, which is exactly the pair the
// rule has to choose between. It is read rather than written: a database trigger
// (location_seeded_scope_guard) reserves those three seeded locations for an approved migration
// plan, so a test that tried to set the code up for itself would be refused.
func TestShedWeightsRowsAndParkFilterBothUseTheParksShortCode(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	// Guards the fixture: if the seeded park ever loses its code, or its code and name stop
	// differing, this test would pass while asserting nothing about which of the two won.
	var code, name string
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(location_code, ''), COALESCE(name, '') FROM locations WHERE tenant_id = $1::uuid AND location_id = $2::uuid`,
		repoTenant, repoPark).Scan(&code, &name); err != nil {
		t.Fatalf("read the seeded park: %v", err)
	}
	if code == "" || name == "" || code == name {
		t.Fatalf("the seeded park must carry a code AND a different name for this to distinguish them, got code=%q name=%q", code, name)
	}

	repo := NewRepository(pool, 5*time.Second)
	from, to := shedWeightsWindow()
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "", "", "", 0, 0, 0)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if len(out.Rows) == 0 {
		t.Fatal("the fixture must produce at least one weighed row for this to say anything")
	}

	for _, row := range out.Rows {
		if row.ParkName != code {
			t.Fatalf("row park label = %q, want the short code %q -- the full name costs a column's width on every row for a word nobody on the farm uses", row.ParkName, code)
		}
	}

	// The vocabulary the filter chips are drawn from, compared to the ROWS rather than to a
	// literal. That comparison is the point: a change that relabels one side and not the other is
	// exactly the defect, and a literal on each side would let them drift apart while both halves
	// still passed their own assertion.
	var found bool
	for _, park := range out.Parks {
		if park.ParkID != repoPark {
			continue
		}
		found = true
		if park.Name != out.Rows[0].ParkName {
			t.Fatalf("the filter says %q while every row says %q -- one screen, two names for one park", park.Name, out.Rows[0].ParkName)
		}
	}
	if !found {
		t.Fatalf("the scoped park is missing from the filter vocabulary: %#v", out.Parks)
	}
}

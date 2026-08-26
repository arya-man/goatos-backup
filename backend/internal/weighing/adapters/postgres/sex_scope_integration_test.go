package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// A WHOLE-SHED WEIGH ON AN ALIAS PARTITION IS STILL CLAIMED BY ITS COHORT'S SEX.
//
// The defect this pins, found on real STG data (2026-08-26): the bucket's location is named
// "Godel 2 - Part 1", from which the partition extracts as the bare key "1", while the herd
// register writes the HUMAN label "Part 1" on the goat. Comparing those two strings raw matched
// nothing, so a shed of 38 live males was claimed by NEITHER sex: the Weights page reported 791
// kids for every kid, 490 male and 225 female, and 490 + 225 did not add up — 76 kids fell out
// of the page with nothing on screen to say why. That is the exact failure a reader spotted.
//
// The fixture is the STG shape in miniature: an alias location carrying no residents of its own,
// its physical shed carrying the animals, and the register labelling them the worded way.
func TestSexScopeClaimsAliasPartitionLumpShedByCohortSex(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		aliasLocation = "00000000-0000-4000-8000-0000000091a1"
		physicalShed  = "00000000-0000-4000-8000-0000000091a2"
		lumpBucket    = "00000000-0000-4000-8000-0000000091a3"
		maleGoat      = "00000000-0000-4000-8000-0000000091a4"
		femaleGoat    = "00000000-0000-4000-8000-0000000091a5"
	)

	// "Sirius 2" holds the animals; "Sirius 2 - Part 1" is the alias the weighing bucket points at
	// and holds none, which is exactly why the parent-shed fallback exists.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'Sirius 2', 'shed', $4::uuid, 'active'),
       ($3::uuid, $2::uuid, 'Sirius 2 - Part 1', 'shed', $4::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, physicalShed, repoTenant, aliasLocation, repoPark)

	// Two live residents on the PHYSICAL shed, both male on Part 1 — a single-sex pen — plus a
	// female on Part 2, so a matcher that ignored the partition entirely would see a mixed shed
	// and wrongly refuse to claim it. Passing this test requires matching the pen, not the shed.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990911', 'Beetal', 'male', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid),
       ($3::uuid, $2::uuid, 'G-990912', 'Beetal', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET sex = EXCLUDED.sex, shed_id = EXCLUDED.shed_id`,
		maleGoat, repoTenant, femaleGoat, repoParty, physicalShed, repoPark)

	// THE WORDED LABEL. This is the whole point: the register says "Part 1", the location name
	// yields "1", and those must resolve to the same pen.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'Part 1', 'Sirius 2 - Part 1'),
       ($1::uuid, $3::uuid, $4::uuid, 'Part 2', 'Sirius 2 - Part 2')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		repoTenant, maleGoat, femaleGoat, physicalShed)

	seedLoadBucket(t, ctx, pool, lumpBucket, repoCampaign, aliasLocation, "per_shed_partition")

	windowFrom := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	windowTo := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	maleScope, err := repo.resolveSexScope(ctx, repoTenant, []string{repoPark}, "male", windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveSexScope(male): %v", err)
	}
	if !claimsBucket(maleScope, aliasLocation) {
		t.Fatalf("the male scope must claim the alias-partition shed, got locations=%v partitions=%v", maleScope.LocationIDs, maleScope.PartitionLabels)
	}

	// And ONLY that sex claims it: a bucket claimed by both would double-count its kids the
	// moment a reader adds the two halves, which is the arithmetic this whole rule protects.
	femaleScope, err := repo.resolveSexScope(ctx, repoTenant, []string{repoPark}, "female", windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveSexScope(female): %v", err)
	}
	if claimsBucket(femaleScope, aliasLocation) {
		t.Fatalf("the female scope must not claim a male pen: locations=%v", femaleScope.LocationIDs)
	}

	// The parallel arrays stay in step. Built any other way — one aggregate DISTINCT and its
	// partner not, or two differently ordered aggregates — every index would shift and pair a
	// location with another bucket's partition.
	if len(maleScope.LocationIDs) != len(maleScope.PartitionLabels) {
		t.Fatalf("bucket arrays disagree: %d locations, %d partitions", len(maleScope.LocationIDs), len(maleScope.PartitionLabels))
	}

	// An empty sex is NO FILTER, not "nothing matched": the unfiltered page must run the query it
	// ran before this file existed and must not read a goat row at all.
	unfiltered, err := repo.resolveSexScope(ctx, repoTenant, []string{repoPark}, "", windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveSexScope(unfiltered): %v", err)
	}
	if !unfiltered.Empty() {
		t.Fatalf("an absent sex must resolve to an empty scope, got %#v", unfiltered)
	}

	// An unknown value is refused rather than silently widening the filter, which would show a
	// reader more kids than the heading they are reading says.
	if _, err := repo.resolveSexScope(ctx, repoTenant, []string{repoPark}, "either", windowFrom, windowTo); err == nil {
		t.Fatal("an unsupported sex must be rejected, not treated as no filter")
	}
}

func claimsBucket(scope SexScope, locationID string) bool {
	for _, id := range scope.LocationIDs {
		if id == locationID {
			return true
		}
	}
	return false
}

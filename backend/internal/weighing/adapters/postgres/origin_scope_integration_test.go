package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// A MIXED PEN IS SPLIT PER ANIMAL ON THE SCANNED ARM, AND CLAIMED BY NEITHER ON THE PENNED ONE.
//
// The defect this pins, caught by the maintainer on real data (2026-09-01): the first version of
// this filter judged a weigh by the PEN it happened in, from the weighing-owned load-tag mapping.
// Channapatna's Mandela 1 - Part 1 carries load tags and holds 13 kids of which only FOUR came off
// a load, so all 12 of that pen's individually scanned kids were filed as purchased — nine of them
// wrongly. "whole mandela 1 - part 1 are not purchased only few are purchased in that right??"
//
// A scanned weigh carries a TAG, so it can be answered per animal, and that is what this fixture
// proves. A whole-shed weigh carries none, so a mixed pen there is claimed by NEITHER side: one
// average weight cannot be divided between two cohorts, and claiming it whole is the very defect
// above one grain up.
func TestOriginScopeSplitsAMixedPenPerAnimalAndRefusesToClaimItWhole(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		mixedShed    = "00000000-0000-4000-8000-0000000092a1"
		mixedScanned = "00000000-0000-4000-8000-0000000092a3"
		mixedLump    = "00000000-0000-4000-8000-0000000092a7"
		// A SECOND campaign in a DIFFERENT week. One pen cannot hold two open buckets for the same
		// park-day, and a pen really does change mode between weeks — which is exactly the case
		// worth covering: the same pen is scanned one week and weighed whole the next, and the
		// filter must answer both consistently.
		lumpCampaign = "00000000-0000-4000-8000-0000000092a8"
		boughtGoat   = "00000000-0000-4000-8000-0000000092a4"
		homeGoat     = "00000000-0000-4000-8000-0000000092a5"
		loadID       = "00000000-0000-4000-8000-0000000092a6"
	)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'Altair North', 'shed', $3::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, mixedShed, repoTenant, repoPark)

	// Two residents of ONE pen: one bought, one not. That is the mixed pen in miniature.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-920001', 'Beetal', 'male', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid),
       ($3::uuid, $2::uuid, 'G-920002', 'Beetal', 'male', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id`,
		boughtGoat, repoTenant, homeGoat, repoParty, mixedShed, repoPark)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'bought-kid', 'bought-kid', 'global', true, 'active', now(), 'test'),
       ($1::uuid, $3::uuid, 'animal_identifier_1', 'home-kid',   'home-kid',   'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET goat_id = EXCLUDED.goat_id`, repoTenant, boughtGoat, homeGoat)

	// Only ONE of the two came off a load.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'originscope:load:920')
ON CONFLICT (load_id) DO NOTHING`, loadID, repoTenant, repoParty)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_load_goats (load_goat_id, tenant_id, load_id, goat_id)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid)
ON CONFLICT DO NOTHING`, repoTenant, loadID, boughtGoat)

	seedLoadBucket(t, ctx, pool, mixedScanned, repoCampaign, mixedShed, "individual_animal")
	seedShedWeightsCampaign(t, ctx, pool, lumpCampaign, "2026-07-22")
	seedLoadBucket(t, ctx, pool, mixedLump, lumpCampaign, mixedShed, "per_shed_partition")
	// Both kids scanned in the SAME pen, in the window.
	seedOriginScan(t, ctx, pool, mixedScanned, "bought-kid", 21.0)
	seedOriginScan(t, ctx, pool, mixedScanned, "home-kid", 22.0)

	windowFrom := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	windowTo := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	purchased, err := repo.resolveOriginScope(ctx, repoTenant, []string{repoPark}, OriginPurchased, windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveOriginScope(purchased): %v", err)
	}
	farmBorn, err := repo.resolveOriginScope(ctx, repoTenant, []string{repoPark}, OriginFarmBorn, windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveOriginScope(farm_born): %v", err)
	}

	// THE SPLIT. Both kids sit in one pen; the filter must still put them on opposite sides.
	if !hasTag(purchased, "bought-kid") || hasTag(purchased, "home-kid") {
		t.Fatalf("purchased must claim ONLY the kid that came off a load, got %v", purchased.Tags)
	}
	if !hasTag(farmBorn, "home-kid") || hasTag(farmBorn, "bought-kid") {
		t.Fatalf("farm born must claim ONLY the kid with no load, got %v", farmBorn.Tags)
	}

	// THE PEN ITSELF is claimed by neither: its one average cannot be divided between the two.
	if claimsBucket(purchased, mixedShed) || claimsBucket(farmBorn, mixedShed) {
		t.Fatalf("a mixed pen must be claimed by neither side, purchased=%v farmBorn=%v",
			purchased.LocationIDs, farmBorn.LocationIDs)
	}

	// An empty origin is NO FILTER, not "nothing matched": the unfiltered page must run the query
	// it ran before this file existed.
	unfiltered, err := repo.resolveOriginScope(ctx, repoTenant, []string{repoPark}, "", windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveOriginScope(unfiltered): %v", err)
	}
	if !unfiltered.Empty() {
		t.Fatalf("an absent origin must resolve to an empty scope, got %#v", unfiltered)
	}
	if _, err := repo.resolveOriginScope(ctx, repoTenant, []string{repoPark}, "bought", windowFrom, windowTo); err == nil {
		t.Fatal("an unsupported origin must be rejected, not treated as no filter")
	}
}

// A PEN WHOSE RESIDENTS ALL AGREE IS CLAIMED WHOLE — the case the seven real bought pens are in,
// and the one that makes the whole-shed arm useful at all. Without this the refusal above would be
// indistinguishable from a filter that simply never claims a pen.
func TestOriginScopeClaimsAWholeShedPenWhenEveryResidentAgrees(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		boughtShed = "00000000-0000-4000-8000-0000000095a1"
		homeShed   = "00000000-0000-4000-8000-0000000095a2"
		boughtPen  = "00000000-0000-4000-8000-0000000095a3"
		homePen    = "00000000-0000-4000-8000-0000000095a4"
		loadID     = "00000000-0000-4000-8000-0000000095a5"
	)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'Vega North', 'shed', $4::uuid, 'active'),
       ($3::uuid, $2::uuid, 'Vega South', 'shed', $4::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, boughtShed, repoTenant, homeShed, repoPark)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ('00000000-0000-4000-8000-0000000095b1'::uuid, $1::uuid, 'G-950001', 'Beetal', 'male', 'kid', 'alive', 'kid', $2::uuid, $3::uuid, $5::uuid, $3::uuid),
       ('00000000-0000-4000-8000-0000000095b2'::uuid, $1::uuid, 'G-950002', 'Beetal', 'male', 'kid', 'alive', 'kid', $2::uuid, $3::uuid, $5::uuid, $3::uuid),
       ('00000000-0000-4000-8000-0000000095b3'::uuid, $1::uuid, 'G-950003', 'Beetal', 'male', 'kid', 'alive', 'kid', $2::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id`,
		repoTenant, repoParty, boughtShed, homeShed, repoPark)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'originscope:load:950')
ON CONFLICT (load_id) DO NOTHING`, loadID, repoTenant, repoParty)
	// EVERY resident of Vega North came off the load; none of Vega South's did.
	// (Shed names avoid a trailing digit on purpose: the partition resolver reads one as a pen
	// suffix — the documented "Yashoda 2 is a whole shed" case — which would make these fixtures
	// fail for a reason that has nothing to do with origin.)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_load_goats (load_goat_id, tenant_id, load_id, goat_id)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, '00000000-0000-4000-8000-0000000095b1'::uuid),
       (gen_random_uuid(), $1::uuid, $2::uuid, '00000000-0000-4000-8000-0000000095b2'::uuid)
ON CONFLICT DO NOTHING`, repoTenant, loadID)

	seedLoadBucket(t, ctx, pool, boughtPen, repoCampaign, boughtShed, "per_shed_partition")
	seedLoadBucket(t, ctx, pool, homePen, repoCampaign, homeShed, "per_shed_partition")

	windowFrom := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	windowTo := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	purchased, err := repo.resolveOriginScope(ctx, repoTenant, []string{repoPark}, OriginPurchased, windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveOriginScope(purchased): %v", err)
	}
	if !claimsBucket(purchased, boughtShed) {
		t.Fatalf("an all-bought pen must be claimed as purchased, got %v", purchased.LocationIDs)
	}
	if claimsBucket(purchased, homeShed) {
		t.Fatalf("a pen with no bought resident must not be purchased, got %v", purchased.LocationIDs)
	}

	farmBorn, err := repo.resolveOriginScope(ctx, repoTenant, []string{repoPark}, OriginFarmBorn, windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveOriginScope(farm_born): %v", err)
	}
	if !claimsBucket(farmBorn, homeShed) {
		t.Fatalf("an all-home-bred pen must be claimed as farm born, got %v", farmBorn.LocationIDs)
	}
	if claimsBucket(farmBorn, boughtShed) {
		t.Fatalf("an all-bought pen must never be farm born, got %v", farmBorn.LocationIDs)
	}
}

// AN ANIMAL ON TWO LOADS IS COUNTED ONCE, AND ITS PEN STILL READS AS ALL-BOUGHT.
//
// procurement_load_goats is keyed per (load, animal), so an animal can legitimately sit on more
// than one load line — Channapatna's Mandela 1 - Part 1 is under loads 100 and 101, and that pen is
// the reason this case exists at all.
//
// HONESTY ABOUT WHAT THIS PINS: today's implementation reads that set through EXISTS, a SEMI-join,
// so it cannot fan out and this test passes even with the DISTINCT removed — mutation-tested, and
// recorded here so nobody reads the test as proof of the DISTINCT. What it pins is the BEHAVIOUR a
// reader depends on: a doubly-bought animal must not stop its pen reading as all-bought and drop
// the pen out of both halves of the page. Turning either EXISTS into a JOIN is exactly the change
// that would break it, and this is the test that would catch it.
func TestOriginScopeCountsAnAnimalOnTwoLoadsOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		twoLoadShed = "00000000-0000-4000-8000-0000000093a1"
		twoLoadPen  = "00000000-0000-4000-8000-0000000093a2"
		twoLoadGoat = "00000000-0000-4000-8000-0000000093a3"
		loadA       = "00000000-0000-4000-8000-0000000093a4"
		loadB       = "00000000-0000-4000-8000-0000000093a5"
	)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'Rigel North', 'shed', $3::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, twoLoadShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-930001', 'Beetal', 'male', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id`,
		twoLoadGoat, repoTenant, repoParty, twoLoadShed, repoPark)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, idempotency_key)
VALUES ($1::uuid, $3::uuid, $4::uuid, 'originscope:load:930a'),
       ($2::uuid, $3::uuid, $4::uuid, 'originscope:load:930b')
ON CONFLICT (load_id) DO NOTHING`, loadA, loadB, repoTenant, repoParty)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_load_goats (load_goat_id, tenant_id, load_id, goat_id)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $4::uuid),
       (gen_random_uuid(), $1::uuid, $3::uuid, $4::uuid)
ON CONFLICT DO NOTHING`, repoTenant, loadA, loadB, twoLoadGoat)

	seedLoadBucket(t, ctx, pool, twoLoadPen, repoCampaign, twoLoadShed, "per_shed_partition")

	windowFrom := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	windowTo := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	purchased, err := repo.resolveOriginScope(ctx, repoTenant, []string{repoPark}, OriginPurchased, windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveOriginScope(purchased): %v", err)
	}
	if !claimsBucket(purchased, twoLoadShed) {
		t.Fatalf("an animal bought on two loads must not stop its pen reading as all-bought, got %v", purchased.LocationIDs)
	}

	farmBorn, err := repo.resolveOriginScope(ctx, repoTenant, []string{repoPark}, OriginFarmBorn, windowFrom, windowTo)
	if err != nil {
		t.Fatalf("resolveOriginScope(farm_born): %v", err)
	}
	if claimsBucket(farmBorn, twoLoadShed) {
		t.Fatalf("a pen the farm bought twice over must never read as farm born, got %v", farmBorn.LocationIDs)
	}
}

func hasTag(scope ReportScope, tag string) bool {
	for _, t := range scope.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// seedOriginScan records one individual weigh of a tag inside a bucket, in the window these tests
// use. Proof and operator come from the shared weighing fixture.
func seedOriginScan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, tag string, weightKg float64) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		repoTenant, repoCampaign, bucketID, tag, weightKg, repoAnimalProof, repoOperator,
		"originscope:"+bucketID+":"+tag, time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC))
}

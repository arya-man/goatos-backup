package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// THE SET-BASED shed_targets CTE RESOLVES EXACTLY WHAT THE CORRELATED ONE DID.
//
// scopeShedTargetsCTE (2026-09-18) replaced two correlated subqueries -- "does this shed hold a
// live resident" and "which physical shed is this alias partition's parent" -- with keyed joins,
// because the correlated form cost every cold Weights read ~160 ms (PR #304's cold-path gate
// found it). A performance rewrite of a REPORTING RULE earns its keep only if it answers the same
// question, so this test keeps the retired CTE text verbatim and runs BOTH shapes over one fixture
// built to exercise every branch the resolution has: a partition whose residents sit on the alias
// location itself, an alias holding no one whose parent shed carries the animals (the parent
// fallback), a worded "Part 1" label against a numeric location suffix (the scrubbed-key match), a
// pen the register shows as MIXED (claimed by neither side), a bucket carrying its own explicit
// partition label, and a RE-ISSUED tag -- two identifier rows with the same scanned value on two
// goats, where DISTINCT ON must pick the newest for both shapes alike.
//
// The assertion is on every array the resolver returns, in order, for every filter value, so a
// change to the shared CTE that shifts a bucket, drops a tag or reorders a pair turns this red.

// legacyScopeShedTargetsCTE is the correlated-subquery form as it shipped before 2026-09-18.
// Kept VERBATIM (comments stripped) so the comparison is against what actually ran, not a
// paraphrase of it.
const legacyScopeShedTargetsCTE = `
shed_targets AS (
  SELECT DISTINCT s.location_id, s.partition_label,
         COALESCE(
           CASE WHEN EXISTS (SELECT 1 FROM goats gg WHERE gg.tenant_id = $1::uuid
                              AND gg.lifecycle_status = 'alive' AND gg.shed_id = s.location_id)
                THEN s.location_id END,
           (SELECT phys.location_id FROM locations phys
            JOIN locations l ON l.location_id = s.location_id AND l.tenant_id = $1::uuid
            WHERE phys.tenant_id = l.tenant_id
              AND phys.parent_location_id = l.parent_location_id
              AND phys.location_type = 'shed'
              AND phys.name = regexp_replace(l.name, '\s*(-\s*)?(Part\s*)?[0-9]+$', '')
            LIMIT 1)
         ) AS resolved_id,
         COALESCE(NULLIF(s.partition_label, ''),
                  NULLIF((regexp_match((SELECT l.name FROM locations l WHERE l.location_id = s.location_id),
                                       '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], ''),
                  '') AS resolved_partition_label
  FROM scoped s
  WHERE s.weighing_category = 'per_shed_partition'
),
`

// Fixture identities for the shed_targets tests (shared by the equivalence test and the
// adversarial matrix test below).
const (
	// An alias partition that HOLDS its own residents (register writes shed_id = alias): two
	// bought males, so it is claimed whole by male and by purchased through the occupied path.
	selfShed = "00000000-0000-4000-8000-0000000093a1"
	// An alias partition holding NO ONE; its physical parent carries the animals.
	emptyAlias   = "00000000-0000-4000-8000-0000000093a2"
	physicalShed = "00000000-0000-4000-8000-0000000093a3"
	// A MIXED pen: both sexes, and one bought one not.
	mixedShed = "00000000-0000-4000-8000-0000000093a4"
	// A second alias of the same physical shed, so the parent lookup must pick by NAME.
	emptyAliasTwo = "00000000-0000-4000-8000-0000000093a5"
	// The self-held alias's PARENT, holding a female on Part 1: it exists precisely so that a
	// resolver which skipped the "holds its own residents" test and went straight to the parent
	// would claim the self-held pen for the wrong sex, and this test would say so.
	selfParent = "00000000-0000-4000-8000-0000000093a6"

	bucketSelf      = "00000000-0000-4000-8000-0000000093b1"
	bucketAlias     = "00000000-0000-4000-8000-0000000093b2"
	bucketMixed     = "00000000-0000-4000-8000-0000000093b3"
	bucketAliasTwo  = "00000000-0000-4000-8000-0000000093b4"
	bucketLabelled  = "00000000-0000-4000-8000-0000000093b5"
	bucketScanned   = "00000000-0000-4000-8000-0000000093b6"
	lumpCampaign    = "00000000-0000-4000-8000-0000000093c1"
	loadID          = "00000000-0000-4000-8000-0000000093d1"
	goatSelfA       = "00000000-0000-4000-8000-0000000093e1"
	goatSelfB       = "00000000-0000-4000-8000-0000000093e2"
	goatPart1A      = "00000000-0000-4000-8000-0000000093e3"
	goatPart1B      = "00000000-0000-4000-8000-0000000093e4"
	goatPart2       = "00000000-0000-4000-8000-0000000093e5"
	goatMixedM      = "00000000-0000-4000-8000-0000000093e6"
	goatMixedF      = "00000000-0000-4000-8000-0000000093e7"
	goatReissuedOld = "00000000-0000-4000-8000-0000000093e8"
	goatReissuedNew = "00000000-0000-4000-8000-0000000093e9"
	goatSelfParentF = "00000000-0000-4000-8000-0000000093ea"
)

// seedShedTargetsFixture builds the whole-shed resolution fixture described on the equivalence
// test: a self-held alias with a parent, an empty alias resolved through its parent, a mixed pen,
// an explicitly labelled bucket, and a re-issued tag.
func seedShedTargetsFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
VALUES ($1::uuid, $6::uuid, 'Vega 1 - Part 1', 'shed', $7::uuid, 'active'),
       ($2::uuid, $6::uuid, 'Vega 2 - Part 1', 'shed', $7::uuid, 'active'),
       ($3::uuid, $6::uuid, 'Vega 2',          'shed', $7::uuid, 'active'),
       ($4::uuid, $6::uuid, 'Vega 3',          'shed', $7::uuid, 'active'),
       ($5::uuid, $6::uuid, 'Vega 2 - Part 2', 'shed', $7::uuid, 'active'),
       ($8::uuid, $6::uuid, 'Vega 1',          'shed', $7::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, selfShed, emptyAlias, physicalShed, mixedShed, emptyAliasTwo, repoTenant, repoPark, selfParent)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid,  $10::uuid, 'G-930001', 'Beetal', 'male',   'kid', 'alive', 'kid', $11::uuid, $13::uuid, $12::uuid, $13::uuid),
       ($2::uuid,  $10::uuid, 'G-930002', 'Beetal', 'male',   'kid', 'alive', 'kid', $11::uuid, $13::uuid, $12::uuid, $13::uuid),
       ($3::uuid,  $10::uuid, 'G-930003', 'Beetal', 'female', 'kid', 'alive', 'kid', $11::uuid, $14::uuid, $12::uuid, $14::uuid),
       ($4::uuid,  $10::uuid, 'G-930004', 'Beetal', 'female', 'kid', 'alive', 'kid', $11::uuid, $14::uuid, $12::uuid, $14::uuid),
       ($5::uuid,  $10::uuid, 'G-930005', 'Beetal', 'male',   'kid', 'alive', 'kid', $11::uuid, $14::uuid, $12::uuid, $14::uuid),
       ($6::uuid,  $10::uuid, 'G-930006', 'Beetal', 'male',   'kid', 'alive', 'kid', $11::uuid, $15::uuid, $12::uuid, $15::uuid),
       ($7::uuid,  $10::uuid, 'G-930007', 'Beetal', 'female', 'kid', 'alive', 'kid', $11::uuid, $15::uuid, $12::uuid, $15::uuid),
       ($8::uuid,  $10::uuid, 'G-930008', 'Beetal', 'female', 'kid', 'alive', 'kid', $11::uuid, $15::uuid, $12::uuid, $15::uuid),
       ($9::uuid,  $10::uuid, 'G-930009', 'Beetal', 'male',   'kid', 'alive', 'kid', $11::uuid, $15::uuid, $12::uuid, $15::uuid),
       ($16::uuid, $10::uuid, 'G-930010', 'Beetal', 'female', 'kid', 'alive', 'kid', $11::uuid, $17::uuid, $12::uuid, $17::uuid)
ON CONFLICT (goat_id) DO UPDATE SET sex = EXCLUDED.sex, shed_id = EXCLUDED.shed_id`,
		goatSelfA, goatSelfB, goatPart1A, goatPart1B, goatPart2, goatMixedM, goatMixedF, goatReissuedOld, goatReissuedNew,
		repoTenant, repoParty, repoPark, selfShed, physicalShed, mixedShed, goatSelfParentF, selfParent)

	// Worded labels: on the physical shed Part 1 is all-female and Part 2 is one male; the
	// self-held alias's own residents carry "Part 1" against a location name ending in "1", so
	// the scrubbed-key match is exercised on the occupied path as well as the parent fallback.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $5::uuid, 'Part 1', 'Vega 2 - Part 1'),
       ($1::uuid, $3::uuid, $5::uuid, 'Part 1', 'Vega 2 - Part 1'),
       ($1::uuid, $4::uuid, $5::uuid, 'Part 2', 'Vega 2 - Part 2'),
       ($1::uuid, $6::uuid, $8::uuid, 'Part 1', 'Vega 1 - Part 1'),
       ($1::uuid, $7::uuid, $8::uuid, 'Part 1', 'Vega 1 - Part 1'),
       ($1::uuid, $9::uuid, $10::uuid, 'Part 1', 'Vega 1 - Part 1')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		repoTenant, goatPart1A, goatPart1B, goatPart2, physicalShed, goatSelfA, goatSelfB, selfShed, goatSelfParentF, selfParent)

	// The self-held pen is ALL bought (claimed whole by purchased); the mixed pen has one bought
	// animal, so origin sees it as mixed too.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shedtargets:load:930')
ON CONFLICT (load_id) DO NOTHING`, loadID, repoTenant, repoParty)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_load_goats (load_goat_id, tenant_id, load_id, goat_id)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid), (gen_random_uuid(), $1::uuid, $2::uuid, $4::uuid), (gen_random_uuid(), $1::uuid, $2::uuid, $5::uuid)
ON CONFLICT DO NOTHING`, repoTenant, loadID, goatMixedM, goatSelfA, goatSelfB)

	// Tags: a straight tag per scanned kid, plus ONE RE-ISSUED tag. "re-930" was on a female first
	// and is now on a male; the identifier rows differ only in case (the lifetime-unique column is
	// normalized_value) and in created_at, and the resolver's DISTINCT ON must pick the newest.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version, created_at)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'self-a-930', 'SELF-A-930', 'global', true, 'active', now(), 'test', now()),
       ($1::uuid, $3::uuid, 'animal_identifier_1', 'mixed-m-930', 'MIXED-M-930', 'global', true, 'active', now(), 'test', now()),
       ($1::uuid, $4::uuid, 'animal_identifier_1', 'mixed-f-930', 'MIXED-F-930', 'global', true, 'active', now(), 'test', now()),
       ($1::uuid, $5::uuid, 'animal_identifier_1', 'RE-930', 'RE-930-OLD', 'global', true, 'retired', now() - interval '30 days', 'test', now() - interval '30 days'),
       ($1::uuid, $6::uuid, 'animal_identifier_1', 're-930', 'RE-930-NEW', 'global', true, 'active', now(), 'test', now())
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET goat_id = EXCLUDED.goat_id`,
		repoTenant, goatSelfA, goatMixedM, goatMixedF, goatReissuedOld, goatReissuedNew)

	// Buckets: one scanned bucket, and whole-shed buckets covering every resolution branch.
	seedLoadBucket(t, ctx, pool, bucketScanned, repoCampaign, selfShed, "individual_animal")
	seedShedWeightsCampaign(t, ctx, pool, lumpCampaign, "2026-07-22")
	seedLoadBucket(t, ctx, pool, bucketSelf, lumpCampaign, selfShed, "per_shed_partition")
	seedLoadBucket(t, ctx, pool, bucketAlias, lumpCampaign, emptyAlias, "per_shed_partition")
	seedLoadBucket(t, ctx, pool, bucketAliasTwo, lumpCampaign, emptyAliasTwo, "per_shed_partition")
	seedLoadBucket(t, ctx, pool, bucketMixed, lumpCampaign, mixedShed, "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, bucketLabelled, lumpCampaign, physicalShed, "Part 2", "per_shed_partition")

	seedOriginScan(t, ctx, pool, bucketScanned, "self-a-930", 21.0)
	seedOriginScan(t, ctx, pool, bucketScanned, "mixed-m-930", 22.0)
	seedOriginScan(t, ctx, pool, bucketScanned, "mixed-f-930", 23.0)
	seedOriginScan(t, ctx, pool, bucketScanned, "RE-930", 24.0)
	seedOriginScan(t, ctx, pool, bucketScanned, "unknown-930", 25.0)

}

func TestSetBasedShedTargetsResolvesTheSameScopeAsTheCorrelatedForm(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	seedShedTargetsFixture(t, ctx, pool)

	windowFrom := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	windowTo := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	parks := []string{repoPark}

	// The fixture must actually reach every branch, or the equality below proves nothing.
	sanity, err := resolveSexScope(ctx, pool, repoTenant, parks, "female", windowFrom, windowTo, true)
	if err != nil {
		t.Fatalf("resolveSexScope(female): %v", err)
	}
	if !claimsBucket(sanity, emptyAlias) {
		t.Fatalf("the empty alias must resolve through its parent shed to an all-female Part 1, got %v / %v", sanity.LocationIDs, sanity.PartitionLabels)
	}
	if claimsBucket(sanity, mixedShed) || claimsBucket(sanity, selfShed) {
		t.Fatalf("a mixed pen must be claimed by neither sex, got %v", sanity.LocationIDs)
	}
	if hasTag(sanity, "re-930") {
		t.Fatalf("a re-issued tag resolves to its NEWEST animal (male), the female scope must not carry it: %v", sanity.Tags)
	}
	male, err := resolveSexScope(ctx, pool, repoTenant, parks, "male", windowFrom, windowTo, true)
	if err != nil {
		t.Fatalf("resolveSexScope(male): %v", err)
	}
	if !hasTag(male, "re-930") || !claimsBucket(male, emptyAliasTwo) || !claimsBucket(male, physicalShed) || !claimsBucket(male, selfShed) {
		t.Fatalf("male must carry the re-issued tag, claim Part 2 through both the alias and the labelled bucket, and claim the self-held pen, got tags=%v locations=%v partitions=%v", male.Tags, male.LocationIDs, male.PartitionLabels)
	}

	// NOW THE EQUALITY: same fixture, both CTE shapes, every filter value, every array.
	sexQuery := scopeQueryText(t, sexScopeQuery, scopeShedTargetsCTE)
	originQuery := scopeQueryText(t, originScopeQuery, originScopeShedTargetsCTE)
	bucketQuery := scopeQueryText(t, originBucketScopeQuery, originScopeShedTargetsCTE)

	for _, sex := range []string{"male", "female"} {
		for _, allTime := range []bool{false, true} {
			got := runScopeQueryForTest(t, ctx, pool, sexQuery.current, repoTenant, parks, windowFrom, windowTo, sex, allTime)
			want := runScopeQueryForTest(t, ctx, pool, sexQuery.legacy, repoTenant, parks, windowFrom, windowTo, sex, allTime)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("sex scope %q allTime=%v: set-based CTE returned %+v, correlated CTE returned %+v", sex, allTime, got, want)
			}
			assertScopeReachesFixture(t, got, sex)
		}
	}
	for _, origin := range []string{OriginPurchased, OriginFarmBorn} {
		for _, allTime := range []bool{false, true} {
			got := runScopeQueryForTest(t, ctx, pool, originQuery.current, repoTenant, parks, windowFrom, windowTo, origin, allTime)
			want := runScopeQueryForTest(t, ctx, pool, originQuery.legacy, repoTenant, parks, windowFrom, windowTo, origin, allTime)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("origin scope %q allTime=%v: set-based CTE returned %+v, correlated CTE returned %+v", origin, allTime, got, want)
			}
		}
		got := runBucketScopeQueryForTest(t, ctx, pool, bucketQuery.current, repoTenant, parks, origin)
		want := runBucketScopeQueryForTest(t, ctx, pool, bucketQuery.legacy, repoTenant, parks, origin)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("origin bucket scope %q: set-based CTE returned %+v, correlated CTE returned %+v", origin, got, want)
		}
		if len(got.LocationIDs) == 0 {
			t.Fatalf("origin bucket scope %q claimed nothing; the fixture must reach the whole-shed arm", origin)
		}
	}
}

// assertScopeReachesFixture guards the guard: an equality between two queries that both returned
// nothing would pass vacuously, so the sex scope must be seen claiming the fixture's buckets.
func assertScopeReachesFixture(t *testing.T, scope scopeArrays, sex string) {
	t.Helper()
	if len(scope.Tags) == 0 || len(scope.LocationIDs) == 0 {
		t.Fatalf("sex scope %q resolved tags=%v locations=%v; the fixture must produce both tags and buckets", sex, scope.Tags, scope.LocationIDs)
	}
}

// scopeArrays is the resolver's raw result row, compared array-for-array and in order.
type scopeArrays struct {
	Tags, AllTimeTags, LocationIDs, PartitionLabels []string
}

type scopeQueryPair struct{ current, legacy string }

// scopeQueryText pairs a shipped query with the same query built on the retired CTE. The current
// text MUST contain the shared CTE exactly once, so a refactor that stops using it (and therefore
// stops being covered here) fails loudly instead of passing vacuously.
func scopeQueryText(t *testing.T, current, cte string) scopeQueryPair {
	t.Helper()
	if strings.Count(current, cte) != 1 {
		t.Fatalf("query does not embed its shed_targets CTE exactly once; this equivalence test no longer covers it")
	}
	return scopeQueryPair{current: current, legacy: strings.Replace(current, cte, legacyScopeShedTargetsCTE, 1)}
}

func runScopeQueryForTest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, q, tenantID string, parkIDs []string, from, to time.Time, filter string, allTime bool) scopeArrays {
	t.Helper()
	var out scopeArrays
	if err := pool.QueryRow(ctx, q, tenantID, parkIDs, from, to, filter, allTime).Scan(&out.Tags, &out.AllTimeTags, &out.LocationIDs, &out.PartitionLabels); err != nil {
		t.Fatalf("scope query: %v", err)
	}
	return out
}

func runBucketScopeQueryForTest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, q, tenantID string, parkIDs []string, origin string) scopeArrays {
	t.Helper()
	var out scopeArrays
	if err := pool.QueryRow(ctx, q, tenantID, parkIDs, origin).Scan(&out.LocationIDs, &out.PartitionLabels); err != nil {
		t.Fatalf("bucket scope query: %v", err)
	}
	return out
}

// ADVERSARIAL GRAIN TEST for the set-based shed_targets CTE: one-to-many fan-out, park scope, the
// status matrix, and the page boundary of the bucket lists, in one fixture.
//
// The bucket arrays are consumed downstream as `(location_id, partition_label) = ANY(...)` pairs by
// index, so a resolver that named one bucket twice, or named a bucket the caller's park does not
// own, or claimed a pen through a resident who has EXITED, would silently double or widen every
// whole-shed aggregate on the page. The joins that prevent it (occupied is DISTINCT on shed_id,
// parent_sheds is DISTINCT ON (parent, name), the residents' lifecycle filter, the campaign park
// predicate) live in the CTE, one hop from the aggregates that depend on them, so they are asserted
// here rather than assumed.
func TestShedTargetsOneToManyPageBoundaryParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedTargetsFixture(t, ctx, pool)

	const (
		otherPark       = "00000000-0000-4000-8000-0000000093f1"
		otherParkShed   = "00000000-0000-4000-8000-0000000093f2"
		otherCampaign   = "00000000-0000-4000-8000-0000000093f3"
		otherParkBucket = "00000000-0000-4000-8000-0000000093f4"
		otherParkGoat   = "00000000-0000-4000-8000-0000000093f5"
		secondCampaign  = "00000000-0000-4000-8000-0000000093f6"
		selfShedAgain   = "00000000-0000-4000-8000-0000000093f7"
		canceledBucket  = "00000000-0000-4000-8000-0000000093f8"
		exitedFemale    = "00000000-0000-4000-8000-0000000093f9"
		// Operators are single-park (migration 000065), so the other park's bucket is executed
		// by a tenant-scoped director, the one principal allowed to span parks.
		otherParkDirector = "00000000-0000-4000-8000-0000000093fa"
	)
	windowFrom := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	windowTo := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	// ONE-TO-MANY, two ways. The self-held pen already holds MANY residents (collapsed by the
	// GROUP BY); it is now also weighed whole in a SECOND campaign week, so one location resolves
	// through shed_targets twice. Each (location, partition) pair must still appear ONCE.
	seedShedWeightsCampaign(t, ctx, pool, secondCampaign, "2026-07-15")
	seedLoadBucket(t, ctx, pool, selfShedAgain, secondCampaign, selfShed, "per_shed_partition")

	// STATUS MATRIX, residents: a SOLD (exited) female in the all-male self-held pen must not turn it
	// mixed -- only live residents answer for a pen.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id, exited_at)
VALUES ($1::uuid, $2::uuid, 'G-930011', 'Beetal', 'female', 'kid', 'sold', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid, now())
ON CONFLICT (goat_id) DO UPDATE SET lifecycle_status = EXCLUDED.lifecycle_status, shed_id = EXCLUDED.shed_id`,
		exitedFemale, repoTenant, repoParty, selfShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Part 1', 'Vega 1 - Part 1')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		repoTenant, exitedFemale, selfShed)

	// STATUS MATRIX, buckets: a CANCELED campaign shed on the mixed pen is not scoped at all, so
	// the mixed pen stays unclaimed for the same reason as before and no canceled row leaks in.
	seedLoadBucket(t, ctx, pool, canceledBucket, secondCampaign, mixedShed, "per_shed_partition")
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaign_sheds SET status = 'canceled' WHERE campaign_shed_id = $1::uuid`, canceledBucket)

	// PARK SCOPE: an all-male pen in ANOTHER park, weighed whole there.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
VALUES ($1::uuid, $3::uuid, 'Other Park', 'park', NULL, 'active'),
       ($2::uuid, $3::uuid, 'Orion',      'shed', $1::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, otherPark, otherParkShed, repoTenant)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-930012', 'Beetal', 'male', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO NOTHING`, otherParkGoat, repoTenant, repoParty, otherParkShed, otherPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-22', '2026-07-28', '2026-07-22', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, otherCampaign, repoTenant, otherPark, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'growth_director', 'tenant', $1::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, otherParkDirector)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'other-park', 'per_shed_partition', $5::uuid, 1)
ON CONFLICT (campaign_shed_id) DO NOTHING`, otherParkBucket, otherCampaign, repoTenant, otherParkShed, otherParkDirector)

	male, err := resolveSexScope(ctx, pool, repoTenant, []string{repoPark}, "male", windowFrom, windowTo, false)
	if err != nil {
		t.Fatalf("resolveSexScope(male): %v", err)
	}
	female, err := resolveSexScope(ctx, pool, repoTenant, []string{repoPark}, "female", windowFrom, windowTo, false)
	if err != nil {
		t.Fatalf("resolveSexScope(female): %v", err)
	}

	// ONE-TO-MANY: every (location, partition) pair once, however many residents or campaign
	// weeks stand behind it.
	seen := map[string]int{}
	for i, loc := range male.LocationIDs {
		seen[loc+"|"+male.PartitionLabels[i]]++
	}
	for pair, n := range seen {
		if n != 1 {
			t.Fatalf("bucket %q resolved %d times; a pair named twice doubles every whole-shed aggregate downstream", pair, n)
		}
	}
	if seen[selfShed+"|"] != 1 {
		t.Fatalf("the self-held pen (weighed whole in two campaign weeks) must be claimed exactly once, got locations=%v partitions=%v", male.LocationIDs, male.PartitionLabels)
	}

	// STATUS MATRIX: the exited female did not make the pen mixed, and the canceled bucket did not
	// bring the mixed pen in for either side.
	if claimsBucket(female, selfShed) {
		t.Fatalf("an EXITED female must not make an all-male pen mixed or claim it for female: %v", female.LocationIDs)
	}
	if claimsBucket(male, mixedShed) || claimsBucket(female, mixedShed) {
		t.Fatalf("a canceled campaign shed must not scope the mixed pen for either side: male=%v female=%v", male.LocationIDs, female.LocationIDs)
	}

	// PARK SCOPE: this park's read never names the other park's pen, and the other park's read
	// names only its own.
	if claimsBucket(male, otherParkShed) {
		t.Fatalf("the other park's pen leaked into this park's scope: %v", male.LocationIDs)
	}
	other, err := resolveSexScope(ctx, pool, repoTenant, []string{otherPark}, "male", windowFrom, windowTo, false)
	if err != nil {
		t.Fatalf("resolveSexScope(other park): %v", err)
	}
	if !claimsBucket(other, otherParkShed) || len(other.LocationIDs) != 1 || len(other.Tags) != 0 {
		t.Fatalf("the other park must resolve exactly its own pen and none of this park's tags, got locations=%v tags=%v", other.LocationIDs, other.Tags)
	}

	// PAGE BOUNDARY: the scope is NOT paginated -- it is bounded by the scoped buckets, and every
	// claimable bucket is present, so a downstream page reads the same cohort on every page.
	for _, want := range []string{selfShed, emptyAliasTwo, physicalShed} {
		if !claimsBucket(male, want) {
			t.Fatalf("male scope must carry every claimable bucket (missing %s): locations=%v", want, male.LocationIDs)
		}
	}
	if got, want := len(male.LocationIDs), len(male.PartitionLabels); got != want {
		t.Fatalf("bucket arrays disagree in length: %d locations vs %d partitions", got, want)
	}
}

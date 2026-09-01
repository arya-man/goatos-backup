package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// LUMP-SUM SHEDS IN THE GAIN CHARTS (maintainer decision 2026-08-25).
//
// The Breed-wise daily gain chart used to count same-animal pairs only, so a
// shed weighed lump-sum three times a week — Castro 1/2/3's ~200 Anantapur —
// never appeared in it. The maintainer directed: include lump-sum, take the
// shed's average change, and keep EVERY animal of that shed in the one band
// that average falls into. These pin the three halves of that rule on the real
// production read (GetWeightDemographics):
//
//  1. a homogeneous-breed lump-sum shed adds ALL its animals to exactly ONE
//     band — the band of its own first→latest average change — and its animals
//     join the breed's gain bucket as a weighted mean;
//  2. the same-animal population is still counted alongside it (the two arms
//     add, never replace);
//  3. a MIXED-breed shed still joins no breed row: the shed average is one
//     number, and splitting it across breeds would invent a distribution
//     nobody measured (standing whole-shed attribution rule).

const (
	lgMixedShed   = "3a4f7d10-52be-4a41-9c3e-6f2b8e9d1c02"
	lgMixedBkt    = "00000000-0000-4000-8000-000000009701"
	lgMixedBktTwo = "00000000-0000-4000-8000-000000009702"
	lgCampaignTwo = "00000000-0000-4000-8000-000000009703"
	lgPerShedBkt2 = "00000000-0000-4000-8000-000000009704"
	lgMixedGoatA  = "00000000-0000-4000-8000-000000009711"
	lgMixedGoatB  = "00000000-0000-4000-8000-000000009712"
	lgTagGoat     = "00000000-0000-4000-8000-000000009713"
)

// lgSeedSecondCampaign models the real shape of repeat lump-sum weighing: each
// week's weigh is its own campaign bucket (one open observation per bucket), and
// lump_span pairs them by (location, partition) across buckets.
func lgSeedSecondCampaign(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-08-03', '2026-08-09', '2026-08-05', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, lgCampaignTwo, repoTenant, repoPark, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, park_id, start_business_date, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Perth', 'per_shed_partition', $5::uuid, 0, $6::uuid, '2026-08-05', 'completed')
ON CONFLICT (campaign_shed_id) DO NOTHING`, lgPerShedBkt2, lgCampaignTwo, repoTenant, repoPerShed, repoOperator, repoPark)
}

// lgNameShedsWithoutTrailingDigits pins the cohort resolution to the WHOLE shed:
// a location name ending in a digit is read as a partition hint by shed_targets
// (the Castro 1 convention), which would then require per-goat pen rows. These
// tests are about the gain arms, not partition resolution, so the sheds get
// digit-free names.
func lgNameShedsWithoutTrailingDigits(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `UPDATE locations SET name='Perth' WHERE location_id=$1::uuid`, repoPerShed)
}

func seedLumpSumObservation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID string, count int, avgKg float64, at time.Time) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid, $8::uuid, $9, $10::timestamptz)`,
		repoTenant, repoCampaign, bucketID, avgKg*float64(count), avgKg, count,
		repoShedProof, repoOperator, "lumpgain:"+bucketID+":"+at.Format(time.RFC3339), at)
}

func TestBreedGainBandsIncludeHomogeneousLumpSumShedsAtShedAverage(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The per-shed bucket's four fixture residents become a homogeneous cohort.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='Anantapur Sheep', sex='male', management_stage='F2-Male'
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid`, repoTenant, repoPerShed)
	lgNameShedsWithoutTrailingDigits(t, ctx, pool)
	lgSeedSecondCampaign(t, ctx, pool)

	// Lump-sum: avg 20.0 → 22.8 over 7 days = 400 g/day, 4 animals ⇒ all four in
	// the >250 band. Two weekly buckets on the same shed, like production.
	seedLumpSumObservation(t, ctx, pool, repoShedScope, 4, 20.0, time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC))
	seedLumpSumObservation(t, ctx, pool, lgPerShedBkt2, 4, 22.8, time.Date(2026, 8, 8, 6, 0, 0, 0, time.UTC))

	// One same-animal pair of the same breed: 24.0 → 24.7 over 7 days = 100 g/day
	// ⇒ the ≤180 band. The two arms must ADD.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-991701', 'Anantapur Sheep', 'male', 'kid', 'alive', 'F2-Male', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO NOTHING`, lgTagGoat, repoTenant, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'chip-lumpgain', 'chip-lumpgain', 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET goat_id=EXCLUDED.goat_id, status='active'`,
		repoTenant, lgTagGoat)
	seedShedWeightScan(t, ctx, pool, "chip-lumpgain", 24.0, time.Date(2026, 8, 1, 7, 0, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, "chip-lumpgain", 24.7, time.Date(2026, 8, 8, 7, 0, 0, 0, time.UTC))

	// A MIXED-breed lump-sum shed with a valid pair: must reach NO breed row.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', 'MixedPen', 'active', 991) ON CONFLICT (location_id) DO NOTHING`,
		lgMixedShed, repoTenant)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, park_id, start_business_date, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'MixedPen', 'per_shed_partition', $5::uuid, 0, $8::uuid, '2026-08-01', 'completed'),
       ($6::uuid, $7::uuid, $3::uuid, $4::uuid, 'shed', 'MixedPen', 'per_shed_partition', $5::uuid, 0, $8::uuid, '2026-08-08', 'completed')
ON CONFLICT (campaign_shed_id) DO NOTHING`, lgMixedBkt, repoCampaign, repoTenant, lgMixedShed, repoOperator, lgMixedBktTwo, lgCampaignTwo, repoPark)
	for i, goat := range []struct{ id, display, breed string }{
		{lgMixedGoatA, "G-991702", "Beetal"},
		{lgMixedGoatB, "G-991703", "Sojat"},
	} {
		_ = i
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, $4, 'female', 'adult', 'alive', 'adult', $5::uuid, $6::uuid, $7::uuid, $6::uuid)
ON CONFLICT (goat_id) DO NOTHING`, goat.id, repoTenant, goat.display, goat.breed, repoParty, lgMixedShed, repoPark)
	}
	seedLumpSumObservation(t, ctx, pool, lgMixedBkt, 2, 30.0, time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC))
	seedLumpSumObservation(t, ctx, pool, lgMixedBktTwo, 2, 31.4, time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC))

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "")
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}

	// Band chart: 4 lump-sum animals at 400 g/day (>250) + 1 scanned animal at
	// 100 g/day (≤180) = 5, partitioned exactly.
	var foundBand bool
	for _, row := range out.GainThresholdsByBreed {
		if row.Label != "Anantapur Sheep" {
			if row.Label == "Beetal" || row.Label == "Sojat" {
				t.Fatalf("mixed-breed lump-sum shed leaked into the band chart as %q: %+v", row.Label, row)
			}
			continue
		}
		foundBand = true
		if row.Animals != 5 || row.Above250 != 4 || row.AtOrBelow180 != 1 || row.Band180To200 != 0 || row.Band200To250 != 0 {
			t.Fatalf("Anantapur bands = %+v, want 5 animals: 4 in >250 (shed average 400 g/day), 1 in ≤180", row)
		}
	}
	if !foundBand {
		t.Fatal("Anantapur Sheep missing from gain_thresholds_by_breed")
	}

	// Gain bucket: weighted mean over the SAME population — (4×400 + 1×100)/5 = 340.
	var foundGain bool
	for _, row := range out.GainByBreed {
		if row.Label != "Anantapur Sheep" {
			if row.Label == "Beetal" || row.Label == "Sojat" {
				t.Fatalf("mixed-breed lump-sum shed leaked into gain_by_breed as %q: %+v", row.Label, row)
			}
			continue
		}
		foundGain = true
		if row.Animals != 5 {
			t.Fatalf("gain_by_breed animals = %d, want 5 (4 lump-sum + 1 scanned)", row.Animals)
		}
		if row.MedianGainGPerDay < 339 || row.MedianGainGPerDay > 341 {
			t.Fatalf("gain_by_breed g/day = %v, want ≈340 (weighted (4×400 + 100)/5)", row.MedianGainGPerDay)
		}
	}
	if !foundGain {
		t.Fatal("Anantapur Sheep missing from gain_by_breed")
	}
}

// A lump-sum shed weighed ONCE has no measurable change and must stay out of the
// gain arms entirely — a single average is a level, not a gain.
func TestSingleLumpSumWeighContributesNoGain(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='Anantapur Sheep', sex='male', management_stage='F2-Male'
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid`, repoTenant, repoPerShed)
	lgNameShedsWithoutTrailingDigits(t, ctx, pool)
	seedLumpSumObservation(t, ctx, pool, repoShedScope, 4, 20.0, time.Date(2026, 8, 3, 6, 0, 0, 0, time.UTC))

	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark},
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC), "", "")
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	for _, row := range out.GainThresholdsByBreed {
		if row.Label == "Anantapur Sheep" {
			t.Fatalf("a single lump-sum weigh must contribute no gain, got %+v", row)
		}
	}
	// The LEVEL bucket still counts them — one weigh is a real weight.
	var level bool
	for _, row := range out.ByBreed {
		if row.Label == "Anantapur Sheep" && row.Animals == 4 {
			level = true
		}
	}
	if !level {
		t.Fatalf("single lump-sum weigh must still feed the weight-level bucket, got %+v", out.ByBreed)
	}
}

// One-to-many, window/page boundary and park scope, adversarially:
//
//   - a shed weighed THREE times in the window still contributes its animals
//     ONCE (first→latest span; intermediate weighs must not multiply the count);
//   - a weigh AFTER the window's end must not become the "latest" side of the
//     span (the boundary is the query window, not the table);
//   - another park's homogeneous lump-sum shed stays out of a park-scoped read
//     and appears when its park is selected.
func TestLumpSumGainOneToManyPageBoundaryParkScopeBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		lgCampaignThree = "00000000-0000-4000-8000-000000009705"
		lgPerShedBkt3   = "00000000-0000-4000-8000-000000009706"
		lgCampaignFour  = "00000000-0000-4000-8000-000000009707"
		lgPerShedBkt4   = "00000000-0000-4000-8000-000000009708"
		lgOtherPark     = "5b6c8e20-71df-4c63-8e5f-0a1b2c3d4e01"
		lgOtherShed     = "5b6c8e20-71df-4c63-8e5f-0a1b2c3d4e02"
		lgOtherCampaign = "00000000-0000-4000-8000-000000009709"
		lgOtherCampTwo  = "00000000-0000-4000-8000-000000009710"
		lgOtherBktA     = "00000000-0000-4000-8000-000000009721"
		lgOtherBktB     = "00000000-0000-4000-8000-000000009722"
		lgOtherGoatA    = "00000000-0000-4000-8000-000000009723"
		lgOtherGoatB    = "00000000-0000-4000-8000-000000009724"
		lgOtherOperator = "00000000-0000-4000-8000-000000009725"
	)

	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='Anantapur Sheep', sex='male', management_stage='F2-Male'
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid`, repoTenant, repoPerShed)
	lgNameShedsWithoutTrailingDigits(t, ctx, pool)
	lgSeedSecondCampaign(t, ctx, pool)
	for _, c := range []struct{ campaignID, bucketID, weighDate string }{
		{lgCampaignThree, lgPerShedBkt3, "2026-08-08"},
		{lgCampaignFour, lgPerShedBkt4, "2026-08-12"},
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-08-03', '2026-08-16', $5::date, 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, c.campaignID, repoTenant, repoPark, repoOperator, c.weighDate)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, park_id, start_business_date, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Perth', 'per_shed_partition', $5::uuid, 0, $6::uuid, $7::date, 'completed')
ON CONFLICT (campaign_shed_id) DO NOTHING`, c.bucketID, c.campaignID, repoTenant, repoPerShed, repoOperator, repoPark, c.weighDate)
	}

	// THREE weighs inside the window: 20.0 → 21.0 → 22.8 over 1..8 Aug. The span is
	// first→latest (400 g/day) and the 4 animals count ONCE — never once per pair.
	seedLumpSumObservation(t, ctx, pool, repoShedScope, 4, 20.0, time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC))
	seedLumpSumObservation(t, ctx, pool, lgPerShedBkt2, 4, 21.0, time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC))
	seedLumpSumObservation(t, ctx, pool, lgPerShedBkt3, 4, 22.8, time.Date(2026, 8, 8, 6, 0, 0, 0, time.UTC))
	// CANCELED and later-past-window weighs are both invisible. Were either boundary
	// leaky, the span would become 20.0 → 60.0 and the count/band would jump.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status='canceled'
WHERE campaign_shed_id=$1::uuid`, lgPerShedBkt4)
	seedLumpSumObservation(t, ctx, pool, lgPerShedBkt4, 99, 60.0, time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC))
	seedLumpSumObservation(t, ctx, pool, lgPerShedBkt4, 4, 60.0, time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC))

	// Another PARK with its own homogeneous shed and a valid pair.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status, display_order) VALUES
  ($1::uuid, $3::uuid, 'park', 'OtherPark', 'active', 992),
  ($2::uuid, $3::uuid, 'shed', 'OtherPen', 'active', 993)
ON CONFLICT (location_id) DO NOTHING`, lgOtherPark, lgOtherShed, repoTenant)
	// Operators are single-park (trigger-enforced), so the other park gets its own
	// operator with its own park grant.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, lgOtherOperator, lgOtherPark)
	for _, c := range []struct{ campaignID, bucketID, weighDate string }{
		{lgOtherCampaign, lgOtherBktA, "2026-08-01"},
		{lgOtherCampTwo, lgOtherBktB, "2026-08-08"},
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-08-03', '2026-08-16', $5::date, 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, c.campaignID, repoTenant, lgOtherPark, lgOtherOperator, c.weighDate)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, park_id, start_business_date, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'OtherPen', 'per_shed_partition', $5::uuid, 0, $6::uuid, $7::date, 'completed')
ON CONFLICT (campaign_shed_id) DO NOTHING`, c.bucketID, c.campaignID, repoTenant, lgOtherShed, lgOtherOperator, lgOtherPark, c.weighDate)
	}
	for _, goat := range []struct{ id, display string }{
		{lgOtherGoatA, "G-991801"}, {lgOtherGoatB, "G-991802"},
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'Jamnapari', 'female', 'adult', 'alive', 'adult', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO NOTHING`, goat.id, repoTenant, goat.display, repoParty, lgOtherShed, lgOtherPark)
	}
	seedLumpSumObservation(t, ctx, pool, lgOtherBktA, 2, 30.0, time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC))
	seedLumpSumObservation(t, ctx, pool, lgOtherBktB, 2, 31.4, time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC))

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	// Scoped to the fixture park: 4 animals, once, in the >250 band; Jamnapari absent.
	out, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "")
	if err != nil {
		t.Fatalf("GetWeightDemographics(one park): %v", err)
	}
	var scopedFound bool
	for _, row := range out.GainThresholdsByBreed {
		switch row.Label {
		case "Anantapur Sheep":
			scopedFound = true
			if row.Animals != 4 || row.Above250 != 4 {
				t.Fatalf("three weighs must contribute the shed's animals ONCE at the first→latest span: %+v", row)
			}
		case "Jamnapari":
			t.Fatalf("another park's shed leaked into a park-scoped read: %+v", row)
		}
	}
	if !scopedFound {
		t.Fatal("Anantapur Sheep missing from park-scoped lump-sum gain bands")
	}

	// Both parks selected: Jamnapari joins with its own 2 animals (200 g/day ⇒ 180–200 band).
	both, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark, lgOtherPark}, from, to, "", "")
	if err != nil {
		t.Fatalf("GetWeightDemographics(both parks): %v", err)
	}
	var jamnapari bool
	for _, row := range both.GainThresholdsByBreed {
		if row.Label == "Jamnapari" {
			jamnapari = true
			if row.Animals != 2 || row.Band180To200 != 2 {
				t.Fatalf("other park's shed = %+v, want 2 animals in the 180–200 band (1400 g over 7 days)", row)
			}
		}
	}
	if !jamnapari {
		t.Fatal("selecting the other park must bring its lump-sum shed into the chart")
	}
}

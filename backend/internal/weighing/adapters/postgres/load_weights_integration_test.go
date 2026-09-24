package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Adversarial coverage for the load-wise growth rollup. Each test seeds real rows and asserts the
// number the chart renders, because each is a way the blend can be quietly wrong while still
// returning a plausible-looking bar.

const (
	loadCampaignTwo    = "00000000-0000-4000-8000-00000000a001"
	loadShedScopeTwo   = "00000000-0000-4000-8000-00000000a101"
	loadCampaignPartA  = "00000000-0000-4000-8000-00000000a002"
	loadCampaignPartB  = "00000000-0000-4000-8000-00000000a003"
	loadFilterShed     = "00000000-0000-4000-8000-00000000a201"
	loadFilterBucket   = "00000000-0000-4000-8000-00000000a202"
	loadFilterBought   = "00000000-0000-4000-8000-00000000a203"
	loadFilterHome     = "00000000-0000-4000-8000-00000000a204"
	loadFilterLoad     = "00000000-0000-4000-8000-00000000a205"
	loadFilterHomeShed = "00000000-0000-4000-8000-00000000a206"
	loadFilterHomeBkt  = "00000000-0000-4000-8000-00000000a207"
	loadPartAOld       = "00000000-0000-4000-8000-00000000a111"
	loadPartANew       = "00000000-0000-4000-8000-00000000a112"
	loadPartBOld       = "00000000-0000-4000-8000-00000000a121"
	loadPartBNew       = "00000000-0000-4000-8000-00000000a122"
)

// A second campaign, so one LOCATION can hold two weighs. weighing_shed_observations is UNIQUE on
// (tenant_id, campaign_shed_id) WHERE withdrawn_at IS NULL, so a shed's history necessarily lives
// across buckets — which is exactly why the gain CTEs key on location_id rather than bucket.
func seedLoadSecondCampaign(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-06', '2026-07-12', '2026-07-08', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, loadCampaignTwo, repoTenant, repoPark, repoOperator)
}

func seedLoadBucket(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, campaignID, locationID, category string) {
	t.Helper()
	seedLoadBucketPartition(t, ctx, pool, bucketID, campaignID, locationID, "", category)
}

func seedLoadBucketPartition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, campaignID, locationID, partitionLabel, category string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, partition_label, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'load-test', NULLIF($5, ''), $6, $7::uuid, 1)
ON CONFLICT (campaign_shed_id) DO UPDATE SET
  weighing_category = EXCLUDED.weighing_category,
  partition_label = EXCLUDED.partition_label`,
		bucketID, campaignID, repoTenant, locationID, partitionLabel, category, repoOperator)
}

func seedLoadLumpWeigh(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, campaignID, proofID string, avgKg float64, animals int, at time.Time) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid, $8::uuid, $9, $10::timestamptz)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		repoTenant, campaignID, bucketID, avgKg*float64(animals), avgKg, animals, proofID, repoOperator,
		fmt.Sprintf("loadweights:%s:%d", bucketID, at.UnixNano()), at)
}

func seedLoadTag(t *testing.T, ctx context.Context, pool *pgxpool.Pool, locationID, loadRef, owner string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_load_tags (tenant_id, location_id, load_ref, owner_name)
VALUES ($1::uuid, $2::uuid, $3, $4)
ON CONFLICT (tenant_id, location_id, load_ref) DO UPDATE SET owner_name = EXCLUDED.owner_name`,
		repoTenant, locationID, loadRef, owner)
}

func seedLoadIndividualWeigh(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, tag string, weightKg float64, at time.Time) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz)`,
		repoTenant, repoCampaign, bucketID, tag, weightKg, repoAnimalProof, repoOperator,
		fmt.Sprintf("loadweights:individual:%s:%s:%d", bucketID, tag, at.UnixNano()), at)
}

func seedLoadOriginGoats(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-A203', 'Load Filter Breed', 'male', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid),
       ($3::uuid, $2::uuid, 'G-A204', 'Load Filter Breed', 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE
SET sex=EXCLUDED.sex, current_location_id=EXCLUDED.current_location_id, park_id=EXCLUDED.park_id, shed_id=EXCLUDED.shed_id`,
		loadFilterBought, repoTenant, loadFilterHome, repoParty, loadFilterShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'load-purchased-kid', 'load-purchased-kid', 'global', true, 'active', now(), 'test'),
       ($1::uuid, $3::uuid, 'animal_identifier_1', 'load-home-kid',      'load-home-kid',      'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id=EXCLUDED.goat_id, identifier_value=EXCLUDED.identifier_value, status='active'`,
		repoTenant, loadFilterBought, loadFilterHome)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'loadweights:origin-filter-load')
ON CONFLICT (load_id) DO NOTHING`, loadFilterLoad, repoTenant, repoParty)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_load_goats (load_goat_id, tenant_id, load_id, goat_id)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid)
ON CONFLICT DO NOTHING`, repoTenant, loadFilterLoad, loadFilterBought)
}

func findLoad(t *testing.T, loads []domain.LoadGainBucket, loadRef string) domain.LoadGainBucket {
	t.Helper()
	for _, load := range loads {
		if load.LoadRef == loadRef {
			return load
		}
	}
	t.Fatalf("missing load %q in %#v", loadRef, loads)
	return domain.LoadGainBucket{}
}

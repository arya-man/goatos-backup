package readcachee2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The FCR fixture from growthdirector (fcr_integration_test.go), condensed: a whole-shed pen
// ("Lump 1", 25 goats, two rounds), a scanned pen reached through the legacy alias bridge
// ("Fcr Shed - Part 2", three tagged kids), seven feed days, one feed load and sale prices.
const (
	tenant    = "00000000-0000-4000-8000-000000000001"
	party     = "00000000-0000-4000-8000-000000001001"
	park      = "00000000-0000-4000-8000-000000003001"
	operator  = "00000000-0000-4000-8000-000000000301"
	shedLump  = "11111111-0000-4000-8000-000000000102"
	shedPhys  = "22222222-0000-4000-8000-000000000101"
	shedAlias = "22222222-0000-4000-8000-000000000102"
	shedOther = "22222222-0000-4000-8000-000000000103"
	campW1    = "11111111-0000-4000-8000-000000000201"
	campW2    = "11111111-0000-4000-8000-000000000202"
	bucketW1L = "22222222-0000-4000-8000-000000000301"
	bucketW2L = "11111111-0000-4000-8000-000000000305"
	bucketW1S = "22222222-0000-4000-8000-000000000302"
	bucketW2S = "22222222-0000-4000-8000-000000000303"
	proof     = "11111111-0000-4000-8000-000000000401"
	kidA      = "33333333-0000-4000-8000-000000000001"
	shedObsW2 = "44444444-0000-4000-8000-000000000002"
)

func execT(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

func day(d, hour int) time.Time { return time.Date(2026, 7, d, hour, 0, 0, 0, time.UTC) }

func seedFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, loc := range [][2]string{{shedLump, "Lump 1"}, {shedPhys, "Fcr Shed"}, {shedAlias, "Fcr Shed - Part 2"}, {shedOther, "Other Shed"}} {
		execT(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active', 600) ON CONFLICT (location_id) DO NOTHING`, loc[0], tenant, loc[1], park)
	}
	execT(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now()) ON CONFLICT DO NOTHING`, tenant, operator, park)
	execT(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $3::uuid, $4::uuid, '2026-07-06', '2026-07-12', '2026-07-06', 'completed', 100, $5::uuid, $5::uuid),
       ($2::uuid, $3::uuid, $4::uuid, '2026-07-13', '2026-07-19', '2026-07-13', 'published', 100, $5::uuid, $5::uuid)`,
		campW1, campW2, tenant, park, operator)
	execT(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $5::uuid, $7::uuid, $8::uuid, 'shed', 'Lump 1', 'per_shed_partition', $10::uuid, 0),
       ($2::uuid, $6::uuid, $7::uuid, $8::uuid, 'shed', 'Lump 1', 'per_shed_partition', $10::uuid, 0),
       ($3::uuid, $5::uuid, $7::uuid, $9::uuid, 'shed', 'Fcr Shed - Part 2', 'individual_animal', $10::uuid, 0),
       ($4::uuid, $6::uuid, $7::uuid, $9::uuid, 'shed', 'Fcr Shed - Part 2', 'individual_animal', $10::uuid, 0)`,
		bucketW1L, bucketW2L, bucketW1S, bucketW2S, campW1, campW2, tenant, shedLump, shedAlias, operator)
	execT(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, $2::uuid, 'local', 'readcachee2e/' || $1, 'video/mp4', 'completed', 'shed', $3::uuid, 'shed', $3::uuid, 'video', $4::uuid, now())`,
		proof, tenant, shedLump, operator)
	execT(t, ctx, pool, `
INSERT INTO weighing_shed_observations (shed_observation_id, tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, 500.0, 20.0, 25, $6::uuid, $7::uuid, 'e2e:lump:w1', $8::timestamptz),
       ($9::uuid,        $1::uuid, $4::uuid, $5::uuid, 517.5, 20.7, 25, $6::uuid, $7::uuid, 'e2e:lump:w2', $10::timestamptz)`,
		tenant, campW1, bucketW1L, campW2, bucketW2L, proof, operator, day(8, 10), shedObsW2, day(15, 10))
	execT(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id, species)
SELECT gen_random_uuid(), $1::uuid, 'G-88' || lpad(i::text, 4, '0'), 'Beetal', 'male', 'kid', 'alive', 'kid', $2::uuid, $3::uuid, $4::uuid, $3::uuid, 'goat'
FROM generate_series(1, 25) AS i`, tenant, party, shedLump, park)
	// Two kids of DIFFERENT breeds, so the pen's resident cohort (and its breed) changes when one
	// of them leaves the pen.
	for i, k := range []struct {
		id, tag, breed string
		first, last    float64
	}{{kidA, "FCR-A", "Sirohi", 10.0, 11.4}, {"33333333-0000-4000-8000-000000000002", "FCR-B", "Beetal", 10.0, 10.7}} {
		execT(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id, species)
VALUES ($1::uuid, $2::uuid, $3, $7, 'female', 'kid', 'alive', 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid, 'goat')`,
			k.id, tenant, fmt.Sprintf("G-99%04d", i), party, shedPhys, park, k.breed)
		execT(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, upper(btrim($3)), 'tenant', true, 'active', now(), 'test')`, tenant, k.id, k.tag)
		execT(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Part 2', 'Fcr Shed')`, tenant, k.id, shedPhys)
		for _, w := range []struct {
			camp, bucket string
			kg           float64
			at           time.Time
		}{{campW1, bucketW1S, k.first, day(8, 9)}, {campW2, bucketW2S, k.last, day(15, 9)}} {
			execT(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz, 'verified')`,
				tenant, w.camp, w.bucket, k.tag, w.kg, proof, operator, fmt.Sprintf("e2e:%s:%s", w.bucket, k.tag), w.at)
		}
	}
	for i := 0; i < 7; i++ {
		feedDay := time.Date(2026, 7, 8+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		issueID := fmt.Sprintf("55555555-0000-4000-8000-00000000000%d", i)
		execT(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow, state, issued_at, generation_input_fingerprint, idempotency_key, request_fingerprint, source_contract, source_contract_version, generated_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, 'normal', 'issued', now(), 'fp:' || $4, 'idem:e2e:' || $4, 'fp:' || $4, 'readcachee2e', '1', 'test')`,
			issueID, tenant, park, feedDay)
		execT(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, partition_label, shed_tag, breed, session_no, head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg, overdue_pending, row_seq, item_seq)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Lump 1', NULL, '', '', 1, 25, false, 'normal', 'Maize Crush', 10.0, 10.0, false, 0, 1),
       ($1::uuid, $2::uuid, $3::uuid, 'CBE', $5::uuid, 'Fcr Shed', 'Part 2', '', '', 1, 2, false, 'normal', 'Maize Crush', 1.0, 1.0, false, 1, 1)`,
			tenant, issueID, park, shedLump, shedPhys)
	}
	execT(t, ctx, pool, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label) SELECT $1::uuid, 'Maize Crush'
WHERE NOT EXISTS (SELECT 1 FROM feed_item_catalog WHERE tenant_id = $1::uuid AND feed_item_key = feed_config_norm('Maize Crush'))`, tenant)
	execT(t, ctx, pool, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg, total_cost, per_kg_cost, depletes_from)
VALUES ($1::uuid, $2::uuid, 'CBE', 'Maize Crush', 1, '2026-07-01', 1000, 20000, 20, '2026-07-01')`, tenant, park)
	execT(t, ctx, pool, `
INSERT INTO growth_sale_price_assumptions (tenant_id, species, price_per_kg_inr, effective_from, set_by)
VALUES ($1::uuid, 'goat', 425, '2026-07-01', 'maintainer'), ($1::uuid, 'sheep', 430, '2026-07-01', 'maintainer')
ON CONFLICT DO NOTHING`, tenant)
}

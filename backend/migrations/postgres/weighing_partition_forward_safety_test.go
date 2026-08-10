package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestWeighingPartitionForwardSafetyDoesNotRewriteActiveWholeShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant       = "f1450000-0000-4000-8000-000000000001"
		park         = "f1450000-0000-4000-8000-000000003001"
		castro       = "f1450000-0000-4000-8000-000000004001"
		castroOne    = "f1450000-0000-4000-8000-000000004002"
		castroReview = "f1450000-0000-4000-8000-000000004003"
		campaign     = "f1450000-0000-4000-8000-000000005001"
		campaignShed = "f1450000-0000-4000-8000-000000006001"
		reviewShed   = "f1450000-0000-4000-8000-000000006002"
		operator     = "f1450000-0000-4000-8000-000000009001"
		creator      = "f1450000-0000-4000-8000-000000009002"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Weighing Forward Safety', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, tenant)
	exec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, tenant, park)
exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'shed', 'Castro', 'active'),
       ($1::uuid, $3::uuid, $4::uuid, 'shed', 'Castro 1', 'active'),
       ($1::uuid, $5::uuid, $4::uuid, 'shed', 'Castro 1', 'review')`, tenant, castro, castroOne, park, castroReview)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, '1', '1', 'active', 'manual')`, tenant, castro)
	exec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`, tenant, operator, park)
	exec(`INSERT INTO weighing_campaigns
  (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, planned_cap_per_day, operator_user_id, created_by, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-08-01', '2026-08-07', '2026-08-03', 100, $4::uuid, $5::uuid, 'published')`,
		campaign, tenant, park, operator, creator)
exec(`INSERT INTO weighing_campaign_sheds
  (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, partition_label, weighing_category, operator_user_id, park_id, start_business_date, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Castro 1', NULL, 'individual_animal', $5::uuid, $6::uuid, '2026-08-03', 'pending')`,
		campaignShed, campaign, tenant, castroOne, operator, park)
	exec(`INSERT INTO weighing_campaign_sheds
  (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, partition_label, weighing_category, operator_user_id, park_id, start_business_date, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Castro 1', NULL, 'individual_animal', $5::uuid, $6::uuid, '2026-08-03', 'pending')`,
		reviewShed, campaign, tenant, castroReview, operator, park)

	raw, err := os.ReadFile("000145_weighing_partition_forward_safety.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("replay 000145: %v", err)
	}

	var locationID string
	var partitionLabel *string
	if err := pool.QueryRow(ctx, `
SELECT location_id::text, partition_label
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, tenant, campaignShed).Scan(&locationID, &partitionLabel); err != nil {
		t.Fatalf("query campaign shed: %v", err)
	}
	if locationID != castroOne {
		t.Fatalf("location_id = %q, want active whole shed %q to remain unchanged", locationID, castroOne)
	}
	if partitionLabel != nil {
		t.Fatalf("partition_label = %q, want NULL for active whole shed", *partitionLabel)
	}

	if err := pool.QueryRow(ctx, `
SELECT location_id::text, partition_label
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, tenant, reviewShed).Scan(&locationID, &partitionLabel); err != nil {
		t.Fatalf("query review campaign shed: %v", err)
	}
	if locationID != castroReview {
		t.Fatalf("location_id = %q, want review shed %q to remain unchanged", locationID, castroReview)
	}
	if partitionLabel != nil {
		t.Fatalf("partition_label = %q, want NULL for review shed", *partitionLabel)
	}
}

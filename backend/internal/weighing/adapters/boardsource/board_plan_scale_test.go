package boardsource

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// TestWeighingBoardQueryPlansUseIndexesAtScale is the at-scale plan proof for the weighing Work
// Board card read: listSQL and countSQL (whose board_state is workStateSQL / countWorkStateSQL,
// now with the feed & water removal-rework arm, removalReworkExistsSQL) and the pen drill-down
// subtasksSQL (now with the removal unit). It loads a two-year weighing history -- 1,000 past
// campaigns, each with one bucket, its work item, its removal task and pen evidence, and 500
// scans per bucket (500k weighing_observations) -- plus 400 scans on each of today's individual
// buckets, ANALYZEs, and EXPLAIN ANALYZEs the exact production statements. The scan table,
// weighing_observations, must never be seq-scanned.
func TestWeighingBoardQueryPlansUseIndexesAtScale(t *testing.T) {
	if os.Getenv("GOATOS_SCALE_CERT") == "" {
		t.Skip("scale certification gate — set GOATOS_SCALE_CERT=1 (make scale-cert)")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seed(t, ctx, pool)
	// Today's removals: one sent back on an open whole pen, one pending on the capturing pen.
	seedRemoval(t, ctx, pool, "00000000-0000-4000-8000-000000009401", "00000000-0000-4000-8000-000000009002", "00000000-0000-4000-8000-000000009102", "rework", "Water trough still full")
	seedRemoval(t, ctx, pool, "00000000-0000-4000-8000-000000009402", "00000000-0000-4000-8000-000000009001", "00000000-0000-4000-8000-000000009101", "", "")

	const proofID = "00000000-0000-4000-8000-000000009301"
	// History: one campaign + bucket + work item + removal per past day.
	exec(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
SELECT ('00000000-0000-4000-8001-' || lpad(i::text, 12, '0'))::uuid, $1::uuid, $2::uuid,
       DATE '2024-09-01' + i, DATE '2024-09-01' + i, DATE '2024-09-01' + i, 'published', 600, $3::uuid, $3::uuid
FROM generate_series(1, 1000) i`, bsTenant, bsPark, bsOperator)
	exec(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, status, partition_label)
SELECT ('00000000-0000-4000-8002-' || lpad(i::text, 12, '0'))::uuid, ('00000000-0000-4000-8001-' || lpad(i::text, 12, '0'))::uuid,
       $1::uuid, $2::uuid, 'shed', 'Godel 1', 'individual_animal', $3::uuid, 500, 'in_progress', 'Part ' || (100 + i)
FROM generate_series(1, 1000) i`, bsTenant, bsShed, bsOperator)
	exec(t, ctx, pool, `
INSERT INTO weighing_work_items (tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id, weighing_category, shed_label, shed_location_id, planned_business_date, due_business_date, work_state)
SELECT $1::uuid, ('00000000-0000-4000-8001-' || lpad(i::text, 12, '0'))::uuid, ('00000000-0000-4000-8002-' || lpad(i::text, 12, '0'))::uuid,
       $2::uuid, $3::uuid, 'individual_animal', 'Godel 1', $4::uuid, DATE '2024-09-01' + i, DATE '2024-09-01' + i, 'scheduled'
FROM generate_series(1, 1000) i`, bsTenant, bsPark, bsOperator, bsShed)
	exec(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, submitted_at, verification_status)
SELECT $1::uuid, ('00000000-0000-4000-8001-' || lpad(c::text, 12, '0'))::uuid, ('00000000-0000-4000-8002-' || lpad(c::text, 12, '0'))::uuid,
       'tag-' || c || '-' || a, 20 + (a % 30), $2::uuid, $3::uuid, 'scale-' || c || '-' || a, now(),
       CASE WHEN a % 50 = 0 THEN 'rework' ELSE 'verified' END
FROM generate_series(1, 1000) c CROSS JOIN generate_series(1, 500) a`, bsTenant, proofID, bsOperator)
	for _, b := range []struct{ campaign, bucket string }{
		{"00000000-0000-4000-8000-000000009001", "00000000-0000-4000-8000-000000009101"},
		{"00000000-0000-4000-8000-000000009011", "00000000-0000-4000-8000-000000009111"},
	} {
		exec(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key)
SELECT $1::uuid, $2::uuid, $3::uuid, 'today-' || $3 || '-' || a, 18, $4::uuid, $5::uuid, 'today-' || $3 || '-' || a
FROM generate_series(1, 400) a`, bsTenant, b.campaign, b.bucket, proofID, bsOperator)
	}
	exec(t, ctx, pool, `
INSERT INTO weighing_fasting_tasks (fasting_task_id, tenant_id, campaign_id, park_id, operator_user_id, planned_weigh_date, weigh_business_date, idempotency_key, created_by)
SELECT ('00000000-0000-4000-8003-' || lpad(i::text, 12, '0'))::uuid, $1::uuid, ('00000000-0000-4000-8001-' || lpad(i::text, 12, '0'))::uuid,
       $2::uuid, $3::uuid, DATE '2024-09-01' + i, DATE '2024-09-01' + i, 'scale-removal-' || i, $3::uuid
FROM generate_series(1, 1000) i`, bsTenant, bsPark, bsOperator)
	exec(t, ctx, pool, `
INSERT INTO weighing_fasting_shed_proofs (tenant_id, fasting_task_id, campaign_shed_id, shed_label, feed_proof_ref, water_proof_ref, status)
SELECT $1::uuid, ('00000000-0000-4000-8003-' || lpad(i::text, 12, '0'))::uuid, ('00000000-0000-4000-8002-' || lpad(i::text, 12, '0'))::uuid,
       'Godel 1', $2::uuid, $2::uuid, 'completed'
FROM generate_series(1, 1000) i`, bsTenant, proofID)
	for _, table := range []string{"weighing_observations", "weighing_shed_observations", "weighing_work_items", "weighing_campaign_sheds", "weighing_campaigns", "weighing_fasting_tasks", "weighing_fasting_shed_proofs", "locations", "workforce_members"} {
		exec(t, ctx, pool, `ANALYZE `+table)
	}
	var observations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM weighing_observations`).Scan(&observations); err != nil || observations < 500_000 {
		t.Fatalf("seeded %d weighing observations (err %v), want >= 500k", observations, err)
	}

	// weighing_observations is the large table (one row per scan). Work items, buckets and removal
	// rows are one per pen-day (~1k over two years here): a seq scan of those is the planner's honest
	// cheapest choice, not a regression, so only the scan table is asserted index-bound.
	big := []string{"weighing_observations"}
	src := New(pool, 5*time.Second)
	var rows []domain.Row
	list, err := src.ListStatement(query(""), &rows)
	if err != nil {
		t.Fatal(err)
	}
	pgtest.ExplainAnalyzeAtScale(t, ctx, pool, list.Query.SQL(), list.Query.Args()...).
		AssertNoSeqScan(t, "listSQL (workStateSQL) @500k observations", 200, big...)

	var counts map[domain.WorkState]int
	count, err := src.CountStatement(query(""), &counts)
	if err != nil {
		t.Fatal(err)
	}
	pgtest.ExplainAnalyzeAtScale(t, ctx, pool, count.Query.SQL(), count.Query.Args()...).
		AssertNoSeqScan(t, "countSQL (countWorkStateSQL) @500k observations", 200, big...)

	for _, bucket := range []string{"00000000-0000-4000-8000-000000009101", "00000000-0000-4000-8000-000000009102"} {
		pgtest.ExplainAnalyzeAtScale(t, ctx, pool, subtasksSQL, bsTenant, bsPark, bsDate, ids[bucket], -1, "", 51).
			AssertNoSeqScan(t, "subtasksSQL @500k observations, bucket "+bucket, 200, big...)
	}
}

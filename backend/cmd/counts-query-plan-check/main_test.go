package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestAnalyzePlanPassesWhenExpectedIndexAppears(t *testing.T) {
	raw := []byte(`[{"Plan":{"Node Type":"Nested Loop","Plans":[{"Node Type":"Index Scan","Relation Name":"count_base_anchors","Index Name":"count_base_anchors_hot_idx"},{"Node Type":"Index Only Scan","Relation Name":"shifting_events","Index Name":"shifting_events_projection_window_idx"}]}}]`)
	result := analyzePlan(planCheck{
		Name:            "projection_anchors_latest",
		ExpectedIndexes: []string{"count_base_anchors_hot_idx"},
		ProtectedTables: []string{"count_base_anchors"},
	}, raw)
	if !result.Passed {
		t.Fatalf("result=%+v, want pass", result)
	}
	if len(result.Indexes) != 2 || result.Indexes[0] != "count_base_anchors_hot_idx" {
		t.Fatalf("indexes=%v", result.Indexes)
	}
}

func TestAnalyzePlanBlocksProtectedSeqScan(t *testing.T) {
	raw := []byte(`[{"Plan":{"Node Type":"Seq Scan","Relation Name":"count_base_anchors"}}]`)
	result := analyzePlan(planCheck{
		Name:            "projection_anchors_latest",
		ExpectedIndexes: []string{"count_base_anchors_hot_idx"},
		ProtectedTables: []string{"count_base_anchors"},
	}, raw)
	if result.Passed || !strings.Contains(result.Failure, "sequential scan") {
		t.Fatalf("result=%+v, want protected seq-scan failure", result)
	}
}

func TestAnalyzePlanBlocksWhenNoExpectedIndexAppears(t *testing.T) {
	raw := []byte(`[{"Plan":{"Node Type":"Index Scan","Relation Name":"count_base_anchors","Index Name":"other_idx"}}]`)
	result := analyzePlan(planCheck{
		Name:            "projection_anchors_latest",
		ExpectedIndexes: []string{"count_base_anchors_hot_idx"},
		ProtectedTables: []string{"count_base_anchors"},
	}, raw)
	if result.Passed || !strings.Contains(result.Failure, "none of the expected indexes") {
		t.Fatalf("result=%+v, want missing expected index failure", result)
	}
}

func TestAnalyzePlanRequiresEveryIndexGroup(t *testing.T) {
	raw := []byte(`[{"Plan":{"Node Type":"Nested Loop","Plans":[{"Node Type":"Index Scan","Relation Name":"shifting_events","Index Name":"shifting_events_destination_park_window_idx"},{"Node Type":"Index Scan","Relation Name":"shifting_event_impacts","Index Name":"shifting_event_impacts_event_breed_idx"}]}}]`)
	result := analyzePlan(planCheck{
		Name:            "projection_movements_feed_target_date",
		ExpectedIndexes: []string{"shifting_events_destination_park_window_idx", "shifting_events_source_park_window_idx", "shifting_event_impacts_event_breed_idx"},
		RequiredIndexGroups: [][]string{
			{"shifting_events_destination_park_window_idx"},
			{"shifting_events_source_park_window_idx"},
			{"shifting_event_impacts_grain_unique", "shifting_event_impacts_event_breed_idx"},
		},
		ProtectedTables: []string{"shifting_events", "shifting_event_impacts"},
	}, raw)
	if result.Passed || !strings.Contains(result.Failure, "shifting_events_source_park_window_idx") {
		t.Fatalf("result=%+v, want missing required source-window group", result)
	}
}

func TestReadinessStatusKeepsCSG10PendingWhenChecksPass(t *testing.T) {
	status, blocker := readinessStatus([]checkResult{{Name: "projection_rows_feed_hot", Passed: true}})
	if status != "pending" {
		t.Fatalf("status=%s, want pending", status)
	}
	if !strings.Contains(blocker, "seeded local E2E") {
		t.Fatalf("blocker=%q", blocker)
	}
}

func TestReadinessStatusBlocksOnFailedCheck(t *testing.T) {
	status, blocker := readinessStatus([]checkResult{{Name: "projection_rows_feed_hot", Failure: "seq scan"}})
	if status != "blocked" {
		t.Fatalf("status=%s, want blocked", status)
	}
	if !strings.Contains(blocker, "projection_rows_feed_hot") {
		t.Fatalf("blocker=%q", blocker)
	}
}

func TestParseFlagsDefaultsTargetDateToTomorrow(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	cfg, err := parseFlags([]string{
		"-tenant-id", "00000000-0000-4000-8000-000000000001",
		"-park-id", "00000000-0000-4000-8000-000000000010",
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if got := cfg.TargetDate.Format(time.RFC3339); got != "2026-07-01T00:00:00+05:30" {
		t.Fatalf("target_date=%s", got)
	}
}

func TestQueryPlanChecksPassAgainstMigratedSchemaAndWriteCSG10(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	cfg := config{
		TenantID:   "00000000-0000-4000-8000-000000000001",
		ParkID:     "00000000-0000-4000-8000-000000000010",
		ShedID:     "00000000-0000-4000-8000-000000000011",
		BreedKey:   "beetal",
		TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		AsOf:       time.Date(2026, 6, 30, 13, 30, 0, 0, time.UTC),
	}
	seedQueryPlanFixtures(t, ctx, pool, cfg)

	results, err := runChecks(ctx, pool, buildChecks(cfg))
	if err != nil {
		t.Fatalf("runChecks: %v", err)
	}
	if len(results) != len(buildChecks(cfg)) {
		t.Fatalf("results=%d checks=%d", len(results), len(buildChecks(cfg)))
	}
	for _, result := range results {
		if !result.Passed || result.Failure != "" {
			t.Fatalf("query-plan result=%+v, want pass", result)
		}
	}
	movement := resultByName(t, results, "projection_movements_feed_target_date")
	assertIndex(t, movement, "shifting_events_destination_park_window_idx")
	assertIndex(t, movement, "shifting_events_source_park_window_idx")
	if !hasAnyIndex(movement, "shifting_event_impacts_grain_unique", "shifting_event_impacts_event_breed_idx") {
		t.Fatalf("movement indexes=%v, want impact join index", movement.Indexes)
	}
	status, blocker := readinessStatus(results)
	if status != "pending" || !strings.Contains(blocker, "seeded local E2E") {
		t.Fatalf("status=%s blocker=%q, want pending caveat", status, blocker)
	}
	if err := upsertCSG10Readiness(ctx, pool, cfg.TenantID, status, blocker); err != nil {
		t.Fatalf("upsertCSG10Readiness: %v", err)
	}
	var gotStatus, gotEvidence string
	if err := pool.QueryRow(ctx, `
SELECT status, evidence_ref
FROM counts_shifting_readiness_subgates
WHERE tenant_id=$1::uuid AND subgate_id='CSG10'`, cfg.TenantID).Scan(&gotStatus, &gotEvidence); err != nil {
		t.Fatalf("load CSG10 readiness: %v", err)
	}
	if gotStatus != "pending" || !strings.Contains(gotEvidence, "counts-query-plan-check") {
		t.Fatalf("CSG10 status=%s evidence=%s, want pending query-plan evidence", gotStatus, gotEvidence)
	}
}

func seedQueryPlanFixtures(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg config) {
	t.Helper()
	otherPark := "00000000-0000-4000-8000-000000000020"
	otherShed := "00000000-0000-4000-8000-000000000021"
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Mesha Query Plan Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, cfg.TenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES
  ($2::uuid, $1::uuid, 'park', 'QP-TARGET', 'Query Plan Target Park', 'active'),
  ($3::uuid, $1::uuid, 'park', 'QP-OTHER', 'Query Plan Other Park', 'active')
ON CONFLICT (location_id) DO NOTHING`,
		cfg.TenantID, cfg.ParkID, otherPark); err != nil {
		t.Fatalf("seed parks: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($3::uuid, $1::uuid, 'shed', 'QP-TARGET-SHED', 'Query Plan Target Shed', $2::uuid, 'active'),
  ($5::uuid, $1::uuid, 'shed', 'QP-OTHER-SHED', 'Query Plan Other Shed', $4::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`,
		cfg.TenantID, cfg.ParkID, cfg.ShedID, otherPark, otherShed); err != nil {
		t.Fatalf("seed sheds: %v", err)
	}
	seedQueryPlanMovements(t, ctx, pool, cfg, "dest-window", otherPark, otherShed, cfg.ParkID, cfg.ShedID, 1, 720)
	seedQueryPlanMovements(t, ctx, pool, cfg, "source-window", cfg.ParkID, cfg.ShedID, otherPark, otherShed, 721, 1440)
	seedQueryPlanProjectionRows(t, ctx, pool, cfg)
	if _, err := pool.Exec(ctx, `
ANALYZE shifting_events;
ANALYZE shifting_event_impacts;
ANALYZE count_base_anchors;
ANALYZE count_projection_snapshots;
ANALYZE count_projection_snapshot_rows;`); err != nil {
		t.Fatalf("analyze query-plan fixtures: %v", err)
	}
}

func seedQueryPlanProjectionRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg config) {
	t.Helper()
	baseAnchorID := "00000000-0000-4000-8000-000000000998"
	if _, err := pool.Exec(ctx, `
INSERT INTO count_base_anchors (
  base_count_anchor_id, tenant_id, park_id, shed_id, breed_key, breed_label,
  counted_at, head_count, source_system, source_ref, source_hash,
  discrepancy_state, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'beetal', 'Beetal',
  $5::timestamptz - interval '12 hours', 720, 'physical_base_count',
  'query-plan-base-anchor', 'query-plan-base-anchor-hash',
  'not_checked', 'query-plan-base-anchor-idem', 'query-plan-base-anchor-fp'
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		baseAnchorID, cfg.TenantID, cfg.ParkID, cfg.ShedID, cfg.TargetDate); err != nil {
		t.Fatalf("seed projection base anchor: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO count_projection_snapshots (
  count_projection_snapshot_id, tenant_id, horizon, park_id, target_date, as_of,
  projection_status, source_contract_version, source_hash, base_anchor_ids_hash,
  shifting_event_ids_hash, row_count, exception_count, generated_by
) VALUES (
  $1::uuid, $2::uuid, 'feed_target_date', $3::uuid, $4, $5::timestamptz,
  'blocked', 'counts-shifting-v1', 'query-plan-snapshot-source-hash',
  'query-plan-anchor-hash', 'query-plan-shifting-hash', 720, 0,
  'counts-query-plan-check-test'
)
ON CONFLICT (tenant_id, horizon, park_id, target_date, source_hash) DO NOTHING`,
		dummyPlanSnapshotID, cfg.TenantID, cfg.ParkID, cfg.TargetDate, cfg.AsOf); err != nil {
		t.Fatalf("seed projection snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO count_projection_snapshot_rows (
  tenant_id, count_projection_snapshot_id, park_id, shed_id, target_date,
  grain_key, base_count_anchor_id, included_shifting_event_ids_hash,
  breed_key, breed_label, stage_tag, age_class, sex,
  head_count, pregnant_count, lactating_count, warmup_count,
  ration_context_resolution_state, ration_context_ref, blocker_reason,
  source_row_hash
)
SELECT
  $1::uuid,
  $2::uuid,
  $3::uuid,
  $4::uuid,
  $5,
  'query-plan:' || g::text || ':beetal:pregnant',
  $6::uuid,
  'query-plan-shifting-hash',
  'beetal',
  'Beetal',
  'pregnant',
  'adult',
  'female',
  3,
  3,
  0,
  0,
  'blocked',
  'query-plan-ration-context',
  'query-plan pregnant destination shortage',
  'query-plan-row-hash:' || g::text
FROM generate_series(1, 720) AS g
ON CONFLICT (tenant_id, count_projection_snapshot_id, grain_key) DO NOTHING`,
		cfg.TenantID, dummyPlanSnapshotID, cfg.ParkID, cfg.ShedID, cfg.TargetDate, baseAnchorID); err != nil {
		t.Fatalf("seed projection rows: %v", err)
	}
}

func seedQueryPlanMovements(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg config, label, sourcePark, sourceShed, destinationPark, destinationShed string, first, last int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
WITH generated AS (
  SELECT
    g,
    gen_random_uuid() AS shifting_event_id,
    CASE WHEN g % 2 = 0 THEN 'authorized' ELSE 'applied' END AS event_status
  FROM generate_series($8::integer, $9::integer) AS g
),
inserted AS (
  INSERT INTO shifting_events (
    shifting_event_id, tenant_id, logical_shifting_event_key, priority, category,
    source_park_id, source_shed_id, destination_park_id, destination_shed_id,
    raised_at, effective_at, authorized_at, authorization_state, verification_state,
    event_status, source_system, source_ref, payload_hash, idempotency_key,
    request_fingerprint
  )
  SELECT
    shifting_event_id,
    $1::uuid,
    $2 || ':' || g::text,
    'high',
    'pregnancy',
    $3::uuid,
    $4::uuid,
    $5::uuid,
    $6::uuid,
    $7::timestamptz - interval '2 hours',
    $7::timestamptz + ((g % 96) * interval '15 minutes'),
    $7::timestamptz - interval '1 hour',
    'authorized',
    'verified',
    event_status,
    'goatos_canonical',
    'query-plan:' || $2 || ':' || g::text,
    'query-plan-payload:' || $2 || ':' || g::text,
    'query-plan-idem:' || $2 || ':' || g::text,
    'query-plan-fp:' || $2 || ':' || g::text
  FROM generated
  ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
  RETURNING tenant_id, shifting_event_id, logical_shifting_event_key
)
INSERT INTO shifting_event_impacts (
  tenant_id, shifting_event_id, grain_key, breed_key, breed_label, stage_tag,
  age_class, sex, head_count, pregnant_count, lactating_count, warmup_count,
  risk_flags, ration_context_resolution_state, ration_context_ref, blocker_reason
)
SELECT
  tenant_id,
  shifting_event_id,
  'beetal:pregnant',
  'beetal',
  'Beetal',
  'pregnant',
  'adult',
  'female',
  3,
  3,
  0,
  0,
  '{"pregnant":true}'::jsonb,
  'blocked',
  'query-plan-ration-context',
  'query-plan pregnant destination ration context unresolved'
FROM inserted
ON CONFLICT (tenant_id, shifting_event_id, grain_key) DO NOTHING`,
		cfg.TenantID, label, sourcePark, sourceShed, destinationPark, destinationShed, cfg.TargetDate, first, last); err != nil {
		t.Fatalf("seed %s movements: %v", label, err)
	}
}

func resultByName(t *testing.T, results []checkResult, name string) checkResult {
	t.Helper()
	for _, result := range results {
		if result.Name == name {
			return result
		}
	}
	t.Fatalf("missing result %s in %+v", name, results)
	return checkResult{}
}

func assertIndex(t *testing.T, result checkResult, index string) {
	t.Helper()
	if !hasAnyIndex(result, index) {
		t.Fatalf("%s indexes=%v, want %s", result.Name, result.Indexes, index)
	}
}

func hasAnyIndex(result checkResult, indexes ...string) bool {
	for _, got := range result.Indexes {
		for _, want := range indexes {
			if got == want {
				return true
			}
		}
	}
	return false
}

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestCalendarCanonicalReadPlanAtScale is the 50k-obligation upper-bound gate for the calendar
// canonical read (calendarCanonicalListSQL), required by step 4 of the operational-kernel-5k-50k
// scale envelope ADR: "run the aggregate path against the upper-bound row count in the
// obligation-volume table (up to ~500k obligation rows at 50k animals), not just the 5k list case."
// calendarCanonicalListSQL's canonical_selected read is a SINGLE query that both (a) pages the
// keyset list and (b) computes the drive_summary aggregate inline (obligation_drive_membership ->
// obligation_drive_vaccine_labels -> obligation_drive_shed_complete -> obligation_drive_animal_coverage
// -> obligation_drive_summary, all folded into park_drive_events) -- there is no separate top-level
// aggregate query to test in isolation, so this one EXPLAIN exercises both shapes at once.
//
// This complements the empty-database structural proof in canonical_read_test.go
// (TestCalendarCanonicalListKeysetPlanUsesIndex, which proves the composed CTE chain plans without
// error) by bulk-loading 50,000 canonical obligations into ONE shed/day so the drive_summary
// aggregation actually has scale-representative volume to fold over, then re-proving the same
// index-only-access invariant with real statistics (post-seed ANALYZE) instead of an empty table.
func TestCalendarCanonicalReadPlanAtScale(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const scaleCount = 50_000
	dueDayStart, dateFrom, dateToExclusive := seedCalendarScaleObligationFixture(t, ctx, pool, scaleCount)

	// Refresh planner statistics after the bulk load so the plan reflects the ~50k-row reality, not
	// the stale near-empty estimate (mirrors the mandatory post-seed ANALYZE contract in AGENTS.md).
	if _, err := pool.Exec(ctx, `ANALYZE obligation_instances, protocol_versions, protocol_definitions, protocol_rules, locations`); err != nil {
		t.Fatalf("analyze canonical tables at scale: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}

	// $1 tenant, $2 dateFrom, $3 dateToExclusive, $4 owner, $5 status, $6 park, $7 shed,
	// $8/$9 keyset cursor, $10 fetch limit, $11 tenantWide, $12/$13 park/shed scope.
	rows, err := tx.Query(ctx, "EXPLAIN (COSTS OFF) "+calendarCanonicalListSQL,
		testTenantID, dateFrom, dateToExclusive, "", "", "", "",
		nil, "", 21, true, []string{}, []string{})
	if err != nil {
		t.Fatalf("canonical list did not plan at 50k-obligation scale: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	plan := strings.Join(lines, "\n")
	t.Logf("calendarCanonicalListSQL @50k obligations, dueDayStart=%s:\n%s", dueDayStart.Format(time.RFC3339), plan)

	if strings.Contains(plan, "Seq Scan on obligation_instances") {
		t.Fatalf("calendarCanonicalListSQL sequentially scans obligation_instances at 50k-obligation scale "+
			"(compute-on-read regression; the list + drive_summary aggregate must stay index-bound):\n%s", plan)
	}
	if !strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Index Only Scan") && !strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("calendarCanonicalListSQL has no indexed access path at 50k-obligation scale:\n%s", plan)
	}
}

// seedCalendarScaleObligationFixture bulk-loads count distinct, unbatched canonical obligations
// (batch_id IS NULL, scope_type='shed') into ONE shed via a single set-based INSERT ... SELECT
// generate_series, all due on the SAME business day so canonical_read.go's catchup_drive_events /
// park_drive_events / obligation_drive_summary CTEs fold every row into ONE park-day drive group --
// exactly the shape whose aggregation this test proves stays index-bound at scale. Returns the
// business-day start the rows are due on, plus a [dateFrom, dateToExclusive) window that comfortably
// covers it for the canonical list call.
func seedCalendarScaleObligationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, count int) (dueDayStart, dateFrom, dateToExclusive time.Time) {
	t.Helper()
	seedCalendarLocations(t, ctx, pool, testParkA, testShedA)

	const (
		scaleProtocolID = "8a000000-0000-4000-8000-000000005001"
		scaleVersionID  = "8a000000-0000-4000-8000-000000005002"
		scaleRuleID     = "8a000000-0000-4000-8000-000000005003"
		scaleGoatID     = "8a000000-0000-4000-8000-000000005004"
	)
	dueDayStart = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	dateFrom = dueDayStart.Add(-24 * time.Hour)
	dateToExclusive = dueDayStart.Add(48 * time.Hour)

	// obligation_instances' goat-target FK trigger (000074_obligation_engine.sql) requires target_id
	// to reference a real row in goats when target_type='goat'. One dedicated scale goat backs every
	// bulk-seeded row below (mirrors vaccinationexecution's seedVaccinationExecutionLargeObligationFixture
	// single scale-goat pattern) -- due_at is distinct per row so the goat is reused without colliding
	// with any uniqueness constraint.
	seedCalendarGoat(t, ctx, pool, scaleGoatID)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.calendar.scale_plan_test', 'Scale Plan Test Vaccine', 'vaccination', 'active')
ON CONFLICT (protocol_id) DO UPDATE SET status = 'active', updated_at = now()`,
		scaleProtocolID, testTenantID); err != nil {
		t.Fatalf("seed scale protocol definition: %v", err)
	}
	// The protocol version must be seeded in 'draft' first: protocol_rules_require_draft_version_trg
	// (migration 000093) forbids inserting/updating protocol_rules against a published version
	// ("published config is immutable"). Insert the rule while draft, then publish the version --
	// mirroring the draft-then-publish seeding pattern used by repository_integration_test.go's
	// seedVaccinationObligation.
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy, published_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'tenant', NULL, 1,
  'Scale plan test published', 'draft', DATE '2026-01-01', DATE '2028-01-01',
  '{}'::jsonb, '{"required_proofs":["administration"]}'::jsonb, NULL
)
ON CONFLICT (protocol_version_id) DO UPDATE
SET status = CASE WHEN protocol_versions.status = 'published' THEN protocol_versions.status ELSE 'draft' END`,
		scaleVersionID, testTenantID, scaleProtocolID); err != nil {
		t.Fatalf("seed scale protocol version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  proof_policy, sort_order
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'SCALE-PRIMARY', 1, 'calendar',
  0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{"required_proofs":["administration"]}'::jsonb, 10
)
ON CONFLICT (tenant_id, rule_id) DO NOTHING`,
		scaleRuleID, testTenantID, scaleVersionID); err != nil {
		t.Fatalf("seed scale protocol rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE protocol_versions
SET status = 'published',
    published_at = COALESCE(published_at, now())
WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid`,
		testTenantID, scaleVersionID); err != nil {
		t.Fatalf("publish scale protocol version: %v", err)
	}

	// Every row is due somewhere inside dueDayStart's business day, scoped directly to testShedA
	// (scope_type='shed'), batch_id NULL -- the catchup_drive_events shape, one row per
	// (park_id, due_day) after grouping. sequence/idempotency_key stay distinct per row.
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id,
  target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence
)
SELECT
  gen_random_uuid(),
  $1::uuid,
  $2::uuid,
  $3::uuid,
  'goat',
  $7::uuid,
  'shed',
  $4::uuid,
  $5::timestamptz + (g || ' seconds')::interval,
  'scheduled',
  'calendar-scale-plan-' || g::text,
  100000 + g
FROM generate_series(1, $6::int) AS g`,
		testTenantID, scaleVersionID, scaleRuleID, testShedA, dueDayStart, count, scaleGoatID); err != nil {
		t.Fatalf("bulk-seed %d canonical obligations at scale: %v", count, err)
	}
	return dueDayStart, dateFrom, dateToExclusive
}

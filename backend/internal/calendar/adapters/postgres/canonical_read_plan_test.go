package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestCalendarCanonicalReadPlanAtScale is the 5k-to-50k-animal upper-bound gate for the calendar
// canonical read (calendarCanonicalListSQL), required by step 4 of the operational-kernel-5k-50k
// scale envelope ADR: "run the aggregate path against the upper-bound row count in the
// obligation-volume table (up to ~500k obligation rows at 50k animals), not just the 5k list case."
// calendarCanonicalListSQL's canonical_selected read is a SINGLE query that both (a) pages the
// keyset list and (b) computes the drive_summary aggregate inline (obligation_drive_membership ->
// obligation_drive_vaccine_labels -> obligation_drive_shed_complete -> obligation_drive_animal_coverage
// -> obligation_drive_summary, all folded into park_drive_events) -- there is no separate top-level
// aggregate query to test in isolation, so this one EXPLAIN ANALYZE exercises both shapes at once.
//
// This complements the empty-database structural proof in canonical_read_test.go
// (TestCalendarCanonicalListKeysetPlanUsesIndex, which proves the composed CTE chain plans without
// error) by bulk-loading ~500k canonical obligations (realistic for 50k animals at ~10 obligations/animal)
// with distribution across multiple sheds and dates so the drive_summary aggregation actually has
// scale-representative volume and complexity to fold over, then proving the index-only-access
// invariant with real statistics (post-seed ANALYZE) and ACTUAL EXECUTION (EXPLAIN ANALYZE) instead
// of zero-row estimates or just explaining the plan.
func TestCalendarCanonicalReadPlanAtScale(t *testing.T) {
	// 50k-animal / ~500k-obligation certification gate: too heavy for every `go test ./...` (per the
	// operational-kernel-5k-50k ADR + local-E2E pipeline, the heavy scale run is a pre-push/scheduled
	// local certification gate, not an inner-loop check). Run via `make scale-cert` / GOATOS_SCALE_CERT=1.
	if os.Getenv("GOATOS_SCALE_CERT") == "" {
		t.Skip("scale certification gate — set GOATOS_SCALE_CERT=1 (make scale-cert); excluded from inner-loop ci-local")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Seed a realistic 500k-obligation dataset: 50k obligations per day across 10 days,
	// distributed across multiple sheds (not all in one shed/day) to mirror real herd complexity.
	const totalObligations = 500_000
	const daysSpan = 10
	const obligationsPerDay = totalObligations / daysSpan
	dueDayStart, dateFrom, dateToExclusive := seedCalendarScaleObligationRealistic(t, ctx, pool, totalObligations, daysSpan, obligationsPerDay)

	// Refresh planner statistics after the bulk load so the plan reflects the ~500k-row reality, not
	// the stale near-empty estimate (mirrors the mandatory post-seed ANALYZE contract in AGENTS.md).
	if _, err := pool.Exec(ctx, `ANALYZE obligation_instances, protocol_versions, protocol_definitions, protocol_rules, locations`); err != nil {
		t.Fatalf("analyze canonical tables at 500k scale: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Do NOT force enable_seqscan=off: the point is to prove the planner CHOOSES indexes with real
	// statistics, not to force a plan regardless of cost. The planner's honest choice is the proof.

	// $1 tenant, $2 dateFrom, $3 dateToExclusive, $4 owner, $5 status, $6 park, $7 shed,
	// $8/$9 keyset cursor, $10 fetch limit, $11 tenantWide, $12/$13 park/shed scope.
	// Use EXPLAIN (ANALYZE, BUFFERS) to measure actual execution time and buffer usage, not just cost.
	rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+calendarCanonicalListSQL,
		testTenantID, dateFrom, dateToExclusive, "", "", "", "",
		nil, "", 21, true, []string{}, []string{})
	if err != nil {
		t.Fatalf("canonical list did not plan/execute at 500k-obligation scale: %v", err)
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
	t.Logf("calendarCanonicalListSQL @500k obligations, dueDayStart=%s:\n%s", dueDayStart.Format(time.RFC3339), plan)

	// Assert the honest planner (not forced) chooses index scans, not seq scans.
	if strings.Contains(plan, "Seq Scan on obligation_instances") {
		t.Fatalf("calendarCanonicalListSQL sequentially scans obligation_instances at 500k-obligation scale "+
			"(compute-on-read regression; the list + drive_summary aggregate must stay index-bound). "+
			"This is NOT a plan-cost issue but a REAL execution problem: the planner cannot justify an index scan:\n%s", plan)
	}
	if !strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Index Only Scan") && !strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("calendarCanonicalListSQL has no indexed access path at 500k-obligation scale "+
			"(the planner could not find an index-based plan even with real statistics):\n%s", plan)
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

// seedCalendarScaleObligationRealistic bulk-loads count distinct canonical obligations distributed
// across multiple sheds and multiple days (realistic for the 5k-50k envelope), with real statistics
// for the planner to work with. Returns the base due-day start, plus a [dateFrom, dateToExclusive)
// window that covers the full distribution for the canonical list call.
func seedCalendarScaleObligationRealistic(t *testing.T, ctx context.Context, pool *pgxpool.Pool, totalCount, daysSpan, obligationsPerDay int) (dueDayStart, dateFrom, dateToExclusive time.Time) {
	t.Helper()
	// Create multiple sheds for realistic distribution (the drive_summary aggregation folds across sheds).
	seedCalendarLocations(t, ctx, pool, testParkA, testShedA)
	shedB := "8a000000-0000-4000-8000-0000000066bb"
	shedC := "8a000000-0000-4000-8000-0000000066cc"
	seedCalendarLocationWithID(t, ctx, pool, testParkA, shedB, "Scale Shed B")
	seedCalendarLocationWithID(t, ctx, pool, testParkA, shedC, "Scale Shed C")

	const (
		scaleProtocolID = "8a000000-0000-4000-8000-000000005001"
		scaleVersionID  = "8a000000-0000-4000-8000-000000005002"
		scaleRuleID     = "8a000000-0000-4000-8000-000000005003"
		scaleGoatID     = "8a000000-0000-4000-8000-000000005004"
	)
	dueDayStart = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	dateFrom = dueDayStart.Add(-24 * time.Hour)
	dateToExclusive = dueDayStart.Add(time.Duration(daysSpan*24) * time.Hour)

	seedCalendarGoat(t, ctx, pool, scaleGoatID)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.calendar.scale_realistic', 'Scale Realistic Test Vaccine', 'vaccination', 'active')
ON CONFLICT (protocol_id) DO UPDATE SET status = 'active', updated_at = now()`,
		scaleProtocolID, testTenantID); err != nil {
		t.Fatalf("seed scale protocol definition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy, published_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'tenant', NULL, 1,
  'Scale realistic test published', 'draft', DATE '2026-01-01', DATE '2028-01-01',
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
  $1::uuid, $2::uuid, $3::uuid, 'SCALE-REALISTIC', 1, 'calendar',
  0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{"required_proofs":["administration"]}'::jsonb, 10
)
ON CONFLICT (tenant_id, rule_id) DO NOTHING`,
		scaleRuleID, testTenantID, scaleVersionID); err != nil {
		t.Fatalf("seed scale protocol rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE protocol_versions
SET status = 'published', published_at = COALESCE(published_at, now())
WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid`,
		testTenantID, scaleVersionID); err != nil {
		t.Fatalf("publish scale protocol version: %v", err)
	}

	// Bulk-load all obligations in ONE server-side statement (generate_series) — 500k per-row
	// round-trips is prohibitively slow. Realistic grain: ~10 obligations per animal, so target_id
	// (the animal) repeats every 10 rows → ~totalCount/10 distinct animals for the distinct-animal
	// coverage aggregate to fold over. The dup guard UNIQUE(tenant, version, rule, target_type,
	// target_id, due_at) stays satisfied because each of an animal's 10 obligations lands on a
	// distinct second within its due day. Days spread via g/obligationsPerDay; sheds rotate 3-ways.
	// target_id is trigger-validated against goats(goat_id) per tenant, so bulk-seed the ~totalCount/10
	// distinct animals first, matching the deterministic goat_id scheme used by the obligation insert.
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex)
SELECT
  ('8a000000-0000-4000-9000-' || lpad(a::text, 12, '0'))::uuid,
  $1::uuid, 'alive', 'goat', $2::uuid, 'female'
FROM generate_series(0, ($3 / 10) - 1) AS a
ON CONFLICT (goat_id) DO NOTHING`,
		testTenantID, testCustodianID, totalCount); err != nil {
		t.Fatalf("bulk-seed %d goats: %v", totalCount/10, err)
	}

	shedArrayLiteral := fmt.Sprintf("ARRAY['%s'::uuid, '%s'::uuid, '%s'::uuid]", testShedA, shedB, shedC)
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
  ('8a000000-0000-4000-9000-' || lpad((g / 10)::text, 12, '0'))::uuid,
  'shed',
  (`+shedArrayLiteral+`)[1 + (g % 3)],
  $4::timestamptz + make_interval(days => (g / $5)::int) + make_interval(secs => (g % 86400)),
  'scheduled',
  'calendar-realistic-' || g::text,
  g
FROM generate_series(0, $6 - 1) AS g`,
		testTenantID, scaleVersionID, scaleRuleID, dueDayStart, obligationsPerDay, totalCount); err != nil {
		t.Fatalf("bulk-seed %d obligations: %v", totalCount, err)
	}

	t.Logf("seeded %d obligations (~%d distinct animals) across 3 sheds over %d days for realistic scale test", totalCount, totalCount/10, daysSpan)
	return dueDayStart, dateFrom, dateToExclusive
}

// seedCalendarLocationWithID is a helper to seed a location with an explicit ID (for multiple shed fixtures).
func seedCalendarLocationWithID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parkID, shedID, shedName string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id,
  country, state_region, timezone, status, updated_at)
VALUES ($1::uuid, $2::uuid, 'shed', 'SCALE_' || $4, $4, $3::uuid, 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now())
ON CONFLICT (location_id) DO UPDATE SET status = 'active', updated_at = now()`,
		shedID, testTenantID, parkID, shedName); err != nil {
		t.Fatalf("seed scale shed location %s: %v", shedName, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, usable_for_sop)
VALUES ($1::uuid, $2::uuid, true, true)
ON CONFLICT (location_id) DO UPDATE SET usable_for_vaccination = true, updated_at = now()`,
		testTenantID, shedID); err != nil {
		t.Fatalf("seed scale shed attrs %s: %v", shedName, err)
	}
}

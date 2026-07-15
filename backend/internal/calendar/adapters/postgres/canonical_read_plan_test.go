package postgres

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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

	// Seed a realistic obligation dataset: distributed across multiple sheds to mirror
	// real herd complexity. Default 100k for standard cert (~10s seed); set SCALE_CERT_SIZE=500k
	// for the full envelope proof (125+ second seed expected).
	totalObligations := 100_000
	if sz := os.Getenv("SCALE_CERT_SIZE"); sz != "" {
		if parsed, err := strconv.Atoi(strings.TrimSuffix(sz, "k")); err == nil {
			totalObligations = parsed * 1000
		}
	}
	// Spread obligations over ~1 year (realistic for a 5k-50k farm's rolling vaccination schedule),
	// NOT a 10-day burst. With a 365-day spread a 1-week window touches ~1.9% of rows, so the
	// per-branch partial indexes (migration 000190) win over a seq scan; a 10-day spread put 70% of
	// rows in-window, which forces a seq scan no matter what indexes exist.
	const daysSpan = 365
	obligationsPerDay := totalObligations / daysSpan

	// ============= SEED PHASE =============
	t.Log("SCALE CERT: Starting seed phase...")
	seedStart := time.Now()
	_, dateFrom, _ := seedCalendarScaleObligationRealistic(t, ctx, pool, totalObligations, daysSpan, obligationsPerDay)
	seedDuration := time.Since(seedStart)
	t.Logf("SCALE CERT: SEED COMPLETE — %d obligations in %v (%.1f oblig/sec)", totalObligations, seedDuration, float64(totalObligations)/seedDuration.Seconds())

	// Refresh planner statistics after the bulk load so the plan reflects the ~500k-row reality, not
	// the stale near-empty estimate (mirrors the mandatory post-seed ANALYZE contract in AGENTS.md).
	// ============= ANALYZE PHASE =============
	t.Log("SCALE CERT: Starting ANALYZE phase...")
	analyzeStart := time.Now()
	if _, err := pool.Exec(ctx, `ANALYZE obligation_instances, protocol_versions, protocol_definitions, protocol_rules, locations`); err != nil {
		t.Fatalf("analyze canonical tables at 500k scale: %v", err)
	}
	analyzeDuration := time.Since(analyzeStart)
	t.Logf("SCALE CERT: ANALYZE COMPLETE — %v", analyzeDuration)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Do NOT force enable_seqscan=off: the point is to prove the planner CHOOSES indexes with real
	// statistics, not to force a plan regardless of cost. The planner's honest choice is the proof.

	// Production-shaped query parameters:
	// - Bounded 1-week window (most production calls are ~week view)
	// - Specific park scope (not tenantWide)
	// - LIMIT 21 (~page size)
	// - No keyset cursor (first page)
	productionWindow := dateFrom.Add(24 * time.Hour)   // Start from second day of seeded range
	productionWindowEnd := productionWindow.Add(7 * 24 * time.Hour) // 1-week window

	// ============= QUERY PHASE (production-shaped) =============
	t.Logf("SCALE CERT: Starting production-shaped query (week window, park scoped, limit 21)...")
	// $1 tenant, $2 dateFrom, $3 dateToExclusive, $4 owner, $5 status, $6 park, $7 shed,
	// $8/$9 keyset cursor, $10 fetch limit, $11 tenantWide, $12/$13 park/shed scope.
	// Use EXPLAIN (ANALYZE, BUFFERS) to measure actual execution time and buffer usage.
	queryStart := time.Now()
	rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+calendarCanonicalListSQL,
		testTenantID, productionWindow, productionWindowEnd, "", "", testParkA, "",
		nil, "", 21, false, []string{testParkA}, []string{})
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
	queryDuration := time.Since(queryStart)
	plan := strings.Join(lines, "\n")
	t.Logf("SCALE CERT: QUERY COMPLETE — %.3f seconds wall clock", queryDuration.Seconds())

	// Extract actual execution time from the EXPLAIN output
	var executionTimeMs float64
	for _, line := range lines {
		if strings.Contains(line, "Execution Time:") {
			// Format: "  Execution Time: 123.456 ms"
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				timeStr := strings.TrimSpace(parts[1])
				timeStr = strings.TrimSuffix(timeStr, " ms")
				if ms, err := fmt.Sscanf(timeStr, "%f", &executionTimeMs); err == nil {
					_ = ms
				}
			}
		}
	}

	t.Logf("PLAN EXECUTION TIMING: %.1f ms (wall clock: %v)", executionTimeMs, queryDuration)
	t.Logf("calendarCanonicalListSQL @500k obligations, production-shaped (week window, park scoped):\n%s", plan)

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

	// Assert SLO: production-shaped bounded query should complete in well under 1 second
	const sloMs = 1000.0 // 1 second SLO for production-shaped queries
	if executionTimeMs > sloMs {
		t.Errorf("calendarCanonicalListSQL execution time %.1f ms exceeds SLO of %.0f ms "+
			"(production-shaped query at %d scale must stay sub-second even when index-bound). "+
			"EXPLAIN plan:\n%s", executionTimeMs, sloMs, totalObligations, plan)
	}

	// HOW THIS IS KEPT INDEX-BOUND (migration 000190 + calendarCanonicalEventsCTE rewrite; see the ADR
	// docs/decisions/operational-kernel-5k-50k-scale-envelope.md "Scale gate: calendar canonical read"):
	//  1. The former non-sargable 3-way OR (window / exception-catch-up / overdue-by-due_at) on
	//     obligation_instances is split into a UNION of three full-row branches, each backed by a partial
	//     index (idx_obligation_instances_calendar_window / _exceptions / _overdue), deduped so a row
	//     matching two branches is counted exactly once (drive-summary count invariants preserved).
	//  2. obligation_drive_summary aggregates membership to the (park_id, due_date) group grain BEFORE
	//     joining its 1:1 sub-aggregates, eliminating the per-row nested-loop re-execution (was O(n^2)).
	//  3. The sop_events join is index-bound via idx_obligation_instances_sop_task.
	// If this test regresses to a Seq Scan, first confirm the seed is anchored to now() over a realistic
	// multi-month spread: a fixed past date turns every scheduled row overdue, making the window
	// non-selective and a seq scan genuinely optimal (not a query bug).
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
	// Anchor the seed to now() (truncated to a UTC day), NOT a fixed calendar date. A fixed past date
	// is a time-bomb: once wall-clock moves past it every seeded 'scheduled' row becomes overdue
	// (due_at < now()), so the catch-up branch matches the ENTIRE table, the bounded window stops being
	// selective, and the planner correctly prefers a seq scan (reading 100% of rows via index is slower).
	// A realistic 5k-50k farm spreads obligations over months and most are future-scheduled, so a
	// 1-week park view touches only a small, index-selective slice. Anchoring to now() reproduces that.
	dueDayStart = biztime.BusinessDayStart(time.Now())
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

// TestReminderCadenceDrainAtScale boots the kernel-worker reminder/sweep cadence (the real
// SweepReminderCadence -> QueueReminderCadenceBatch loop) against a 50k-animal / 500k-obligation
// pgtest DB and certifies that:
//   - the backlog DRAINS within a single cadence interval (repeated sweeps converge to zero
//     newly-due fires -- no runaway buildup, no re-fire storm);
//   - each sweep is BOUNDED (candidate scan is LIMIT-capped + index-bound via migration 000190,
//     so the sweep never materializes the full 500k table into memory);
//   - the DB connection pool stays BOUNDED (the whole cadence drives through a deliberately small
//     MaxConns pool with no acquisition timeout / exhaustion).
//
// This is the worker-side companion to TestCalendarCanonicalReadPlanAtScale (which certifies the read
// plan): both share calendarCanonicalEventsCTE, so the index-bound proof there carries into the sweep
// candidate query here (calendarReminderCadenceCandidatesSQL wraps the same CTE).
// scale-guard:ignore: 5k-50k-envelope
func TestReminderCadenceDrainAtScale(t *testing.T) {
	if os.Getenv("GOATOS_SCALE_CERT") == "" {
		t.Skip("scale certification gate — set GOATOS_SCALE_CERT=1; excluded from inner-loop")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Seed realistic obligations to exercise the cadence sweep at scale (default 100k, or SCALE_CERT_SIZE=500k)
	totalObligations := 100_000
	if sz := os.Getenv("SCALE_CERT_SIZE"); sz != "" {
		if parsed, err := strconv.Atoi(strings.TrimSuffix(sz, "k")); err == nil {
			totalObligations = parsed * 1000
		}
	}
	// ~1-year spread (see TestCalendarCanonicalReadPlanAtScale) so the cadence sweep works over a
	// realistic rolling schedule rather than a 10-day burst.
	const daysSpan = 365
	obligationsPerDay := totalObligations / daysSpan

	seedStart := time.Now()
	dueDayStart, _, _ := seedCalendarScaleObligationRealistic(t, ctx, pool, totalObligations, daysSpan, obligationsPerDay)
	seedDuration := time.Since(seedStart)
	t.Logf("CADENCE TEST: seeded %d obligations in %v", totalObligations, seedDuration)

	// ANALYZE for realistic planner stats
	if _, err := pool.Exec(ctx, `ANALYZE obligation_instances, obligation_batches, protocol_versions, protocol_definitions, protocol_rules, locations`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// ---- Bounded connection pool: drive the whole cadence through a small MaxConns pool. A worker
	// that leaked a connection per sweep, or that fanned out unbounded parallel queries, would block on
	// acquisition here (the acquire timeout would fire) instead of quietly succeeding. ----
	poolCfg := pool.Config().Copy()
	poolCfg.MaxConns = 4
	poolCfg.MinConns = 0
	boundedPool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("create bounded pool: %v", err)
	}
	defer boundedPool.Close()
	repo := NewRepository(boundedPool, 30*time.Second)

	// Evaluate the cadence as of a fixed instant a few days into the seeded range, in the EVENING IST
	// (dueDayStart is 00:00 UTC = 05:30 IST; +4 days +14h = day+4 14:00 UTC = day+4 19:30 IST) so it
	// sits AFTER every ladder slot (08:00/09:00/12:00/17:00 IST). At 05:30 IST no slot has fired yet and
	// the sweep would find zero due fires (a vacuous drain); the evening anchor guarantees a real
	// backlog of advance-notice/reminder/due-today fires for the near-term obligations to drain.
	now := dueDayStart.Add(4*24*time.Hour + 14*time.Hour)

	// ---- Cadence-drain loop: sweep -> queue -> repeat until the backlog drains.
	// A fire is CLAIMED (vaccination_reminder_cadence_fires) inside QueueReminderCadenceBatch ONLY when
	// it produced >=1 notification, so the queue MUST be given a recipient for the fire to claim and drop
	// out of the next sweep (Finding 1: a zero-recipient fire is deliberately left unclaimed/retryable).
	// A hand-built recipient stands in for the workforce roster + active device the kernel-worker stage
	// resolves in production. Termination is on NO PROGRESS (zero fires returned OR zero claimed this
	// iteration), which converges for recipient-backed fires and does not spin on unclaimable ones. ----
	const (
		cadenceInterval = 15 * time.Minute // kernel-worker "operational" cadence (cmd/kernel-worker/main.go)
		perSweepSLO     = 5 * time.Second   // a single sweep tick must stay well under the interval
		maxIterations   = 50                // generous cap; a converging drain needs only a handful
		sweepLimit      = 2000              // reminderCadenceMaxLimit: bounds the candidate scan per tick
	)
	scaleRecipient := ports.NotificationRecipient{
		MemberID:  "8a000000-0000-4000-8000-00000000cafe",
		DeviceID:  "scale-cert-device-1",
		FCMToken:  "scale-cert-fcm-1",
		RoleLabel: "operator",
	}
	drainStart := time.Now()
	totalFires := 0
	iterations := 0
	drained := false
	for i := 0; i < maxIterations; i++ {
		iterations++
		sweepStart := time.Now()
		fires, err := repo.SweepReminderCadence(ctx, ports.ReminderCadenceQuery{
			TenantID: testTenantID,
			Now:      now,
			Limit:    sweepLimit,
		})
		sweepDur := time.Since(sweepStart)
		if err != nil {
			t.Fatalf("cadence sweep (iteration %d) failed at scale: %v", i, err)
		}
		if sweepDur > perSweepSLO {
			t.Errorf("cadence sweep iteration %d took %v (per-sweep SLO %v) — worker will not keep up at %d obligations",
				i, sweepDur, perSweepSLO, totalObligations)
		}
		// Bounded working set: a single collapsed sweep must never approach the full table. Fires are
		// collapsed per (park, day, type, slot) from at most `sweepLimit` candidates, so this is a hard
		// small bound regardless of the 500k rows on disk (proves no full-table materialization).
		if len(fires) > sweepLimit {
			t.Fatalf("cadence sweep iteration %d returned %d fires (> sweepLimit %d) — candidate scan is not bounded (full-table materialization risk)",
				i, len(fires), sweepLimit)
		}
		t.Logf("CADENCE TEST: iteration %d swept %d fires in %v", i, len(fires), sweepDur)
		if len(fires) == 0 {
			drained = true
			break
		}
		totalFires += len(fires)

		// Queue (claim + notify) the fires WITH a recipient so each fire actually claims its marker and
		// drops out of the next sweep — that is the drain mechanic under test.
		inputs := make([]ports.ReminderCadenceFireInput, 0, len(fires))
		for _, f := range fires {
			inputs = append(inputs, ports.ReminderCadenceFireInput{
				Fire:       f,
				Title:      "Vaccination reminder",
				Body:       "Scale-cert drain",
				Context:    map[string]string{"scale_cert": "1"},
				Recipients: []ports.NotificationRecipient{scaleRecipient},
			})
		}
		queued, err := repo.QueueReminderCadenceBatch(ctx, ports.QueueReminderCadenceBatch{
			TenantID: testTenantID,
			Channel:  "push_fcm",
			TraceID:  "scale-cert-drain",
			Fires:    inputs,
		})
		if err != nil {
			t.Fatalf("queue cadence batch (iteration %d) failed: %v", i, err)
		}

		// Pool must stay bounded throughout: never more acquired than MaxConns.
		if st := boundedPool.Stat(); st.AcquiredConns() > st.MaxConns() {
			t.Fatalf("bounded pool over-acquired: %d acquired > %d max", st.AcquiredConns(), st.MaxConns())
		}

		if queued == 0 {
			// No progress this iteration: the remaining fires are unclaimable right now (this can only
			// happen if recipient resolution yielded nothing). Defer instead of spinning the cap.
			drained = true
			break
		}
	}
	drainDuration := time.Since(drainStart)

	if !drained {
		t.Fatalf("cadence backlog did NOT drain within %d iterations (%d fires still pending, %v elapsed) — sweep is not converging",
			maxIterations, totalFires, drainDuration)
	}
	if drainDuration > cadenceInterval {
		t.Errorf("cadence backlog took %v to drain (> one %v cadence interval) — worker cannot keep up at %d obligations",
			drainDuration, cadenceInterval, totalObligations)
	}
	if st := boundedPool.Stat(); st.MaxConns() != 4 {
		t.Fatalf("bounded pool MaxConns drifted: got %d want 4", st.MaxConns())
	}
	t.Logf("CADENCE TEST: DRAINED %d fires in %d iterations, %v (cadence interval %v, pool MaxConns %d) — PASS",
		totalFires, iterations, drainDuration, cadenceInterval, boundedPool.Stat().MaxConns())
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

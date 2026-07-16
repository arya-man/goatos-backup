package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// seedRV04FillerObligations inserts n obligation_instances rows that CANNOT ever classify as
// missing-anchor candidates regardless of their protocol rule's trigger type: target_type='tenant'
// (not 'goat') makes the classify query's INNER JOIN to goats find no match, dropping every filler
// row from the result set before status/anchor/history filtering is even considered. Each row gets
// its own due_at (obligation_instances_dup_guard is UNIQUE NULLS NOT DISTINCT on (tenant_id,
// protocol_version_id, rule_id, target_type, target_id, due_at), and every filler here shares the
// same tenant/version/rule/target) and an ascending obligation_id below the real candidate's, so the
// base keyset scan (ORDER BY obligation_id) visits every filler BEFORE the real candidate.
func seedRV04FillerObligations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, versionID, ruleID string, n int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence
)
SELECT
  ('01000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
  $1::uuid,
  $2::uuid,
  $3::uuid,
  'tenant',
  $1::uuid,
  'tenant',
  $1::uuid,
  TIMESTAMPTZ '2026-01-01 00:00:00+00' + i * INTERVAL '1 minute',
  'scheduled',
  'rv04-filler-' || i::text,
  1
FROM generate_series(1, $4) AS s(i)
ON CONFLICT DO NOTHING`,
		tenantID, versionID, ruleID, n); err != nil {
		t.Fatalf("seed %d filler obligations: %v", n, err)
	}
}

// TestCountFabricatedMissingAnchorWorkPagedReachesMatchBeyondFirstPage is the RV-04 guard. Before
// this test, countFabricatedMissingAnchorWorkPaged's two-query keyset pagination (base
// obligation_ids off the PK, THEN classify only that bounded id set) had no test exercising more
// than one page: nothing exercised the "first page empty, match on a later page" boundary, so a
// regression that silently stopped after the first page (e.g. reverting to a single bounded
// LIMIT/NOT-EXISTS shape, or an off-by-one in the "< pageSize" continuation check) could ship
// undetected.
//
// Rather than bulk-seeding missingAnchorReconcilePageSize (5000) real rows to force that boundary
// -- slow, and beside the point being tested -- this exercises the EXACT SAME pagination code path
// through countFabricatedMissingAnchorWorkPagedWithPageSize with a tiny pageSize (2): the bug this
// guards is a page-BOUNDARY-crossing defect, not a row-VOLUME defect, so a page size of 2 crossed by
// 3 filler rows is exactly as discriminating as 5000 fillers crossed by 5001, for a fraction of the
// seed cost. 3 fillers (page 1 = rows 1-2, a full page triggering continuation; page 2 = row 3, a
// short page) sit entirely below the real candidate's obligation_id, and the real candidate --
// seeded with an obligation_id above all fillers -- lands on page 2. The real two-query path must
// still find and count it: countFabricatedMissingAnchorWorkPagedWithPageSize must return exactly 1,
// proving the pagination reaches a match beyond the first page instead of silently stopping at page
// one, AND that it counts EXACTLY the fabricated row (not the fillers, which a broken classify join
// could otherwise mis-count if it ever silently included non-goat targets).
func TestCountFabricatedMissingAnchorWorkPagedReachesMatchBeyondFirstPage(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := seedProtocolFixture(t, ctx, pool)

	// 3 fillers with a page size of 2: page 1 (rows 1-2) is a FULL page -- exactly pageSize -- which
	// correctly triggers the loop's "keep paging" continuation; page 2 (row 3 + the real candidate)
	// is where the real candidate must be found.
	const pageSize = 2
	const fillerCount = 3
	seedRV04FillerObligations(t, ctx, pool, defaultTenantID, versionID, ruleID, fillerCount)

	// The real candidate's obligation_id ('02...') sorts strictly after every filler's ('01...') in
	// the base scan's ORDER BY obligation_id, guaranteeing it lands beyond page one.
	const candidateObligationID = "02000000-0000-4000-8000-000000000001"
	const candidateIdempotencyKey = "rv04-real-fabricated-candidate"
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $5::uuid,
  'tenant', $2::uuid, TIMESTAMPTZ '2026-06-01 00:00:00+00', 'scheduled', $6, 1
)`, candidateObligationID, defaultTenantID, versionID, ruleID, testLedgerGoat, candidateIdempotencyKey); err != nil {
		t.Fatalf("seed real missing-anchor candidate: %v", err)
	}

	got, err := countFabricatedMissingAnchorWorkPagedWithPageSize(ctx, pool, defaultTenantID, pageSize)
	if err != nil {
		t.Fatalf("countFabricatedMissingAnchorWorkPagedWithPageSize: %v", err)
	}
	if got != 1 {
		t.Fatalf("countFabricatedMissingAnchorWorkPagedWithPageSize = %d, want exactly 1 (the real candidate on page two; %d fillers on page one must never be counted)", got, fillerCount)
	}

	// Sanity: the production entry point (fixed pageSize=5000) must also find it -- this table has
	// only 4 rows total, so it is trivially all on "page one" at the production page size, proving
	// the wiring between countFabricatedMissingAnchorWorkPaged and the WithPageSize variant is
	// correct independent of the pagination-boundary property above.
	gotDefault, err := countFabricatedMissingAnchorWorkPaged(ctx, pool, defaultTenantID)
	if err != nil {
		t.Fatalf("countFabricatedMissingAnchorWorkPaged: %v", err)
	}
	if gotDefault != 1 {
		t.Fatalf("countFabricatedMissingAnchorWorkPaged = %d, want exactly 1", gotDefault)
	}
}

// TestCountFabricatedMissingAnchorWorkPagedExcludesRoutedCatchUp is the companion negative case:
// a candidate that otherwise matches every missing-anchor predicate but whose idempotency key IS
// the routed Contract-§89 option-4 catch-up key must NOT be counted -- it is a deliberate routing
// decision, not a defect. This guards against a regression that removed the isRoutedCatchUp
// classification and counted every missing-anchor row unconditionally.
func TestCountFabricatedMissingAnchorWorkPagedExcludesRoutedCatchUp(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := seedProtocolFixture(t, ctx, pool)

	routedKey := vaccinationapp.AnchorMissingCatchUpKey(defaultTenantID, versionID, ruleID, testLedgerGoat, "birth_age", 1)
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence
) VALUES (
  '03000000-0000-4000-8000-000000000001'::uuid, $1::uuid, $2::uuid, $3::uuid, 'goat', $4::uuid,
  'tenant', $1::uuid, TIMESTAMPTZ '2026-06-01 00:00:00+00', 'scheduled', $5, 1
)`, defaultTenantID, versionID, ruleID, testLedgerGoat, routedKey); err != nil {
		t.Fatalf("seed routed catch-up candidate: %v", err)
	}

	got, err := countFabricatedMissingAnchorWorkPaged(ctx, pool, defaultTenantID)
	if err != nil {
		t.Fatalf("countFabricatedMissingAnchorWorkPaged: %v", err)
	}
	if got != 0 {
		t.Fatalf("countFabricatedMissingAnchorWorkPaged = %d, want 0 (the routed §89 catch-up must be excluded, not counted as a defect)", got)
	}
}

// TestCountFabricatedMissingAnchorWorkPagedExcludesPrimaryCourseContinuation guards the seed
// postflight audit for the ET+TT/Blue Tongue course-continuation path. A goat can lack the original
// DOB/entry anchor yet still have an accepted previous primary-course dose from the seed sheet; the
// next dose is then legitimate future work from that accepted history, not fabricated missing-anchor
// work. Before this guard, such rows were counted as missing-anchor defects and a clean full seed
// failed even though dose 2 was correctly scheduled from dose 1 history.
func TestCountFabricatedMissingAnchorWorkPagedExcludesPrimaryCourseContinuation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, firstRuleID := seedProtocolFixture(t, ctx, pool)
	const secondRuleID = "20000000-0000-4000-8000-000000000222"
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json, proof_policy, sort_order
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'et_tt_kid_7w', 2, 'birth_age',
  49, 7, 21, 'none', 'immediate', '{}'::jsonb, '{}'::jsonb, 2
)`, secondRuleID, defaultTenantID, versionID); err != nil {
		t.Fatalf("seed second course rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code,
  vaccine_code, trigger_type, sequence, offset_days, due_window_days, min_gap_days, repeat
) VALUES
  ($1::uuid, $2::uuid, $3::uuid, 'vaccination', 'et_tt_kid_4w', 'et_tt_kid_4w',
   'ET_TT', 'birth_age', 1, 28, 7, 0, 'none'),
  ($1::uuid, $2::uuid, $4::uuid, 'vaccination', 'et_tt_kid_7w', 'et_tt_kid_7w',
   'ET_TT', 'birth_age', 2, 49, 7, 21, 'none')`,
		defaultTenantID, versionID, firstRuleID, secondRuleID); err != nil {
		t.Fatalf("seed course rule dimensions: %v", err)
	}

	const firstObligationID = "20000000-0000-4000-8000-000000000111"
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, completed_at, idempotency_key, sequence
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $5::uuid,
  'tenant', $2::uuid, TIMESTAMPTZ '2026-06-30 00:00:00+00',
  'completed', TIMESTAMPTZ '2026-06-30 00:00:00+00', 'accepted-et-tt-dose-one', 1
)`, firstObligationID, defaultTenantID, versionID, firstRuleID, testLedgerGoat); err != nil {
		t.Fatalf("seed accepted first-dose obligation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, goat_id, doses, dose_ml_given,
  administered_at, verified_at, status, idempotency_key
) VALUES (
  '30000000-0000-4000-8000-000000000111'::uuid, $1::uuid, $2::uuid, $3::uuid,
  1, 1, TIMESTAMPTZ '2026-06-30 00:00:00+00', TIMESTAMPTZ '2026-06-30 00:00:00+00',
  'accepted', 'accepted-et-tt-dose-one-completion'
)`, defaultTenantID, firstObligationID, testLedgerGoat); err != nil {
		t.Fatalf("seed accepted first-dose completion: %v", err)
	}

	const secondObligationID = "20000000-0000-4000-8000-000000000222"
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $5::uuid,
  'tenant', $2::uuid, TIMESTAMPTZ '2026-07-21 00:00:00+00',
  'scheduled', 'et-tt-dose-two-from-history', 2
)`, secondObligationID, defaultTenantID, versionID, secondRuleID, testLedgerGoat); err != nil {
		t.Fatalf("seed scheduled course continuation: %v", err)
	}

	got, err := countFabricatedMissingAnchorWorkPaged(ctx, pool, defaultTenantID)
	if err != nil {
		t.Fatalf("countFabricatedMissingAnchorWorkPaged: %v", err)
	}
	if got != 0 {
		t.Fatalf("countFabricatedMissingAnchorWorkPaged = %d, want 0 (course dose 2 from accepted dose 1 history is legitimate)", got)
	}
}

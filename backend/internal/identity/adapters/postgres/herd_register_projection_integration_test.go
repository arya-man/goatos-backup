package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// C35-005 herd-register projection regression suite.
//
// Migration 000164 was reordered so the maintenance triggers are installed BEFORE the projection
// backfill and the backfill is an idempotent convergent upsert. These tests prove the two properties
// that closes the live-write convergence gap:
//   1. no gap — a canonical write that lands after the projection is created is captured by the live
//      trigger (exactly the write that the old "backfill then create triggers" ordering would have
//      dropped if it happened during the backfill window);
//   2. convergence — replaying the backfill/refresh over a row the trigger already wrote does not
//      duplicate the row or drift the summary counts.
//
// Both run against a real Postgres with every committed migration applied statement-by-statement
// (pgtest mirrors the production statement-individual runner).

const (
	hrTenant = "00000000-0000-4000-8000-000000000001"
	hrParty  = "00000000-0000-4000-8000-000000001001"
	hrPark   = "00000000-0000-4000-8000-000000003001" // CBE
	hrGoatA  = "aa000000-0000-4000-8000-0000000000a1"
	hrGoatB  = "aa000000-0000-4000-8000-0000000000b2"
)

func scalarInt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("scalar query %q: %v", sql, err)
	}
	return n
}

func TestHerdRegisterProjectionCapturesPostMigrationWriteWithoutGap(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Baseline: no goats are seeded by the migrations, so the projection starts empty.
	var baseGoats, baseSummary int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id = $1`, hrTenant).Scan(&baseGoats); err != nil {
		t.Fatalf("baseline projection count: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(active_count),0) FROM herd_register_summary_projection WHERE tenant_id = $1`, hrTenant).Scan(&baseSummary); err != nil {
		t.Fatalf("baseline summary count: %v", err)
	}

	// A canonical write that arrives AFTER the projection/backfill exists. With triggers-first this is
	// captured immediately; under the old backfill-then-trigger ordering a write in that window was lost.
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, breed, sex, age_band)
VALUES ($1, $2, 'alive', 'goat', $3, $4, $4, 'Boer', 'female', 'adult')`,
		hrGoatA, hrTenant, hrParty, hrPark); err != nil {
		t.Fatalf("insert goat A: %v", err)
	}

	// The live goats trigger must have projected the new goat.
	if got := scalarInt(t, ctx, pool, `SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id = $1 AND goat_id = $2`, hrTenant, hrGoatA); got != 1 {
		t.Fatalf("post-write projection rows for goat A = %d, want 1 (live trigger must capture the write)", got)
	}
	if got := scalarInt(t, ctx, pool, `SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id = $1`, hrTenant); got != baseGoats+1 {
		t.Fatalf("projection total = %d, want baseline+1 (%d)", got, baseGoats+1)
	}
	// Summary reflects the new adult goat.
	if got := scalarInt(t, ctx, pool, `SELECT COALESCE(sum(active_count),0) FROM herd_register_summary_projection WHERE tenant_id = $1`, hrTenant); got != baseSummary+1 {
		t.Fatalf("summary active total = %d, want baseline+1 (%d)", got, baseSummary+1)
	}
	if got := scalarInt(t, ctx, pool, `SELECT COALESCE(sum(adult_count),0) FROM herd_register_summary_projection WHERE tenant_id = $1 AND park_id = $2`, hrTenant, hrPark); got != 1 {
		t.Fatalf("summary adult_count for park = %d, want 1", got)
	}

	// Exactness: projection equals canonical alive, non-merged goats.
	canonical := scalarInt(t, ctx, pool, `SELECT count(*) FROM goats WHERE tenant_id = $1 AND merged_into_goat_id IS NULL`, hrTenant)
	projected := scalarInt(t, ctx, pool, `SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id = $1`, hrTenant)
	if canonical != projected {
		t.Fatalf("projection (%d) != canonical non-merged goats (%d)", projected, canonical)
	}
}

func TestHerdRegisterBackfillConvergesWithoutDoubleCount(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// A goat written by the live trigger (the row a concurrent write would produce during a backfill).
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, breed, sex, age_band)
VALUES ($1, $2, 'alive', 'goat', $3, $4, $4, 'Boer', 'male', 'kid')`,
		hrGoatB, hrTenant, hrParty, hrPark); err != nil {
		t.Fatalf("insert goat B: %v", err)
	}

	summaryBefore := scalarInt(t, ctx, pool, `SELECT COALESCE(sum(active_count),0) FROM herd_register_summary_projection WHERE tenant_id = $1`, hrTenant)
	kidBefore := scalarInt(t, ctx, pool, `SELECT COALESCE(sum(kid_count),0) FROM herd_register_summary_projection WHERE tenant_id = $1`, hrTenant)
	rowsBefore := scalarInt(t, ctx, pool, `SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id = $1`, hrTenant)
	if summaryBefore != 1 || kidBefore != 1 || rowsBefore != 1 {
		t.Fatalf("pre-convergence state active=%d kid=%d rows=%d, want 1/1/1", summaryBefore, kidBefore, rowsBefore)
	}

	// Replay the convergent backfill/refresh over the same row twice. The migration's backfill uses this
	// exact idempotent refresh path; running it again must NOT duplicate the row or drift the summary
	// (the double-count hazard when a backfill overlaps a row the trigger already wrote).
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, `SELECT herd_register_refresh_goat_projection($1, $2)`, hrTenant, hrGoatB); err != nil {
			t.Fatalf("refresh replay %d: %v", i, err)
		}
	}

	if got := scalarInt(t, ctx, pool, `SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id = $1`, hrTenant); got != rowsBefore {
		t.Fatalf("projection rows after replay = %d, want unchanged %d (no duplication)", got, rowsBefore)
	}
	if got := scalarInt(t, ctx, pool, `SELECT COALESCE(sum(active_count),0) FROM herd_register_summary_projection WHERE tenant_id = $1`, hrTenant); got != summaryBefore {
		t.Fatalf("summary active after replay = %d, want unchanged %d (no double count)", got, summaryBefore)
	}
	if got := scalarInt(t, ctx, pool, `SELECT COALESCE(sum(kid_count),0) FROM herd_register_summary_projection WHERE tenant_id = $1`, hrTenant); got != kidBefore {
		t.Fatalf("summary kid after replay = %d, want unchanged %d", got, kidBefore)
	}
}

// TestHerdRegisterProjectionKeysetReadUsesIndex proves the bounded read path pages the projection by
// display_id via an index scan (no full projection/goats scan) — the read model the SSR reader consumes.
func TestHerdRegisterProjectionKeysetReadUsesIndex(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("set enable_seqscan: %v", err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN (COSTS OFF)
SELECT goat_id, display_id, park_id, breed, sex, lifecycle_status, is_kid, is_untagged
FROM herd_register_goat_projection
WHERE tenant_id = $1::uuid
  AND lifecycle_status = 'alive'
  AND display_id > $2::text
ORDER BY display_id ASC
LIMIT 20`, hrTenant, "")
	if err != nil {
		t.Fatalf("explain keyset read: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	planText := plan.String()
	if strings.Contains(planText, "Seq Scan on herd_register_goat_projection") {
		t.Fatalf("keyset read used a sequential scan:\n%s", planText)
	}
	if !strings.Contains(planText, "Index Scan") && !strings.Contains(planText, "Index Only Scan") && !strings.Contains(planText, "Bitmap Index Scan") {
		t.Fatalf("keyset read did not use an index:\n%s", planText)
	}
}

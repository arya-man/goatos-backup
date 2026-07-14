package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestCalendarCanonicalListKeysetPlanUsesIndex is the query-plan gate for the U4a canonical
// read-through (calendarCanonicalListSQL). It proves two things against a freshly migrated, empty
// database:
//
//  1. The full assembled canonical read is a valid, plannable query. EXPLAIN resolves every table
//     and column reference in the composed CTE chain, so a broken fragment splice fails here rather
//     than at request time.
//  2. The scale-critical access path — the tenant+due-window keyset page over obligation_instances
//     that drives the whole read — is index-backed, never a sequential scan. This is the "keyset,
//     ~20 rows" plan the ADR (operational-kernel-5k-50k-scale-envelope) requires for a
//     compute-on-read Calendar list at the 5k-to-50k envelope.
//
// The bash sibling `validate_calendar_canonical_read_plan` in
// backend/tests/integration/validate-sqlc-query-plans.sh runs the same obligation_instances keyset
// EXPLAIN under `make validate-sqlc-plans`.
func TestCalendarCanonicalListKeysetPlanUsesIndex(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	dateFrom := time.Now().Add(-time.Hour)
	dateToExclusive := time.Now().Add(45 * 24 * time.Hour)

	// (1) The full canonical read must plan without error against the real schema.
	explainRows, err := pool.Query(ctx, "EXPLAIN (COSTS OFF) "+calendarCanonicalListSQL,
		testTenantID, dateFrom, dateToExclusive, "", "", "", "",
		nil, "", 21, true, []string{}, []string{})
	if err != nil {
		t.Fatalf("canonical read did not plan (composition/schema error): %v", err)
	}
	var full []string
	for explainRows.Next() {
		var line string
		if err := explainRows.Scan(&line); err != nil {
			explainRows.Close()
			t.Fatal(err)
		}
		full = append(full, line)
	}
	explainRows.Close()
	if err := explainRows.Err(); err != nil {
		t.Fatal(err)
	}
	fullPlan := strings.Join(full, "\n")
	if strings.Contains(fullPlan, "Seq Scan on obligation_instances") {
		t.Fatalf("canonical read sequentially scans obligation_instances (the keyset driver):\n%s", fullPlan)
	}

	// (2) The obligation_instances tenant+due-window keyset page — the scan that bounds the whole
	// canonical read — must be index-backed. Prove it in isolation with enable_seqscan off so the
	// planner cannot hide an unindexed scan behind cheap empty-table estimates.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN (COSTS OFF)
SELECT obligation_id
FROM obligation_instances
WHERE tenant_id = $1::uuid
  AND batch_id IS NULL
  AND due_at >= $2::timestamptz
  AND due_at < $3::timestamptz
ORDER BY due_at ASC, obligation_id ASC
LIMIT 21`, testTenantID, dateFrom, dateToExclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(lines, "\n")
	if strings.Contains(plan, "Seq Scan on obligation_instances") {
		t.Fatalf("canonical read keyset driver used a sequential scan:\n%s", plan)
	}
	if !strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Index Only Scan") && !strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("canonical read keyset driver is not index-backed:\n%s", plan)
	}
}

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestCanonicalReadQueryPlans is the query-plan gate for the 5k-50k-envelope canonical serving reads
// (docs/decisions/operational-kernel-5k-50k-scale-envelope.md, step 4). The four reads
// (shed / execution / operations / full schedule) now serve directly from canonical tables via god-CTEs carrying a
// scale-guard:ignore exemption; the exemption is only valid while the driving obligation_instances /
// goats scans stay indexed. This test EXPLAINs each read with enable_seqscan disabled and asserts the
// two scale-critical (millions-of-rows) tables -- obligation_instances and goats -- are reached through
// an index, never a sequential scan, and that the plan uses an indexed access path overall. A regression
// that drops the tenant/status/due index usage on obligation_instances fails here, keeping the exemption
// honest.
func TestCanonicalReadQueryPlans(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	dueBefore := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	closedAfter := asOf.Add(-defaultClosedHistoryAge)
	cfg := domain.DefaultCapacityConfig()

	shedSQL := strings.Replace(shedSummaryCanonicalReadSQL, "__ORDER_BY__", shedSummaryOrderBy(domain.ShedSortParkShed), 1)

	cases := []struct {
		name string
		sql  string
		args []any
	}{
		{
			name: "execution list (vaccinationExecutionSQL)",
			sql:  vaccinationExecutionSQL,
			// $1 tenant, $2 park, $3 shed, $4 dueBefore, $5 limit, $6 workState, $7 asOf,
			// $8 closedAfter, $9 severity, $10 openOnly, $11 cursorPresent, $12 rank,
			// $13 dueMicros, $14 rowKey, $15 operatorScopeActorID
			args: []any{testTenant, "", "", dueBefore, 20, "", asOf, closedAfter, "", false, false, 0, int64(0), "", ""},
		},
		{
			name: "operations list (vaccinationOperationsSQL)",
			sql:  vaccinationOperationsSQL,
			// $1 tenant, $2 asOf, $3 dueBefore, $4 park, $5 shed, $6 cursorPark, $7 cursorShed,
			// $8 cursorStage, $9 limit, $10 cursorParkName, $11 cursorShedName
			args: []any{testTenant, asOf, dueBefore, "", "", "", "", "", 21, "", ""},
		},
		{
			name: "shed summary (shedSummaryCanonicalReadSQL)",
			sql:  shedSQL,
			// $1 tenant, $2 asOf, $3 dueBefore, $4 maxPerDay, $5 maxBufferDays, $6 park, $7 shed,
			// $8 search, $9 status, $10 capacity, $11 limit, $12 offset
			args: []any{testTenant, asOf, dueBefore, cfg.MaxPerDay, cfg.MaxBufferDays, "", "", "", "", "", 50, 0},
		},
		{
			name: "full schedule month (vaccinationScheduleWindowSQL)",
			sql:  vaccinationScheduleWindowSQL,
			// $1 tenant, $2 asOf, $3 monthStart, $4 monthEnd, $5 park, $6 cursorPark,
			// $7 cursorShed, $8 cursorStage, $9 cursorParkName, $10 cursorShedName,
			// $11 limit, $12 authorizedParks
			args: []any{
				testTenant,
				asOf,
				time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				"", "", "", "", "", "", 50, nil,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := explainPlan(t, ctx, pool, tc.sql, tc.args...)
			for _, forbidden := range []string{"Seq Scan on obligation_instances", "Seq Scan on goats"} {
				if strings.Contains(plan, forbidden) {
					t.Fatalf("%s: unexpected %q in plan (scale-critical table must stay indexed):\n%s", tc.name, forbidden, plan)
				}
			}
			if !strings.Contains(plan, "Index Scan") &&
				!strings.Contains(plan, "Index Only Scan") &&
				!strings.Contains(plan, "Bitmap Index Scan") {
				t.Fatalf("%s: expected an indexed access path, got:\n%s", tc.name, plan)
			}
		})
	}
}

// explainPlan runs EXPLAIN (COSTS OFF) on a parameterized read with enable_seqscan disabled on a single
// pinned session, so the planner is forced to prefer index paths wherever one exists and any remaining
// Seq Scan is a genuine "no usable index" signal rather than a small-table cost choice. It returns the
// full plan text.
func explainPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	rows, err := conn.Query(ctx, "EXPLAIN (COSTS OFF) "+sql, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	return b.String()
}

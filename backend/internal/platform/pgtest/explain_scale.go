package pgtest

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ScalePlan is one EXPLAIN (ANALYZE, BUFFERS) of a production statement, for the Test*AtScale plan
// proofs that `make scale-guard-plan-proof` asks for.
type ScalePlan struct {
	Text   string
	ExecMs float64
}

// ExplainAnalyzeAtScale runs EXPLAIN (ANALYZE, BUFFERS) of sql with args in a rolled-back transaction
// with JIT off (production API pools disable JIT) and NEVER with enable_seqscan=off: the planner's
// honest choice over real statistics is the proof. Call ANALYZE on the seeded tables first.
func ExplainAnalyzeAtScale(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) ScalePlan {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin explain tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
		t.Fatalf("disable JIT: %v", err)
	}
	rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+sql, args...)
	if err != nil {
		t.Fatalf("explain analyze: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	plan := ScalePlan{Text: strings.Join(lines, "\n")}
	for _, line := range lines {
		if _, after, ok := strings.Cut(line, "Execution Time:"); ok {
			plan.ExecMs, _ = strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(after), "ms")), 64)
		}
	}
	return plan
}

// AssertNoSeqScan fails when the plan sequentially scans any of the named (large) tables, or when it
// ran slower than budgetMs.
func (p ScalePlan) AssertNoSeqScan(t *testing.T, name string, budgetMs float64, tables ...string) {
	t.Helper()
	t.Logf("%s: %.1f ms\n%s", name, p.ExecMs, p.Text)
	for _, table := range tables {
		if strings.Contains(p.Text, "Seq Scan on "+table+" ") || strings.HasSuffix(p.Text, "Seq Scan on "+table) ||
			strings.Contains(p.Text, "Seq Scan on "+table+"\n") {
			t.Errorf("%s sequentially scans %s at scale:\n%s", name, table, p.Text)
		}
	}
	if p.ExecMs > budgetMs {
		t.Errorf("%s took %.1f ms at scale, budget %.0f ms:\n%s", name, p.ExecMs, budgetMs, p.Text)
	}
}

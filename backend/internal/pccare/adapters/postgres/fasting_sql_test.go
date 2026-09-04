package postgres

import (
	"os"
	"strings"
	"testing"
)

// Source-shape pins for the feed & water removal SQL (maintainer decision 2026-09-03). These
// are the pure-Go halves of the proofs; the Postgres integration test in
// fasting_precondition_integration_test.go exercises the same SQL against the real schema.

// The list read hides a feed_water_removal card until 20:00 IST of its due day, judged by the
// CALLER's clock bind (the shiftingActionsVisibleSQL shape) — never a DB-side now().
func TestListTasksHidesFeedWaterRemovalUntilEvening(t *testing.T) {
	src, err := os.ReadFile("tasks.go")
	if err != nil {
		t.Fatalf("read tasks.go: %v", err)
	}
	sql := string(src)
	start := strings.Index(sql, "listTasksPageSQL := `")
	if start < 0 {
		t.Fatal("listTasksPageSQL not found")
	}
	end := strings.Index(sql[start:], "ORDER BY")
	if end < 0 {
		t.Fatal("listTasksPageSQL ORDER BY not found")
	}
	page := sql[start : start+end]
	for _, want := range []string{
		"t.category <> 'feed_water_removal'",
		"AT TIME ZONE 'Asia/Kolkata'",
		"t.due_business_date + TIME '20:00'",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("listTasksPageSQL must carry the evening-visibility predicate piece %q", want)
		}
	}
	if !strings.Contains(page, "$15::timestamptz") {
		t.Fatal("the visibility predicate must compare against the caller's clock bind, not now()")
	}
	if strings.Contains(page, "now()") {
		t.Fatal("listTasksPageSQL must not read the DB clock for the visibility rule")
	}
}

// The midnight gate: a deworming whose linked removal was never SUBMITTED is pushed to
// today + 1 (never today), delayed, chunk-claimed with SKIP LOCKED. The gate is
// submitted_at IS NULL — a later verifier rework does NOT re-block the deworming.
func TestDewormingRemovalGateSweepShape(t *testing.T) {
	src, err := os.ReadFile("kernel.go")
	if err != nil {
		t.Fatalf("read kernel.go: %v", err)
	}
	sql := string(src)
	start := strings.Index(sql, "func (r *Repository) sweepDewormingRemovalGate")
	if start < 0 {
		t.Fatal("sweepDewormingRemovalGate not found — the midnight gate pass is missing")
	}
	gate := sql[start:]
	for _, want := range []string{
		"removal.gates_task_id = d.task_id",
		"removal.submitted_at IS NULL",
		"removal.work_state <> 'canceled'",
		"due_business_date = $2::date + 1",
		"d.due_business_date <= $2::date",
		"FOR UPDATE OF d SKIP LOCKED",
		"d.category = 'deworming'",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("removal gate sweep must carry %q", want)
		}
	}
	// The sweep entry point must actually RUN the gate after the roll-forward pass.
	if !strings.Contains(sql[:start], "sweepDewormingRemovalGate(") {
		t.Fatal("SweepTaskRollForward must invoke the removal gate pass after roll-forward")
	}
}

// The deworming create births the removal row in the SAME transaction: linked by
// gates_task_id, keyed off the deworming's idempotency key, one day earlier.
func TestCreateTaskLinksTheRemovalRowInTheSameTransaction(t *testing.T) {
	src, err := os.ReadFile("tasks.go")
	if err != nil {
		t.Fatalf("read tasks.go: %v", err)
	}
	sql := string(src)
	if !strings.Contains(sql, `p.IdempotencyKey+":fasting"`) {
		t.Fatal("the removal task must derive its idempotency key from the deworming's (key + \":fasting\")")
	}
	if !strings.Contains(sql, "gates_task_id, idempotency_key, created_by") {
		t.Fatal("the removal insert must write gates_task_id")
	}
	if !strings.Contains(sql, "p.PlannedBusinessDate.AddDate(0, 0, -1)") {
		t.Fatal("the removal task must be planned one day BEFORE the deworming date")
	}
	// The SQL body is a package-level const (scale-guard hoisting); what must sit
	// INSIDE the create transaction, before its commit, is the CALL that executes it.
	callIdx := strings.Index(sql, "tx.QueryRow(ctx, removalTaskInsertSQL")
	if callIdx < 0 {
		t.Fatal("CreateTask must execute removalTaskInsertSQL through the create transaction")
	}
	commitIdx := strings.Index(sql, `pccare: commit create task`)
	if commitIdx >= 0 && callIdx > commitIdx {
		t.Fatal("the removal insert must happen inside the create transaction, before its commit")
	}
}

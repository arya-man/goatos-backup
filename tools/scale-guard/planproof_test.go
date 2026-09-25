package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Fixtures for -plan-proof: a real throwaway git repo, a base commit, then the PR change.

const ppAdapter = "backend/internal/example/adapters/postgres/repository.go"

const ppBaseSQL = "package postgres\n\nconst dueSQL = `\nSELECT oi.obligation_id\nFROM obligation_instances oi\nWHERE oi.tenant_id = $1\n  AND oi.due_at <= $2\n`\n"

func ppRepo(t *testing.T, base map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	ppWrite(t, dir, base)
	run("add", "-A")
	run("commit", "-qm", "base")
	run("tag", "base")
	return dir
}

func ppWrite(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const ppChangedSQL = "package postgres\n\nconst dueSQL = `\nSELECT oi.obligation_id\nFROM obligation_instances oi\nWHERE oi.tenant_id = $1\n  AND (oi.due_at <= $2 OR oi.batch_id = ANY($3::uuid[]))\n`\n"

func TestPlanProofFailsWhenLargeTableSQLChangesWithoutAtScaleTest(t *testing.T) {
	dir := ppRepo(t, map[string]string{ppAdapter: ppBaseSQL})
	ppWrite(t, dir, map[string]string{ppAdapter: ppChangedSQL})
	if code := runPlanProof(dir, "base"); code != 1 {
		t.Fatalf("plan-proof exit = %d, want 1 (STG-only validation of a changed obligation_instances read)", code)
	}
	// An at-scale test that exists but is NOT part of this diff is not proof for this change.
	dir = ppRepo(t, map[string]string{ppAdapter: ppBaseSQL,
		"backend/internal/example/adapters/postgres/plan_test.go": "package postgres\n\nfunc TestDueQueryPlanUsesIndexesAtScale(t *testing.T) {}\n"})
	ppWrite(t, dir, map[string]string{ppAdapter: ppChangedSQL})
	if code := runPlanProof(dir, "base"); code != 1 {
		t.Fatalf("an untouched at-scale test must not satisfy the gate, exit = %d", code)
	}
}

func TestPlanProofPassesWithAChangedAtScaleTestOrSqlcExplainOrExempt(t *testing.T) {
	dir := ppRepo(t, map[string]string{ppAdapter: ppBaseSQL})
	ppWrite(t, dir, map[string]string{ppAdapter: ppChangedSQL,
		"backend/internal/example/adapters/postgres/plan_test.go": "package postgres\n\nfunc TestDueQueryPlanUsesIndexesAtScale(t *testing.T) {}\n"})
	if code := runPlanProof(dir, "base"); code != 0 {
		t.Fatalf("an added Test*AtScale in the package must satisfy the gate, exit = %d", code)
	}

	sqlc := "backend/internal/example/adapters/postgres/sqlc/query.sql.go"
	dir = ppRepo(t, map[string]string{sqlc: ppBaseSQL, sqlcPlanScript: "#!/bin/bash\n"})
	ppWrite(t, dir, map[string]string{sqlc: ppChangedSQL,
		sqlcPlanScript: "#!/bin/bash\n  explain_must_use_index \"ObligationDue\" 'Seq Scan on obligation_instances' \"EXPLAIN ...\"\n"})
	if code := runPlanProof(dir, "base"); code != 0 {
		t.Fatalf("an added validate-sqlc-plans explain entry must satisfy the gate for sqlc, exit = %d", code)
	}

	dir = ppRepo(t, map[string]string{ppAdapter: ppBaseSQL})
	ppWrite(t, dir, map[string]string{ppAdapter: ppChangedSQL + "\n// scale-guard:plan-proof-exempt: bind-array arm only, same index\n"})
	if code := runPlanProof(dir, "base"); code != 0 {
		t.Fatalf("an exempt reason must satisfy the gate, exit = %d", code)
	}
}

func TestPlanProofIgnoresCommentsSmallTablesAndTests(t *testing.T) {
	// A SQL comment edit cannot move a plan.
	dir := ppRepo(t, map[string]string{ppAdapter: ppBaseSQL})
	ppWrite(t, dir, map[string]string{ppAdapter: "package postgres\n\nconst dueSQL = `\nSELECT oi.obligation_id\n  -- due window only; OR the drive arm later\nFROM obligation_instances oi\nWHERE oi.tenant_id = $1\n  AND oi.due_at <= $2\n`\n"})
	if code := runPlanProof(dir, "base"); code != 0 {
		t.Fatalf("a SQL comment change must pass, exit = %d", code)
	}
	// A small table is out of scope.
	small := "package postgres\n\nconst parkSQL = `\nSELECT p.id FROM parks p\nWHERE p.tenant_id = $1\n`\n"
	dir = ppRepo(t, map[string]string{ppAdapter: small})
	ppWrite(t, dir, map[string]string{ppAdapter: "package postgres\n\nconst parkSQL = `\nSELECT p.id FROM parks p\nWHERE p.tenant_id = $1 AND p.active\n`\n"})
	if code := runPlanProof(dir, "base"); code != 0 {
		t.Fatalf("a small-table change must pass, exit = %d", code)
	}
	// SQL inside a _test.go file is not serving SQL.
	tf := "backend/internal/example/adapters/postgres/fixture_test.go"
	dir = ppRepo(t, map[string]string{tf: ppBaseSQL})
	ppWrite(t, dir, map[string]string{tf: ppChangedSQL})
	if code := runPlanProof(dir, "base"); code != 0 {
		t.Fatalf("test fixtures must pass, exit = %d", code)
	}
}

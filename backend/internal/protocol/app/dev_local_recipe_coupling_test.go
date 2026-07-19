package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDevLocalRecipeKeepsMigrateAndSeedCoupled is a lightweight regression guard for R50-002 (the
// R50 audit flagged that the local-stack Make recipe could silently regress if a future edit splits
// the DB migration step and the seed/closeout step apart — for example gating one behind a flag
// while leaving the other unconditional, or reordering seed ahead of migrate).
//
// The actual migrate+seed coupling with failure semantics lives in tools/dev/run-local-stack-supervised.sh,
// which enforces the coupling via an `if ! (migrate_local_database && seed_dev_grant && seed_closeout_if_present); then`
// statement (line ~318) that forces both to succeed together. This test checks that the supervised
// script retains this explicit coupling so removing a step or flag would fail the test.
func TestDevLocalRecipeKeepsMigrateAndSeedCoupled(t *testing.T) {
	repoRoot := repoRootFromThisFile(t)

	// Check the actual coupling script (not the basic run-local-stack.sh which may be permissive).
	const couplingScript = "tools/dev/run-local-stack-supervised.sh"
	script := readRepoFile(t, repoRoot, couplingScript)

	// The coupling must be explicit: all three operations in one if statement with && operators
	// so failure of any step is caught immediately.
	if !strings.Contains(script, "migrate_local_database && seed_dev_grant && seed_closeout_if_present") {
		t.Fatalf(
			"%s must contain explicit coupling: `migrate_local_database && seed_dev_grant && seed_closeout_if_present` in one conditional. "+
				"This ensures all three operations succeed together; removing or splitting one is caught by the test.",
			couplingScript,
		)
	}

	// Verify the functions themselves are defined and contain the actual commands.
	if !strings.Contains(script, "cmd/migrate -timeout=10m") {
		t.Fatalf("%s must run migrate with explicit -timeout=10m flag", couplingScript)
	}
	if !strings.Contains(script, "seed-closeout.sh") {
		t.Fatalf("%s must run the seed closeout step (seed-closeout.sh)", couplingScript)
	}

	// Order verification: migrate_local_database function must be defined before the coupling statement.
	lines := strings.Split(script, "\n")
	migrateFnLine := findFunctionDefinitionLine(t, lines, "migrate_local_database")
	couplingLine := findCouplingStatementLine(t, lines)

	if migrateFnLine >= couplingLine {
		t.Fatalf(
			"expected migrate_local_database function definition (line %d) to appear before coupling statement (line %d) in %s",
			migrateFnLine+1, couplingLine+1, couplingScript,
		)
	}
}

// repoRootFromThisFile resolves the repo root from this test file's own path
// (backend/internal/protocol/app/<file>_test.go), so the test works regardless of the working
// directory `go test` is invoked from.
func repoRootFromThisFile(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file path via runtime.Caller")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func readRepoFile(t *testing.T, repoRoot, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// extractMakeRecipe returns the tab-indented recipe body immediately following a bare
// "<target>:" header line (no prerequisites), stopping at the first non-recipe line.
func extractMakeRecipe(t *testing.T, makefile, target string) string {
	t.Helper()
	header := target + ":"
	lines := strings.Split(makefile, "\n")
	var recipe []string
	inRecipe := false
	for _, line := range lines {
		if inRecipe {
			if !strings.HasPrefix(line, "\t") {
				break
			}
			recipe = append(recipe, line)
			continue
		}
		if line == header {
			inRecipe = true
		}
	}
	if len(recipe) == 0 {
		t.Fatalf("could not find a Make recipe body for target %q in Makefile", target)
	}
	return strings.Join(recipe, "\n")
}

// findFunctionDefinitionLine returns the (0-based) line index of a function definition `fn() {`.
func findFunctionDefinitionLine(t *testing.T, lines []string, fn string) int {
	t.Helper()
	fnDef := fn + "() {"
	for i, line := range lines {
		if strings.TrimSpace(line) == fnDef {
			return i
		}
	}
	t.Fatalf("did not find function definition for %s (expected a line with `%s`)", fn, fnDef)
	return -1
}

// findCouplingStatementLine returns the (0-based) line index of the coupling statement that chains
// migrate_local_database && seed_dev_grant && seed_closeout_if_present together.
func findCouplingStatementLine(t *testing.T, lines []string) int {
	t.Helper()
	for i, line := range lines {
		if strings.Contains(line, "migrate_local_database && seed_dev_grant && seed_closeout_if_present") {
			return i
		}
	}
	t.Fatalf("did not find the coupling statement with migrate_local_database && seed_dev_grant && seed_closeout_if_present")
	return -1
}

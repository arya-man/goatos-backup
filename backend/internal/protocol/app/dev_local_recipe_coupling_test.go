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
// The Makefile `dev-local` target is a one-line delegate to tools/dev/run-local-stack.sh; the actual
// migrate+seed coupling lives inside that script (migrate_local_database, then
// seed_closeout_if_present). This test walks both files rather than only grepping the Makefile
// recipe body, so it still catches drift if the coupling logic moves within the script.
func TestDevLocalRecipeKeepsMigrateAndSeedCoupled(t *testing.T) {
	repoRoot := repoRootFromThisFile(t)

	makefile := readRepoFile(t, repoRoot, "Makefile")
	recipe := extractMakeRecipe(t, makefile, "dev-local")
	const delegateScript = "tools/dev/run-local-stack.sh"
	if !strings.Contains(recipe, delegateScript) {
		t.Fatalf("dev-local recipe no longer delegates to %s:\n%s", delegateScript, recipe)
	}

	script := readRepoFile(t, repoRoot, delegateScript)
	lines := strings.Split(script, "\n")

	migrateCallLine := findUnconditionalCallLine(t, lines, "migrate_local_database")
	seedCallLine := findUnconditionalCallLine(t, lines, "seed_closeout_if_present")

	if migrateCallLine >= seedCallLine {
		t.Fatalf(
			"expected migrate_local_database (line %d) to run BEFORE seed_closeout_if_present (line %d) in %s",
			migrateCallLine+1, seedCallLine+1, delegateScript,
		)
	}

	if !strings.Contains(script, "cmd/migrate") {
		t.Fatalf("%s no longer runs the migration binary (cmd/migrate)", delegateScript)
	}
	if !strings.Contains(script, "seed-closeout.sh") {
		t.Fatalf("%s no longer runs the seed closeout step (seed-closeout.sh)", delegateScript)
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

// findUnconditionalCallLine returns the (0-based) line index of a bare `fn` invocation — i.e. the
// call site, not the `fn() {` function definition — so the test proves the step actually RUNS
// rather than merely being defined.
func findUnconditionalCallLine(t *testing.T, lines []string, fn string) int {
	t.Helper()
	for i, line := range lines {
		if strings.TrimSpace(line) == fn {
			return i
		}
	}
	t.Fatalf("did not find an unconditional call to %s (expected a bare `%s` invocation line, not just its function definition)", fn, fn)
	return -1
}

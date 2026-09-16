package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSOPCaptureParityMigrationsAreAdditive pins migrations 000332-000334 (SOP capture parity,
// 2026-09-16) as additive: the Up half only adds defaulted columns IF NOT EXISTS, a CHECK on a new
// column, and a concurrent partial index -- no DROP, no type change, no data rewrite -- so an
// installed phone and every existing row keep working mid-rollout. Reads the SQL; no database.
func TestSOPCaptureParityMigrationsAreAdditive(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "migrations", "postgres")
	banned := regexp.MustCompile(`(?i)\b(DROP\s+TABLE|DROP\s+COLUMN|DROP\s+INDEX|ALTER\s+COLUMN|UPDATE\s+|DELETE\s+FROM|TRUNCATE|RENAME)`)
	for _, name := range []string{
		"000332_sop_capture_parity_workflows.sql",
		"000333_counts_approval_capture.sql",
		"000334_counts_birth_reported_outbox_index.sql",
	} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		up, _, found := strings.Cut(string(raw), "-- +goose Down")
		if !found {
			t.Fatalf("%s has no Down section", name)
		}
		for _, line := range strings.Split(up, "\n") {
			code := strings.TrimSpace(line)
			if code == "" || strings.HasPrefix(code, "--") || strings.HasPrefix(code, "'") {
				continue
			}
			upper := strings.ToUpper(code)
			if strings.Contains(upper, "DROP CONSTRAINT IF EXISTS COUNTS_APPROVAL_REQUESTS_CAPTURE_REVIEW_STATUS_CHECK") {
				continue // re-runnable CHECK on the new column
			}
			if hit := banned.FindString(code); hit != "" {
				t.Fatalf("%s Up is not additive (%s): %q", name, hit, code)
			}
			if strings.Contains(upper, "ADD COLUMN") && !strings.Contains(upper, "IF NOT EXISTS") {
				t.Fatalf("%s: ADD COLUMN must be IF NOT EXISTS: %q", name, code)
			}
		}
	}
}

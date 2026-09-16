package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrationEmbedsTheSeededWeighingSOP pins migration 000315 to the embedded seed: the
// section it adds to each tenant's published weighing.session version is the same bytes the
// code compiles for a tenant with no authored version, so day one on STG is the current
// behaviour exactly. The seed appears twice (the fresh-tenant insert and the in-place add).
func TestMigrationEmbedsTheSeededWeighingSOP(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "postgres", "000315_weighing_sop_rules.sql")
	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Historical migration stays immutable; 000318 widens only the seeded calendar floor.
	legacySeed := strings.ReplaceAll(string(SeededWeighingSOPJSON()), `"earliest_date": "2026-07-05"`, `"earliest_date": "2026-08-01"`)
	want := "$seed$" + strings.TrimSpace(legacySeed) + "$seed$"
	if got := strings.Count(string(sql), want); got != 2 {
		t.Fatalf("migration embeds the seeded weighing sop verbatim %d time(s), want 2", got)
	}
}

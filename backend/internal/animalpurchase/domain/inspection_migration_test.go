package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrationEmbedsTheSeededInspection pins migration 000304 to the embedded seed: the
// document it publishes as procurement.animal_purchase v1 is the same bytes the code compiles
// for a tenant with no authored version, so day one on STG is the current flow exactly.
func TestMigrationEmbedsTheSeededInspection(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "postgres", "000304_procurement_sop_animal_purchase_inspection.sql")
	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "$seed$" + strings.TrimSpace(string(SeededInspectionJSON())) + "$seed$"
	if !strings.Contains(string(sql), want) {
		t.Fatalf("migration does not embed the seeded inspection verbatim")
	}
}

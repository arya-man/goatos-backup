package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestShedWeightsUseExactShedIDWithoutPartitionSplit(t *testing.T) {
	source, err := os.ReadFile("shed_weights.go")
	if err != nil {
		t.Fatalf("read shed_weights.go: %v", err)
	}
	text := string(source)
	if strings.Contains(text, "DISTINCT ON (park_id, location_id, COALESCE(partition_label") {
		t.Fatal("shed weights must not split exact shed rows by legacy partition_label")
	}
	if !strings.Contains(text, "SELECT DISTINCT ON (park_id, location_id)") {
		t.Fatal("latest bucket selection must be keyed by exact shed location only")
	}
	if !strings.Contains(text, "''::text,") {
		t.Fatal("shed weights should return blank compatibility partition labels")
	}
}

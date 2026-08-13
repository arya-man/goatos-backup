package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestShedWeightsOneToManyPageBoundaryParkScopeStatusBucketsScopedProjectsPartitionLabel(t *testing.T) {
	source, err := os.ReadFile("shed_weights.go")
	if err != nil {
		t.Fatalf("read shed_weights.go: %v", err)
	}
	text := string(source)
	if !strings.Contains(text, "COALESCE(partition_label, '')") {
		t.Fatal("guard is stale: shed weights no longer uses partition_label in downstream CTEs")
	}
	if !strings.Contains(text, "cs.location_id, cs.partition_label, cs.weighing_category") {
		t.Fatal("scoped CTE must project cs.partition_label before downstream CTEs group/order by partition_label")
	}
}

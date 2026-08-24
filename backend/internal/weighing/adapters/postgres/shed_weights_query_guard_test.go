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

func TestShedWeightsResponseCarriesLumpCalendarMarkers(t *testing.T) {
	source, err := os.ReadFile("shed_weights.go")
	if err != nil {
		t.Fatalf("read shed_weights.go: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"LumpWeighingDates: []string{}",
		"SELECT DISTINCT to_char((sh.accepted_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD') AS weigh_date",
		"cs.weighing_category = 'per_shed_partition'",
		"out.LumpWeighingDates = append(out.LumpWeighingDates, day)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("shed weights marker query missing %q", want)
		}
	}
}

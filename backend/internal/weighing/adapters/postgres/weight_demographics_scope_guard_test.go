package postgres

import (
	"strings"
	"testing"
)

func TestWeightDemographicsResolvesBucketLocationsBeforeFallbackPartitions(t *testing.T) {
	src := readSource(t, "weight_demographics.go")
	// Both the complete and shed-only reads must resolve a physical bucket once,
	// independent of how many goat partition rows the fallback considers.
	if got := strings.Count(src, "FROM (SELECT DISTINCT tenant_id, location_id, partition_label FROM scoped) s\n  LEFT JOIN locations l ON l.location_id = s.location_id"); got != 2 {
		t.Fatalf("both shed target reads require one location lookup per distinct bucket, got %d", got)
	}
	if strings.Contains(src, "JOIN locations l ON l.location_id = s.location_id AND l.tenant_id = s.tenant_id") {
		t.Fatal("bucket location lookup must not return inside the correlated fallback join")
	}
	for _, predicate := range []string{
		"WHERE l.tenant_id = s.tenant_id", "AND phys.tenant_id = l.tenant_id",
		"AND phys.parent_location_id = l.parent_location_id", "AND phys.location_type = 'shed'",
		"AND gg.lifecycle_status = 'alive'", "live ON live.shed_id = s.location_id",
		"AND gsp.scrubbed_label = k.want_label", "AND phys.name = k.phys_name",
	} {
		if strings.Count(src, predicate) != 2 {
			t.Fatalf("both target reads must preserve %q", predicate)
		}
	}
}

func TestWeightDemographicsDirectResidentsKeepWholePartition(t *testing.T) {
	src := readSource(t, "weight_demographics.go")
	if strings.Count(src, "CASE WHEN live.present THEN '' ELSE") != 2 {
		t.Fatal("both demographic queries must preserve whole direct-resident sheds before inferring alias partitions")
	}
	if strings.Count(src, "COALESCE(NULLIF(s.partition_label, ''),\n                  -- A physical shed") != 2 {
		t.Fatal("explicit partition labels must win over direct-resident whole-shed fallback")
	}
}

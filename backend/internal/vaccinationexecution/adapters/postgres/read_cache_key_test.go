package postgres

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestVaccinationCacheExactTimeDoesNotBucketSnapshots(t *testing.T) {
	first := time.Date(2026, 9, 5, 12, 0, 1, 123, time.UTC)
	second := time.Date(2026, 9, 5, 12, 4, 59, 456, time.UTC)

	if vaccinationCacheTimeBucket(first) != vaccinationCacheTimeBucket(second) {
		t.Fatal("test setup must keep both timestamps in the same legacy bucket")
	}
	if vaccinationCacheExactTime(first) == vaccinationCacheExactTime(second) {
		t.Fatal("exact cache timestamps must not collide inside a 5-minute bucket")
	}
}

func TestVaccinationCacheExactTimeNormalizesToUTC(t *testing.T) {
	ist := time.FixedZone("IST", 5*60*60+30*60)
	at := time.Date(2026, 9, 5, 17, 30, 0, 789, ist)

	got := vaccinationCacheExactTime(at)
	if !strings.HasSuffix(got, "Z") {
		t.Fatalf("exact cache timestamp = %q, want UTC RFC3339Nano", got)
	}
}

func TestCommandBoardCachesUseExactAsOfSnapshots(t *testing.T) {
	commandBoard, err := os.ReadFile("commandboard.go")
	if err != nil {
		t.Fatal(err)
	}
	drilldown, err := os.ReadFile("commandboard_drilldown.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(commandBoard) + "\n" + string(drilldown)
	for _, required := range []string{
		`"command_board", strings.TrimSpace(q.TenantID), vaccinationCacheExactTime(asOf)`,
		`"command_board_cohort_matrix", strings.TrimSpace(q.TenantID), vaccinationCacheExactTime(asOf)`,
		`"command_board_shed_dose_matrix", strings.TrimSpace(q.TenantID), vaccinationCacheExactTime(asOf)`,
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("command-board cache key must preserve exact as_of snapshot; missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`"command_board", strings.TrimSpace(q.TenantID), vaccinationCacheTimeBucket(asOf)`,
		`"command_board_cohort_matrix", strings.TrimSpace(q.TenantID), vaccinationCacheTimeBucket(asOf)`,
		`"command_board_shed_dose_matrix", strings.TrimSpace(q.TenantID), vaccinationCacheTimeBucket(asOf)`,
	} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("command-board cache key must not bucket explicit as_of snapshots; found %q", forbidden)
		}
	}
}

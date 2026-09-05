package postgres

import (
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

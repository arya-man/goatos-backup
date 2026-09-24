package postgres

import (
	"os"
	"strings"
	"testing"
	"time"
)

// The vaccination read caches keyed on the request instant (RFC3339Nano). The handler sets
// as_of = now() with nanosecond precision, so no two requests ever shared a key and every cache
// was dead (docs/perf/2026-09-24-stg-latency/audit/part-vaccination.md, root cause 2). The key is
// the IST business date plus a 30 s floor: requests inside one 30 s window share one entry, and a
// committed vaccination write evicts it (migration 000411 triggers -> readcache listener).
func TestVaccinationCacheAsOfKeySharesA30sBucket(t *testing.T) {
	first := time.Date(2026, 9, 5, 12, 0, 1, 123, time.UTC)
	second := time.Date(2026, 9, 5, 12, 0, 29, 456, time.UTC)
	if vaccinationCacheAsOfKey(first) != vaccinationCacheAsOfKey(second) {
		t.Fatalf("requests 28s apart in one 30s bucket must share a cache key: %q vs %q",
			vaccinationCacheAsOfKey(first), vaccinationCacheAsOfKey(second))
	}
	third := time.Date(2026, 9, 5, 12, 0, 30, 0, time.UTC)
	if vaccinationCacheAsOfKey(first) == vaccinationCacheAsOfKey(third) {
		t.Fatal("the next 30s bucket must be a new key")
	}
}

func TestVaccinationCacheAsOfKeyCarriesTheISTBusinessDate(t *testing.T) {
	// 18:29:50 UTC and 18:30:10 UTC are different IST business dates (23:59:50 vs 00:00:10 IST).
	before := time.Date(2026, 9, 5, 18, 29, 50, 0, time.UTC)
	after := time.Date(2026, 9, 5, 18, 30, 10, 0, time.UTC)
	if !strings.HasPrefix(vaccinationCacheAsOfKey(before), "2026-09-05") || !strings.HasPrefix(vaccinationCacheAsOfKey(after), "2026-09-06") {
		t.Fatalf("keys must lead with the IST business date: %q, %q", vaccinationCacheAsOfKey(before), vaccinationCacheAsOfKey(after))
	}
	ist := time.FixedZone("IST", 5*60*60+30*60)
	if vaccinationCacheAsOfKey(before) != vaccinationCacheAsOfKey(before.In(ist)) {
		t.Fatal("the key must not depend on the caller's time zone")
	}
}

// Guard: no vaccination read-cache key may carry the raw request instant again.
func TestVaccinationReadCacheKeysNeverUseTheRequestInstant(t *testing.T) {
	for _, file := range []string{"commandboard.go", "commandboard_drilldown.go", "repository.go", "execution_combined.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"vaccinationCacheExactTime(", "RFC3339Nano), dueBefore", "getVaccinationReadCache(", "setVaccinationReadCache("} {
			if strings.Contains(string(src), forbidden) {
				t.Fatalf("%s: vaccination cache keys must use vaccinationCacheAsOfKey and the shared readcache; found %q", file, forbidden)
			}
		}
	}
}

func TestVaccinationExecutionSideReadsAvoidPreparedGenericPlan(t *testing.T) {
	srcBytes, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)
	for _, required := range []string{
		`r.pool.Query(ctx, vaccinationExecutionCarrySummarySQL, pgx.QueryExecModeExec, q.TenantID, q.OperatorScopeActorID, q.AsOf, q.DueBefore)`,
		`r.pool.Query(ctx, cardSummariesSQL, pgx.QueryExecModeExec,`,
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("vaccination execution repeated app reads must force exec mode to avoid slow prepared generic plans; missing %q", required)
		}
	}
}

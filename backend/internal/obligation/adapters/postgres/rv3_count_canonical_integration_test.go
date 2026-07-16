package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
)

// These four tests are the RV-03 canonicalization-robustness companions to the round-1
// TestCountVisitShots* suite. RV-03 canonicalizes every UUID before it is folded into an advisory-
// lock key so equivalent representations (upper- vs lower-case hex of the SAME uuid) map to one
// lock. CountVisitShotsForTargets is the read half of that same shot-cap subsystem: callers that
// hold a differently-cased UUID string for a tenant/target must still get the CORRECT per-target
// shot count, on the SAME grain, from the SAME query the lock path protects. That is the property
// verified here, distinct from the round-1 tests (which only ever pass canonical lowercase inputs
// and therefore never exercise the mixed-case path RV-03 made safe).
//
// countVisitShots keys its result map on Postgres's oi.target_id::text, which always renders the
// canonical lowercase form regardless of the case a caller passed in. So each test passes UPPER-case
// tenant/target UUIDs into CountVisitShotsForTargets and asserts on the lowercase (canonical) key --
// proving BOTH that an uppercase input still matches the underlying rows (Postgres compares uuid
// VALUES, case-insensitively) AND that the per-target count grain is unchanged by the input case.
// The goat ids below all contain hex letters (a-f), so upper-casing them genuinely changes the
// string; the all-digit tenant constant is upper-cased too (a harmless no-op that documents intent).

// TestCountVisitShotsMixedCaseTenantOneToManyCountsEachShotOnce proves the 1:1 semijoin grain
// (each shot counted exactly once, never a JOIN fan-out) holds when the tenant/target UUIDs are
// passed in a DIFFERENT case than they were stored: one goat with two shots in two SEPARATE
// non-canceled batches on the same planned_date must still count 2, looked up by the canonical key.
func TestCountVisitShotsMixedCaseTenantOneToManyCountsEachShotOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.canon.onetomany", 2)

	const goatID = "10000000-0000-4000-8000-00000000fa01"
	const shedID = "00000000-0000-4000-8000-00000000da01"
	seedParkConsolidationShed(t, ctx, pool, shedID, "canon-onetomany-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, batchA := seedShotOnDate(t, ctx, repo, versions[0], goatID, "shed", shedID, date, date, "canon-onetomany-A")
	_, batchB := seedShotOnDate(t, ctx, repo, versions[1], goatID, "shed", shedID, date, date, "canon-onetomany-B")
	if batchA == batchB {
		t.Fatalf("fixture invalid: both shots landed in the same batch %s, want two separate batches", batchA)
	}

	// Pass BOTH ids in a case different from how they were stored.
	upperGoat := strings.ToUpper(goatID)
	if upperGoat == goatID {
		t.Fatalf("fixture invalid: goat id %q has no hex letters, so upper-casing does not change it", goatID)
	}
	counts, err := repo.CountVisitShotsForTargets(ctx, strings.ToUpper(tenantID), []string{upperGoat}, date)
	if err != nil {
		t.Fatalf("CountVisitShotsForTargets (mixed case): %v", err)
	}
	if counts[goatID] != 2 {
		t.Fatalf("count[%s] = %d, want 2 (two shots in two batches, each counted once; mixed-case input must not change the grain)", goatID, counts[goatID])
	}
	// The uppercase string is NOT a valid map key (Postgres canonicalizes it away), which is exactly
	// why a caller must resolve counts by the canonical form -- assert that explicitly.
	if counts[upperGoat] != 0 {
		t.Fatalf("count is keyed on the canonical lowercase target id, not the caller's uppercase string; got count[%s]=%d", upperGoat, counts[upperGoat])
	}
}

// TestCountVisitShotsMixedCaseMultiPageTargetsStable proves the per-target count is a pure function
// of each target's own committed shots and is invariant to BOTH how the caller chunks its target-id
// list AND the case of the ids in each chunk. Three goats with 1 / 2 / 1 shots are counted as the
// full set and as arbitrary mixed-case subsets; every per-target count is identical.
func TestCountVisitShotsMixedCaseMultiPageTargetsStable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.canon.multipage", 2)

	const goatA = "10000000-0000-4000-8000-00000000fb01"
	const goatB = "10000000-0000-4000-8000-00000000fb02"
	const goatC = "10000000-0000-4000-8000-00000000fb03"
	const shedID = "00000000-0000-4000-8000-00000000db01"
	seedParkConsolidationShed(t, ctx, pool, shedID, "canon-multipage-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatA, goatB, goatC)

	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	seedShotOnDate(t, ctx, repo, versions[0], goatA, "shed", shedID, date, date, "canon-multipage-A0")
	seedShotOnDate(t, ctx, repo, versions[0], goatB, "shed", shedID, date, date, "canon-multipage-B0")
	seedShotOnDate(t, ctx, repo, versions[1], goatB, "shed", shedID, date, date, "canon-multipage-B1")
	seedShotOnDate(t, ctx, repo, versions[1], goatC, "shed", shedID, date, date, "canon-multipage-C1")

	// Full set passed all-uppercase: canonical (lowercase) keys carry the counts.
	full, err := repo.CountVisitShotsForTargets(ctx, tenantID, []string{strings.ToUpper(goatA), strings.ToUpper(goatB), strings.ToUpper(goatC)}, date)
	if err != nil {
		t.Fatalf("full-set count (mixed case): %v", err)
	}
	if full[goatA] != 1 || full[goatB] != 2 || full[goatC] != 1 {
		t.Fatalf("full-set counts = A:%d B:%d C:%d, want A:1 B:2 C:1", full[goatA], full[goatB], full[goatC])
	}

	// Every chunking -- with mixed casing per chunk -- must yield the SAME per-animal count.
	pages := [][]string{
		{strings.ToUpper(goatA)},
		{goatB},
		{strings.ToUpper(goatC)},
		{strings.ToUpper(goatA), goatC},
		{goatB, strings.ToUpper(goatA)},
	}
	for _, page := range pages {
		got, err := repo.CountVisitShotsForTargets(ctx, strings.ToUpper(tenantID), page, date)
		if err != nil {
			t.Fatalf("page %v count: %v", page, err)
		}
		for _, target := range page {
			canonical := strings.ToLower(target)
			if got[canonical] != full[canonical] {
				t.Fatalf("page %v: count[%s] = %d, want %d (per-target count must not depend on page composition OR input case)", page, canonical, got[canonical], full[canonical])
			}
		}
	}
}

// TestCountVisitShotsDateShiftExcludesOtherDatesCanonical proves the count stays scoped to exactly
// the queried planned_date even when the tenant/target ids are supplied in a different case: a goat
// with one shot on D and another on D+7 counts 1 on D and 1 on D+7 -- the D+7 batch never bleeds
// into the D count.
func TestCountVisitShotsDateShiftExcludesOtherDatesCanonical(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.canon.dateshift", 2)

	const goatID = "10000000-0000-4000-8000-00000000fc01"
	const shedID = "00000000-0000-4000-8000-00000000dc01"
	seedParkConsolidationShed(t, ctx, pool, shedID, "canon-dateshift-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	dateD := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	dateD7 := dateD.AddDate(0, 0, 7)
	seedShotOnDate(t, ctx, repo, versions[0], goatID, "shed", shedID, dateD, dateD, "canon-dateshift-D")
	seedShotOnDate(t, ctx, repo, versions[1], goatID, "shed", shedID, dateD7, dateD7, "canon-dateshift-D7")

	upperGoat := strings.ToUpper(goatID)
	onD, err := repo.CountVisitShotsForTargets(ctx, strings.ToUpper(tenantID), []string{upperGoat}, dateD)
	if err != nil {
		t.Fatalf("count on D (mixed case): %v", err)
	}
	if onD[goatID] != 1 {
		t.Fatalf("count on D = %d, want 1 (only the D shot; the D+7 shot must be excluded by planned_date scoping regardless of input case)", onD[goatID])
	}
	onD7, err := repo.CountVisitShotsForTargets(ctx, strings.ToUpper(tenantID), []string{upperGoat}, dateD7)
	if err != nil {
		t.Fatalf("count on D+7 (mixed case): %v", err)
	}
	if onD7[goatID] != 1 {
		t.Fatalf("count on D+7 = %d, want 1 (only the D+7 shot)", onD7[goatID])
	}
}

// TestCountVisitShotsStatusBucketsExcludesCanceledCanonical proves the status filter
// (ob.status NOT IN ('canceled','superseded') AND oi.status NOT IN ('canceled')) still excludes the
// released/superseded/canceled buckets when the tenant/target ids are supplied in a different case:
// a goat with one ACTIVE shot plus a canceled-batch shot, a superseded-batch shot, and a
// canceled-obligation shot counts exactly 1.
func TestCountVisitShotsStatusBucketsExcludesCanceledCanonical(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.canon.statusbuckets", 4)

	const goatID = "10000000-0000-4000-8000-00000000fd01"
	const shedID = "00000000-0000-4000-8000-00000000dd01"
	seedParkConsolidationShed(t, ctx, pool, shedID, "canon-statusbuckets-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// 0: active (planned batch, scheduled obligation) -> counts.
	seedShotOnDate(t, ctx, repo, versions[0], goatID, "shed", shedID, date, date, "canon-status-active")
	// 1: batch canceled -> excluded by ob.status filter.
	_, canceledBatch := seedShotOnDate(t, ctx, repo, versions[1], goatID, "shed", shedID, date, date, "canon-status-batch-canceled")
	// 2: batch superseded -> excluded by ob.status filter.
	_, supersededBatch := seedShotOnDate(t, ctx, repo, versions[2], goatID, "shed", shedID, date, date, "canon-status-batch-superseded")
	// 3: obligation canceled (batch still planned) -> excluded by oi.status filter.
	canceledObl, _ := seedShotOnDate(t, ctx, repo, versions[3], goatID, "shed", shedID, date, date, "canon-status-obl-canceled")

	if _, err := pool.Exec(ctx, `UPDATE obligation_batches SET status='canceled' WHERE tenant_id=$1 AND batch_id=$2`, tenantID, canceledBatch); err != nil {
		t.Fatalf("cancel batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_batches SET status='superseded' WHERE tenant_id=$1 AND batch_id=$2`, tenantID, supersededBatch); err != nil {
		t.Fatalf("supersede batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, canceledObl); err != nil {
		t.Fatalf("cancel obligation: %v", err)
	}

	counts, err := repo.CountVisitShotsForTargets(ctx, strings.ToUpper(tenantID), []string{strings.ToUpper(goatID)}, date)
	if err != nil {
		t.Fatalf("CountVisitShotsForTargets (mixed case): %v", err)
	}
	if counts[goatID] != 1 {
		t.Fatalf("count = %d, want 1 (only the active shot; canceled batch, superseded batch, and canceled obligation must all be excluded regardless of input case)", counts[goatID])
	}
}

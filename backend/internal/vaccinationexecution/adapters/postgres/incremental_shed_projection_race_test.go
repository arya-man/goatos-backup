package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestRebuildShedShardSerializesAgainstConcurrentFullRebuild is the P0 cross-projector version-race
// regression test (see the header comment on incremental_shed_projection.go). Before the fix,
// RebuildShedShard read serving_projection_version ONCE at tx start with no lock; a concurrent
// RecomputeShedProjection could commit a brand-new version, flip the serving pointer, and prune the
// old version entirely while RebuildShedShard was still mid-flight, so RebuildShedShard would blindly
// upsert into the now-pruned version and stamp vaccination_shed_shard_state 'fresh' -- a silently
// lost incremental write plus a falsely-fresh read-time gate.
//
// This test forces exactly that interleaving: a test-only hook fires the instant RebuildShedShard
// has captured its serving version (under FOR SHARE), and from there kicks off a concurrent full
// RecomputeShedProjection for the SAME tenant. It proves two things:
//  1. The concurrent RecomputeShedProjection's flip cannot COMMIT until RebuildShedShard's own
//     transaction ends -- the FOR SHARE lock genuinely serializes the two operations, closing the
//     window where a full rebuild's commit+prune could land while RebuildShedShard is mid-computation.
//  2. RebuildShedShard, having captured the lock first, completes normally (never Deferred or
//     VersionConflict) and the eventual actually-served projection (the concurrent recompute's new
//     version) still independently reflects the same fresh canonical state -- so no observable
//     staleness survives the race even though RebuildShedShard's own write target is later pruned.
func TestRebuildShedShardSerializesAgainstConcurrentFullRebuild(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	seedIncrementalShardSheds(t, ctx, pool)

	goatID := "70000000-0000-4000-8000-000000000150"
	insertProjectionGoat(t, ctx, pool, goatID, testShedIncA, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000151", testShedIncA,
		goatID, "scheduled", "2026-06-20 00:00:00+00", "race-a-1")

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	bootstrap, err := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{TenantID: testTenant, AsOf: asOf})
	if err != nil {
		t.Fatalf("bootstrap RecomputeShedProjection: %v", err)
	}
	v1 := bootstrap.ProjectionVersion

	// A second goat shows up in shed A -- the write RebuildShedShard(A) is reacting to. It is
	// committed to canonical goats/obligations BEFORE either projector call below runs, so both the
	// incremental rebuild and the concurrent full recompute independently see it.
	insertProjectionGoat(t, ctx, pool, "70000000-0000-4000-8000-000000000152", testShedIncA, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000153", testShedIncA,
		"70000000-0000-4000-8000-000000000152", "scheduled", "2026-06-01 00:00:00+00", "race-a-2")

	recomputeCommittedAt := make(chan time.Time, 1)
	recomputeErr := make(chan error, 1)
	rebuildShedShardTestHookAfterInitialRead = func() {
		go func() {
			// Runs concurrently while RebuildShedShard's own transaction still holds FOR SHARE on
			// the tenant's vaccination_shed_projection_state row. This full recompute's flip UPDATE
			// needs an exclusive lock on that SAME row, so it can only commit after RebuildShedShard
			// ends (commits or rolls back) -- exactly the serialization under test.
			_, recErr := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{TenantID: testTenant, AsOf: asOf})
			recomputeErr <- recErr
			recomputeCommittedAt <- time.Now()
		}()
		// Give the background recompute a moment to reach (and block on) its flip before letting
		// RebuildShedShard continue, so the overlap is real rather than incidental.
		time.Sleep(150 * time.Millisecond)
	}
	defer func() { rebuildShedShardTestHookAfterInitialRead = nil }()

	result, err := repo.RebuildShedShard(ctx, domain.RebuildShedShardRequest{TenantID: testTenant, ShedID: testShedIncA, AsOf: asOf})
	rebuildDoneAt := time.Now()
	if err != nil {
		t.Fatalf("RebuildShedShard: %v", err)
	}
	if result.Deferred || result.VersionConflict {
		t.Fatalf("RebuildShedShard result = %#v, want a plain successful write (it captured the serving version lock first)", result)
	}
	if result.ProjectionVersion != v1 {
		t.Fatalf("RebuildShedShard wrote projection_version %d, want the version it captured at start (%d)", result.ProjectionVersion, v1)
	}

	select {
	case recErr := <-recomputeErr:
		if recErr != nil {
			t.Fatalf("concurrent RecomputeShedProjection: %v", recErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("concurrent RecomputeShedProjection did not finish in time")
	}
	committedAt := <-recomputeCommittedAt
	if !committedAt.After(rebuildDoneAt) {
		t.Fatalf("concurrent RecomputeShedProjection finished at %s, want strictly after RebuildShedShard finished at %s -- the FOR SHARE lock did not serialize the two operations", committedAt, rebuildDoneAt)
	}

	// The now-serving version (from the concurrent recompute) independently recomputed shed A from
	// the same already-committed canonical goats/obligations, so even though RebuildShedShard's own
	// write landed in the now-pruned v1, the actually-served state for shed A is still correct: no
	// silent staleness survives the race.
	v2, ok := repo.shedProjectionServingVersion(ctx, testTenant)
	if !ok {
		t.Fatalf("expected a serving version after the concurrent recompute")
	}
	if v2 == v1 {
		t.Fatalf("serving version did not change (%d); the concurrent recompute never actually landed", v2)
	}
	served := readShedRow(t, ctx, pool, testTenant, v2, testShedIncA)
	if served.Animals != 2 {
		t.Fatalf("served (current) shed A animals = %d, want 2 -- the concurrent full recompute must independently reflect the same fresh canonical state RebuildShedShard computed", served.Animals)
	}

	// v1 (RebuildShedShard's own write target) is pruned once it is no longer the serving version --
	// proving the write was NOT left behind as permanent orphaned/undiscoverable data either.
	if rows := readShedProjectionRows(t, ctx, pool, testTenant, v1); len(rows) != 0 {
		t.Fatalf("orphaned version %d still has %d rows after the concurrent recompute pruned it", v1, len(rows))
	}
}

// TestRebuildShedShardAbortsWhenServingVersionAlreadyMoved is a direct, deterministic proof of the
// abort branch itself (RebuildShedShardResult.VersionConflict). Under the FOR SHARE design in this
// file, a genuine version change between the initial read and the pre-write recheck cannot occur in
// production while the lock is held correctly -- that invariant IS the fix, proven for real
// concurrent access by TestRebuildShedShardSerializesAgainstConcurrentFullRebuild above. This test
// instead uses a narrow fault-injection seam (rebuildShedShardTestOverrideRecheckVersion) to force the
// recheck to observe a different version than the one initially captured, and proves the abort branch
// itself does the right thing when triggered: no row written for the new/mismatched version, no
// shard_state stamp, and a VersionConflict=true result the worker can use to re-enqueue.
func TestRebuildShedShardAbortsWhenServingVersionAlreadyMoved(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	seedIncrementalShardSheds(t, ctx, pool)

	goatID := "70000000-0000-4000-8000-000000000160"
	insertProjectionGoat(t, ctx, pool, goatID, testShedIncA, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000161", testShedIncA,
		goatID, "scheduled", "2026-06-20 00:00:00+00", "abort-a-1")

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	bootstrap, err := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{TenantID: testTenant, AsOf: asOf})
	if err != nil {
		t.Fatalf("bootstrap RecomputeShedProjection: %v", err)
	}
	v1 := bootstrap.ProjectionVersion

	// The bootstrap recompute already seeds a row for shed A (1 animal) and its shard_state
	// (row_present=true, projected_at=bootstrap time) -- capture both as the "before" baseline.
	beforeRow := readShedRow(t, ctx, pool, testTenant, v1, testShedIncA)
	if beforeRow.Animals != 1 {
		t.Fatalf("precondition: bootstrap shed A animals = %d, want 1", beforeRow.Animals)
	}
	beforeState := readShardState(t, ctx, pool, testTenant, testShedIncA)
	if !beforeState.RowPresent {
		t.Fatalf("precondition: bootstrap should have seeded shard_state row_present=true for shed A")
	}

	// A second goat appears in shed A -- if the aborted rebuild below wrote anything at all, this is
	// what it would have written (animals=2). It must NOT show up in the assertions afterward.
	insertProjectionGoat(t, ctx, pool, "70000000-0000-4000-8000-000000000162", testShedIncA, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000163", testShedIncA,
		"70000000-0000-4000-8000-000000000162", "scheduled", "2026-06-01 00:00:00+00", "abort-a-2")

	// Force the pre-write recheck to see a version that differs from v1 (what RebuildShedShard
	// captured at the start) -- simulating "the serving version moved out from under us".
	rebuildShedShardTestOverrideRecheckVersion = func(actual int64) int64 {
		if actual != v1 {
			t.Fatalf("recheck observed %d before override, want the unchanged captured version %d", actual, v1)
		}
		return actual + 999
	}
	defer func() { rebuildShedShardTestOverrideRecheckVersion = nil }()

	result, err := repo.RebuildShedShard(ctx, domain.RebuildShedShardRequest{TenantID: testTenant, ShedID: testShedIncA, AsOf: asOf})
	if err != nil {
		t.Fatalf("RebuildShedShard: %v", err)
	}
	if !result.VersionConflict {
		t.Fatalf("RebuildShedShard result = %#v, want VersionConflict=true", result)
	}
	if result.Deferred {
		t.Fatalf("RebuildShedShard result = %#v, want Deferred=false (a serving version DID exist, it just moved)", result)
	}

	// Nothing was committed by the aborted attempt: the v1 row for shed A must be BYTE-FOR-BYTE the
	// bootstrap row (still animals=1, not the fresher animals=2 the aborted compute would have
	// written), and shard_state must be untouched (same projected_at/as_of as before this call) --
	// proving the abort neither wrote the doomed version nor falsely stamped the shed fresh.
	afterRow := readShedRow(t, ctx, pool, testTenant, v1, testShedIncA)
	if !shedProjectionRowsEqual(afterRow, beforeRow) {
		t.Fatalf("aborted RebuildShedShard changed shed A's row: before=%#v after=%#v", beforeRow, afterRow)
	}
	afterState := readShardState(t, ctx, pool, testTenant, testShedIncA)
	if !afterState.ProjectedAt.Equal(beforeState.ProjectedAt) {
		t.Fatalf("aborted RebuildShedShard stamped shard_state.projected_at (%s -> %s); an aborted attempt must not mark the shed fresh", beforeState.ProjectedAt, afterState.ProjectedAt)
	}
}

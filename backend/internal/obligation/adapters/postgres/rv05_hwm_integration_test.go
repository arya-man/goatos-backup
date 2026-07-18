package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
)

// rv05VaccineIdentity builds a SweepConfig for one plan (protocol version + its single rule) with
// an explicit, equal vaccine priority across all RV-05 test plans, so three plans genuinely compete
// for one animal's over-cap visit instead of resolving by priority order.
func rv05VaccineIdentity(v struct{ versionID, ruleID string }, vaccineCode string, priority int32, planner domain.DrivePlannerSettings) oblapp.SweepConfig {
	return oblapp.SweepConfig{
		VaccineCode:  vaccineCode,
		DrivePlanner: planner,
		RuleVaccineIDs: map[string]oblapp.RuleVaccineIdentity{
			v.ruleID: {VaccineCode: vaccineCode, VaccinePriority: priority},
		},
	}
}

// TestPreflightPlusRealSweepHWMExcludesPostPreflightObligation is the RV-05 guard. It reproduces
// the exact race the finding describes:
//
//  1. Two equal-priority obligations (vaccines FMD, PPR) are due now for one animal's visit, cap=2.
//  2. A high-water mark is captured (SweeperService.CaptureSweepHighWaterMark) -- mirroring
//     kernelstages.ObligationSweeperStage.Run capturing it right after LockTenantSweep, before
//     PreflightVisitShotCapTies.
//  3. PreflightVisitShotCapTies runs against that HWM: clean, since only 2 obligations existed at
//     that instant and they exactly fill the cap.
//  4. A THIRD, equal-priority obligation (vaccine HS) is generated for the SAME animal/visit AFTER
//     the HWM was captured -- exactly like vaccination/app's generation.go/booster.go
//     InsertObligation, which does not participate in LockTenantSweep and can race an in-progress
//     sweep at any point.
//  5. The real per-version sweep for all three plans runs, bound by the SAME frozen HWM via
//     SweepVersionWithSessionNoFinalizeHWM.
//
// Before RV-05, the real sweep's read had no way to exclude the third obligation: it would see all
// 3 candidates, batch FMD and PPR (2/2, filling the cap), and then hit vaccine HS at cap with an
// equal priority to the last claimant -- an unresolved *ShotCapPriorityTieError -- AFTER FMD/PPR's
// batches were ALREADY durably committed. That is the silent partial medical plan the preflight
// gate exists to prevent, reopened by a generator that races outside the tenant-sweep lock.
//
// After RV-05, the third obligation's created_at is strictly after the frozen HWM, so it is
// invisible to this cycle's real sweep read entirely: the run completes with NO error, FMD/PPR are
// batched normally, and the third obligation is left unbatched (batch_id IS NULL) to be picked up by
// next cycle's own fresh preflight/HWM pair.
//
// This test only compiles against the RV-05 fix (CaptureSweepHighWaterMark,
// SweepVersionWithSessionNoFinalizeHWM, and PreflightVisitShotCapTies's createdAtHWM parameter did
// not exist before it), so "fails before / passes after" manifests as a build failure against the
// pre-fix repo/sweeper packages -- expected for a net-new capability, not a behavior change to an
// existing call. TestUnboundedPreflightThenSweepStillPartialCommitsWithoutHWM below independently
// reproduces the SAME underlying race using only the pre-existing, still-present unbounded entry
// points, so the hazard itself (not just the new API's existence) is demonstrated at runtime too.
func TestPreflightPlusRealSweepHWMExcludesPostPreflightObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.rv05hwm", 3)

	const goatID = "10000000-0000-4000-8000-00000000f401"
	const shedID = "00000000-0000-4000-8000-00000000d401"
	seedParkConsolidationShed(t, ctx, pool, shedID, "rv05-hwm-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// A wide window (5 days) so a rejection is a genuine PRIORITY TIE, not an ordinary date overflow
	// (see rv3_guard_test.go's TestPreflightDetectsSingleVersionMatrixTie, same shape).
	windowEnd := due.AddDate(0, 0, 5)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}

	planA := oblapp.SweepVersionPriority{VersionID: versions[0].versionID, Config: rv05VaccineIdentity(versions[0], "FMD", 5, planner)}
	planB := oblapp.SweepVersionPriority{VersionID: versions[1].versionID, Config: rv05VaccineIdentity(versions[1], "PPR", 5, planner)}
	planC := oblapp.SweepVersionPriority{VersionID: versions[2].versionID, Config: rv05VaccineIdentity(versions[2], "HS", 5, planner)}

	for i, plan := range []struct {
		v    struct{ versionID, ruleID string }
		code string
	}{{versions[0], "FMD"}, {versions[1], "PPR"}} {
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: plan.v.versionID, RuleID: plan.v.ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: "rv05hwm-pre-" + plan.code, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert pre-hwm obligation %d: applied=%v err=%v", i, applied, err)
		}
	}

	sweeper := oblapp.NewSweeperService(repo, nil, nil)

	hwm, err := sweeper.CaptureSweepHighWaterMark(ctx)
	if err != nil {
		t.Fatalf("capture high-water mark: %v", err)
	}
	if hwm.IsZero() {
		t.Fatalf("captured high-water mark is zero, want the database server's current time")
	}

	// Preflight against the frozen candidate set (only FMD + PPR exist): must be clean, they exactly
	// fill the two-shot cap with no tie.
	if err := sweeper.PreflightVisitShotCapTies(ctx, tenantID, []oblapp.SweepVersionPriority{planA, planB}, due, hwm); err != nil {
		t.Fatalf("preflight before the third obligation existed: %v, want nil", err)
	}

	// RACE: vaccine HS becomes due for the SAME animal/visit AFTER the HWM snapshot but BEFORE the
	// real sweep below -- exactly like a concurrent vaccination/app generation call, which does not
	// hold the tenant-sweep lock and can run at any point during an in-progress sweep.
	if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: planC.VersionID, RuleID: versions[2].ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
		DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: "rv05hwm-post-HS", Sequence: 1,
	}); err != nil || !applied {
		t.Fatalf("insert post-hwm obligation: applied=%v err=%v", applied, err)
	}

	session := oblapp.NewSweepSession()
	for _, plan := range []oblapp.SweepVersionPriority{planA, planB, planC} {
		if _, err := sweeper.SweepVersionWithSessionNoFinalizeHWM(ctx, tenantID, plan.VersionID, plan.Config, due, session, hwm); err != nil {
			t.Fatalf("real sweep version %s returned an error (want nil -- the HWM must exclude the post-preflight obligation instead of tying): %v", plan.VersionID, err)
		}
	}

	// The drive planner is free to pick any feasible date within the wide window (not necessarily
	// `due` itself), and FMD/PPR -- competing for the same visit -- always resolve to the SAME
	// picked date, so counting committed shots for the goat across ALL non-canceled/superseded
	// batches (without pinning a specific planned_date) is the correct, planner-agnostic assertion.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances oi JOIN obligation_batches b ON b.batch_id = oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2
  AND b.status NOT IN ('canceled','superseded')`, tenantID, goatID); got != 2 {
		t.Fatalf("shots committed for goat %s = %d, want 2 (FMD + PPR, at cap)", goatID, got)
	}

	var thirdBatchID *string
	if err := pool.QueryRow(ctx, `
SELECT batch_id::text FROM obligation_instances WHERE tenant_id=$1 AND idempotency_key=$2`,
		tenantID, "rv05hwm-post-HS").Scan(&thirdBatchID); err != nil {
		t.Fatalf("read third obligation: %v", err)
	}
	if thirdBatchID != nil {
		t.Fatalf("third (post-HWM) obligation was batched this cycle (batch_id=%s), want it excluded/deferred to next cycle", *thirdBatchID)
	}
}

// TestPreflightSnapshotExcludesPreexistingRowReopenedAfterPreflight is the counter-review guard for
// the gap a created_at-only HWM cannot close. HS already exists before the HWM, but is deferred and
// therefore absent from preflight. Reopening it after preflight does not change created_at, so the
// old HWM-only real sweep admitted it and raised a same-priority tie after FMD/PPR had committed.
// The returned snapshot allowlist makes candidate membership monotonic for the cycle: HS waits for
// the next cycle even though its current status becomes scheduled.
func TestPreflightSnapshotExcludesPreexistingRowReopenedAfterPreflight(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.rv05snapshot", 3)

	const goatID = "10000000-0000-4000-8000-00000000f403"
	const shedID = "00000000-0000-4000-8000-00000000d403"
	seedParkConsolidationShed(t, ctx, pool, shedID, "rv05-snapshot-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := due.AddDate(0, 0, 5)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}
	plans := []oblapp.SweepVersionPriority{
		{VersionID: versions[0].versionID, Config: rv05VaccineIdentity(versions[0], "FMD", 5, planner)},
		{VersionID: versions[1].versionID, Config: rv05VaccineIdentity(versions[1], "PPR", 5, planner)},
		{VersionID: versions[2].versionID, Config: rv05VaccineIdentity(versions[2], "HS", 5, planner)},
	}

	for i, status := range []string{"scheduled", "scheduled", "deferred"} {
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versions[i].versionID, RuleID: versions[i].ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &windowEnd, Status: status,
			IdempotencyKey: "rv05snapshot-" + []string{"FMD", "PPR", "HS"}[i], Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert obligation %d: applied=%v err=%v", i, applied, err)
		}
	}

	sweeper := oblapp.NewSweeperService(repo, nil, nil)
	hwm, err := sweeper.CaptureSweepHighWaterMark(ctx)
	if err != nil {
		t.Fatalf("capture high-water mark: %v", err)
	}
	snapshot, err := sweeper.PreflightVisitShotCapTiesWithSnapshot(ctx, tenantID, plans, due, hwm)
	if err != nil {
		t.Fatalf("preflight before reopen: %v", err)
	}

	if _, changed, err := repo.ReopenDeferredObligationByIdempotencyKey(ctx, tenantID, "rv05snapshot-HS", due, nil); err != nil || !changed {
		t.Fatalf("reopen pre-existing HS after preflight: changed=%v err=%v", changed, err)
	}

	session := oblapp.NewSweepSession()
	for _, plan := range plans {
		if _, err := sweeper.SweepVersionWithSessionNoFinalizeSnapshot(ctx, tenantID, plan.VersionID, plan.Config, due, session, hwm, snapshot); err != nil {
			t.Fatalf("snapshot sweep version %s: %v", plan.VersionID, err)
		}
	}

	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances oi JOIN obligation_batches b ON b.batch_id = oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2
  AND b.status NOT IN ('canceled','superseded')`, tenantID, goatID); got != 2 {
		t.Fatalf("shots committed after reopen race = %d, want 2 (FMD + PPR only)", got)
	}
	var hsBatchID *string
	if err := pool.QueryRow(ctx, `
SELECT batch_id::text FROM obligation_instances WHERE tenant_id=$1 AND idempotency_key=$2`,
		tenantID, "rv05snapshot-HS").Scan(&hsBatchID); err != nil {
		t.Fatalf("read reopened HS: %v", err)
	}
	if hsBatchID != nil {
		t.Fatalf("reopened HS was batched in the preflight cycle (batch_id=%s), want deferred to next cycle", *hsBatchID)
	}
}

// TestUnboundedPreflightThenSweepStillPartialCommitsWithoutHWM independently demonstrates the RV-05
// hazard at runtime using ONLY the pre-existing unbounded entry points (zero createdAtHWM /
// SweepVersionWithSessionNoFinalize): with no shared high-water mark, a same-priority obligation
// generated between preflight and the real sweep DOES cause exactly the partial commit RV-05
// closes -- FMD/PPR batch, then vaccine HS's real-sweep call raises *ShotCapPriorityTieError. This
// documents WHY production orchestration (kernelstages, cmd/obligation-sweeper) must call the HWM
// variants, independent of whether the new API compiles.
func TestUnboundedPreflightThenSweepStillPartialCommitsWithoutHWM(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.rv05unbounded", 3)

	const goatID = "10000000-0000-4000-8000-00000000f402"
	const shedID = "00000000-0000-4000-8000-00000000d402"
	seedParkConsolidationShed(t, ctx, pool, shedID, "rv05-unbounded-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := due.AddDate(0, 0, 5)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}

	planA := oblapp.SweepVersionPriority{VersionID: versions[0].versionID, Config: rv05VaccineIdentity(versions[0], "FMD", 5, planner)}
	planB := oblapp.SweepVersionPriority{VersionID: versions[1].versionID, Config: rv05VaccineIdentity(versions[1], "PPR", 5, planner)}
	planC := oblapp.SweepVersionPriority{VersionID: versions[2].versionID, Config: rv05VaccineIdentity(versions[2], "HS", 5, planner)}

	for i, plan := range []struct {
		v    struct{ versionID, ruleID string }
		code string
	}{{versions[0], "FMD"}, {versions[1], "PPR"}} {
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: plan.v.versionID, RuleID: plan.v.ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: "rv05unb-pre-" + plan.code, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert pre obligation %d: applied=%v err=%v", i, applied, err)
		}
	}

	sweeper := oblapp.NewSweeperService(repo, nil, nil)

	// Unbounded preflight (zero HWM): clean, same as before -- only FMD/PPR exist.
	if err := sweeper.PreflightVisitShotCapTies(ctx, tenantID, []oblapp.SweepVersionPriority{planA, planB}, due, time.Time{}); err != nil {
		t.Fatalf("preflight before the third obligation existed: %v, want nil", err)
	}

	// Same race: HS becomes due for the same visit after preflight, before the real sweep.
	if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: planC.VersionID, RuleID: versions[2].ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
		DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: "rv05unb-post-HS", Sequence: 1,
	}); err != nil || !applied {
		t.Fatalf("insert post obligation: applied=%v err=%v", applied, err)
	}

	session := oblapp.NewSweepSession()
	var sweepErr error
	for _, plan := range []oblapp.SweepVersionPriority{planA, planB, planC} {
		if _, err := sweeper.SweepVersionWithSessionNoFinalize(ctx, tenantID, plan.VersionID, plan.Config, due, session); err != nil {
			sweepErr = err
			break
		}
	}
	var tieErr *oblapp.ShotCapPriorityTieError
	if sweepErr == nil || !errors.As(sweepErr, &tieErr) {
		t.Fatalf("real sweep err = %v, want *ShotCapPriorityTieError reproducing the pre-HWM race", sweepErr)
	}

	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances oi JOIN obligation_batches b ON b.batch_id = oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2
  AND b.status NOT IN ('canceled','superseded')`, tenantID, goatID); got != 2 {
		t.Fatalf("partial commit not reproduced: shots committed = %d, want 2 (FMD+PPR already committed before the HS abort)", got)
	}
}

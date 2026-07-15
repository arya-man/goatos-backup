package postgres

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// seedShotCapVersions creates n independent vaccination protocol versions (one rule each) --
// standing in for n different vaccines' obligation-sweeper versions -- and returns their
// (versionID, ruleID) pairs. Each is swept independently (mirroring the production caller sweeping
// one plan/version per SweepVersionWithSession call), so a shared-visit-cap race across them can
// only be closed by the persisted, cross-call state this test exercises -- never by any single
// call's own in-memory SweepSession.
func seedShotCapVersions(t *testing.T, ctx context.Context, proto *protopg.Repository, codePrefix string, n int) []struct{ versionID, ruleID string } {
	t.Helper()
	out := make([]struct{ versionID, ruleID string }, 0, n)
	for i := 0; i < n; i++ {
		code := codePrefix + ".v" + strconv.Itoa(i)
		protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
			TenantID: tenantID, Code: code, Name: code, Category: "vaccination", Status: "draft",
		})
		if err != nil {
			t.Fatalf("definition %s: %v", code, err)
		}
		versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
			TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
			EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("version %s: %v", code, err)
		}
		ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
			TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
			EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("rule %s: %v", code, err)
		}
		out = append(out, struct{ versionID, ruleID string }{versionID, ruleID})
	}
	return out
}

// TestVisitShotCapPersistsAcrossSweepPasses is the VAX-REV-01(a) guard: a first sweep pass fills
// an animal's visit to the MaxShotsPerAnimalPerDrive cap (2) on one planned date using its own
// fresh SweepSession, exactly like the production caller's per-tick ObligationSweeperStage.Run.
// A SECOND, entirely independent pass (a brand new SweepSession, simulating the next 15-minute
// tick) then sweeps a THIRD vaccine newly due for the SAME animal on the SAME date. Before the
// fix, the second pass's fresh session started counting the visit at zero -- unaware of the batch
// the first pass already committed -- and would schedule a 3rd shot on the same date, exceeding
// the cap. With the fix, the second pass seeds its session from the persisted, already-committed
// shot count and defers the 3rd dose to the next feasible date instead.
func TestVisitShotCapPersistsAcrossSweepPasses(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.crosspass", 3)

	const goatID = "10000000-0000-4000-8000-00000000f001"
	const shedID = "00000000-0000-4000-8000-00000000d101"
	seedParkConsolidationShed(t, ctx, pool, shedID, "visit-cap-crosspass-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// windowEnd == due (a zero-width window) makes `due` the ONLY feasible drive date, so the
	// drive planner's own date-picking heuristics (which otherwise prefer a later date within a
	// wider window to maximize coverage) cannot introduce ambiguity about which date this test's
	// assertions are about.
	windowEnd := due
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}

	// Pass 1: vaccines 0 and 1 are due now; sweep them together (their own private, single-call
	// session correctly caps them at 2/2 on `due` even without the cross-pass fix -- this just
	// establishes the "already committed" baseline the cross-pass fix must then respect).
	for _, v := range versions[:2] {
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: v.versionID, RuleID: v.ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: "crosspass-" + v.versionID, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert pass-1 obligation for %s: applied=%v err=%v", v.versionID, applied, err)
		}
	}
	sweepPass1 := oblapp.NewSweeperService(repo, nil, nil)
	session1 := oblapp.NewSweepSession()
	for _, v := range versions[:2] {
		if _, err := sweepPass1.SweepVersionWithSession(ctx, tenantID, v.versionID, oblapp.SweepConfig{DrivePlanner: planner}, due, session1); err != nil {
			t.Fatalf("pass 1 sweep %s: %v", v.versionID, err)
		}
	}

	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances oi JOIN obligation_batches b ON b.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND b.planned_date=$3::date
  AND b.status NOT IN ('canceled','superseded')`, tenantID, goatID, due); got != 2 {
		t.Fatalf("pass 1: shots committed on %s = %d, want 2", due.Format("2006-01-02"), got)
	}

	// Pass 2: a brand NEW sweeper + brand NEW SweepSession (no in-memory memory of pass 1 at all),
	// exactly like the next scheduled tick of ObligationSweeperStage.Run. A 3rd vaccine becomes due
	// for the SAME goat on the SAME date. Its window is exactly 1 day wide (due, due+1): both dates
	// fall in the drive planner's own "<=3 days left" scoring bucket and tie, so its own date-pick
	// heuristic (independent of the shot cap) prefers the EARLIER of the two -- `due` -- meaning
	// this obligation genuinely attempts `due` first (exercising the seed-then-reject path) rather
	// than trivially preferring a distant date the wider single-row window would otherwise score
	// higher (see scoreDriveDate's "days left" bucketing). due+1 is left as its one feasible
	// overflow date.
	thirdWindowEnd := due.AddDate(0, 0, 1)
	if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versions[2].versionID, RuleID: versions[2].ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
		DueAt: due, WindowEnd: &thirdWindowEnd, Status: "scheduled", IdempotencyKey: "crosspass-" + versions[2].versionID, Sequence: 1,
	}); err != nil || !applied {
		t.Fatalf("insert pass-2 obligation: applied=%v err=%v", applied, err)
	}
	sweepPass2 := oblapp.NewSweeperService(repo, nil, nil)
	session2 := oblapp.NewSweepSession()
	if _, err := sweepPass2.SweepVersionWithSession(ctx, tenantID, versions[2].versionID, oblapp.SweepConfig{DrivePlanner: planner}, due, session2); err != nil {
		t.Fatalf("pass 2 sweep: %v", err)
	}

	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances oi JOIN obligation_batches b ON b.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND b.planned_date=$3::date
  AND b.status NOT IN ('canceled','superseded')`, tenantID, goatID, due); got != 2 {
		t.Fatalf("after pass 2: shots committed on %s = %d, want STILL 2 (cross-pass cap must hold)", due.Format("2006-01-02"), got)
	}
	var thirdBatched bool
	var thirdPlannedDate time.Time
	if err := pool.QueryRow(ctx, `
SELECT true, b.planned_date FROM obligation_instances oi JOIN obligation_batches b ON b.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.protocol_version_id=$2 AND oi.target_id=$3`,
		tenantID, versions[2].versionID, goatID).Scan(&thirdBatched, &thirdPlannedDate); err != nil {
		t.Fatalf("3rd vaccine's obligation was not batched at all (dropped instead of overflowed): %v", err)
	}
	if thirdPlannedDate.Equal(due) {
		t.Fatalf("3rd vaccine planned_date = %s, want overflowed off %s (cap already full from pass 1)", thirdPlannedDate, due.Format("2006-01-02"))
	}
}

// TestVisitShotCapAtomicAcrossConcurrentWorkers is the VAX-REV-01(b) guard: N independent
// sweeper workers (N different protocol versions/vaccines, each with its own SweeperService and
// its own fresh SweepSession -- exactly as if N worker replicas independently picked up N
// different vaccines due for the SAME animal on the SAME date) race via SweepVersion concurrently
// against the SAME Postgres database. Without the per-visit advisory lock, each worker's
// read-then-decide step can observe the visit as empty before any of the others commit, and all N
// could independently decide they fit under the cap -- jointly exceeding
// MaxShotsPerAnimalPerDrive. With the lock, the total number of shots committed for the animal on
// that date across ALL workers never exceeds the cap, and no obligation is silently dropped: every
// one lands on either the capped date or a later overflow date.
func TestVisitShotCapAtomicAcrossConcurrentWorkers(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	// n=4 with a cap of 2 and exactly 2 feasible dates (due, due+1) is a PERFECT fit (4 slots for
	// 4 obligations) that the sweeper's single overflow-retry-per-group logic can always resolve
	// in one pass, regardless of the goroutine scheduling order: whichever 2 workers lose the race
	// for `due` both overflow to `due+1` (itself lock-protected the same way), and exactly fit
	// there. A wider contention scenario (more obligations than 2 retry levels can resolve in one
	// pass) is a separate, pre-existing single-pass-convergence property of the overflow retry --
	// not what this test is isolating -- and is still covered by TestVisitShotCapPersistsAcrossSweepPasses's
	// cross-PASS convergence.
	const n = 4
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.concurrent", n)

	const goatID = "10000000-0000-4000-8000-00000000f101"
	const shedID = "00000000-0000-4000-8000-00000000d102"
	seedParkConsolidationShed(t, ctx, pool, shedID, "visit-cap-concurrent-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// A 1-day window (due..due+1) keeps both candidate dates in the drive planner's OWN
	// "<=3 days left" scoring bucket (see scoreDriveDate), so every one of the 4 single-obligation
	// groups ties on score and its date-pick heuristic (independent of the shot cap) prefers the
	// EARLIER candidate -- `due` -- for all 4. That is what makes this a genuine race: all 4
	// concurrent workers first attempt the SAME visit.
	windowEnd := due.AddDate(0, 0, 1)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}

	for _, v := range versions {
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: v.versionID, RuleID: v.ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: "concurrent-" + v.versionID, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert obligation for %s: applied=%v err=%v", v.versionID, applied, err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i, v := range versions {
		wg.Add(1)
		go func(i int, versionID string) {
			defer wg.Done()
			sweep := oblapp.NewSweeperService(repo, nil, nil)
			_, err := sweep.SweepVersion(ctx, tenantID, versionID, oblapp.SweepConfig{DrivePlanner: planner}, due)
			errs[i] = err
		}(i, v.versionID)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent sweep %d: %v", i, err)
		}
	}

	rows, err := pool.Query(ctx, `
SELECT b.planned_date, count(*)
FROM obligation_instances oi JOIN obligation_batches b ON b.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND b.status NOT IN ('canceled','superseded')
GROUP BY b.planned_date
ORDER BY b.planned_date`, tenantID, goatID)
	if err != nil {
		t.Fatalf("query per-date counts: %v", err)
	}
	defer rows.Close()
	total := 0
	maxOnAnyDate := 0
	for rows.Next() {
		var plannedDate time.Time
		var count int
		if err := rows.Scan(&plannedDate, &count); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if count > maxOnAnyDate {
			maxOnAnyDate = count
		}
		total += count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if maxOnAnyDate > 2 {
		t.Fatalf("max shots committed on any single planned date = %d, want <= 2 (MaxShotsPerAnimalPerDrive)", maxOnAnyDate)
	}
	if total != n {
		t.Fatalf("total shots committed across all dates = %d, want %d (none silently dropped)", total, n)
	}
}

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

// seedShotOnDate inserts one scheduled obligation for goatID under (versionID, ruleID) and attaches
// it to a freshly created 'planned' batch scoped as (scopeType, scopeID) with planned_date =
// plannedDate. Each shot is given a UNIQUE session ("shot:"+idemKey) so CreateBatchWithObligations'
// planned-batch merge (which keys on tenant/version/scope/session/planned_date/window) never folds
// two of a goat's shots into one batch -- every call yields its OWN batch, giving the cap-count
// tests below precise control over the goat->batches fan-out and each batch's status. Returns the
// obligation id and batch id so callers can mutate their status directly (canceled/superseded
// buckets). Callers MUST pass a distinct protocol version per shot for the same goat+due_at, to
// satisfy obligation_instances' (tenant, version, rule, target, due_at) duplicate-spawn guard.
func seedShotOnDate(t *testing.T, ctx context.Context, repo *Repository, v struct{ versionID, ruleID string }, goatID, scopeType, scopeID string, dueAt, plannedDate time.Time, idemKey string) (obligationID, batchID string) {
	t.Helper()
	oblID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: v.versionID, RuleID: v.ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: scopeType, ScopeID: scopeID,
		DueAt: dueAt, Status: "scheduled", IdempotencyKey: idemKey, Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed obligation %s: applied=%v err=%v", idemKey, applied, err)
	}
	pd := plannedDate
	bID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: v.versionID, ScopeType: scopeType, ScopeID: scopeID,
		Session: "shot:" + idemKey, PlannedDate: &pd, Status: "planned",
		EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose",
	}, []string{oblID})
	if err != nil {
		t.Fatalf("attach obligation %s to batch: %v", idemKey, err)
	}
	if attached != 1 {
		t.Fatalf("attach obligation %s: attached=%d want 1", idemKey, attached)
	}
	return oblID, bID
}

// TestCountVisitShotsOneToManyBatchesCountsEachShotOnce is the adversarial guard that the cap-count
// query's oi->obligation_batches JOIN is a 1:1 semijoin, not a fan-out: a goat with two shots in
// TWO SEPARATE non-canceled batches on the SAME planned_date must return count = 2 (each obligation
// counted exactly once). obligation_batches is keyed by (tenant_id, batch_id) and every
// obligation_instances row carries exactly one batch_id, so COUNT(*) counts obligation rows -- if a
// regression ever widened the JOIN (e.g. joined on a non-unique column and multiplied rows), this
// goat-with-many-batches shape would inflate past 2.
func TestCountVisitShotsOneToManyBatchesCountsEachShotOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.onetomany", 2)

	const goatID = "10000000-0000-4000-8000-00000000f201"
	const shedID = "00000000-0000-4000-8000-00000000d201"
	seedParkConsolidationShed(t, ctx, pool, shedID, "count-onetomany-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, batchA := seedShotOnDate(t, ctx, repo, versions[0], goatID, "shed", shedID, date, date, "onetomany-A")
	_, batchB := seedShotOnDate(t, ctx, repo, versions[1], goatID, "shed", shedID, date, date, "onetomany-B")
	if batchA == batchB {
		t.Fatalf("fixture invalid: both shots landed in the same batch %s, want two separate batches", batchA)
	}

	counts, err := repo.CountVisitShotsForTargets(ctx, tenantID, []string{goatID}, date)
	if err != nil {
		t.Fatalf("CountVisitShotsForTargets: %v", err)
	}
	if counts[goatID] != 2 {
		t.Fatalf("count = %d, want 2 (two shots in two batches, each counted once; no JOIN fan-out)", counts[goatID])
	}
}

// TestCountVisitShotsMultiPageTargetsStableTotal is the adversarial guard that the count is a pure
// function of each target's own committed shots and has NO dependence on how the caller chunks its
// target_id list (the sweeper seeds the cap per distinct target, and any batching/paging of that
// target set must not change a single per-animal count). Three goats with 1 / 2 / 1 shots on the
// same date are counted as the full set and as arbitrary subsets ("pages"); every per-target count
// is identical regardless of which targets accompany it in the query.
func TestCountVisitShotsMultiPageTargetsStableTotal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.multipage", 2)

	const goatA = "10000000-0000-4000-8000-00000000f2a1"
	const goatB = "10000000-0000-4000-8000-00000000f2a2"
	const goatC = "10000000-0000-4000-8000-00000000f2a3"
	const shedID = "00000000-0000-4000-8000-00000000d202"
	seedParkConsolidationShed(t, ctx, pool, shedID, "count-multipage-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatA, goatB, goatC)

	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	seedShotOnDate(t, ctx, repo, versions[0], goatA, "shed", shedID, date, date, "multipage-A0")
	seedShotOnDate(t, ctx, repo, versions[0], goatB, "shed", shedID, date, date, "multipage-B0")
	seedShotOnDate(t, ctx, repo, versions[1], goatB, "shed", shedID, date, date, "multipage-B1")
	seedShotOnDate(t, ctx, repo, versions[1], goatC, "shed", shedID, date, date, "multipage-C1")

	full, err := repo.CountVisitShotsForTargets(ctx, tenantID, []string{goatA, goatB, goatC}, date)
	if err != nil {
		t.Fatalf("full-set count: %v", err)
	}
	if full[goatA] != 1 || full[goatB] != 2 || full[goatC] != 1 {
		t.Fatalf("full-set counts = A:%d B:%d C:%d, want A:1 B:2 C:1", full[goatA], full[goatB], full[goatC])
	}

	// Every chunking of the target list must yield the SAME per-animal count as the full set.
	pages := [][]string{{goatA}, {goatB}, {goatC}, {goatA, goatC}, {goatB, goatA}}
	for _, page := range pages {
		got, err := repo.CountVisitShotsForTargets(ctx, tenantID, page, date)
		if err != nil {
			t.Fatalf("page %v count: %v", page, err)
		}
		for _, target := range page {
			if got[target] != full[target] {
				t.Fatalf("page %v: count[%s] = %d, want %d (per-target count must not depend on page composition)", page, target, got[target], full[target])
			}
		}
	}
}

// TestCountVisitShotsDateShiftExcludesOtherDates is the adversarial guard that the count is scoped
// to exactly the queried planned_date (ob.planned_date = $3::date) and never bleeds a shot from an
// adjacent visit into the wrong day. One goat has a shot on D and another on D+7; counting D returns
// only the D shot and counting D+7 returns only the D+7 shot.
func TestCountVisitShotsDateShiftExcludesOtherDates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.dateshift", 2)

	const goatID = "10000000-0000-4000-8000-00000000f203"
	const shedID = "00000000-0000-4000-8000-00000000d203"
	seedParkConsolidationShed(t, ctx, pool, shedID, "count-dateshift-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	dateD := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	dateD7 := dateD.AddDate(0, 0, 7)
	seedShotOnDate(t, ctx, repo, versions[0], goatID, "shed", shedID, dateD, dateD, "dateshift-D")
	seedShotOnDate(t, ctx, repo, versions[1], goatID, "shed", shedID, dateD7, dateD7, "dateshift-D7")

	onD, err := repo.CountVisitShotsForTargets(ctx, tenantID, []string{goatID}, dateD)
	if err != nil {
		t.Fatalf("count on D: %v", err)
	}
	if onD[goatID] != 1 {
		t.Fatalf("count on D = %d, want 1 (only the D shot; the D+7 shot must be excluded by planned_date scoping)", onD[goatID])
	}
	onD7, err := repo.CountVisitShotsForTargets(ctx, tenantID, []string{goatID}, dateD7)
	if err != nil {
		t.Fatalf("count on D+7: %v", err)
	}
	if onD7[goatID] != 1 {
		t.Fatalf("count on D+7 = %d, want 1 (only the D+7 shot)", onD7[goatID])
	}
}

// TestCountVisitShotsScopeHierarchyAggregatesPerAnimalAcrossScopes is the adversarial guard that
// the per-animal cap count is keyed on target_id ALONE and is scope-independent: a goat with one
// shot in a SHED-scoped batch and one in a PARK-scoped batch on the same date counts as 2. The
// count must aggregate per animal across scope levels of the location hierarchy -- it must not
// fragment (report per scope) or double-count. This is what makes MaxShotsPerAnimalPerDrive an
// actual per-ANIMAL cap rather than a per-scope one, so a shed drive plus a park drive on one day
// correctly reads as the animal's 2 shots.
func TestCountVisitShotsScopeHierarchyAggregatesPerAnimalAcrossScopes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.scopehier", 2)

	const goatID = "10000000-0000-4000-8000-00000000f204"
	const shedID = "00000000-0000-4000-8000-00000000d204"
	seedParkConsolidationShed(t, ctx, pool, shedID, "count-scopehier-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// One shot in a shed-scoped batch, one in a park-scoped batch (cbePark), same animal, same day.
	_, shedBatch := seedShotOnDate(t, ctx, repo, versions[0], goatID, "shed", shedID, date, date, "scopehier-shed")
	_, parkBatch := seedShotOnDate(t, ctx, repo, versions[1], goatID, "park", cbePark, date, date, "scopehier-park")
	if shedBatch == parkBatch {
		t.Fatalf("fixture invalid: shed and park shots landed in the same batch %s", shedBatch)
	}

	counts, err := repo.CountVisitShotsForTargets(ctx, tenantID, []string{goatID}, date)
	if err != nil {
		t.Fatalf("CountVisitShotsForTargets: %v", err)
	}
	if counts[goatID] != 2 {
		t.Fatalf("count = %d, want 2 (shed-scoped + park-scoped shots aggregate per animal, not per scope)", counts[goatID])
	}
}

// TestCountVisitShotsStatusBucketsExcludesCanceledSuperseded is the adversarial guard for the
// status filter (ob.status NOT IN ('canceled','superseded') AND oi.status NOT IN ('canceled')): a
// goat with an ACTIVE shot plus three shots that must NOT count -- one in a 'canceled' batch, one in
// a 'superseded' batch, and one whose OBLIGATION is 'canceled' (its batch still planned) -- returns
// exactly 1. A regression that dropped either the batch-status or the obligation-status exclusion
// would over-count a released/superseded/canceled shot against the animal's cap.
func TestCountVisitShotsStatusBucketsExcludesCanceledSuperseded(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.statusbuckets", 4)

	const goatID = "10000000-0000-4000-8000-00000000f205"
	const shedID = "00000000-0000-4000-8000-00000000d205"
	seedParkConsolidationShed(t, ctx, pool, shedID, "count-statusbuckets-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// 0: active (planned batch, scheduled obligation) -> counts.
	seedShotOnDate(t, ctx, repo, versions[0], goatID, "shed", shedID, date, date, "status-active")
	// 1: batch canceled -> excluded by ob.status filter.
	_, canceledBatch := seedShotOnDate(t, ctx, repo, versions[1], goatID, "shed", shedID, date, date, "status-batch-canceled")
	// 2: batch superseded -> excluded by ob.status filter.
	_, supersededBatch := seedShotOnDate(t, ctx, repo, versions[2], goatID, "shed", shedID, date, date, "status-batch-superseded")
	// 3: obligation canceled (batch still planned) -> excluded by oi.status filter.
	canceledObl, _ := seedShotOnDate(t, ctx, repo, versions[3], goatID, "shed", shedID, date, date, "status-obl-canceled")

	if _, err := pool.Exec(ctx, `UPDATE obligation_batches SET status='canceled' WHERE tenant_id=$1 AND batch_id=$2`, tenantID, canceledBatch); err != nil {
		t.Fatalf("cancel batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_batches SET status='superseded' WHERE tenant_id=$1 AND batch_id=$2`, tenantID, supersededBatch); err != nil {
		t.Fatalf("supersede batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, canceledObl); err != nil {
		t.Fatalf("cancel obligation: %v", err)
	}

	counts, err := repo.CountVisitShotsForTargets(ctx, tenantID, []string{goatID}, date)
	if err != nil {
		t.Fatalf("CountVisitShotsForTargets: %v", err)
	}
	if counts[goatID] != 1 {
		t.Fatalf("count = %d, want 1 (only the active shot; canceled batch, superseded batch, and canceled obligation must all be excluded)", counts[goatID])
	}
}

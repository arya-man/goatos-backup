package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
)

// These tests cover the age-floor proof a drive-date override now runs over every obligation row
// its date sync rewrites (syncVaccinationObligationDatesFromAssignmentsTx). The proof is one
// set-based lock + validation over the re-planned batches, so each test pins one axis of that set:
//
//	OneToMany    -- one goat holds several open vaccination rows in the batch; only the moved
//	                lane's rows are rewritten, each exactly once, the other lane's rows stay put.
//	MultiPage    -- a move spanning many goats in several cells is validated as ONE set: a single
//	                violator at the far end of the lock order rejects the whole move atomically.
//	StatusMatrix -- only scheduled/due/deferred rows are rewritten and therefore judged; rows in
//	                in_progress/completed/canceled are neither moved nor allowed to veto the move.
//
// The PPR rule carries a 28-day birth-age floor and an override may only push a drive LATER, so
// the realistic violation is a goat whose DOB was corrected after its row was planned: the row now
// sits below its floor and a move that is still short of the floor must not rewrite it there.
// Every goat not named as young is born in 2020, so only the explicitly young goats can fail.

var (
	floorSource = time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	floorTarget = time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	// Corrected DOB 2026-07-15: 28-day floor is 2026-08-12, after both the source and the target.
	youngDOB = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
)

type floorFixture struct {
	repo       *Repository
	pool       *pgxpool.Pool
	ppr        struct{ versionID, ruleID string }
	blueTongue struct{ versionID, ruleID string }
	operator   string
	batchID    string
}

type floorSeed struct {
	lane   string // "ppr" or "bt"
	goat   string
	shed   string
	due    time.Time
	key    string
	status string // applied after the batch is built; "" keeps scheduled
}

// newFloorFixture seeds an operator executable on both dates, the two vaccine lanes, one batch of
// obligations, and one drive-assignment row per (shed, date, lane) mirroring the seeds.
func newFloorFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, prefix, operator string, sheds map[string]string, youngGoats []string, seeds []floorSeed) floorFixture {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	f := floorFixture{repo: NewRepository(pool, 60*time.Second), pool: pool, operator: operator}
	versions := seedShotCapVersions(t, ctx, proto, "vaccination."+strings.ReplaceAll(prefix, "-", ""), 2)
	f.ppr, f.blueTongue = versions[0], versions[1]
	if _, err := pool.Exec(ctx, `UPDATE protocol_rules SET offset_days = 28 WHERE tenant_id = $1 AND rule_id = $2::uuid`, tenantID, f.ppr.ruleID); err != nil {
		t.Fatalf("set PPR age floor: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code
) VALUES
  ($1, $2, $3, 'vaccination', $6 || '-ppr', 'ppr_kid_4w', 'PPR'),
  ($1, $4, $5, 'vaccination', $6 || '-bt', 'blue_tongue_adult_w1', 'BLUE_TONGUE')`,
		tenantID, f.ppr.versionID, f.ppr.ruleID, f.blueTongue.versionID, f.blueTongue.ruleID, prefix); err != nil {
		t.Fatalf("seed rule dimensions: %v", err)
	}

	goatsByShed := map[string][]string{}
	for _, s := range seeds {
		if !containsString(goatsByShed[s.shed], s.goat) {
			goatsByShed[s.shed] = append(goatsByShed[s.shed], s.goat)
		}
	}
	for shed, code := range sheds {
		seedParkConsolidationShed(t, ctx, pool, shed, code)
		seedReserveGoats(t, ctx, pool, shed, cbePark, goatsByShed[shed]...)
	}

	weekOff := strings.ToLower(floorSource.AddDate(0, 0, 2).Weekday().String())
	position := strings.ReplaceAll(prefix, "-", "_") + "_operator"
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, $3, $3, 'active', 'operator', $4)`, operator, tenantID, prefix, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($1, $2, 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active')`, tenantID, position); err != nil {
		t.Fatalf("seed duties: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($1, $2, 'center', $3, $4, 'manager', $5, 200, 'active', '2026-01-01')`, tenantID, operator, cbePark, position, weekOff); err != nil {
		t.Fatalf("seed position: %v", err)
	}

	lane := func(name string) struct{ versionID, ruleID string } {
		if name == "bt" {
			return f.blueTongue
		}
		return f.ppr
	}
	oblIDs := make([]string, 0, len(seeds))
	for _, s := range seeds {
		v := lane(s.lane)
		id, applied, err := f.repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: v.versionID, RuleID: v.ruleID,
			TargetType: "goat", TargetID: s.goat, ScopeType: "shed", ScopeID: s.shed,
			DueAt: s.due, Status: "scheduled", IdempotencyKey: s.key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("seed obligation %s: applied=%v err=%v", s.key, applied, err)
		}
		oblIDs = append(oblIDs, id)
	}
	planned := floorSource
	batchID, attached, err := f.repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: f.ppr.versionID, ScopeType: "park", ScopeID: cbePark,
		Session: prefix, PlannedDate: &planned, Status: "planned",
		EstimatedTargets: int32(len(seeds)), PlannedQuantity: fmt.Sprint(len(seeds)), QuantityUnit: "dose",
	}, oblIDs)
	if err != nil || int(attached) != len(oblIDs) {
		t.Fatalf("create batch: attached=%d want %d err=%v", attached, len(oblIDs), err)
	}
	f.batchID = batchID

	type cell struct {
		shed, lane string
		date       time.Time
	}
	animals := map[cell]map[string]bool{}
	var order []cell
	for _, s := range seeds {
		c := cell{s.shed, s.lane, s.due}
		if animals[c] == nil {
			animals[c] = map[string]bool{}
			order = append(order, c)
		}
		animals[c][s.goat] = true
	}
	rows := make([]domain.DriveAssignment, 0, len(order))
	for _, c := range order {
		n := int32(len(animals[c]))
		rows = append(rows, domain.DriveAssignment{
			BatchID: batchID, PlannedDate: c.date, OperatorID: testStringPtr(operator), ParkID: cbePark,
			ShedID: testStringPtr(c.shed), PhysicalShed: sheds[c.shed], PartitionLabel: "1", AnimalCount: n,
			VaccineRuleIDs: []string{lane(c.lane).ruleID}, TotalDoses: n, CapacityStatus: "within_cap",
		})
	}
	if err := f.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, rows); err != nil {
		t.Fatalf("seed assignments: %v", err)
	}
	for _, s := range seeds {
		if s.status == "" {
			continue
		}
		if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status = $3 WHERE tenant_id = $1 AND idempotency_key = $2`,
			tenantID, s.key, s.status); err != nil {
			t.Fatalf("set %s status %s: %v", s.key, s.status, err)
		}
	}
	// The DOB correction lands after planning, which is how a live row ends up below its floor.
	for _, goat := range youngGoats {
		if _, err := pool.Exec(ctx, `UPDATE goats SET dob = $2 WHERE tenant_id = $1 AND goat_id = $3::uuid`, tenantID, youngDOB, goat); err != nil {
			t.Fatalf("seed young goat %s: %v", goat, err)
		}
	}
	return f
}

func (f floorFixture) movePPR(ctx context.Context) error {
	_, err := f.repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: floorSource,
		OverrideDate: floorTarget, Reason: "admin pushes the PPR drive back", CreatedBy: f.operator,
	})
	return err
}

// dueDates returns each seeded obligation's business due date keyed by idempotency key.
func (f floorFixture) dueDates(t *testing.T, ctx context.Context) map[string]string {
	t.Helper()
	rows, err := f.pool.Query(ctx, `
SELECT idempotency_key, to_char((due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
FROM obligation_instances
WHERE tenant_id = $1 AND batch_id = $2::uuid`, tenantID, f.batchID)
	if err != nil {
		t.Fatalf("read due dates: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, day string
		if err := rows.Scan(&key, &day); err != nil {
			t.Fatalf("scan due date: %v", err)
		}
		out[key] = day
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate due dates: %v", err)
	}
	return out
}

func (f floorFixture) overrideCount(t *testing.T, ctx context.Context) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM vaccination_drive_date_overrides WHERE tenant_id = $1 AND park_id = $2`, tenantID, cbePark).Scan(&n); err != nil {
		t.Fatalf("count overrides: %v", err)
	}
	return n
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func floorGoat(block, i int) string {
	return fmt.Sprintf("10000000-0000-4000-8000-0000%04x%04x", block, i)
}

// TestDriveDateOverrideFloorOneToManyDateShift: goats that hold BOTH a PPR and a Blue Tongue row in
// the same batch. Pushing PPR later must rewrite every PPR row to the target date exactly once
// and leave every Blue Tongue row on its own date; a young goat whose PPR row would still land below its
// (corrected-DOB) floor must reject the whole move and leave every row, lane and override untouched.
func TestDriveDateOverrideFloorOneToManyDateShift(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const shed = "00000000-0000-4000-8000-00000000ea01"
	btDate := floorTarget.AddDate(0, 0, 30) // clear of live-vaccine spacing either side of the move
	var seeds []floorSeed
	for i := 1; i <= 3; i++ {
		goat := floorGoat(0xa1, i)
		seeds = append(seeds,
			floorSeed{lane: "ppr", goat: goat, shed: shed, due: floorSource, key: fmt.Sprintf("floor-o2m-ppr-%d", i)},
			floorSeed{lane: "bt", goat: goat, shed: shed, due: btDate, key: fmt.Sprintf("floor-o2m-bt-%d", i)})
	}
	f := newFloorFixture(t, ctx, pool, "floor-o2m", "20000000-0000-4000-8000-00000000ea01",
		map[string]string{shed: "Floor O2M"}, nil, seeds)

	if err := f.movePPR(ctx); err != nil {
		t.Fatalf("legal PPR move rejected: %v", err)
	}
	got := f.dueDates(t, ctx)
	if len(got) != 6 {
		t.Fatalf("batch holds %d obligation rows after the move, want 6 (3 goats x 2 lanes; a one-to-many goat must not gain or lose rows): %v", len(got), got)
	}
	for i := 1; i <= 3; i++ {
		if d := got[fmt.Sprintf("floor-o2m-ppr-%d", i)]; d != floorTarget.Format(time.DateOnly) {
			t.Errorf("goat %d PPR due %s, want the override date %s", i, d, floorTarget.Format(time.DateOnly))
		}
		if d := got[fmt.Sprintf("floor-o2m-bt-%d", i)]; d != btDate.Format(time.DateOnly) {
			t.Errorf("goat %d Blue Tongue due %s, want its own untouched date %s -- a PPR move must not drag the goat's other lane", i, d, btDate.Format(time.DateOnly))
		}
	}
	var ledger int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
JOIN obligation_instances oi ON oi.tenant_id = m.tenant_id AND oi.obligation_id = m.obligation_id
WHERE m.tenant_id = $1 AND oi.batch_id = $2::uuid
  AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date = vda.planned_date`, tenantID, f.batchID).Scan(&ledger); err != nil {
		t.Fatalf("count ledger parity: %v", err)
	}
	if ledger != 6 {
		t.Errorf("%d of 6 obligation rows sit on the date of the assignment that owns them; due_at and planned_date drifted", ledger)
	}
}

func TestDriveDateOverrideFloorOneToManyYoungGoatRejectsWholeMove(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const shed = "00000000-0000-4000-8000-00000000ea02"
	btDate := floorTarget.AddDate(0, 0, 30) // clear of live-vaccine spacing either side of the move
	young := floorGoat(0xa2, 3)
	var seeds []floorSeed
	for i := 1; i <= 3; i++ {
		goat := floorGoat(0xa2, i)
		seeds = append(seeds,
			floorSeed{lane: "ppr", goat: goat, shed: shed, due: floorSource, key: fmt.Sprintf("floor-o2m-young-ppr-%d", i)},
			floorSeed{lane: "bt", goat: goat, shed: shed, due: btDate, key: fmt.Sprintf("floor-o2m-young-bt-%d", i)})
	}
	f := newFloorFixture(t, ctx, pool, "floor-o2m-young", "20000000-0000-4000-8000-00000000ea02",
		map[string]string{shed: "Floor O2M Young"}, []string{young}, seeds)
	before := f.dueDates(t, ctx)

	if err := f.movePPR(ctx); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("moving PPR below goat 3's 28-day floor returned %v, want ErrBeforeVaccinationAgeFloor", err)
	}
	after := f.dueDates(t, ctx)
	for key, day := range before {
		if after[key] != day {
			t.Errorf("%s moved %s -> %s although the override was rejected; the move must be all-or-nothing", key, day, after[key])
		}
	}
	if n := f.overrideCount(t, ctx); n != 0 {
		t.Errorf("%d override row(s) persisted for a rejected move", n)
	}
	if rows := laneRowsOnDate(t, ctx, pool, floorTarget); len(rows) != 0 {
		t.Errorf("rejected move left %d drive row(s) on the target date: %+v", len(rows), rows)
	}
}

// TestDriveDateOverrideFloorMultiPageRejectsLastViolator spreads the moved lane across two cells
// and 2x40 goats. The only young goat has the highest goat_id in the second cell, so it is the last
// row in both lock orders; a proof that stopped at any page, chunk or first cell would miss it.
// After its DOB is corrected the same move must rewrite all 80 rows.
func TestDriveDateOverrideFloorMultiPageRejectsLastViolator(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		shed1   = "00000000-0000-4000-8000-00000000eb01"
		shed2   = "00000000-0000-4000-8000-00000000eb02"
		perShed = 40
	)
	var seeds []floorSeed
	for i := 1; i <= perShed; i++ {
		seeds = append(seeds, floorSeed{lane: "ppr", goat: floorGoat(0xb1, i), shed: shed1, due: floorSource, key: fmt.Sprintf("floor-page-1-%02d", i)})
		seeds = append(seeds, floorSeed{lane: "ppr", goat: floorGoat(0xb2, i), shed: shed2, due: floorSource, key: fmt.Sprintf("floor-page-2-%02d", i)})
	}
	last := floorGoat(0xb2, perShed)
	f := newFloorFixture(t, ctx, pool, "floor-page", "20000000-0000-4000-8000-00000000eb01",
		map[string]string{shed1: "Floor Page 1", shed2: "Floor Page 2"}, []string{last}, seeds)

	if err := f.movePPR(ctx); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("move with one young goat at the end of the second cell returned %v, want ErrBeforeVaccinationAgeFloor", err)
	}
	moved := 0
	for _, day := range f.dueDates(t, ctx) {
		if day == floorTarget.Format(time.DateOnly) {
			moved++
		}
	}
	if moved != 0 {
		t.Fatalf("%d of %d rows moved although the override was rejected", moved, 2*perShed)
	}

	if _, err := pool.Exec(ctx, `UPDATE goats SET dob = '2020-01-01'::date WHERE tenant_id = $1 AND goat_id = $2::uuid`, tenantID, last); err != nil {
		t.Fatalf("correct young goat dob: %v", err)
	}
	if err := f.movePPR(ctx); err != nil {
		t.Fatalf("move after the DOB correction rejected: %v", err)
	}
	got := f.dueDates(t, ctx)
	perCell := map[string]int{}
	for key, day := range got {
		if day != floorTarget.Format(time.DateOnly) {
			t.Errorf("%s due %s after the move, want %s", key, day, floorTarget.Format(time.DateOnly))
			continue
		}
		perCell[key[:len("floor-page-1")]]++
	}
	if perCell["floor-page-1"] != perShed || perCell["floor-page-2"] != perShed {
		t.Errorf("moved rows per cell = %v, want %d in each of both cells", perCell, perShed)
	}
}

// TestDriveDateOverrideFloorStatusMatrix puts one PPR row in every obligation status on the source
// date. The goats behind in_progress/completed/canceled rows are too young for the target date (corrected DOB), but
// those rows are not rewritten, so they must neither move nor veto the move. scheduled/due/deferred
// rows belong to adult goats and must all move. A second batch then proves a young goat's DEFERRED
// row is judged: it is rewritten by the sync, so it rejects the move.
func TestDriveDateOverrideFloorStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const shed = "00000000-0000-4000-8000-00000000ec01"
	statuses := []string{"scheduled", "due", "deferred", "in_progress", "completed", "canceled"}
	rewritten := map[string]bool{"scheduled": true, "due": true, "deferred": true}
	var seeds []floorSeed
	var young []string
	for i, status := range statuses {
		goat := floorGoat(0xc1, i+1)
		seeds = append(seeds, floorSeed{lane: "ppr", goat: goat, shed: shed, due: floorSource, key: "floor-status-" + status, status: status})
		if !rewritten[status] {
			young = append(young, goat)
		}
	}
	f := newFloorFixture(t, ctx, pool, "floor-status", "20000000-0000-4000-8000-00000000ec01",
		map[string]string{shed: "Floor Status"}, young, seeds)

	if err := f.movePPR(ctx); err != nil {
		t.Fatalf("move rejected although every young goat's row is in a status the sync does not rewrite: %v", err)
	}
	got := f.dueDates(t, ctx)
	for _, status := range statuses {
		want := floorSource
		if rewritten[status] {
			want = floorTarget
		}
		if d := got["floor-status-"+status]; d != want.Format(time.DateOnly) {
			t.Errorf("%s row due %s, want %s", status, d, want.Format(time.DateOnly))
		}
	}
}

func TestDriveDateOverrideFloorStatusMatrixDeferredYoungGoatRejects(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const shed = "00000000-0000-4000-8000-00000000ec02"
	adult, youngDeferred := floorGoat(0xc2, 1), floorGoat(0xc2, 2)
	f := newFloorFixture(t, ctx, pool, "floor-status-deferred", "20000000-0000-4000-8000-00000000ec02",
		map[string]string{shed: "Floor Status Deferred"}, []string{youngDeferred}, []floorSeed{
			{lane: "ppr", goat: adult, shed: shed, due: floorSource, key: "floor-deferred-adult"},
			{lane: "ppr", goat: youngDeferred, shed: shed, due: floorSource, key: "floor-deferred-young", status: "deferred"},
		})

	if err := f.movePPR(ctx); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("move rewriting a young goat's deferred row below its floor returned %v, want ErrBeforeVaccinationAgeFloor", err)
	}
	got := f.dueDates(t, ctx)
	for _, key := range []string{"floor-deferred-adult", "floor-deferred-young"} {
		if got[key] != floorSource.Format(time.DateOnly) {
			t.Errorf("%s due %s after a rejected move, want unchanged %s", key, got[key], floorSource.Format(time.DateOnly))
		}
	}
}

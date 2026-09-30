package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// KID STAGE SHIFT TASKS on the Work Board (docs/decisions/kid-stage-shift-tasks.md). A litter's
// workflow surfaces ONLY while a shift step is owed -- past due and still pending -- or on the day an
// owed step got done; never before its deadline, never when the kids moved in time, never on an
// operator's own lens. This is the owning package's read-model test: the workflow rows are the
// engine's derived state and are seeded directly (the engine opens them in production; the
// counts package's E2E drives that path).
//
// projection-review: membership=workflow_instances of the park and day, and for birth_litter only
// those with an owed-or-owed-and-done-today shift step; group_key=workflow_id; join_cardinality=the
// surfacing predicate is an EXISTS over the workflow's own (<= 2) shift steps, so a litter with two
// owed steps is still ONE row; pagination=keyset on workflow_id, pages add up to the count;
// scope=tenant + park, the same predicate in list and count.

const (
	litterOwedTwice   = "00000000-0000-4000-8000-00000000c101" // both steps past due and pending: one row
	litterNotYetDue   = "00000000-0000-4000-8000-00000000c102" // K1 due tomorrow
	litterDoneEarly   = "00000000-0000-4000-8000-00000000c103" // K1 done before its deadline, K2 not due
	litterDoneLate    = "00000000-0000-4000-8000-00000000c104" // K1 owed, done late TODAY
	litterDoneLateOld = "00000000-0000-4000-8000-00000000c105" // K1 owed, done late YESTERDAY, K2 not due
	litterCanceled    = "00000000-0000-4000-8000-00000000c106" // step canceled (every kid left the farm)
	litterOtherPark   = "00000000-0000-4000-8000-00000000c107" // owed, but in the other park
	litterOwedToday   = "00000000-0000-4000-8000-00000000c108" // a second owed litter, for paging
)

type litterFixture struct {
	id, park, state string
	steps           []litterStepFixture
}

type litterStepFixture struct {
	key, status    string
	due, completed *time.Time
}

func at(date, clock string) *time.Time { v := ist(date, clock); return &v }

func seedLitters(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	wbSeed(t, ctx, pool)
	litters := []litterFixture{
		{litterOwedTwice, wbPark, "open", []litterStepFixture{
			{"shift_to_k1", "pending", at("2026-09-23", "09:00"), nil},
			{"shift_to_k2", "pending", at("2026-09-24", "08:00"), nil},
		}},
		{litterNotYetDue, wbPark, "open", []litterStepFixture{
			{"shift_to_k1", "pending", at("2026-09-25", "09:00"), nil},
			{"shift_to_k2", "pending", nil, nil},
		}},
		{litterDoneEarly, wbPark, "open", []litterStepFixture{
			{"shift_to_k1", "completed", at("2026-09-24", "09:00"), at("2026-09-24", "07:00")},
			{"shift_to_k2", "pending", at("2026-10-01", "07:00"), nil},
		}},
		{litterDoneLate, wbPark, "open", []litterStepFixture{
			{"shift_to_k1", "completed", at("2026-09-23", "09:00"), at("2026-09-24", "10:00")},
			{"shift_to_k2", "pending", at("2026-10-01", "10:00"), nil},
		}},
		{litterDoneLateOld, wbPark, "open", []litterStepFixture{
			{"shift_to_k1", "completed", at("2026-09-22", "09:00"), at("2026-09-23", "10:00")},
			{"shift_to_k2", "pending", at("2026-09-30", "10:00"), nil},
		}},
		{litterCanceled, wbPark, "canceled", []litterStepFixture{
			{"shift_to_k1", "canceled", at("2026-09-23", "09:00"), nil},
		}},
		{litterOtherPark, wbOther, "open", []litterStepFixture{
			{"shift_to_k1", "pending", at("2026-09-23", "09:00"), nil},
		}},
		{litterOwedToday, wbPark, "open", []litterStepFixture{
			{"shift_to_k1", "pending", at("2026-09-24", "11:00"), nil},
		}},
	}
	for _, l := range litters {
		wbExec(t, ctx, pool, `
INSERT INTO workflow_instances (workflow_id, tenant_id, template_key, module, subject_ref_id, event_at, event_date,
  park_id, state, actions_total, actions_done, next_action_key, next_action_title, next_due_at, awaiting_verification, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, 'birth_litter', 'birth', $1::uuid, '2026-09-22'::timestamptz, '2026-09-22'::date,
  $3::uuid, $4, 2, 0, 'shift_to_k1', 'Shift the kids to K1', NULL, false, now(), $5)`,
			l.id, wbTenant, l.park, l.state, ist(wbDate, "10:00"))
		for i, st := range l.steps {
			wbExec(t, ctx, pool, `
INSERT INTO workflow_actions (tenant_id, workflow_id, action_key, seq, action_type, title, status, due_at, completed_at,
  task_type, engine_hook, owner_role, target_stage)
VALUES ($1::uuid, $2::uuid, $3, $4, 'action', $3, $5, $6, $7, 'shift_kids_stage', 'shift_kids_stage', 'park_head', 'K1')`,
				wbTenant, l.id, st.key, i+1, st.status, st.due, st.completed)
		}
	}
}

// litterRows lists the Counts lane at wbDate 12:00 IST and returns the ids of every row on it.
func litterRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, q ports.SourceQuery) map[string]bool {
	t.Helper()
	rows, err := laneSource(t, pool, domain.ModuleCounts).ListRows(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, r := range rows {
		out[r.SourceID] = true
	}
	return out
}

func TestLitterBoardStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedLitters(t, ctx, pool)

	got := litterRows(t, ctx, pool, wbQuery())
	want := map[string]bool{litterOwedTwice: true, litterDoneLate: true, litterOwedToday: true}
	for id, label := range map[string]string{
		litterOwedTwice: "owed", litterNotYetDue: "not yet due", litterDoneEarly: "moved before the deadline",
		litterDoneLate: "owed and done today", litterDoneLateOld: "owed and done yesterday, next not due",
		litterCanceled: "canceled", litterOtherPark: "other park", litterOwedToday: "owed since this morning",
	} {
		if got[id] != want[id] {
			t.Errorf("%s litter on the board = %v, want %v", label, got[id], want[id])
		}
	}

	// The operator's own lens never carries the park head's task.
	op := wbQuery()
	op.OwnerUserID = wbUser
	opRows := litterRows(t, ctx, pool, op)
	for _, id := range []string{litterOwedTwice, litterDoneLate, litterOwedToday} {
		if opRows[id] {
			t.Errorf("operator lens carries litter %s", id)
		}
	}
}

func TestLitterBoardOneToManyStepsStayOneRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedLitters(t, ctx, pool)

	rows, err := laneSource(t, pool, domain.ModuleCounts).ListRows(ctx, wbQuery())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		if r.SourceID == litterOwedTwice {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("a litter with two owed steps rendered %d rows, want 1", n)
	}
}

// Keyset pages of one row add up to the whole-filter count, which applies the same predicate.
func TestLitterBoardPaginationMatchesCount(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedLitters(t, ctx, pool)

	src := laneSource(t, pool, domain.ModuleCounts)
	q := wbQuery()
	q.Limit = 1
	listed := 0
	for page := 0; page < 50; page++ {
		rows, err := src.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		listed += len(rows)
		q.AfterSourceID = rows[len(rows)-1].SourceID
	}
	counts, err := src.CountByState(ctx, wbQuery())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	if listed != total {
		t.Fatalf("pages listed %d rows, the count says %d", listed, total)
	}
}

// A late completion surfaces on the board day it happened, not the day before or after.
func TestLitterBoardDateShiftOnlyTheDayItWasDone(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedLitters(t, ctx, pool)

	src := laneSource(t, pool, domain.ModuleCounts).WithClock(func() time.Time { return ist("2026-09-25", "12:00") })
	q := wbQuery()
	q.BusinessDate = "2026-09-25"
	rows, err := src.ListRows(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.SourceID == litterDoneLate {
			t.Fatal("a litter done late on 24/09 is still on the 25/09 board")
		}
	}
}

// Another park's owed litter never reaches this park's board.
func TestLitterBoardParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedLitters(t, ctx, pool)

	if litterRows(t, ctx, pool, wbQuery())[litterOtherPark] {
		t.Fatal("the other park's litter is on this park's board")
	}
	other := wbQuery()
	other.ParkID = wbOther
	if !litterRows(t, ctx, pool, other)[litterOtherPark] {
		t.Fatal("the other park's owed litter is missing from its own board")
	}
}

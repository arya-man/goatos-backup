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

const (
	wbTenant = "00000000-0000-4000-8000-00000000a001"
	wbPark   = "00000000-0000-4000-8000-000000004001"
	wbOther  = "00000000-0000-4000-8000-000000004002"
	wbUser   = "00000000-0000-4000-8000-000000000301"
	wbMember = "00000000-0000-4000-8000-000000000401"
	wbDate   = "2026-09-24"

	wfOpenToday   = "00000000-0000-4000-8000-000000009101" // feed purchase raised today, nothing done
	wfOverdue     = "00000000-0000-4000-8000-000000009102" // animal purchase raised 22/09, next step past due
	wfRework      = "00000000-0000-4000-8000-000000009103" // feed purchase, a step sent back
	wfInReview    = "00000000-0000-4000-8000-000000009104" // animal purchase, nothing left to record
	wfDoneToday   = "00000000-0000-4000-8000-000000009105" // feed purchase raised 21/09, completed today
	wfDoneEarlier = "00000000-0000-4000-8000-000000009106" // completed 22/09: history, not today's
	wfCanceled    = "00000000-0000-4000-8000-000000009107"
	wfOtherPark   = "00000000-0000-4000-8000-000000009108"
	wfTomorrow    = "00000000-0000-4000-8000-000000009109"
	wfSale        = "00000000-0000-4000-8000-000000009110" // Sales lane
	wfShifting    = "00000000-0000-4000-8000-000000009111" // Counts lane
	wfGeneral     = "00000000-0000-4000-8000-000000009112" // Tasks lane (the catch-all)
)

func wbExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

// ist is a wall-clock instant on the farm's calendar.
func ist(date, clock string) time.Time {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	at, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
	if err != nil {
		panic(err)
	}
	return at
}

// Workflow rows are the engine's own derived state; this is the owning package's read-model
// test, so it seeds them directly (they are opened by the engine in production).
func wbSeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	wbExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Board Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, wbTenant)
	wbExec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active'),
       ($3::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')
ON CONFLICT (location_id) DO NOTHING`, wbPark, wbTenant, wbOther)
	wbExec(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'HEM', 'Hemant', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, wbMember, wbTenant, wbUser, wbPark)

	type wf struct {
		id, template, module, park, eventDate, state string
		total, done                                  int
		nextTitle                                    string
		nextDue                                      *time.Time
		awaiting                                     bool
		updatedAt                                    time.Time
	}
	future := ist("2026-09-25", "09:00")
	past := ist("2026-09-23", "09:00")
	flows := []wf{
		{wfOpenToday, "feed_purchase_intake", "procurement", wbPark, wbDate, "open", 4, 0, "Weighbridge slip", &future, false, ist(wbDate, "08:00")},
		{wfOverdue, "animal_purchase_intake", "procurement", wbPark, "2026-09-22", "open", 3, 1, "Arrival at the farm", &past, false, ist("2026-09-22", "08:00")},
		{wfRework, "feed_purchase_intake", "procurement", wbPark, wbDate, "open", 3, 1, "Aflatoxin test signed off", &future, false, ist(wbDate, "08:00")},
		{wfInReview, "animal_purchase_intake", "procurement", wbPark, wbDate, "open", 3, 3, "", nil, true, ist(wbDate, "08:00")},
		{wfDoneToday, "feed_purchase_intake", "procurement", wbPark, "2026-09-21", "completed", 4, 4, "", nil, false, ist(wbDate, "11:00")},
		{wfDoneEarlier, "feed_purchase_intake", "procurement", wbPark, "2026-09-21", "completed", 4, 4, "", nil, false, ist("2026-09-22", "11:00")},
		{wfCanceled, "feed_purchase_intake", "procurement", wbPark, wbDate, "canceled", 4, 0, "", nil, false, ist(wbDate, "08:00")},
		{wfOtherPark, "feed_purchase_intake", "procurement", wbOther, wbDate, "open", 4, 0, "Weighbridge slip", &future, false, ist(wbDate, "08:00")},
		{wfTomorrow, "feed_purchase_intake", "procurement", wbPark, "2026-09-25", "open", 4, 0, "Weighbridge slip", &future, false, ist("2026-09-25", "08:00")},
		{wfSale, "sales_deal", "sales", wbPark, wbDate, "open", 3, 0, "Tag the animals", &future, false, ist(wbDate, "08:00")},
		{wfShifting, "shifting", "shifting", wbPark, wbDate, "open", 2, 0, "Film the move", &future, false, ist(wbDate, "08:00")},
		{wfGeneral, "general:general.gate_visitor_check", "general", wbPark, wbDate, "open", 2, 0, "Check the visitor", &future, false, ist(wbDate, "08:00")},
	}
	for _, f := range flows {
		wbExec(t, ctx, pool, `
INSERT INTO workflow_instances (workflow_id, tenant_id, template_key, module, subject_ref_id, event_at, event_date,
  park_id, state, actions_total, actions_done, next_action_key, next_action_title, next_due_at, awaiting_verification, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $1::uuid, $5::date::timestamptz, $5::date, $6::uuid, $7, $8, $9, NULLIF(lower(replace($10, ' ', '_')), ''), NULLIF($10, ''), $11, $12, $13, $13)`,
			f.id, wbTenant, f.template, f.module, f.eventDate, f.park, f.state, f.total, f.done, f.nextTitle, f.nextDue, f.awaiting, f.updatedAt)
	}
	// wfRework: step 1 done, step 2 sent back, step 3 owed since yesterday. Step 4 is SKIPPED (a
	// branch not taken, or a step a later SOP version dropped) and past due: it is not owed, so it
	// must never reach the board -- read as owed it rendered "Needs attention" on work nobody owes.
	wbExec(t, ctx, pool, `
INSERT INTO workflow_actions (tenant_id, workflow_id, action_key, seq, action_type, title, status, due_at, completed_at, completed_by, rework_reason, requires_video)
VALUES ($1::uuid, $2::uuid, 'slip', 1, 'action', 'Weighbridge slip', 'completed', $3, $3, $4::uuid, NULL, true),
       ($1::uuid, $2::uuid, 'reached', 2, 'action', 'Load reached the farm', 'rework', $3, NULL, NULL, 'slip photo is blurry', true),
       ($1::uuid, $2::uuid, 'paid', 3, 'action', 'Payment settled', 'pending', $5, NULL, NULL, NULL, false),
       ($1::uuid, $2::uuid, 'store_photo', 4, 'action', 'Feed in the store', 'skipped', $5, NULL, NULL, NULL, true)`,
		wbTenant, wfRework, ist("2026-09-23", "10:00"), wbUser, past)
}

func wbQuery() ports.SourceQuery {
	return ports.SourceQuery{TenantID: wbTenant, ParkID: wbPark, BusinessDate: wbDate, Limit: 50}
}

func laneSource(t *testing.T, pool *pgxpool.Pool, lane domain.Module) *Source {
	t.Helper()
	for _, s := range Sources(pool, 5*time.Second) {
		if s.Module() == lane {
			return s.WithClock(func() time.Time { return ist(wbDate, "12:00") })
		}
	}
	t.Fatalf("no source for lane %q", lane)
	return nil
}

// TestEngineWorkflowsRowOnTheBoardOnADatabaseRoundTrip pins the engine source on real Postgres:
// the procurement intakes land in the Procurement lane with the phone card's words; the day
// predicate keeps today's, carries an open one from an earlier day and a completion from today,
// and drops canceled, earlier-completed, other-park and tomorrow's; the state mapping; and the
// other lanes each see only their own engine modules.
func TestEngineWorkflowsRowOnTheBoardOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	wbSeed(t, ctx, pool)

	proc := laneSource(t, pool, domain.ModuleProcurement)
	rows, err := proc.ListRows(ctx, wbQuery())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]domain.Row{}
	for _, r := range rows {
		got[r.SourceID] = r
	}
	want := map[string]domain.WorkState{
		wfOpenToday: domain.WorkStateDue,
		wfOverdue:   domain.WorkStateOverdue,
		wfRework:    domain.WorkStateRejected,
		wfInReview:  domain.WorkStateVerificationPending,
		wfDoneToday: domain.WorkStateCompleted,
	}
	if len(got) != len(want) {
		t.Fatalf("procurement lane rows = %d %v, want %d", len(got), keys(got), len(want))
	}
	for id, state := range want {
		r, ok := got[id]
		if !ok {
			t.Fatalf("workflow %s missing from the procurement lane", id)
		}
		if r.WorkState != state {
			t.Errorf("workflow %s state = %q, want %q", id, r.WorkState, state)
		}
		if r.Module != domain.ModuleProcurement || r.SourceType != SourceType || r.OwnerState != domain.OwnerStatePool {
			t.Errorf("workflow %s row identity = %s/%s/%s", id, r.Module, r.SourceType, r.OwnerState)
		}
		if r.ParkName != "Coimbatore" || r.BusinessDate != wbDate {
			t.Errorf("workflow %s park/date = %q/%q", id, r.ParkName, r.BusinessDate)
		}
	}
	if r := got[wfOpenToday]; r.Title != "Feed purchase" || r.Subtitle != "Next: Weighbridge slip · 0 of 4 steps done" ||
		r.Counts.Pending != 4 || r.Counts.Done != 0 || r.ClockLabel != "Next step due 25/09/2026" {
		t.Errorf("open row = title %q subtitle %q counts %+v clock %q", r.Title, r.Subtitle, r.Counts, r.ClockLabel)
	}
	if r := got[wfOverdue]; r.Title != "Animal purchase" || r.ClockLabel != "Raised 22/09/2026" ||
		r.Severity != domain.SeverityAtRisk || r.Counts.NeedsAttention != 1 {
		t.Errorf("overdue row = title %q clock %q severity %q counts %+v", r.Title, r.ClockLabel, r.Severity, r.Counts)
	}
	if r := got[wfDoneToday]; r.Subtitle != "All 4 steps done" || r.Lane != domain.LaneDone {
		t.Errorf("done row = subtitle %q lane %q", r.Subtitle, r.Lane)
	}

	counts, err := proc.CountByState(ctx, wbQuery())
	if err != nil {
		t.Fatal(err)
	}
	for id, state := range want {
		_ = id
		if counts[state] != 1 {
			t.Errorf("count[%s] = %d, want 1 (whole-filter counts must match the rows)", state, counts[state])
		}
	}

	// Keyset: one row per page walks the lane in workflow_id order and ends.
	q := wbQuery()
	q.Limit = 1
	seen := 0
	for {
		page, err := proc.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		seen++
		q.AfterSourceID = page[0].SourceID
	}
	if seen != len(want) {
		t.Errorf("keyset walk saw %d rows, want %d", seen, len(want))
	}

	// State filter is applied in SQL.
	q = wbQuery()
	q.WorkStates = []domain.WorkState{domain.WorkStateRejected}
	only, err := proc.ListRows(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].SourceID != wfRework {
		t.Errorf("state-filtered rows = %v", only)
	}

	// Every other lane sees only its own engine modules.
	for lane, id := range map[domain.Module]string{domain.ModuleSales: wfSale, domain.ModuleCounts: wfShifting, domain.ModuleTasks: wfGeneral} {
		rs, err := laneSource(t, pool, lane).ListRows(ctx, wbQuery())
		if err != nil {
			t.Fatal(err)
		}
		if len(rs) != 1 || rs[0].SourceID != id || rs[0].Module != lane {
			t.Errorf("lane %s rows = %v, want just %s", lane, rs, id)
		}
	}
	if rs, _ := laneSource(t, pool, domain.ModuleSales).ListRows(ctx, wbQuery()); len(rs) == 1 && rs[0].Title != "Sale" {
		t.Errorf("sale title = %q", rs[0].Title)
	}
}

// TestEngineWorkflowSubtasksAreItsStepsWorstFirst: a workflow drills into its own steps, sent
// back and overdue first, each with a do -> verify chain when the step records proof.
func TestEngineWorkflowSubtasksAreItsStepsWorstFirst(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	wbSeed(t, ctx, pool)
	proc := laneSource(t, pool, domain.ModuleProcurement)

	sq := ports.SubtaskQuery{TenantID: wbTenant, ParkID: wbPark, BusinessDate: wbDate, SourceID: wfRework, Limit: 10}
	page, err := proc.ListSubtasks(ctx, sq)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Subtasks) != 3 {
		t.Fatalf("subtasks = %d of %d, want 3 of 3", len(page.Subtasks), page.Total)
	}
	names := []string{page.Subtasks[0].Name, page.Subtasks[1].Name, page.Subtasks[2].Name}
	if names[0] != "Load reached the farm" || names[1] != "Payment settled" || names[2] != "Weighbridge slip" {
		t.Fatalf("order = %v, want sent back, overdue, done", names)
	}
	sent := page.Subtasks[0]
	if sent.WorkState != domain.WorkStateRejected || !sent.NeedsAttention || sent.Subtitle != "Sent back: slip photo is blurry" || len(sent.Steps) != 2 {
		t.Errorf("sent-back step = %+v", sent)
	}
	if owed := page.Subtasks[1]; owed.WorkState != domain.WorkStateOverdue || len(owed.Steps) != 1 {
		t.Errorf("overdue step = %+v", owed)
	}
	if done := page.Subtasks[2]; done.WorkState != domain.WorkStateCompleted || done.Owner.Name != "Hemant" || done.Subtitle != "Done 23/09/2026" {
		t.Errorf("done step = %+v", done)
	}

	// Paging: two, then the rest.
	sq.Limit = 2
	first, err := proc.ListSubtasks(ctx, sq)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Subtasks) != 2 || first.NextCursor == "" || first.Total != 3 {
		t.Fatalf("first page = %d, cursor %q, total %d", len(first.Subtasks), first.NextCursor, first.Total)
	}
	sq.AfterKey = first.NextCursor
	rest, err := proc.ListSubtasks(ctx, sq)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Subtasks) != 1 || rest.Subtasks[0].Name != "Weighbridge slip" || rest.NextCursor != "" {
		t.Errorf("second page = %+v", rest)
	}

	// Out of scope: another lane, another park, a canceled workflow -> an empty page.
	for _, c := range []struct {
		src *Source
		q   ports.SubtaskQuery
	}{
		{laneSource(t, pool, domain.ModuleSales), ports.SubtaskQuery{TenantID: wbTenant, ParkID: wbPark, BusinessDate: wbDate, SourceID: wfRework}},
		{proc, ports.SubtaskQuery{TenantID: wbTenant, ParkID: wbOther, BusinessDate: wbDate, SourceID: wfRework}},
		{proc, ports.SubtaskQuery{TenantID: wbTenant, ParkID: wbPark, BusinessDate: wbDate, SourceID: wfCanceled}},
	} {
		p, err := c.src.ListSubtasks(ctx, c.q)
		if err != nil {
			t.Fatal(err)
		}
		if p.Total != 0 || len(p.Subtasks) != 0 {
			t.Errorf("out-of-scope drill returned %d subtasks", p.Total)
		}
	}
}

func keys(m map[string]domain.Row) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func startSeeded(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)
	wbSeed(t, ctx, pool)
	return ctx, pool
}

// TestEngineBoardOneToManyStepsStayOneRow: a workflow with MANY steps -- several of them sent
// back -- is ONE board row and counts ONCE; the rework EXISTS and the card joins never fan it out.
func TestEngineBoardOneToManyStepsStayOneRow(t *testing.T) {
	ctx, pool := startSeeded(t)
	wbExec(t, ctx, pool, `
INSERT INTO workflow_actions (tenant_id, workflow_id, action_key, seq, action_type, title, status, rework_reason)
VALUES ($1::uuid, $2::uuid, 'extra1', 4, 'action', 'Extra one', 'rework', 'again'),
       ($1::uuid, $2::uuid, 'extra2', 5, 'action', 'Extra two', 'rework', 'again')`, wbTenant, wfRework)
	src := laneSource(t, pool, domain.ModuleProcurement)
	rows, err := src.ListRows(ctx, wbQuery())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		if r.SourceID == wfRework {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("workflow with three sent-back steps rows %d times, want 1", n)
	}
	counts, err := src.CountByState(ctx, wbQuery())
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.WorkStateRejected] != 1 {
		t.Fatalf("rejected count = %d, want 1", counts[domain.WorkStateRejected])
	}
}

// TestEngineBoardPaginationWalksEveryRowOnce: a one-row page walks the lane in keyset order,
// every row exactly once, and ends; the count is the whole filter whatever the page size.
func TestEngineBoardPaginationWalksEveryRowOnce(t *testing.T) {
	ctx, pool := startSeeded(t)
	src := laneSource(t, pool, domain.ModuleProcurement)
	q := wbQuery()
	q.Limit = 1
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		page, err := src.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if seen[page[0].SourceID] {
			t.Fatalf("row %s served twice", page[0].SourceID)
		}
		seen[page[0].SourceID] = true
		q.AfterSourceID = page[0].SourceID
	}
	counts, err := src.CountByState(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if len(seen) != 5 || total != 5 {
		t.Fatalf("walk saw %d rows, count says %d, want 5 and 5", len(seen), total)
	}
}

// TestEngineBoardParkScopeAndOperatorLens: another park's workflow never shows, and the operator
// lens (an owner filter) keeps every pool row -- engine steps are owned by a designation.
func TestEngineBoardParkScopeAndOperatorLens(t *testing.T) {
	ctx, pool := startSeeded(t)
	src := laneSource(t, pool, domain.ModuleProcurement)
	q := wbQuery()
	q.ParkID = wbOther
	other, err := src.ListRows(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].SourceID != wfOtherPark {
		t.Fatalf("other park rows = %v, want just its own workflow", other)
	}
	q = wbQuery()
	q.OwnerUserID = wbUser
	mine, err := src.ListRows(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 5 {
		t.Fatalf("operator lens rows = %d, want every pool row (5)", len(mine))
	}
}

// TestEngineBoardStatusMatrix: every board state the source can produce is produced by exactly
// the workflow built for it, and a state filter returns exactly that one.
func TestEngineBoardStatusMatrix(t *testing.T) {
	ctx, pool := startSeeded(t)
	src := laneSource(t, pool, domain.ModuleProcurement)
	for id, state := range map[string]domain.WorkState{
		wfOpenToday: domain.WorkStateDue, wfOverdue: domain.WorkStateOverdue, wfRework: domain.WorkStateRejected,
		wfInReview: domain.WorkStateVerificationPending, wfDoneToday: domain.WorkStateCompleted,
	} {
		q := wbQuery()
		q.WorkStates = []domain.WorkState{state}
		rows, err := src.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].SourceID != id {
			t.Errorf("state %s rows = %v, want just %s", state, rows, id)
		}
	}
}

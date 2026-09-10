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
	mkTenant   = "00000000-0000-4000-8000-00000000a001"
	mkPark     = "00000000-0000-4000-8000-00000000a301"
	mkOtherPk  = "00000000-0000-4000-8000-00000000a302"
	mkOperator = "00000000-0000-4000-8000-00000000a501"
	mkOtherOp  = "00000000-0000-4000-8000-00000000a502"
	mkMember   = "00000000-0000-4000-8000-00000000a401"
	mkDate     = "2026-09-10"
)

func exec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

func seedOrg(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, park, otherPark, member, operator string) {
	t.Helper()
	exec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Board Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, tenant)
	exec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active'),
       ($3::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')
ON CONFLICT (location_id) DO NOTHING`, park, tenant, otherPark)
	exec(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIN', 'Dinakar', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, member, tenant, operator, park)
}

// seedMilk lays down four farm sessions in the four live statuses, a retired legacy row, a
// session at the other park and one on another day, so every branch of milkWorkStateSQL and
// every bound of milkBaseWhere is exercised on a database round trip.
func seedMilk(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[int]string {
	t.Helper()
	seedOrg(t, ctx, pool, mkTenant, mkPark, mkOtherPk, mkMember, mkOperator)
	type task struct {
		park, date, status, operator string
		session                      int
	}
	tasks := []task{
		{mkPark, mkDate, "not_submitted", "", 1},
		{mkPark, mkDate, "pending_verification", mkOperator, 2},
		{mkPark, mkDate, "completed", mkOtherOp, 3},
		{mkPark, mkDate, "rework", mkOperator, 4},
		{mkPark, mkDate, "retired", "", 1},
		{mkOtherPk, mkDate, "not_submitted", "", 1},
		{mkPark, "2026-09-11", "not_submitted", "", 1},
	}
	ids := map[int]string{}
	for _, tk := range tasks {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO milk_feeding_tasks (tenant_id, park_id, shed_id, feeding_date, session_no, due_at, head_count, status, assigned_operator_id)
VALUES ($1::uuid, $2::uuid, NULL, $3::date, $4::int, ($3::date + time '08:00' + make_interval(hours => ($4::int - 1) * 4)) AT TIME ZONE 'Asia/Kolkata', 12, $5, nullif($6, '')::uuid)
RETURNING task_id::text`, mkTenant, tk.park, tk.date, tk.session, tk.status, tk.operator).Scan(&id); err != nil {
			t.Fatalf("seed milk task %+v: %v", tk, err)
		}
		if tk.park == mkPark && tk.date == mkDate && tk.status != "retired" {
			ids[tk.session] = id
		}
	}
	return ids
}

func milkQuery(owner string, states ...domain.WorkState) ports.SourceQuery {
	return ports.SourceQuery{TenantID: mkTenant, ParkID: mkPark, BusinessDate: mkDate, OwnerUserID: owner, WorkStates: states, Limit: 50}
}

func bySourceID(rows []domain.Row) map[string]domain.Row {
	out := map[string]domain.Row{}
	for _, r := range rows {
		out[r.SourceID] = r
	}
	return out
}

// TestMilkFeedingBoardRowsOnADatabaseRoundTrip asserts the OUTPUT STRINGS and states of every
// branch on a real database: the status mapping, the farm-grain title, the IST session clock,
// the claim-pool owner state and the owner resolved through the workforce profile.
func TestMilkFeedingBoardRowsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seedMilk(t, ctx, pool)
	src := NewMilkFeeding(pool, 5*time.Second)

	if src.Module() != domain.ModuleMilk || src.SourceType() != MilkFeedingSourceType {
		t.Fatalf("identity %s/%s", src.Module(), src.SourceType())
	}
	rows, err := src.ListRows(ctx, milkQuery(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("4 live sessions expected (retired, other park and other day are out), got %d", len(rows))
	}
	got := bySourceID(rows)
	want := map[int]struct {
		state domain.WorkState
		lane  domain.Lane
		clock string
	}{
		1: {domain.WorkStateDue, domain.LaneToDo, "08:00 session"},
		2: {domain.WorkStateVerificationPending, domain.LaneInReview, "12:00 session"},
		3: {domain.WorkStateCompleted, domain.LaneDone, "16:00 session"},
		4: {domain.WorkStateRejected, domain.LaneInProgress, "20:00 session"},
	}
	for session, w := range want {
		r, ok := got[ids[session]]
		if !ok {
			t.Fatalf("session %d missing", session)
		}
		if r.WorkState != w.state || r.Lane != w.lane || r.ClockLabel != w.clock {
			t.Errorf("session %d: state=%s lane=%s clock=%q, want %s/%s/%q", session, r.WorkState, r.Lane, r.ClockLabel, w.state, w.lane, w.clock)
		}
		if r.Title != "Milk feeding · Coimbatore" || r.Subtitle != "Session "+string(rune('0'+session)) {
			t.Errorf("session %d: title %q subtitle %q", session, r.Title, r.Subtitle)
		}
		if r.RowKey != "milk|"+MilkFeedingSourceType+"|"+ids[session] || r.BusinessDate != mkDate || r.ParkID != mkPark || r.ParkName != "Coimbatore" {
			t.Errorf("session %d: identity/scope %s %s %s %s", session, r.RowKey, r.BusinessDate, r.ParkID, r.ParkName)
		}
		if r.DueAt == nil || r.Pen.Display != "" {
			t.Errorf("session %d: due %v pen %q (farm grain carries no pen)", session, r.DueAt, r.Pen.Display)
		}
	}
	// Unsubmitted: nobody has claimed it, so it is a POOL row, never a missing owner.
	open := got[ids[1]]
	if open.OwnerState != domain.OwnerStatePool || open.Owner.UserID != "" || open.Counts.Pending != 1 || open.Counts.Done != 0 {
		t.Errorf("open session owner %+v %s counts %+v", open.Owner, open.OwnerState, open.Counts)
	}
	// Submitted: the first submitter is the owner, resolved through the workforce profile.
	sub := got[ids[2]]
	if sub.OwnerState != domain.OwnerStateAssigned || sub.Owner.Name != "Dinakar" || sub.Owner.WorkforceMemberID != mkMember || sub.Counts.Done != 1 {
		t.Errorf("submitted session owner %+v %s counts %+v", sub.Owner, sub.OwnerState, sub.Counts)
	}
	// An operator with no workforce profile is still the owner; only the name is blank.
	done := got[ids[3]]
	if done.Owner.UserID != mkOtherOp || done.Owner.Name != "" || done.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("unprofiled owner %+v %s", done.Owner, done.OwnerState)
	}
	rework := got[ids[4]]
	if rework.Counts.NeedsAttention != 1 || rework.Severity != domain.SeverityWatch {
		t.Errorf("rework counts %+v severity %s", rework.Counts, rework.Severity)
	}
}

// TestMilkFeedingBoardScopeAndKeyset: owner scope, state filter and the keyset boundary all
// happen in SQL, and the counts agree with the rows they summarise.
func TestMilkFeedingBoardScopeAndKeyset(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedMilk(t, ctx, pool)
	src := NewMilkFeeding(pool, 5*time.Second)

	mine, err := src.ListRows(ctx, milkQuery(mkOperator))
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 2 {
		t.Fatalf("operator lens: 2 own rows expected, got %d", len(mine))
	}
	for _, r := range mine {
		if r.Owner.UserID != mkOperator {
			t.Fatalf("operator lens leaked %s", r.Owner.UserID)
		}
	}
	open, err := src.ListRows(ctx, milkQuery("", domain.WorkStateDue, domain.WorkStateRejected))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("due+rejected: 2 expected, got %d", len(open))
	}

	seen := map[string]bool{}
	after := ""
	for i := 0; i < 5; i++ {
		q := milkQuery("")
		q.Limit = 2
		q.AfterSourceID = after
		page, err := src.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			if r.SourceID <= after || seen[r.SourceID] {
				t.Fatalf("keyset broken at %s after %s", r.SourceID, after)
			}
			seen[r.SourceID] = true
			after = r.SourceID
		}
	}
	if len(seen) != 4 {
		t.Fatalf("keyset walk saw %d rows, want 4", len(seen))
	}

	counts, err := src.CountByState(ctx, milkQuery(""))
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.WorkStateDue] != 1 || counts[domain.WorkStateVerificationPending] != 1 || counts[domain.WorkStateCompleted] != 1 || counts[domain.WorkStateRejected] != 1 || len(counts) != 4 {
		t.Fatalf("counts %+v", counts)
	}
	otherPark, err := src.CountByState(ctx, ports.SourceQuery{TenantID: mkTenant, ParkID: mkOtherPk, BusinessDate: mkDate})
	if err != nil {
		t.Fatal(err)
	}
	if otherPark[domain.WorkStateDue] != 1 || len(otherPark) != 1 {
		t.Fatalf("the other park sees only its own session, got %+v", otherPark)
	}
}

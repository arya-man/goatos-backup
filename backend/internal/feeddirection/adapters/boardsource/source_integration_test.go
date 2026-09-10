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
	bsTenant   = "00000000-0000-4000-8000-000000000001"
	bsPark     = "00000000-0000-4000-8000-000000003001"
	bsOtherPk  = "00000000-0000-4000-8000-000000003002"
	bsShed     = "00000000-0000-4000-8000-000000003101"
	bsShedB    = "00000000-0000-4000-8000-000000003102"
	bsShedC    = "00000000-0000-4000-8000-000000003103"
	bsShedD    = "00000000-0000-4000-8000-000000003104"
	bsShedE    = "00000000-0000-4000-8000-000000003105"
	bsOperator = "00000000-0000-4000-8000-000000000301"
	bsOtherOp  = "00000000-0000-4000-8000-000000000302"
	bsMember   = "00000000-0000-4000-8000-000000000401"
	bsDate     = "2026-09-10"

	taskDue       = "00000000-0000-4000-8000-000000009101"
	taskVerifying = "00000000-0000-4000-8000-000000009102"
	taskRework    = "00000000-0000-4000-8000-000000009103"
	taskDone      = "00000000-0000-4000-8000-000000009104"
	taskRetired   = "00000000-0000-4000-8000-000000009105"
	taskPenRow    = "00000000-0000-4000-8000-000000009106"
	taskFilmed    = "00000000-0000-4000-8000-000000009107" // never assigned; owned by whoever filmed its attempt
	attemptFilmed = "00000000-0000-4000-8000-000000009901"
)

func exec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

// seed lays down two parks, five sheds, one operator with a workforce profile, and six
// transport tasks -- one per status plus a surviving pen row -- so every branch of
// workStateSQL is exercised on a database round trip. The other park gets one task too, so
// the park bound is proven rather than assumed.
func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Board Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, bsTenant)
	exec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active'),
       ($3::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active'),
       ($4::uuid, $2::uuid, 'shed', 'GODEL1', 'Godel 1', 'active'),
       ($5::uuid, $2::uuid, 'shed', 'CASTRO', 'Castro', 'active'),
       ($6::uuid, $2::uuid, 'shed', 'YASHODA2', 'Yashoda 2', 'active'),
       ($7::uuid, $2::uuid, 'shed', 'MANDELA1', 'Mandela 1', 'active'),
       ($8::uuid, $2::uuid, 'shed', 'GANDHI', 'Gandhi', 'active')
ON CONFLICT (location_id) DO NOTHING`, bsPark, bsTenant, bsOtherPk, bsShed, bsShedB, bsShedC, bsShedD, bsShedE)
	exec(t, ctx, pool, `UPDATE locations SET parent_location_id = $1::uuid WHERE location_id IN ($2::uuid, $3::uuid, $4::uuid, $5::uuid)`, bsPark, bsShed, bsShedB, bsShedC, bsShedD)
	exec(t, ctx, pool, `UPDATE locations SET parent_location_id = $1::uuid WHERE location_id = $2::uuid`, bsOtherPk, bsShedE)
	exec(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIN', 'Dinakar', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, bsMember, bsTenant, bsOperator, bsPark)

	scheduled := "2026-09-10 15:30:00+05:30"
	type task struct {
		id, park, shed, partition, status, operator string
	}
	tasks := []task{
		{taskDue, bsPark, bsShed, "", "due", ""},                               // owed, nobody named yet
		{taskVerifying, bsPark, bsShedB, "", "verification_due", bsOperator},   // filmed, verdict outstanding
		{taskRework, bsPark, bsShedC, "", "rework", bsOperator},                // sent back
		{taskDone, bsPark, bsShedD, "", "completed", bsOtherOp},                // approved, operator has no profile
		{taskRetired, bsPark, bsShedD, "Part 9", "retired", ""},                // superseded pen row: not work
		{taskPenRow, bsPark, bsShed, "Part 3", "verification_due", bsOperator}, // surviving pen row with evidence
		{"00000000-0000-4000-8000-000000009201", bsOtherPk, bsShedE, "", "due", ""},
		{taskFilmed, bsPark, bsShedC, "Part 2", "verification_due", ""}, // materializer named nobody; the attempt names Dinakar
	}
	for _, x := range tasks {
		exec(t, ctx, pool, `
INSERT INTO feed_transport_tasks (task_id, tenant_id, park_id, shed_id, partition_label, business_date, scheduled_at, status, operator_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6::date, $7::timestamptz, $8, NULLIF($9, '')::uuid)
ON CONFLICT (task_id) DO NOTHING`, x.id, bsTenant, x.park, x.shed, x.partition, bsDate, scheduled, x.status, x.operator)
	}
	// The real submit path: the operator lands on the ATTEMPT, and the task points at it.
	exec(t, ctx, pool, `
INSERT INTO feed_transport_attempts (attempt_id, tenant_id, task_id, attempt_no, proof_ref, operator_id, status, idempotency_key, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'proof:board-test', $4::uuid, 'verification_due', 'board-test:' || $1, $5::timestamptz)
ON CONFLICT (attempt_id) DO NOTHING`, attemptFilmed, bsTenant, taskFilmed, bsOperator, scheduled)
	exec(t, ctx, pool, `UPDATE feed_transport_tasks SET current_attempt_id = $1::uuid WHERE task_id = $2::uuid`, attemptFilmed, taskFilmed)
}

func query(owner string, states ...domain.WorkState) ports.SourceQuery {
	return ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, OwnerUserID: owner, WorkStates: states, Limit: 50}
}

func byID(rows []domain.Row) map[string]domain.Row {
	out := map[string]domain.Row{}
	for _, r := range rows {
		out[r.SourceID] = r
	}
	return out
}

// TestFeedTransportBoardRowsOnADatabaseRoundTrip asserts the OUTPUT STRINGS and states of
// every branch on a real database: the status mapping, the pen display through oploc, the
// owner resolved through the workforce profile, the missing owner, and the counts.
func TestFeedTransportBoardRowsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	if src.Module() != domain.ModuleFeed || src.SourceType() != "feed_transport_task" {
		t.Fatalf("identity %s/%s", src.Module(), src.SourceType())
	}
	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("6 live tasks expected (the retired one is not work, the other park is not here), got %d", len(rows))
	}
	got := byID(rows)
	filmed := got[taskFilmed]
	if filmed.Owner.UserID != bsOperator || filmed.Owner.Name != "Dinakar" || filmed.OwnerState != domain.OwnerStateAssigned || filmed.WorkState != domain.WorkStateVerificationPending {
		t.Errorf("a task nobody was assigned to belongs to whoever filmed its attempt: %+v %s %s", filmed.Owner, filmed.OwnerState, filmed.WorkState)
	}
	want := map[string]struct {
		state domain.WorkState
		lane  domain.Lane
	}{
		taskDue:       {domain.WorkStateDue, domain.LaneToDo},
		taskVerifying: {domain.WorkStateVerificationPending, domain.LaneInReview},
		taskRework:    {domain.WorkStateRejected, domain.LaneInProgress},
		taskDone:      {domain.WorkStateCompleted, domain.LaneDone},
		taskPenRow:    {domain.WorkStateVerificationPending, domain.LaneInReview},
	}
	for id, w := range want {
		r, ok := got[id]
		if !ok {
			t.Fatalf("row %s missing", id)
		}
		if r.WorkState != w.state || r.Lane != w.lane || r.Severity != domain.SeverityOK {
			t.Errorf("%s: state=%s lane=%s sev=%s, want %s/%s/ok", id, r.WorkState, r.Lane, r.Severity, w.state, w.lane)
		}
		if r.Module != domain.ModuleFeed || r.SourceType != SourceType || r.RowKey != "feed|"+SourceType+"|"+id {
			t.Errorf("%s: identity %s/%s/%s", id, r.Module, r.SourceType, r.RowKey)
		}
		if r.BusinessDate != bsDate || r.ParkID != bsPark || r.ParkName != "Coimbatore" {
			t.Errorf("%s: scope %s %s %s", id, r.BusinessDate, r.ParkID, r.ParkName)
		}
		if r.ClockLabel != "Stage by 15:00" || r.Subtitle != "One trip · stage by 15:00" || r.DueAt == nil {
			t.Errorf("%s: clock %q subtitle %q due %v", id, r.ClockLabel, r.Subtitle, r.DueAt)
		}
	}
	// Shed-grain row: bare shed name, "Transport <pen>", owner missing (never fabricated).
	due := got[taskDue]
	if due.Pen.Display != "Godel 1" || due.Title != "Transport Godel 1" || due.Pen.ShedID != bsShed {
		t.Errorf("due pen %q title %q shed %s", due.Pen.Display, due.Title, due.Pen.ShedID)
	}
	if due.OwnerState != domain.OwnerStateMissing || due.Owner.UserID != "" || due.Owner.Name != "" {
		t.Errorf("due owner %+v %s, want missing", due.Owner, due.OwnerState)
	}
	if due.Counts != (domain.Counts{Pending: 1}) {
		t.Errorf("due counts %+v", due.Counts)
	}
	// A surviving pen row composes shed + partition through oploc: "Godel 1 - Part 3".
	pen := got[taskPenRow]
	if pen.Pen.Display != "Godel 1 - Part 3" || pen.Title != "Transport Godel 1 - Part 3" || pen.Pen.PartitionLabel != "Part 3" {
		t.Errorf("pen row display %q title %q label %q", pen.Pen.Display, pen.Title, pen.Pen.PartitionLabel)
	}
	if pen.Owner.Name != "Dinakar" || pen.Owner.WorkforceMemberID != bsMember || pen.Owner.UserID != bsOperator || pen.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("pen row owner %+v %s", pen.Owner, pen.OwnerState)
	}
	// Rework is back with the operator and needs attention.
	rework := got[taskRework]
	if rework.Counts != (domain.Counts{Pending: 1, NeedsAttention: 1}) || rework.Pen.Display != "Yashoda 2" {
		t.Errorf("rework counts %+v pen %q", rework.Counts, rework.Pen.Display)
	}
	// Completed is done; an operator with no workforce profile is still the owner, only the
	// name is blank.
	done := got[taskDone]
	if done.Counts != (domain.Counts{Done: 1}) || done.Owner.UserID != bsOtherOp || done.Owner.Name != "" || done.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("done counts %+v owner %+v %s", done.Counts, done.Owner, done.OwnerState)
	}
}

// TestFeedTransportBoardScopeAndKeyset: owner scope, state filter and the keyset boundary
// all happen in SQL, and the counts agree with the rows they summarise.
func TestFeedTransportBoardScopeAndKeyset(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	mine, err := src.ListRows(ctx, query(bsOperator))
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 4 {
		t.Fatalf("operator lens: 4 own rows expected (three assigned, one filmed), got %d", len(mine))
	}
	for _, r := range mine {
		if r.Owner.UserID != bsOperator {
			t.Fatalf("operator lens leaked %s", r.Owner.UserID)
		}
	}

	open, err := src.ListRows(ctx, query("", domain.WorkStateDue, domain.WorkStateRejected))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("due+rejected: 2 expected, got %d", len(open))
	}

	seen := map[string]bool{}
	after := ""
	for i := 0; i < 5; i++ {
		q := query("")
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
			if r.SourceID <= after {
				t.Fatalf("keyset order broken: %s after %s", r.SourceID, after)
			}
			if seen[r.SourceID] {
				t.Fatalf("row %s served twice", r.SourceID)
			}
			seen[r.SourceID] = true
			after = r.SourceID
		}
	}
	if len(seen) != 6 {
		t.Fatalf("keyset walk saw %d rows, want 6", len(seen))
	}

	counts, err := src.CountByState(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 6 || counts[domain.WorkStateDue] != 1 || counts[domain.WorkStateVerificationPending] != 3 || counts[domain.WorkStateRejected] != 1 || counts[domain.WorkStateCompleted] != 1 {
		t.Fatalf("counts %+v", counts)
	}
	mineCounts, err := src.CountByState(ctx, query(bsOperator))
	if err != nil {
		t.Fatal(err)
	}
	if mineCounts[domain.WorkStateVerificationPending] != 3 || mineCounts[domain.WorkStateRejected] != 1 || len(mineCounts) != 2 {
		t.Fatalf("operator counts %+v", mineCounts)
	}
	otherPark, err := src.CountByState(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherPark) != 1 || otherPark[domain.WorkStateDue] != 1 {
		t.Fatalf("the other park sees only its own task, got %+v", otherPark)
	}
	otherDay, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: "2026-09-11", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherDay) != 0 {
		t.Fatalf("another business date must see nothing, got %d", len(otherDay))
	}
}

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
	bsOperator = "00000000-0000-4000-8000-000000000301"
	bsOtherOp  = "00000000-0000-4000-8000-000000000302"
	bsMember   = "00000000-0000-4000-8000-000000000401"
	bsCEO      = "00000000-0000-4000-8000-000000000501"
	bsDate     = "2026-09-10"

	// Task ids are fixed so the round-trip assertions read by name.
	tScanning  = "00000000-0000-4000-8000-000000007101" // deworming, Part 3, 2 of 3 animals filmed
	tUntouched = "00000000-0000-4000-8000-000000007102" // hoof trimming, whole pen, nothing scanned
	tSubmitted = "00000000-0000-4000-8000-000000007103" // deworming, Part 4, awaiting the verdict
	tVerified  = "00000000-0000-4000-8000-000000007104" // ticks removal, accepted; unprofiled owner
	tDelayed   = "00000000-0000-4000-8000-000000007105" // hair trimming, rolled from 06/09; nobody assigned
	tRework    = "00000000-0000-4000-8000-000000007106" // ticks removal, Part 3, sent back
	tClosed    = "00000000-0000-4000-8000-000000007107" // hoof trimming, Part 3, closed
	tCanceled  = "00000000-0000-4000-8000-000000007108" // legacy canceled row: not work
	tOtherPark = "00000000-0000-4000-8000-000000007109" // the other park
	tTomorrow  = "00000000-0000-4000-8000-000000007110" // same park, due tomorrow
)

func exec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

// seed lays down two parks, one shed with partitions, one profiled operator and one
// unprofiled one, and ten tasks covering every branch of workStateSQL plus the scope
// boundaries (other park, other day, canceled).
func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Board Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, bsTenant)
	exec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active'),
       ($3::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active'),
       ($4::uuid, $2::uuid, 'shed', 'GODEL1', 'Godel 1', 'active')
ON CONFLICT (location_id) DO NOTHING`, bsPark, bsTenant, bsOtherPk, bsShed)
	exec(t, ctx, pool, `UPDATE locations SET parent_location_id = $1::uuid WHERE location_id = $2::uuid`, bsPark, bsShed)
	exec(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIN', 'Dinakar', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, bsMember, bsTenant, bsOperator, bsPark)

	type task struct {
		id, category, park, partition, planned, due, workState, status string
		assignees                                                      []string
	}
	tasks := []task{
		{tScanning, "deworming", bsPark, "Part 3", bsDate, bsDate, "scheduled", "open", []string{bsOperator, bsOtherOp}},
		{tUntouched, "hoof_trimming", bsPark, "", bsDate, bsDate, "scheduled", "open", []string{bsOperator}},
		{tSubmitted, "deworming", bsPark, "Part 4", bsDate, bsDate, "scheduled", "pending_verification", []string{bsOperator}},
		{tVerified, "ticks_removal", bsPark, "", bsDate, bsDate, "scheduled", "completed", []string{bsOtherOp}},
		{tDelayed, "hair_trimming", bsPark, "", "2026-09-06", bsDate, "delayed", "open", nil},
		{tRework, "ticks_removal", bsPark, "Part 3", bsDate, bsDate, "scheduled", "rework", []string{bsOtherOp}},
		{tClosed, "hoof_trimming", bsPark, "Part 3", bsDate, bsDate, "closed", "open", []string{bsOtherOp}},
		{tCanceled, "deworming", bsPark, "", bsDate, bsDate, "canceled", "open", []string{bsOperator}},
		{tOtherPark, "deworming", bsOtherPk, "", bsDate, bsDate, "scheduled", "open", []string{bsOperator}},
		{tTomorrow, "deworming", bsPark, "Part 5", bsDate, "2026-09-11", "scheduled", "open", []string{bsOperator}},
	}
	for _, tk := range tasks {
		exec(t, ctx, pool, `
INSERT INTO pc_care_tasks (task_id, tenant_id, category, park_id, shed_id, partition_label, planned_business_date, due_business_date, work_state, status, idempotency_key, created_by,
                           closed_by, close_reason, terminal_at)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, NULLIF($6, ''), $7::date, $8::date, $9, $10, 'board-' || $1, $11::uuid,
        CASE WHEN $9 = 'closed' THEN $11::uuid END, CASE WHEN $9 = 'closed' THEN 'Pen emptied before the work' END, CASE WHEN $9 = 'closed' THEN now() END)
ON CONFLICT (task_id) DO NOTHING`, tk.id, bsTenant, tk.category, tk.park, bsShed, tk.partition, tk.planned, tk.due, tk.workState, tk.status, bsCEO)
		for _, a := range tk.assignees {
			exec(t, ctx, pool, `
INSERT INTO pc_care_task_assignees (tenant_id, task_id, operator_user_id) VALUES ($1::uuid, $2::uuid, $3::uuid) ON CONFLICT DO NOTHING`, bsTenant, tk.id, a)
		}
	}
	// Three animals scanned into the deworming task; two carry their video, one is still
	// only scanned. One animal filmed on the submitted task.
	scan := func(task, tag string, filmed bool) {
		if filmed {
			exec(t, ctx, pool, `
INSERT INTO pc_care_task_animals (tenant_id, task_id, scanned_identifier, scanned_by, video_proof_ref, video_captured_by, video_captured_at, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, 'proof/' || $3, $4::uuid, now(), 'board-' || $2 || '-' || $3) ON CONFLICT DO NOTHING`, bsTenant, task, tag, bsOperator)
			return
		}
		exec(t, ctx, pool, `
INSERT INTO pc_care_task_animals (tenant_id, task_id, scanned_identifier, scanned_by, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, 'board-' || $2 || '-' || $3) ON CONFLICT DO NOTHING`, bsTenant, task, tag, bsOperator)
	}
	scan(tScanning, "tag-a", true)
	scan(tScanning, "tag-b", true)
	scan(tScanning, "tag-c", false)
	scan(tSubmitted, "tag-d", true)
}

func query(owner string, states ...domain.WorkState) ports.SourceQuery {
	return ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, OwnerUserID: owner, WorkStates: states, Limit: 50}
}

func byTask(rows []domain.Row) map[string]domain.Row {
	out := map[string]domain.Row{}
	for _, r := range rows {
		out[r.SourceID] = r
	}
	return out
}

// TestPCCareBoardRowsOnADatabaseRoundTrip asserts the OUTPUT STRINGS and states of every
// branch on a real database: the status-then-clock mapping, the pen display with its
// partition, the category label, the first-assignee owner with its " +N", capture counts,
// and the delayed clock label.
func TestPCCareBoardRowsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 7 {
		t.Fatalf("7 live tasks on the park-day expected (canceled, other park, tomorrow excluded), got %d", len(rows))
	}
	got := byTask(rows)
	want := map[string]struct {
		state domain.WorkState
		lane  domain.Lane
		sev   domain.Severity
	}{
		tScanning:  {domain.WorkStateInProgress, domain.LaneInProgress, domain.SeverityOK},
		tUntouched: {domain.WorkStateDue, domain.LaneToDo, domain.SeverityOK},
		tSubmitted: {domain.WorkStateVerificationPending, domain.LaneInReview, domain.SeverityOK},
		tVerified:  {domain.WorkStateCompleted, domain.LaneDone, domain.SeverityOK},
		tDelayed:   {domain.WorkStateOverdue, domain.LaneToDo, domain.SeverityAtRisk},
		tRework:    {domain.WorkStateRejected, domain.LaneInProgress, domain.SeverityAtRisk},
		tClosed:    {domain.WorkStateCompleted, domain.LaneDone, domain.SeverityOK},
	}
	for id, w := range want {
		r, ok := got[id]
		if !ok {
			t.Fatalf("row %s missing", id)
		}
		if r.WorkState != w.state || r.Lane != w.lane || r.Severity != w.sev {
			t.Errorf("%s: state=%s lane=%s sev=%s, want %s/%s/%s", id, r.WorkState, r.Lane, r.Severity, w.state, w.lane, w.sev)
		}
		if r.Module != domain.ModulePCCare || r.SourceType != SourceType || r.RowKey != "pc_care|"+SourceType+"|"+id {
			t.Errorf("%s: identity %s/%s/%s", id, r.Module, r.SourceType, r.RowKey)
		}
		if r.BusinessDate != bsDate || r.ParkID != bsPark || r.ParkName != "Coimbatore" {
			t.Errorf("%s: scope %s %s %s", id, r.BusinessDate, r.ParkID, r.ParkName)
		}
	}

	// Pen display composes shed + partition through oploc ("Godel 1 - Part 3", never
	// "Godel 1 3"); the title leads with the category's human label.
	scanning := got[tScanning]
	if scanning.Pen.Display != "Godel 1 - Part 3" || scanning.Title != "Deworming · Godel 1 - Part 3" {
		t.Errorf("pen display %q title %q", scanning.Pen.Display, scanning.Title)
	}
	if scanning.Subtitle != "3 animals" || scanning.Counts.Done != 2 || scanning.Counts.Pending != 1 || scanning.Counts.NeedsAttention != 0 {
		t.Errorf("scanning subtitle %q counts %+v, want 3 animals / 2 done / 1 pending", scanning.Subtitle, scanning.Counts)
	}
	// Two assignees: the profiled one sorts first and owns the row; " +1" names the other.
	if scanning.Owner.Name != "Dinakar +1" || scanning.Owner.UserID != bsOperator || scanning.Owner.WorkforceMemberID != bsMember || scanning.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("multi-assignee owner %+v %s", scanning.Owner, scanning.OwnerState)
	}

	// An undivided pen renders the bare shed name; an unscanned task is one pending unit.
	untouched := got[tUntouched]
	if untouched.Pen.Display != "Godel 1" || untouched.Title != "Hoof Trimming · Godel 1" || untouched.Subtitle != "" {
		t.Errorf("whole pen display %q title %q subtitle %q", untouched.Pen.Display, untouched.Title, untouched.Subtitle)
	}
	if untouched.Counts != (domain.Counts{Pending: 1}) || untouched.Owner.Name != "Dinakar" || untouched.ClockLabel != "Planned 10/09/2026" {
		t.Errorf("untouched counts %+v owner %q clock %q", untouched.Counts, untouched.Owner.Name, untouched.ClockLabel)
	}

	// The submitted task's one filmed animal is done, nothing pending.
	submitted := got[tSubmitted]
	if submitted.Counts != (domain.Counts{Done: 1}) || submitted.Subtitle != "1 animal" {
		t.Errorf("submitted counts %+v subtitle %q", submitted.Counts, submitted.Subtitle)
	}

	// The delayed row names its original plan in farm date order and needs attention; with
	// nobody assigned it reports a missing owner rather than a fallback.
	late := got[tDelayed]
	if late.ClockLabel != "Delayed · planned 06/09/2026" || late.Counts.NeedsAttention != 1 {
		t.Errorf("delayed clock %q attention %d", late.ClockLabel, late.Counts.NeedsAttention)
	}
	if late.OwnerState != domain.OwnerStateMissing || late.Owner != (domain.Owner{}) {
		t.Errorf("unassigned owner %+v %s", late.Owner, late.OwnerState)
	}

	// An assignee with no workforce profile is still the owner; only the name is blank.
	verified := got[tVerified]
	if verified.Owner.UserID != bsOtherOp || verified.Owner.Name != "" || verified.Owner.WorkforceMemberID != "" || verified.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("unprofiled owner %+v %s", verified.Owner, verified.OwnerState)
	}
	if verified.Counts != (domain.Counts{Done: 1}) {
		t.Errorf("verified counts %+v", verified.Counts)
	}
	if got[tRework].Counts.NeedsAttention != 1 {
		t.Errorf("rework row must need attention: %+v", got[tRework].Counts)
	}
	for _, r := range rows {
		for _, s := range []string{r.Title, r.Subtitle, r.ClockLabel} {
			if containsFold(s, "shed") {
				t.Errorf("visible copy says shed: %q", s)
			}
		}
	}
}

// TestPCCareBoardScopeAndKeyset: owner scope matches ANY assignee, the state filter and the
// keyset boundary happen in SQL, and the counts agree with the rows they summarise.
func TestPCCareBoardScopeAndKeyset(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	// The second assignee of the scanning task also holds the verified, rework and closed
	// tasks, plus the task with no assignee at all (the park's pool): five rows, including
	// the one where they are NOT the row's displayed owner.
	theirs, err := src.ListRows(ctx, query(bsOtherOp))
	if err != nil {
		t.Fatal(err)
	}
	if len(theirs) != 5 {
		t.Fatalf("second assignee lens: 4 own rows plus the unassigned one expected, got %d", len(theirs))
	}
	shared, ok := byTask(theirs)[tScanning]
	if !ok {
		t.Fatalf("owner scope must match any assignee, not only the displayed owner")
	}
	// On THEIR board the shared task names them, not their partner: the scoped caller is
	// preferred as the displayed owner (the "+1" still says the task is shared).
	if shared.Owner.UserID != bsOtherOp {
		t.Fatalf("scoped caller must be the displayed owner of a shared task, got %+v", shared.Owner)
	}
	mine, err := src.ListRows(ctx, query(bsOperator))
	if err != nil {
		t.Fatal(err)
	}
	// Their three tasks plus the unassigned one (the park's pool).
	if len(mine) != 4 {
		t.Fatalf("operator lens: 3 own rows plus the unassigned one expected, got %d", len(mine))
	}

	open, err := src.ListRows(ctx, query("", domain.WorkStateDue, domain.WorkStateOverdue))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("due+overdue: 2 expected, got %d", len(open))
	}

	seen := map[string]bool{}
	after := ""
	for i := 0; i < 6; i++ {
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
	if len(seen) != 7 {
		t.Fatalf("keyset walk saw %d rows, want 7", len(seen))
	}

	counts, err := src.CountByState(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 7 || counts[domain.WorkStateCompleted] != 2 || counts[domain.WorkStateDue] != 1 || counts[domain.WorkStateOverdue] != 1 ||
		counts[domain.WorkStateInProgress] != 1 || counts[domain.WorkStateVerificationPending] != 1 || counts[domain.WorkStateRejected] != 1 {
		t.Fatalf("counts %+v", counts)
	}
	ownerCounts, err := src.CountByState(ctx, query(bsOtherOp))
	if err != nil {
		t.Fatal(err)
	}
	if n := sum(ownerCounts); n != 5 {
		t.Fatalf("owner-scoped counts %+v total %d, want 5 (the counts describe the same set as the rows, pool included)", ownerCounts, n)
	}
	otherPark, err := src.CountByState(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate})
	if err != nil {
		t.Fatal(err)
	}
	if sum(otherPark) != 1 || otherPark[domain.WorkStateDue] != 1 {
		t.Fatalf("the other park sees only its own task, got %+v", otherPark)
	}
}

func sum(m map[domain.WorkState]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func containsFold(s, sub string) bool {
	if len(sub) == 0 || len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

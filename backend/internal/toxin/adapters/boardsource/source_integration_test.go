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
	txTenant = "00000000-0000-4000-8000-00000000b001"
	txPark   = "00000000-0000-4000-8000-00000000b101"
	txOther  = "00000000-0000-4000-8000-00000000b102"
	txUser   = "00000000-0000-4000-8000-00000000b201"
	txMember = "00000000-0000-4000-8000-00000000b301"
	txDate   = "2026-09-24"

	tOpenOld        = "00000000-0000-4000-8000-00000000c001" // opened 22/09, nothing done
	tStarted        = "00000000-0000-4000-8000-00000000c002" // opened today, steps 1 and 2 filmed
	tInReview       = "00000000-0000-4000-8000-00000000c003" // reading submitted
	tAcceptedToday  = "00000000-0000-4000-8000-00000000c004" // signed off today, Positive
	tAcceptedBefore = "00000000-0000-4000-8000-00000000c005" // signed off 23/09: not today's
	tCancelled      = "00000000-0000-4000-8000-00000000c006"
	tRetest         = "00000000-0000-4000-8000-00000000c007" // retest after a refused review
	tOtherPark      = "00000000-0000-4000-8000-00000000c008"
	tTomorrow       = "00000000-0000-4000-8000-00000000c009"
)

func txExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

func txAt(date, clock string) time.Time {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	at, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
	if err != nil {
		panic(err)
	}
	return at
}

// Rounds are the toxin module's own runtime rows; this is the owning package's read-model test.
func txSeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	txExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Toxin Board', 'active') ON CONFLICT (tenant_id) DO NOTHING`, txTenant)
	txExec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active'),
       ($3::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')
ON CONFLICT (location_id) DO NOTHING`, txPark, txTenant, txOther)
	txExec(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIN', 'Dinakar', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, txMember, txTenant, txUser, txPark)
	type round struct {
		id, farm, status, origin, outcome string
		created                           time.Time
		reviewed                          *time.Time
	}
	reviewedToday, reviewedBefore := txAt(txDate, "15:00"), txAt("2026-09-23", "15:00")
	rounds := []round{
		{tOpenOld, "CBE", "in_progress", "purchase", "", txAt("2026-09-22", "10:00"), nil},
		{tStarted, "cbe", "in_progress", "purchase", "", txAt(txDate, "09:00"), nil},
		{tInReview, "CBE", "pending_review", "purchase", "negative", txAt("2026-09-23", "09:00"), nil},
		{tAcceptedToday, "CBE", "accepted", "purchase", "positive", txAt("2026-09-23", "09:00"), &reviewedToday},
		{tAcceptedBefore, "CBE", "accepted", "purchase", "negative", txAt("2026-09-21", "09:00"), &reviewedBefore},
		{tCancelled, "CBE", "cancelled", "purchase", "invalid", txAt("2026-09-22", "09:00"), nil},
		{tRetest, "CBE", "in_progress", "rejected_retest", "", txAt(txDate, "10:00"), nil},
		{tOtherPark, "CPT", "in_progress", "purchase", "", txAt(txDate, "09:00"), nil},
		{tTomorrow, "CBE", "in_progress", "purchase", "", txAt("2026-09-25", "09:00"), nil},
	}
	for i, r := range rounds {
		txExec(t, ctx, pool, `
INSERT INTO toxin_test_tasks (tenant_id, task_id, feed_purchase_id, farm_label, feed_item_key, feed_item_label, vendor,
  batch_no, purchase_date, quantity_kg, status, origin, outcome, created_at, reviewed_at)
VALUES ($1::uuid, $2::uuid, gen_random_uuid(), $3, 'dry_masoor_bhusa', 'Dry Masoor Bhusa', 'Sri Balaji',
  $4, '2026-09-20', 2400, $5, $6, NULLIF($7, ''), $8, $9)`,
			txTenant, r.id, r.farm, 350+i, r.status, r.origin, r.outcome, r.created, r.reviewed)
	}
	txExec(t, ctx, pool, `
INSERT INTO toxin_test_step_completions (tenant_id, task_id, step_no, proof_ref, completed_by, completed_at)
VALUES ($1::uuid, $2::uuid, 1, 'proof-c002-1', $3::uuid, $4), ($1::uuid, $2::uuid, 2, 'proof-c002-2', $3::uuid, $5)`,
		txTenant, tStarted, txUser, txAt(txDate, "09:10"), txAt(txDate, "09:20"))
}

func txQuery() ports.SourceQuery {
	return ports.SourceQuery{TenantID: txTenant, ParkID: txPark, BusinessDate: txDate, Limit: 50}
}

// TestToxinRoundsRowOnTheBoardOnADatabaseRoundTrip pins the toxin source on real Postgres: a
// round rows under Toxin at its park (resolved from the load's park code, case-insensitively)
// from the day it was opened until the day it is signed off; the state mapping; the procedure's
// step counts; and the title and subtitle words.
func TestToxinRoundsRowOnTheBoardOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	txSeed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	rows, err := src.ListRows(ctx, txQuery())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]domain.WorkState{
		tOpenOld:       domain.WorkStateDue,
		tStarted:       domain.WorkStateInProgress,
		tInReview:      domain.WorkStateVerificationPending,
		tAcceptedToday: domain.WorkStateCompleted,
		tRetest:        domain.WorkStateRejected,
	}
	got := map[string]domain.Row{}
	for _, r := range rows {
		got[r.SourceID] = r
	}
	if len(got) != len(want) {
		t.Fatalf("toxin rows = %d, want %d: %+v", len(got), len(want), rows)
	}
	for id, state := range want {
		r, ok := got[id]
		if !ok {
			t.Fatalf("round %s missing", id)
		}
		if r.WorkState != state || r.Module != domain.ModuleToxin || r.OwnerState != domain.OwnerStatePool || r.ParkName != "Coimbatore" {
			t.Errorf("round %s = state %q module %q owner %q park %q", id, r.WorkState, r.Module, r.OwnerState, r.ParkName)
		}
		if r.Title != "Aflatoxin test · Dry Masoor Bhusa" {
			t.Errorf("round %s title = %q", id, r.Title)
		}
	}
	if r := got[tOpenOld]; r.ClockLabel != "Owed since 22/09/2026" || r.Counts.Pending != 6 || r.Counts.Done != 0 {
		t.Errorf("open round = clock %q counts %+v", r.ClockLabel, r.Counts)
	}
	if r := got[tStarted]; r.Counts.Done != 2 || r.Counts.Pending != 4 || r.ClockLabel != "" {
		t.Errorf("started round = counts %+v clock %q", r.Counts, r.ClockLabel)
	}
	if r := got[tAcceptedToday]; r.Severity != domain.SeverityWatch || r.Subtitle != "Load 353 · Sri Balaji · 2400 kg · Positive" {
		t.Errorf("accepted round = severity %q subtitle %q", r.Severity, r.Subtitle)
	}
	if r := got[tRetest]; r.Subtitle != "Load 356 · Sri Balaji · 2400 kg · Retest — review sent the last test back" {
		t.Errorf("retest subtitle = %q", r.Subtitle)
	}

	counts, err := src.CountByState(ctx, txQuery())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range want {
		if counts[state] != 1 {
			t.Errorf("count[%s] = %d, want 1", state, counts[state])
		}
	}

	// The drill: the procedure's seven steps, the next one in progress, the two filmed ones done
	// with the tester's name.
	page, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: txTenant, ParkID: txPark, BusinessDate: txDate, SourceID: tStarted, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 7 {
		t.Fatalf("subtasks total = %d, want 7", page.Total)
	}
	var done, inProgress int
	for _, st := range page.Subtasks {
		switch st.WorkState {
		case domain.WorkStateCompleted:
			done++
			if st.Owner.Name != "Dinakar" {
				t.Errorf("done step %q owner = %q", st.Name, st.Owner.Name)
			}
		case domain.WorkStateInProgress:
			inProgress++
		}
	}
	if done != 2 || inProgress != 1 {
		t.Errorf("done %d in progress %d, want 2 and 1", done, inProgress)
	}
	// A round the board would not show on that day is an empty drill.
	empty, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: txTenant, ParkID: txPark, BusinessDate: txDate, SourceID: tAcceptedBefore})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Total != 0 {
		t.Errorf("out-of-day drill returned %d", empty.Total)
	}
}

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
	hbTenant   = "00000000-0000-4000-8000-00000000c001"
	hbParty    = "00000000-0000-4000-8000-00000000c101"
	hbPark     = "00000000-0000-4000-8000-00000000c301"
	hbOtherPk  = "00000000-0000-4000-8000-00000000c302"
	hbShed     = "00000000-0000-4000-8000-00000000c311"
	hbOperator = "00000000-0000-4000-8000-00000000c501"
	hbMember   = "00000000-0000-4000-8000-00000000c401"
	hbGoat     = "00000000-0000-4000-8000-00000000c601"
	hbProtocol = "00000000-0000-4000-8000-00000000c801"
	hbCase     = "00000000-0000-4000-8000-00000000c901"
	hbCaseOth  = "00000000-0000-4000-8000-00000000c902"
	// A future business date, so a 'scheduled' session with a same-day due_at stays scheduled
	// however late in the day the suite runs; the passed-due branch uses a fixed past instant.
	hbDate = "2027-03-15"
)

func exec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

// seed lays down a park, a shed, one animal (an external fact the case needs; the source
// never reads it), one published protocol, a case per park and nine sessions across every
// status, so every branch of workStateSQL is exercised on a database round trip.
func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[int]string {
	t.Helper()
	exec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Board Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, hbTenant)
	exec(t, ctx, pool, `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'org', 'Board Custodian', 'active') ON CONFLICT DO NOTHING`, hbParty)
	exec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active'),
       ($3::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active'),
       ($4::uuid, $2::uuid, 'shed', 'GODEL1', 'Godel 1', 'active')
ON CONFLICT (location_id) DO NOTHING`, hbPark, hbTenant, hbOtherPk, hbShed)
	exec(t, ctx, pool, `UPDATE locations SET parent_location_id = $1::uuid WHERE location_id = $2::uuid`, hbPark, hbShed)
	exec(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIN', 'Dinakar', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, hbMember, hbTenant, hbOperator, hbPark)
	exec(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, age_band, custodian_party_id, park_id, shed_id, current_location_id, origin_type, dob, entry_date)
VALUES ($1::uuid, $2::uuid, 'G-880001', 'goat', 'female', 'alive', 'adult', $3::uuid, $4::uuid, $5::uuid, $5::uuid, 'procured', DATE '2024-01-01', DATE '2024-01-01')
ON CONFLICT (goat_id) DO NOTHING`, hbGoat, hbTenant, hbParty, hbPark, hbShed)
	exec(t, ctx, pool, `
INSERT INTO health_protocol_versions (health_protocol_version_id, tenant_id, disease_key, display_name, age_band, version, duration_days, status, content_hash, published_at)
VALUES ($1::uuid, $2::uuid, 'mastitis', 'Mastitis', 'adult', 1, 3, 'published', 'board-hash', now())
ON CONFLICT (health_protocol_version_id) DO NOTHING`, hbProtocol, hbTenant)
	for _, c := range []struct{ id, park, partition, key string }{{hbCase, hbPark, "Part 3", "a"}, {hbCaseOth, hbOtherPk, "", "b"}} {
		exec(t, ctx, pool, `
INSERT INTO health_cases (health_case_id, tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name, age_band, start_date, duration_days, status, park_id, shed_id, partition_label, idempotency_key, request_fingerprint)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'mastitis', 'Mastitis', 'adult', $5::date, 3, 'active', $6::uuid, $7::uuid, nullif($8, ''), 'board-case-' || $9, 'fp-' || $9)
ON CONFLICT (health_case_id) DO NOTHING`, c.id, hbTenant, hbGoat, hbProtocol, hbDate, c.park, hbShed, c.partition, c.key)
	}

	type session struct {
		caseID, date, status, dueAt, completedBy string
		dayNo                                    int
	}
	future := hbDate + " 09:00"
	past := "2020-01-01 09:00"
	sessions := []session{
		{hbCase, hbDate, "scheduled", future, "", 1},
		{hbCase, hbDate, "scheduled", past, "", 2}, // scheduled but the clock has passed: due, as the module's own worklist says
		{hbCase, hbDate, "due", future, "", 3},
		{hbCase, hbDate, "in_progress", future, "", 4},
		{hbCase, hbDate, "completed", future, hbOperator, 5},
		{hbCase, hbDate, "rework", future, "", 6},
		{hbCase, hbDate, "held_death_review", future, "", 7},
		{hbCase, hbDate, "canceled_death", future, "", 8},
		{hbCase, hbDate, "canceled", future, "", 9},
		{hbCase, "2027-03-16", "scheduled", "2027-03-16 09:00", "", 10},
		{hbCaseOth, hbDate, "due", future, "", 1},
	}
	ids := map[int]string{}
	for _, s := range sessions {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO health_treatment_sessions (tenant_id, health_case_id, goat_id, day_no, business_date, session, due_at, status, completed_by, completed_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::date, 'morning', ($6 || ':00')::timestamp AT TIME ZONE 'Asia/Kolkata', $7,
        nullif($8, '')::uuid, CASE WHEN $8 <> '' THEN now() END)
RETURNING health_session_id::text`, hbTenant, s.caseID, hbGoat, s.dayNo, s.date, s.dueAt, s.status, s.completedBy).Scan(&id); err != nil {
			t.Fatalf("seed session %+v: %v", s, err)
		}
		if s.caseID == hbCase && s.date == hbDate {
			ids[s.dayNo] = id
		}
	}
	// Three steps on the in-progress session: two done, one still pending.
	for i, status := range []string{"completed", "completed", "pending"} {
		exec(t, ctx, pool, `
INSERT INTO health_session_steps (tenant_id, health_session_id, seq, record_type, medicine_name, status)
VALUES ($1::uuid, $2::uuid, $3, 'medication', 'Enrofloxacin', $4)`, hbTenant, ids[4], i+1, status)
	}
	return ids
}

func query(owner string, states ...domain.WorkState) ports.SourceQuery {
	return ports.SourceQuery{TenantID: hbTenant, ParkID: hbPark, BusinessDate: hbDate, OwnerUserID: owner, WorkStates: states, Limit: 50}
}

func bySourceID(rows []domain.Row) map[string]domain.Row {
	out := map[string]domain.Row{}
	for _, r := range rows {
		out[r.SourceID] = r
	}
	return out
}

// TestHealthBoardRowsOnADatabaseRoundTrip asserts the OUTPUT STRINGS and states of every
// branch on a real database: the status mapping (including read-time due and the
// completed-is-done decision), the pen display with the case's snapshotted partition, the
// IST session clock, the step counts and the pool/assigned owner states.
func TestHealthBoardRowsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	if src.Module() != domain.ModuleHealth || src.SourceType() != SourceType {
		t.Fatalf("identity %s/%s", src.Module(), src.SourceType())
	}
	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 7 {
		t.Fatalf("7 live sessions expected (canceled_death, canceled, other day and other park are out), got %d", len(rows))
	}
	got := bySourceID(rows)
	want := map[int]struct {
		state domain.WorkState
		lane  domain.Lane
		sev   domain.Severity
	}{
		1: {domain.WorkStateScheduled, domain.LaneToDo, domain.SeverityOK},
		2: {domain.WorkStateDue, domain.LaneToDo, domain.SeverityOK},
		3: {domain.WorkStateDue, domain.LaneToDo, domain.SeverityOK},
		4: {domain.WorkStateInProgress, domain.LaneInProgress, domain.SeverityOK},
		5: {domain.WorkStateCompleted, domain.LaneDone, domain.SeverityOK},
		6: {domain.WorkStateRejected, domain.LaneInProgress, domain.SeverityWatch},
		7: {domain.WorkStateBlocked, domain.LaneInProgress, domain.SeverityAtRisk},
	}
	for day, w := range want {
		r, ok := got[ids[day]]
		if !ok {
			t.Fatalf("day %d missing", day)
		}
		if r.WorkState != w.state || r.Lane != w.lane || r.Severity != w.sev {
			t.Errorf("day %d: state=%s lane=%s sev=%s, want %s/%s/%s", day, r.WorkState, r.Lane, r.Severity, w.state, w.lane, w.sev)
		}
		if r.Subtitle != "Morning · Godel 1 - Part 3" || r.Pen.Display != "Godel 1 - Part 3" || r.Pen.ShedID != hbShed {
			t.Errorf("day %d: subtitle %q pen %+v", day, r.Subtitle, r.Pen)
		}
		if r.RowKey != "health|"+SourceType+"|"+ids[day] || r.BusinessDate != hbDate || r.ParkID != hbPark || r.ParkName != "Coimbatore" || r.DueAt == nil {
			t.Errorf("day %d: identity/scope %s %s %s %s %v", day, r.RowKey, r.BusinessDate, r.ParkID, r.ParkName, r.DueAt)
		}
	}
	first := got[ids[1]]
	if first.Title != "Mastitis · Day 1" || first.ClockLabel != "09:00 session" || first.OwnerState != domain.OwnerStatePool || first.Owner.UserID != "" {
		t.Errorf("scheduled row title %q clock %q owner %+v %s", first.Title, first.ClockLabel, first.Owner, first.OwnerState)
	}
	if first.Counts.Pending != 1 || first.Counts.Done != 0 {
		t.Errorf("a session with no steps is one piece of work: %+v", first.Counts)
	}
	working := got[ids[4]]
	if working.Counts.Done != 2 || working.Counts.Pending != 1 {
		t.Errorf("step counts %+v, want 2 done / 1 pending", working.Counts)
	}
	done := got[ids[5]]
	if done.Owner.Name != "Dinakar" || done.Owner.UserID != hbOperator || done.Owner.WorkforceMemberID != hbMember || done.OwnerState != domain.OwnerStateAssigned || done.Counts.Done != 1 {
		t.Errorf("completed row owner %+v %s counts %+v", done.Owner, done.OwnerState, done.Counts)
	}
	if got[ids[6]].Counts.NeedsAttention != 1 || got[ids[7]].Counts.NeedsAttention != 1 {
		t.Errorf("rework/held rows need attention: %+v %+v", got[ids[6]].Counts, got[ids[7]].Counts)
	}
}

// TestHealthBoardScopeAndKeyset: the owner lens matches completed_by, the state filter and
// keyset happen in SQL, and the counts agree with the rows they summarise.
func TestHealthBoardScopeAndKeyset(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	mine, err := src.ListRows(ctx, query(hbOperator))
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].Owner.UserID != hbOperator {
		t.Fatalf("operator lens: exactly the session they completed, got %d", len(mine))
	}
	open, err := src.ListRows(ctx, query("", domain.WorkStateDue, domain.WorkStateScheduled))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("due+scheduled: 3 expected, got %d", len(open))
	}

	seen := map[string]bool{}
	after := ""
	for i := 0; i < 8; i++ {
		q := query("")
		q.Limit = 3
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
	if total != 7 || counts[domain.WorkStateDue] != 2 || counts[domain.WorkStateScheduled] != 1 || counts[domain.WorkStateCompleted] != 1 || counts[domain.WorkStateBlocked] != 1 {
		t.Fatalf("counts %+v", counts)
	}
	otherPark, err := src.CountByState(ctx, ports.SourceQuery{TenantID: hbTenant, ParkID: hbOtherPk, BusinessDate: hbDate})
	if err != nil {
		t.Fatal(err)
	}
	if otherPark[domain.WorkStateDue] != 1 || len(otherPark) != 1 {
		t.Fatalf("the other park sees only its own session, got %+v", otherPark)
	}
}

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
	apTenant   = "00000000-0000-4000-8000-00000000b001"
	apPark     = "00000000-0000-4000-8000-00000000b301"
	apOtherPk  = "00000000-0000-4000-8000-00000000b302"
	apShed     = "00000000-0000-4000-8000-00000000b311"
	apRaiser   = "00000000-0000-4000-8000-00000000b501"
	apApprover = "00000000-0000-4000-8000-00000000b502"
	apMember   = "00000000-0000-4000-8000-00000000b401"
	apGoat     = "00000000-0000-4000-8000-00000000b601"
	apEventA   = "00000000-0000-4000-8000-00000000b701"
	apEventB   = "00000000-0000-4000-8000-00000000b702"
	apEventC   = "00000000-0000-4000-8000-00000000b703" // approved, authorized: the walk is owed
	apEventD   = "00000000-0000-4000-8000-00000000b704" // approved, pending_verification: filmed
	apEventE   = "00000000-0000-4000-8000-00000000b705" // approved, applied: done
	apEventF   = "00000000-0000-4000-8000-00000000b706" // approved, canceled: never happened
	apParty    = "00000000-0000-4000-8000-00000000b801"
	apDate     = "2026-09-10"
)

func seedShiftingEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, park string) {
	seedShiftingEventInState(t, ctx, pool, eventID, park, "pending", "pending")
}

func seedShiftingEventInState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, park, eventStatus, authorization string) {
	t.Helper()
	exec(t, ctx, pool, `
INSERT INTO shifting_events (shifting_event_id, tenant_id, logical_shifting_event_key, priority, category,
  destination_park_id, destination_shed_id, raised_at, effective_at, source_system, source_ref,
  payload_hash, idempotency_key, request_fingerprint, event_status, authorization_state, proof_ref,
  applied_at, canceled_at, canceled_by, cancel_reason)
VALUES ($1::uuid, $2::uuid, 'board-' || $1, 'low', 'growth', $3::uuid, $4::uuid, now(), now(),
  'goatos_canonical', 'board-test', 'hash-' || $1, 'board-shift-' || $1, 'fp-' || $1, $5, $6,
  CASE WHEN $5 IN ('pending_verification', 'applied') THEN 'proof:board-test:' || $1 END,
  CASE WHEN $5 = 'applied' THEN now() END,
  CASE WHEN $5 = 'canceled' THEN now() END,
  CASE WHEN $5 = 'canceled' THEN $7::uuid END,
  CASE WHEN $5 = 'canceled' THEN 'board test' END)
ON CONFLICT (shifting_event_id) DO NOTHING`, eventID, apTenant, park, apShed, eventStatus, authorization, apRaiser)
}

type approvalSeed struct {
	key, requestType, status, payload, raisedAtIST string
	shiftingEvent, subjectGoat                     string
}

// seedApprovals lays down the three statuses across birth and pen-move requests, a death
// (attributed to its subject animal's own park and pen), a pen move at the other park, and
// two rows that sit on either side of the IST day boundary.
func seedApprovals(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	seedOrg(t, ctx, pool, apTenant, apPark, apOtherPk, apMember, apRaiser)
	exec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'GODEL1', 'Godel 1', $3::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, apShed, apTenant, apPark)
	seedShiftingEvent(t, ctx, pool, apEventA, apPark)
	seedShiftingEvent(t, ctx, pool, apEventB, apOtherPk)
	seedShiftingEventInState(t, ctx, pool, apEventC, apPark, "authorized", "authorized")
	seedShiftingEventInState(t, ctx, pool, apEventD, apPark, "pending_verification", "authorized")
	seedShiftingEventInState(t, ctx, pool, apEventE, apPark, "applied", "authorized")
	seedShiftingEventInState(t, ctx, pool, apEventF, apPark, "canceled", "authorized")
	// The death's subject animal lives in Godel 1 - Part 3 at this park; the board reads its
	// park and pen from the animal itself (goats.custodian_party_id is a real FK).
	exec(t, ctx, pool, `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'org', 'Board Farm', 'active') ON CONFLICT (party_id) DO NOTHING`, apParty)
	exec(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, species, sex, lifecycle_status, custodian_party_id, park_id, shed_id, current_location_id, origin_type, dob, entry_date)
VALUES ($1::uuid, $2::uuid, 'goat', 'female', 'alive', $3::uuid, $4::uuid, $5::uuid, $5::uuid, 'birth', DATE '2026-07-01', DATE '2026-07-01')
ON CONFLICT (goat_id) DO NOTHING`, apGoat, apTenant, apParty, apPark, apShed)
	exec(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Part 3', 'Godel 1 - Part 3')
ON CONFLICT (tenant_id, goat_id) DO NOTHING`, apTenant, apGoat, apShed)

	shift := `{"shifting_event_id":"` + apEventA + `","destination_park_id":"` + apPark + `","destination_shed_id":"` + apShed + `","destination_partition_label":"Part 3","goat_ids":["a","b","c"]}`
	shiftFor := func(event string) string {
		return `{"shifting_event_id":"` + event + `","destination_park_id":"` + apPark + `","destination_shed_id":"` + apShed + `","destination_partition_label":"Part 3","goat_ids":["a","b"]}`
	}
	shiftOther := `{"shifting_event_id":"` + apEventB + `","destination_park_id":"` + apOtherPk + `","destination_shed_id":"` + apShed + `","goat_ids":["a"]}`
	birth := `{"park_id":"` + apPark + `","shed_id":"` + apShed + `","litter_size":1,"children":[{"child_ordinal":1}]}`
	death := `{"goat_id":"` + apGoat + `","reason":"illness"}`

	seeds := []approvalSeed{
		{"shift-pending", "shifting", "pending", shift, apDate + " 09:15", apEventA, ""},
		{"birth-pending", "birth", "pending", birth, apDate + " 00:10", "", ""}, // inside the IST day, outside a UTC one
		{"birth-approved", "birth", "approved", birth, apDate + " 11:00", "", ""},
		{"birth-rejected", "birth", "rejected", birth, apDate + " 12:00", "", ""},
		{"death-pending", "death", "pending", death, apDate + " 13:00", "", apGoat},
		{"shift-other-park", "shifting", "pending", shiftOther, apDate + " 14:00", apEventB, ""},
		// Approved pen moves follow their event: approving moves nothing.
		{"shift-authorized", "shifting", "approved", shiftFor(apEventC), apDate + " 15:00", apEventC, ""},
		{"shift-filmed", "shifting", "approved", shiftFor(apEventD), apDate + " 15:10", apEventD, ""},
		{"shift-applied", "shifting", "approved", shiftFor(apEventE), apDate + " 15:20", apEventE, ""},
		{"shift-canceled", "shifting", "approved", shiftFor(apEventF), apDate + " 15:30", apEventF, ""},
		{"birth-yesterday", "birth", "pending", birth, "2026-09-09 23:50", "", ""},
	}
	ids := map[string]string{}
	for _, s := range seeds {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO counts_approval_requests (tenant_id, request_type, payload, shifting_event_id, subject_goat_id, status,
  raised_by_user_id, raised_at, decided_by_user_id, decided_at, decision_reason, applied_result_type, applied_result_id,
  idempotency_key, request_fingerprint)
VALUES ($1::uuid, $2, $3::jsonb, nullif($4, '')::uuid, nullif($5, '')::uuid, $6,
  $7::uuid, ($8 || ':00')::timestamp AT TIME ZONE 'Asia/Kolkata',
  CASE WHEN $6 = 'pending' THEN NULL ELSE $9::uuid END,
  CASE WHEN $6 = 'pending' THEN NULL ELSE ($8 || ':00')::timestamp AT TIME ZONE 'Asia/Kolkata' END,
  CASE WHEN $6 = 'rejected' THEN 'not this pen' END,
  CASE WHEN $6 = 'approved' THEN 'goat' END,
  CASE WHEN $6 = 'approved' THEN $10::uuid END,
  'board-' || $11, 'fp-' || $11)
RETURNING approval_request_id::text`,
			apTenant, s.requestType, s.payload, s.shiftingEvent, s.subjectGoat, s.status,
			apRaiser, s.raisedAtIST, apApprover, apGoat, s.key).Scan(&id); err != nil {
			t.Fatalf("seed approval %s: %v", s.key, err)
		}
		ids[s.key] = id
	}
	return ids
}

func approvalQuery(owner string, states ...domain.WorkState) ports.SourceQuery {
	return ports.SourceQuery{TenantID: apTenant, ParkID: apPark, BusinessDate: apDate, OwnerUserID: owner, WorkStates: states, Limit: 50}
}

// TestApprovalsBoardRowsOnADatabaseRoundTrip asserts the OUTPUT STRINGS and states on a real
// database: the status mapping, "Pen move" never "Shifting", the pen display with its
// partition, the raiser in the subtitle, the pool owner state and the IST day bound.
func TestApprovalsBoardRowsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seedApprovals(t, ctx, pool)
	src := NewApprovals(pool, 5*time.Second)

	if src.Module() != domain.ModuleCounts || src.SourceType() != ApprovalsSourceType {
		t.Fatalf("identity %s/%s", src.Module(), src.SourceType())
	}
	rows, err := src.ListRows(ctx, approvalQuery(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 7 {
		t.Fatalf("7 rows expected (other park, yesterday, the canceled move and the rejected birth are out), got %d", len(rows))
	}
	got := bySourceID(rows)
	if _, leaked := got[ids["shift-canceled"]]; leaked {
		t.Fatal("an approved move whose event was canceled never happened and must leave the board")
	}
	// A REJECTED request is final: no work is owed on it, so it leaves the board like a canceled
	// move (maintainer 2026-09-25: "once it's rejected that action should also be gone"). It used
	// to sit In progress as unclaimed pool work, which every operator board showed.
	if _, leaked := got[ids["birth-rejected"]]; leaked {
		t.Fatal("a rejected request owes no work and must leave the board")
	}
	want := map[string]struct {
		state domain.WorkState
		lane  domain.Lane
		title string
	}{
		"shift-pending":  {domain.WorkStateDue, domain.LaneToDo, "Pen move · Godel 1 - Part 3"},
		"birth-pending":  {domain.WorkStateDue, domain.LaneToDo, "Birth · Godel 1"},
		"birth-approved": {domain.WorkStateCompleted, domain.LaneDone, "Birth · Godel 1"},
		// A death sits on its animal's own park board, at the animal's own pen.
		"death-pending": {domain.WorkStateDue, domain.LaneToDo, "Death · Godel 1 - Part 3"},
		// Approving a pen move authorizes it and moves nothing: the card follows the event.
		"shift-authorized": {domain.WorkStateInProgress, domain.LaneInProgress, "Pen move · Godel 1 - Part 3"},
		"shift-filmed":     {domain.WorkStateVerificationPending, domain.LaneInReview, "Pen move · Godel 1 - Part 3"},
		"shift-applied":    {domain.WorkStateCompleted, domain.LaneDone, "Pen move · Godel 1 - Part 3"},
	}
	for key, w := range want {
		r, ok := got[ids[key]]
		if !ok {
			t.Fatalf("%s missing", key)
		}
		if r.WorkState != w.state || r.Lane != w.lane || r.Title != w.title {
			t.Errorf("%s: state=%s lane=%s title=%q, want %s/%s/%q", key, r.WorkState, r.Lane, r.Title, w.state, w.lane, w.title)
		}
		if r.Owner.UserID != "" || r.OwnerState != domain.OwnerStatePool {
			t.Errorf("%s: approvals are a pool, got owner %+v %s", key, r.Owner, r.OwnerState)
		}
		if r.RowKey != "counts|"+ApprovalsSourceType+"|"+ids[key] || r.BusinessDate != apDate || r.ParkID != apPark || r.ParkName != "Coimbatore" {
			t.Errorf("%s: identity/scope %s %s %s %s", key, r.RowKey, r.BusinessDate, r.ParkID, r.ParkName)
		}
	}
	shift := got[ids["shift-pending"]]
	if shift.Subtitle != "Raised by Dinakar · 3 animals" || shift.ClockLabel != "Raised 09:15" || shift.Pen.PartitionLabel != "Part 3" || shift.Pen.ShedID != apShed {
		t.Errorf("pen move subtitle %q clock %q pen %+v", shift.Subtitle, shift.ClockLabel, shift.Pen)
	}
	early := got[ids["birth-pending"]]
	if early.ClockLabel != "Raised 00:10" || early.Subtitle != "Raised by Dinakar" {
		t.Errorf("birth raised at 00:10 IST belongs to the IST day: clock %q subtitle %q", early.ClockLabel, early.Subtitle)
	}
	if got[ids["birth-approved"]].Counts.Done != 1 {
		t.Errorf("counts approved %+v", got[ids["birth-approved"]].Counts)
	}
}

// TestApprovalsBoardScopeAndKeyset: the owner lens selects nothing from a pool, the state
// filter and keyset happen in SQL, and the counts agree with the rows they summarise.
func TestApprovalsBoardScopeAndKeyset(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedApprovals(t, ctx, pool)
	src := NewApprovals(pool, 5*time.Second)

	mine, err := src.ListRows(ctx, approvalQuery(apRaiser))
	if err != nil {
		t.Fatal(err)
	}
	// The raiser's own board lists what they raised (their work in flight); the approver
	// pool owns no row, so an approver's own lens still lists nothing of theirs.
	if len(mine) != 7 {
		t.Fatalf("raiser lens: everything they raised on this park-day that still owes work, got %d", len(mine))
	}
	if theirs, err := src.ListRows(ctx, approvalQuery(apApprover)); err != nil || len(theirs) != 0 {
		t.Fatalf("an approver owns no request: got %d err %v", len(theirs), err)
	}
	open, err := src.ListRows(ctx, approvalQuery("", domain.WorkStateDue))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("due: 3 expected (pen move, birth, death), got %d", len(open))
	}

	seen := map[string]bool{}
	after := ""
	for i := 0; i < 5; i++ {
		q := approvalQuery("")
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
	if len(seen) != 7 {
		t.Fatalf("keyset walk saw %d rows, want 7", len(seen))
	}

	counts, err := src.CountByState(ctx, approvalQuery(""))
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.WorkStateDue] != 3 || counts[domain.WorkStateCompleted] != 2 || counts[domain.WorkStateRejected] != 0 || counts[domain.WorkStateInProgress] != 1 || counts[domain.WorkStateVerificationPending] != 1 || len(counts) != 4 {
		t.Fatalf("counts %+v", counts)
	}
	otherPark, err := src.CountByState(ctx, ports.SourceQuery{TenantID: apTenant, ParkID: apOtherPk, BusinessDate: apDate})
	if err != nil {
		t.Fatal(err)
	}
	if otherPark[domain.WorkStateDue] != 1 || len(otherPark) != 1 {
		t.Fatalf("the other park sees only its own pen move, got %+v", otherPark)
	}
	if _, err := src.ListRows(ctx, ports.SourceQuery{TenantID: apTenant, ParkID: apPark, BusinessDate: "not-a-date"}); err == nil {
		t.Fatal("a malformed business date must be refused, not widened")
	}
}

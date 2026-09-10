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
	apDate     = "2026-09-10"
)

func seedShiftingEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, park string) {
	t.Helper()
	exec(t, ctx, pool, `
INSERT INTO shifting_events (shifting_event_id, tenant_id, logical_shifting_event_key, priority, category,
  destination_park_id, destination_shed_id, raised_at, effective_at, source_system, source_ref,
  payload_hash, idempotency_key, request_fingerprint)
VALUES ($1::uuid, $2::uuid, 'board-' || $1, 'normal', 'routine', $3::uuid, $4::uuid, now(), now(),
  'goatos_canonical', 'board-test', 'hash-' || $1, 'board-shift-' || $1, 'fp-' || $1)
ON CONFLICT (shifting_event_id) DO NOTHING`, eventID, apTenant, park, apShed)
}

type approvalSeed struct {
	key, requestType, status, payload, raisedAtIST string
	shiftingEvent, subjectGoat                     string
}

// seedApprovals lays down the three statuses across birth and pen-move requests, a death (no
// park in its payload, so never on a park board), a pen move at the other park, and two rows
// that sit on either side of the IST day boundary.
func seedApprovals(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	seedOrg(t, ctx, pool, apTenant, apPark, apOtherPk, apMember, apRaiser)
	exec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'GODEL1', 'Godel 1', $3::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, apShed, apTenant, apPark)
	seedShiftingEvent(t, ctx, pool, apEventA, apPark)
	seedShiftingEvent(t, ctx, pool, apEventB, apOtherPk)

	shift := `{"shifting_event_id":"` + apEventA + `","destination_park_id":"` + apPark + `","destination_shed_id":"` + apShed + `","destination_partition_label":"Part 3","goat_ids":["a","b","c"]}`
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
	if len(rows) != 4 {
		t.Fatalf("4 rows expected (death has no park, other park and yesterday are out), got %d", len(rows))
	}
	got := bySourceID(rows)
	if _, leaked := got[ids["death-pending"]]; leaked {
		t.Fatal("a death request has no park in its payload and must not be attributed to one")
	}
	want := map[string]struct {
		state domain.WorkState
		lane  domain.Lane
		title string
	}{
		"shift-pending":  {domain.WorkStateDue, domain.LaneToDo, "Pen move · Godel 1 - Part 3"},
		"birth-pending":  {domain.WorkStateDue, domain.LaneToDo, "Birth · Godel 1"},
		"birth-approved": {domain.WorkStateCompleted, domain.LaneDone, "Birth · Godel 1"},
		"birth-rejected": {domain.WorkStateRejected, domain.LaneInProgress, "Birth · Godel 1"},
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
	if got[ids["birth-approved"]].Counts.Done != 1 || got[ids["birth-rejected"]].Counts.NeedsAttention != 1 {
		t.Errorf("counts approved %+v rejected %+v", got[ids["birth-approved"]].Counts, got[ids["birth-rejected"]].Counts)
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
	if len(mine) != 0 {
		t.Fatalf("raising a request is not owning its decision: owner lens must be empty, got %d", len(mine))
	}
	open, err := src.ListRows(ctx, approvalQuery("", domain.WorkStateDue))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("due: 2 expected, got %d", len(open))
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
	if len(seen) != 4 {
		t.Fatalf("keyset walk saw %d rows, want 4", len(seen))
	}

	counts, err := src.CountByState(ctx, approvalQuery(""))
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.WorkStateDue] != 2 || counts[domain.WorkStateCompleted] != 1 || counts[domain.WorkStateRejected] != 1 || len(counts) != 3 {
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

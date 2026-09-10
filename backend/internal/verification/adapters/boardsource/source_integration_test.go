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
	bsOperator = "00000000-0000-4000-8000-000000000301"
	bsOtherOp  = "00000000-0000-4000-8000-000000000302"
	bsVerifier = "00000000-0000-4000-8000-000000000303"
	bsMember   = "00000000-0000-4000-8000-000000000401"
	bsDate     = "2026-09-10"

	itemPending   = "00000000-0000-4000-8000-000000009101" // feed transport, pen display title
	itemApproved  = "00000000-0000-4000-8000-000000009102" // weighing, subject label title
	itemRejected  = "00000000-0000-4000-8000-000000009103" // health adults, module-prefixed label
	itemSampled   = "00000000-0000-4000-8000-000000009104" // approved by the closeout, no verifier
	itemNoOwner   = "00000000-0000-4000-8000-000000009105" // shifting, no operator named
	itemLateNight = "00000000-0000-4000-8000-000000009106" // 23:30 IST on the day: still this day
	itemNextDay   = "00000000-0000-4000-8000-000000009107" // 00:10 IST next day: not this day
	itemOtherPark = "00000000-0000-4000-8000-000000009108"
)

func exec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

// seed lays down two parks, two sheds, one operator with a workforce profile, and eight
// verification items across every status, both edges of the IST business day, and the
// other park, so every branch of workStateSQL and the day bound are exercised on a
// database round trip.
func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Board Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, bsTenant)
	exec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active'),
       ($3::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active'),
       ($4::uuid, $2::uuid, 'shed', 'GODEL1', 'Godel 1', 'active'),
       ($5::uuid, $2::uuid, 'shed', 'CASTRO', 'Castro', 'active')
ON CONFLICT (location_id) DO NOTHING`, bsPark, bsTenant, bsOtherPk, bsShed, bsShedB)
	exec(t, ctx, pool, `UPDATE locations SET parent_location_id = $1::uuid WHERE location_id IN ($2::uuid, $3::uuid)`, bsPark, bsShed, bsShedB)
	exec(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIN', 'Dinakar', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, bsMember, bsTenant, bsOperator, bsPark)

	type item struct {
		id, vertical, module, category, park, shed, partition, subject, status, reason, operator, verifier, auto, captured string
	}
	items := []item{
		{itemPending, "feed", "feed", "feed_transport", bsPark, bsShed, "Part 3", "", "pending", "", bsOperator, "", "", "2026-09-10 09:15:00+05:30"},
		{itemApproved, "weighing", "weighing", "weighing_proof", bsPark, bsShedB, "2", "Castro 2 · 41.5 kg", "approved", "", bsOperator, bsVerifier, "", "2026-09-10 10:00:00+05:30"},
		{itemRejected, "health", "health", "health_adults", bsPark, bsShed, "", "", "rejected", "Wrong animal in frame", bsOperator, bsVerifier, "", "2026-09-10 11:00:00+05:30"},
		{itemSampled, "feed", "feed", "feed_packing", bsPark, bsShedB, "1", "", "approved", "", bsOtherOp, "", "not_sampled", "2026-09-10 12:00:00+05:30"},
		{itemNoOwner, "counts", "counts", "shifting_move", bsPark, bsShed, "", "", "pending", "", "", "", "", "2026-09-10 13:00:00+05:30"},
		{itemLateNight, "feed", "feed", "feed_transport", bsPark, bsShedB, "", "", "pending", "", bsOtherOp, "", "", "2026-09-10 23:30:00+05:30"},
		{itemNextDay, "feed", "feed", "feed_transport", bsPark, bsShedB, "", "", "pending", "", bsOtherOp, "", "", "2026-09-11 00:10:00+05:30"},
		{itemOtherPark, "feed", "feed", "feed_transport", bsOtherPk, "", "", "", "pending", "", bsOtherOp, "", "", "2026-09-10 09:00:00+05:30"},
	}
	for _, x := range items {
		exec(t, ctx, pool, `
INSERT INTO verification_items (
  item_id, tenant_id, vertical, module, category, source_module, source_ref_type, source_ref_id,
  media_refs, status, verdict_reason, operator_id, park_id, shed_id, partition_label, subject_label,
  captured_at, verified_by, verified_at, auto_resolution, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $4, 'board_test_ref', gen_random_uuid(),
  '["proof"]'::jsonb, $6, NULLIF($7, ''), NULLIF($8, '')::uuid, $9::uuid, NULLIF($10, '')::uuid, NULLIF($11, ''), NULLIF($12, ''),
  $13::timestamptz, NULLIF($14, '')::uuid, CASE WHEN $6 = 'pending' THEN NULL ELSE $13::timestamptz + interval '1 hour' END, NULLIF($15, ''),
  'board-test:' || $1
) ON CONFLICT (item_id) DO NOTHING`,
			x.id, bsTenant, x.vertical, x.module, x.category, x.status, x.reason, x.operator, x.park, x.shed, x.partition, x.subject,
			x.captured, x.verifier, x.auto)
	}
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

// TestVerificationBoardRowsOnADatabaseRoundTrip asserts the OUTPUT STRINGS and states of
// every branch on a real database: the status mapping, the category label from the
// catalog, subject label vs pen display in the title, the IST day bound on captured_at,
// the owner resolved through the workforce profile, and the counts.
func TestVerificationBoardRowsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	if src.Module() != domain.ModuleVerification || src.SourceType() != "verification_item" {
		t.Fatalf("identity %s/%s", src.Module(), src.SourceType())
	}
	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("6 items captured on the day in this park expected (00:10 next day and the other park are out), got %d", len(rows))
	}
	got := byID(rows)
	if _, leaked := got[itemNextDay]; leaked {
		t.Fatalf("an item captured 00:10 IST the next day leaked into %s", bsDate)
	}
	want := map[string]struct {
		state domain.WorkState
		lane  domain.Lane
	}{
		itemPending:   {domain.WorkStateVerificationPending, domain.LaneInReview},
		itemApproved:  {domain.WorkStateCompleted, domain.LaneDone},
		itemRejected:  {domain.WorkStateRejected, domain.LaneInProgress},
		itemSampled:   {domain.WorkStateCompleted, domain.LaneDone},
		itemNoOwner:   {domain.WorkStateVerificationPending, domain.LaneInReview},
		itemLateNight: {domain.WorkStateVerificationPending, domain.LaneInReview},
	}
	for id, w := range want {
		r, ok := got[id]
		if !ok {
			t.Fatalf("row %s missing", id)
		}
		if r.WorkState != w.state || r.Lane != w.lane || r.Severity != domain.SeverityOK {
			t.Errorf("%s: state=%s lane=%s sev=%s, want %s/%s/ok", id, r.WorkState, r.Lane, r.Severity, w.state, w.lane)
		}
		if r.Module != domain.ModuleVerification || r.SourceType != SourceType || r.RowKey != "verification|"+SourceType+"|"+id {
			t.Errorf("%s: identity %s/%s/%s", id, r.Module, r.SourceType, r.RowKey)
		}
		if r.BusinessDate != bsDate || r.ParkID != bsPark || r.ParkName != "Coimbatore" {
			t.Errorf("%s: scope %s %s %s", id, r.BusinessDate, r.ParkID, r.ParkName)
		}
		if r.ClockLabel != "Captured 10/09/2026" || r.Subtitle != "Proof review" || r.DueAt == nil {
			t.Errorf("%s: clock %q subtitle %q due %v", id, r.ClockLabel, r.Subtitle, r.DueAt)
		}
	}
	// No subject label: the title falls back to the pen composed through oploc.
	pending := got[itemPending]
	if pending.Pen.Display != "Godel 1 - Part 3" || pending.Title != "Feed Transport · Godel 1 - Part 3" {
		t.Errorf("pending pen %q title %q", pending.Pen.Display, pending.Title)
	}
	if pending.Owner.Name != "Dinakar" || pending.Owner.WorkforceMemberID != bsMember || pending.Owner.UserID != bsOperator || pending.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("pending owner %+v %s", pending.Owner, pending.OwnerState)
	}
	if pending.Counts != (domain.Counts{Pending: 1}) {
		t.Errorf("pending counts %+v", pending.Counts)
	}
	// A subject label wins over the pen in the title; a bare numeric partition renders with a space.
	approved := got[itemApproved]
	if approved.Title != "Weighing · Castro 2 · 41.5 kg" || approved.Pen.Display != "Castro 2" {
		t.Errorf("approved title %q pen %q", approved.Title, approved.Pen.Display)
	}
	if approved.Counts != (domain.Counts{Done: 1}) {
		t.Errorf("approved counts %+v", approved.Counts)
	}
	// A tab label that does not name its module is prefixed with it.
	rejected := got[itemRejected]
	if rejected.Title != "Health Adults · Godel 1" || rejected.Counts != (domain.Counts{NeedsAttention: 1}) {
		t.Errorf("rejected title %q counts %+v", rejected.Title, rejected.Counts)
	}
	// A closeout approval (auto_resolution, no verifier) is completed like a human approve, and an
	// operator without a profile is still the owner with a blank name.
	sampled := got[itemSampled]
	if sampled.Counts != (domain.Counts{Done: 1}) || sampled.Owner.UserID != bsOtherOp || sampled.Owner.Name != "" || sampled.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("sampled counts %+v owner %+v %s", sampled.Counts, sampled.Owner, sampled.OwnerState)
	}
	// An item with no operator has no owner: reported as missing, never filled in.
	none := got[itemNoOwner]
	if none.OwnerState != domain.OwnerStateMissing || none.Owner.UserID != "" || none.Title != "Counts Shifting · Godel 1" {
		t.Errorf("no-owner %+v %s title %q", none.Owner, none.OwnerState, none.Title)
	}
	// 23:30 IST is still the same business day; its UTC instant is 18:00 the same day, which is
	// the trap a UTC date would fall into.
	if got[itemLateNight].BusinessDate != bsDate {
		t.Errorf("late-night business date %q", got[itemLateNight].BusinessDate)
	}
}

// TestVerificationBoardScopeAndKeyset: owner scope, state filter and the keyset boundary all
// happen in SQL, and the counts agree with the rows they summarise.
func TestVerificationBoardScopeAndKeyset(t *testing.T) {
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
	if len(mine) != 3 {
		t.Fatalf("operator lens: 3 own rows expected, got %d", len(mine))
	}
	for _, r := range mine {
		if r.Owner.UserID != bsOperator {
			t.Fatalf("operator lens leaked %s", r.Owner.UserID)
		}
	}

	open, err := src.ListRows(ctx, query("", domain.WorkStateVerificationPending, domain.WorkStateRejected))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 4 {
		t.Fatalf("pending+rejected: 4 expected, got %d", len(open))
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
	if total != 6 || counts[domain.WorkStateVerificationPending] != 3 || counts[domain.WorkStateCompleted] != 2 || counts[domain.WorkStateRejected] != 1 {
		t.Fatalf("counts %+v", counts)
	}
	mineCounts, err := src.CountByState(ctx, query(bsOperator))
	if err != nil {
		t.Fatal(err)
	}
	if mineCounts[domain.WorkStateVerificationPending] != 1 || mineCounts[domain.WorkStateCompleted] != 1 || mineCounts[domain.WorkStateRejected] != 1 {
		t.Fatalf("operator counts %+v", mineCounts)
	}
	otherPark, err := src.CountByState(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherPark) != 1 || otherPark[domain.WorkStateVerificationPending] != 1 {
		t.Fatalf("the other park sees only its own item, got %+v", otherPark)
	}
	nextDay, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: "2026-09-11", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(nextDay) != 1 || nextDay[0].SourceID != itemNextDay {
		t.Fatalf("the next business day holds exactly the 00:10 item, got %d", len(nextDay))
	}
	if _, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: "not-a-date"}); err == nil {
		t.Fatal("an unparseable business date must be refused, not widened")
	}
}

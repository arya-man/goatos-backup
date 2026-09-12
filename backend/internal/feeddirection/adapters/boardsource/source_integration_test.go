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
	bsTenant  = "00000000-0000-4000-8000-000000000001"
	bsPark    = "00000000-0000-4000-8000-000000003001"
	bsOtherPk = "00000000-0000-4000-8000-000000003002"
	bsShedA   = "00000000-0000-4000-8000-000000003101" // Godel 1
	bsShedB   = "00000000-0000-4000-8000-000000003102" // Castro
	bsShedC   = "00000000-0000-4000-8000-000000003103" // Yashoda 2
	bsShedD   = "00000000-0000-4000-8000-000000003104" // Mandela 1
	bsShedE   = "00000000-0000-4000-8000-000000003105" // other park

	bsOperator = "00000000-0000-4000-8000-000000000301"
	bsOtherOp  = "00000000-0000-4000-8000-000000000302"
	bsMember   = "00000000-0000-4000-8000-000000000401"
	bsDate     = "2026-09-10" // work day D; packing/transport serve D+1
	bsServeNxt = "2026-09-11" // D+1
)

func exec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

// seed lays down one park with four sheds plus one shed in a second park, one operator with a
// workforce profile, and one row per activity so every card is exercised on a real round trip:
//
//	transport (business_date=D): A due, B verification_due, C completed, D rework
//	packing   (target_date=D+1): A completed, B pending_verification
//	direction (target_date=D):   A rework
//	wastage   (target_date=D):   A completed
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
ON CONFLICT (location_id) DO NOTHING`, bsPark, bsTenant, bsOtherPk, bsShedA, bsShedB, bsShedC, bsShedD, bsShedE)
	exec(t, ctx, pool, `UPDATE locations SET parent_location_id = $1::uuid WHERE location_id IN ($2::uuid,$3::uuid,$4::uuid,$5::uuid)`, bsPark, bsShedA, bsShedB, bsShedC, bsShedD)
	exec(t, ctx, pool, `UPDATE locations SET parent_location_id = $1::uuid WHERE location_id = $2::uuid`, bsOtherPk, bsShedE)
	exec(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIN', 'Dinakar', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, bsMember, bsTenant, bsOperator, bsPark)

	// Transport tasks on the work day D.
	sched := "2026-09-10 15:30:00+05:30"
	type tk struct{ id, shed, status, op string }
	for i, x := range []tk{
		{"00000000-0000-4000-8000-0000000091a1", bsShedA, "due", ""}, // pool
		{"00000000-0000-4000-8000-0000000091a2", bsShedB, "verification_due", bsOperator},
		{"00000000-0000-4000-8000-0000000091a3", bsShedC, "completed", bsOtherOp}, // someone else's
		{"00000000-0000-4000-8000-0000000091a4", bsShedD, "rework", bsOperator},
		{"00000000-0000-4000-8000-0000000091e1", bsShedE, "due", ""}, // other park
	} {
		park := bsPark
		if i == 4 {
			park = bsOtherPk
		}
		exec(t, ctx, pool, `
INSERT INTO feed_transport_tasks (task_id, tenant_id, park_id, shed_id, partition_label, business_date, scheduled_at, status, operator_id)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,'',$5::date,$6::timestamptz,$7,NULLIF($8,'')::uuid)
ON CONFLICT (task_id) DO NOTHING`, x.id, bsTenant, park, x.shed, bsDate, sched, x.status, x.op)
	}

	packing := func(id, shed, status, proof, owner string) {
		exec(t, ctx, pool, `
INSERT INTO feed_packing_completions (completion_id, tenant_id, park_id, shed_id, session_no, target_date, workflow, status, packing_proof_ref, completed_by, idempotency_key)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,1,$5::date,'normal',$6,NULLIF($7,''),NULLIF($8,'')::uuid,$1::text)
ON CONFLICT (completion_id) DO NOTHING`, id, bsTenant, bsPark, shed, bsServeNxt, status, proof, owner)
	}
	packing("00000000-0000-4000-8000-0000000092a1", bsShedA, "completed", "proof:pk-a", bsOperator)
	packing("00000000-0000-4000-8000-0000000092a2", bsShedB, "pending_verification", "proof:pk-b", bsOperator)

	// Direction: A rework (no proof needed), served on D.
	exec(t, ctx, pool, `
INSERT INTO feed_distribution_completions (completion_id, tenant_id, park_id, shed_id, session_no, target_date, workflow, status, completed_by, idempotency_key)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,1,$5::date,'normal','rework',$6::uuid,$1::text)
ON CONFLICT (completion_id) DO NOTHING`, "00000000-0000-4000-8000-0000000093a1", bsTenant, bsPark, bsShedA, bsDate, bsOperator)

	// Wastage: A completed (experiment-only workflow), measured on D.
	exec(t, ctx, pool, `
INSERT INTO feed_wastage_completions (completion_id, tenant_id, park_id, shed_id, target_date, workflow, status, wastage_proof_ref, completed_by, idempotency_key)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::date,'experiment','completed','proof:w-a',$6::uuid,$1::text)
ON CONFLICT (completion_id) DO NOTHING`, "00000000-0000-4000-8000-0000000094a1", bsTenant, bsPark, bsShedA, bsDate, bsOperator)
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

// TestFeedActivityCardsOnADatabaseRoundTrip asserts the four aggregate cards' output on a real
// database: one card per activity per park, each rolled up to the leftmost lane its sheds hold.
func TestFeedActivityCardsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	if src.Module() != domain.ModuleFeed || src.SourceType() != "feed_activity" {
		t.Fatalf("identity %s/%s", src.Module(), src.SourceType())
	}
	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("four activity cards expected, got %d", len(rows))
	}
	got := byID(rows)
	type want struct {
		title    string
		state    domain.WorkState
		lane     domain.Lane
		counts   domain.Counts
		subtitle string
	}
	cases := map[string]want{
		"packing":   {"Feed packing", domain.WorkStateVerificationPending, domain.LaneInReview, domain.Counts{Done: 1, Pending: 1}, "2 pens · 1 done"},
		"direction": {"Feed direction", domain.WorkStateRejected, domain.LaneInProgress, domain.Counts{Pending: 1, NeedsAttention: 1}, "1 pen · 0 done"},
		"transport": {"Feed transport", domain.WorkStateDue, domain.LaneToDo, domain.Counts{Done: 1, Pending: 3, NeedsAttention: 1}, "4 pens · 1 done"},
		"wastage":   {"Feed wastage", domain.WorkStateCompleted, domain.LaneDone, domain.Counts{Done: 1}, "1 pen · 1 done"},
	}
	for id, w := range cases {
		r, ok := got[id]
		if !ok {
			t.Fatalf("card %s missing", id)
		}
		if r.Title != w.title || r.WorkState != w.state || r.Lane != w.lane || r.Counts != w.counts || r.Subtitle != w.subtitle {
			t.Errorf("%s: title=%q state=%s lane=%s counts=%+v subtitle=%q", id, r.Title, r.WorkState, r.Lane, r.Counts, r.Subtitle)
		}
		if r.Module != domain.ModuleFeed || r.SourceType != SourceType || r.RowKey != "feed|feed_activity|"+id {
			t.Errorf("%s: identity %s/%s/%s", id, r.Module, r.SourceType, r.RowKey)
		}
		if r.ParkID != bsPark || r.ParkName != "Coimbatore" || r.BusinessDate != bsDate || r.Href != "/feed/analytics" {
			t.Errorf("%s: scope %s %s %s href %q", id, r.ParkID, r.ParkName, r.BusinessDate, r.Href)
		}
	}
	// A rejected card is amber (domain.Row.Finalize).
	if got["direction"].Severity != domain.SeverityWatch {
		t.Errorf("rejected direction card severity %s, want watch", got["direction"].Severity)
	}
}

// TestFeedActivityScopeKeysetOwnerLens: keyset order, state filter, owner lens and the park/day
// bounds all hold, and CountByState agrees with the rows. Adversarial dimensions: Pagination /
// PageBoundary (the keyset walk), ParkScope (the other park sees only its own card), StatusBuckets
// (CountByState covers every status the cards roll up to).
func TestFeedActivityScopeKeysetOwnerLens(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	// Keyset: two at a time, in activity rank order, never repeated.
	order := []string{}
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
			order = append(order, r.SourceID)
			after = r.SourceID
		}
	}
	if len(order) != 4 || order[0] != "packing" || order[1] != "direction" || order[2] != "transport" || order[3] != "wastage" {
		t.Fatalf("keyset order %v", order)
	}

	// State filter: only the Done lane's card (wastage).
	done, err := src.ListRows(ctx, query("", domain.StatesInLane(domain.LaneDone)...))
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0].SourceID != "wastage" {
		t.Fatalf("done-lane filter %v", done)
	}

	// Owner lens: the transport card drops the shed owned by someone else (C completed) and the
	// pool shed A stays; done falls to 0 and the shed count to 3.
	mine := byID(mustRows(t, ctx, src, query(bsOperator)))
	tr := mine["transport"]
	if tr.Counts.Done != 0 || tr.Subtitle != "3 pens · 0 done" {
		t.Errorf("owner-lens transport counts %+v subtitle %q", tr.Counts, tr.Subtitle)
	}

	// CountByState over the whole filter: one card per lane here.
	counts, err := src.CountByState(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.WorkStateDue] != 1 || counts[domain.WorkStateVerificationPending] != 1 || counts[domain.WorkStateRejected] != 1 || counts[domain.WorkStateCompleted] != 1 {
		t.Fatalf("counts %+v", counts)
	}

	// The other park sees only its own transport card; another day sees nothing.
	other, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].SourceID != "transport" {
		t.Fatalf("other park cards %v", other)
	}
	empty, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: "2026-09-20", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("a day with no feed work must be empty, got %d", len(empty))
	}
}

func mustRows(t *testing.T, ctx context.Context, src *Source, q ports.SourceQuery) []domain.Row {
	t.Helper()
	rows, err := src.ListRows(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestFeedActivityOneToManyPenSessionsFoldToOneShedLine is the OneToMany / MultipleDimensions
// adversarial case for the card's group_key=shed_id pre-aggregation: a pen with TWO packing
// sessions in DIFFERENT statuses (completed and rework) must fold to ONE shed line at the worst
// lane, never two. It also covers StatusBuckets / EveryStatus -- the shed's rollup is the worst of
// its sessions -- and keeps its own business date so the shared seed's assertions are untouched.
func TestFeedActivityOneToManyPenSessionsFoldToOneShedLine(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	// Shed A, two packing sessions for the SAME serve day (D2+1), different statuses.
	const d2 = "2026-09-15"
	const serve2 = "2026-09-16"
	packing2 := func(id, shed string, session int, status, proof string) {
		exec(t, ctx, pool, `
INSERT INTO feed_packing_completions (completion_id, tenant_id, park_id, shed_id, session_no, target_date, workflow, status, packing_proof_ref, completed_by, idempotency_key)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6::date,'normal',$7,NULLIF($8,''),$9::uuid,$1::text)
ON CONFLICT (completion_id) DO NOTHING`, id, bsTenant, bsPark, shed, session, serve2, status, proof, bsOperator)
	}
	packing2("00000000-0000-4000-8000-0000000095a1", bsShedA, 1, "completed", "proof:s1")
	packing2("00000000-0000-4000-8000-0000000095a2", bsShedA, 2, "rework", "")

	src := New(pool, 5000000000)
	rows, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: d2, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var packing *domain.Row
	for i := range rows {
		if rows[i].SourceID == "packing" {
			packing = &rows[i]
		}
	}
	if packing == nil {
		t.Fatal("packing card expected")
	}
	// ONE shed line, not two, and the worst status wins (rework -> rejected, needs attention).
	if packing.Counts != (domain.Counts{Pending: 1, NeedsAttention: 1}) || packing.WorkState != domain.WorkStateRejected {
		t.Fatalf("two sessions of one pen must fold to one rejected shed line: counts=%+v state=%s", packing.Counts, packing.WorkState)
	}
	// And the subtasks list shows that ONE pen once.
	page, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: d2, SourceID: "packing", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 || page.Subtasks[0].Name != "Godel 1" {
		t.Fatalf("one pen, one line: total=%d rows=%d", page.Total, len(page.Subtasks))
	}
}

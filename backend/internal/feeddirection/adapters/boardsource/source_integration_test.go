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

	issue := func(id, park, day, workflow string) {
		exec(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5, 'issued', now(), $1::text, $1::text,
  $1::text, 'test', 'feed.direction.sheet', '1')
ON CONFLICT (feed_direction_issue_id) DO NOTHING`, id, bsTenant, park, day, workflow)
	}
	issue("00000000-0000-4000-8000-0000000081a1", bsPark, bsServeNxt, "normal")
	issue("00000000-0000-4000-8000-0000000081a2", bsPark, bsDate, "normal")
	issue("00000000-0000-4000-8000-0000000081a3", bsPark, bsDate, "experiment")

	issueRow := func(issueID, shed, workflow, item string, session, rowSeq, itemSeq int) {
		exec(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Pen', NULL, 'Dry', 'Beetal',
  'Beetal', $5, 'Morning', 10, false, $6, $7, 1.0, 1.0, false, $8, $9, false)
ON CONFLICT DO NOTHING`, bsTenant, issueID, bsPark, shed, session, workflow, item, rowSeq, itemSeq)
	}
	// Packing for D+1: A/B have completion rows below; C is issued but not filmed yet.
	issueRow("00000000-0000-4000-8000-0000000081a1", bsShedA, "normal", "Concentrate", 1, 0, 0)
	issueRow("00000000-0000-4000-8000-0000000081a1", bsShedB, "normal", "Concentrate", 1, 1, 0)
	issueRow("00000000-0000-4000-8000-0000000081a1", bsShedC, "normal", "Concentrate", 1, 2, 0)
	// Direction on D: A has a rework completion; B is issued but not filmed yet.
	issueRow("00000000-0000-4000-8000-0000000081a2", bsShedA, "normal", "Concentrate", 1, 0, 0)
	issueRow("00000000-0000-4000-8000-0000000081a2", bsShedB, "normal", "Concentrate", 1, 1, 0)
	// Experiment/wastage on D: A has completion; B is issued but not measured yet.
	issueRow("00000000-0000-4000-8000-0000000081a3", bsShedA, "experiment", "Trial", 1, 0, 0)
	issueRow("00000000-0000-4000-8000-0000000081a3", bsShedB, "experiment", "Trial", 1, 1, 0)

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

func feedActivityID(activity string) string { return bsPark + ":" + activity }

func byID(rows []domain.Row) map[string]domain.Row {
	out := map[string]domain.Row{}
	for _, r := range rows {
		out[r.SourceID] = r
	}
	return out
}

// TestFeedActivityCardsOnADatabaseRoundTrip asserts the four aggregate cards' output on a real
// database: one card per activity per park, each rolled up from its pens by the 2026-09-14 rule.
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
		// Every card is a MIX of started and unstarted pens, so every card is In progress; a card
		// with a pen sent back reads Rejected in that same lane.
		feedActivityID("packing"):   {"Feed packing", domain.WorkStateInProgress, domain.LaneInProgress, domain.Counts{Done: 1, Pending: 2}, "3 pens · 1 done"},
		feedActivityID("direction"): {"Feed direction", domain.WorkStateRejected, domain.LaneInProgress, domain.Counts{Pending: 2, NeedsAttention: 1}, "2 pens · 0 done"},
		feedActivityID("transport"): {"Feed transport", domain.WorkStateRejected, domain.LaneInProgress, domain.Counts{Done: 1, Pending: 3, NeedsAttention: 1}, "4 pens · 1 done"},
		feedActivityID("wastage"):   {"Feed wastage", domain.WorkStateInProgress, domain.LaneInProgress, domain.Counts{Done: 1, Pending: 1}, "2 pens · 1 done"},
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
	// The direction card has one pen sent back and one still unfilled: work has begun and is
	// not all handed in, so it is In progress, and the rework makes it Rejected (amber) so the
	// board points at the pen that needs the operator again.
	if got[feedActivityID("direction")].Severity != domain.SeverityWatch {
		t.Errorf("direction card (rejected pen) severity %s, want watch", got[feedActivityID("direction")].Severity)
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
	wantOrder := []string{feedActivityID("packing"), feedActivityID("direction"), feedActivityID("transport"), feedActivityID("wastage")}
	if len(order) != 4 || order[0] != wantOrder[0] || order[1] != wantOrder[1] || order[2] != wantOrder[2] || order[3] != wantOrder[3] {
		t.Fatalf("keyset order %v", order)
	}

	// State filter: with unstarted issued feed work visible, no feed activity is fully Done.
	done, err := src.ListRows(ctx, query("", domain.StatesInLane(domain.LaneDone)...))
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 0 {
		t.Fatalf("done-lane filter %v", done)
	}

	// Owner lens: the transport card drops the shed owned by someone else (C completed) and the
	// pool shed A stays; done falls to 0 and the shed count to 3.
	mine := byID(mustRows(t, ctx, src, query(bsOperator)))
	tr := mine[feedActivityID("transport")]
	if tr.Counts.Done != 0 || tr.Subtitle != "3 pens · 0 done" {
		t.Errorf("owner-lens transport counts %+v subtitle %q", tr.Counts, tr.Subtitle)
	}

	// CountByState over the whole filter agrees with the rows: two mixed cards In progress, two
	// with a rejected pen.
	counts, err := src.CountByState(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.WorkStateInProgress] != 2 || counts[domain.WorkStateRejected] != 2 {
		t.Fatalf("counts %+v", counts)
	}

	// The other park sees only its own transport card; another day sees nothing.
	other, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].SourceID != bsOtherPk+":transport" {
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
	exec(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version)
VALUES ('00000000-0000-4000-8000-0000000082a1'::uuid, $1::uuid, $2::uuid, $3::date, 'normal',
  'issued', now(), 'fp-d2', 'rfp-d2', 'idem-d2', 'test', 'feed.direction.sheet', '1')
ON CONFLICT (feed_direction_issue_id) DO NOTHING`, bsTenant, bsPark, serve2)
	for session := 1; session <= 2; session++ {
		exec(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
VALUES ($1::uuid, '00000000-0000-4000-8000-0000000082a1'::uuid, $2::uuid, 'CBE',
  $3::uuid, 'Godel 1', NULL, 'Dry', 'Beetal', 'Beetal', $4, 'Morning', 10, false,
  'normal', 'Concentrate', 1.0, 1.0, false, $5, 0, false)
ON CONFLICT DO NOTHING`, bsTenant, bsPark, bsShedA, session, session-1)
	}
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
		if rows[i].SourceID == feedActivityID("packing") {
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
	page, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: d2, SourceID: feedActivityID("packing"), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 || page.Subtasks[0].Name != "Godel 1" {
		t.Fatalf("one pen, one line: total=%d rows=%d", page.Total, len(page.Subtasks))
	}
}

// TestFeedActivityPensComposePartitions is the maintainer's 2026-09-14 fix: a partitioned shed's
// pens must each show as their own line with the partition composed (Godel 1 - Part 3), never a
// single bare "Godel 1". Two pens of one shed, one completed and one in review, must be two lines.
func TestFeedActivityPensComposePartitions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	const d3 = "2026-09-17"
	const serve3 = "2026-09-18"
	const issueID = "00000000-0000-4000-8000-0000000083c1"
	exec(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, 'normal', 'issued', now(), $1::text, $1::text,
  $1::text, 'test', 'feed.direction.sheet', '1')
ON CONFLICT (feed_direction_issue_id) DO NOTHING`, issueID, bsTenant, bsPark, serve3)
	penRow := func(rowSeq int, partitionLabel string) {
		exec(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Godel 1', $5, 'Dry', 'Beetal',
  'Beetal', 1, 'Morning', 10, false, 'normal', 'Concentrate', 1.0, 1.0, false, $6, 0, false)
ON CONFLICT DO NOTHING`, bsTenant, issueID, bsPark, bsShedA, partitionLabel, rowSeq)
	}
	penRow(0, "Part 3")
	penRow(1, "Part 4")
	completion := func(id, partitionLabel, status string) {
		exec(t, ctx, pool, `
INSERT INTO feed_packing_completions (completion_id, tenant_id, park_id, shed_id, partition_label, session_no, target_date, workflow, status, packing_proof_ref, completed_by, idempotency_key)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,1,$6::date,'normal',$7,'proof:'||$1,$8::uuid,$1::text)
ON CONFLICT (completion_id) DO NOTHING`, id, bsTenant, bsPark, bsShedA, partitionLabel, serve3, status, bsOperator)
	}
	completion("00000000-0000-4000-8000-0000000096c1", "Part 3", "completed")
	completion("00000000-0000-4000-8000-0000000096c2", "Part 4", "pending_verification")

	src := New(pool, 5000000000)
	rows, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: d3, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var packing *domain.Row
	for i := range rows {
		if rows[i].SourceID == feedActivityID("packing") {
			packing = &rows[i]
		}
	}
	if packing == nil {
		t.Fatal("packing card expected")
	}
	// TWO pens of one shed: Part 4 in review is the leftmost lane, Part 3 done.
	if packing.Counts != (domain.Counts{Done: 1, Pending: 1}) || packing.Subtitle != "2 pens · 1 done" {
		t.Fatalf("two pens of one shed: counts=%+v subtitle=%q", packing.Counts, packing.Subtitle)
	}
	page, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: d3, SourceID: feedActivityID("packing"), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Subtasks) != 2 {
		t.Fatalf("two pens, two lines: total=%d rows=%d", page.Total, len(page.Subtasks))
	}
	names := map[string]bool{page.Subtasks[0].Name: true, page.Subtasks[1].Name: true}
	if !names["Godel 1 - Part 3"] || !names["Godel 1 - Part 4"] {
		t.Fatalf("pen names must compose shed + partition, got %q and %q", page.Subtasks[0].Name, page.Subtasks[1].Name)
	}

	// Adversarial dimensions this fixture also exercises, named for the aggregate-projection guard:
	//
	// OneToMany / MultipleDimensions: two pens under ONE shed (Godel 1) fold to two DISTINCT lines,
	//   never one shed line -- the shed->pen relation is one-to-many and must not collapse.
	// StatusBuckets / EveryStatus: the two pens span two status buckets (completed + in review), so
	//   the card's done/pending split is proven across buckets, not a single status.
	// PageBoundary / Pagination: paging AFTER the first pen returns exactly the remaining pen.
	// ParkScope: the other park sees none of these pens.
	if page.Subtasks[0].WorkState != domain.WorkStateVerificationPending || page.Subtasks[1].WorkState != domain.WorkStateCompleted {
		t.Fatalf("StatusBuckets: worst-first should be in review then done, got %s then %s", page.Subtasks[0].WorkState, page.Subtasks[1].WorkState)
	}
	afterFirst, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: d3, SourceID: feedActivityID("packing"), AfterKey: page.Subtasks[0].Key, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if afterFirst.Total != 2 || len(afterFirst.Subtasks) != 1 || afterFirst.Subtasks[0].Name != page.Subtasks[1].Name {
		t.Fatalf("PageBoundary: after the first pen expected exactly the second, got total=%d rows=%d", afterFirst.Total, len(afterFirst.Subtasks))
	}
	otherPark, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: d3, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range otherPark {
		if r.SourceID == bsOtherPk+":packing" {
			t.Fatalf("ParkScope: the other park has no issued packing for this day, so it must show no packing card")
		}
	}
}

// TestFeedActivityPenIdentityUsesPartitionKey is the guard for PR 255's follow-up review finding:
// pen identity must be the normalized partition_key, not the raw display label. A sheet can carry
// two feed items for the same pen/session where one row says "Part 3" and another says "part 3";
// those are one operational pen and must roll up to one card count and one drawer line.
func TestFeedActivityPenIdentityUsesPartitionKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	const d4 = "2026-09-19"
	const serve4 = "2026-09-20"
	const issueID = "00000000-0000-4000-8000-0000000084c1"
	exec(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, 'normal', 'issued', now(), $1::text, $1::text,
  $1::text, 'test', 'feed.direction.sheet', '1')
ON CONFLICT (feed_direction_issue_id) DO NOTHING`, issueID, bsTenant, bsPark, serve4)
	penItem := func(rowSeq int, partitionLabel, item string) {
		exec(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Godel 1', $5, 'Dry', 'Beetal',
  'Beetal', 1, 'Morning', 10, false, 'normal', $6, 1.0, 2.0, false, $7, 0, false)
ON CONFLICT DO NOTHING`, bsTenant, issueID, bsPark, bsShedA, partitionLabel, item, rowSeq)
	}
	penItem(0, "Part 3", "Concentrate")
	penItem(1, "part 3", "Mineral Mix")
	exec(t, ctx, pool, `
INSERT INTO feed_packing_completions (completion_id, tenant_id, park_id, shed_id, partition_label, session_no, target_date, workflow, status, packing_proof_ref, completed_by, idempotency_key)
VALUES ('00000000-0000-4000-8000-0000000097c1'::uuid,$1::uuid,$2::uuid,$3::uuid,'Part 3',1,$4::date,'normal','pending_verification','proof:partition-key',$5::uuid,'partition-key')
ON CONFLICT (completion_id) DO NOTHING`, bsTenant, bsPark, bsShedA, serve4, bsOperator)

	src := New(pool, 5000000000)
	rows, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: d4, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var packing *domain.Row
	for i := range rows {
		if rows[i].SourceID == feedActivityID("packing") {
			packing = &rows[i]
		}
	}
	if packing == nil {
		t.Fatal("packing card expected")
	}
	if packing.Counts != (domain.Counts{Pending: 1}) || packing.Subtitle != "1 pen · 0 done" {
		t.Fatalf("cosmetic label variants must be one pen: counts=%+v subtitle=%q", packing.Counts, packing.Subtitle)
	}
	page, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: d4, SourceID: feedActivityID("packing"), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("cosmetic label variants must be one line: total=%d rows=%d", page.Total, len(page.Subtasks))
	}
	if page.Subtasks[0].Name != "Godel 1 - Part 3" {
		t.Fatalf("display label should remain composed and human-readable, got %q", page.Subtasks[0].Name)
	}
}

// TestFeedActivityCardRollsUpByAllOrAny pins the maintainer's 2026-09-14 roll-up: a card is To do
// only while NO pen has started, In progress once any pen has started and not all are handed in
// (a mix of unstarted and in-review pens is In progress, not To do), In review only when EVERY pen
// is handed in and one is still with the verifier, and Done only when EVERY pen is completed.
// The old leftmost-lane rule parked a 62/63-done card in To do; this walks one packing card
// through all four lanes with two pens of one shed on a fresh work-day.
func TestFeedActivityCardRollsUpByAllOrAny(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	const d3 = "2026-09-17"
	const serve3 = "2026-09-18"
	exec(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version)
VALUES ('00000000-0000-4000-8000-0000000082a3'::uuid, $1::uuid, $2::uuid, $3::date, 'normal',
  'issued', now(), 'fp-d3', 'rfp-d3', 'idem-d3', 'test', 'feed.direction.sheet', '1')
ON CONFLICT (feed_direction_issue_id) DO NOTHING`, bsTenant, bsPark, serve3)
	for i, part := range []string{"Part 1", "Part 2"} {
		exec(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
VALUES ($1::uuid, '00000000-0000-4000-8000-0000000082a3'::uuid, $2::uuid, 'CBE',
  $3::uuid, 'Godel 1', $4, 'Dry', 'Beetal', 'Beetal', 1, 'Morning', 10, false,
  'normal', 'Concentrate', 1.0, 1.0, false, $5, 0, false)
ON CONFLICT DO NOTHING`, bsTenant, bsPark, bsShedA, part, i)
	}
	setPen := func(id, part, status string) {
		exec(t, ctx, pool, `
INSERT INTO feed_packing_completions (completion_id, tenant_id, park_id, shed_id, partition_label, session_no, target_date, workflow, status, packing_proof_ref, completed_by, idempotency_key)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,1,$6::date,'normal',$7,'proof:'||$1::text,$8::uuid,$1::text)
ON CONFLICT (completion_id) DO UPDATE SET status = EXCLUDED.status`, id, bsTenant, bsPark, bsShedA, part, serve3, status, bsOperator)
	}
	card := func() domain.Row {
		t.Helper()
		rows := mustRows(t, ctx, src(pool), ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: d3, Limit: 50})
		for _, r := range rows {
			if r.SourceID == feedActivityID("packing") {
				return r
			}
		}
		t.Fatal("packing card expected")
		return domain.Row{}
	}
	const p1, p2 = "00000000-0000-4000-8000-0000000095b1", "00000000-0000-4000-8000-0000000095b2"

	// Nothing started: To do.
	if c := card(); c.WorkState != domain.WorkStateDue || c.Lane != domain.LaneToDo {
		t.Fatalf("no pen started: state=%s lane=%s, want due/todo", c.WorkState, c.Lane)
	}
	// One pen handed in, the other untouched: a MIX is In progress, never To do.
	setPen(p1, "Part 1", "pending_verification")
	if c := card(); c.WorkState != domain.WorkStateInProgress || c.Lane != domain.LaneInProgress {
		t.Fatalf("one in review, one unstarted: state=%s lane=%s, want in_progress", c.WorkState, c.Lane)
	}
	// Both handed in, one still with the verifier: In review.
	setPen(p2, "Part 2", "completed")
	if c := card(); c.WorkState != domain.WorkStateVerificationPending || c.Lane != domain.LaneInReview {
		t.Fatalf("all handed in, one in review: state=%s lane=%s, want verification_pending", c.WorkState, c.Lane)
	}
	// Both completed: Done.
	setPen(p1, "Part 1", "completed")
	if c := card(); c.WorkState != domain.WorkStateCompleted || c.Lane != domain.LaneDone || c.Counts.Done != 2 {
		t.Fatalf("all completed: state=%s lane=%s done=%d, want completed/2", c.WorkState, c.Lane, c.Counts.Done)
	}
	// A pen sent back pulls the card back to In progress and reads Rejected.
	setPen(p2, "Part 2", "rework")
	if c := card(); c.WorkState != domain.WorkStateRejected || c.Lane != domain.LaneInProgress {
		t.Fatalf("one pen sent back: state=%s lane=%s, want rejected/in_progress", c.WorkState, c.Lane)
	}
}

func src(pool *pgxpool.Pool) *Source { return New(pool, 5*time.Second) }

// Reuse the same long-lived source across writes, as the API does. Summary and
// lane reads must immediately reflect another request completing or reopening work.
func TestFeedActivityFreshLaneAndCountsAfterMutation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)
	if _, err := src.CountByState(ctx, query("")); err != nil {
		t.Fatal(err)
	}
	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if before := byID(rows)[feedActivityID("transport")]; before.WorkState != domain.WorkStateRejected || before.Counts.Done != 1 {
		t.Fatalf("fixture must start with partially completed transport: %+v", before)
	}
	for _, step := range []struct {
		name, status string
		completed    int
		state        domain.WorkState
	}{
		{"complete", "completed", 1, domain.WorkStateCompleted},
		{"reopen", "rework", 0, domain.WorkStateRejected},
	} {
		t.Run(step.name, func(t *testing.T) {
			exec(t, ctx, pool, `UPDATE feed_transport_tasks SET status = $4 WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND business_date = $3::date`, bsTenant, bsPark, bsDate, step.status)
			counts, err := src.CountByState(ctx, query(""))
			if err != nil {
				t.Fatal(err)
			}
			if counts[domain.WorkStateCompleted] != step.completed {
				t.Fatalf("stale feed summary after %s: %+v", step.name, counts)
			}
			rows, err := src.ListRows(ctx, query("", step.state))
			if err != nil {
				t.Fatal(err)
			}
			current, ok := byID(rows)[feedActivityID("transport")]
			if !ok || current.WorkState != step.state {
				t.Fatalf("fresh lane missing transport after %s: %+v", step.name, rows)
			}
			if step.completed == 1 && (current.Counts.Done != 4 || current.Counts.Pending != 0) {
				t.Fatalf("completed card has stale pen counts: %+v", current)
			}
			if step.completed == 0 && (current.Counts.Done != 0 || current.Counts.NeedsAttention != 4) {
				t.Fatalf("reopened card has stale pen counts: %+v", current)
			}
		})
	}
}

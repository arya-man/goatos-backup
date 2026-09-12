package boardsource

import (
	"context"
	"fmt"
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
	bsDate     = "2026-09-10"
)

func exec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

// seed lays down a park, a shed with one partition, an operator with a workforce profile,
// one campaign and five buckets in five kernel states, so every branch of workStateSQL
// is exercised on a database round trip.
func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
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
	for _, op := range []string{bsOperator, bsOtherOp} {
		exec(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now()) ON CONFLICT DO NOTHING`, bsTenant, op, bsPark)
	}

	type bucket struct {
		id, kernel, bucketStatus, category, partition string
		planned                                       string
		operator                                      string
	}
	buckets := []bucket{
		{"00000000-0000-4000-8000-000000009101", "scheduled", "pending", "individual_animal", "Part 3", bsDate, bsOperator},
		{"00000000-0000-4000-8000-000000009102", "scheduled", "in_progress", "per_shed_partition", "Part 4", bsDate, bsOperator},
		{"00000000-0000-4000-8000-000000009103", "completed", "completed", "per_shed_partition", "", bsDate, bsOperator},
		// One OPEN bucket per (park, date, shed, partition): every open bucket below carries its
		// own partition; only the closed whole-pen one (9103) stays unpartitioned.
		{"00000000-0000-4000-8000-000000009104", "closed", "completed", "per_shed_partition", "Part 7", bsDate, bsOtherOp},
		{"00000000-0000-4000-8000-000000009105", "delayed", "pending", "per_shed_partition", "Part 5", "2026-09-06", bsOtherOp}, // 4 days: past the band
		{"00000000-0000-4000-8000-000000009106", "delayed", "pending", "per_shed_partition", "Part 6", "2026-09-09", bsOtherOp}, // 1 day: inside the band
		{"00000000-0000-4000-8000-000000009107", "canceled", "canceled", "per_shed_partition", "Part 8", bsDate, bsOtherOp},
		// Closed AFTER submit: CLOSE writes only the bucket, so the item still says 'completed'.
		// The board must read the bucket and call it Done, never leave it "In review" forever.
		{"00000000-0000-4000-8000-000000009108", "completed", "closed", "per_shed_partition", "Part 9", bsDate, bsOtherOp},
		// SUBMIT of a whole pen writes only the bucket (item still 'scheduled'): In review.
		{"00000000-0000-4000-8000-000000009109", "scheduled", "completed", "per_shed_partition", "Part 10", bsDate, bsOtherOp},
		// CANCEL writes only the bucket: not work, off the board and out of the counts.
		{"00000000-0000-4000-8000-000000009110", "scheduled", "canceled", "per_shed_partition", "Part 11", bsDate, bsOtherOp},
		// A started bucket with one scan bounced by the verifier: rejected, amber, attention 1.
		{"00000000-0000-4000-8000-000000009111", "scheduled", "in_progress", "individual_animal", "Part 12", bsDate, bsOperator},
	}
	ids := map[string]string{}
	for i, b := range buckets {
		// One campaign per bucket: a campaign may hold a (shed, partition) only once, and
		// several of these buckets are the same whole shed in different states.
		campaign := fmt.Sprintf("00000000-0000-4000-8000-0000000090%02d", i+1)
		exec(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $4::date, $4::date, 'published', 100, $5::uuid, $5::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, campaign, bsTenant, bsPark, bsDate, b.operator)
		exec(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, status, partition_label)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Godel 1', $5, $6::uuid, 0, $7, NULLIF($8, ''))
ON CONFLICT (campaign_shed_id) DO NOTHING`, b.id, campaign, bsTenant, bsShed, b.category, b.operator, b.bucketStatus, b.partition)
		var workItemID string
		if err := pool.QueryRow(ctx, `
INSERT INTO weighing_work_items (tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id, weighing_category, shed_label, shed_location_id, planned_business_date, due_business_date, work_state)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, 'Godel 1', $7::uuid, $8::date, $9::date, $10)
ON CONFLICT (tenant_id, campaign_shed_id) DO UPDATE SET work_state = EXCLUDED.work_state
RETURNING work_item_id::text`, bsTenant, campaign, b.id, bsPark, b.operator, b.category, bsShed, b.planned, bsDate, b.kernel).Scan(&workItemID); err != nil {
			t.Fatalf("seed work item %d: %v", i, err)
		}
		ids[b.id] = workItemID
	}
	// Two scans on the individual bucket, so its counts read 2 done / 1 pending of 3. An
	// observation needs a proof artifact; one bucket-scoped video serves both.
	const proofID = "00000000-0000-4000-8000-000000009301"
	exec(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, $2::uuid, 'local', 'board-test/' || $1, 'video/mp4', 'completed', 'shed', $3::uuid, 'shed', $3::uuid, 'video', $4::uuid, now())
ON CONFLICT (proof_id) DO NOTHING`, proofID, bsTenant, bsShed, bsOperator)
	for _, tag := range []string{"tag-a", "tag-b"} {
		exec(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 12.5, $5::uuid, $6::uuid, 'board-' || $4)
ON CONFLICT DO NOTHING`, bsTenant, "00000000-0000-4000-8000-000000009001", "00000000-0000-4000-8000-000000009101", tag, proofID, bsOperator)
	}
	// The started bucket (9111, campaign 9011) holds two submitted scans, one of which the
	// verifier bounced: the module keeps that row as ONE mutable 'rework' observation.
	for _, scan := range []struct{ tag, status string }{{"tag-r1", "verified"}, {"tag-r2", "rework"}} {
		exec(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, submitted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 14.0, $5::uuid, $6::uuid, 'board-' || $4, now(), $7)
ON CONFLICT DO NOTHING`, bsTenant, "00000000-0000-4000-8000-000000009011", "00000000-0000-4000-8000-000000009111", scan.tag, proofID, bsOperator, scan.status)
	}
	return ids
}

func query(owner string, states ...domain.WorkState) ports.SourceQuery {
	return ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, OwnerUserID: owner, WorkStates: states, Limit: 50}
}

func byBucket(rows []domain.Row) map[string]domain.Row {
	out := map[string]domain.Row{}
	for _, r := range rows {
		out[r.SourceID] = r
	}
	return out
}

// TestWeighingBoardRowsOnADatabaseRoundTrip asserts the OUTPUT STRINGS and states of every
// branch, on a real database: the state mapping, the D+2 carry band, the pen display with
// its partition, the owner resolved through the workforce profile, and the counts.
func TestWeighingBoardEveryStatusAndOneToManyScansOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 9 {
		t.Fatalf("9 live buckets expected (the two canceled ones, on the item and on the bucket, are not work), got %d", len(rows))
	}
	got := byBucket(rows)
	want := map[string]struct {
		state domain.WorkState
		lane  domain.Lane
		sev   domain.Severity
	}{
		ids["00000000-0000-4000-8000-000000009101"]: {domain.WorkStateDue, domain.LaneToDo, domain.SeverityOK},
		ids["00000000-0000-4000-8000-000000009102"]: {domain.WorkStateInProgress, domain.LaneInProgress, domain.SeverityOK},
		ids["00000000-0000-4000-8000-000000009103"]: {domain.WorkStateVerificationPending, domain.LaneInReview, domain.SeverityOK},
		ids["00000000-0000-4000-8000-000000009104"]: {domain.WorkStateCompleted, domain.LaneDone, domain.SeverityOK},
		ids["00000000-0000-4000-8000-000000009105"]: {domain.WorkStateOverdue, domain.LaneToDo, domain.SeverityAtRisk},
		ids["00000000-0000-4000-8000-000000009106"]: {domain.WorkStateDue, domain.LaneToDo, domain.SeverityWatch},
		ids["00000000-0000-4000-8000-000000009108"]: {domain.WorkStateCompleted, domain.LaneDone, domain.SeverityOK},
		ids["00000000-0000-4000-8000-000000009109"]: {domain.WorkStateVerificationPending, domain.LaneInReview, domain.SeverityOK},
		ids["00000000-0000-4000-8000-000000009111"]: {domain.WorkStateRejected, domain.LaneInProgress, domain.SeverityWatch},
	}
	if _, leaked := got[ids["00000000-0000-4000-8000-000000009110"]]; leaked {
		t.Fatal("a bucket canceled on the bucket alone must leave the board")
	}
	for id, w := range want {
		r, ok := got[id]
		if !ok {
			t.Fatalf("row %s missing", id)
		}
		if r.WorkState != w.state || r.Lane != w.lane || r.Severity != w.sev {
			t.Errorf("%s: state=%s lane=%s sev=%s, want %s/%s/%s", id, r.WorkState, r.Lane, r.Severity, w.state, w.lane, w.sev)
		}
		if r.Module != domain.ModuleWeighing || r.SourceType != SourceType || r.RowKey != "weighing|"+SourceType+"|"+id {
			t.Errorf("%s: identity %s/%s/%s", id, r.Module, r.SourceType, r.RowKey)
		}
		if r.BusinessDate != bsDate || r.ParkID != bsPark || r.ParkName != "Coimbatore" {
			t.Errorf("%s: scope %s %s %s", id, r.BusinessDate, r.ParkID, r.ParkName)
		}
	}
	// Pen display composes shed + partition through oploc: "Godel 1 - Part 3", never "Godel 1 3".
	first := got[ids["00000000-0000-4000-8000-000000009101"]]
	if first.Pen.Display != "Godel 1 - Part 3" || first.Title != "Weigh Godel 1 - Part 3" {
		t.Errorf("pen display %q title %q", first.Pen.Display, first.Title)
	}
	if first.Href != "/weighing/weights?park="+bsPark+"&weighing=individual_animal" {
		t.Errorf("href %q", first.Href)
	}
	// Free-flow: no expected count exists, so an individual bucket reads scanned as done and
	// no roster remainder; only a bucket with nothing scanned yet carries itself as pending.
	if first.Counts.Done != 2 || first.Counts.Pending != 0 || first.Counts.NeedsAttention != 0 {
		t.Errorf("individual bucket counts %+v, want 2 done / 0 pending", first.Counts)
	}
	bounced := got[ids["00000000-0000-4000-8000-000000009111"]]
	if bounced.Counts.Done != 2 || bounced.Counts.NeedsAttention != 1 {
		t.Errorf("a bounced scan is the card's attention: %+v", bounced.Counts)
	}
	if first.Owner.Name != "Dinakar" || first.Owner.WorkforceMemberID != bsMember || first.Owner.UserID != bsOperator || first.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("owner %+v %s", first.Owner, first.OwnerState)
	}
	// A whole-pen bucket with no partition renders the bare shed name.
	whole := got[ids["00000000-0000-4000-8000-000000009103"]]
	if whole.Pen.Display != "Godel 1" || whole.Subtitle != "Whole pen" {
		t.Errorf("whole pen display %q subtitle %q", whole.Pen.Display, whole.Subtitle)
	}
	// The delayed row names its original plan in farm date order.
	late := got[ids["00000000-0000-4000-8000-000000009105"]]
	if late.ClockLabel != "Delayed · planned 06/09/2026" || late.Counts.NeedsAttention != 1 {
		t.Errorf("delayed clock %q attention %d", late.ClockLabel, late.Counts.NeedsAttention)
	}
	// An operator with no workforce profile is still an owner (the row names them); only the
	// name is blank.
	other := got[ids["00000000-0000-4000-8000-000000009104"]]
	if other.Owner.UserID != bsOtherOp || other.Owner.Name != "" || other.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("unprofiled owner %+v %s", other.Owner, other.OwnerState)
	}
}

// TestWeighingBoardScopeAndKeyset: owner scope, state filter and the keyset boundary all
// happen in SQL, and the counts agree with the rows they summarise.
func TestWeighingBoardParkScopePageBoundaryAndDateShift(t *testing.T) {
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
		t.Fatalf("operator lens: 4 own rows expected (three seeded plus the bounced bucket), got %d", len(mine))
	}
	for _, r := range mine {
		if r.Owner.UserID != bsOperator {
			t.Fatalf("operator lens leaked %s", r.Owner.UserID)
		}
	}

	open, err := src.ListRows(ctx, query("", domain.WorkStateDue, domain.WorkStateOverdue))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("due+overdue: 3 expected, got %d", len(open))
	}

	// Keyset: page through in twos; no duplicate, no gap, ordered by source id.
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
	if len(seen) != 9 {
		t.Fatalf("keyset walk saw %d rows, want 9", len(seen))
	}

	counts, err := src.CountByState(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 9 || counts[domain.WorkStateDue] != 2 || counts[domain.WorkStateOverdue] != 1 || counts[domain.WorkStateCompleted] != 2 || counts[domain.WorkStateVerificationPending] != 2 || counts[domain.WorkStateRejected] != 1 {
		t.Fatalf("counts %+v", counts)
	}
	otherPark, err := src.CountByState(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherPark) != 0 {
		t.Fatalf("the other park must see nothing, got %+v", otherPark)
	}
}

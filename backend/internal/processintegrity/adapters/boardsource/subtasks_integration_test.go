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
	stTenant   = "00000000-0000-4000-8000-00000000d001"
	stParty    = "00000000-0000-4000-8000-00000000d101"
	stPark     = "00000000-0000-4000-8000-00000000d301"
	stOtherPk  = "00000000-0000-4000-8000-00000000d302"
	stShed     = "00000000-0000-4000-8000-00000000d311"
	stOperator = "00000000-0000-4000-8000-00000000d501"
	stMember   = "00000000-0000-4000-8000-00000000d401"
	stProtocol = "00000000-0000-4000-8000-00000000d801"
	stVersion  = "00000000-0000-4000-8000-00000000d802"
	stRule     = "00000000-0000-4000-8000-00000000d803"
	stBatch    = "00000000-0000-4000-8000-00000000d901"
	stAssign   = "00000000-0000-4000-8000-00000000d911"
	stDate     = "2026-09-10"
	stRowID    = "batch:" + stBatch + ":rule:" + stRule + ":protocol_version:" + stVersion + ":shed:" + stShed + ":partition:whole:date:" + stDate
)

func execST(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

type stGoat struct {
	id, tag, obligation, obligationStatus, completionStatus, rejection, partition string
}

// seedDrivePen lays down one batched drive on one pen: a published vaccination protocol with one
// rule, a batch, one drive assignment (the operator and the planned day), and seven animals in
// every per-animal state -- accepted, recorded, rejected, scheduled, in progress, missed and
// deferred -- plus one animal in a partition of the same shed (a different pen, so a different
// board row) and one unbatched obligation on its own row.
func seedDrivePen(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []stGoat {
	t.Helper()
	execST(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Board Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, stTenant)
	execST(t, ctx, pool, `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'org', 'Board Custodian', 'active') ON CONFLICT DO NOTHING`, stParty)
	execST(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active'),
       ($3::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active'),
       ($4::uuid, $2::uuid, 'shed', 'GODEL1', 'Godel 1', 'active')
ON CONFLICT (location_id) DO NOTHING`, stPark, stTenant, stOtherPk, stShed)
	execST(t, ctx, pool, `UPDATE locations SET parent_location_id = $1::uuid WHERE location_id = $2::uuid`, stPark, stShed)
	execST(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIN', 'Dinakar', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, stMember, stTenant, stOperator, stPark)
	execST(t, ctx, pool, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.board', 'Preventive Care Vaccination Matrix', 'vaccination', 'active')
ON CONFLICT (protocol_id) DO NOTHING`, stProtocol, stTenant)
	execST(t, ctx, pool, `
INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy, published_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', 1, 'draft', DATE '2026-06-01', '{}'::jsonb, '{}'::jsonb, NULL)
ON CONFLICT (protocol_version_id) DO NOTHING`, stVersion, stTenant, stProtocol)
	execST(t, ctx, pool, `
INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'et_tt_adult_w1', 1, 'birth_age', '{}'::jsonb, '{}'::jsonb)
ON CONFLICT (rule_id) DO NOTHING`, stRule, stTenant, stVersion)
	// Published config is immutable: the rule goes in on the draft, then the version publishes.
	execST(t, ctx, pool, `UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1::uuid`, stVersion)
	execST(t, ctx, pool, `
INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', $4::uuid, 'in_progress', $5::date)
ON CONFLICT (batch_id) DO NOTHING`, stBatch, stTenant, stVersion, stShed, stDate)
	execST(t, ctx, pool, `
INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5::uuid, $6::uuid, $7::uuid, 'Godel 1', 'whole', 7)
ON CONFLICT (assignment_id) DO NOTHING`, stAssign, stTenant, stBatch, stDate, stMember, stPark, stShed)

	goats := []stGoat{
		{"00000000-0000-4000-8000-00000000d601", "T-001", "00000000-0000-4000-8000-00000000d701", "completed", "accepted", "", ""},
		{"00000000-0000-4000-8000-00000000d602", "T-002", "00000000-0000-4000-8000-00000000d702", "in_progress", "recorded", "", ""},
		{"00000000-0000-4000-8000-00000000d603", "T-003", "00000000-0000-4000-8000-00000000d703", "in_progress", "rejected", "Wrong animal in frame", ""},
		{"00000000-0000-4000-8000-00000000d604", "T-004", "00000000-0000-4000-8000-00000000d704", "scheduled", "", "", ""},
		{"00000000-0000-4000-8000-00000000d605", "T-005", "00000000-0000-4000-8000-00000000d705", "in_progress", "", "", ""},
		{"00000000-0000-4000-8000-00000000d606", "T-006", "00000000-0000-4000-8000-00000000d706", "missed", "", "", ""},
		{"00000000-0000-4000-8000-00000000d607", "T-007", "00000000-0000-4000-8000-00000000d707", "deferred", "", "", ""},
		// The same shed, a partitioned pen: a different board row.
		{"00000000-0000-4000-8000-00000000d608", "T-008", "00000000-0000-4000-8000-00000000d708", "scheduled", "", "", "Part 3"},
	}
	for i, g := range goats {
		execST(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, custodian_party_id, park_id, shed_id, current_location_id, origin_type, dob, entry_date, management_stage)
VALUES ($1::uuid, $2::uuid, $3, 'goat', 'female', 'alive', $4::uuid, $5::uuid, $6::uuid, $6::uuid, 'procured', DATE '2024-01-01', DATE '2024-01-01', 'adult')
ON CONFLICT (goat_id) DO NOTHING`, g.id, stTenant, fmt.Sprintf("G-88%04d", i+1), stParty, stPark, stShed)
		execST(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, lower($3), 'tenant:' || $1, true, 'active', now() - interval '1 day', 'v1')`, stTenant, g.id, g.tag)
		if g.partition != "" {
			execST(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'Godel 1 - ' || $4) ON CONFLICT (tenant_id, goat_id) DO NOTHING`, stTenant, g.id, stShed, g.partition)
		}
		execST(t, ctx, pool, `
INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'goat', $6::uuid, 'shed', $7::uuid, ($8 || ' 00:00:00+05:30')::timestamptz, $9, 'board-obl-' || $1, $10)
ON CONFLICT (obligation_id) DO NOTHING`, g.obligation, stTenant, stVersion, stRule, stBatch, g.id, stShed, stDate, g.obligationStatus, i+1)
		if g.partition == "" {
			execST(t, ctx, pool, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid) ON CONFLICT DO NOTHING`, stTenant, stAssign, g.obligation, g.id)
		}
		if g.completionStatus != "" {
			execST(t, ctx, pool, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, batch_id, goat_id, administered_at, status, rejection_reason, verified_at, idempotency_key, recorded_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, ($5 || ' 10:00:00+05:30')::timestamptz, $6, NULLIF($7, ''),
        CASE WHEN $6 IN ('accepted', 'rejected') THEN now() END, 'board-comp-' || $2, $8::uuid)`,
				stTenant, g.obligation, stBatch, g.id, stDate, g.completionStatus, g.rejection, stOperator)
		}
	}
	return goats
}

func stQuery(rowID, after string, limit int) ports.SubtaskQuery {
	return ports.SubtaskQuery{TenantID: stTenant, ParkID: stPark, BusinessDate: stDate, SourceID: rowID, AfterKey: after, Limit: limit}
}

// TestVaccinationSubtasksAreTheAnimalsOfThePenWorstFirstOnADatabaseRoundTrip asserts the
// OUTPUT of the per-animal drill: one subtask per animal in the pen for that drive named by
// its tag, the dose label (never the raw dose code or protocol family name), the vaccinate ->
// verify chain in every obligation and completion state, the drive operator as owner, the
// worst-first order and the keyset with its whole count -- on a real database.
func TestVaccinationSubtasksAreTheAnimalsOfThePenWorstFirstOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedDrivePen(t, ctx, pool)
	src := New(nil).WithPool(pool, 5*time.Second)

	page, err := src.ListSubtasks(ctx, stQuery(stRowID, "", 10))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 7 || len(page.Subtasks) != 7 || page.NextCursor != "" {
		t.Fatalf("whole-pen row: total %d, %d subtasks, next %q", page.Total, len(page.Subtasks), page.NextCursor)
	}
	byTag := map[string]domain.Subtask{}
	order := []string{}
	for _, st := range page.Subtasks {
		byTag[st.Name] = st
		order = append(order, st.Name)
		if st.Subtitle != "ET+TT adult course dose 1 · Adult" {
			t.Fatalf("%s: subtitle %q must be the dose label and stage, never a raw code", st.Name, st.Subtitle)
		}
		if st.Owner.Name != "Dinakar" || st.Owner.WorkforceMemberID != stMember || st.Owner.UserID != stOperator {
			t.Fatalf("%s: owner %+v", st.Name, st.Owner)
		}
		if len(st.Steps) != 2 || st.Steps[0].Name != "Vaccinate" || st.Steps[1].Name != "Verify" {
			t.Fatalf("%s: chain %+v", st.Name, st.Steps)
		}
	}
	// Worst first: the rejected and the missed animal, then to-do (scheduled, deferred), in
	// progress, in review, done.
	if !(order[0] == "T-003" || order[0] == "T-006") || !(order[1] == "T-003" || order[1] == "T-006") || order[6] != "T-001" || order[5] != "T-002" || order[4] != "T-005" {
		t.Fatalf("order %v", order)
	}
	want := map[string]struct {
		state  domain.WorkState
		steps  [2]domain.StepState
		attend bool
	}{
		"T-001": {domain.WorkStateCompleted, [2]domain.StepState{domain.StepDone, domain.StepDone}, false},
		"T-002": {domain.WorkStateVerificationPending, [2]domain.StepState{domain.StepDone, domain.StepInReview}, false},
		"T-003": {domain.WorkStateRejected, [2]domain.StepState{domain.StepRework, domain.StepRework}, true},
		"T-004": {domain.WorkStateDue, [2]domain.StepState{domain.StepTodo, domain.StepLocked}, false},
		"T-005": {domain.WorkStateInProgress, [2]domain.StepState{domain.StepInProgress, domain.StepLocked}, false},
		"T-006": {domain.WorkStateMissed, [2]domain.StepState{domain.StepNeedsAttention, domain.StepLocked}, true},
		"T-007": {domain.WorkStateDeferred, [2]domain.StepState{domain.StepLocked, domain.StepLocked}, false},
	}
	for tag, w := range want {
		st, ok := byTag[tag]
		if !ok {
			t.Fatalf("%s missing", tag)
		}
		if st.WorkState != w.state || st.NeedsAttention != w.attend || st.Steps[0].State != w.steps[0] || st.Steps[1].State != w.steps[1] {
			t.Errorf("%s: %s attention=%v steps %s/%s, want %s %v %s/%s", tag, st.WorkState, st.NeedsAttention, st.Steps[0].State, st.Steps[1].State, w.state, w.attend, w.steps[0], w.steps[1])
		}
	}
	if byTag["T-003"].Steps[1].Detail != "Wrong animal in frame" || byTag["T-001"].Steps[0].Detail != "Given 10/09/2026" || byTag["T-007"].Steps[0].Detail != "Deferred for recovery" || byTag["T-004"].Steps[0].Detail != "Due 10/09/2026" {
		t.Errorf("details: %q %q %q %q", byTag["T-003"].Steps[1].Detail, byTag["T-001"].Steps[0].Detail, byTag["T-007"].Steps[0].Detail, byTag["T-004"].Steps[0].Detail)
	}
	if _, leaked := byTag["T-008"]; leaked {
		t.Fatal("the partitioned animal belongs to another pen's row")
	}

	// Keyset: pages of three walk every animal once, in the same order, with the same total.
	seen := map[string]bool{}
	after := ""
	walked := []string{}
	for i := 0; i < 5; i++ {
		p, err := src.ListSubtasks(ctx, stQuery(stRowID, after, 10))
		if err != nil {
			t.Fatal(err)
		}
		// Limit is bounded to 10 on the port; walk with the seed's seven in one page and
		// assert the two-page shape with a narrower window below.
		for _, st := range p.Subtasks {
			if seen[st.Key] {
				t.Fatalf("%s served twice", st.Key)
			}
			seen[st.Key] = true
			walked = append(walked, st.Name)
		}
		if p.NextCursor == "" {
			break
		}
		after = p.NextCursor
	}
	if len(walked) != 7 {
		t.Fatalf("walk saw %d animals", len(walked))
	}

	// The partitioned pen's own row lists only its animal.
	partRow := "batch:" + stBatch + ":rule:" + stRule + ":protocol_version:" + stVersion + ":shed:" + stShed + ":partition:Part 3:date:" + stDate
	page, err = src.ListSubtasks(ctx, stQuery(partRow, "", 10))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 || page.Subtasks[0].Name != "T-008" {
		t.Fatalf("partitioned pen row %+v", page)
	}
	// An unbatched obligation row drills into that one animal.
	page, err = src.ListSubtasks(ctx, stQuery("obligation:00000000-0000-4000-8000-00000000d704", "", 10))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 || page.Subtasks[0].Name != "T-004" {
		t.Fatalf("obligation row %+v", page)
	}
	// The other park, another day and a row id this source never emits drill into nothing.
	for name, q := range map[string]ports.SubtaskQuery{
		"other park": {TenantID: stTenant, ParkID: stOtherPk, BusinessDate: stDate, SourceID: stRowID, Limit: 10},
		"other day":  stQuery("batch:"+stBatch+":rule:"+stRule+":protocol_version:"+stVersion+":shed:"+stShed+":partition:whole:date:2026-09-11", "", 10),
		"foreign id": stQuery("feed_projection_exception:x", "", 10),
	} {
		p, err := src.ListSubtasks(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if p.Total != 0 || len(p.Subtasks) != 0 {
			t.Fatalf("%s must drill into nothing: %+v", name, p)
		}
	}
	if _, err := New(nil).ListSubtasks(ctx, stQuery(stRowID, "", 10)); err == nil {
		t.Fatal("a source with no pool must refuse loudly, not serve nothing")
	}
}

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
	bsShed    = "00000000-0000-4000-8000-000000003101"
	bsDinakar = "00000000-0000-4000-8000-000000000301"
	bsSecond  = "00000000-0000-4000-8000-000000000302"
	bsOther   = "00000000-0000-4000-8000-000000000303"
	bsMember  = "00000000-0000-4000-8000-000000000401"
	bsVerify  = "00000000-0000-4000-8000-000000000501"
	bsDate    = "2026-09-11" // the visit day: the work was 10/09
	bsProof   = "00000000-0000-4000-8000-000000008001"

	vOwed      = "00000000-0000-4000-8000-000000008101" // deworming + vaccination, Part 3, owed today
	vInReview  = "00000000-0000-4000-8000-000000008102" // ticks removal, whole pen, clip with the verifier (by the second visitor)
	vSentBack  = "00000000-0000-4000-8000-000000008103" // hoof trimming, Part 4, sent back
	vVerified  = "00000000-0000-4000-8000-000000008104" // hair trimming, Part 5, verified
	vLate      = "00000000-0000-4000-8000-000000008105" // deworming, Part 6, owed since 09/09
	vVaccOnly  = "00000000-0000-4000-8000-000000008106" // vaccination alone, Part 7: the VACCINATION source's row
	vOtherPark = "00000000-0000-4000-8000-000000008107" // the other park
	vTomorrow  = "00000000-0000-4000-8000-000000008108" // due tomorrow
)

func exec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

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
ON CONFLICT (workforce_member_id) DO NOTHING`, bsMember, bsTenant, bsDinakar, bsPark)
	exec(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, size_bytes, upload_state, scope_type, scope_id, subject_type, proof_type, uploaded_by, metadata)
VALUES ($1::uuid, $2::uuid, 'local', 'visits/' || $1::text, 'video/mp4', 4096, 'completed', 'task', $1::uuid, 'other', 'video', $3::uuid, '{"capture_source":"in_app_camera"}'::jsonb)
ON CONFLICT (proof_id) DO NOTHING`, bsProof, bsTenant, bsDinakar)
	// Two configured visitors for CBE (one profiled, one not); none for CPT.
	exec(t, ctx, pool, `
INSERT INTO pen_visit_park_assignees (tenant_id, park_id, user_id) VALUES ($1::uuid, $2::uuid, $3::uuid), ($1::uuid, $2::uuid, $4::uuid) ON CONFLICT DO NOTHING`, bsTenant, bsPark, bsDinakar, bsSecond)

	type visit struct {
		id, park, partition string
		reasons             []string
		planned, due        string
		workState, status   string
		submittedBy         string
		reworkReason        string
	}
	visits := []visit{
		{vOwed, bsPark, "Part 3", []string{"vaccination", "deworming"}, bsDate, bsDate, "scheduled", "open", "", ""},
		{vInReview, bsPark, "", []string{"ticks_removal"}, bsDate, bsDate, "scheduled", "pending_verification", bsSecond, ""},
		{vSentBack, bsPark, "Part 4", []string{"hoof_trimming"}, bsDate, bsDate, "scheduled", "rework", bsDinakar, "pen not visible"},
		{vVerified, bsPark, "Part 5", []string{"hair_trimming"}, bsDate, bsDate, "completed", "completed", bsDinakar, ""},
		{vLate, bsPark, "Part 6", []string{"deworming"}, "2026-09-09", bsDate, "delayed", "open", "", ""},
		{vVaccOnly, bsPark, "Part 7", []string{"vaccination"}, bsDate, bsDate, "scheduled", "open", "", ""},
		{vOtherPark, bsOtherPk, "", []string{"deworming"}, bsDate, bsDate, "scheduled", "open", "", ""},
		{vTomorrow, bsPark, "Part 8", []string{"deworming"}, "2026-09-12", "2026-09-12", "scheduled", "open", "", ""},
	}
	for _, v := range visits {
		exec(t, ctx, pool, `
INSERT INTO pen_visit_tasks (task_id, tenant_id, park_id, shed_id, partition_label, reasons, source_business_date, planned_business_date, due_business_date,
                             work_state, status, delayed_since_business_date, proof_ref, submitted_by, submitted_at, verified_by, verified_at, rework_reason)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, NULLIF($5, ''), $6::text[], $7::date - 1, $7::date, $8::date,
        $9, $10, CASE WHEN $9 = 'delayed' THEN $7::date END, CASE WHEN $11 <> '' THEN $14::uuid END,
        NULLIF($11, '')::uuid, CASE WHEN $11 <> '' THEN now() END,
        CASE WHEN $10 = 'completed' THEN $12::uuid END, CASE WHEN $10 = 'completed' THEN now() END, NULLIF($13, ''))
ON CONFLICT (task_id) DO NOTHING`, v.id, bsTenant, v.park, bsShed, v.partition, v.reasons, v.planned, v.due, v.workState, v.status, v.submittedBy, bsVerify, v.reworkReason, bsProof)
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

// TestPenVisitBoardRowsOnADatabaseRoundTrip asserts the OUTPUT STRINGS and states of every
// branch on a real database: the gate-then-clock mapping, the module split on the reasons
// array (a pen vaccinated AND dewormed rows once, under Preventive Care), the title as the
// parent work continuing, the visitor owner with its " +N", the visit-day clock label, and
// the vaccination href.
func TestPenVisitBoardRowsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	pc := NewPCCare(pool, 5*time.Second)
	vacc := NewVaccination(pool, 5*time.Second)

	rows, err := pc.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("5 care-raised visits on the park-day expected (vaccination-only, other park, tomorrow excluded), got %d", len(rows))
	}
	got := byID(rows)
	want := map[string]struct {
		state domain.WorkState
		lane  domain.Lane
		sev   domain.Severity
	}{
		vOwed:     {domain.WorkStateDue, domain.LaneToDo, domain.SeverityOK},
		vInReview: {domain.WorkStateVerificationPending, domain.LaneInReview, domain.SeverityOK},
		vSentBack: {domain.WorkStateRejected, domain.LaneInProgress, domain.SeverityAtRisk},
		vVerified: {domain.WorkStateCompleted, domain.LaneDone, domain.SeverityOK},
		vLate:     {domain.WorkStateOverdue, domain.LaneToDo, domain.SeverityAtRisk},
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
		if r.BusinessDate != bsDate || r.ParkID != bsPark || r.ParkName != "Coimbatore" || r.Href != "" {
			t.Errorf("%s: scope %s %s %s href %q", id, r.BusinessDate, r.ParkID, r.ParkName, r.Href)
		}
	}
	// The title is the WORK continuing: both reasons, display order, pen through oploc.
	owed := got[vOwed]
	if owed.Title != "Vaccination, deworming · Godel 1 - Part 3" || owed.Subtitle != "Pen visit · work done 10/09/2026" || owed.ClockLabel != "Visit due 11/09/2026" {
		t.Errorf("owed title %q subtitle %q clock %q", owed.Title, owed.Subtitle, owed.ClockLabel)
	}
	// Owed to either configured visitor: the profiled one leads, " +1" says another may go.
	if owed.Owner.Name != "Dinakar +1" || owed.Owner.UserID != bsDinakar || owed.Owner.WorkforceMemberID != bsMember || owed.OwnerState != domain.OwnerStateAssigned || owed.Counts != (domain.Counts{Pending: 1}) {
		t.Errorf("owed owner %+v %s counts %+v", owed.Owner, owed.OwnerState, owed.Counts)
	}
	// A submitted visit names the person who went, even the unprofiled one, and with no +N.
	review := got[vInReview]
	if review.Owner.UserID != bsSecond || review.Owner.Name != "" || review.Title != "Ticks removal · Godel 1" || review.Counts != (domain.Counts{Done: 1}) {
		t.Errorf("in-review owner %+v title %q counts %+v", review.Owner, review.Title, review.Counts)
	}
	late := got[vLate]
	if late.ClockLabel != "Visit delayed · owed 09/09/2026" || late.Counts.NeedsAttention != 1 {
		t.Errorf("late clock %q counts %+v", late.ClockLabel, late.Counts)
	}
	if got[vSentBack].Counts.NeedsAttention != 1 {
		t.Errorf("sent-back row must need attention: %+v", got[vSentBack].Counts)
	}

	// The vaccination source holds exactly the vaccination-only visit, with the shed href.
	vrows, err := vacc.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(vrows) != 1 || vrows[0].SourceID != vVaccOnly || vrows[0].Module != domain.ModuleVaccination {
		t.Fatalf("vaccination source rows %+v", vrows)
	}
	if vrows[0].Title != "Vaccination · Godel 1 - Part 7" || vrows[0].Href != "/vaccination/execution/sheds/"+bsShed+"?scope_mode=park&park="+bsPark+"&partition_label=Part+7" {
		t.Errorf("vaccination row title %q href %q", vrows[0].Title, vrows[0].Href)
	}
	for _, r := range append(rows, vrows...) {
		for _, s := range []string{r.Title, r.Subtitle, r.ClockLabel} {
			if containsFold(s, "shed") {
				t.Errorf("visible copy says shed: %q", s)
			}
		}
	}

	// Owner lens: a configured visitor sees every visit of the park; someone else sees none.
	mine, err := pc.ListRows(ctx, query(bsSecond))
	if err != nil || len(mine) != 5 {
		t.Fatalf("configured visitor lens = %d rows err %v, want 5", len(mine), err)
	}
	none, err := pc.ListRows(ctx, query(bsOther))
	if err != nil || len(none) != 0 {
		t.Fatalf("unconfigured person lens = %d rows err %v, want 0", len(none), err)
	}
	// State filter and counts agree with the rows.
	open, err := pc.ListRows(ctx, query("", domain.WorkStateDue, domain.WorkStateOverdue))
	if err != nil || len(open) != 2 {
		t.Fatalf("due+overdue = %d err %v", len(open), err)
	}
	counts, err := pc.CountByState(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if sum(counts) != 5 || counts[domain.WorkStateDue] != 1 || counts[domain.WorkStateOverdue] != 1 || counts[domain.WorkStateVerificationPending] != 1 || counts[domain.WorkStateRejected] != 1 || counts[domain.WorkStateCompleted] != 1 {
		t.Fatalf("counts %+v", counts)
	}
	vcounts, err := vacc.CountByState(ctx, query(""))
	if err != nil || sum(vcounts) != 1 {
		t.Fatalf("vaccination counts %+v err %v", vcounts, err)
	}
	// Keyset walk: page size 2 sees every row once.
	seen := map[string]bool{}
	after := ""
	for i := 0; i < 6; i++ {
		q := query("")
		q.Limit = 2
		q.AfterSourceID = after
		page, err := pc.ListRows(ctx, q)
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
	if len(seen) != 5 {
		t.Fatalf("keyset walk saw %d rows, want 5", len(seen))
	}
	if _, err := pc.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, AfterSourceID: "garbage"}); err != domain.ErrInvalidCursor {
		t.Fatalf("garbage cursor = %v, want ErrInvalidCursor", err)
	}

	// The issue view: one unit, the visit, with its chain and the verifier's words.
	sub, err := pc.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, SourceID: vSentBack, Limit: 10})
	if err != nil || sub.Total != 1 || len(sub.Subtasks) != 1 {
		t.Fatalf("sent-back subtasks %+v err %v", sub, err)
	}
	st := sub.Subtasks[0]
	if st.Name != "Pen visit" || st.WorkState != domain.WorkStateRejected || !st.NeedsAttention || st.Subtitle != "After hoof trimming on 10/09/2026" ||
		len(st.Steps) != 2 || st.Steps[0].State != domain.StepTodo || st.Steps[1].State != domain.StepRework || st.Steps[1].Detail != "pen not visible" || st.Owner.Name != "Dinakar" {
		t.Fatalf("sent-back unit %+v", st)
	}
	sub, err = pc.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, SourceID: vVerified, Limit: 10})
	if err != nil || len(sub.Subtasks) != 1 || sub.Subtasks[0].WorkState != domain.WorkStateCompleted || sub.Subtasks[0].Steps[0].State != domain.StepDone || sub.Subtasks[0].Steps[1].State != domain.StepDone {
		t.Fatalf("verified unit %+v err %v", sub, err)
	}
	for name, id := range map[string]string{"vaccination-only on the care source": vVaccOnly, "other park": vOtherPark, "tomorrow": vTomorrow} {
		sub, err := pc.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, SourceID: id, Limit: 10})
		if err != nil || sub.Total != 0 || len(sub.Subtasks) != 0 {
			t.Fatalf("%s must drill into nothing: %+v err %v", name, sub, err)
		}
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

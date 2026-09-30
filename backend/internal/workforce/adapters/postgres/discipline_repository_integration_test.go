package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	hrmssoppg "github.com/vgoats/goatos/backend/internal/hrmssop/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	dsTenant    = "00000000-0000-4000-8000-0000000000b1"
	dsCBE       = "7a000000-0000-4000-8000-000000000301"
	dsCPT       = "7a000000-0000-4000-8000-000000000302"
	dsHRUser    = "91000000-0000-4000-8000-000000000500"
	dsHeadUser  = "91000000-0000-4000-8000-000000000501"
	dsOtherHead = "91000000-0000-4000-8000-000000000502"
)

func dsMember(i int) string { return fmt.Sprintf("97000000-0000-4000-8000-%012d", 500+i) }

type dsFixture struct {
	repo *Repository
	svc  *app.DisciplineService
	exec func(sql string, args ...any)
	ctx  context.Context
}

// dsSeed builds one tenant: two parks, people at each, a park head per park (one park-scoped, one
// tenant-scoped seated by home park -- the live roster's shape), and a published HRMS SOP v2 with
// two violation types.
func dsSeed(t *testing.T) dsFixture {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Discipline Tenant', 'active')`, dsTenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active')`, dsCBE, dsTenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')`, dsCPT, dsTenant)
	for i, m := range []struct{ name, park, user string }{
		{"Amit", dsCPT, ""}, {"Bhavya", dsCPT, ""}, {"Chetan", dsCPT, ""}, {"Farid", dsCBE, ""},
		{"Head CPT", dsCPT, dsHeadUser}, {"Head CBE", dsCBE, dsOtherHead}, {"HR", dsCBE, dsHRUser},
	} {
		exec(`INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_location_id)
VALUES ($1::uuid, $2::uuid, NULLIF($3, '')::uuid, $4, $4, 'active', $5::uuid)`, dsMember(i), dsTenant, m.user, m.name, m.park)
	}
	exec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park_head', 'park', $3::uuid, 'active', now() - interval '1 day')`, dsTenant, dsHeadUser, dsCPT)
	exec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park_head', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, dsTenant, dsOtherHead)
	// The seed v1 was published by the migration for tenants that existed then; this tenant is new,
	// so publish its own v1 + v2 the way the web editor would.
	exec(`INSERT INTO sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
VALUES ($1::uuid, 'hrms.violations', 'Violations and enquiries', '', 'active', 'action', 'module', 'hrms')`, dsTenant)
	doc := `{"schema_version":"goatos.sop-form.v1","sop_code":"hrms.violations","title":"x","fields":[],
"violations":{"schema_version":"goatos.sop-hrms-violations.v1",
 "violation_types":[{"key":"negligence","title":"Negligence","default_fine":500,"active":true},
                    {"key":"late","title":"Late to shift","default_fine":100,"active":true},
                    {"key":"old","title":"Old type","default_fine":50,"active":false}],
 "enquiries":[{"trigger":"animal_death","title":"Death enquiry","due_hours":48,
   "questions":[{"id":"what_happened","kind":"text","title":"What happened","required":true},
                {"id":"preventable","kind":"yes_no","title":"Could it have been prevented?","required":false}]}]}}`
	exec(`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT $1::uuid, sop_id, 2, 'v2', 'published', $2::jsonb, '{}'::jsonb, '{}'::jsonb, '{"valid":true,"errors":[],"warnings":[]}'::jsonb, now()
FROM sop_definitions WHERE tenant_id = $1::uuid AND code = 'hrms.violations'`, dsTenant, doc)
	repo := NewRepository(pool, 5*time.Second)
	return dsFixture{repo: repo, svc: app.NewDisciplineService(repo, repo, hrmssoppg.NewSource(pool, 5*time.Second), nil), exec: exec, ctx: ctx}
}

var (
	dsHR   = app.DisciplineCaller{All: true, UserID: dsHRUser}
	dsHead = app.DisciplineCaller{UserID: dsHeadUser}
)

func ip(v int) *int { return &v }

// A hand-recorded violation: pinned to the published version, the type's label snapshotted, the
// fine defaulted, an exact replay returning the original, a reused key with other content refused,
// a retired or unknown type refused, and a withdraw fenced and kept on record.
func TestViolationRecordReplayAndWithdrawWithDockerPostgres(t *testing.T) {
	f := dsSeed(t)
	req := domain.RecordViolationRequest{PersonID: dsMember(0), TypeKey: "negligence", OccurredOn: "2026-09-29", Note: "Left the gate open", IdempotencyKey: "k1"}
	got, err := f.svc.RecordViolation(f.ctx, dsTenant, dsHRUser, req, "t")
	if err != nil {
		t.Fatal(err)
	}
	v := got.Violation
	if v.FineRupees != 500 || v.FineLabel != "₹500" || v.TypeLabel != "Negligence" || v.SOPVersion != 2 || v.ParkLabel != "Channapatna" || v.OccurredOnLabel != "29/09/2026" || v.Status != "recorded" {
		t.Fatalf("recorded = %+v", v)
	}
	again, err := f.svc.RecordViolation(f.ctx, dsTenant, dsHRUser, req, "t")
	if err != nil || again.Violation.ViolationID != v.ViolationID {
		t.Fatalf("replay = %+v %v", again, err)
	}
	other := req
	other.FineRupees = ip(900)
	if _, err := f.svc.RecordViolation(f.ctx, dsTenant, dsHRUser, other, "t"); !isCode(err, "idempotency_conflict") {
		t.Fatalf("same key, other fine = %v", err)
	}
	for _, key := range []string{"old", "missing"} {
		bad := req
		bad.TypeKey, bad.IdempotencyKey = key, "k-"+key
		if _, err := f.svc.RecordViolation(f.ctx, dsTenant, dsHRUser, bad, "t"); !isCode(err, "unknown_violation_type") {
			t.Fatalf("type %s = %v", key, err)
		}
	}
	future := req
	future.OccurredOn, future.IdempotencyKey = time.Now().AddDate(0, 0, 3).Format("2006-01-02"), "k-future"
	if _, err := f.svc.RecordViolation(f.ctx, dsTenant, dsHRUser, future, "t"); !isCode(err, "invalid_date") {
		t.Fatalf("future date = %v", err)
	}
	if _, err := f.svc.WithdrawViolation(f.ctx, dsTenant, dsHRUser, v.ViolationID, domain.WithdrawViolationRequest{Reason: " ", RowVersion: 1}, "t"); !isCode(err, "reason_required") {
		t.Fatalf("blank reason = %v", err)
	}
	w, err := f.svc.WithdrawViolation(f.ctx, dsTenant, dsHRUser, v.ViolationID, domain.WithdrawViolationRequest{Reason: "Wrong person", RowVersion: 1}, "t")
	if err != nil || w.Violation.Status != "withdrawn" || w.Violation.StatusLabel != "Withdrawn" {
		t.Fatalf("withdraw = %+v %v", w, err)
	}
	if _, err := f.svc.WithdrawViolation(f.ctx, dsTenant, dsHRUser, v.ViolationID, domain.WithdrawViolationRequest{Reason: "Other reason", RowVersion: 1}, "t"); !isCode(err, "violation_version_conflict") {
		t.Fatalf("second withdraw = %v", err)
	}
}

// The death enquiry: opened once per animal however often the event arrives, pinned to the
// published version with its 48-hour deadline; the park head of THAT park sees and fills it, the
// other park's head does not; submitting records every named violation in the same transaction;
// a second submit is refused; required answers and unknown answers are enforced.
func TestDeathEnquiryOpensOnceAndItsReportRecordsViolationsWithDockerPostgres(t *testing.T) {
	f := dsSeed(t)
	goat := "a1000000-0000-4000-8000-000000000001"
	died := time.Date(2026, 9, 29, 6, 30, 0, 0, time.UTC)
	for range 3 {
		if err := f.svc.OpenDeathEnquiry(f.ctx, dsTenant, goat, dsCPT, "", died); err != nil {
			t.Fatal(err)
		}
	}
	page, err := f.svc.Enquiries(f.ctx, dsTenant, dsHR, "", "", "", 0, "t")
	if err != nil || len(page.Items) != 1 || page.Summary.Open != 1 {
		t.Fatalf("enquiries = %+v %v", page, err)
	}
	e := page.Items[0]
	if e.Title != "Death enquiry" || e.ParkLabel != "Channapatna" || e.OccurredAtLabel != "29/09/2026" {
		t.Fatalf("enquiry = %+v", e)
	}
	if head, err := f.svc.Enquiries(f.ctx, dsTenant, dsHead, "", "", "", 0, "t"); err != nil || len(head.Items) != 1 {
		t.Fatalf("CPT head sees %+v %v", head, err)
	}
	cbeHead := app.DisciplineCaller{UserID: dsOtherHead}
	if other, err := f.svc.Enquiries(f.ctx, dsTenant, cbeHead, "", "", "", 0, "t"); err != nil || len(other.Items) != 0 {
		t.Fatalf("CBE head (tenant-scoped, seated at CBE) must not see CPT's enquiry: %+v %v", other, err)
	}
	if _, err := f.svc.Enquiry(f.ctx, dsTenant, cbeHead, e.EnquiryID, "t"); !isCode(err, "not_found") {
		t.Fatalf("CBE head opening CPT's enquiry = %v", err)
	}
	detail, err := f.svc.Enquiry(f.ctx, dsTenant, dsHead, e.EnquiryID, "t")
	if err != nil || len(detail.Questions) != 2 || len(detail.Types) != 2 || !detail.CanSubmit || detail.SOPVersion != 2 {
		t.Fatalf("detail = %+v %v", detail, err)
	}
	for _, p := range detail.People {
		if p.ParkID != dsCPT {
			t.Fatalf("a person from another park is offered: %+v", p)
		}
	}
	submit := domain.SubmitEnquiryRequest{RowVersion: e.RowVersion, Answers: map[string]any{"preventable": true}}
	if _, err := f.svc.SubmitEnquiry(f.ctx, dsTenant, dsHead, dsHeadUser, e.EnquiryID, submit, "t"); !isCode(err, "answer_required") {
		t.Fatalf("missing required answer = %v", err)
	}
	submit.Answers = map[string]any{"what_happened": "Found dead in the morning", "weather": "rain"}
	if _, err := f.svc.SubmitEnquiry(f.ctx, dsTenant, dsHead, dsHeadUser, e.EnquiryID, submit, "t"); !isCode(err, "unknown_answer") {
		t.Fatalf("unknown answer = %v", err)
	}
	submit.Answers = map[string]any{"what_happened": "Found dead in the morning", "preventable": true}
	submit.Penalties = []domain.EnquiryPenalty{{PersonID: dsMember(3), TypeKey: "negligence"}}
	if _, err := f.svc.SubmitEnquiry(f.ctx, dsTenant, dsHead, dsHeadUser, e.EnquiryID, submit, "t"); !isCode(err, "penalty_person") {
		t.Fatalf("penalising a CBE person on a CPT enquiry = %v", err)
	}
	submit.Penalties = []domain.EnquiryPenalty{
		{PersonID: dsMember(0), TypeKey: "negligence", Note: "Night round skipped"},
		{PersonID: dsMember(1), TypeKey: "late", FineRupees: ip(250)},
	}
	done, err := f.svc.SubmitEnquiry(f.ctx, dsTenant, dsHead, dsHeadUser, e.EnquiryID, submit, "t")
	if err != nil {
		t.Fatal(err)
	}
	if done.CanSubmit || done.Enquiry.Status != "submitted" || len(done.Violations) != 2 || done.Enquiry.PenaltyLabel != "2 people penalised" || done.Enquiry.SubmittedByName != "Head CPT" {
		t.Fatalf("submitted = %+v", done)
	}
	for _, v := range done.Violations {
		if v.Source != "enquiry" || v.EnquiryID != e.EnquiryID || v.OccurredOn != "2026-09-29" || v.SOPVersion != 2 {
			t.Fatalf("enquiry violation = %+v", v)
		}
	}
	if _, err := f.svc.SubmitEnquiry(f.ctx, dsTenant, dsHead, dsHeadUser, e.EnquiryID, submit, "t"); !isCode(err, "enquiry_submitted") {
		t.Fatalf("second submit = %v", err)
	}
	month, err := f.svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", "", "", 0, "t")
	if err != nil || month.Summary.Count != 2 || month.Summary.FineRupees != 750 || month.Summary.People != 2 || month.Summary.FineLabel != "₹750" {
		t.Fatalf("month = %+v %v", month.Summary, err)
	}
}

// An enquiry can close with nobody penalised.
func TestEnquiryNobodyPenalisedWithDockerPostgres(t *testing.T) {
	f := dsSeed(t)
	if err := f.svc.OpenDeathEnquiry(f.ctx, dsTenant, "a1000000-0000-4000-8000-000000000002", dsCBE, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	page, _ := f.svc.Enquiries(f.ctx, dsTenant, dsHR, "", "open", "", 0, "t")
	done, err := f.svc.SubmitEnquiry(f.ctx, dsTenant, dsHR, dsHRUser, page.Items[0].EnquiryID,
		domain.SubmitEnquiryRequest{RowVersion: page.Items[0].RowVersion, Answers: map[string]any{"what_happened": "Old age"}}, "t")
	if err != nil || done.Enquiry.PenaltyLabel != "Nobody penalised" || len(done.Violations) != 0 {
		t.Fatalf("nobody = %+v %v", done, err)
	}
}

func dsRecord(t *testing.T, f dsFixture, person int, key, date string, fine int) domain.Violation {
	t.Helper()
	got, err := f.svc.RecordViolation(f.ctx, dsTenant, dsHRUser, domain.RecordViolationRequest{
		PersonID: dsMember(person), TypeKey: key, FineRupees: ip(fine), OccurredOn: date, IdempotencyKey: fmt.Sprintf("%d-%s-%s-%d", person, key, date, fine)}, "t")
	if err != nil {
		t.Fatal(err)
	}
	return got.Violation
}

// One person with several violations joined to their designation and park: totals count each
// violation once and each person once.
func TestViolationTotalsOneToManyCountEachViolationOnce(t *testing.T) {
	f := dsSeed(t)
	f.exec(`INSERT INTO person_access (tenant_id, workforce_member_id, designation_code) VALUES ($1::uuid, $2::uuid, 'manager_feed')`, dsTenant, dsMember(0))
	dsRecord(t, f, 0, "negligence", "2026-09-10", 500)
	dsRecord(t, f, 0, "late", "2026-09-11", 100)
	dsRecord(t, f, 0, "late", "2026-09-12", 100)
	dsRecord(t, f, 1, "late", "2026-09-12", 100)
	page, err := f.svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", "", "", 0, "t")
	if err != nil || page.Summary.Count != 4 || page.Summary.FineRupees != 800 || page.Summary.People != 2 {
		t.Fatalf("summary = %+v %v", page.Summary, err)
	}
	if len(page.ByPerson) != 2 || page.ByPerson[0].PersonName != "Amit" || page.ByPerson[0].Count != 3 || page.ByPerson[0].FineRupees != 700 || page.ByPerson[0].Designation != "Feed Manager" {
		t.Fatalf("by person = %+v", page.ByPerson)
	}
}

// Keyset pages of two across the month boundary: every violation exactly once, newest first, and
// the summary never moves with the page.
func TestViolationListPageBoundary(t *testing.T) {
	f := dsSeed(t)
	for d := 1; d <= 5; d++ {
		dsRecord(t, f, 0, "late", fmt.Sprintf("2026-09-%02d", d), 10*d)
	}
	dsRecord(t, f, 0, "late", "2026-08-31", 999)
	var dates []string
	cursor := ""
	for range 10 {
		page, err := f.svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", "", cursor, 2, "t")
		if err != nil {
			t.Fatal(err)
		}
		if page.Summary.Count != 5 {
			t.Fatalf("summary moved with the page: %+v", page.Summary)
		}
		for _, v := range page.Items {
			dates = append(dates, v.OccurredOn[8:])
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if strings.Join(dates, ",") != "05,04,03,02,01" {
		t.Fatalf("paged = %v", dates)
	}
}

// Park scope: HR narrowed to one park sees only it; a park head sees only their park.
func TestViolationParkScope(t *testing.T) {
	f := dsSeed(t)
	dsRecord(t, f, 0, "late", "2026-09-10", 100)
	dsRecord(t, f, 3, "late", "2026-09-10", 100)
	cbe, err := f.svc.Violations(f.ctx, dsTenant, dsHR, dsCBE, "2026-09", "", "", 0, "t")
	if err != nil || cbe.Summary.Count != 1 || cbe.Items[0].PersonName != "Farid" {
		t.Fatalf("CBE = %+v %v", cbe, err)
	}
	head, err := f.svc.Violations(f.ctx, dsTenant, dsHead, "", "2026-09", "", "", 0, "t")
	if err != nil || head.Summary.Count != 1 || head.Items[0].PersonName != "Amit" || len(head.Parks) != 1 {
		t.Fatalf("CPT head = %+v %v", head, err)
	}
	if _, err := f.svc.Violations(f.ctx, dsTenant, dsHead, dsCBE, "2026-09", "", "", 0, "t"); !isCode(err, "unknown_park") {
		t.Fatalf("CPT head asking for CBE = %v", err)
	}
}

// Status matrix: recorded and withdrawn are disjoint and together are everything.
func TestViolationStatusMatrix(t *testing.T) {
	f := dsSeed(t)
	a := dsRecord(t, f, 0, "late", "2026-09-10", 100)
	dsRecord(t, f, 1, "late", "2026-09-10", 200)
	if _, err := f.svc.WithdrawViolation(f.ctx, dsTenant, dsHRUser, a.ViolationID, domain.WithdrawViolationRequest{Reason: "Mistake", RowVersion: 1}, "t"); err != nil {
		t.Fatal(err)
	}
	for status, want := range map[string]int{"": 2, "recorded": 1, "withdrawn": 1} {
		page, err := f.svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", status, "", 0, "t")
		if err != nil || page.Summary.Count != want || len(page.Items) != want {
			t.Fatalf("status %q = %+v %v", status, page.Summary, err)
		}
	}
	if _, err := f.svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", "deleted", "", 0, "t"); !isCode(err, "invalid_filter") {
		t.Fatalf("unknown status = %v", err)
	}
}

func isCode(err error, code string) bool {
	var appErr *app.Error
	return errors.As(err, &appErr) && appErr.Code == code
}

var _ = ports.ErrNotFound

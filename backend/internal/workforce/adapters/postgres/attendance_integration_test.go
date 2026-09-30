package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// The automatic clock-in check (maintainer decisions 2026-09-30) on real Postgres: on time is
// nothing, past the 15-minute grace is ONE waiting "late" violation, no clock-in is ONE waiting
// "did not clock in" violation -- but only once the shift has ended -- and a day covered by leave
// the person applied for is never checked. A re-run inserts nothing; HR keeps one (with a fine) and
// closes the other; the per-person totals carry violations, waiting, closed AND leave days.
func TestAttendanceCheckRaisesKeepsAndClosesWithDockerPostgres(t *testing.T) {
	f := dsSeed(t)
	// v3 adds the clock-in check.
	doc := `{"schema_version":"goatos.sop-form.v1","sop_code":"hrms.violations","title":"x","fields":[],
"violations":{"schema_version":"goatos.sop-hrms-violations.v1",
 "violation_types":[{"key":"negligence","title":"Negligence","active":true},
                    {"key":"late_clock_in","title":"Late clock-in","active":true},
                    {"key":"did_not_clock_in","title":"Did not clock in","active":true}],
 "enquiries":[],
 "attendance":{"grace_minutes":15,"late_type":"late_clock_in","absent_type":"did_not_clock_in"}}}`
	f.exec(`UPDATE sop_versions SET status = 'retired' WHERE tenant_id = $1::uuid AND status = 'published'
AND sop_id = (SELECT sop_id FROM sop_definitions WHERE tenant_id = $1::uuid AND code = 'hrms.violations')`, dsTenant)
	f.exec(`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT $1::uuid, sop_id, 3, 'v3', 'published', $2::jsonb, '{}'::jsonb, '{}'::jsonb, '{"valid":true,"errors":[],"warnings":[]}'::jsonb, now() + interval '1 second'
FROM sop_definitions WHERE tenant_id = $1::uuid AND code = 'hrms.violations'`, dsTenant, doc)
	// CPT General shift 8:30 am - 6:00 pm; CBE has no time set, so Farid (CBE) is never checked.
	f.exec(`INSERT INTO workforce_park_shift_timings (tenant_id, park_id, shift_code, start_minute, end_minute)
VALUES ($1::uuid, $2::uuid, 'general', 510, 1080)`, dsTenant, dsCPT)
	// Amit on time, Bhavya late, Chetan on leave he applied for, Head CPT never clocks in, Farid no timing.
	for _, i := range []int{0, 1, 2, 3, 4} {
		f.exec(`INSERT INTO workforce_member_shifts (tenant_id, workforce_member_id, shift_code, updated_at)
VALUES ($1::uuid, $2::uuid, 'general', '2026-09-28 10:00+05:30')`, dsTenant, dsMember(i))
	}
	punch := func(member int, at string) {
		f.exec(`WITH ev AS (
  INSERT INTO workforce_clock_events (tenant_id, workforce_member_id, user_id, event_type, business_date, captured_at, recorded_at, location_status)
  VALUES ($1::uuid, $2::uuid, $2::uuid, 'clock_in', '2026-09-29', $3::timestamptz, $3::timestamptz, 'unavailable')
  RETURNING clock_event_id)
INSERT INTO workforce_clock_entries (tenant_id, workforce_member_id, business_date, clock_in_event_id, clock_in_at, status)
SELECT $1::uuid, $2::uuid, '2026-09-29', clock_event_id, $3::timestamptz, 'open' FROM ev`, dsTenant, dsMember(member), at)
	}
	punch(0, "2026-09-29 08:40+05:30")
	punch(1, "2026-09-29 09:10+05:30")
	f.exec(`INSERT INTO workforce_leave_requests (tenant_id, workforce_member_id, park_id, starts_on, ends_on, reason, status,
  raised_by_user_id, idempotency_key, request_fingerprint)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-09-29', '2026-09-30', 'Family function', 'pending', $2::uuid, 'lv-att-1', 'fp')`, dsTenant, dsMember(2), dsCPT)

	at := func(s string) func() time.Time {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return func() time.Time { return ts }
	}
	// Noon: Bhavya is late; nobody is absent yet (the shift has not ended).
	n, err := f.svc.WithClock(at("2026-09-29T12:00:00+05:30")).RaiseAttendanceViolations(f.ctx, dsTenant)
	if err != nil || n != 1 {
		t.Fatalf("noon raise = %d %v, want 1 (Bhavya late)", n, err)
	}
	// Evening: Head CPT did not clock in. Re-running inserts nothing twice.
	svc := f.svc.WithClock(at("2026-09-29T19:00:00+05:30"))
	if n, err = svc.RaiseAttendanceViolations(f.ctx, dsTenant); err != nil || n != 1 {
		t.Fatalf("evening raise = %d %v, want 1 (Head CPT absent)", n, err)
	}
	if n, err = svc.RaiseAttendanceViolations(f.ctx, dsTenant); err != nil || n != 0 {
		t.Fatalf("re-run = %d %v, want 0", n, err)
	}
	page, err := svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", "", domain.ViolationPending, "", 0, "t")
	if err != nil || len(page.Items) != 2 || page.Summary.Pending != 2 {
		t.Fatalf("waiting = %+v %v", page, err)
	}
	var late, absent domain.Violation
	for _, v := range page.Items {
		switch v.PersonName {
		case "Bhavya":
			late = v
		case "Head CPT":
			absent = v
		default:
			t.Fatalf("unexpected violation for %s (on time, on leave or untimed must be skipped)", v.PersonName)
		}
	}
	if late.TypeLabel != "Late clock-in" || late.StatusLabel != "Waiting for HR" || late.SourceLabel != "Clock-in check" ||
		!strings.Contains(late.Detail, "9:10 am") || !strings.Contains(late.Detail, "40 min late") || late.FineLabel != "No fine" {
		t.Fatalf("late = %+v", late)
	}
	if absent.TypeLabel != "Did not clock in" || absent.AttendanceKind != "absent" || !strings.Contains(absent.Detail, "starts 8:30 am") {
		t.Fatalf("absent = %+v", absent)
	}

	// HR keeps the late one with a fine and closes the absent one; a second decision is refused.
	kept, err := svc.KeepViolation(f.ctx, dsTenant, dsHRUser, late.ViolationID, domain.KeepViolationRequest{FineRupees: ip(200), Note: "Second time this week", RowVersion: late.RowVersion}, "t")
	if err != nil || kept.Violation.Status != "recorded" || kept.Violation.FineRupees != 200 || kept.Violation.DecidedByName != "HR" {
		t.Fatalf("keep = %+v %v", kept, err)
	}
	if _, err := svc.CloseViolation(f.ctx, dsTenant, dsHRUser, absent.ViolationID, domain.CloseViolationRequest{Reason: " ", RowVersion: absent.RowVersion}, "t"); !isCode(err, "reason_required") {
		t.Fatalf("close without a reason = %v", err)
	}
	closed, err := svc.CloseViolation(f.ctx, dsTenant, dsHRUser, absent.ViolationID, domain.CloseViolationRequest{Reason: "Was at the vet with an animal", RowVersion: absent.RowVersion}, "t")
	if err != nil || closed.Violation.Status != "closed" || closed.Violation.StatusLabel != "Closed by HR" || closed.Violation.DecisionNote != "Was at the vet with an animal" {
		t.Fatalf("close = %+v %v", closed, err)
	}
	if _, err := svc.KeepViolation(f.ctx, dsTenant, dsHRUser, absent.ViolationID, domain.KeepViolationRequest{RowVersion: closed.Violation.RowVersion}, "t"); !isCode(err, "violation_decided") {
		t.Fatalf("keeping a closed one = %v", err)
	}
	// A closed one is never raised again for the same day.
	if n, err = svc.RaiseAttendanceViolations(f.ctx, dsTenant); err != nil || n != 0 {
		t.Fatalf("after decisions = %d %v", n, err)
	}

	// The month: one violation owed (₹200), nothing waiting; per person, Chetan appears for his
	// applied leave alone, Head CPT for the closed one.
	month, err := svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", "month", "", "", 0, "t")
	if err != nil || month.Summary.Count != 1 || month.Summary.FineRupees != 200 || month.Summary.Pending != 0 {
		t.Fatalf("month summary = %+v %v", month.Summary, err)
	}
	by := map[string]domain.ViolationPersonTotal{}
	for _, p := range month.ByPerson {
		by[p.PersonName] = p
	}
	if b := by["Bhavya"]; b.Count != 1 || b.FineRupees != 200 {
		t.Fatalf("Bhavya = %+v", b)
	}
	if h := by["Head CPT"]; h.Count != 0 || h.Closed != 1 {
		t.Fatalf("Head CPT = %+v", h)
	}
	if c := by["Chetan"]; c.LeavePendingDays != 2 || c.LeaveLabel != "2 days applied" || c.Count != 0 {
		t.Fatalf("Chetan = %+v", c)
	}
	// Year and all time carry the same here; a bad period is refused.
	for _, p := range []string{"year", "all"} {
		got, err := svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", p, "", "", 0, "t")
		if err != nil || got.Summary.Count != 1 || got.Period != p {
			t.Fatalf("%s = %+v %v", p, got, err)
		}
	}
	if _, err := svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", "decade", "", "", 0, "t"); !isCode(err, "invalid_period") {
		t.Fatalf("bad period = %v", err)
	}
}

// Per-person totals are pre-aggregated apart: a person with three leave requests and two
// violations is counted two violations and the leave days summed once -- never multiplied.
func TestPersonTotalsDoNotMultiplyLeaveByViolationsOneToManyWithDockerPostgres(t *testing.T) {
	f := dsSeed(t)
	for i, d := range []string{"2026-09-03", "2026-09-10"} {
		f.exec(`INSERT INTO workforce_violations (tenant_id, workforce_member_id, park_id, type_key, type_label, fine_rupees, occurred_on, source, sop_version, recorded_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'late', 'Late to shift', $4, $5::date, 'manual', 2, $6::uuid)`, dsTenant, dsMember(0), dsCPT, 100*(i+1), d, dsHRUser)
	}
	for i, d := range [][2]string{{"2026-09-01", "2026-09-02"}, {"2026-09-15", "2026-09-15"}, {"2026-09-29", "2026-10-02"}} {
		f.exec(`INSERT INTO workforce_leave_requests (tenant_id, workforce_member_id, park_id, starts_on, ends_on, reason, status,
  raised_by_user_id, idempotency_key, request_fingerprint)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5::date, 'x', 'pending', $2::uuid, $6, 'fp')`, dsTenant, dsMember(0), dsCPT, d[0], d[1], "lv-otm-"+string(rune('a'+i)))
	}
	page, err := f.svc.Violations(f.ctx, dsTenant, dsHR, "", "2026-09", "", "", "", 0, "t")
	if err != nil || len(page.ByPerson) != 1 {
		t.Fatalf("by person = %+v %v", page.ByPerson, err)
	}
	p := page.ByPerson[0]
	// 2 + 1 + 2 (29-30 Sep only; the October days are outside the month) = 5 days applied.
	if p.Count != 2 || p.FineRupees != 300 || p.LeavePendingDays != 5 {
		t.Fatalf("Amit = %+v, want 2 violations, ₹300, 5 days applied", p)
	}
}

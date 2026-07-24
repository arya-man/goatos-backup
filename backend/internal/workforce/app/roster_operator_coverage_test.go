package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// ---- min-operator-coverage guard fixtures ----------------------------------
//
// Three vaccination-operator seats at the SAME park (center scope), each with
// a distinct weekly week-off, so the coverage math (week-off + overlapping
// approved/escalation leave) is exercised without relying on any other
// position type.
const (
	coverageOperatorA = "40000000-0000-4000-8000-000000000001" // week-off: monday
	coverageOperatorB = "40000000-0000-4000-8000-000000000002" // week-off: tuesday
	coverageOperatorC = "40000000-0000-4000-8000-000000000003" // week-off: wednesday
)

func operatorCoverageFixture(t *testing.T) (*RosterService, *fakeRosterRepo) {
	t.Helper()
	repo := newFakeRosterRepo()
	caps := newFakeCapabilityGranter()
	svc := NewRosterService(repo, caps)
	ctx := context.Background()

	create := func(memberID, code, weekOff string) {
		wo := weekOff
		if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
			TenantID: testTenant, ActorID: testActor,
			Body: domain.CreatePositionRequest{
				WorkforceMemberID: memberID, ScopeType: "center", ScopeID: rosterCenter,
				PositionCode: code, PositionTier: domain.PositionTierAssistant, WeekOffWeekday: &wo,
			},
		}, "trace-fixture"); err != nil {
			t.Fatalf("create position %s: %v", code, err)
		}
	}
	create(coverageOperatorA, "vaccination_operator_a", "monday")
	create(coverageOperatorB, "vaccination_operator_b", "tuesday")
	create(coverageOperatorC, "vaccination_operator_c", "wednesday")

	return svc, repo
}

func applyLeave(t *testing.T, svc *RosterService, memberID, startsOn, endsOn string) domain.StaffLeave {
	t.Helper()
	applied, err := svc.ApplyLeave(context.Background(), testTenant, testActor, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: memberID, ScopeType: "center", ScopeID: rosterCenter,
		ReasonCode: "personal", StartsOn: startsOn, EndsOn: endsOn,
	}, "trace-leave")
	if err != nil {
		t.Fatalf("ApplyLeave(%s): %v", memberID, err)
	}
	return applied.Leave
}

// TestRosterApproveLeaveRejectsWhenCoverageWouldHitZero: 2026-08-10 is a
// Monday (Operator A's week-off), so Operator A is already unavailable on
// that day by week-off alone. Operator B takes approved leave covering that
// same Monday, leaving only Operator C. Now approving Operator C's OWN leave
// for that Monday would leave ZERO available operators that day -- must be
// REJECTED and must NOT write a row (status stays 'reported', row_version
// unchanged).
func TestRosterApproveLeaveRejectsWhenCoverageWouldHitZero(t *testing.T) {
	svc, repo := operatorCoverageFixture(t)
	ctx := context.Background()

	// Operator B approved-leave covering Monday 2026-08-10.
	leaveB := applyLeave(t, svc, coverageOperatorB, "2026-08-10", "2026-08-10")
	if _, err := svc.ApproveLeave(ctx, testTenant, testActor, leaveB.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve-b"); err != nil {
		t.Fatalf("ApproveLeave(B): %v", err)
	}

	// Operator C requests leave for the SAME Monday. Operator A is off
	// (week-off=monday) and Operator B is on approved leave that day, so
	// approving C's leave would leave 0 available operators on 2026-08-10.
	leaveC := applyLeave(t, svc, coverageOperatorC, "2026-08-10", "2026-08-10")
	_, err := svc.ApproveLeave(ctx, testTenant, testActor, leaveC.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve-c")
	if err == nil {
		t.Fatal("ApproveLeave(C) succeeded, want rejection: coverage would hit zero on 2026-08-10")
	}
	appErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T, want *app.Error", err)
	}
	if appErr.Code != "min_operator_coverage" {
		t.Fatalf("error code = %q, want %q", appErr.Code, "min_operator_coverage")
	}
	if appErr.HTTPStatus != 409 {
		t.Fatalf("HTTPStatus = %d, want 409", appErr.HTTPStatus)
	}

	// No row must have been persisted/mutated: still 'reported', row_version 1.
	stored, ok := repo.leaves[leaveC.AbsenceID]
	if !ok {
		t.Fatal("leave C row vanished from the store")
	}
	if stored.Status != domain.LeaveStatusReported {
		t.Fatalf("leave C status = %q, want %q (rejection must not persist)", stored.Status, domain.LeaveStatusReported)
	}
	if stored.RowVersion != 1 {
		t.Fatalf("leave C row_version = %d, want unchanged 1", stored.RowVersion)
	}
}

// TestRosterApproveLeaveAcceptsWhenAtLeastOneOperatorRemains: same setup, but
// Operator C's leave is for TUESDAY (Operator B's week-off day, not Monday).
// On Tuesday: Operator A is available (week-off=monday, no leave), Operator B
// is off (week-off=tuesday) but that's fine since Operator A still covers it,
// and Operator C is the one taking leave. >=1 operator (A) remains -> ACCEPT.
func TestRosterApproveLeaveAcceptsWhenAtLeastOneOperatorRemains(t *testing.T) {
	svc, _ := operatorCoverageFixture(t)
	ctx := context.Background()

	leaveC := applyLeave(t, svc, coverageOperatorC, "2026-08-11", "2026-08-11") // Tuesday
	resp, err := svc.ApproveLeave(ctx, testTenant, testActor, leaveC.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve-c")
	if err != nil {
		t.Fatalf("ApproveLeave(C) on Tuesday: %v, want acceptance (Operator A still available)", err)
	}
	if resp.Leave.Status != domain.LeaveStatusApproved && resp.Leave.Status != domain.LeaveStatusEscalationRequired {
		t.Fatalf("status = %q, want approved or escalation_required (leave itself must still resolve)", resp.Leave.Status)
	}
}

// TestRosterMinOperatorCoverageCountsWeekOffAsUnavailable: with NO leave
// applied at all, requesting Operator B's leave on a WEDNESDAY (Operator C's
// week-off day) leaves only Operator A available (Operator B is the one
// leaving, Operator C is off for the week). >=1 remains (A) -> accepted. This
// confirms week-off days count toward unavailability in the coverage math
// (Operator C is correctly excluded from the "available" count purely via
// week-off, no leave record involved).
func TestRosterMinOperatorCoverageCountsWeekOffAsUnavailable(t *testing.T) {
	svc, _ := operatorCoverageFixture(t)
	ctx := context.Background()

	leaveB := applyLeave(t, svc, coverageOperatorB, "2026-08-12", "2026-08-12") // Wednesday
	if _, err := svc.ApproveLeave(ctx, testTenant, testActor, leaveB.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve-b"); err != nil {
		t.Fatalf("ApproveLeave(B) on Wednesday: %v, want acceptance (Operator A still available)", err)
	}

	// Now push a SECOND leave (Operator A) for the same Wednesday: Operator B
	// already approved-absent, Operator C off (week-off=wednesday) -> 0 remain.
	leaveA := applyLeave(t, svc, coverageOperatorA, "2026-08-12", "2026-08-12")
	_, err := svc.ApproveLeave(ctx, testTenant, testActor, leaveA.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve-a")
	if err == nil {
		t.Fatal("ApproveLeave(A) succeeded, want rejection: week-off (Operator C) + approved leave (Operator B) leaves 0 available on Wednesday")
	}
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "min_operator_coverage" {
		t.Fatalf("error = %v, want *app.Error{Code: min_operator_coverage}", err)
	}
}

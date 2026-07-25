package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// ---- BLOCKER 2: leave scope must not gate the coverage guard --------------
//
// The coverage guard must be keyed off the LEAVING MEMBER's own vaccination-
// operator seat, not the scope_type of the leave being applied/approved: a
// tenant-wide or shed-scoped leave for a vaccination operator drops their
// park's availability exactly like a center-scoped leave does (this mirrors
// the scheduler's own leave-overlap predicate, which has no scope filter at
// all -- see AvailableVaccinationOperatorsForDrive).

// TestRosterApplyLeaveTenantScopeCountsTowardCoverage: Operator B's week-off
// is Tuesday, Operator C's is Wednesday. A TENANT-scoped approved leave for
// Operator A on Monday, plus Operator B's own leave request for the SAME
// Monday, would leave 0 available operators (A on leave tenant-wide, B taking
// leave, C week-off is Wednesday so C IS available on Monday... to force zero
// we also need C off). Use Wednesday instead: Operator C's week-off is
// Wednesday, Operator A holds a TENANT-scoped approved leave covering
// Wednesday, so requesting Operator B's leave for Wednesday must be REJECTED
// (0 remaining: A tenant-leave, C week-off, B is the one leaving).
func TestRosterApplyLeaveTenantScopeCountsTowardCoverage(t *testing.T) {
	svc, repo := operatorCoverageFixture(t)
	ctx := context.Background()

	// Operator A takes a TENANT-scoped leave for Wednesday 2026-08-12 (not center-scoped).
	appliedA, err := svc.ApplyLeave(ctx, testTenant, testActor, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: coverageOperatorA, ScopeType: "tenant", ScopeID: testTenant,
		ReasonCode: "personal", StartsOn: "2026-08-12", EndsOn: "2026-08-12",
	}, "trace-tenant-a")
	if err != nil {
		t.Fatalf("ApplyLeave(A, tenant-scope): %v", err)
	}
	if _, err := svc.ApproveLeave(ctx, testTenant, testActor, appliedA.Leave.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve-a"); err != nil {
		t.Fatalf("ApproveLeave(A, tenant-scope): %v", err)
	}

	// Operator B now requests center-scoped leave for the SAME Wednesday. Operator C is off
	// (week-off=wednesday), Operator A is on approved TENANT-scoped leave (must still count) ->
	// approving/applying B's leave would leave 0 available operators. Must be REJECTED.
	before := len(repo.leaves)
	err = applyLeaveExpectErr(t, svc, coverageOperatorB, "2026-08-12", "2026-08-12")
	if err == nil {
		t.Fatal("ApplyLeave(B) succeeded, want rejection: Operator A's TENANT-scoped leave must count toward coverage")
	}
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "min_operator_coverage" {
		t.Fatalf("error = %v, want *app.Error{Code: min_operator_coverage}", err)
	}
	if len(repo.leaves) != before {
		t.Fatalf("leave store size = %d, want unchanged %d (rejection must not create a reported row)", len(repo.leaves), before)
	}
}

// TestRosterApplyLeaveShedScopeCountsTowardCoverage: same shape as the tenant-scope
// test, but Operator A's leave is SHED-scoped (a shed under the park), not tenant- or
// center-scoped. Must still count toward the park's coverage.
func TestRosterApplyLeaveShedScopeCountsTowardCoverage(t *testing.T) {
	svc, repo := operatorCoverageFixture(t)
	ctx := context.Background()
	const shedID = "30000000-0000-4000-8000-000000000009"
	repo.registerShedPark(shedID, rosterCenter)

	appliedA, err := svc.ApplyLeave(ctx, testTenant, testActor, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: coverageOperatorA, ScopeType: "shed", ScopeID: shedID,
		ReasonCode: "personal", StartsOn: "2026-08-12", EndsOn: "2026-08-12",
	}, "trace-shed-a")
	if err != nil {
		t.Fatalf("ApplyLeave(A, shed-scope): %v", err)
	}
	if _, err := svc.ApproveLeave(ctx, testTenant, testActor, appliedA.Leave.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve-a"); err != nil {
		t.Fatalf("ApproveLeave(A, shed-scope): %v", err)
	}

	before := len(repo.leaves)
	err = applyLeaveExpectErr(t, svc, coverageOperatorB, "2026-08-12", "2026-08-12")
	if err == nil {
		t.Fatal("ApplyLeave(B) succeeded, want rejection: Operator A's SHED-scoped leave must count toward coverage")
	}
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "min_operator_coverage" {
		t.Fatalf("error = %v, want *app.Error{Code: min_operator_coverage}", err)
	}
	if len(repo.leaves) != before {
		t.Fatalf("leave store size = %d, want unchanged %d (rejection must not create a reported row)", len(repo.leaves), before)
	}
}

// ---- BLOCKER 3: membership must be duty-based, not position_code prefix ----

// TestRosterCoverageCountsDutyBasedNonPrefixedSeat: a seat named "pc_field_staff_1"
// (does NOT match the vaccination_operator_ prefix) but carrying the vaccination
// execute duty must still be counted as a vaccination operator. With this seat as
// the ONLY other operator besides the leaving member, its presence is what keeps
// coverage >= 1; if the guard were still prefix-based this seat would be invisible
// and the leave would be wrongly REJECTED.
func TestRosterCoverageCountsDutyBasedNonPrefixedSeat(t *testing.T) {
	repo := newFakeRosterRepo()
	caps := newFakeCapabilityGranter()
	svc := NewRosterService(repo, caps)
	ctx := context.Background()

	const leavingMember = "40000000-0000-4000-8000-000000000011"
	const nonPrefixedMember = "40000000-0000-4000-8000-000000000012"

	mustCreate := func(memberID, code string) {
		if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
			TenantID: testTenant, ActorID: testActor,
			Body: domain.CreatePositionRequest{
				WorkforceMemberID: memberID, ScopeType: "center", ScopeID: rosterCenter,
				PositionCode: code, PositionTier: domain.PositionTierAssistant,
			},
		}, "trace-fixture"); err != nil {
			t.Fatalf("create position %s: %v", code, err)
		}
	}
	mustCreate(leavingMember, "vaccination_operator_x")
	mustCreate(nonPrefixedMember, "pc_field_staff_1")
	repo.registerVaccinationDuty("vaccination_operator_x")
	repo.registerVaccinationDuty("pc_field_staff_1") // duty present despite non-matching name

	leave, err := svc.ApplyLeave(ctx, testTenant, testActor, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: leavingMember, ScopeType: "center", ScopeID: rosterCenter,
		ReasonCode: "personal", StartsOn: "2026-08-10", EndsOn: "2026-08-10",
	}, "trace-leave")
	if err != nil {
		t.Fatalf("ApplyLeave: %v", err)
	}
	if _, err := svc.ApproveLeave(ctx, testTenant, testActor, leave.Leave.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve"); err != nil {
		t.Fatalf("ApproveLeave: %v, want acceptance (duty-based non-prefixed seat must be counted)", err)
	}
}

// TestRosterCoverageIgnoresPrefixedSeatWithoutDuty: a seat NAMED
// "vaccination_operator_ghost" (matches the OLD prefix convention) but with NO
// registered vaccination-execute duty must NOT be counted. With this seat as the
// only other "operator", the leave must be REJECTED (0 real coverage remains) --
// if the guard were still prefix-based, this ghost seat would incorrectly count
// and the leave would be wrongly ACCEPTED.
func TestRosterCoverageIgnoresPrefixedSeatWithoutDuty(t *testing.T) {
	repo := newFakeRosterRepo()
	caps := newFakeCapabilityGranter()
	svc := NewRosterService(repo, caps)
	ctx := context.Background()

	const leavingMember = "40000000-0000-4000-8000-000000000013"
	const ghostMember = "40000000-0000-4000-8000-000000000014"

	mustCreate := func(memberID, code string) {
		if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
			TenantID: testTenant, ActorID: testActor,
			Body: domain.CreatePositionRequest{
				WorkforceMemberID: memberID, ScopeType: "center", ScopeID: rosterCenter,
				PositionCode: code, PositionTier: domain.PositionTierAssistant,
			},
		}, "trace-fixture"); err != nil {
			t.Fatalf("create position %s: %v", code, err)
		}
	}
	mustCreate(leavingMember, "vaccination_operator_y")
	mustCreate(ghostMember, "vaccination_operator_ghost") // NOT registered with the duty
	repo.registerVaccinationDuty("vaccination_operator_y")

	// The apply-time guard (same checkMinOperatorCoverage helper) already rejects this,
	// same as the approve-time guard would -- the duty-less "ghost" seat must not count
	// toward coverage at either checkpoint.
	err := applyLeaveExpectErr(t, svc, leavingMember, "2026-08-10", "2026-08-10")
	if err == nil {
		t.Fatal("ApplyLeave succeeded, want rejection: duty-less prefixed seat must NOT be counted as a vaccination operator")
	}
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "min_operator_coverage" {
		t.Fatalf("error = %v, want *app.Error{Code: min_operator_coverage}", err)
	}
}

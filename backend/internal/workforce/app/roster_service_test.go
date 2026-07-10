package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// fakeRosterRepo is an in-memory, DB-free fake of ports.RosterRepository so
// the coverage engine's rule logic (effective_backup resolution, escalation,
// derived execute permission, dedupe) is unit-testable without Postgres.
// Mirrors the fakeRepo pattern already used for the non-roster Service tests
// in service_test.go.
type fakeRosterRepo struct {
	positions           map[string]domain.Position
	leaves              map[string]domain.StaffLeave
	createPositionCalls int
	nextSeq             int
}

func newFakeRosterRepo() *fakeRosterRepo {
	return &fakeRosterRepo{positions: map[string]domain.Position{}, leaves: map[string]domain.StaffLeave{}}
}

var _ ports.RosterRepository = (*fakeRosterRepo)(nil)

func fakeUUID(seq int) string {
	return fmt.Sprintf("%08x-0000-4000-8000-%012x", seq, seq)
}

func (f *fakeRosterRepo) CreatePosition(_ context.Context, cmd ports.CreatePositionCommand) (domain.Position, error) {
	f.createPositionCalls++
	for id, p := range f.positions {
		if p.ScopeType == cmd.Body.ScopeType && p.ScopeID == cmd.Body.ScopeID && p.PositionCode == cmd.Body.PositionCode && p.Status == "active" {
			p.Status = "ended"
			f.positions[id] = p
		}
	}
	f.nextSeq++
	id := fakeUUID(f.nextSeq)
	now := time.Now().UTC().Format(time.RFC3339)
	item := domain.Position{
		PositionID: id, WorkforceMemberID: cmd.Body.WorkforceMemberID, ScopeType: cmd.Body.ScopeType, ScopeID: cmd.Body.ScopeID,
		PositionCode: cmd.Body.PositionCode, PositionTier: cmd.Body.PositionTier, IsBackupSlot: cmd.Body.IsBackupSlot,
		BackupGroupCode: cmd.Body.BackupGroupCode, WeekOffWeekday: cmd.Body.WeekOffWeekday, Status: "active",
		ValidFrom: now, RowVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	f.positions[id] = item
	return item, nil
}

func (f *fakeRosterRepo) ListPositions(_ context.Context, params ports.ListPositionsParams) ([]domain.Position, error) {
	out := []domain.Position{}
	for _, p := range f.positions {
		if params.WorkforceMemberID != "" && p.WorkforceMemberID != params.WorkforceMemberID {
			continue
		}
		if params.PositionCode != "" && p.PositionCode != params.PositionCode {
			continue
		}
		if params.Status != "" && p.Status != params.Status {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeRosterRepo) GetActivePositionByCode(_ context.Context, _ string, scopeType, scopeID, positionCode string, _ time.Time) (domain.Position, error) {
	for _, p := range f.positions {
		if p.ScopeType == scopeType && p.ScopeID == scopeID && p.PositionCode == positionCode && p.Status == "active" {
			return p, nil
		}
	}
	return domain.Position{}, ports.ErrNotFound
}

func (f *fakeRosterRepo) GetActiveBackupSlot(_ context.Context, _ string, scopeType, scopeID, backupGroupCode string, _ time.Time) (domain.Position, error) {
	for _, p := range f.positions {
		if p.ScopeType == scopeType && p.ScopeID == scopeID && p.IsBackupSlot && p.BackupGroupCode != nil && *p.BackupGroupCode == backupGroupCode && p.Status == "active" {
			return p, nil
		}
	}
	return domain.Position{}, ports.ErrNotFound
}

func (f *fakeRosterRepo) GetActivePositionForMember(_ context.Context, _ string, workforceMemberID string) (domain.Position, error) {
	for _, p := range f.positions {
		if p.WorkforceMemberID == workforceMemberID && p.Status == "active" {
			return p, nil
		}
	}
	return domain.Position{}, ports.ErrNotFound
}

func (f *fakeRosterRepo) ApplyLeave(_ context.Context, cmd ports.ApplyLeaveCommand) (domain.StaffLeave, error) {
	f.nextSeq++
	id := fakeUUID(f.nextSeq)
	leave := domain.StaffLeave{
		AbsenceID: id, WorkforceMemberID: cmd.Body.WorkforceMemberID, ScopeType: cmd.Body.ScopeType, ScopeID: cmd.Body.ScopeID,
		ReasonCode: cmd.Body.ReasonCode, Status: domain.LeaveStatusReported,
		StartsAt: cmd.StartsAt.UTC().Format(time.RFC3339), EndsAt: cmd.EndsAt.UTC().Format(time.RFC3339), RowVersion: 1,
	}
	f.leaves[id] = leave
	return leave, nil
}

func (f *fakeRosterRepo) ApproveLeave(_ context.Context, cmd ports.ApproveLeaveCommand) (domain.StaffLeave, error) {
	leave, ok := f.leaves[cmd.AbsenceID]
	if !ok || leave.RowVersion != cmd.RowVersion || leave.Status != domain.LeaveStatusReported {
		return domain.StaffLeave{}, ports.ErrConflict
	}
	leave.Status = domain.LeaveStatusApproved
	leave.RowVersion++
	f.leaves[cmd.AbsenceID] = leave
	return leave, nil
}

func (f *fakeRosterRepo) ResolveLeaveCoverage(_ context.Context, cmd ports.ResolveLeaveCoverageCommand) (domain.StaffLeave, error) {
	leave, ok := f.leaves[cmd.AbsenceID]
	if !ok {
		return domain.StaffLeave{}, ports.ErrNotFound
	}
	leave.Status = cmd.Status
	leave.ReplacementMemberID = cmd.ReplacementMemberID
	if cmd.OverrideReason != nil {
		leave.CoverageOverrideReason = cmd.OverrideReason
	}
	leave.RowVersion++
	f.leaves[cmd.AbsenceID] = leave
	return leave, nil
}

func (f *fakeRosterRepo) GetLeave(_ context.Context, _ string, absenceID string) (domain.StaffLeave, error) {
	leave, ok := f.leaves[absenceID]
	if !ok {
		return domain.StaffLeave{}, ports.ErrNotFound
	}
	return leave, nil
}

func (f *fakeRosterRepo) ListLeave(_ context.Context, params ports.ListLeaveParams) ([]domain.StaffLeave, error) {
	out := []domain.StaffLeave{}
	for _, l := range f.leaves {
		if params.WorkforceMemberID != "" && l.WorkforceMemberID != params.WorkforceMemberID {
			continue
		}
		if params.Status != "" && l.Status != params.Status {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

func (f *fakeRosterRepo) IsMemberOnApprovedLeave(_ context.Context, _ string, workforceMemberID string, at time.Time) (bool, string, error) {
	for _, l := range f.leaves {
		if l.WorkforceMemberID != workforceMemberID || (l.Status != domain.LeaveStatusApproved && l.Status != domain.LeaveStatusEscalationRequired) {
			continue
		}
		starts, err1 := time.Parse(time.RFC3339, l.StartsAt)
		ends, err2 := time.Parse(time.RFC3339, l.EndsAt)
		if err1 != nil || err2 != nil {
			continue
		}
		if !at.Before(starts) && at.Before(ends) {
			return true, l.AbsenceID, nil
		}
	}
	return false, "", nil
}

// fakeCapabilityGranter is an in-memory fake of ports.CapabilityGranter
// (workforce_member_capabilities reuse, design doc S4.6).
type fakeCapabilityGranter struct {
	grants      map[string][]domain.CapabilityAssignment // keyed by workforce_member_id
	assignCalls int
}

func newFakeCapabilityGranter() *fakeCapabilityGranter {
	return &fakeCapabilityGranter{grants: map[string][]domain.CapabilityAssignment{}}
}

var _ ports.CapabilityGranter = (*fakeCapabilityGranter)(nil)

func (f *fakeCapabilityGranter) AssignCapability(_ context.Context, cmd ports.CapabilityCommand) (domain.CapabilityAssignment, error) {
	f.assignCalls++
	validFrom := time.Now().UTC().Format(time.RFC3339)
	if cmd.Body.ValidFrom != nil {
		validFrom = *cmd.Body.ValidFrom
	}
	item := domain.CapabilityAssignment{
		MemberCapabilityID: fmt.Sprintf("cap-%d", f.assignCalls),
		CapabilityCode:     cmd.Body.CapabilityCode,
		ScopeType:          cmd.Body.ScopeType,
		ScopeID:            cmd.Body.ScopeID,
		Status:             "active",
		ValidFrom:          validFrom,
		ValidTo:            cmd.Body.ValidTo,
	}
	f.grants[cmd.OperatorID] = append(f.grants[cmd.OperatorID], item)
	return item, nil
}

func (f *fakeCapabilityGranter) ListCapabilities(_ context.Context, _ string, operatorID string) ([]domain.CapabilityAssignment, error) {
	return f.grants[operatorID], nil
}

// ---- shared roster test fixtures -------------------------------------------

const (
	rosterCenter        = "20000000-0000-4000-8000-000000000001"
	memberPCM           = "30000000-0000-4000-8000-000000000001" // Preventive Care Manager holder
	memberBackupManager = "30000000-0000-4000-8000-000000000002" // Backup Manager holder (manager tier)
	memberFeedingAM1    = "30000000-0000-4000-8000-000000000003" // Feeding AM1 holder (assistant tier)
	memberBackupAM1     = "30000000-0000-4000-8000-000000000004" // Backup AM1 holder (assistant tier)
	memberParkHead      = "30000000-0000-4000-8000-000000000005"
	memberFeedingMgr    = "30000000-0000-4000-8000-000000000006" // a DIFFERENT functional manager, never a valid backup
)

// rosterFixture wires the exact two-tier shape confirmed in
// docs/hr/roster-rbac-design.md S4.3/S0's confirmed decisions: manager-tier
// positions share one "manager_backup" group (-> Backup Manager), and the
// assistant tier has its OWN parallel "am1_backup" group (-> Backup AM1) --
// both resolved through the SAME generic effective_backup lookup.
func rosterFixture(t *testing.T) (*RosterService, *fakeRosterRepo, *fakeCapabilityGranter) {
	t.Helper()
	repo := newFakeRosterRepo()
	caps := newFakeCapabilityGranter()
	svc := NewRosterService(repo, caps)
	ctx := context.Background()

	create := func(memberID, code, tier string, isBackupSlot bool, backupGroup *string) {
		if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
			TenantID: testTenant, ActorID: testActor,
			Body: domain.CreatePositionRequest{
				WorkforceMemberID: memberID, ScopeType: "center", ScopeID: rosterCenter,
				PositionCode: code, PositionTier: tier, IsBackupSlot: isBackupSlot, BackupGroupCode: backupGroup,
			},
		}, "trace-fixture"); err != nil {
			t.Fatalf("create position %s: %v", code, err)
		}
	}
	create(memberPCM, "preventive_care_manager", domain.PositionTierManager, false, stringPtr("manager_backup"))
	create(memberBackupManager, "backup_manager", domain.PositionTierManager, true, stringPtr("manager_backup"))
	create(memberFeedingAM1, "feeding_am1", domain.PositionTierAssistant, false, stringPtr("am1_backup"))
	create(memberBackupAM1, "backup_am1", domain.PositionTierAssistant, true, stringPtr("am1_backup"))
	create(memberParkHead, "park_head", domain.PositionTierHead, false, nil)
	create(memberFeedingMgr, "feeding_manager", domain.PositionTierManager, false, stringPtr("manager_backup"))

	return svc, repo, caps
}

func applyAndApproveLeave(t *testing.T, svc *RosterService, memberID string) domain.StaffLeave {
	t.Helper()
	ctx := context.Background()
	applied, err := svc.ApplyLeave(ctx, testTenant, testActor, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: memberID, ScopeType: "center", ScopeID: rosterCenter,
		ReasonCode: "personal", StartsOn: "2026-08-03", EndsOn: "2026-08-05", // Monday-Wednesday
	}, "trace-leave")
	if err != nil {
		t.Fatalf("ApplyLeave(%s): %v", memberID, err)
	}
	approved, err := svc.ApproveLeave(ctx, testTenant, testActor, applied.Leave.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve")
	if err != nil {
		t.Fatalf("ApproveLeave(%s): %v", memberID, err)
	}
	return approved.Leave
}

// ---- tests ------------------------------------------------------------------

func TestRosterApproveLeaveAssignsManagerTierBackup(t *testing.T) {
	svc, _, caps := rosterFixture(t)
	leave := applyAndApproveLeave(t, svc, memberPCM)

	if leave.Status != domain.LeaveStatusApproved {
		t.Fatalf("status = %q, want approved", leave.Status)
	}
	if leave.ReplacementMemberID == nil || *leave.ReplacementMemberID != memberBackupManager {
		t.Fatalf("replacement_member_id = %v, want %q", leave.ReplacementMemberID, memberBackupManager)
	}
	grants := caps.grants[memberBackupManager]
	if len(grants) != 1 {
		t.Fatalf("expected exactly one temporary capability grant for the backup manager, got %d", len(grants))
	}
	if grants[0].CapabilityCode != "vaccination.execute" || grants[0].ScopeType != "center" || grants[0].ScopeID != rosterCenter {
		t.Fatalf("unexpected grant: %#v", grants[0])
	}
}

func TestRosterApproveLeaveAssignsOperatorTierBackup(t *testing.T) {
	svc, _, _ := rosterFixture(t)
	leave := applyAndApproveLeave(t, svc, memberFeedingAM1)

	if leave.Status != domain.LeaveStatusApproved {
		t.Fatalf("status = %q, want approved", leave.Status)
	}
	if leave.ReplacementMemberID == nil || *leave.ReplacementMemberID != memberBackupAM1 {
		t.Fatalf("operator-tier replacement = %v, want %q (Backup AM1, not Backup Manager)", leave.ReplacementMemberID, memberBackupAM1)
	}
}

func TestRosterApproveLeaveEscalatesWhenBackupAlsoUnavailable(t *testing.T) {
	svc, _, _ := rosterFixture(t)
	// Backup Manager is on leave for the SAME window as the covered manager.
	applyAndApproveLeave(t, svc, memberBackupManager)
	leave := applyAndApproveLeave(t, svc, memberPCM)

	if leave.Status != domain.LeaveStatusEscalationRequired {
		t.Fatalf("status = %q, want escalation_required", leave.Status)
	}
	if leave.ReplacementMemberID != nil {
		t.Fatalf("replacement_member_id = %v, want nil (never auto-assign a different functional manager)", leave.ReplacementMemberID)
	}
}

func TestRosterResolveLeaveCoverageRejectsNonBackupOverride(t *testing.T) {
	svc, _, _ := rosterFixture(t)
	ctx := context.Background()
	applied, err := svc.ApplyLeave(ctx, testTenant, testActor, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: memberPCM, ScopeType: "center", ScopeID: rosterCenter,
		ReasonCode: "personal", StartsOn: "2026-08-03", EndsOn: "2026-08-05",
	}, "trace-leave")
	if err != nil {
		t.Fatalf("ApplyLeave: %v", err)
	}
	if _, err := svc.ApproveLeave(ctx, testTenant, testActor, applied.Leave.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve"); err != nil {
		t.Fatalf("ApproveLeave: %v", err)
	}

	// A CEO tries to override cover to the Feeding Manager -- a different
	// functional manager, never a valid backup_group_code member.
	_, err = svc.ResolveLeaveCoverage(ctx, testTenant, testActor, applied.Leave.AbsenceID, domain.ResolveLeaveCoverageRequest{
		ReplacementMemberID: stringPtr(memberFeedingMgr),
		OverrideReason:      stringPtr("testing an invalid override"),
	}, "trace-ceo")
	assertAppCode(t, err, "cross_cover_rejected")
}

func TestRosterApproveLeaveDoesNotChangePositionOwnership(t *testing.T) {
	svc, repo, _ := rosterFixture(t)
	callsBefore := repo.createPositionCalls

	applyAndApproveLeave(t, svc, memberPCM)

	if repo.createPositionCalls != callsBefore {
		t.Fatalf("createPositionCalls changed from %d to %d -- leave/coverage must never mutate position ownership", callsBefore, repo.createPositionCalls)
	}
	holder, err := repo.GetActivePositionForMember(context.Background(), testTenant, memberPCM)
	if err != nil {
		t.Fatalf("GetActivePositionForMember: %v", err)
	}
	if holder.Status != "active" || holder.PositionCode != "preventive_care_manager" {
		t.Fatalf("member's position changed: %#v", holder)
	}
}

func TestRosterGrantTemporaryExecuteCapabilityOnlyForWindow(t *testing.T) {
	svc, _, caps := rosterFixture(t)
	leave := applyAndApproveLeave(t, svc, memberPCM)

	grants := caps.grants[memberBackupManager]
	if len(grants) != 1 {
		t.Fatalf("expected exactly one grant, got %d", len(grants))
	}
	grant := grants[0]
	if grant.ValidTo == nil || *grant.ValidTo != leave.EndsAt {
		t.Fatalf("grant valid_to = %v, want the leave window end %q", grant.ValidTo, leave.EndsAt)
	}
	if grant.Status != "active" {
		t.Fatalf("grant status = %q, want active (workforce_member_capabilities expires on its own via valid_to)", grant.Status)
	}
}

func TestRosterResolveVaccinationOwnerWeekOffBackupDedupesCapabilityGrant(t *testing.T) {
	svc, repo, caps := rosterFixture(t)
	ctx := context.Background()

	// 2026-08-03 is a Monday. Give the Preventive Care Manager a Monday
	// week-off directly on the position record.
	for id, p := range repo.positions {
		if p.WorkforceMemberID == memberPCM {
			weekday := "monday"
			p.WeekOffWeekday = &weekday
			repo.positions[id] = p
		}
	}

	owner1, err := svc.ResolveVaccinationOwner(ctx, testTenant, testActor, "center", rosterCenter, "2026-08-03", "trace-1")
	if err != nil {
		t.Fatalf("ResolveVaccinationOwner (1st call): %v", err)
	}
	if owner1.Owner.OwnerSource != domain.OwnerSourceWeekOffBackup {
		t.Fatalf("owner_source = %q, want %q (reason=%v)", owner1.Owner.OwnerSource, domain.OwnerSourceWeekOffBackup, owner1.Owner.Reason)
	}
	if owner1.Owner.OwnerWorkforceMemberID == nil || *owner1.Owner.OwnerWorkforceMemberID != memberBackupManager {
		t.Fatalf("owner = %v, want backup manager %q", owner1.Owner.OwnerWorkforceMemberID, memberBackupManager)
	}

	// Resolving again for the SAME day must not grant a second capability row
	// (maintainer 2026-07-10: write/notify once for the same day).
	if _, err := svc.ResolveVaccinationOwner(ctx, testTenant, testActor, "center", rosterCenter, "2026-08-03", "trace-2"); err != nil {
		t.Fatalf("ResolveVaccinationOwner (2nd call): %v", err)
	}
	grants := caps.grants[memberBackupManager]
	if len(grants) != 1 {
		t.Fatalf("expected exactly one dedup'd grant after two resolutions for the same day, got %d", len(grants))
	}

	// A normal (non-week-off) day resolves straight to the position holder,
	// no grant at all.
	ownerNormal, err := svc.ResolveVaccinationOwner(ctx, testTenant, testActor, "center", rosterCenter, "2026-08-04", "trace-3")
	if err != nil {
		t.Fatalf("ResolveVaccinationOwner (normal day): %v", err)
	}
	if ownerNormal.Owner.OwnerSource != domain.OwnerSourcePositionHolder {
		t.Fatalf("owner_source on normal day = %q, want %q", ownerNormal.Owner.OwnerSource, domain.OwnerSourcePositionHolder)
	}
	if ownerNormal.Owner.OwnerWorkforceMemberID == nil || *ownerNormal.Owner.OwnerWorkforceMemberID != memberPCM {
		t.Fatalf("owner on normal day = %v, want holder %q", ownerNormal.Owner.OwnerWorkforceMemberID, memberPCM)
	}
}

func TestRosterResolveVaccinationOwnerEscalatesToParkHead(t *testing.T) {
	svc, repo, _ := rosterFixture(t)
	ctx := context.Background()
	// Both the Preventive Care Manager and the Backup Manager are unavailable
	// on the same day (Monday week-off for both).
	applyAndApproveLeave(t, svc, memberBackupManager)

	for id, p := range repo.positions {
		if p.WorkforceMemberID == memberPCM {
			weekday := "monday"
			p.WeekOffWeekday = &weekday
			repo.positions[id] = p
		}
	}

	owner, err := svc.ResolveVaccinationOwner(ctx, testTenant, testActor, "center", rosterCenter, "2026-08-03", "trace-escalate")
	if err != nil {
		t.Fatalf("ResolveVaccinationOwner: %v", err)
	}
	if owner.Owner.OwnerSource != domain.OwnerSourceNone {
		t.Fatalf("owner_source = %q, want none (escalated)", owner.Owner.OwnerSource)
	}
	if owner.Owner.EscalationParkHeadMemberID == nil || *owner.Owner.EscalationParkHeadMemberID != memberParkHead {
		t.Fatalf("escalation target = %v, want park head %q", owner.Owner.EscalationParkHeadMemberID, memberParkHead)
	}
}

func TestRosterHasNoHolderWhenPositionUnassigned(t *testing.T) {
	repo := newFakeRosterRepo()
	svc := NewRosterService(repo, newFakeCapabilityGranter())
	owner, err := svc.ResolveVaccinationOwner(context.Background(), testTenant, testActor, "center", rosterCenter, "2026-08-03", "trace-empty")
	if err != nil {
		t.Fatalf("ResolveVaccinationOwner: %v", err)
	}
	if owner.Owner.OwnerSource != domain.OwnerSourceNone || owner.Owner.Reason == nil || *owner.Owner.Reason != domain.OwnerReasonNoHolder {
		t.Fatalf("unexpected owner for an unassigned position: %#v", owner.Owner)
	}
}

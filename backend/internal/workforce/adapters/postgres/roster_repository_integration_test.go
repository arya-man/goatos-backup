package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	rosterTenant       = "00000000-0000-4000-8000-000000000001"
	rosterActor        = "90000000-0000-4000-8000-000000000001"
	rosterCenterScope  = "95000000-0000-4000-8000-000000000001"
	rosterPCMMember    = "96000000-0000-4000-8000-000000000001"
	rosterBackupMember = "96000000-0000-4000-8000-000000000002"
)

func TestRosterPositionAndLeaveCoverageWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint) VALUES
  ($2, $1, 'ROSTER-PCM-01', 'Preventive Care Manager', 'active', 'other'),
  ($3, $1, 'ROSTER-BKP-01', 'Backup Manager', 'active', 'other')`,
		rosterTenant, rosterPCMMember, rosterBackupMember); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	svc := workforceapp.NewRosterService(repo, repo)

	// Create the covered position and its manager-tier backup slot -- the
	// SAME backup_group_code, as the confirmed two-tier design requires.
	backupGroup := "manager_backup"
	pcmPosition, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
		TenantID: rosterTenant, ActorID: rosterActor,
		Body: domain.CreatePositionRequest{
			WorkforceMemberID: rosterPCMMember, ScopeType: "center", ScopeID: rosterCenterScope,
			PositionCode: "preventive_care_manager", PositionTier: "manager", BackupGroupCode: &backupGroup,
		},
	}, "trace-create-pcm")
	if err != nil {
		t.Fatalf("create pcm position: %v", err)
	}
	if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
		TenantID: rosterTenant, ActorID: rosterActor,
		Body: domain.CreatePositionRequest{
			WorkforceMemberID: rosterBackupMember, ScopeType: "center", ScopeID: rosterCenterScope,
			PositionCode: "backup_manager", PositionTier: "manager", IsBackupSlot: true, BackupGroupCode: &backupGroup,
		},
	}, "trace-create-backup"); err != nil {
		t.Fatalf("create backup position: %v", err)
	}

	// The partial unique index rejects a second active holder of the same
	// seat at the same scope.
	if _, err := repo.CreatePosition(ctx, ports.CreatePositionCommand{
		TenantID: rosterTenant, ActorID: rosterActor,
		Body: domain.CreatePositionRequest{
			WorkforceMemberID: rosterBackupMember, ScopeType: "center", ScopeID: rosterCenterScope,
			PositionCode: "preventive_care_manager", PositionTier: "manager",
		},
	}); err != nil {
		t.Fatalf("reassigning the same seat should REPLACE the holder, not error: %v", err)
	}
	holder, err := repo.GetActivePositionByCode(ctx, rosterTenant, "center", rosterCenterScope, "preventive_care_manager", time.Now())
	if err != nil {
		t.Fatalf("GetActivePositionByCode: %v", err)
	}
	if holder.WorkforceMemberID != rosterBackupMember {
		t.Fatalf("seat holder after reassignment = %s, want %s (replace semantics)", holder.WorkforceMemberID, rosterBackupMember)
	}
	// Restore the original holder for the rest of the test.
	if _, err := repo.CreatePosition(ctx, ports.CreatePositionCommand{
		TenantID: rosterTenant, ActorID: rosterActor,
		Body: domain.CreatePositionRequest{
			WorkforceMemberID: rosterPCMMember, ScopeType: "center", ScopeID: rosterCenterScope,
			PositionCode: "preventive_care_manager", PositionTier: "manager", BackupGroupCode: &backupGroup,
		},
	}); err != nil {
		t.Fatalf("restore pcm holder: %v", err)
	}
	_ = pcmPosition

	// Apply and approve leave -- the approval should auto-resolve
	// replacement_member_id to the Backup Manager and grant them a temporary
	// vaccination.execute capability bounded to the leave window.
	applied, err := svc.ApplyLeave(ctx, rosterTenant, rosterActor, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: rosterPCMMember, ScopeType: "center", ScopeID: rosterCenterScope,
		ReasonCode: "personal", StartsOn: "2026-08-03", EndsOn: "2026-08-05",
	}, "trace-apply")
	if err != nil {
		t.Fatalf("ApplyLeave: %v", err)
	}
	if applied.Leave.Status != domain.LeaveStatusReported {
		t.Fatalf("initial leave status = %q, want reported", applied.Leave.Status)
	}

	approved, err := svc.ApproveLeave(ctx, rosterTenant, rosterActor, applied.Leave.AbsenceID, domain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve")
	if err != nil {
		t.Fatalf("ApproveLeave: %v", err)
	}
	if approved.Leave.Status != domain.LeaveStatusApproved {
		t.Fatalf("status = %q, want approved", approved.Leave.Status)
	}
	if approved.Leave.ReplacementMemberID == nil || *approved.Leave.ReplacementMemberID != rosterBackupMember {
		t.Fatalf("replacement_member_id = %v, want %s", approved.Leave.ReplacementMemberID, rosterBackupMember)
	}

	caps, err := repo.ListCapabilities(ctx, rosterTenant, rosterBackupMember)
	if err != nil {
		t.Fatalf("ListCapabilities: %v", err)
	}
	found := false
	for _, c := range caps {
		if c.CapabilityCode == "vaccination.execute" && c.ScopeType == "center" && c.ScopeID == rosterCenterScope && c.Status == "active" {
			found = true
			if c.ValidTo == nil || *c.ValidTo != approved.Leave.EndsAt {
				t.Fatalf("capability valid_to = %v, want the leave's ends_at %q", c.ValidTo, approved.Leave.EndsAt)
			}
		}
	}
	if !found {
		t.Fatal("expected a temporary vaccination.execute capability grant for the backup manager")
	}

	// Vaccination-ownership resolution for a day within the leave window
	// returns the resolved replacement.
	owner, err := svc.ResolveVaccinationOwner(ctx, rosterTenant, rosterActor, "center", rosterCenterScope, "2026-08-04", "trace-owner")
	if err != nil {
		t.Fatalf("ResolveVaccinationOwner: %v", err)
	}
	if owner.Owner.OwnerSource != domain.OwnerSourceReplacement {
		t.Fatalf("owner_source = %q, want replacement", owner.Owner.OwnerSource)
	}
	if owner.Owner.OwnerWorkforceMemberID == nil || *owner.Owner.OwnerWorkforceMemberID != rosterBackupMember {
		t.Fatalf("owner = %v, want %s", owner.Owner.OwnerWorkforceMemberID, rosterBackupMember)
	}
}

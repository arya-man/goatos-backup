package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// TestShedManagerAndBackupResolution covers the shed-wise ownership reads that back the vaccination
// table's Manager/Backup columns: Manager = shed-scoped position holder, Backup = a shed-specific backup
// slot if present else the center Backup Manager slot, and a missing seat = nil (seed gap), not an error.
func TestShedManagerAndBackupResolution(t *testing.T) {
	repo := newFakeRosterRepo()
	svc := NewRosterService(repo, newFakeCapabilityGranter())
	ctx := context.Background()
	at := time.Now()

	shedA := "40000000-0000-4000-8000-00000000000a"
	shedB := "40000000-0000-4000-8000-00000000000b"
	mgrA := "50000000-0000-4000-8000-00000000000a"
	backupCenter := "50000000-0000-4000-8000-0000000000c0"
	backupShedB := "50000000-0000-4000-8000-0000000000b0"

	create := func(member, scopeType, scopeID, code, tier string, isBackup bool, group *string) {
		if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
			TenantID: testTenant, ActorID: testActor,
			Body: domain.CreatePositionRequest{
				WorkforceMemberID: member, ScopeType: scopeType, ScopeID: scopeID,
				PositionCode: code, PositionTier: tier, IsBackupSlot: isBackup, BackupGroupCode: group,
			},
		}, "trace"); err != nil {
			t.Fatalf("create %s: %v", code, err)
		}
	}
	create(mgrA, "shed", shedA, ShedManagerPositionCode, domain.PositionTierManager, false, stringPtr(ManagerBackupGroupCode))
	create(backupCenter, "center", rosterCenter, "backup_manager", domain.PositionTierManager, true, stringPtr(ManagerBackupGroupCode))
	create(backupShedB, "shed", shedB, "shed_backup_manager", domain.PositionTierManager, true, stringPtr(ManagerBackupGroupCode))

	// Manager = the shed-scoped position holder.
	mgr, err := svc.ShedManager(ctx, testTenant, shedA, at)
	if err != nil {
		t.Fatalf("ShedManager A: %v", err)
	}
	if mgr == nil || mgr.WorkforceMemberID != mgrA {
		t.Fatalf("want manager %s, got %+v", mgrA, mgr)
	}

	// Missing shed manager -> nil (seed gap, never an error).
	none, err := svc.ShedManager(ctx, testTenant, shedB, at)
	if err != nil {
		t.Fatalf("ShedManager B: %v", err)
	}
	if none != nil {
		t.Errorf("want nil manager for shed with no seat, got %+v", none)
	}

	// Backup falls back to the center Backup Manager slot when the shed has no shed-specific backup.
	bkp, err := svc.ShedBackup(ctx, testTenant, shedA, rosterCenter, at)
	if err != nil {
		t.Fatalf("ShedBackup A: %v", err)
	}
	if bkp == nil || bkp.WorkforceMemberID != backupCenter {
		t.Fatalf("want center backup %s, got %+v", backupCenter, bkp)
	}

	// A shed-specific backup slot wins over the center fallback.
	bkpB, err := svc.ShedBackup(ctx, testTenant, shedB, rosterCenter, at)
	if err != nil {
		t.Fatalf("ShedBackup B: %v", err)
	}
	if bkpB == nil || bkpB.WorkforceMemberID != backupShedB {
		t.Fatalf("want shed-specific backup %s, got %+v", backupShedB, bkpB)
	}

	// No shed backup and no center -> nil (gap), not an error.
	nb, err := svc.ShedBackup(ctx, testTenant, "40000000-0000-4000-8000-0000000000ff", "", at)
	if err != nil {
		t.Fatalf("ShedBackup none: %v", err)
	}
	if nb != nil {
		t.Errorf("want nil backup, got %+v", nb)
	}
}

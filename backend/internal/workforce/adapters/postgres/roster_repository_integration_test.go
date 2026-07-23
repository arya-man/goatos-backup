package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

// seedPreventiveCareDuty inserts the preventive_care_manager module duty
// (position_module_duties, migration 000157) that RosterRepository.
// ResolveExecuteCapability now reads to decide the vaccination.execute backup
// grant -- the DB-backed replacement for the removed hardcoded
// positionExecuteCapability map. Mirrors what backend/cmd/seed-position-duties
// derives for this seat (pc.vaccination / manage / vaccination.execute).
func seedPreventiveCareDuty(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code)
VALUES ($1::uuid, 'preventive_care_manager', 'pc.vaccination', 'manage', 'vaccination.execute')
ON CONFLICT (tenant_id, position_code, module_code, duty_type, effective_from) DO NOTHING`,
		rosterTenant); err != nil {
		t.Fatalf("seed position_module_duties: %v", err)
	}
}

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
	seedPreventiveCareDuty(t, ctx, pool)

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

// ---- P1 cross-tenant workforce_member_id rejection (real Postgres) ---------

// TestRosterCreatePositionRejectsCrossTenantMemberWithDockerPostgres guards
// against the P1 gap where workforce_positions.workforce_member_id's FK
// (migration 000151) is global, not tenant-scoped: without a service-layer
// check, a caller in rosterTenant could link a DIFFERENT tenant's workforce
// member into a position or leave. Exercises MemberExistsInTenant against a
// real Postgres instance (not the in-memory fake).
func TestRosterCreatePositionRejectsCrossTenantMemberWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		otherTenant       = "00000000-0000-4000-8000-000000000002"
		otherTenantMember = "96000000-0000-4000-8000-000000000098"
	)
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Other Tenant', 'active')`, otherTenant); err != nil {
		t.Fatalf("seed other tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($2, $1, 'OTHER-TENANT-01', 'Other Tenant Member', 'active', 'other')`, otherTenant, otherTenantMember); err != nil {
		t.Fatalf("seed other tenant member: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)

	exists, err := repo.MemberExistsInTenant(ctx, rosterTenant, otherTenantMember)
	if err != nil {
		t.Fatalf("MemberExistsInTenant: %v", err)
	}
	if exists {
		t.Fatal("MemberExistsInTenant must return false for a member belonging to a different tenant")
	}
	exists, err = repo.MemberExistsInTenant(ctx, otherTenant, otherTenantMember)
	if err != nil {
		t.Fatalf("MemberExistsInTenant (own tenant): %v", err)
	}
	if !exists {
		t.Fatal("MemberExistsInTenant must return true for a member's own tenant")
	}

	svc := workforceapp.NewRosterService(repo, repo)
	_, err = svc.CreatePosition(ctx, ports.CreatePositionCommand{
		TenantID: rosterTenant, ActorID: rosterActor,
		Body: domain.CreatePositionRequest{
			WorkforceMemberID: otherTenantMember, ScopeType: "center", ScopeID: rosterCenterScope,
			PositionCode: "preventive_care_manager", PositionTier: "manager",
		},
	}, "trace-cross-tenant-create")
	var appErr *workforceapp.Error
	if !errors.As(err, &appErr) || appErr.Code != "workforce_member_wrong_tenant" {
		t.Fatalf("CreatePosition with a cross-tenant member: err=%v, want app error workforce_member_wrong_tenant", err)
	}

	_, err = svc.ApplyLeave(ctx, rosterTenant, rosterActor, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: otherTenantMember, ScopeType: "center", ScopeID: rosterCenterScope,
		ReasonCode: "personal", StartsOn: "2026-08-03", EndsOn: "2026-08-05",
	}, "trace-cross-tenant-leave")
	if !errors.As(err, &appErr) || appErr.Code != "workforce_member_wrong_tenant" {
		t.Fatalf("ApplyLeave with a cross-tenant member: err=%v, want app error workforce_member_wrong_tenant", err)
	}
}

// ---- Request-level idempotency (repo mandatory write-path contract) --------
//
// These exercise the REAL shared idempotency_keys table (migration 000001) via
// the roster write paths, covering the four mandated cases per endpoint: first
// call, exact replay (original result, NO new side effects), same-key/
// different-payload rejection, and downstream duplicate prevention (no duplicate
// coverage/capability rows on replay of approve + resolve-coverage).

func strptr(s string) *string { return &s }

func countActiveVaccExecCaps(t *testing.T, repo *Repository, tenantID, memberID, scopeID string) int {
	t.Helper()
	caps, err := repo.ListCapabilities(context.Background(), tenantID, memberID)
	if err != nil {
		t.Fatalf("ListCapabilities: %v", err)
	}
	n := 0
	for _, c := range caps {
		if c.CapabilityCode == "vaccination.execute" && c.ScopeType == "center" && c.ScopeID == scopeID && c.Status == "active" {
			n++
		}
	}
	return n
}

func TestRosterCreatePositionIdempotencyWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint) VALUES
  ($2, $1, 'ROSTER-PCM-01', 'Preventive Care Manager', 'active', 'other')`,
		rosterTenant, rosterPCMMember); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}
	repo := NewRepository(pool, 5*time.Second)
	svc := workforceapp.NewRosterService(repo, repo)

	key := "idem-create-001"
	body := domain.CreatePositionRequest{
		WorkforceMemberID: rosterPCMMember, ScopeType: "center", ScopeID: rosterCenterScope,
		PositionCode: "preventive_care_manager", PositionTier: "manager", IdempotencyKey: &key,
	}

	// First call.
	first, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{TenantID: rosterTenant, ActorID: rosterActor, Body: body}, "t1")
	if err != nil {
		t.Fatalf("first CreatePosition: %v", err)
	}

	// Exact replay: same key + same payload returns the ORIGINAL position and
	// inserts no second row.
	replay, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{TenantID: rosterTenant, ActorID: rosterActor, Body: body}, "t2")
	if err != nil {
		t.Fatalf("replay CreatePosition: %v", err)
	}
	if replay.Position.PositionID != first.Position.PositionID {
		t.Fatalf("replay position_id = %s, want original %s", replay.Position.PositionID, first.Position.PositionID)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workforce_positions WHERE tenant_id=$1 AND scope_id=$2 AND position_code=$3`,
		rosterTenant, rosterCenterScope, "preventive_care_manager").Scan(&rows); err != nil {
		t.Fatalf("count positions: %v", err)
	}
	if rows != 1 {
		t.Fatalf("workforce_positions rows after replay = %d, want 1 (no duplicate insert / no spurious replace)", rows)
	}

	// Same key, different payload -> idempotency conflict.
	diff := body
	diff.PositionTier = "head"
	if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{TenantID: rosterTenant, ActorID: rosterActor, Body: diff}, "t3"); err == nil {
		t.Fatal("same-key/different-payload CreatePosition must be rejected, got nil error")
	} else {
		var appErr *workforceapp.Error
		if !errors.As(err, &appErr) || appErr.Code != "idempotency_conflict" {
			t.Fatalf("expected idempotency_conflict app error, got %v", err)
		}
	}
}

// TestRosterUpdatePositionAndProfileWithDockerPostgres exercises the Phase
// Contract-Client write/read additions against real Postgres: in-place attribute
// edit (partial CASE update + optimistic lock + null-clearing), request-level
// idempotency (first / exact replay / same-key-different-payload), the profile
// drawer read, the backup-config upsert (replace semantics on a backup slot),
// and the bulk import (partial success + per-row idempotency).
func TestRosterUpdatePositionAndProfileWithDockerPostgres(t *testing.T) {
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
	seedPreventiveCareDuty(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	svc := workforceapp.NewRosterService(repo, repo)

	backupGroup := "manager_backup"
	created, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
		TenantID: rosterTenant, ActorID: rosterActor,
		Body: domain.CreatePositionRequest{
			WorkforceMemberID: rosterPCMMember, ScopeType: "center", ScopeID: rosterCenterScope,
			PositionCode: "preventive_care_manager", PositionTier: "manager", BackupGroupCode: &backupGroup,
		},
	}, "t-create")
	if err != nil {
		t.Fatalf("CreatePosition: %v", err)
	}
	posID := created.Position.PositionID

	// In-place edit: set a week-off and bump tier; leave backup_group untouched.
	weekOff := "sunday"
	newTier := "head"
	updKey := "idem-update-001"
	updated, err := svc.UpdatePosition(ctx, rosterTenant, rosterActor, posID, domain.UpdatePositionRequest{
		RowVersion: 1, WeekOffWeekday: &weekOff, PositionTier: &newTier, IdempotencyKey: &updKey,
	}, "t-update")
	if err != nil {
		t.Fatalf("UpdatePosition: %v", err)
	}
	if updated.Position.WeekOffWeekday == nil || *updated.Position.WeekOffWeekday != "sunday" {
		t.Fatalf("week_off_weekday = %v, want sunday", updated.Position.WeekOffWeekday)
	}
	if updated.Position.PositionTier != "head" {
		t.Fatalf("position_tier = %q, want head", updated.Position.PositionTier)
	}
	if updated.Position.BackupGroupCode == nil || *updated.Position.BackupGroupCode != backupGroup {
		t.Fatalf("backup_group_code = %v, want unchanged %q", updated.Position.BackupGroupCode, backupGroup)
	}
	operatorCap := 125
	capKey := "idem-update-cap-001"
	capUpdated, err := svc.UpdatePosition(ctx, rosterTenant, rosterActor, posID, domain.UpdatePositionRequest{
		RowVersion: 2, VaccinationDailyAnimalCap: domain.NullInt{Set: true, Value: &operatorCap}, IdempotencyKey: &capKey,
	}, "t-update-cap")
	if err != nil {
		t.Fatalf("UpdatePosition vaccination_daily_animal_cap: %v", err)
	}
	if capUpdated.Position.VaccinationDailyAnimalCap == nil || *capUpdated.Position.VaccinationDailyAnimalCap != operatorCap {
		t.Fatalf("vaccination_daily_animal_cap = %v, want %d", capUpdated.Position.VaccinationDailyAnimalCap, operatorCap)
	}
	capReplay, err := svc.UpdatePosition(ctx, rosterTenant, rosterActor, posID, domain.UpdatePositionRequest{
		RowVersion: 2, VaccinationDailyAnimalCap: domain.NullInt{Set: true, Value: &operatorCap}, IdempotencyKey: &capKey,
	}, "t-update-cap-replay")
	if err != nil {
		t.Fatalf("UpdatePosition cap replay: %v", err)
	}
	if capReplay.Position.RowVersion != capUpdated.Position.RowVersion {
		t.Fatalf("cap replay row_version = %d, want %d", capReplay.Position.RowVersion, capUpdated.Position.RowVersion)
	}
	if updated.Position.RowVersion != 2 {
		t.Fatalf("row_version = %d, want 2", updated.Position.RowVersion)
	}

	capCleared, err := svc.UpdatePosition(ctx, rosterTenant, rosterActor, posID, domain.UpdatePositionRequest{
		RowVersion: capUpdated.Position.RowVersion, VaccinationDailyAnimalCap: domain.NullInt{Set: true},
	}, "t-update-cap-clear")
	if err != nil {
		t.Fatalf("UpdatePosition clear vaccination_daily_animal_cap: %v", err)
	}
	if capCleared.Position.VaccinationDailyAnimalCap != nil {
		t.Fatalf("vaccination_daily_animal_cap = %v, want nil after clear", capCleared.Position.VaccinationDailyAnimalCap)
	}

	// Exact idempotent replay returns the original without a second bump.
	replay, err := svc.UpdatePosition(ctx, rosterTenant, rosterActor, posID, domain.UpdatePositionRequest{
		RowVersion: 1, WeekOffWeekday: &weekOff, PositionTier: &newTier, IdempotencyKey: &updKey,
	}, "t-update-replay")
	if err != nil {
		t.Fatalf("UpdatePosition replay: %v", err)
	}
	if replay.Position.RowVersion != 2 {
		t.Fatalf("replay row_version = %d, want 2 (no second update)", replay.Position.RowVersion)
	}
	// The replay must return the ORIGINAL response body, not a fresh read of the record's CURRENT
	// state: two unrelated edits (the cap set + the cap clear) landed between the first call and this
	// replay, and their effects must not leak out under this key (BUG-037).
	if replay.Position.PositionTier != "head" || replay.Position.WeekOffWeekday == nil || *replay.Position.WeekOffWeekday != "sunday" {
		t.Fatalf("replay body = tier %q week_off %v, want the original head/sunday",
			replay.Position.PositionTier, replay.Position.WeekOffWeekday)
	}
	if replay.Position.VaccinationDailyAnimalCap != nil {
		t.Fatalf("replay vaccination_daily_animal_cap = %v, want the original nil (later edits must not leak into a replay)",
			replay.Position.VaccinationDailyAnimalCap)
	}
	// ...and the replay must not have written anything: current state is still the post-clear v4.
	if current, err := repo.GetPositionByID(ctx, rosterTenant, posID); err != nil {
		t.Fatalf("GetPositionByID after replay: %v", err)
	} else if current.RowVersion != capCleared.Position.RowVersion {
		t.Fatalf("stored row_version after replay = %d, want unchanged %d (no side effects on replay)",
			current.RowVersion, capCleared.Position.RowVersion)
	}

	// Same key, different payload -> idempotency conflict.
	otherTier := "director"
	if _, err := svc.UpdatePosition(ctx, rosterTenant, rosterActor, posID, domain.UpdatePositionRequest{
		RowVersion: 1, PositionTier: &otherTier, IdempotencyKey: &updKey,
	}, "t-update-conflict"); err == nil {
		t.Fatal("same-key/different-payload UpdatePosition must be rejected")
	} else {
		var appErr *workforceapp.Error
		if !errors.As(err, &appErr) || appErr.Code != "idempotency_conflict" {
			t.Fatalf("expected idempotency_conflict, got %v", err)
		}
	}

	// Stale row_version (no idempotency key) -> write conflict.
	if _, err := svc.UpdatePosition(ctx, rosterTenant, rosterActor, posID, domain.UpdatePositionRequest{
		RowVersion: 1, PositionTier: &otherTier,
	}, "t-update-stale"); err == nil {
		t.Fatal("stale row_version UpdatePosition must be rejected")
	} else {
		var appErr *workforceapp.Error
		if !errors.As(err, &appErr) || appErr.Code != "write_conflict" {
			t.Fatalf("expected write_conflict, got %v", err)
		}
	}

	// Clear the week-off (non-nil empty pointer) at the current row_version.
	clear := ""
	cleared, err := svc.UpdatePosition(ctx, rosterTenant, rosterActor, posID, domain.UpdatePositionRequest{
		RowVersion: capCleared.Position.RowVersion, WeekOffWeekday: &clear,
	}, "t-update-clear")
	if err != nil {
		t.Fatalf("UpdatePosition clear: %v", err)
	}
	if cleared.Position.WeekOffWeekday != nil {
		t.Fatalf("week_off_weekday = %v, want nil after clear", cleared.Position.WeekOffWeekday)
	}

	// Profile drawer read: enriched seat + duties, no active coverage.
	profile, err := svc.GetPositionProfile(ctx, rosterTenant, posID, "t-profile")
	if err != nil {
		t.Fatalf("GetPositionProfile: %v", err)
	}
	if profile.Profile.Position.PositionID != posID {
		t.Fatalf("profile position_id = %s, want %s", profile.Profile.Position.PositionID, posID)
	}
	if len(profile.Profile.Position.Duties) == 0 {
		t.Fatal("expected enriched duties on the profile (pc.vaccination)")
	}
	if profile.Profile.ActiveCoverage != nil {
		t.Fatalf("active_coverage = %v, want nil (holder present)", profile.Profile.ActiveCoverage)
	}

	// Backup-config upsert creates a backup-slot seat; replace semantics move
	// the holder on a second upsert.
	bc, err := svc.UpsertBackupConfig(ctx, rosterTenant, rosterActor, domain.UpsertBackupConfigRequest{
		WorkforceMemberID: rosterBackupMember, ScopeType: "center", ScopeID: rosterCenterScope,
		BackupGroupCode: backupGroup, BackupPositionCode: "backup_manager", PositionTier: "manager",
	}, "t-backup")
	if err != nil {
		t.Fatalf("UpsertBackupConfig: %v", err)
	}
	if !bc.Position.IsBackupSlot {
		t.Fatal("upserted backup config must be an is_backup_slot seat")
	}

	// Bulk import: one valid new seat + one invalid row (bad scope) -> partial
	// success with a per-row error.
	imp, err := svc.ImportPositions(ctx, rosterTenant, rosterActor, domain.ImportPositionsRequest{
		Rows: []domain.CreatePositionRequest{
			{WorkforceMemberID: rosterPCMMember, ScopeType: "center", ScopeID: rosterCenterScope, PositionCode: "health_kidding_manager", PositionTier: "manager"},
			{WorkforceMemberID: rosterPCMMember, ScopeType: "bogus", ScopeID: rosterCenterScope, PositionCode: "cleaning_am1", PositionTier: "assistant"},
		},
	}, "t-import")
	if err != nil {
		t.Fatalf("ImportPositions: %v", err)
	}
	if imp.Imported != 1 || imp.Failed != 1 {
		t.Fatalf("import imported=%d failed=%d, want 1/1", imp.Imported, imp.Failed)
	}
	if imp.Results[1].Status != "error" || imp.Results[1].ErrorCode == nil {
		t.Fatalf("expected row 1 to be an error with a code, got %+v", imp.Results[1])
	}
}

func TestRosterApplyLeaveIdempotencyWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint) VALUES
  ($2, $1, 'ROSTER-PCM-01', 'Preventive Care Manager', 'active', 'other')`,
		rosterTenant, rosterPCMMember); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}
	repo := NewRepository(pool, 5*time.Second)
	svc := workforceapp.NewRosterService(repo, repo)

	key := "idem-apply-001"
	body := domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: rosterPCMMember, ScopeType: "center", ScopeID: rosterCenterScope,
		ReasonCode: "personal", StartsOn: "2026-08-03", EndsOn: "2026-08-05", IdempotencyKey: &key,
	}

	first, err := svc.ApplyLeave(ctx, rosterTenant, rosterActor, body, "t1")
	if err != nil {
		t.Fatalf("first ApplyLeave: %v", err)
	}
	replay, err := svc.ApplyLeave(ctx, rosterTenant, rosterActor, body, "t2")
	if err != nil {
		t.Fatalf("replay ApplyLeave: %v", err)
	}
	if replay.Leave.AbsenceID != first.Leave.AbsenceID {
		t.Fatalf("replay absence_id = %s, want original %s", replay.Leave.AbsenceID, first.Leave.AbsenceID)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workforce_absences WHERE tenant_id=$1 AND workforce_member_id=$2`,
		rosterTenant, rosterPCMMember).Scan(&rows); err != nil {
		t.Fatalf("count absences: %v", err)
	}
	if rows != 1 {
		t.Fatalf("workforce_absences rows after replay = %d, want 1 (no duplicate leave)", rows)
	}

	diff := body
	diff.EndsOn = "2026-08-06"
	if _, err := svc.ApplyLeave(ctx, rosterTenant, rosterActor, diff, "t3"); err == nil {
		t.Fatal("same-key/different-payload ApplyLeave must be rejected, got nil error")
	} else {
		var appErr *workforceapp.Error
		if !errors.As(err, &appErr) || appErr.Code != "idempotency_conflict" {
			t.Fatalf("expected idempotency_conflict app error, got %v", err)
		}
	}
}

func TestRosterApproveAndResolveCoverageIdempotencyWithDockerPostgres(t *testing.T) {
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
	seedPreventiveCareDuty(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	svc := workforceapp.NewRosterService(repo, repo)

	backupGroup := "manager_backup"
	if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{TenantID: rosterTenant, ActorID: rosterActor,
		Body: domain.CreatePositionRequest{WorkforceMemberID: rosterPCMMember, ScopeType: "center", ScopeID: rosterCenterScope,
			PositionCode: "preventive_care_manager", PositionTier: "manager", BackupGroupCode: &backupGroup}}, "c1"); err != nil {
		t.Fatalf("create pcm position: %v", err)
	}
	if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{TenantID: rosterTenant, ActorID: rosterActor,
		Body: domain.CreatePositionRequest{WorkforceMemberID: rosterBackupMember, ScopeType: "center", ScopeID: rosterCenterScope,
			PositionCode: "backup_manager", PositionTier: "manager", IsBackupSlot: true, BackupGroupCode: &backupGroup}}, "c2"); err != nil {
		t.Fatalf("create backup position: %v", err)
	}

	applied, err := svc.ApplyLeave(ctx, rosterTenant, rosterActor, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: rosterPCMMember, ScopeType: "center", ScopeID: rosterCenterScope,
		ReasonCode: "personal", StartsOn: "2026-08-03", EndsOn: "2026-08-05",
	}, "apply")
	if err != nil {
		t.Fatalf("ApplyLeave: %v", err)
	}
	absenceID := applied.Leave.AbsenceID

	// ---- Approve: first call auto-resolves the backup + grants ONE capability.
	approveKey := "idem-approve-001"
	approved, err := svc.ApproveLeave(ctx, rosterTenant, rosterActor, absenceID,
		domain.ApproveStaffLeaveRequest{RowVersion: 1, IdempotencyKey: &approveKey}, "approve-1")
	if err != nil {
		t.Fatalf("first ApproveLeave: %v", err)
	}
	if approved.Leave.Status != domain.LeaveStatusApproved ||
		approved.Leave.ReplacementMemberID == nil || *approved.Leave.ReplacementMemberID != rosterBackupMember {
		t.Fatalf("first approve: status=%q replacement=%v", approved.Leave.Status, approved.Leave.ReplacementMemberID)
	}
	if n := countActiveVaccExecCaps(t, repo, rosterTenant, rosterBackupMember, rosterCenterScope); n != 1 {
		t.Fatalf("capability rows after first approve = %d, want 1", n)
	}
	rvAfterApprove := approved.Leave.RowVersion

	// ---- Exact replay of approve: returns the ORIGINAL resolved leave, runs NO
	// side effects (no status re-write, no duplicate capability). Note: without
	// idempotency this same-row_version replay would fail optimistic-concurrency.
	replayApprove, err := svc.ApproveLeave(ctx, rosterTenant, rosterActor, absenceID,
		domain.ApproveStaffLeaveRequest{RowVersion: 1, IdempotencyKey: &approveKey}, "approve-2")
	if err != nil {
		t.Fatalf("replay ApproveLeave: %v", err)
	}
	if replayApprove.Leave.RowVersion != rvAfterApprove {
		t.Fatalf("replay approve row_version = %d, want unchanged %d (no re-run)", replayApprove.Leave.RowVersion, rvAfterApprove)
	}
	if n := countActiveVaccExecCaps(t, repo, rosterTenant, rosterBackupMember, rosterCenterScope); n != 1 {
		t.Fatalf("capability rows after approve replay = %d, want 1 (downstream duplicate prevention)", n)
	}

	// ---- Same approve key, different payload (row_version) -> conflict.
	if _, err := svc.ApproveLeave(ctx, rosterTenant, rosterActor, absenceID,
		domain.ApproveStaffLeaveRequest{RowVersion: 2, IdempotencyKey: &approveKey}, "approve-3"); err == nil {
		t.Fatal("same-key/different-payload ApproveLeave must be rejected, got nil error")
	} else {
		var appErr *workforceapp.Error
		if !errors.As(err, &appErr) || appErr.Code != "idempotency_conflict" {
			t.Fatalf("expected idempotency_conflict, got %v", err)
		}
	}

	// ---- Explicit resolve-coverage (CEO override to the SAME backup): first
	// call, then exact replay must not add a duplicate capability nor re-write.
	resolveKey := "idem-resolve-001"
	resolved, err := svc.ResolveLeaveCoverage(ctx, rosterTenant, rosterActor, absenceID,
		domain.ResolveLeaveCoverageRequest{ReplacementMemberID: strptr(rosterBackupMember), IdempotencyKey: &resolveKey}, "resolve-1")
	if err != nil {
		t.Fatalf("first ResolveLeaveCoverage: %v", err)
	}
	if resolved.Leave.ReplacementMemberID == nil || *resolved.Leave.ReplacementMemberID != rosterBackupMember {
		t.Fatalf("resolve replacement = %v, want %s", resolved.Leave.ReplacementMemberID, rosterBackupMember)
	}
	rvAfterResolve := resolved.Leave.RowVersion
	capsAfterResolve := countActiveVaccExecCaps(t, repo, rosterTenant, rosterBackupMember, rosterCenterScope)

	replayResolve, err := svc.ResolveLeaveCoverage(ctx, rosterTenant, rosterActor, absenceID,
		domain.ResolveLeaveCoverageRequest{ReplacementMemberID: strptr(rosterBackupMember), IdempotencyKey: &resolveKey}, "resolve-2")
	if err != nil {
		t.Fatalf("replay ResolveLeaveCoverage: %v", err)
	}
	if replayResolve.Leave.RowVersion != rvAfterResolve {
		t.Fatalf("replay resolve row_version = %d, want unchanged %d (no re-run)", replayResolve.Leave.RowVersion, rvAfterResolve)
	}
	if n := countActiveVaccExecCaps(t, repo, rosterTenant, rosterBackupMember, rosterCenterScope); n != capsAfterResolve {
		t.Fatalf("capability rows after resolve replay = %d, want unchanged %d (downstream duplicate prevention)", n, capsAfterResolve)
	}

	// ---- Same resolve key, different payload (escalate) -> conflict.
	if _, err := svc.ResolveLeaveCoverage(ctx, rosterTenant, rosterActor, absenceID,
		domain.ResolveLeaveCoverageRequest{ReplacementMemberID: strptr(""), IdempotencyKey: &resolveKey}, "resolve-3"); err == nil {
		t.Fatal("same-key/different-payload ResolveLeaveCoverage must be rejected, got nil error")
	} else {
		var appErr *workforceapp.Error
		if !errors.As(err, &appErr) || appErr.Code != "idempotency_conflict" {
			t.Fatalf("expected idempotency_conflict, got %v", err)
		}
	}
}

// TestUpdatePositionEnqueuesVaccinationOperatorCascade reproduces a real gap found on the CPT
// operator-drive validation: editing vaccination_daily_animal_cap or week_off_weekday directly on a
// vaccination_operator_* workforce_positions seat (the admin HRMS "Config" screen's real write
// path, PATCH /admin/roster/positions/{id}) silently updated the seat with zero cascade -- unlike
// UpsertOperatorAssignmentConfig (N/default-operator) and ApplyLeave, which both already enqueue
// vaccination.capacity.changed/vaccination.roster.changed. A non-vaccination position edit (e.g.
// preventive_care_manager, exercised earlier in this file) must NOT enqueue either event.
func TestUpdatePositionEnqueuesVaccinationOperatorCascade(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const tenantID = rosterTenant
	const parkID = "00000000-0000-4000-8000-0000000000c2"
	const memberID = "00000000-0000-4000-8000-0000000000c3"
	const actorID = rosterActor

	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'PARK-CASCADE', 'Cascade Park', 'active')
ON CONFLICT (location_id) DO NOTHING`, parkID, tenantID); err != nil {
		t.Fatalf("seed park location: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, 'OP-CASCADE-01', 'Cascade Operator', 'active', 'operator')`, memberID, tenantID); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}
	repo := NewRepository(pool, 5*time.Second)
	svc := workforceapp.NewRosterService(repo, repo)

	created, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
		TenantID: tenantID, ActorID: actorID,
		Body: domain.CreatePositionRequest{
			WorkforceMemberID: memberID, ScopeType: "center", ScopeID: parkID,
			PositionCode: "vaccination_operator_cascadetest", PositionTier: "manager",
		},
	}, "t-cascade-create")
	if err != nil {
		t.Fatalf("CreatePosition: %v", err)
	}
	posID := created.Position.PositionID

	countOutbox := func(eventType string) int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type=$2`, tenantID, eventType).Scan(&n); err != nil {
			t.Fatalf("count outbox %s: %v", eventType, err)
		}
		return n
	}

	if n := countOutbox("vaccination.capacity.changed") + countOutbox("vaccination.roster.changed"); n != 0 {
		t.Fatalf("cascade outbox rows before any edit = %d, want 0", n)
	}

	cap := 150
	if _, err := svc.UpdatePosition(ctx, tenantID, actorID, posID, domain.UpdatePositionRequest{
		RowVersion: 1, VaccinationDailyAnimalCap: domain.NullInt{Set: true, Value: &cap},
	}, "t-cascade-cap"); err != nil {
		t.Fatalf("UpdatePosition cap: %v", err)
	}
	if n := countOutbox("vaccination.capacity.changed"); n != 1 {
		t.Fatalf("vaccination.capacity.changed outbox rows after cap edit = %d, want 1 (editing a vaccination_operator_* seat's cap must enqueue the same event UpsertOperatorAssignmentConfig uses)", n)
	}

	weekOff := "sunday"
	if _, err := svc.UpdatePosition(ctx, tenantID, actorID, posID, domain.UpdatePositionRequest{
		RowVersion: 2, WeekOffWeekday: &weekOff,
	}, "t-cascade-weekoff"); err != nil {
		t.Fatalf("UpdatePosition week-off: %v", err)
	}
	if n := countOutbox("vaccination.roster.changed"); n != 1 {
		t.Fatalf("vaccination.roster.changed outbox rows after week-off edit = %d, want 1", n)
	}

	// A non-vaccination position (already created above in the sibling test as
	// preventive_care_manager) must never enqueue either cascade event on the same edit shape.
	pcCreated, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{
		TenantID: tenantID, ActorID: actorID,
		Body: domain.CreatePositionRequest{
			WorkforceMemberID: memberID, ScopeType: "center", ScopeID: parkID,
			PositionCode: "goats_head", PositionTier: "head",
		},
	}, "t-cascade-noop-create")
	if err != nil {
		t.Fatalf("CreatePosition non-vaccination: %v", err)
	}
	if _, err := svc.UpdatePosition(ctx, tenantID, actorID, pcCreated.Position.PositionID, domain.UpdatePositionRequest{
		RowVersion: 1, WeekOffWeekday: &weekOff,
	}, "t-cascade-noop-update"); err != nil {
		t.Fatalf("UpdatePosition non-vaccination: %v", err)
	}
	if n := countOutbox("vaccination.capacity.changed") + countOutbox("vaccination.roster.changed"); n != 2 {
		t.Fatalf("cascade outbox rows after editing a non-vaccination position = %d, want unchanged 2 (non-vaccination position edits must not enqueue either cascade event)", n)
	}
}

// TestLeaveCascadeEmitsOnEffectiveTransitions reproduces the leave-cascade timing defect.
//
// ApplyLeave enqueues vaccination.leave.changed while the absence is still status='reported',
// but the scheduler's availability predicate
// (obligation/adapters/postgres/visit_shot_lock.go:586 and :694) only EXCLUDES an operator whose
// absence status IN ('approved','escalation_required'). So the event fires when nothing has
// changed for planning, and the transitions that DO change availability -- approval
// (reported -> approved) and coverage resolution/escalation -- emit nothing at all. A
// park/center-scoped operator leave (the shape vaccination_operator_* seats use) emits nothing
// even on apply, because ApplyLeave only enqueues for scope_type='shed'.
func TestLeaveCascadeEmitsOnEffectiveTransitions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const tenantID = rosterTenant
	const parkID = "00000000-0000-4000-8000-0000000000d2"
	const operatorMember = "00000000-0000-4000-8000-0000000000d3"
	const backupMember = "00000000-0000-4000-8000-0000000000d4"
	const actorID = rosterActor

	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'PARK-LEAVE-CASCADE', 'Leave Cascade Park', 'active')
ON CONFLICT (location_id) DO NOTHING`, parkID, tenantID); err != nil {
		t.Fatalf("seed park location: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint) VALUES
  ($2, $1, 'OP-LEAVE-01', 'Leave Cascade Operator', 'active', 'operator'),
  ($3, $1, 'OP-LEAVE-BKP', 'Leave Cascade Backup', 'active', 'operator')`,
		tenantID, operatorMember, backupMember); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	svc := workforceapp.NewRosterService(repo, repo)

	backupGroup := "operator_backup"
	if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{TenantID: tenantID, ActorID: actorID,
		Body: domain.CreatePositionRequest{WorkforceMemberID: operatorMember, ScopeType: "center", ScopeID: parkID,
			PositionCode: "vaccination_operator_leavetest", PositionTier: "manager", BackupGroupCode: &backupGroup}},
		"lc-create-op"); err != nil {
		t.Fatalf("CreatePosition operator: %v", err)
	}
	if _, err := svc.CreatePosition(ctx, ports.CreatePositionCommand{TenantID: tenantID, ActorID: actorID,
		Body: domain.CreatePositionRequest{WorkforceMemberID: backupMember, ScopeType: "center", ScopeID: parkID,
			PositionCode: "backup_manager", PositionTier: "manager", IsBackupSlot: true, BackupGroupCode: &backupGroup}},
		"lc-create-backup"); err != nil {
		t.Fatalf("CreatePosition backup: %v", err)
	}

	countLeaveEventsBy := func(producer string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'vaccination.leave.changed'
  AND headers->>'producer' = $2`, tenantID, producer).Scan(&n); err != nil {
			t.Fatalf("count leave.changed by %s: %v", producer, err)
		}
		return n
	}

	applied, err := svc.ApplyLeave(ctx, tenantID, actorID, domain.ApplyStaffLeaveRequest{
		WorkforceMemberID: operatorMember, ScopeType: "center", ScopeID: parkID,
		ReasonCode: "personal", StartsOn: "2026-08-03", EndsOn: "2026-08-05",
	}, "lc-apply")
	if err != nil {
		t.Fatalf("ApplyLeave: %v", err)
	}
	absenceID := applied.Leave.AbsenceID
	if applied.Leave.Status != domain.LeaveStatusReported {
		t.Fatalf("applied leave status = %q, want reported", applied.Leave.Status)
	}

	// The APPROVAL is the transition that actually changes scheduler availability
	// (status becomes 'approved', which the availability predicate excludes).
	approveKey := "idem-leave-cascade-approve"
	approved, err := svc.ApproveLeave(ctx, tenantID, actorID, absenceID,
		domain.ApproveStaffLeaveRequest{RowVersion: 1, IdempotencyKey: &approveKey}, "lc-approve")
	if err != nil {
		t.Fatalf("ApproveLeave: %v", err)
	}
	if approved.Leave.Status != domain.LeaveStatusApproved {
		t.Fatalf("approved leave status = %q, want approved", approved.Leave.Status)
	}
	if n := countLeaveEventsBy("workforce.ApproveLeave"); n != 1 {
		t.Fatalf("vaccination.leave.changed rows produced by the APPROVAL transition = %d, want 1 "+
			"(approval is the moment the scheduler starts excluding this operator; without an event the "+
			"already-planned future drives are never re-planned)", n)
	}

	// The approval-produced event must carry the park scope the replan consumer routes on.
	var parkFromPayload, scopeType, scopeID, aggregateType string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(payload#>>'{payload,park_id}', ''), COALESCE(payload#>>'{payload,scope_type}', ''),
       COALESCE(payload#>>'{payload,scope_id}', ''), aggregate_type
FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'vaccination.leave.changed'
  AND headers->>'producer' = 'workforce.ApproveLeave'`, tenantID).
		Scan(&parkFromPayload, &scopeType, &scopeID, &aggregateType); err != nil {
		t.Fatalf("read approval-produced envelope: %v", err)
	}
	if parkFromPayload != parkID || scopeType != "center" || scopeID != parkID {
		t.Fatalf("approval envelope payload park_id=%q scope_type=%q scope_id=%q, want %q/center/%q",
			parkFromPayload, scopeType, scopeID, parkID, parkID)
	}
	if aggregateType != "absence" {
		t.Fatalf("approval envelope aggregate_type = %q, want absence", aggregateType)
	}

	// Exact idempotent replay of the approval must NOT enqueue a second event.
	if _, err := svc.ApproveLeave(ctx, tenantID, actorID, absenceID,
		domain.ApproveStaffLeaveRequest{RowVersion: 1, IdempotencyKey: &approveKey}, "lc-approve-replay"); err != nil {
		t.Fatalf("replay ApproveLeave: %v", err)
	}
	if n := countLeaveEventsBy("workforce.ApproveLeave"); n != 1 {
		t.Fatalf("vaccination.leave.changed rows after approve replay = %d, want 1 (idempotent)", n)
	}

	// Coverage resolution / escalation also changes who actually covers the park,
	// so it must cascade too.
	beforeResolve := countLeaveEventsBy("workforce.ResolveLeaveCoverage")
	resolveKey := "idem-leave-cascade-resolve"
	if _, err := svc.ResolveLeaveCoverage(ctx, tenantID, actorID, absenceID,
		domain.ResolveLeaveCoverageRequest{ReplacementMemberID: strptr(""), IdempotencyKey: &resolveKey}, "lc-resolve"); err != nil {
		t.Fatalf("ResolveLeaveCoverage (escalate): %v", err)
	}
	if n := countLeaveEventsBy("workforce.ResolveLeaveCoverage"); n != beforeResolve+1 {
		t.Fatalf("vaccination.leave.changed rows produced by coverage resolution = %d, want %d", n, beforeResolve+1)
	}
	if _, err := svc.ResolveLeaveCoverage(ctx, tenantID, actorID, absenceID,
		domain.ResolveLeaveCoverageRequest{ReplacementMemberID: strptr(""), IdempotencyKey: &resolveKey}, "lc-resolve-replay"); err != nil {
		t.Fatalf("replay ResolveLeaveCoverage: %v", err)
	}
	if n := countLeaveEventsBy("workforce.ResolveLeaveCoverage"); n != beforeResolve+1 {
		t.Fatalf("vaccination.leave.changed rows after resolve replay = %d, want %d (idempotent)", n, beforeResolve+1)
	}
}

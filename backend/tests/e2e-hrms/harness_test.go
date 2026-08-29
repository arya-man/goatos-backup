package e2ehrms

// Run the whole HRMS kernel-story suite with:
//
//	go test ./backend/tests/e2e-hrms/... -run TestHRMS -v
//
// or via the helper script `backend/tests/e2e-hrms/run.sh` (same command, plus it prints the HTML
// report path on success). Each story boots its own throwaway Postgres container via
// backend/internal/platform/pgtest (Docker required; tests call pgtest.SkipIfNoDocker and skip
// cleanly when Docker is unavailable) and renders its outcome into
// backend/tests/e2e-hrms/report/index.html via TestMain in main_test.go.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
	workforceports "github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// Baseline fixture ids: every migrated database already has these rows (see migration
// 000001_phase_1_identity_foundation.sql), the same way the rest of the backend's integration
// tests reuse them (e.g. internal/obligation/adapters/postgres/cancel_integration_test.go's
// tenantID/meshaParty/cbePark constants). Reusing them means the harness never has to create a
// tenant/party/park from scratch.
const (
	fxTenant = "00000000-0000-4000-8000-000000000001" // baseline tenant
	fxParty  = "00000000-0000-4000-8000-000000001001" // baseline custodian party (Mesha)
	fxPark   = "00000000-0000-4000-8000-000000003001" // baseline park location (CBE, Coimbatore)
)

// Fixture bundles an ephemeral pool with the roster repository and service, plus small seed
// helpers shared by all HRMS kernel stories.
type Fixture struct {
	T    *testing.T
	Ctx  context.Context
	Pool *pgxpool.Pool
	Repo *workforcepg.Repository // Explicitly typed repository for all interface methods
	Svc  *workforceapp.RosterService
}

// NewFixture boots a fresh throwaway Postgres container (all committed migrations applied) and
// wires the roster service. The container is removed via t.Cleanup.
func NewFixture(t *testing.T) *Fixture {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)

	const timeout = 5 * time.Second
	repo := workforcepg.NewRepository(pool, timeout)
	return &Fixture{
		T:    t,
		Ctx:  ctx,
		Pool: pool,
		Repo: repo,
		Svc:  workforceapp.NewRosterService(repo, repo), // repo satisfies both RosterRepository and CapabilityGranter
	}
}

func (f *Fixture) exec(label, sql string, args ...any) {
	f.T.Helper()
	if _, err := f.Pool.Exec(f.Ctx, sql, args...); err != nil {
		f.T.Fatalf("seed %s: %v", label, err)
	}
}

func (f *Fixture) scanText(sql string, args ...any) string {
	f.T.Helper()
	var v string
	if err := f.Pool.QueryRow(f.Ctx, sql, args...).Scan(&v); err != nil {
		f.T.Fatalf("scan text (%s): %v", sql, err)
	}
	return v
}

// SeedWorkforceMembers creates test workforce members for the roster stories.
func (f *Fixture) SeedWorkforceMembers(memberIDs map[string]string) {
	f.T.Helper()
	for id, name := range memberIDs {
		code := "HRMS-" + name
		f.exec("workforce member "+id,
			`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
			 VALUES ($1, $2, $3, $4, 'active', 'other')`,
			id, fxTenant, code, name)
	}
}

// SeedPositionModuleDuties seeds the position_module_duties row for preventive_care_manager
// so ResolveExecuteCapability works correctly in the tests (design doc S4.6).
func (f *Fixture) SeedPositionModuleDuties() {
	f.T.Helper()
	f.exec("position_module_duties preventive_care_manager",
		`INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code)
		 VALUES ($1::uuid, 'preventive_care_manager', 'pc.vaccination', 'manage', 'vaccination.execute')
		 ON CONFLICT (tenant_id, position_code, module_code, duty_type, effective_from) DO NOTHING`,
		fxTenant)
}

// TestHRMSKernelStory_ManagerLeaveWithBackupCoverage exercises the core roster coverage engine:
// seed a Preventive Care Manager position + Backup Manager backup slot, apply manager leave,
// approve it to auto-resolve the backup as replacement, then verify ResolveVaccinationOwner
// returns the backup as effective owner. Then make the backup also unavailable to verify
// escalation.
func TestHRMSKernelStory_ManagerLeaveWithBackupCoverage(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-hrms-1", "Manager leave with backup coverage and escalation",
		"A Preventive Care Manager at a shed takes leave with an available Backup Manager. The approval "+
			"auto-resolves the backup as replacement and grants them a temporary vaccination.execute capability. "+
			"ResolveVaccinationOwner correctly returns the backup as effective owner on the leave dates. "+
			"When the backup also becomes unavailable, ResolveVaccinationOwner escalates to the park head.")
	defer story.Finish()

	// ---- Setup: seed members, positions, and module duties ----

	const (
		managerID  = "91000000-0000-4000-8000-000000000001"
		backupID   = "91000000-0000-4000-8000-000000000002"
		parkHeadID = "91000000-0000-4000-8000-000000000003"
		shedID     = "92000000-0000-4000-8000-000000000001"
		centerID   = fxPark
		actorID    = "90000000-0000-4000-8000-000000000001"
	)

	story.Step("Seed workforce members and position module duties",
		"Create three workforce members (Manager, Backup Manager, Park Head) and seed the position_module_duties "+
			"for preventive_care_manager so ResolveExecuteCapability reads the vaccination.execute capability.")

	fx.SeedWorkforceMembers(map[string]string{
		managerID:  "Manager",
		backupID:   "Backup Manager",
		parkHeadID: "Park Head",
	})
	fx.SeedPositionModuleDuties()
	story.Assert("workforce members seeded successfully", true, "3 members created")

	// ---- Create positions ----

	story.Step("Create the Preventive Care Manager position",
		"Create a fixed position for the manager at the shed scope with backup_group_code='manager_backup' "+
			"so the roster engine can resolve coverage.")

	backupGroup := "manager_backup"
	createPCMCmd := workforceports.CreatePositionCommand{
		TenantID: fxTenant,
		ActorID:  actorID,
		Body: workforcedomain.CreatePositionRequest{
			WorkforceMemberID: managerID,
			ScopeType:         "shed",
			ScopeID:           shedID,
			PositionCode:      "preventive_care_manager",
			PositionTier:      "manager",
			BackupGroupCode:   &backupGroup,
		},
	}
	pcmResp, err := fx.Svc.CreatePosition(fx.Ctx, createPCMCmd, "trace-create-manager")
	story.Assert("CreatePosition for manager succeeded", err == nil, "err=%v", err)
	story.Assert("manager position_code is preventive_care_manager", pcmResp.Position.PositionCode == "preventive_care_manager", "code=%q", pcmResp.Position.PositionCode)
	managerPositionID := pcmResp.Position.PositionID

	story.Step("Create the Backup Manager backup slot",
		"Create a backup-slot position for the backup manager at the same shed scope with the same "+
			"backup_group_code so the roster engine resolves them as coverage pair.")

	createBackupCmd := workforceports.CreatePositionCommand{
		TenantID: fxTenant,
		ActorID:  actorID,
		Body: workforcedomain.CreatePositionRequest{
			WorkforceMemberID: backupID,
			ScopeType:         "shed",
			ScopeID:           shedID,
			PositionCode:      "backup_manager",
			PositionTier:      "manager",
			IsBackupSlot:      true,
			BackupGroupCode:   &backupGroup,
		},
	}
	backupResp, err := fx.Svc.CreatePosition(fx.Ctx, createBackupCmd, "trace-create-backup")
	story.Assert("CreatePosition for backup succeeded", err == nil, "err=%v", err)
	story.Assert("backup is_backup_slot=true", backupResp.Position.IsBackupSlot == true, "is_backup_slot=%v", backupResp.Position.IsBackupSlot)

	// Also seed the park head at the park scope so escalation can resolve them
	story.Step("Create the Park Head position",
		"Create a park-scoped park_head position so that escalation can resolve the park head as escalation target.")

	parkHeadTier := "head"
	createParkHeadCmd := workforceports.CreatePositionCommand{
		TenantID: fxTenant,
		ActorID:  actorID,
		Body: workforcedomain.CreatePositionRequest{
			WorkforceMemberID: parkHeadID,
			ScopeType:         "shed",
			ScopeID:           shedID,
			PositionCode:      "park_head",
			PositionTier:      parkHeadTier,
		},
	}
	_, err = fx.Svc.CreatePosition(fx.Ctx, createParkHeadCmd, "trace-create-parkhead")
	story.Assert("CreatePosition for park head succeeded", err == nil, "err=%v", err)

	// ---- Leave workflow ----

	story.Step("Apply leave for the manager",
		"The manager requests leave from 2026-08-03 to 2026-08-05 (3 days) with reason 'personal'.")

	leaveStart := "2026-08-03"
	leaveEnd := "2026-08-05"
	applyResp, err := fx.Svc.ApplyLeave(fx.Ctx, fxTenant, actorID, workforcedomain.ApplyStaffLeaveRequest{
		WorkforceMemberID: managerID,
		ScopeType:         "shed",
		ScopeID:           shedID,
		ReasonCode:        "personal",
		StartsOn:          leaveStart,
		EndsOn:            leaveEnd,
	}, "trace-apply-leave")
	story.Assert("ApplyLeave succeeded", err == nil, "err=%v", err)
	story.Assert("initial status is reported", applyResp.Leave.Status == workforcedomain.LeaveStatusReported,
		"status=%q", applyResp.Leave.Status)
	absenceID := applyResp.Leave.AbsenceID

	story.Step("Approve the leave (auto-resolve coverage)",
		"Approve the manager's leave. The roster service's resolveLeaveCoverage engine automatically resolves "+
			"the backup manager as replacement_member_id (design doc S4.5) and grants them a temporary "+
			"vaccination.execute capability for the leave window (design doc S4.6).")

	approveResp, err := fx.Svc.ApproveLeave(fx.Ctx, fxTenant, actorID, absenceID,
		workforcedomain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve-leave")
	story.Assert("ApproveLeave succeeded", err == nil, "err=%v", err)
	story.Assert("approved status is approved", approveResp.Leave.Status == workforcedomain.LeaveStatusApproved,
		"status=%q", approveResp.Leave.Status)
	story.Assert("replacement_member_id is the backup", approveResp.Leave.ReplacementMemberID != nil && *approveResp.Leave.ReplacementMemberID == backupID,
		"replacement=%v want=%s", approveResp.Leave.ReplacementMemberID, backupID)

	// Verify the backup has a temporary capability grant
	caps, err := fx.Repo.ListCapabilities(fx.Ctx, fxTenant, backupID)
	story.Assert("ListCapabilities for backup succeeded", err == nil, "err=%v", err)
	vaccExecFound := false
	for _, c := range caps {
		if c.CapabilityCode == "vaccination.execute" && c.ScopeType == "shed" && c.ScopeID == shedID && c.Status == "active" {
			vaccExecFound = true
			story.Assert("capability window matches leave window", c.ValidFrom != "" && c.ValidTo != nil,
				"valid_from=%q valid_to=%v", c.ValidFrom, c.ValidTo)
		}
	}
	story.Assert("backup was granted vaccination.execute capability", vaccExecFound, "no capability found")

	// ---- ResolveVaccinationOwner: backup as replacement on leave dates ----

	story.Step("Resolve vaccination owner on a leave date (backup as replacement)",
		"Call ResolveVaccinationOwner for the shed on 2026-08-04 (during the leave). Since the manager "+
			"is on approved leave with a resolved replacement, the result must return the backup manager "+
			"as owner_workforce_member_id with owner_source='replacement' (design doc S4.8).")

	ownerResp, err := fx.Svc.ResolveVaccinationOwner(fx.Ctx, fxTenant, actorID, "shed", shedID, "2026-08-04", "trace-resolve-owner-1")
	story.Assert("ResolveVaccinationOwner succeeded", err == nil, "err=%v", err)
	story.Assert("owner is the backup manager", ownerResp.Owner.OwnerWorkforceMemberID != nil && *ownerResp.Owner.OwnerWorkforceMemberID == backupID,
		"owner=%v want=%s", ownerResp.Owner.OwnerWorkforceMemberID, backupID)
	story.Assert("owner_source is replacement", ownerResp.Owner.OwnerSource == workforcedomain.OwnerSourceReplacement,
		"source=%q", ownerResp.Owner.OwnerSource)
	story.Assert("position_id is the manager's position", ownerResp.Owner.PositionID != nil && *ownerResp.Owner.PositionID == managerPositionID,
		"position_id=%v want=%s", ownerResp.Owner.PositionID, managerPositionID)
	story.Assert("no escalation required (reason is nil)", ownerResp.Owner.Reason == nil,
		"reason=%v", ownerResp.Owner.Reason)

	// ---- Backup becomes unavailable (escalation) ----

	story.Step("Apply leave for the backup manager",
		"While the manager's leave is still active, the backup manager also takes leave (2026-08-04 to 2026-08-04, "+
			"overlapping the manager's leave window). Now the backup is unavailable to cover the manager's position.")

	backupLeaveStart := "2026-08-04"
	backupLeaveEnd := "2026-08-04"
	backupApplyResp, err := fx.Svc.ApplyLeave(fx.Ctx, fxTenant, actorID, workforcedomain.ApplyStaffLeaveRequest{
		WorkforceMemberID: backupID,
		ScopeType:         "shed",
		ScopeID:           shedID,
		ReasonCode:        "personal",
		StartsOn:          backupLeaveStart,
		EndsOn:            backupLeaveEnd,
	}, "trace-apply-backup-leave")
	story.Assert("ApplyLeave for backup succeeded", err == nil, "err=%v", err)

	backupAbsenceID := backupApplyResp.Leave.AbsenceID

	story.Step("Approve the backup's leave (escalates due to no backup-of-backup)",
		"Approve the backup manager's leave. The backup has a fixed position (backup_manager) at the same scope, "+
			"so the roster service tries to resolve coverage for the backup. Since no backup is configured for the "+
			"manager_backup group (no backup-of-backup), the approval escalates (status='escalation_required'). "+
			"This is correct per design doc S4.7: escalation happens at approval time, not at query time.")

	backupApproveResp, err := fx.Svc.ApproveLeave(fx.Ctx, fxTenant, actorID, backupAbsenceID,
		workforcedomain.ApproveStaffLeaveRequest{RowVersion: 1}, "trace-approve-backup-leave")
	story.Assert("ApproveLeave for backup succeeded", err == nil, "err=%v", err)
	story.Assert("backup leave escalates (no backup-of-backup configured)", backupApproveResp.Leave.Status == workforcedomain.LeaveStatusEscalationRequired,
		"status=%q want=escalation_required", backupApproveResp.Leave.Status)
	story.Assert("escalation has no replacement_member_id", backupApproveResp.Leave.ReplacementMemberID == nil,
		"replacement=%v", backupApproveResp.Leave.ReplacementMemberID)

	story.Step("Resolve vaccination owner (commitment semantics)",
		"Call ResolveVaccinationOwner again for 2026-08-04 (while backup is on escalated leave). Since the "+
			"manager's leave was APPROVED with a resolved replacement (backup), that commitment is durable "+
			"(design doc S4.8: replacement 'wins first'). ResolveVaccinationOwner returns the backup as owner, "+
			"even though the backup is now on escalated leave. Escalation checks happen at APPROVAL time, not "+
			"at query time, so the already-resolved replacement stands.")

	stillResolvedResp, err := fx.Svc.ResolveVaccinationOwner(fx.Ctx, fxTenant, actorID, "shed", shedID, "2026-08-04", "trace-resolve-owner-2")
	story.Assert("ResolveVaccinationOwner still returns approved replacement", err == nil, "err=%v", err)
	story.Assert("owner is still the backup (durable commitment)", stillResolvedResp.Owner.OwnerWorkforceMemberID != nil && *stillResolvedResp.Owner.OwnerWorkforceMemberID == backupID,
		"owner=%v want=%s", stillResolvedResp.Owner.OwnerWorkforceMemberID, backupID)
	story.Assert("owner_source is still replacement", stillResolvedResp.Owner.OwnerSource == workforcedomain.OwnerSourceReplacement,
		"source=%q want=replacement", stillResolvedResp.Owner.OwnerSource)
	story.Assert("no escalation for the resolved replacement (commitment is durable)", stillResolvedResp.Owner.Reason == nil,
		"reason=%v should be nil", stillResolvedResp.Owner.Reason)

	story.Step("Resolve vaccination owner after manager's leave ends (manager as owner again)",
		"Call ResolveVaccinationOwner for 2026-08-06 (after the manager's leave ends). The manager is now present, "+
			"so the result must return the manager as owner with owner_source='position_holder'.")

	afterLeaveResp, err := fx.Svc.ResolveVaccinationOwner(fx.Ctx, fxTenant, actorID, "shed", shedID, "2026-08-06", "trace-resolve-owner-3")
	story.Assert("ResolveVaccinationOwner succeeded", err == nil, "err=%v", err)
	story.Assert("owner is the manager (position holder)", afterLeaveResp.Owner.OwnerWorkforceMemberID != nil && *afterLeaveResp.Owner.OwnerWorkforceMemberID == managerID,
		"owner=%v want=%s", afterLeaveResp.Owner.OwnerWorkforceMemberID, managerID)
	story.Assert("owner_source is position_holder", afterLeaveResp.Owner.OwnerSource == workforcedomain.OwnerSourcePositionHolder,
		"source=%q", afterLeaveResp.Owner.OwnerSource)
	story.Assert("no escalation required", afterLeaveResp.Owner.Reason == nil,
		"reason=%v", afterLeaveResp.Owner.Reason)
}

// ---- Story narration / assertion recorder ----

// Story records one kernel story's narrative steps and assertions as a test runs, both driving
// real Go test pass/fail (via t.Errorf on a failed assertion) and building the StoryResult that
// gets rendered into the HTML report on Finish.
type Story struct {
	t      *testing.T
	result StoryResult
}

// NewStory starts recording a new story. Call Finish (typically via defer) to file it into the
// shared report.
func NewStory(t *testing.T, id, title, narrative string) *Story {
	t.Helper()
	return &Story{t: t, result: StoryResult{ID: id, Title: title, Narrative: narrative, Pass: true}}
}

// Step opens a new narrated step. Subsequent Assert calls attach to this step until the next Step.
func (s *Story) Step(name, narrative string) {
	s.t.Helper()
	s.result.Steps = append(s.result.Steps, StepResult{Name: name, Narrative: narrative})
}

// Assert records one pass/fail check against the current step (creating a default step if Step
// was never called) and fails the Go test (non-fatally, via t.Errorf) when cond is false so the
// suite still runs to completion and the report shows every check, not just the first failure.
func (s *Story) Assert(description string, cond bool, detailFormat string, args ...any) bool {
	s.t.Helper()
	if len(s.result.Steps) == 0 {
		s.result.Steps = append(s.result.Steps, StepResult{Name: "Result"})
	}
	detail := fmt.Sprintf(detailFormat, args...)
	idx := len(s.result.Steps) - 1
	s.result.Steps[idx].Assertions = append(s.result.Steps[idx].Assertions, AssertionResult{
		Description: description, Pass: cond, Detail: detail,
	})
	if !cond {
		s.result.Pass = false
		s.t.Errorf("%s / %s: %s (%s)", s.result.Steps[idx].Name, description, "FAILED", detail)
	}
	return cond
}

// Finish files the story into the shared report. Safe to call even after t.Fatalf elsewhere in the
// same goroutine (it still runs as a deferred call), so a setup failure still yields a partial,
// honest report instead of a silently missing story.
func (s *Story) Finish() {
	s.t.Helper()
	if s.t.Failed() {
		s.result.Pass = false
	}
	globalReport.Add(s.result)
}

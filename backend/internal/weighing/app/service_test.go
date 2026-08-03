package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

const (
	testTenant   = "00000000-0000-4000-8000-000000000001"
	testActor    = "00000000-0000-4000-8000-000000000101"
	testPark     = "00000000-0000-4000-8000-000000000201"
	testOp       = "00000000-0000-4000-8000-000000000301"
	testShed     = "00000000-0000-4000-8000-000000000401"
	secondShed   = "00000000-0000-4000-8000-000000000402"
	perShedScope = "00000000-0000-4000-8000-000000000403"
	animalOne    = "00000000-0000-4000-8000-000000000601"
	animalTwo    = "00000000-0000-4000-8000-000000000602"
	proofOne     = "00000000-0000-4000-8000-000000000701"
	proofTwo     = "00000000-0000-4000-8000-000000000702"
	proofThree   = "00000000-0000-4000-8000-000000000703"
	proofShed    = "00000000-0000-4000-8000-000000000704"
)

func TestWeighingRBACSeparatesPlanMonitorExecute(t *testing.T) {
	service := NewService(&fakeRepo{})
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	pcDirector := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RolePCDirector}}
	growthDirector := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}
	cmd := validCreate()

	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("CEO create errored: %v", err)
	}
	// Planning a weighing task is CEO-only (maintainer decision 2026-08-01). The Growth
	// Director monitors and oversees, but does not raise the task.
	if _, err := service.CreateCampaign(context.Background(), growthDirector, cmd); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("growth director create err = %v, want forbidden", err)
	}
	if _, err := service.ListCampaigns(context.Background(), pcDirector, domain.CampaignListScopeAll, "", "", 20); err == nil {
		t.Fatal("pc director monitored weighing; want forbidden")
	}
	if _, err := service.ListCampaigns(context.Background(), growthDirector, domain.CampaignListScopeAll, "", "", 20); err != nil {
		t.Fatalf("growth director monitor errored: %v", err)
	}
	if _, err := service.ListCampaigns(context.Background(), operator, domain.CampaignListScopeMine, "", "", 20); err != nil {
		t.Fatalf("operator execution list errored: %v", err)
	}
	if _, err := service.ListScopeRoster(context.Background(), operator, "00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801", "", "", 50, true); err != nil {
		t.Fatalf("operator roster read errored: %v", err)
	}
	if _, err := service.ListScopeRoster(context.Background(), growthDirector, "00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801", "", "", 50, true); err != nil {
		t.Fatalf("growth director read execution roster errored: %v", err)
	}
	// GetLeadershipShedVideos additionally park-scopes on the campaign's park; a growth
	// director needs a tenant-wide grant carrying WeighingMonitor to read across parks,
	// mirroring how checkParkScope authorizes reopen/close/abandon.
	growthDirectorTenantWideCtx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{ScopeType: "tenant", ScopeID: testTenant, Role: permissions.RoleGrowthDirector},
	})
	if _, err := service.GetLeadershipShedVideos(growthDirectorTenantWideCtx, growthDirector, "00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801", "", 0); err != nil {
		t.Fatalf("growth director leadership videos read errored: %v", err)
	}
	if _, err := service.GetLeadershipShedVideos(context.Background(), operator, "00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801", "", 0); err == nil {
		t.Fatal("operator read leadership videos; want forbidden")
	}
	if _, err := service.RecordAnimalObservation(context.Background(), operator, domain.RecordAnimalObservation{
		CampaignID: "00000000-0000-4000-8000-000000000501", CampaignShedID: "00000000-0000-4000-8000-000000000801", ScannedIdentifier: "rfid-app-1", WeightKg: 12.3, ProofArtifactID: "00000000-0000-4000-8000-000000000701", ActualLocationID: testShed, IdempotencyKey: "scan-1",
	}); err != nil {
		t.Fatalf("operator execute errored: %v", err)
	}
	if _, err := service.RecordAnimalObservation(context.Background(), growthDirector, domain.RecordAnimalObservation{
		CampaignID: "00000000-0000-4000-8000-000000000501", CampaignShedID: "00000000-0000-4000-8000-000000000801", ScannedIdentifier: "rfid-app-2", WeightKg: 12.3, ProofArtifactID: "00000000-0000-4000-8000-000000000701", ActualLocationID: testShed, IdempotencyKey: "scan-2",
	}); err != nil {
		t.Fatalf("growth director execute errored: %v", err)
	}
}

func TestListCampaignsUsesRepositoryScopedPaginationForExecuteOnlyOperator(t *testing.T) {
	repo := &campaignListRepo{
		operatorPage: domain.CampaignPage{Items: []domain.Campaign{{
			CampaignID: "00000000-0000-4000-8000-000000000501",
			TenantID:   testTenant,
			Status:     domain.StatusPublished,
			Sheds: []domain.CampaignShed{
				{CampaignShedID: "00000000-0000-4000-8000-000000000801", DisplayName: "Yashoda 1", OperatorUserID: testOp},
			},
		}}},
		monitorPage: domain.CampaignPage{Items: []domain.Campaign{{
			CampaignID: "00000000-0000-4000-8000-000000000501",
			TenantID:   testTenant,
			Status:     domain.StatusPublished,
			Sheds: []domain.CampaignShed{
				{CampaignShedID: "00000000-0000-4000-8000-000000000801", DisplayName: "Yashoda 1", OperatorUserID: testOp},
				{CampaignShedID: "00000000-0000-4000-8000-000000000802", DisplayName: "Yashoda 2", OperatorUserID: "00000000-0000-4000-8000-000000000302"},
			},
		}}},
	}
	service := NewService(repo)

	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	page, err := service.ListCampaigns(context.Background(), operator, domain.CampaignListScopeMine, "", "", 20)
	if err != nil {
		t.Fatalf("operator list campaigns: %v", err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Sheds) != 1 || page.Items[0].Sheds[0].DisplayName != "Yashoda 1" {
		t.Fatalf("operator page = %+v, want only assigned shed", page.Items)
	}
	if repo.operatorUserID != testOp || repo.operatorCalls != 1 || repo.monitorCalls != 0 {
		t.Fatalf("repo calls operator=(%q,%d) monitor=%d, want operator-scoped pagination", repo.operatorUserID, repo.operatorCalls, repo.monitorCalls)
	}

	monitor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}
	page, err = service.ListCampaigns(context.Background(), monitor, domain.CampaignListScopeAll, "", "", 20)
	if err != nil {
		t.Fatalf("monitor list campaigns: %v", err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Sheds) != 2 {
		t.Fatalf("monitor page = %+v, want all sheds", page.Items)
	}
	if repo.monitorCalls != 1 {
		t.Fatalf("repo monitor calls=%d, want normal monitor listing", repo.monitorCalls)
	}
}

// TestListCampaignsMineIsAssigneeScopedForEveryExecutor is the regression for the defect where
// the growth director's work list showed all four sheds with live Scan actions.
//
// The old service asked `canExecute && !canMonitor` to decide "is this a worker", which is true
// only for RoleOperator. A growth director holds BOTH, so he fell into the unfiltered branch.
// Scoping is now per-surface: ScopeMine is assignee-scoped for EVERY executor, director included.
func TestListCampaignsMineIsAssigneeScopedForEveryExecutor(t *testing.T) {
	for _, tc := range []struct {
		name string
		role string
	}{
		{name: "operator", role: permissions.RoleOperator},
		{name: "growth director", role: permissions.RoleGrowthDirector},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &campaignListRepo{
				operatorPage: domain.CampaignPage{Items: []domain.Campaign{{CampaignID: "00000000-0000-4000-8000-000000000501", TenantID: testTenant}}},
				monitorPage:  domain.CampaignPage{Items: []domain.Campaign{{CampaignID: "00000000-0000-4000-8000-000000000501", TenantID: testTenant}}},
			}
			service := NewService(repo)
			actor := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{tc.role}}

			if _, err := service.ListCampaigns(context.Background(), actor, domain.CampaignListScopeMine, "", "", 20); err != nil {
				t.Fatalf("scope=mine: %v", err)
			}
			if repo.operatorCalls != 1 || repo.monitorCalls != 0 {
				t.Fatalf("scope=mine used operator=%d monitor=%d, want the assignee-scoped read", repo.operatorCalls, repo.monitorCalls)
			}
			if repo.operatorUserID != testOp {
				t.Fatalf("scope=mine scoped to %q, want the caller %q", repo.operatorUserID, testOp)
			}
		})
	}
}

// TestListCampaignsScopeAuthority pins each surface to its own capability, so no scope is
// reachable by holding a different surface's permission.
func TestListCampaignsScopeAuthority(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	growthDirector := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	for _, tc := range []struct {
		name      string
		actor     domain.Actor
		scope     domain.CampaignListScope
		wantAllow bool
	}{
		// The CEO plans: the flat list is his, and he must never reach an executable surface.
		{name: "ceo may read the flat planner list", actor: ceo, scope: domain.CampaignListScopeAll, wantAllow: true},
		{name: "ceo may not read an executable work list", actor: ceo, scope: domain.CampaignListScopeMine, wantAllow: false},
		{name: "ceo may not read the operators surface", actor: ceo, scope: domain.CampaignListScopeOperators, wantAllow: false},
		// The growth director executes his own sheds AND oversees other people's.
		{name: "growth director may read his own work", actor: growthDirector, scope: domain.CampaignListScopeMine, wantAllow: true},
		{name: "growth director may oversee operators", actor: growthDirector, scope: domain.CampaignListScopeOperators, wantAllow: true},
		// An operator sees his own work and nothing wider.
		{name: "operator may read his own work", actor: operator, scope: domain.CampaignListScopeMine, wantAllow: true},
		{name: "operator may not read the flat list", actor: operator, scope: domain.CampaignListScopeAll, wantAllow: false},
		{name: "operator may not oversee operators", actor: operator, scope: domain.CampaignListScopeOperators, wantAllow: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(&campaignListRepo{})
			_, err := service.ListCampaigns(context.Background(), tc.actor, tc.scope, "", "", 20)
			if tc.wantAllow && err != nil {
				t.Fatalf("scope %q: %v, want allowed", tc.scope, err)
			}
			if !tc.wantAllow && err == nil {
				t.Fatalf("scope %q was allowed, want forbidden", tc.scope)
			}
		})
	}
}

// TestListCampaignsRejectsUnknownScope keeps an unrecognised surface from silently falling back
// to a wider listing than the caller asked for.
func TestListCampaignsRejectsUnknownScope(t *testing.T) {
	service := NewService(&campaignListRepo{})
	actor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	if _, err := service.ListCampaigns(context.Background(), actor, domain.CampaignListScope("everything"), "", "", 20); err == nil {
		t.Fatal("unknown scope was accepted; want rejected")
	}
}

func TestCreateCampaignDefaultsPlannedCapBeforeRepository(t *testing.T) {
	repo := &capDefaultRepo{}
	service := NewService(repo)
	cmd := validCreate()
	cmd.PlannedCapPerDay = 0
	actor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}

	if _, err := service.CreateCampaign(context.Background(), actor, cmd); err != nil {
		t.Fatalf("create with omitted cap: %v", err)
	}
	if repo.received.PlannedCapPerDay != 100 {
		t.Fatalf("repository received planned cap=%d want default 100", repo.received.PlannedCapPerDay)
	}
}

func TestRecordAnimalObservationEnqueuesVerifierItem(t *testing.T) {
	repo := &animalObservationRepo{}
	enqueuer := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enqueuer)
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	if _, err := service.RecordAnimalObservation(context.Background(), operator, domain.RecordAnimalObservation{
		CampaignID:        "00000000-0000-4000-8000-000000000501",
		CampaignShedID:    "00000000-0000-4000-8000-000000000801",
		ScannedIdentifier: "RFID-FREEFLOW-1",
		WeightKg:          12.3,
		ProofArtifactID:   proofOne,
		IdempotencyKey:    "scan-freeflow-1",
	}); err != nil {
		t.Fatalf("record animal observation: %v", err)
	}

	if enqueuer.calls != 1 {
		t.Fatalf("verification enqueue calls=%d, want 1", enqueuer.calls)
	}
	if enqueuer.received.Category != domain.VerificationRefTypeAnimal {
		t.Fatalf("ref_type=%q, want %q", enqueuer.received.Category, domain.VerificationRefTypeAnimal)
	}
	if got := enqueuer.received.MediaRefs; len(got) != 1 || got[0] != proofOne {
		t.Fatalf("media refs=%v, want [%s]", got, proofOne)
	}
	if enqueuer.received.OperatorID != testOp || enqueuer.received.ShedID != testShed {
		t.Fatalf("operator/shed=%q/%q, want %q/%q", enqueuer.received.OperatorID, enqueuer.received.ShedID, testOp, testShed)
	}
}

// TestRecordAnimalObservationFirstCaptureNeverWithdraws proves the "first
// capture" branch (obs.Superseded=false, the CTE's `inserted` arm) never calls
// the verification withdrawer -- there is no stale item to retire yet.
func TestRecordAnimalObservationFirstCaptureNeverWithdraws(t *testing.T) {
	repo := &animalObservationRepo{acceptedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)}
	enqueuer := &captureVerificationEnqueuer{}
	withdrawer := &captureVerificationWithdrawer{}
	service := NewService(repo).WithVerificationEnqueuer(enqueuer).WithVerificationWithdrawer(withdrawer)
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	if _, err := service.RecordAnimalObservation(context.Background(), operator, domain.RecordAnimalObservation{
		CampaignID:        "00000000-0000-4000-8000-000000000501",
		CampaignShedID:    "00000000-0000-4000-8000-000000000801",
		ScannedIdentifier: "RFID-FREEFLOW-1",
		WeightKg:          12.3,
		ProofArtifactID:   proofOne,
		IdempotencyKey:    "scan-first-capture",
	}); err != nil {
		t.Fatalf("record animal observation: %v", err)
	}

	if withdrawer.calls != 0 {
		t.Fatalf("withdraw calls=%d, want 0 on a first capture", withdrawer.calls)
	}
	if enqueuer.calls != 1 {
		t.Fatalf("verification enqueue calls=%d, want 1", enqueuer.calls)
	}
}

// TestRecordAnimalObservationEditWithdrawsStaleVerificationBeforeRaisingNewOne
// is the B06 regression. Root cause: enqueueVerification fires on EVERY
// capture, including an edit of a not-yet-submitted/reworked observation
// (recordUnknownAnimalObservationTx's `updated` CTE branch). The old
// idempotency key was `weighing:<category>:<observation_id>` -- content-blind
// -- so CreateItem's `ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`
// silently no-opped on the edit and left the SAME verification_items row bound
// to the OLD weight/proof, including a stale 'verified' decision if a verifier
// had already approved it before the operator touched the draft again.
//
// FAILING evidence (pre-fix, reproduced by temporarily reverting
// reviseVerificationRound to a no-op and reusing the old
// `fmt.Sprintf("weighing:%s:%s", category, obs.ObservationID)` key): this test
// asserted withdrawer.calls==1 and got withdrawer.calls==0, and the second
// enqueue's IdempotencyKey was IDENTICAL to the first -- proving the second
// capture would have silently collided with (and never displaced) the first
// verification item.
//
// PASSING evidence (current code): obs.Superseded=true on the edit triggers
// reviseVerificationRound, which withdraws the prior item for this
// observation_id BEFORE the new item is raised, and the two enqueue calls
// carry DIFFERENT (AcceptedAt-versioned) idempotency keys, so the edit's item
// is a fresh 'pending' row rather than a no-op against stale evidence.
func TestRecordAnimalObservationEditWithdrawsStaleVerificationBeforeRaisingNewOne(t *testing.T) {
	repo := &animalObservationRepo{
		superseded: false,
		acceptedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
	}
	enqueuer := &captureVerificationEnqueuer{}
	withdrawer := &captureVerificationWithdrawer{}
	service := NewService(repo).WithVerificationEnqueuer(enqueuer).WithVerificationWithdrawer(withdrawer)
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	cmd := domain.RecordAnimalObservation{
		CampaignID:        "00000000-0000-4000-8000-000000000501",
		CampaignShedID:    "00000000-0000-4000-8000-000000000801",
		ScannedIdentifier: "RFID-FREEFLOW-1",
		WeightKg:          12.3,
		ProofArtifactID:   proofOne,
		IdempotencyKey:    "scan-first",
	}
	// First capture: a brand-new row (the CTE's `inserted` branch). No prior
	// evidence exists, so nothing should be withdrawn.
	if _, err := service.RecordAnimalObservation(context.Background(), operator, cmd); err != nil {
		t.Fatalf("first capture: %v", err)
	}
	if withdrawer.calls != 0 {
		t.Fatalf("withdraw calls after first capture=%d, want 0", withdrawer.calls)
	}
	firstKey := enqueuer.received.IdempotencyKey

	// Operator edits the draft (a verifier may already have APPROVED the first
	// capture at this point -- that is exactly the scenario the fix must
	// close). The repo now reports Superseded=true (the CTE's `updated`
	// branch) with an advanced AcceptedAt (a new evidence round).
	repo.superseded = true
	repo.acceptedAt = time.Date(2026, 8, 1, 9, 5, 0, 0, time.UTC)
	cmd.WeightKg = 13.1
	cmd.IdempotencyKey = "scan-edit"
	if _, err := service.RecordAnimalObservation(context.Background(), operator, cmd); err != nil {
		t.Fatalf("edit capture: %v", err)
	}

	if withdrawer.calls != 1 {
		t.Fatalf("withdraw calls after edit=%d, want 1 -- the stale verification item must be retired before the new one is raised", withdrawer.calls)
	}
	if withdrawer.refType != domain.VerificationRefTypeAnimal {
		t.Fatalf("withdraw ref_type=%q, want %q", withdrawer.refType, domain.VerificationRefTypeAnimal)
	}
	if len(withdrawer.observationIDs) != 1 || withdrawer.observationIDs[0] != "00000000-0000-4000-8000-000000000901" {
		t.Fatalf("withdraw observation ids=%v, want the edited observation's id", withdrawer.observationIDs)
	}
	if enqueuer.calls != 2 {
		t.Fatalf("verification enqueue calls=%d, want 2 (one per capture)", enqueuer.calls)
	}
	secondKey := enqueuer.received.IdempotencyKey
	if secondKey == firstKey {
		t.Fatalf("edit re-enqueue reused the SAME idempotency key %q as the first capture -- it would silently no-op against the just-withdrawn item instead of raising fresh pending work", secondKey)
	}
}

func TestRecordShedObservationEnqueuesVerifierItemWithAllProofs(t *testing.T) {
	repo := &shedObservationRepo{}
	enqueuer := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enqueuer)
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	if _, err := service.RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
		CampaignID:       "00000000-0000-4000-8000-000000000501",
		CampaignShedID:   "00000000-0000-4000-8000-000000000801",
		WeightKg:         250,
		AnimalCount:      10,
		ProofArtifactID:  proofOne,
		ProofArtifactIDs: []string{proofOne, proofTwo},
		IdempotencyKey:   "shed-lumpsum-1",
	}); err != nil {
		t.Fatalf("record shed observation: %v", err)
	}

	if enqueuer.calls != 1 {
		t.Fatalf("verification enqueue calls=%d, want 1", enqueuer.calls)
	}
	if enqueuer.received.Category != domain.VerificationRefTypeShed {
		t.Fatalf("ref_type=%q, want %q", enqueuer.received.Category, domain.VerificationRefTypeShed)
	}
	if got := enqueuer.received.MediaRefs; len(got) != 2 || got[0] != proofOne || got[1] != proofTwo {
		t.Fatalf("media refs=%v, want [%s %s]", got, proofOne, proofTwo)
	}
}

func TestPerShedCategoryRoutesToShedObservationOnly(t *testing.T) {
	repo := &fakeRepo{}
	service := NewService(repo)
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}
	if _, err := service.RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
		CampaignID: "00000000-0000-4000-8000-000000000501", CampaignShedID: "00000000-0000-4000-8000-000000000801", WeightKg: 450, AnimalCount: 30, ProofArtifactID: "00000000-0000-4000-8000-000000000701", IdempotencyKey: "shed-1",
	}); err != nil {
		t.Fatalf("shed observation errored: %v", err)
	}
	if repo.animalWrites != 0 {
		t.Fatalf("per-shed observation wrote %d animal rows; want 0", repo.animalWrites)
	}
	if repo.shedWrites != 1 {
		t.Fatalf("shed writes = %d, want 1", repo.shedWrites)
	}
}

func TestLumpSumObservationAcceptsTotalWeightAndOneToFiveVideos(t *testing.T) {
	repo := &shedCaptureRepo{}
	service := NewService(repo)
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}
	proofIDs := []string{
		proofOne,
		proofTwo,
		proofThree,
		proofShed,
		"00000000-0000-4000-8000-000000000705",
	}

	if _, err := service.RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
		CampaignID:       "00000000-0000-4000-8000-000000000501",
		CampaignShedID:   perShedScope,
		WeightKg:         132.5,
		AnimalCount:      10,
		ProofArtifactIDs: proofIDs,
		IdempotencyKey:   "shed:five-videos",
	}); err != nil {
		t.Fatalf("record five-video lump sum: %v", err)
	}
	if repo.received.AverageWeightKg != 13.25 || repo.received.WeightKg != 132.5 {
		t.Fatalf("normalized weight=(%v,%v), want average 13.25 and total 132.5", repo.received.AverageWeightKg, repo.received.WeightKg)
	}
	if len(repo.received.ProofArtifactIDs) != 5 || repo.received.ProofArtifactID != proofOne {
		t.Fatalf("normalized proofs=%v primary=%q", repo.received.ProofArtifactIDs, repo.received.ProofArtifactID)
	}

	tooMany := append(append([]string(nil), proofIDs...), "00000000-0000-4000-8000-000000000706")
	if _, err := service.RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
		CampaignID:       "00000000-0000-4000-8000-000000000501",
		CampaignShedID:   perShedScope,
		WeightKg:         132.5,
		AnimalCount:      10,
		ProofArtifactIDs: tooMany,
		IdempotencyKey:   "shed:six-videos",
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("six-video error=%v, want invalid argument", err)
	}
}

func TestRecordAnimalObservationRejectsInvalidWeight(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}
	for name, weight := range map[string]float64{
		"missing":           0,
		"negative":          -1,
		"not a number":      math.NaN(),
		"positive infinity": math.Inf(1),
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{}
			_, err := NewService(repo).RecordAnimalObservation(context.Background(), operator, domain.RecordAnimalObservation{
				CampaignID:        "00000000-0000-4000-8000-000000000501",
				CampaignShedID:    perShedScope,
				ScannedIdentifier: "RFID-1",
				WeightKg:          weight,
				ProofArtifactID:   proofOne,
				IdempotencyKey:    "animal:invalid-weight",
			})
			if !errors.Is(err, ports.ErrInvalidArgument) {
				t.Fatalf("weight %v error=%v, want invalid argument", weight, err)
			}
			if repo.animalWrites != 0 {
				t.Fatalf("repository writes=%d, want 0", repo.animalWrites)
			}
		})
	}
}

func TestLumpSumObservationRejectsInvalidCountAndWeight(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}
	tests := []struct {
		name   string
		weight float64
		count  int
	}{
		{name: "missing count", weight: 100, count: 0},
		{name: "negative count", weight: 100, count: -2},
		{name: "missing total weight", weight: 0, count: 4},
		{name: "negative total weight", weight: -100, count: 4},
		{name: "not a number total weight", weight: math.NaN(), count: 4},
		{name: "infinite total weight", weight: math.Inf(1), count: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &shedCaptureRepo{}
			_, err := NewService(repo).RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
				CampaignID:      "00000000-0000-4000-8000-000000000501",
				CampaignShedID:  perShedScope,
				WeightKg:        tt.weight,
				AnimalCount:     tt.count,
				ProofArtifactID: proofShed,
				IdempotencyKey:  "shed:invalid-numeric",
			})
			if !errors.Is(err, ports.ErrInvalidArgument) {
				t.Fatalf("weight=%v count=%d error=%v, want invalid argument", tt.weight, tt.count, err)
			}
			if repo.shedWrites != 0 {
				t.Fatalf("repository writes=%d, want 0", repo.shedWrites)
			}
		})
	}
}

func TestLumpSumObservationAcceptsSingleProofWithRequiredAnimalCount(t *testing.T) {
	repo := &shedCaptureRepo{}
	service := NewService(repo)
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}

	if _, err := service.RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
		CampaignID:      "00000000-0000-4000-8000-000000000501",
		CampaignShedID:  perShedScope,
		WeightKg:        12.75,
		AnimalCount:     1,
		ProofArtifactID: proofShed,
		IdempotencyKey:  "shed:legacy-client",
	}); err != nil {
		t.Fatalf("record legacy lump sum: %v", err)
	}
	if repo.received.AverageWeightKg != 12.75 || len(repo.received.ProofArtifactIDs) != 1 || repo.received.ProofArtifactIDs[0] != proofShed {
		t.Fatalf("legacy request normalized to %+v", repo.received)
	}
}

func TestRecordAnimalObservationRejectsMalformedActualLocation(t *testing.T) {
	service := NewService(&fakeRepo{})
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}

	_, err := service.RecordAnimalObservation(context.Background(), operator, domain.RecordAnimalObservation{
		CampaignID:        "00000000-0000-4000-8000-000000000501",
		CampaignShedID:    "00000000-0000-4000-8000-000000000801",
		ScannedIdentifier: "rfid-bad-location",
		WeightKg:          12.3,
		ProofArtifactID:   proofOne,
		ActualLocationID:  "not-a-uuid",
		IdempotencyKey:    "scan-bad-location",
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("malformed actual_location_id err = %v, want invalid argument", err)
	}
}

func TestSubmitIndividualScopeRequiresIdempotencyKey(t *testing.T) {
	repo := &fakeRepo{}
	service := NewService(repo)
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}

	err := service.SubmitIndividualScope(
		context.Background(),
		operator,
		"00000000-0000-4000-8000-000000000501",
		"00000000-0000-4000-8000-000000000801",
		"",
		[]string{"RFID-ONE"},
	)
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("missing idempotency key err=%v, want invalid argument", err)
	}
}

func TestReopenScopeRequiresMonitorRole(t *testing.T) {
	service := NewService(&fakeRepo{})
	for _, tc := range []struct {
		name string
		role string
		want error
	}{
		{name: "operator cannot reopen", role: permissions.RoleOperator, want: ports.ErrForbidden},
		{name: "pc director cannot reopen weighing", role: permissions.RolePCDirector, want: ports.ErrForbidden},
		{name: "growth director can reopen", role: permissions.RoleGrowthDirector},
		{name: "ceo can reopen", role: permissions.RoleCEOInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{tc.role}}

			// Set up context with appropriate grant
			// Growth director and CEO need tenant-wide grants for weighing
			// Other roles don't have weighing permissions anyway
			ctx := context.Background()
			if tc.role == permissions.RoleGrowthDirector || tc.role == permissions.RoleCEOInternal {
				grant := permissions.ActiveGrant{
					Role:      tc.role,
					ScopeType: "tenant",
					ScopeID:   testTenant,
				}
				ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{grant})
				ctx = httpmiddleware.WithTenantID(ctx, testTenant)
			}

			err := service.ReopenScope(ctx, actor, "00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801", "reopen:"+tc.role, "missed tags")
			if !errors.Is(err, tc.want) {
				t.Fatalf("ReopenScope() error=%v want %v", err, tc.want)
			}
		})
	}
}

func TestCreateCampaignDefaultsPlannedCapBeforeRepositoryInsert(t *testing.T) {
	repo := &captureCreateRepo{}
	service := NewService(repo)
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	cmd := validCreate()
	cmd.PlannedCapPerDay = 0

	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("create campaign with omitted cap errored: %v", err)
	}
	if repo.created.PlannedCapPerDay != 100 {
		t.Fatalf("repository saw planned cap %d, want default 100", repo.created.PlannedCapPerDay)
	}
}

func TestWeighingSeedScenarioDrivesEndToEndServiceContract(t *testing.T) {
	repo := newScenarioRepo()
	service := NewService(repo)
	ctx := context.Background()
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	director := domain.Actor{TenantID: testTenant, UserID: "00000000-0000-4000-8000-000000000102", Roles: []string{permissions.RoleGrowthDirector}}
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	campaign, err := service.CreateCampaign(ctx, ceo, domain.CreateCampaign{
		ParkID:            testPark,
		PeriodStartDate:   "2026-07-29",
		PeriodEndDate:     "2026-07-29",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    testOp,
		IdempotencyKey:    "weighing-seed:create",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: testShed, LocationType: "shed", DisplayName: "Kid Shed A", WeighingCategory: domain.CategoryIndividualAnimal},
			{LocationID: secondShed, LocationType: "shed", DisplayName: "Kid Shed B", WeighingCategory: domain.CategoryIndividualAnimal},
			{LocationID: perShedScope, LocationType: "shed", DisplayName: "Kid Shed C", WeighingCategory: domain.CategoryPerShedPartition},
		},
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	// Free-flow: the two individual_animal buckets claim NO animal expectation.
	// Only the lump-sum bucket has a real, bucket-grained one.
	if campaign.Progress.IndividualExpectedCount != 0 || campaign.Progress.PerScopeExpectedCount != 1 {
		t.Fatalf("category-aware progress after create = %+v, want 0 individual + 1 per-scope", campaign.Progress)
	}

	if _, err := service.PublishCampaign(ctx, ceo, campaign.CampaignID, "weighing-seed:publish"); err != nil {
		t.Fatalf("publish campaign: %v", err)
	}
	// Planning a weighing task is CEO-only (maintainer decision 2026-08-01), so the Growth
	// Director cannot publish one even though he monitors every park's weighing.
	if _, err := service.PublishCampaign(ctx, director, campaign.CampaignID, "weighing-seed:publish-director"); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("director publish err = %v, want forbidden", err)
	}
	roster, err := service.ListScopeRoster(ctx, operator, campaign.CampaignID, repo.shedByLocation[testShed].CampaignShedID, "", "", 50, true)
	if err != nil {
		t.Fatalf("operator roster read: %v", err)
	}
	if len(roster.Items) != 1 || roster.Items[0].AnimalID != animalOne || roster.Items[0].PrimaryIdentifier != "RFID-ONE" {
		t.Fatalf("roster = %+v, want animal one with RFID", roster.Items)
	}

	first, err := service.RecordAnimalObservation(ctx, operator, domain.RecordAnimalObservation{
		CampaignID: campaign.CampaignID, CampaignShedID: repo.shedByLocation[testShed].CampaignShedID, ScannedIdentifier: "rfid-app-3", WeightKg: 10.2, ProofArtifactID: proofOne, ActualLocationID: testShed, IdempotencyKey: "weighing-seed:animal-1",
	})
	if err != nil {
		t.Fatalf("record first animal: %v", err)
	}
	if first.CampaignShedID != repo.shedByLocation[testShed].CampaignShedID || first.ExpectedLocationID != testShed || first.ActualLocationID != testShed {
		t.Fatalf("first animal context = %+v, want expected current shed", first)
	}
	replay, err := service.RecordAnimalObservation(ctx, operator, domain.RecordAnimalObservation{
		CampaignID: campaign.CampaignID, CampaignShedID: repo.shedByLocation[testShed].CampaignShedID, ScannedIdentifier: "rfid-app-4", WeightKg: 10.2, ProofArtifactID: proofOne, ActualLocationID: testShed, IdempotencyKey: "weighing-seed:animal-1",
	})
	if err != nil {
		t.Fatalf("replay animal observation: %v", err)
	}
	if replay.ObservationID != first.ObservationID || repo.animalWrites != 1 {
		t.Fatalf("idempotent replay = %+v writes=%d, want original observation and one write", replay, repo.animalWrites)
	}

	wrongShed, err := service.RecordAnimalObservation(ctx, operator, domain.RecordAnimalObservation{
		CampaignID: campaign.CampaignID, CampaignShedID: repo.shedByLocation[secondShed].CampaignShedID, ScannedIdentifier: "rfid-app-5", WeightKg: 11.4, ProofArtifactID: proofTwo, ActualLocationID: testShed, IdempotencyKey: "weighing-seed:wrong-shed",
	})
	if err != nil {
		t.Fatalf("record wrong-shed animal: %v", err)
	}
	if wrongShed.ExpectedLocationID != secondShed || wrongShed.ActualLocationID != testShed || wrongShed.ActualLocationLabel != "Kid Shed A" {
		t.Fatalf("wrong-shed context = %+v, want expected shed B and actual shed A", wrongShed)
	}

	freeFlow, err := service.RecordAnimalObservation(ctx, operator, domain.RecordAnimalObservation{
		CampaignID: campaign.CampaignID, CampaignShedID: repo.shedByLocation[testShed].CampaignShedID, ScannedIdentifier: "RFID-NEW-001", WeightKg: 12.7, ProofArtifactID: proofThree, IdempotencyKey: "weighing-seed:free-flow",
	})
	if err != nil {
		t.Fatalf("record free-flow RFID animal: %v", err)
	}
	if freeFlow.ScannedIdentifier != "RFID-NEW-001" || freeFlow.CampaignShedID != repo.shedByLocation[testShed].CampaignShedID {
		t.Fatalf("free-flow context = %+v, want RFID-only observation scoped to selected shed", freeFlow)
	}

	shedObs, err := service.RecordShedObservation(ctx, operator, domain.RecordShedObservation{
		CampaignID: campaign.CampaignID, CampaignShedID: repo.shedByLocation[perShedScope].CampaignShedID, WeightKg: 452.5, AnimalCount: 32, ProofArtifactID: proofShed, IdempotencyKey: "weighing-seed:shed-c",
	})
	if err != nil {
		t.Fatalf("record per-shed observation: %v", err)
	}
	if shedObs.ScannedIdentifier != "" || repo.latestAnimalWeightWrites != 0 {
		t.Fatalf("per-shed observation touched animal truth: obs=%+v latestWrites=%d", shedObs, repo.latestAnimalWeightWrites)
	}
	if _, err := service.RecordShedObservation(ctx, operator, domain.RecordShedObservation{
		CampaignID: campaign.CampaignID, CampaignShedID: repo.shedByLocation[testShed].CampaignShedID, WeightKg: 220, AnimalCount: 10, ProofArtifactID: proofShed, IdempotencyKey: "weighing-seed:bad-shed-category",
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("individual shed accepted per-shed observation err = %v, want not found", err)
	}

	if _, err := service.RecordAnimalObservation(ctx, director, domain.RecordAnimalObservation{
		CampaignID: campaign.CampaignID, CampaignShedID: repo.shedByLocation[testShed].CampaignShedID, ScannedIdentifier: "rfid-app-6", WeightKg: 10.8, ProofArtifactID: proofThree, IdempotencyKey: "weighing-seed:director-execute",
	}); err != nil {
		t.Fatalf("director execute err = %v, want allowed", err)
	}
}

func validCreate() domain.CreateCampaign {
	return domain.CreateCampaign{
		ParkID: testPark, PeriodStartDate: "2026-07-29", PeriodEndDate: "2026-07-29", StartBusinessDate: "2026-07-29", PlannedCapPerDay: 100, OperatorUserID: testOp, IdempotencyKey: "create-1",
		Sheds: []domain.CreateCampaignShed{{LocationID: testShed, LocationType: "shed", DisplayName: "Kid Shed", WeighingCategory: domain.CategoryIndividualAnimal}},
	}
}

type fakeRepo struct {
	animalWrites int
	shedWrites   int
}

type campaignListRepo struct {
	fakeRepo
	monitorPage    domain.CampaignPage
	operatorPage   domain.CampaignPage
	operatorUserID string
	parkID         string
	monitorCalls   int
	operatorCalls  int
}

func (r *campaignListRepo) ListCampaigns(_ context.Context, _, parkID string, _ string, _ int) (domain.CampaignPage, error) {
	r.monitorCalls++
	r.parkID = parkID
	return r.monitorPage, nil
}

func (r *campaignListRepo) ListCampaignsForOperator(_ context.Context, _, operatorUserID, parkID string, _ string, _ int) (domain.CampaignPage, error) {
	r.operatorCalls++
	r.operatorUserID = operatorUserID
	r.parkID = parkID
	return r.operatorPage, nil
}

// The park chip is an OPTIONAL row filter. It must reach the repository verbatim when
// it is a real id, and a malformed one must be refused rather than silently ignored --
// a dropped filter would show the planner another park's tasks under this park's chip.
func TestListCampaignsPassesParkFilterThroughAndRejectsAMalformedOne(t *testing.T) {
	repo := &campaignListRepo{}
	service := NewService(repo)
	monitor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}

	const park = "00000000-0000-4000-8000-000000003001"
	if _, err := service.ListCampaigns(context.Background(), monitor, domain.CampaignListScopeAll, park, "", 20); err != nil {
		t.Fatalf("list with park filter: %v", err)
	}
	if repo.parkID != park {
		t.Fatalf("repo park filter=%q, want %q", repo.parkID, park)
	}

	if _, err := service.ListCampaigns(context.Background(), monitor, domain.CampaignListScopeAll, "not-a-uuid", "", 20); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("malformed park filter err=%v, want invalid argument", err)
	}
}

// The existing-task decoration is date-scoped, so the park read needs a real business DATE.
func TestPlannerCatalogRejectsANonBusinessDate(t *testing.T) {
	service := NewService(&campaignListRepo{})
	monitor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}

	for _, date := range []string{"", "next week", "2026-7-4", "2026-07-04T00:00:00Z"} {
		if _, err := service.PlannerCatalog(context.Background(), monitor, date); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("planner catalog date %q err=%v, want invalid argument", date, err)
		}
	}
	if _, err := service.PlannerCatalog(context.Background(), monitor, "2026-07-04"); err != nil {
		t.Fatalf("valid planner catalog request: %v", err)
	}
}

// The bucket page is park-scoped and date-scoped, so it needs a real park id, a real
// business DATE, and a real "exclude the task being edited" id when one is sent.
func TestPlannerParkBucketsRejectsAMalformedParkDateOrExcludeID(t *testing.T) {
	service := NewService(&campaignListRepo{})
	monitor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}
	park := "00000000-0000-4000-8000-000000003001"

	for _, parkID := range []string{"", "not-a-uuid"} {
		if _, err := service.PlannerParkBuckets(context.Background(), monitor, parkID, "2026-07-04", "", "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("planner buckets park %q err=%v, want invalid argument", parkID, err)
		}
	}
	for _, date := range []string{"", "next week", "2026-7-4", "2026-07-04T00:00:00Z"} {
		if _, err := service.PlannerParkBuckets(context.Background(), monitor, park, date, "", "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("planner buckets date %q err=%v, want invalid argument", date, err)
		}
	}
	if _, err := service.PlannerParkBuckets(context.Background(), monitor, park, "2026-07-04", "not-a-uuid", "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("malformed exclude id err=%v, want invalid argument", err)
	}
	if _, err := service.PlannerParkBuckets(context.Background(), monitor, park, "2026-07-04", "", "", 0); err != nil {
		t.Fatalf("valid planner bucket request: %v", err)
	}
}

type shedCaptureRepo struct {
	fakeRepo
	received domain.RecordShedObservation
}

func (r *shedCaptureRepo) RecordShedObservation(_ context.Context, cmd domain.RecordShedObservation) (domain.Observation, error) {
	r.received = cmd
	return domain.Observation{
		WeightKg:         cmd.WeightKg,
		AverageWeightKg:  cmd.AverageWeightKg,
		ProofArtifactID:  cmd.ProofArtifactID,
		ProofArtifactIDs: append([]string(nil), cmd.ProofArtifactIDs...),
	}, nil
}

type captureVerificationEnqueuer struct {
	calls    int
	received VerificationEnqueueRequest
}

func (e *captureVerificationEnqueuer) EnqueueWeighingVerification(_ context.Context, in VerificationEnqueueRequest) error {
	e.calls++
	e.received = in
	return nil
}

type animalObservationRepo struct {
	fakeRepo
	// superseded makes RecordAnimalObservation report that the write updated an
	// existing not-yet-submitted (or reworked) row in place, exactly like
	// recordUnknownAnimalObservationTx's `updated` CTE branch does on a real edit.
	superseded bool
	acceptedAt time.Time
}

func (r *animalObservationRepo) RecordAnimalObservation(_ context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	return domain.Observation{
		ObservationID:      "00000000-0000-4000-8000-000000000901",
		CampaignID:         cmd.CampaignID,
		CampaignShedID:     cmd.CampaignShedID,
		WeightKg:           cmd.WeightKg,
		ProofArtifactID:    cmd.ProofArtifactID,
		ExpectedLocationID: testShed,
		Superseded:         r.superseded,
		AcceptedAt:         r.acceptedAt,
	}, nil
}

// captureVerificationWithdrawer records every withdraw call the service makes,
// so a test can assert whether reviseVerificationRound fired (edit) or stayed
// silent (first capture).
type captureVerificationWithdrawer struct {
	calls          int
	tenantID       string
	refType        string
	observationIDs []string
}

func (w *captureVerificationWithdrawer) WithdrawWeighingVerification(_ context.Context, tenantID, refType string, observationIDs []string) error {
	w.calls++
	w.tenantID = tenantID
	w.refType = refType
	w.observationIDs = append([]string(nil), observationIDs...)
	return nil
}

type shedObservationRepo struct {
	fakeRepo
}

func (r *shedObservationRepo) RecordShedObservation(_ context.Context, cmd domain.RecordShedObservation) (domain.Observation, error) {
	return domain.Observation{
		ObservationID:    "00000000-0000-4000-8000-000000000902",
		CampaignID:       cmd.CampaignID,
		CampaignShedID:   cmd.CampaignShedID,
		WeightKg:         cmd.WeightKg,
		AverageWeightKg:  cmd.AverageWeightKg,
		AnimalCount:      cmd.AnimalCount,
		ProofArtifactID:  cmd.ProofArtifactID,
		ProofArtifactIDs: append([]string(nil), cmd.ProofArtifactIDs...),
	}, nil
}

func (f fakeRepo) CreateCampaign(context.Context, domain.CreateCampaign) (domain.Campaign, error) {
	return domain.Campaign{CampaignID: "00000000-0000-4000-8000-000000000501"}, nil
}
func (f fakeRepo) UpdateCampaign(context.Context, string, domain.UpdateCampaign) (domain.Campaign, error) {
	return domain.Campaign{CampaignID: "00000000-0000-4000-8000-000000000501"}, nil
}
func (f fakeRepo) PublishCampaign(context.Context, string, string, string, string) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}
func (f fakeRepo) CampaignByID(context.Context, string, string, ports.CampaignAccess) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}
func (f fakeRepo) WeighingParks(context.Context, string, []string) ([]domain.WeighingPark, error) {
	return nil, nil
}
func (f fakeRepo) ListCampaigns(context.Context, string, string, string, int) (domain.CampaignPage, error) {
	return domain.CampaignPage{}, nil
}
func (f fakeRepo) ListCampaignsForOperator(context.Context, string, string, string, string, int) (domain.CampaignPage, error) {
	return domain.CampaignPage{}, nil
}
func (f fakeRepo) ListCampaignSheds(context.Context, string, string, string, int, ports.CampaignAccess) (domain.CampaignShedPage, error) {
	return domain.CampaignShedPage{}, nil
}

func (f fakeRepo) PlannerCatalog(context.Context, string, string) (domain.PlannerCatalog, error) {
	return domain.PlannerCatalog{}, nil
}

func (f fakeRepo) PlannerParkBuckets(context.Context, string, string, string, string, string, int) (domain.PlannerParkBuckets, error) {
	return domain.PlannerParkBuckets{}, nil
}
func (f fakeRepo) ListScopeRoster(context.Context, string, string, string, string, string, int, bool) (domain.RosterPage, error) {
	return domain.RosterPage{Items: []domain.ExpectedAnimal{{AnimalID: animalOne, PrimaryIdentifier: "RFID-ONE"}}}, nil
}
func (f fakeRepo) ListScopeRosterForOperator(context.Context, string, string, string, string, string, string, int, bool) (domain.RosterPage, error) {
	return domain.RosterPage{Items: []domain.ExpectedAnimal{{AnimalID: animalOne, PrimaryIdentifier: "RFID-ONE"}}}, nil
}
func (f fakeRepo) ListLeadershipSheds(context.Context, string, []string, string, int, int) (domain.LeadershipShedPage, error) {
	return domain.LeadershipShedPage{}, nil
}

func (f fakeRepo) GetLeadershipShedVideos(context.Context, string, string, string, string, int, ports.CampaignAccess) (domain.LeadershipShedVideos, error) {
	return domain.LeadershipShedVideos{}, nil
}
func (f *fakeRepo) RecordAnimalObservation(context.Context, domain.RecordAnimalObservation) (domain.Observation, error) {
	f.animalWrites++
	return domain.Observation{}, nil
}
func (f *fakeRepo) RecordShedObservation(context.Context, domain.RecordShedObservation) (domain.Observation, error) {
	f.shedWrites++
	return domain.Observation{}, nil
}
func (f fakeRepo) SubmitIndividualScope(context.Context, string, string, string, string, string, []string) error {
	return nil
}
func (f fakeRepo) ReopenScope(context.Context, string, string, string, string, string, string) ([]string, error) {
	return nil, nil
}
func (f fakeRepo) CloseScope(context.Context, domain.CloseCommand) (domain.CloseResult, error) {
	return domain.CloseResult{Status: domain.StatusClosed}, nil
}
func (f fakeRepo) CloseCampaign(context.Context, domain.CloseCommand) (domain.CloseResult, error) {
	return domain.CloseResult{Status: domain.StatusClosed}, nil
}
func (f fakeRepo) RefreshAvailability(context.Context, string, string) error { return nil }

// CampaignParkID answers the park routing lookup the verification enqueue makes.
func (f fakeRepo) CampaignParkID(context.Context, string, string) (string, error) {
	return testPark, nil
}

// ListAlerts is the inert default; alerts_test.go's recordingAlertsRepo overrides
// it where the call's arguments are the thing under test.
func (f fakeRepo) ListAlerts(context.Context, string, string, bool, []string, string, int) (domain.AlertPage, error) {
	return domain.AlertPage{Title: domain.AlertFeedTitle, EmptyMessage: domain.AlertFeedEmptyMessage}, nil
}

type captureCreateRepo struct {
	fakeRepo
	created domain.CreateCampaign
}

func (r *captureCreateRepo) CreateCampaign(_ context.Context, cmd domain.CreateCampaign) (domain.Campaign, error) {
	r.created = cmd
	return domain.Campaign{CampaignID: "00000000-0000-4000-8000-000000000501"}, nil
}

type capDefaultRepo struct {
	fakeRepo
	received domain.CreateCampaign
}

func (r *capDefaultRepo) CreateCampaign(_ context.Context, cmd domain.CreateCampaign) (domain.Campaign, error) {
	r.received = cmd
	return domain.Campaign{CampaignID: "00000000-0000-4000-8000-000000000501"}, nil
}

type scenarioRepo struct {
	campaign                 domain.Campaign
	shedByLocation           map[string]domain.CampaignShed
	expectedByAnimal         map[string]domain.ExpectedAnimal
	currentLocation          map[string]string
	currentLocationName      map[string]string
	animalByIdem             map[string]domain.Observation
	shedByIdem               map[string]domain.Observation
	animalWrites             int
	shedWrites               int
	latestAnimalWeightWrites int
	closeScopeCalls          []domain.CloseCommand
	abandonScopeCalls        []domain.CloseCommand
	closeCampaignCalls       []domain.CloseCommand
}

func newScenarioRepo() *scenarioRepo {
	return &scenarioRepo{
		shedByLocation:   map[string]domain.CampaignShed{},
		expectedByAnimal: map[string]domain.ExpectedAnimal{},
		currentLocation: map[string]string{
			animalOne: testShed,
			animalTwo: testShed,
		},
		currentLocationName: map[string]string{
			testShed:     "Kid Shed A",
			secondShed:   "Kid Shed B",
			perShedScope: "Kid Shed C",
		},
		animalByIdem: map[string]domain.Observation{},
		shedByIdem:   map[string]domain.Observation{},
	}
}

func (r *scenarioRepo) CreateCampaign(_ context.Context, cmd domain.CreateCampaign) (domain.Campaign, error) {
	r.campaign = domain.Campaign{
		CampaignID:        "00000000-0000-4000-8000-000000000501",
		TenantID:          cmd.TenantID,
		ParkID:            cmd.ParkID,
		PeriodStartDate:   cmd.PeriodStartDate,
		PeriodEndDate:     cmd.PeriodEndDate,
		StartBusinessDate: cmd.StartBusinessDate,
		Status:            domain.StatusDraft,
		PlannedCapPerDay:  cmd.PlannedCapPerDay,
		OperatorUserID:    cmd.OperatorUserID,
		CreatedBy:         cmd.CreatedBy,
	}
	for i, shed := range cmd.Sheds {
		campaignShed := domain.CampaignShed{
			CampaignShedID: []string{
				"00000000-0000-4000-8000-000000000801",
				"00000000-0000-4000-8000-000000000802",
				"00000000-0000-4000-8000-000000000803",
			}[i],
			CampaignID:       r.campaign.CampaignID,
			LocationID:       shed.LocationID,
			LocationType:     shed.LocationType,
			DisplayName:      shed.DisplayName,
			WeighingCategory: shed.WeighingCategory,
			OperatorUserID:   strings.TrimSpace(shed.OperatorUserID),
			Status:           "pending",
		}
		if campaignShed.OperatorUserID == "" {
			campaignShed.OperatorUserID = cmd.OperatorUserID
		}
		campaignShed.ExpectedAnimalCount = 1
		r.shedByLocation[shed.LocationID] = campaignShed
		r.campaign.Sheds = append(r.campaign.Sheds, campaignShed)
	}
	r.expectedByAnimal[animalOne] = domain.ExpectedAnimal{CampaignID: r.campaign.CampaignID, AnimalID: animalOne, ExpectedLocationID: testShed, ExpectedLocationLabel: "Kid Shed A", Status: "pending"}
	r.expectedByAnimal[animalTwo] = domain.ExpectedAnimal{CampaignID: r.campaign.CampaignID, AnimalID: animalTwo, ExpectedLocationID: secondShed, ExpectedLocationLabel: "Kid Shed B", Status: "pending"}
	r.campaign.Progress = scenarioProgress(r.campaign.Sheds, r.expectedByAnimal)
	return r.campaign, nil
}

func (r *scenarioRepo) PublishCampaign(_ context.Context, tenantID, campaignID, _ string, _ string) (domain.Campaign, error) {
	if tenantID != r.campaign.TenantID || campaignID != r.campaign.CampaignID {
		return domain.Campaign{}, ports.ErrNotFound
	}
	r.campaign.Status = domain.StatusPublished
	return r.campaign, nil
}

func (r *scenarioRepo) UpdateCampaign(_ context.Context, campaignID string, _ domain.UpdateCampaign) (domain.Campaign, error) {
	if campaignID != r.campaign.CampaignID {
		return domain.Campaign{}, ports.ErrNotFound
	}
	return r.campaign, nil
}

func (r *scenarioRepo) CampaignByID(context.Context, string, string, ports.CampaignAccess) (domain.Campaign, error) {
	return r.campaign, nil
}
func (r *scenarioRepo) WeighingParks(context.Context, string, []string) ([]domain.WeighingPark, error) {
	return nil, nil
}
func (r *scenarioRepo) ListCampaigns(context.Context, string, string, string, int) (domain.CampaignPage, error) {
	return domain.CampaignPage{Items: []domain.Campaign{r.campaign}}, nil
}
func (r *scenarioRepo) ListCampaignsForOperator(_ context.Context, _ string, operatorUserID string, _ string, _ string, _ int) (domain.CampaignPage, error) {
	campaign := r.campaign
	campaign.Sheds = nil
	for _, shed := range r.campaign.Sheds {
		if shed.OperatorUserID == operatorUserID {
			campaign.Sheds = append(campaign.Sheds, shed)
		}
	}
	if len(campaign.Sheds) == 0 {
		return domain.CampaignPage{}, nil
	}
	return domain.CampaignPage{Items: []domain.Campaign{campaign}}, nil
}

func (r *scenarioRepo) ListCampaignSheds(context.Context, string, string, string, int, ports.CampaignAccess) (domain.CampaignShedPage, error) {
	return domain.CampaignShedPage{}, nil
}

func (r *scenarioRepo) PlannerCatalog(context.Context, string, string) (domain.PlannerCatalog, error) {
	return domain.PlannerCatalog{}, nil
}

func (r *scenarioRepo) PlannerParkBuckets(context.Context, string, string, string, string, string, int) (domain.PlannerParkBuckets, error) {
	return domain.PlannerParkBuckets{}, nil
}

func (r *scenarioRepo) ListScopeRoster(_ context.Context, tenantID, campaignID, campaignShedID string, _ string, _ string, limit int, _ bool) (domain.RosterPage, error) {
	if tenantID != r.campaign.TenantID || campaignID != r.campaign.CampaignID {
		return domain.RosterPage{}, ports.ErrNotFound
	}
	out := []domain.ExpectedAnimal{}
	for _, animal := range r.expectedByAnimal {
		shed := r.shedByLocation[animal.ExpectedLocationID]
		if shed.CampaignShedID != campaignShedID {
			continue
		}
		switch animal.AnimalID {
		case animalOne:
			animal.DisplayAnimalID = "KID-A-001"
			animal.PrimaryIdentifier = "RFID-ONE"
		case animalTwo:
			animal.DisplayAnimalID = "KID-B-001"
			animal.PrimaryIdentifier = "RFID-TWO"
		}
		animal.CampaignShedID = campaignShedID
		animal.Seq = int64(len(out) + 1)
		out = append(out, animal)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return domain.RosterPage{}, ports.ErrNotFound
	}
	return domain.RosterPage{Items: out}, nil
}
func (r *scenarioRepo) ListScopeRosterForOperator(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID string, cursor string, observationsCursor string, limit int, includeRoster bool) (domain.RosterPage, error) {
	for _, shed := range r.campaign.Sheds {
		if shed.CampaignShedID == campaignShedID {
			if shed.OperatorUserID != operatorUserID {
				return domain.RosterPage{}, ports.ErrForbidden
			}
			return r.ListScopeRoster(ctx, tenantID, campaignID, campaignShedID, cursor, observationsCursor, limit, includeRoster)
		}
	}
	return domain.RosterPage{}, ports.ErrNotFound
}

func (r *scenarioRepo) ListLeadershipSheds(context.Context, string, []string, string, int, int) (domain.LeadershipShedPage, error) {
	return domain.LeadershipShedPage{}, nil
}

func (r *scenarioRepo) GetLeadershipShedVideos(context.Context, string, string, string, string, int, ports.CampaignAccess) (domain.LeadershipShedVideos, error) {
	return domain.LeadershipShedVideos{}, nil
}

// RecordAnimalObservation is the free-flow scan write. There is no animal_id on
// the command (domain.RecordAnimalObservation carries only ScannedIdentifier) and
// this fake never resolves a scan to r.expectedByAnimal: the real write path
// never does either (maintainer decision 2026-07-31, weighing free-flow; column
// dropped entirely by 000078_weighing_observations_drop_animal_id.sql).
func (r *scenarioRepo) RecordAnimalObservation(_ context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	if obs, ok := r.animalByIdem[cmd.IdempotencyKey]; ok {
		return obs, nil
	}
	var shed domain.CampaignShed
	shedOK := false
	for _, candidate := range r.shedByLocation {
		if candidate.CampaignShedID == cmd.CampaignShedID {
			shed = candidate
			shedOK = true
			break
		}
	}
	if !shedOK || strings.TrimSpace(cmd.ScannedIdentifier) == "" {
		return domain.Observation{}, ports.ErrNotFound
	}
	obs := domain.Observation{
		ObservationID:      fmt.Sprintf("00000000-0000-4000-8000-00000000090%d", len(r.animalByIdem)+1),
		CampaignID:         cmd.CampaignID,
		CampaignShedID:     shed.CampaignShedID,
		ScannedIdentifier:  cmd.ScannedIdentifier,
		WeightKg:           cmd.WeightKg,
		ProofArtifactID:    cmd.ProofArtifactID,
		ExpectedLocationID: shed.LocationID,
		// Client-supplied, never inferred from the herd.
		ActualLocationID:    cmd.ActualLocationID,
		ActualLocationLabel: r.currentLocationName[cmd.ActualLocationID],
	}
	r.animalByIdem[cmd.IdempotencyKey] = obs
	r.animalWrites++
	return obs, nil
}

func (r *scenarioRepo) RecordShedObservation(_ context.Context, cmd domain.RecordShedObservation) (domain.Observation, error) {
	if obs, ok := r.shedByIdem[cmd.IdempotencyKey]; ok {
		return obs, nil
	}
	var campaignShed domain.CampaignShed
	var found bool
	for _, shed := range r.shedByLocation {
		if shed.CampaignShedID == cmd.CampaignShedID {
			campaignShed = shed
			found = true
			break
		}
	}
	if !found || campaignShed.WeighingCategory != domain.CategoryPerShedPartition {
		return domain.Observation{}, ports.ErrNotFound
	}
	campaignShed.Status = "completed"
	r.shedByLocation[campaignShed.LocationID] = campaignShed
	for i := range r.campaign.Sheds {
		if r.campaign.Sheds[i].CampaignShedID == campaignShed.CampaignShedID {
			r.campaign.Sheds[i] = campaignShed
		}
	}
	obs := domain.Observation{
		ObservationID:   "00000000-0000-4000-8000-000000000951",
		CampaignID:      cmd.CampaignID,
		CampaignShedID:  cmd.CampaignShedID,
		WeightKg:        cmd.WeightKg,
		ProofArtifactID: cmd.ProofArtifactID,
	}
	r.shedByIdem[cmd.IdempotencyKey] = obs
	r.shedWrites++
	r.campaign.Progress = scenarioProgress(r.campaign.Sheds, r.expectedByAnimal)
	return obs, nil
}

func (r *scenarioRepo) RefreshAvailability(context.Context, string, string) error { return nil }

func (r *scenarioRepo) CampaignParkID(context.Context, string, string) (string, error) {
	return testPark, nil
}
func (r *scenarioRepo) SubmitIndividualScope(context.Context, string, string, string, string, string, []string) error {
	return nil
}
func (r *scenarioRepo) ReopenScope(context.Context, string, string, string, string, string, string) ([]string, error) {
	return nil, nil
}

func (r *scenarioRepo) CloseScope(_ context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
	r.closeScopeCalls = append(r.closeScopeCalls, cmd)
	return domain.CloseResult{
		CampaignID:     cmd.CampaignID,
		CampaignShedID: cmd.CampaignShedID,
		Status:         domain.StatusClosed,
		Reason:         cmd.Reason,
		ClosedBy:       cmd.ClosedBy,
	}, nil
}

func (f *fakeRepo) AbandonScope(_ context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
	return domain.CloseResult{CampaignID: cmd.CampaignID, CampaignShedID: cmd.CampaignShedID, Status: domain.StatusClosed, Reason: cmd.Reason, ClosedBy: cmd.ClosedBy}, nil
}

func (r *scenarioRepo) AbandonScope(_ context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
	r.abandonScopeCalls = append(r.abandonScopeCalls, cmd)
	return domain.CloseResult{
		CampaignID:     cmd.CampaignID,
		CampaignShedID: cmd.CampaignShedID,
		Status:         domain.StatusClosed,
		Reason:         cmd.Reason,
		ClosedBy:       cmd.ClosedBy,
	}, nil
}

func (r *scenarioRepo) CloseCampaign(_ context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
	r.closeCampaignCalls = append(r.closeCampaignCalls, cmd)
	return domain.CloseResult{
		CampaignID: cmd.CampaignID,
		Status:     domain.StatusClosed,
		Reason:     cmd.Reason,
		ClosedBy:   cmd.ClosedBy,
	}, nil
}

func scenarioProgress(sheds []domain.CampaignShed, animals map[string]domain.ExpectedAnimal) domain.Progress {
	progress := domain.Progress{}
	for _, shed := range sheds {
		switch shed.WeighingCategory {
		case domain.CategoryPerShedPartition:
			progress.PerScopeExpectedCount++
			if shed.Status == "completed" {
				progress.PerScopeCompletedCount++
			}
		}
	}
	for _, animal := range animals {
		if animal.Status == "weighed" {
			progress.IndividualCompletedCount++
		}
		if animal.AvailabilityStatus == domain.AvailabilityMovedOtherShed {
			progress.WrongShedCount++
		}
	}
	progress.RemainingCount = progress.PerScopeExpectedCount - progress.PerScopeCompletedCount
	return progress
}

func (r *scenarioRepo) ListAlerts(context.Context, string, string, bool, []string, string, int) (domain.AlertPage, error) {
	return domain.AlertPage{}, nil
}

package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestKernelStoryL_ProofVerifyRework drives the full proof → verify → reject → rework → re-verify
// loop through the REAL services (generation, sweeper, proof kernel, completion service): a dose is
// administered against a drive with a video proof captured, the verifier REJECTS it (proof unclear
// → rework, obligation stays open, no stock consumed, coverage not counted), the operator reworks
// (re-administers with a fresh idempotency key), and the verifier ACCEPTS the re-work -- only then
// does coverage count and stock get consumed.
//
// This is the genuine SOP verification loop: CompletionService.RejectExisting keeps the obligation
// open, and coverage (the execution read model's completed count) advances only on AcceptExisting.
func TestKernelStoryL_ProofVerifyRework(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-l", "Proof → verify → reject → rework → re-verify",
		"An operator administers a dose against a drive and captures a video proof. The verifier reviews "+
			"the proof and REJECTS it (unclear video → rework): the obligation stays open, no stock is "+
			"consumed, and coverage does not advance. The operator re-administers (rework, fresh key) and the "+
			"verifier ACCEPTS the re-work. Only now does the obligation complete, stock is consumed, and "+
			"coverage counts. Verified evidence, not mere administration, is what closes the loop.")
	defer story.Finish()

	const (
		shedID     = "ec000000-0000-4000-8000-000000000001"
		stageID    = "ec000000-0000-4000-8000-000000000002"
		operatorID = "ec000000-0000-4000-8000-000000000003"
		parkHeadID = "ec000000-0000-4000-8000-000000000004"
		verifierID = "ec000000-0000-4000-8000-000000000005"
		goatID     = "ec000000-0000-4000-8000-000000000006"
		itemID     = "ec000000-0000-4000-8000-000000000008"
		lotID      = "ec000000-0000-4000-8000-000000000009"
	)

	story.Step("Seed shed, workforce, protocol, one due goat, and vaccine stock",
		"One shed/stage/workforce topology, one published PC vaccination rule, one goat past due, and a "+
			"vaccine lot with stock to reserve for the drive.")
	fx.SeedShed(shedID, "E2E-L", stageID)
	fx.SeedWorkforce(operatorID, parkHeadID, verifierID, shedID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_l", 21, 0, nil)

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-L', 'E2E Story L vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, lotID, fxTenant, itemID, shedID)

	story.Step("Generate + sweep the goat into a one-shed drive",
		"Run the real generation service and the real SM-4 sweeper (with stock reservation) so the goat's "+
			"dose becomes a real drive batch scoped to its shed.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	genRes, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("the goat's dose was generated", genRes.Generated == 1, "generated=%d", genRes.Generated)

	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("one shed drive was formed", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)

	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	fx.exec("assign operator to drive", `UPDATE obligation_batches SET conducted_by=$3 WHERE tenant_id=$1 AND batch_id=$2`, fxTenant, batchID, operatorID)
	oblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)

	story.Step("Capture the video proof for the drive",
		"Upload and complete a real video proof artifact scoped to the batch/shed via the proof kernel -- "+
			"the evidence the verifier will review.")
	shedSubject := shedID
	proof, err := fx.Proof.CreateProof(fx.Ctx, proofdomain.CreateUpload{
		TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4",
		ScopeType: "batch", ScopeID: batchID, SubjectType: "shed", SubjectID: &shedSubject,
		Metadata: map[string]any{"story": "kernel-story-l"},
	}, "local")
	story.Assert("proof upload created", err == nil, "err=%v", err)
	completedProof, err := fx.Proof.CompleteProof(fx.Ctx, proofdomain.CompleteUpload{
		TenantID: fxTenant, ProofID: proof.ProofID, ContentHash: "sha256:kernel-story-l", MimeType: "video/mp4", SizeBytes: 4096,
	})
	story.Assert("proof upload completed (durable evidence)", err == nil && completedProof.UploadState == "completed", "err=%v state=%q", err, completedProof.UploadState)

	svc := vaccapp.NewService(fx.Vacc)
	completion := vaccapp.NewCompletionService(svc, fx.Obl, fx.Inv)
	doses := int32(1)
	verifier := verifierID

	story.Step("Operator administers the dose (recorded, awaiting verification)",
		"Record the dose against the drive/lot with status 'recorded' -- administered, not yet verified.")
	batch, lot := batchID, lotID
	cid1, applied, err := svc.RecordCompletion(fx.Ctx, vaccdomain.NewCompletion{
		TenantID: fxTenant, ObligationID: oblID, GoatID: goatID, BatchID: &batch,
		VaccineInventoryLotID: &lot, Doses: &doses, RouteSite: "SC", AdministeredAt: now,
		ColdChainVerified: true, Status: "recorded", IdempotencyKey: "e2e-story-l-attempt-1",
	})
	story.Assert("first administration recorded", err == nil && applied && cid1 != "", "cid=%q applied=%v err=%v", cid1, applied, err)

	story.Step("Verifier REJECTS the proof (rework): obligation stays open, no coverage, no consume",
		"The verifier finds the video unclear and rejects the recorded dose. The obligation must stay open, "+
			"the completed/coverage count must stay 0, and the reserved stock must not be consumed.")
	rej, err := completion.RejectExisting(fx.Ctx, fxTenant, cid1, "video_unclear_rework", &verifier)
	story.Assert("reject ran without error", err == nil && rej.Applied, "applied=%v err=%v", rej.Applied, err)

	statusAfterReject := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("obligation is still open after rejection (not completed)", statusAfterReject != "completed", "status=%q", statusAfterReject)
	rejectedCount := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2 AND status='rejected'`, fxTenant, oblID)
	story.Assert("the first dose is recorded as rejected", rejectedCount == 1, "rejected=%d", rejectedCount)
	consumedAfterReject := fx.countRows(`SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND batch_id=$2 AND movement_type='consume'`, fxTenant, batchID)
	story.Assert("no stock was consumed by the rejected dose", consumedAfterReject == 0, "consume_movements=%d", consumedAfterReject)

	preAsOf := time.Now().UTC().Add(2 * time.Hour)
	execRowsPre, err := fx.VaccExec.ListVaccinationExecution(fx.Ctx, vaccexecdomain.ExecutionQuery{TenantID: fxTenant, AsOf: preAsOf, DueBefore: now.AddDate(0, 0, 1), Limit: 10})
	story.Assert("execution rollup query ran (post-reject)", err == nil, "err=%v", err)
	story.Assert("coverage has NOT advanced after rejection (0 completed)", execCompletedForBatch(execRowsPre, batchID) == 0, "completed=%d", execCompletedForBatch(execRowsPre, batchID))

	story.Step("Operator reworks: re-administers with a fresh idempotency key",
		"After rework the operator records the dose again under a new idempotency key -- a distinct "+
			"administration attempt, not a replay of the rejected one.")
	cid2, applied2, err := svc.RecordCompletion(fx.Ctx, vaccdomain.NewCompletion{
		TenantID: fxTenant, ObligationID: oblID, GoatID: goatID, BatchID: &batch,
		VaccineInventoryLotID: &lot, Doses: &doses, RouteSite: "SC", AdministeredAt: now,
		ColdChainVerified: true, Status: "recorded", IdempotencyKey: "e2e-story-l-attempt-2",
	})
	story.Assert("rework administration recorded under a new key", err == nil && applied2 && cid2 != "" && cid2 != cid1, "cid2=%q applied=%v err=%v", cid2, applied2, err)

	story.Step("Verifier ACCEPTS the rework: obligation completes, stock consumed, coverage counts",
		"The verifier accepts the re-worked dose. Only now does the obligation complete, the reserved dose "+
			"is consumed, and the execution rollup shows coverage advance to 1.")
	acc, err := completion.AcceptExisting(fx.Ctx, vaccapp.AcceptExistingInput{TenantID: fxTenant, CompletionID: cid2})
	story.Assert("accept ran without error", err == nil, "err=%v", err)
	story.Assert("the reworked dose was accepted and the obligation completed", acc.Applied && acc.Completed, "applied=%v completed=%v", acc.Applied, acc.Completed)

	statusFinal := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("obligation is now completed", statusFinal == "completed", "status=%q", statusFinal)
	consumedFinal := fx.countRows(`SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND batch_id=$2 AND movement_type='consume'`, fxTenant, batchID)
	story.Assert("exactly one dose was consumed (only on accept)", consumedFinal == 1, "consume_movements=%d", consumedFinal)

	acceptedCount := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2 AND status='accepted'`, fxTenant, oblID)
	story.Assert("exactly one accepted (verified) completion counts toward coverage -- only the rework", acceptedCount == 1, "accepted=%d", acceptedCount)
	stillRejected := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2 AND status='rejected'`, fxTenant, oblID)
	story.Assert("the rejected first attempt remains rejected (never counts as coverage)", stillRejected == 1, "rejected=%d", stillRejected)
}

// execCompletedForBatch returns the CompletedCount for the given batch from an execution rollup, or
// -1 when the batch is absent.
func execCompletedForBatch(rows []vaccexecdomain.ExecutionProjection, batchID string) int {
	for i := range rows {
		if rows[i].BatchID != nil && *rows[i].BatchID == batchID {
			return int(rows[i].CompletedCount)
		}
	}
	return -1
}

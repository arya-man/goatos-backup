package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	pidomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestKernelStoryC_BatchDriveVerifyControlTower drives the batching -> execution -> proof ->
// verification -> control-tower chain end to end: two goats sharing one shed generate two
// obligations that the real sweeper (SM-4) combines into ONE drive ("one shed = one drive"), both
// doses are administered and proof is captured, the verifier accepts both, and the process-integrity
// control-tower read model is asserted to show the open alert (verification_pending, NOT process
// intact) and then its clearing (completed, process intact).
//
// Uses time.Now()-relative timestamps throughout (rather than fixed 2026 dates like Stories A/B)
// because CompletionService.AcceptExisting writes verified_at/completed_at using the database's
// wall-clock now(), not a caller-supplied "as of" time -- so the read-model queries below must be
// bounded relative to the real clock, not a fictional story date.
func TestKernelStoryC_BatchDriveVerifyControlTower(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-c", "Batch drive + per-shed execution + proof/verify + control-tower clears",
		"Two goats in the same shed become due for the same PC vaccination dose. The sweeper combines "+
			"them into one shed-scoped drive. Both doses are administered and a proof video is captured for "+
			"the drive. Before the verifier reviews the proof, the control tower shows an open "+
			"verification-pending alert (not process-intact). Once the verifier accepts both doses, the "+
			"control tower shows the drive completed and the alert cleared.")
	defer story.Finish()

	const (
		shedID     = "e3000000-0000-4000-8000-000000000001"
		stageID    = "e3000000-0000-4000-8000-000000000002"
		operatorID = "e3000000-0000-4000-8000-000000000003"
		parkHeadID = "e3000000-0000-4000-8000-000000000004"
		verifierID = "e3000000-0000-4000-8000-000000000005"
		goat1      = "e3000000-0000-4000-8000-000000000006"
		goat2      = "e3000000-0000-4000-8000-000000000007"
		itemID     = "e3000000-0000-4000-8000-000000000008"
		lotID      = "e3000000-0000-4000-8000-000000000009"
	)

	story.Step("Seed the shed, workforce, protocol, and two goats sharing a shed",
		"One park/shed/stage topology (with an operator, park head, and verifier), one published PC "+
			"vaccination rule, and two goats born the same day in the same shed.")
	fx.SeedShed(shedID, "E2E-C", stageID)
	fx.SeedWorkforce(operatorID, parkHeadID, verifierID, shedID)

	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_c", 21, 0, nil)

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53) // 53 days old: 21-day-offset rule due 32 days ago, well past due
	fx.SeedGoat(GoatSpec{GoatID: goat1, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goat2, ShedID: shedID, DOB: &dob})

	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-C', 'E2E Story C vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, lotID, fxTenant, itemID, shedID)

	story.Step("Generate + batch: the sweeper combines both goats into one shed drive",
		"Run the real generation service for the whole protocol version (both goats are eligible), then "+
			"run the real obligation sweeper (SM-4) with stock reservation wired -- it must combine both "+
			"goats' obligations into exactly ONE batch scoped to their shared shed.")

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	genRes, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("both goats' doses were generated", genRes.Generated == 2, "generated=%d", genRes.Generated)

	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepCfg := oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}
	dueBefore := now.AddDate(0, 0, 1)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, sweepCfg, dueBefore)
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("one shed = one drive: both obligations combined into a single batch", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)
	story.Assert("both obligations were attached to that drive", sweepRes.Obligations == 2, "obligations=%d", sweepRes.Obligations)

	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	// Assign an operator so the row reads as owned (not "blocked") once execution starts --
	// the same conducted_by assignment the real vaccination-execution UI records when a drive starts.
	fx.exec("assign operator to drive", `UPDATE obligation_batches SET conducted_by=$3 WHERE tenant_id=$1 AND batch_id=$2`, fxTenant, batchID, operatorID)

	obl1 := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goat1)
	obl2 := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goat2)

	story.Step("Capture proof for the drive",
		"Upload and complete a real proof artifact (video) scoped to the batch/shed via the standalone "+
			"proof kernel component -- the same CreateProof/CompleteProof path SOP submissions use.")
	shedSubject := shedID
	proof, err := fx.Proof.CreateProof(fx.Ctx, proofdomain.CreateUpload{
		TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4",
		ScopeType: "batch", ScopeID: batchID, SubjectType: "shed", SubjectID: &shedSubject,
		Metadata: map[string]any{"story": "kernel-story-c"},
	}, "local")
	story.Assert("proof upload was created", err == nil, "err=%v", err)
	completedProof, err := fx.Proof.CompleteProof(fx.Ctx, proofdomain.CompleteUpload{
		TenantID: fxTenant, ProofID: proof.ProofID,
		ContentHash: "sha256:kernel-story-c", MimeType: "video/mp4", SizeBytes: 4096,
	})
	story.Assert("proof upload was completed", err == nil, "err=%v", err)
	story.Assert("the completed proof artifact is durable (upload_state=completed)", completedProof.UploadState == "completed", "upload_state=%q", completedProof.UploadState)

	story.Step("Execute the drive: administer both doses",
		"Record both goats' completions against the drive batch and the reserved vaccine lot (status "+
			"'recorded' -- administered, awaiting verification).")
	svc := vaccapp.NewService(fx.Vacc)
	completion := vaccapp.NewCompletionService(svc, fx.Obl, fx.Inv)
	doses := int32(1)
	record := func(obligationID, goatID, key string) string {
		t.Helper()
		batch, lot := batchID, lotID
		cid, applied, err := svc.RecordCompletion(fx.Ctx, vaccdomain.NewCompletion{
			TenantID: fxTenant, ObligationID: obligationID, GoatID: goatID, BatchID: &batch,
			VaccineInventoryLotID: &lot, Doses: &doses, RouteSite: "SC", AdministeredAt: now,
			ColdChainVerified: true, Status: "recorded", IdempotencyKey: key,
		})
		if err != nil || !applied || cid == "" {
			t.Fatalf("record completion for %s: cid=%q applied=%v err=%v", goatID, cid, applied, err)
		}
		return cid
	}
	cid1 := record(obl1, goat1, "e2e-story-c-g1")
	cid2 := record(obl2, goat2, "e2e-story-c-g2")

	story.Step("Control tower shows the open verification-pending alert",
		"Before the verifier reviews anything, the process-integrity control tower must show this "+
			"shed/rule row as verification_pending and NOT process-intact -- the real open gap a "+
			"director would see on Control Tower.")
	preAsOf := time.Now().UTC().Add(2 * time.Hour)
	preResult, err := fx.PI.ListRows(fx.Ctx, pidomain.Query{
		TenantID: fxTenant, AsOf: preAsOf, DueBefore: now.AddDate(0, 0, 1), Limit: 10,
	})
	story.Assert("control tower query ran without error", err == nil, "err=%v", err)
	story.Assert("exactly one control-tower row for this shed/rule/batch", len(preResult.Rows) == 1, "rows=%d", len(preResult.Rows))
	if len(preResult.Rows) == 1 {
		row := preResult.Rows[0]
		story.Assert("work_state is verification_pending", row.WorkState == pidomain.WorkStateVerificationPending, "work_state=%q", row.WorkState)
		story.Assert("process_intact is false (this IS the open alert)", !row.ProcessIntact, "process_intact=%v", row.ProcessIntact)
	}

	story.Step("Per-shed execution rollup also shows both doses awaiting verification",
		"The vaccination-execution read model (the per-shed drilldown behind /vaccination/execution/sheds/{shed_id}) "+
			"must show both obligations recorded and none yet completed.")
	execRows, err := fx.VaccExec.ListVaccinationExecution(fx.Ctx, vaccexecdomain.ExecutionQuery{
		TenantID: fxTenant, AsOf: preAsOf, DueBefore: now.AddDate(0, 0, 1), Limit: 10,
	})
	story.Assert("execution rollup query ran without error", err == nil, "err=%v", err)
	if err == nil {
		var execRow *vaccexecdomain.ExecutionProjection
		for i := range execRows {
			if execRows[i].BatchID != nil && *execRows[i].BatchID == batchID {
				execRow = &execRows[i]
				break
			}
		}
		story.Assert("the shed drive appears in the execution rollup", execRow != nil, "batch=%s rows=%d", batchID, len(execRows))
		if execRow != nil {
			story.Assert("both obligations are recorded, none completed yet",
				execRow.ObligationCount == 2 && execRow.CompletionRecorded == 2 && execRow.CompletedCount == 0,
				"obligations=%d recorded=%d completed=%d", execRow.ObligationCount, execRow.CompletionRecorded, execRow.CompletedCount)
		}
	}

	story.Step("Verifier accepts both doses",
		"The verifier reviews and accepts both recorded completions -- consuming the reserved doses and "+
			"completing both obligations.")
	ar1, err := completion.AcceptExisting(fx.Ctx, vaccapp.AcceptExistingInput{TenantID: fxTenant, CompletionID: cid1})
	story.Assert("accept goat 1's dose ran without error", err == nil, "err=%v", err)
	story.Assert("goat 1's dose was accepted and its obligation completed", ar1.Applied && ar1.Completed, "applied=%v completed=%v", ar1.Applied, ar1.Completed)
	ar2, err := completion.AcceptExisting(fx.Ctx, vaccapp.AcceptExistingInput{TenantID: fxTenant, CompletionID: cid2})
	story.Assert("accept goat 2's dose ran without error", err == nil, "err=%v", err)
	story.Assert("goat 2's dose was accepted and its obligation completed", ar2.Applied && ar2.Completed, "applied=%v completed=%v", ar2.Applied, ar2.Completed)

	story.Step("Control tower alert clears",
		"After both doses are verified, the same control-tower row must now read as completed and "+
			"process-intact -- the alert has cleared with no manual dismissal.")
	postAsOf := time.Now().UTC().Add(2 * time.Hour)
	postResult, err := fx.PI.ListRows(fx.Ctx, pidomain.Query{
		TenantID: fxTenant, AsOf: postAsOf, DueBefore: now.AddDate(0, 0, 1), Limit: 10, IncludeCompleted: true,
	})
	story.Assert("control tower re-query ran without error", err == nil, "err=%v", err)
	story.Assert("still exactly one control-tower row for this shed/rule/batch", len(postResult.Rows) == 1, "rows=%d", len(postResult.Rows))
	if len(postResult.Rows) == 1 {
		row := postResult.Rows[0]
		story.Assert("work_state is completed", row.WorkState == pidomain.WorkStateCompleted, "work_state=%q", row.WorkState)
		story.Assert("process_intact is true (the alert cleared)", row.ProcessIntact, "process_intact=%v", row.ProcessIntact)
	}
}

package e2e

import (
	"bytes"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
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
	story.Certify("backend kernel + SOP proof/submission/rework/review")
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

	sopRepo := soppg.NewRepository(fx.Pool, 5*time.Second)
	sopService := sopapp.NewService(sopRepo)
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: sopService, actorID: operatorID}, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{SOPVersionID: canonicalVaccinationSOPVersion, VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("one shed drive was formed", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)

	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	oblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	taskID := fx.scanText(`SELECT sop_task_id::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2::uuid`, fxTenant, batchID)
	story.Assert("sweeper created executable SOP task", taskID != "", "task_id=%q", taskID)

	story.Step("Operator uploads canonical proof and submits the first administration",
		"All three task-bound proof videos and the exact goat/form payload pass through the SOP submission bridge, which records the completion.")
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "story-l-proof-secret"))
	proofRefs := make([]sopdomain.ProofReference, 0, 3)
	proofIDs := make(map[string]string, 3)
	for _, subject := range []string{"shed", "vial_lot", "administration"} {
		var subjectID *string
		if subject == "shed" {
			subjectID = storyAAPtrString(shedID)
		}
		target, proofErr := proofService.CreateUpload(fx.Ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4", ScopeType: "task", ScopeID: taskID,
			SubjectType: subject, SubjectID: subjectID, UploadedBy: storyAAPtrString(operatorID), Metadata: map[string]any{"story": "L"},
		})
		story.Assert("proof registered for "+subject, proofErr == nil, "err=%v", proofErr)
		if proofErr != nil {
			continue
		}
		_, proofErr = proofService.StoreUpload(fx.Ctx, fxTenant, target.Proof.ProofID, "video/mp4", bytes.NewBufferString("story-l-"+subject))
		story.Assert("proof binary completed for "+subject, proofErr == nil, "err=%v", proofErr)
		proofRefs = append(proofRefs, sopdomain.ProofReference{ProofID: target.Proof.ProofID})
		proofIDs[subject] = target.Proof.ProofID
	}
	vaccinationService := vaccapp.NewService(fx.Vacc)
	verifyBus := eventbus.NewInProcessBus()
	vaccapp.NewVerificationHandler(vaccapp.NewCompletionService(vaccinationService, fx.Obl, fx.Inv)).Register(verifyBus)
	sopService.WithProofValidator(proofService).
		WithSubmissionHook(sopbridge.NewVaccinationSubmissionBridge(vaccinationService)).
		WithTaskReviewFanout(sopbridge.NewVerifyFanout(vaccinationService, verifyBus))
	answers := map[string]any{
		"vaccine_lot_id": lotID, "cold_chain_verified": true, "goat_ids": []any{goatID},
		"dose_ml_given": 1.0, "doses": 1, "route_site": "subcutaneous", "administered_at": now.Format(time.RFC3339),
		"adverse_reaction": false, "shed_video": proofIDs["shed"], "vial_lot_video": proofIDs["vial_lot"],
		"administration_video": proofIDs["administration"],
	}
	first, err := sopService.SubmitTask(fx.Ctx, sopports.SubmitTaskCommand{
		TenantID: fxTenant, ActorID: operatorID, TaskID: taskID,
		Body: sopdomain.SubmitTaskRequest{SOPVersionID: canonicalVaccinationSOPVersion, IdempotencyKey: "story-l-attempt-1", Answers: answers, ProofRefs: proofRefs},
	}, "story-l-submit-1")
	story.Assert("first SOP administration recorded", err == nil, "err=%v", err)
	if err != nil {
		return
	}

	story.Step("Verifier REJECTS the proof (rework): obligation stays open, no coverage, no consume",
		"The verifier finds the video unclear and rejects the recorded dose. The obligation must stay open, "+
			"the completed/coverage count must stay 0, and the reserved stock must not be consumed.")
	reworked, err := sopService.ReworkTask(fx.Ctx, sopports.ReviewTaskCommand{
		TenantID: fxTenant, ActorID: verifierID, TaskID: taskID,
		Body: sopdomain.ReviewTaskRequest{Reason: "video_unclear_rework", RowVersion: first.Task.RowVersion},
	}, "story-l-rework")
	story.Assert("SOP rework review ran without error", err == nil && reworked != nil && reworked.Task.State == "rework_requested", "err=%v", err)
	if err != nil {
		return
	}

	statusAfterReject := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("obligation is still open after rejection (not completed)", statusAfterReject != "completed", "status=%q", statusAfterReject)
	rejectedCount := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2 AND status='rejected'`, fxTenant, oblID)
	story.Assert("the first dose is recorded as rejected", rejectedCount == 1, "rejected=%d", rejectedCount)
	consumedAfterReject := fx.countRows(`SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND batch_id=$2 AND movement_type='consume'`, fxTenant, batchID)
	story.Assert("no stock was consumed by the rejected dose", consumedAfterReject == 0, "consume_movements=%d", consumedAfterReject)

	// Live as-of a small margin past now (skew-safe, inside the freshness TTL); a now()+hours as-of
	// would trip the AGE-based buildAge gate that the real HTTP path avoids via ClampFutureAsOf.
	preAsOf := time.Now().UTC().Add(2 * time.Minute)
	// Drive the real per-shed execution projector before reading it (without a serving version the
	// read model is honestly "unavailable").
	_, err = fx.VaccExec.RecomputeExecutionProjection(fx.Ctx, vaccexecdomain.ExecutionProjectionRecomputeRequest{TenantID: fxTenant, AsOf: preAsOf, DueBefore: now.AddDate(0, 0, 1)})
	story.Assert("production vaccination-execution projector refreshed the serving version", err == nil, "err=%v", err)
	execRowsPre, err := fx.VaccExec.ListVaccinationExecution(fx.Ctx, vaccexecdomain.ExecutionQuery{TenantID: fxTenant, AsOf: preAsOf, DueBefore: now.AddDate(0, 0, 1), Limit: 10})
	story.Assert("execution rollup query ran (post-reject)", err == nil, "err=%v", err)
	story.Assert("coverage has NOT advanced after rejection (0 completed)", execCompletedForBatch(execRowsPre, batchID) == 0, "completed=%d", execCompletedForBatch(execRowsPre, batchID))

	story.Step("Operator reworks through the same SOP task with a fresh idempotency key",
		"After rework the operator records the dose again under a new idempotency key -- a distinct "+
			"administration attempt, not a replay of the rejected one.")
	second, err := sopService.SubmitTask(fx.Ctx, sopports.SubmitTaskCommand{
		TenantID: fxTenant, ActorID: operatorID, TaskID: taskID,
		Body: sopdomain.SubmitTaskRequest{SOPVersionID: canonicalVaccinationSOPVersion, IdempotencyKey: "story-l-attempt-2", Answers: answers, ProofRefs: proofRefs},
	}, "story-l-submit-2")
	story.Assert("rework administration recorded under a new key", err == nil, "err=%v", err)
	if err != nil {
		return
	}

	story.Step("Verifier ACCEPTS the rework: obligation completes, stock consumed, coverage counts",
		"The verifier accepts the re-worked dose. Only now does the obligation complete, the reserved dose "+
			"is consumed, and the execution rollup shows coverage advance to 1.")
	accepted, err := sopService.VerifyTask(fx.Ctx, sopports.ReviewTaskCommand{
		TenantID: fxTenant, ActorID: verifierID, TaskID: taskID,
		Body: sopdomain.ReviewTaskRequest{Reason: "corrected proof accepted", RowVersion: second.Task.RowVersion},
	}, "story-l-accept")
	story.Assert("SOP accept ran without error", err == nil, "err=%v", err)
	acceptedState := ""
	acceptedRowVersion := 0
	if accepted != nil {
		acceptedState = accepted.Task.State
		acceptedRowVersion = accepted.Task.RowVersion
	}
	story.Assert("the reworked dose was accepted and the obligation completed", err == nil && accepted.Task.State == "accepted", "task_state=%q row_version=%d", acceptedState, acceptedRowVersion)

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

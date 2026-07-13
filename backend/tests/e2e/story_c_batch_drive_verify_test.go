package e2e

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	pidomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	sophttp "github.com/vgoats/goatos/backend/internal/sop/adapters/http"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
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
	story.Certify("backend kernel + SOP proof/submission/review + authenticated admin-web HTTP review route")
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

	sopRepo := soppg.NewRepository(fx.Pool, 5*time.Second)
	sopService := sopapp.NewService(sopRepo)
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: sopService, actorID: operatorID}, fx.Inv)
	sweepCfg := oblapp.SweepConfig{SOPVersionID: canonicalVaccinationSOPVersion, VaccineItemID: itemID, DosesPerGoat: 1}
	dueBefore := now.AddDate(0, 0, 1)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, sweepCfg, dueBefore)
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("one shed = one drive: both obligations combined into a single batch", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)
	story.Assert("both obligations were attached to that drive", sweepRes.Obligations == 2, "obligations=%d", sweepRes.Obligations)

	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	taskID := fx.scanText(`SELECT sop_task_id::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2::uuid`, fxTenant, batchID)
	story.Assert("sweeper created the executable SOP task", taskID != "", "task_id=%q", taskID)

	story.Step("Capture canonical proof and submit both administrations",
		"The operator completes all three task-bound videos and the canonical SOP form. Submission fanout, not test code, records one completion per goat.")
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "story-c-proof-secret"))
	proofRefs := make([]sopdomain.ProofReference, 0, 3)
	proofIDs := make(map[string]string, 3)
	for _, subject := range []string{"shed", "vial_lot", "administration"} {
		var subjectID *string
		if subject == "shed" {
			subjectID = storyAAPtrString(shedID)
		}
		target, proofErr := proofService.CreateUpload(fx.Ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4", ScopeType: "task", ScopeID: taskID,
			SubjectType: subject, SubjectID: subjectID, UploadedBy: storyAAPtrString(operatorID), Metadata: map[string]any{"story": "C"},
		})
		story.Assert("proof registered for "+subject, proofErr == nil, "err=%v", proofErr)
		if proofErr != nil {
			continue
		}
		_, proofErr = proofService.StoreUpload(fx.Ctx, fxTenant, target.Proof.ProofID, "video/mp4", bytes.NewBufferString("story-c-"+subject))
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
	submitted, err := sopService.SubmitTask(fx.Ctx, sopports.SubmitTaskCommand{
		TenantID: fxTenant, ActorID: operatorID, TaskID: taskID,
		Body: sopdomain.SubmitTaskRequest{SOPVersionID: canonicalVaccinationSOPVersion, IdempotencyKey: "story-c-submit", ProofRefs: proofRefs,
			Answers: map[string]any{
				"vaccine_lot_id": lotID, "cold_chain_verified": true, "goat_ids": []any{goat1, goat2},
				"dose_ml_given": 1.0, "doses": 1, "route_site": "subcutaneous", "administered_at": now.Format(time.RFC3339),
				"adverse_reaction": false, "shed_video": proofIDs["shed"], "vial_lot_video": proofIDs["vial_lot"],
				"administration_video": proofIDs["administration"],
			}},
	}, "story-c-submit")
	story.Assert("canonical two-goat submission succeeded", err == nil, "err=%v", err)
	if err != nil {
		return
	}
	story.Assert("submission fanout recorded both doses", fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND batch_id=$2::uuid AND status='recorded'`, fxTenant, batchID) == 2, "batch=%s", batchID)

	story.Step("Control tower shows the open verification-pending alert",
		"Before the verifier reviews anything, the process-integrity control tower must show this "+
			"shed/rule row as verification_pending and NOT process-intact -- the real open gap a "+
			"director would see on Control Tower.")
	// Live control-tower read: as-of a small margin past "now" (skew-safe, after every seeded write)
	// but inside the projection freshness TTL. now()+hours would exceed the AGE-based buildAge gate
	// (a future as-of is clamped to now on the real HTTP path via ClampFutureAsOf, so it never reaches
	// the repo below the clamp); this test drives the repo directly, so it must honor the live contract.
	preAsOf := time.Now().UTC().Add(2 * time.Minute)
	_, err = fx.PI.RecomputeProjection(fx.Ctx, pidomain.ProjectionRecomputeRequest{TenantID: fxTenant, AsOf: preAsOf})
	story.Assert("production process-integrity projector refreshed the serving version", err == nil, "err=%v", err)
	if err != nil {
		return
	}
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
	// Drive the real per-shed execution projector before reading it (parity with the PI projector
	// recompute above); without a serving version the read model is honestly "unavailable".
	_, err = fx.VaccExec.RecomputeExecutionProjection(fx.Ctx, vaccexecdomain.ExecutionProjectionRecomputeRequest{TenantID: fxTenant, AsOf: preAsOf, DueBefore: now.AddDate(0, 0, 1)})
	story.Assert("production vaccination-execution projector refreshed the serving version", err == nil, "err=%v", err)
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

	story.Step("Director accepts the task through the admin-web HTTP review route",
		"One independent tenant-scoped admin-web review reaches the SOP handler, passes task.verify authorization, then fans out to both recorded completions, consuming reserved doses and completing both obligations.")
	grantSource := storyAAGrantSource{byActor: map[string][]permissions.ActiveGrant{
		verifierID: {{Role: permissions.RolePCDirector, ScopeType: "tenant", ScopeID: fxTenant}},
	}}
	auth, authErr := httpmiddleware.NewAuthMiddleware(httpmiddleware.AuthConfig{
		Mode: httpmiddleware.AuthModeDevHeaders, DevHeadersAllowed: true, Environment: "test",
	}, nil, grantSource, nil)
	if authErr != nil {
		t.Fatalf("build auth middleware: %v", authErr)
	}
	mux := http.NewServeMux()
	sophttp.Register(mux, sophttp.NewHandler(sopService))
	app := auth.Wrap(mux)
	body := fmt.Sprintf(`{"reason":"all proof accepted","row_version":%d}`, submitted.Task.RowVersion)
	req := httptest.NewRequest(http.MethodPost, "/admin/tasks/"+taskID+"/verify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpmiddleware.TenantContextHeader, fxTenant)
	req.Header.Set("X-GoatOS-Actor-ID", verifierID)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	story.Assert("Director admin-web review route succeeded", rec.Code == http.StatusOK, "HTTP=%d body=%s", rec.Code, rec.Body.String())
	if rec.Code != http.StatusOK {
		return
	}
	story.Assert("task accepted and both obligations completed", fx.scanText(`SELECT state FROM sop_tasks WHERE tenant_id=$1 AND task_id=$2::uuid`, fxTenant, taskID) == "accepted" && fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND batch_id=$2::uuid AND status='completed'`, fxTenant, batchID) == 2, "task=%s", taskID)

	story.Step("Control tower alert clears",
		"After both doses are verified, the same control-tower row must now read as completed and "+
			"process-intact -- the alert has cleared with no manual dismissal.")
	postAsOf := time.Now().UTC().Add(2 * time.Minute)
	_, err = fx.PI.RecomputeProjection(fx.Ctx, pidomain.ProjectionRecomputeRequest{TenantID: fxTenant, AsOf: postAsOf})
	story.Assert("production projector refreshed the accepted completion state", err == nil, "err=%v", err)
	if err != nil {
		return
	}
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

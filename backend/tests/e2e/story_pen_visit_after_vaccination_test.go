package e2e

import (
	"bytes"
	"strings"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	penvisitsboard "github.com/vgoats/goatos/backend/internal/penvisits/adapters/boardsource"
	penvisitspg "github.com/vgoats/goatos/backend/internal/penvisits/adapters/postgres"
	penvisitsproof "github.com/vgoats/goatos/backend/internal/penvisits/adapters/proof"
	penvisitsbridge "github.com/vgoats/goatos/backend/internal/penvisits/adapters/verificationbridge"
	penvisitsapp "github.com/vgoats/goatos/backend/internal/penvisits/app"
	penvisitsdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
	penvisitsports "github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
	workboarddomain "github.com/vgoats/goatos/backend/internal/workboard/domain"
	workboardports "github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// TestKernelStory_PenVisitAfterVaccination is the vaccination half of the 2026-09-12 rule: a
// vaccination shed proof raises the same next-day pen visit, linked to the shed's SOP
// submission, and the vaccination shed drilldown carries the visit as the pen's last step --
// owed, in review, verified -- while the drive's own five-bucket progress is untouched. On the
// visit's day the Work Board rows the visit under Vaccination, titled as the work continuing.
//
// The shed proof travels the production path (the SOP submission bridge with the verification
// producer wired, exactly as bootstrap/api.go composes it); the visit is materialized by the
// pen-visit repository's own write, and its verdict rides the durable bus.
func TestKernelStory_PenVisitAfterVaccination(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-pen-visit-after-vaccination",
		"A vaccinated pen owes the same next-day visit, and the shed card carries it as its last step",
		"An operator vaccinates a pen and submits the shed proof. The morning after, the pen owes a visit "+
			"to one of the park's configured visitors; the vaccination shed drilldown shows that step, the "+
			"Work Board rows it under Vaccination as the work continuing, and once the verifier approves the "+
			"visit the drilldown reads it as verified. The drive's own progress buckets never move for it.")
	defer story.Finish()
	story.Certify("backend kernel + SOP shed proof + pen visits + vaccination execution read")

	ctx := fx.Ctx
	const (
		shedID     = "7f000000-0000-4000-8000-0000000f0201"
		stageID    = "7f000000-0000-4000-8000-0000000f0202"
		operatorID = "7f000000-0000-4000-8000-0000000f0203"
		parkHeadID = "7f000000-0000-4000-8000-0000000f0204"
		verifierID = "7f000000-0000-4000-8000-0000000f0205"
		goatID     = "7f000000-0000-4000-8000-0000000f0206"
		itemID     = "7f000000-0000-4000-8000-0000000f0208"
		lotID      = "7f000000-0000-4000-8000-0000000f0209"
		visitorID  = "7f000000-0000-4000-8000-0000000f0210"
	)

	story.Step("Seed shed, workforce, protocol, one due goat, vaccine stock, and the park's visitor",
		"The Story L topology, plus one person configured on /people to walk this park's pens.")
	fx.SeedShed(shedID, "E2E-PVV", stageID)
	fx.SeedWorkforce(operatorID, parkHeadID, verifierID, shedID)
	fx.exec("visitor",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, $1, 'VIS-E2E', 'Chandrakant', 'active', 'pc_director', $3)`, visitorID, fxTenant, fxPark)
	fx.exec("park visitor", `INSERT INTO pen_visit_park_assignees (tenant_id, park_id, user_id) VALUES ($1, $2, $3)`, fxTenant, fxPark, visitorID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_pvv", 21, 0, nil)
	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-PVV', 'E2E pen-visit vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, lotID, fxTenant, itemID, fxPark)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	if _, err := gen.GenerateForVersion(ctx, fxTenant, versionID, now); err != nil {
		t.Fatalf("generate: %v", err)
	}
	sopRepo := soppg.NewRepository(fx.Pool, 5*time.Second)
	sopService := sopapp.NewService(sopRepo)
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: sopService, actorID: operatorID}, fx.Inv)
	if _, err := sweeper.SweepVersion(ctx, fxTenant, versionID, oblapp.SweepConfig{SOPVersionID: canonicalVaccinationSOPVersion, VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	taskID := fx.scanText(`SELECT sop_task_id::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2::uuid`, fxTenant, batchID)
	story.Assert("the sweeper created the shed's executable SOP task", taskID != "", "task_id=%q", taskID)

	verification := verificationapp.NewService(fx.VerifRepo, fx.VerifMedia)
	if err := verification.RegisterCategory(verificationcatalog.Vaccination); err != nil {
		t.Fatalf("register vaccination category: %v", err)
	}
	if err := verification.RegisterCategory(verificationcatalog.PenVisit); err != nil {
		t.Fatalf("register pen visit category: %v", err)
	}
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "story-pvv-proof-secret"))
	captureVideo := func(scopeType, scopeID, subjectType, subjectID, by, key string) string {
		t.Helper()
		target, err := proofService.CreateUpload(ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4", ScopeType: scopeType, ScopeID: scopeID,
			SubjectType: subjectType, SubjectID: storyAAPtrString(subjectID), UploadedBy: storyAAPtrString(by),
			Metadata: map[string]any{"capture_source": "in_app_camera", "captured_start_ms": int64(1000), "captured_end_ms": int64(5200), "story": "PVV:" + key},
		})
		if err != nil {
			t.Fatalf("create upload %s: %v", key, err)
		}
		stored, err := proofService.StoreUpload(ctx, fxTenant, target.Proof.ProofID, "video/mp4", bytes.NewBufferString("story-pvv-"+key))
		if err != nil {
			t.Fatalf("store upload %s: %v", key, err)
		}
		duration := int64(4200)
		if _, err := proofService.CompleteUpload(ctx, proofdomain.CompleteUpload{TenantID: fxTenant, ProofID: target.Proof.ProofID, ContentHash: stored.ContentHash, MimeType: stored.MimeType, SizeBytes: stored.SizeBytes, DurationMS: &duration}); err != nil {
			t.Fatalf("complete upload %s: %v", key, err)
		}
		return target.Proof.ProofID
	}

	story.Step("The shed proof reaches the verifier as ONE shed-level item filed against the SOP submission",
		"This is the row the pen-visit kernel reads: the vaccination submission bridge files it with "+
			"category vaccination_proof and source ref sop_submission (sopbridge/vaccination_submission.go). "+
			"The SOP task's own readiness gate is outside this story (Story L owns it, and is red on this "+
			"clock for its own reasons), so the item is filed here with the bridge's exact source shape.")
	submissionID := "7f000000-0000-4000-8000-0000000f0301"
	subjectLabel := "1 goat · E2E-PVV"
	shedRef, parkRef, opRef := shedID, fxPark, operatorID
	if _, err := verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID: fxTenant, Vertical: "preventive_care", Module: "vaccination", Category: sopbridge.VaccinationVerificationCategory,
		SubjectLabel: &subjectLabel,
		Source:       verificationdomain.SourceRef{Module: "vaccination", TaskID: &taskID, SubmissionID: &submissionID, RefType: "sop_submission", RefID: submissionID},
		MediaRefs:    []string{captureVideo("task", taskID, "goat", goatID, operatorID, "shed-1")},
		OperatorID:   &opRef, ShedID: &shedRef, ParkID: &parkRef, CapturedAt: time.Now().UTC(),
		IdempotencyKey: "vaccination:submission:" + submissionID + ":group",
	}); err != nil {
		t.Fatalf("file shed proof item: %v", err)
	}
	filed := fx.countRows(`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND category='vaccination_proof' AND source_ref_type='sop_submission' AND source_ref_id=$2::uuid AND shed_id=$3::uuid`, fxTenant, submissionID, shedID)
	story.Assert("the shed proof's verification item names the pen and the submission", filed == 1, "items=%d", filed)

	story.Step("The morning after, the pen owes its visit -- linked to the SOP submission",
		"One visit, reason 'vaccination', due tomorrow, linked to the sop_submission the shed proof filed.")
	penRepo := penvisitspg.NewRepository(fx.Pool, 10*time.Second)
	today := biztime.BusinessDate(time.Now())
	tomorrow := biztime.BusinessDayStart(time.Now()).AddDate(0, 0, 1).Format("2006-01-02")
	result, _, err := penRepo.Materialize(ctx, fxTenant, today, tomorrow, time.Now())
	story.Assert("one visit was raised for the vaccinated pen", err == nil && result.Created == 1, "err=%v result=%+v", err, result)
	linked, err := penRepo.ForSources(ctx, fxTenant, penvisitsdomain.SourceKindVaccinationSubmission, []string{submissionID})
	visit, ok := linked[submissionID]
	story.Assert("the visit is linked to the shed's SOP submission", err == nil && ok && len(visit.Reasons) == 1 && visit.Reasons[0] == penvisitsdomain.ReasonVaccination, "err=%v ok=%v visit=%+v", err, ok, visit)
	if !ok {
		return
	}

	story.Step("The visit is the visitor's own task on the Tasks module; the shed drilldown and the drive's buckets do not carry it",
		"penvisits.ListMine serves the configured visitor's 'For me' list (maintainer decision 2026-09-14: a task of its own, "+
			"never a step on the vaccination shed card); vaccinationexecution.ShedDrilldown reads no visit at all.")
	execService := vaccexecapp.NewService(fx.VaccExec)
	asOf := time.Now().UTC().Add(2 * time.Minute)
	query := vaccexecdomain.ExecutionQuery{TenantID: fxTenant, ShedID: storyAAPtrString(shedID), AsOf: asOf, DueBefore: now.AddDate(0, 0, 2), Limit: 20}
	before, found, err := execService.ShedDrilldown(ctx, query)
	story.Assert("the drilldown resolves the pen", err == nil && found, "err=%v found=%v", err, found)
	penVisits := penvisitsapp.NewService(penRepo).WithProofValidator(penvisitsproof.NewValidator(fx.Proof))
	mine, err := penVisits.ListMine(ctx, fxTenant, visitorID, "todo", 20, "")
	story.Assert("the visitor's For me list carries the visit, owed today, recordable by them", err == nil && len(mine.Rows) == 1 && mine.Rows[0].TaskID == visit.TaskID && mine.Rows[0].CanSubmit(penvisitsdomain.Actor{UserID: visitorID}) && !mine.Rows[0].IsVerified(), "err=%v rows=%+v", err, mine.Rows)

	story.Step("On the visit's day the Work Board rows it under Tasks, as a task of its own",
		"ONE pen-visit source rows every visit under the Tasks module -- never Preventive Care or Vaccination -- with no href (visits are phone-only).")
	boardQuery := workboardports.SourceQuery{TenantID: fxTenant, ParkID: fxPark, BusinessDate: tomorrow, Limit: 50}
	vaccRows, err := penvisitsboard.New(fx.Pool, 10*time.Second).ListRows(ctx, boardQuery)
	story.Assert("the Tasks source rows the visit, titled as the visit, the vaccination in its subtitle", err == nil && len(vaccRows) == 1 && vaccRows[0].Module == workboarddomain.ModuleTasks && vaccRows[0].Title == "Pen visit · E2E-PVV" && strings.HasPrefix(vaccRows[0].Subtitle, "Vaccination · work done ") && vaccRows[0].Href == "" && vaccRows[0].WorkState == workboarddomain.WorkStateDue, "err=%v rows=%+v", err, vaccRows)

	story.Step("The visitor records the visit, the verifier approves it, the For me list reads it as verified",
		"Submit -> pen_visit.submitted -> the durable enqueue consumer -> ONE verifier item; approve ->"+
			" the verdict applier -> the visit is verified on both dimensions.")
	penvisitsapp.NewPendingVerificationHandler(penvisitsbridge.New(verification), nil).Register(fx.Bus)
	relay := func() {
		for i := 0; i < 2; i++ {
			time.Sleep(1500 * time.Millisecond)
			fx.RelayOutboxEvents()
		}
	}
	submitted, err := penVisits.Submit(ctx, penvisitsports.SubmitParams{TenantID: fxTenant, Actor: penvisitsdomain.Actor{UserID: visitorID}, TaskID: visit.TaskID, ProofRef: captureVideo("task", visit.TaskID, "other", visit.TaskID, visitorID, "visit-1"), RowVersion: visit.RowVersion, IdempotencyKey: "pvv-visit-1", TraceID: "pvv-visit-1"})
	story.Assert("the visit is submitted for review", err == nil && submitted.Status == penvisitsdomain.StatusPendingVerification, "err=%v visit=%+v", err, submitted)
	relay()
	inReview, err := penVisits.GetTask(ctx, fxTenant, penvisitsdomain.Actor{UserID: visitorID}, visit.TaskID)
	story.Assert("the visit reads in review and is no longer recordable", err == nil && inReview.Status == penvisitsdomain.StatusPendingVerification && !inReview.CanSubmit(penvisitsdomain.Actor{UserID: visitorID}), "err=%v visit=%+v", err, inReview)
	var itemIDStr, rowVersion string
	if err := fx.Pool.QueryRow(ctx, `SELECT item_id::text, row_version::text FROM verification_items WHERE tenant_id=$1 AND source_module=$2 AND source_ref_id=$3::uuid ORDER BY created_at DESC LIMIT 1`, fxTenant, penvisitsapp.VerificationModule, visit.TaskID).Scan(&itemIDStr, &rowVersion); err != nil {
		t.Fatalf("visit verification item: %v", err)
	}
	item, err := verification.GetItem(ctx, fxTenant, itemIDStr)
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if _, err := verification.RecordVerdict(ctx, verificationdomain.Verdict{TenantID: fxTenant, ItemID: item.ItemID, Decision: verificationdomain.DecisionApproved, VerifierID: verifierID, RowVersion: item.RowVersion, IdempotencyKey: "pvv-verdict-1"}); err != nil {
		t.Fatalf("verdict: %v", err)
	}
	relay()
	verifiedVisit, err := penVisits.GetTask(ctx, fxTenant, penvisitsdomain.Actor{UserID: visitorID}, visit.TaskID)
	story.Assert("the visit reads verified once the verifier approves it", err == nil && verifiedVisit.IsVerified() && penvisitsdomain.StateChip(verifiedVisit, tomorrow) == "Visit verified", "err=%v visit=%+v", err, verifiedVisit)
	after, found, err := execService.ShedDrilldown(ctx, query)
	story.Assert("the drive's own buckets did not move for the visit", err == nil && found && after.Summary == before.Summary, "after=%+v before=%+v", after.Summary, before.Summary)
	doneRows, err := penvisitsboard.New(fx.Pool, 10*time.Second).ListRows(ctx, boardQuery)
	story.Assert("the board's visit row is Done", err == nil && len(doneRows) == 1 && doneRows[0].WorkState == workboarddomain.WorkStateCompleted, "err=%v rows=%+v", err, doneRows)
}

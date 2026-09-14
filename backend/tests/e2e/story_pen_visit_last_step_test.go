package e2e

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	fwrdomain "github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
	fwrports "github.com/vgoats/goatos/backend/internal/feedwaterremoval/ports"
	pccareboard "github.com/vgoats/goatos/backend/internal/pccare/adapters/boardsource"
	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
	pccareproof "github.com/vgoats/goatos/backend/internal/pccare/adapters/proof"
	pccarebridge "github.com/vgoats/goatos/backend/internal/pccare/adapters/verificationbridge"
	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	pccaredomain "github.com/vgoats/goatos/backend/internal/pccare/domain"
	pccareports "github.com/vgoats/goatos/backend/internal/pccare/ports"
	penvisitsboard "github.com/vgoats/goatos/backend/internal/penvisits/adapters/boardsource"
	penvisitspg "github.com/vgoats/goatos/backend/internal/penvisits/adapters/postgres"
	penvisitsproof "github.com/vgoats/goatos/backend/internal/penvisits/adapters/proof"
	penvisitsbridge "github.com/vgoats/goatos/backend/internal/penvisits/adapters/verificationbridge"
	penvisitsapp "github.com/vgoats/goatos/backend/internal/penvisits/app"
	penvisitsdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
	penvisitsports "github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
	workboarddomain "github.com/vgoats/goatos/backend/internal/workboard/domain"
	workboardports "github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// TestKernelStory_PenVisitIsTheCareWorksLastStep is the production-path proof of the maintainer's
// 2026-09-12 rule: the day-after pen visit is the LAST STEP of the care work done in a pen -- not a
// task of its own -- and the care task closes only when EVERY video of its chain is verified.
//
// Every state transition below is produced by the same code production runs:
//
//	PLAN / SCAN / RECORD / SUBMIT   pccare app.Service (operators' writes, the CEO's plan)
//	VERIFIER ITEM                    pccare verificationbridge -> verification.CreateItem
//	VERDICT                          verification app.Service.RecordVerdict -> outbox
//	APPLY                            the durable domain bus (eventwiring.RegisterVerificationAppliers)
//	THE VISIT                        penvisits Materialize (the kernel stage's write), Submit,
//	                                 pen_visit.submitted -> PendingVerificationHandler -> CreateItem,
//	                                 verdict -> PenVisitVerificationHandler -> pen_visit.verified
//	                                 -> PenVisitVerifiedHandler -> pccare.PenVisitVerified
//	THE BOARD                        the PC Care and pen-visit Work Board sources
//
// The cases it walks, in order: the deworming's own clips verified BEFORE the visit exists (the
// task must NOT read done); the visit raised, listed for BOTH configured visitors, recorded by
// the second one; the verifier REJECTS the visit (the task stays open, the visitor records
// again); the re-shoot APPROVED (the task closes, once, with its audit row); then a SECOND task
// whose visit is verified BEFORE its own clips (the other order converges); and a hoof-trimming
// task whose own clips are never submitted (the visit alone never closes it).
func TestKernelStory_PenVisitIsTheCareWorksLastStep(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-pen-visit-last-step",
		"The pen visit is the care work's last step; the task closes only when every video is verified",
		"Deworming is done in a pen and submitted with its videos. The verifier approves them -- but the "+
			"task is not done: the day after, one of the park's configured visitors walks the pen and records "+
			"one video, and THAT goes to the verifier too. Only when the visit is approved does the deworming "+
			"task close. A rejected visit sends the visitor back; a visit approved before the work's own clips "+
			"waits for them; a task whose own work was never verified is never closed by a visit alone. And "+
			"the Work Board follows the step on both days.")
	defer story.Finish()
	story.Certify("backend kernel + verification + pen visits + work board")

	ctx := fx.Ctx
	const (
		shedID     = "7e000000-0000-4000-8000-0000000e0201"
		stageID    = "7e000000-0000-4000-8000-0000000e02a1"
		operatorID = "7e000000-0000-4000-8000-0000000e0301" // works the deworming
		parkHeadID = "7e000000-0000-4000-8000-0000000e0302"
		verifierID = "7e000000-0000-4000-8000-0000000e0303"
		ceoID      = "7e000000-0000-4000-8000-0000000e0304"
		dinakarID  = "7e000000-0000-4000-8000-0000000e0305" // configured visitor #1
		secondID   = "7e000000-0000-4000-8000-0000000e0306" // configured visitor #2
		strangerID = "7e000000-0000-4000-8000-0000000e0307" // nobody's visitor
	)

	// ---------------------------------------------------------------------------
	story.Step("A pen, an operator, a verifier and TWO configured visitors",
		"The park's visitors are HRMS config (pen_visit_park_assignees), one row per person: either "+
			"of the two may record a visit. A third person with no row is nobody's visitor.")
	fx.SeedShed(shedID, "E2E-PV", stageID)
	fx.SeedWorkforce(operatorID, parkHeadID, verifierID, shedID)
	fx.exec("operator login", `UPDATE workforce_members SET user_id = $1 WHERE workforce_member_id = $1`, operatorID)
	fx.exec("operator park grant",
		`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
		 VALUES ($1, $2, 'operator', 'park', $3, 'active', now())`, fxTenant, operatorID, fxPark)
	for _, v := range []struct{ id, code, name string }{{dinakarID, "DIN-E2E", "Dinakar"}, {secondID, "SEC-E2E", "Second Visitor"}, {strangerID, "STR-E2E", "Stranger"}} {
		fx.exec("visitor "+v.name,
			`INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
			 VALUES ($1, $2, $1, $3, $4, 'active', 'pc_director', $5)`, v.id, fxTenant, v.code, v.name, fxPark)
	}
	fx.exec("park visitors",
		`INSERT INTO pen_visit_park_assignees (tenant_id, park_id, user_id) VALUES ($1, $2, $3), ($1, $2, $4)`,
		fxTenant, fxPark, dinakarID, secondID)

	// Services, composed the way bootstrap/api.go composes them.
	verification := verificationapp.NewService(fx.VerifRepo, fx.VerifMedia)
	for _, def := range verificationcatalog.PCCare() {
		if err := verification.RegisterCategory(def); err != nil {
			t.Fatalf("register pc care category: %v", err)
		}
	}
	if err := verification.RegisterCategory(verificationcatalog.PenVisit); err != nil {
		t.Fatalf("register pen visit category: %v", err)
	}
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "story-pv-proof-secret"))
	pcRepo := pccarepg.NewRepository(fx.Pool, 10*time.Second)
	penRepo := penvisitspg.NewRepository(fx.Pool, 10*time.Second)
	today := biztime.BusinessDate(time.Now())
	tomorrow := biztime.BusinessDayStart(time.Now()).AddDate(0, 0, 1).Format("2006-01-02")
	pcNow := biztime.BusinessDayStart(time.Now()).Add(9 * time.Hour)
	pcCare := pccareapp.NewService(pcRepo).
		WithRoundStore(pcRepo).
		WithProofValidator(pccareproof.NewValidator(fx.Proof)).
		WithVerificationEnqueuer(pccarebridge.New(verification)).
		WithFeedWaterRemovalCutoff(fwrports.StaticCutoff{Cutoff: fwrdomain.MustCutoff(20, 0)}).
		WithNow(func() time.Time { return pcNow })
	penVisits := penvisitsapp.NewService(penRepo).WithProofValidator(penvisitsproof.NewValidator(fx.Proof))
	// The two pen-visit consumers production registers on every durable bus: the submit ->
	// verifier-item enqueue (kernelstages/bus.go, the relay, the consumer) rides fx.Bus here;
	// the verdict applier and the parent closer are already on it through
	// eventwiring.RegisterVerificationAppliers (domainconsumer wiring).
	penvisitsapp.NewPendingVerificationHandler(penvisitsbridge.New(verification), nil).Register(fx.Bus)
	// PC Care's own submit -> verifier-item enqueue rides the same durable event shape.
	pccareapp.NewPCCarePendingVerificationHandler(pccarebridge.New(verification), nil).Register(fx.Bus)

	// The outbox stamps next_attempt_at with the DATABASE clock and the relay claims with the
	// test process's; against a remote Postgres the two can differ by a second, so a row written
	// a moment ago is not yet claimable. Settling for that skew is harness truth, not product.
	// Two rounds, because a hop can write the NEXT hop's row during the drain (the verdict
	// applier emits pen_visit.verified), and that row is behind the same skew.
	relay := func() {
		for i := 0; i < 2; i++ {
			time.Sleep(1500 * time.Millisecond)
			fx.RelayOutboxEvents()
		}
	}

	ceo := pccaredomain.Actor{TenantID: fxTenant, UserID: ceoID, Roles: []string{permissions.RoleCEOInternal}}
	operator := pccaredomain.Actor{TenantID: fxTenant, UserID: operatorID, Roles: []string{permissions.RoleOperator}}

	captureVideo := func(scopeType, scopeID, by, key string) string {
		t.Helper()
		target, err := proofService.CreateUpload(ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4", ScopeType: scopeType, ScopeID: scopeID,
			SubjectType: "other", UploadedBy: storyAAPtrString(by),
			Metadata: map[string]any{"capture_source": "in_app_camera", "captured_start_ms": int64(1000), "captured_end_ms": int64(6000), "story": "PV:" + key},
		})
		if err != nil {
			t.Fatalf("create upload %s: %v", key, err)
		}
		stored, err := proofService.StoreUpload(ctx, fxTenant, target.Proof.ProofID, "video/mp4", bytes.NewBufferString("story-pv-"+key))
		if err != nil {
			t.Fatalf("store upload %s: %v", key, err)
		}
		duration := int64(5000)
		if _, err := proofService.CompleteUpload(ctx, proofdomain.CompleteUpload{
			TenantID: fxTenant, ProofID: target.Proof.ProofID, ContentHash: stored.ContentHash,
			MimeType: stored.MimeType, SizeBytes: stored.SizeBytes, DurationMS: &duration,
		}); err != nil {
			t.Fatalf("complete upload %s: %v", key, err)
		}
		return target.Proof.ProofID
	}
	planAndSubmit := func(category, key string) pccareports.TaskRow {
		t.Helper()
		task, err := pcCare.CreateTask(ctx, ceo, pccareapp.CreateTaskInput{
			Category: category, ParkID: fxPark, ShedID: shedID, PlannedBusinessDate: today,
			AssigneeUserIDs: []string{operatorID}, IdempotencyKey: "pv-plan-" + key, ActorID: ceoID, ActorType: "human", TraceID: "pv-plan-" + key,
		})
		if err != nil {
			t.Fatalf("plan %s: %v", key, err)
		}
		scan, err := pcCare.ScanAnimal(ctx, operator, pccareapp.ScanAnimalInput{TaskID: task.TaskID, ScannedIdentifier: "RFID-PV-" + key, IdempotencyKey: "pv-scan-" + key, ActorID: operatorID, ActorType: "operator"})
		if err != nil {
			t.Fatalf("scan %s: %v", key, err)
		}
		slots := pccaredomain.SlotsForCategory(category)
		for i, slot := range slots {
			proof := captureVideo("task", task.TaskID, operatorID, fmt.Sprintf("%s-%d", key, i))
			if err := pcCare.RegisterSlotProof(ctx, operator, pccareapp.RegisterSlotProofInput{TaskID: task.TaskID, AnimalRowID: scan.AnimalRowID, SlotFieldKey: slot.FieldKey, ProofRef: proof, IdempotencyKey: fmt.Sprintf("pv-slot-%s-%d", key, i), ActorID: operatorID, ActorType: "operator"}); err != nil {
				t.Fatalf("slot %s/%s: %v", key, slot.FieldKey, err)
			}
		}
		if _, err := pcCare.SubmitTask(ctx, operator, pccareapp.SubmitTaskInput{TaskID: task.TaskID, IdempotencyKey: "pv-submit-" + key, ActorID: operatorID, ActorType: "operator", TraceID: "pv-submit-" + key}); err != nil {
			t.Fatalf("submit %s: %v", key, err)
		}
		// The submit announces pc_care.task.pending_verification; the durable consumer mints
		// the verifier item.
		relay()
		return task
	}
	itemFor := func(module, refType, refID string) verificationdomain.Item {
		t.Helper()
		var itemID string
		if err := fx.Pool.QueryRow(ctx, `SELECT item_id::text FROM verification_items WHERE tenant_id=$1 AND source_module=$2 AND source_ref_type=$3 AND source_ref_id=$4::uuid ORDER BY created_at DESC LIMIT 1`, fxTenant, module, refType, refID).Scan(&itemID); err != nil {
			t.Fatalf("verification item for %s/%s %s: %v", module, refType, refID, err)
		}
		item, err := verification.GetItem(ctx, fxTenant, itemID)
		if err != nil {
			t.Fatalf("get item %s: %v", itemID, err)
		}
		return item
	}
	verdict := func(item verificationdomain.Item, decision, reason, key string) {
		t.Helper()
		if _, err := verification.RecordVerdict(ctx, verificationdomain.Verdict{TenantID: fxTenant, ItemID: item.ItemID, Decision: decision, Reason: reason, VerifierID: verifierID, RowVersion: item.RowVersion, IdempotencyKey: "pv-verdict-" + key}); err != nil {
			t.Fatalf("verdict %s: %v", key, err)
		}
		relay()
	}
	taskStates := func(taskID string) (status, workState string) {
		t.Helper()
		if err := fx.Pool.QueryRow(ctx, `SELECT status, work_state FROM pc_care_tasks WHERE tenant_id=$1 AND task_id=$2::uuid`, fxTenant, taskID).Scan(&status, &workState); err != nil {
			t.Fatalf("read task %s: %v", taskID, err)
		}
		return status, workState
	}

	// ---------------------------------------------------------------------------
	story.Step("Deworming is planned, done, submitted and its clips approved -- the task is NOT done",
		"The verifier approves the deworming clips the same day. status = completed (the field work "+
			"is verified), but the kernel clock stays open: the pen still owes tomorrow's visit.")
	deworming := planAndSubmit(pccaredomain.CategoryDeworming, "dw")
	verdict(itemFor("pc_care", "pc_care_task", deworming.TaskID), verificationdomain.DecisionApproved, "", "dw-1")
	status, workState := taskStates(deworming.TaskID)
	story.Assert("the deworming's own videos are verified", status == pccaredomain.StatusCompleted, "status=%q", status)
	story.Assert("but the task's clock stays OPEN for the visit", workState == pccaredomain.WorkStateScheduled, "work_state=%q", workState)
	rowAfterOwnApproval, err := pcCare.GetTask(ctx, ceo, deworming.TaskID)
	story.Assert("the task read shows the task's own state only (the visit is a task of its own, 2026-09-14)", err == nil && rowAfterOwnApproval.Status == pccaredomain.StatusCompleted && rowAfterOwnApproval.WorkState == pccaredomain.WorkStateScheduled, "err=%v row=%+v", err, rowAfterOwnApproval)
	sweep, err := pcRepo.SweepTaskRollForward(ctx, fxTenant, biztime.BusinessDayStart(time.Now()).AddDate(0, 0, 3), 200, 50)
	story.Assert("the kernel roll-forward does not roll a verified task as late work", err == nil && sweep.RolledForward == 0, "err=%v rolled=%d", err, sweep.RolledForward)

	boardQuery := func(date, owner string) workboardports.SourceQuery {
		return workboardports.SourceQuery{TenantID: fxTenant, ParkID: fxPark, BusinessDate: date, OwnerUserID: owner, Limit: 50}
	}
	pcSource := pccareBoardSource(fx)
	rowsToday, err := pcSource.ListRows(ctx, boardQuery(today, ""))
	story.Assert("the Work Board row for the task reads DONE -- the operator's own work is verified; the visit rows on its own under Tasks", err == nil && boardState(rowsToday, deworming.TaskID) == workboarddomain.WorkStateCompleted && boardSubtitle(rowsToday, deworming.TaskID) == "1 animal",
		"err=%v state=%q subtitle=%q", err, boardState(rowsToday, deworming.TaskID), boardSubtitle(rowsToday, deworming.TaskID))

	// ---------------------------------------------------------------------------
	story.Step("The morning after, the kernel raises the pen's visit, linked to the deworming",
		"penvisits.Materialize reads the day's verification items (the deworming's submit) and "+
			"writes one visit per pen, due tomorrow, linked to the task it closes. A replay writes nothing.")
	result, digests, err := penRepo.Materialize(ctx, fxTenant, today, tomorrow, time.Now())
	story.Assert("one visit was raised for the pen", err == nil && result.Created == 1, "err=%v result=%+v", err, result)
	story.Assert("the digest names both configured visitors", len(digests) == 1 && len(digests[0].VisitorIDs) == 2, "digests=%+v", digests)
	replay, _, err := penRepo.Materialize(ctx, fxTenant, today, tomorrow, time.Now())
	story.Assert("a replay tick raises nothing new", err == nil && replay.Created == 0, "err=%v replay=%+v", err, replay)
	linked, err := penRepo.ForSources(ctx, fxTenant, penvisitsdomain.SourceKindPCCareTask, []string{deworming.TaskID})
	visit, ok := linked[deworming.TaskID]
	story.Assert("the visit is linked to the deworming task", err == nil && ok, "err=%v linked=%v", err, ok)
	if !ok {
		return
	}
	story.Assert("the visit is owed tomorrow, open on both dimensions", visit.DueDate == tomorrow && visit.WorkState == penvisitsdomain.WorkStateScheduled && visit.Status == penvisitsdomain.StatusOpen, "visit=%+v", visit)

	story.Step("Both configured visitors see it on their For me list; a stranger sees nothing; no PC Care worklist carries it",
		"The visit list is 'the parks I am configured for' (the Tasks module's For me tab). The PC Care "+
			"worklist is the assignee's own list and never admits a visitor for a task they were not assigned.")
	dinakarList, err := penVisits.ListMine(ctx, fxTenant, dinakarID, "todo", 20, "")
	secondList, err2 := penVisits.ListMine(ctx, fxTenant, secondID, "todo", 20, "")
	strangerList, err3 := penVisits.ListMine(ctx, fxTenant, strangerID, "todo", 20, "")
	story.Assert("both configured visitors list the visit", err == nil && err2 == nil && len(dinakarList.Rows) == 1 && len(secondList.Rows) == 1, "err=%v/%v rows=%d/%d", err, err2, len(dinakarList.Rows), len(secondList.Rows))
	story.Assert("a person nobody configured lists nothing", err3 == nil && len(strangerList.Rows) == 0, "err=%v rows=%d", err3, len(strangerList.Rows))
	secondActor := pccaredomain.Actor{TenantID: fxTenant, UserID: secondID, Roles: []string{permissions.RolePCDirector}}
	pcNow = biztime.BusinessDayStart(time.Now()).AddDate(0, 0, 1).Add(8 * time.Hour)
	visitorWorklist, err := pcCare.Worklist(ctx, secondActor, pccaredomain.CategoryDeworming, tomorrow, "", 20)
	story.Assert("the visitor's deworming worklist does NOT carry the task (the visit lives on Tasks)", err == nil && !hasTask(visitorWorklist.Items, deworming.TaskID), "err=%v items=%d", err, len(visitorWorklist.Items))
	operatorCarry, err := pcCare.Worklist(ctx, operator, pccaredomain.CategoryDeworming, tomorrow, "", 20)
	story.Assert("the operator's carry list no longer shows the verified task as work owed", err == nil && !hasTask(operatorCarry.Items, deworming.TaskID), "err=%v items=%d", err, len(operatorCarry.Items))
	visitRows, err := penvisitsboard.New(fx.Pool, 10*time.Second).ListRows(ctx, boardQuery(tomorrow, secondID))
	story.Assert("on the visit's day the board rows the visit under TASKS as a task of its own, on the visitor's own board naming THEM first", err == nil && len(visitRows) == 1 && visitRows[0].Module == workboarddomain.ModuleTasks && visitRows[0].Title == "Pen visit · E2E-PV" && visitRows[0].WorkState == workboarddomain.WorkStateDue && visitRows[0].Owner.Name == "Second Visitor +1" && visitRows[0].Subtitle == "Deworming · work done "+biztime.FarmDateFromBusinessDate(today),
		"err=%v rows=%+v", err, visitRows)
	unscopedRows, err := penvisitsboard.New(fx.Pool, 10*time.Second).ListRows(ctx, boardQuery(tomorrow, ""))
	story.Assert("on the park's board the first configured visitor leads, with +1 for the other", err == nil && len(unscopedRows) == 1 && unscopedRows[0].Owner.Name == "Dinakar +1", "err=%v rows=%+v", err, unscopedRows)
	strangerRows, err := penvisitsboard.New(fx.Pool, 10*time.Second).ListRows(ctx, boardQuery(tomorrow, strangerID))
	story.Assert("the stranger's board carries no visit", err == nil && len(strangerRows) == 0, "err=%v rows=%d", err, len(strangerRows))

	// ---------------------------------------------------------------------------
	story.Step("The SECOND visitor records the visit; the clip goes to the verifier; the task is still open",
		"Any configured person may go ('if anyone does then enough'). Submit locks the visit "+
			"pending_verification, the outbox announces pen_visit.submitted, and the production consumer "+
			"turns that into ONE verifier item. Nothing closes yet.")
	if _, err := penVisits.Submit(ctx, penvisitsports.SubmitParams{TenantID: fxTenant, Actor: penvisitsdomain.Actor{UserID: strangerID}, TaskID: visit.TaskID, ProofRef: captureVideo("task", visit.TaskID, strangerID, "stranger"), IdempotencyKey: "pv-visit-stranger"}); err == nil {
		story.Assert("a stranger's submit is refused", false, "err=nil")
	} else {
		story.Assert("a stranger's submit is refused", true, "")
	}
	submitted, err := penVisits.Submit(ctx, penvisitsports.SubmitParams{TenantID: fxTenant, Actor: penvisitsdomain.Actor{UserID: secondID}, TaskID: visit.TaskID, ProofRef: captureVideo("task", visit.TaskID, secondID, "visit-1"), RowVersion: visit.RowVersion, IdempotencyKey: "pv-visit-1", TraceID: "pv-visit-1"})
	story.Assert("the second visitor's submit lands as pending verification on an open clock", err == nil && submitted.Status == penvisitsdomain.StatusPendingVerification && submitted.WorkState == penvisitsdomain.WorkStateScheduled, "err=%v visit=%+v", err, submitted)
	relay()
	visitItem := itemFor(penvisitsapp.VerificationModule, penvisitsapp.VerificationRefType, visit.TaskID)
	story.Assert("the verifier holds ONE pen-visit item for the clip, in the Preventive Care tab", visitItem.Category == penvisitsapp.VerificationCategory && len(visitItem.MediaRefs) == 1, "item=%+v", visitItem)
	status, workState = taskStates(deworming.TaskID)
	story.Assert("the deworming task is still open while the visit is with the verifier", status == pccaredomain.StatusCompleted && workState == pccaredomain.WorkStateScheduled, "status=%q work_state=%q", status, workState)
	rowsToday, err = pcSource.ListRows(ctx, boardQuery(today, ""))
	story.Assert("the task's own board row stays DONE; the review is the VISIT row's, under Tasks", err == nil && boardState(rowsToday, deworming.TaskID) == workboarddomain.WorkStateCompleted, "err=%v state=%q", err, boardState(rowsToday, deworming.TaskID))
	visitRowsInReview, err := penvisitsboard.New(fx.Pool, 10*time.Second).ListRows(ctx, boardQuery(tomorrow, ""))
	story.Assert("the visit's Tasks row reads IN REVIEW", err == nil && len(visitRowsInReview) == 1 && visitRowsInReview[0].WorkState == workboarddomain.WorkStateVerificationPending, "err=%v rows=%+v", err, visitRowsInReview)

	// ---------------------------------------------------------------------------
	story.Step("The verifier REJECTS the visit: the visitor records again, the task waits",
		"Reject flips the visit to rework with the verifier's words; the task's clock stays open; the "+
			"re-shoot carries a fresh row version and mints a fresh verifier item.")
	verdict(visitItem, verificationdomain.DecisionRejected, "pen not in frame", "visit-1")
	bounced, err := penRepo.GetTask(ctx, fxTenant, visit.TaskID)
	story.Assert("the visit is sent back with the verifier's words", err == nil && bounced.Status == penvisitsdomain.StatusRework && bounced.ReworkReason == "pen not in frame", "err=%v visit=%+v", err, bounced)
	status, workState = taskStates(deworming.TaskID)
	story.Assert("the deworming task is still open after the visit was sent back", status == pccaredomain.StatusCompleted && workState == pccaredomain.WorkStateScheduled, "status=%q work_state=%q", status, workState)
	visitRowsRejected, err := penvisitsboard.New(fx.Pool, 10*time.Second).ListRows(ctx, boardQuery(tomorrow, ""))
	story.Assert("the visit's Tasks row reads REJECTED", err == nil && len(visitRowsRejected) == 1 && visitRowsRejected[0].WorkState == workboarddomain.WorkStateRejected, "err=%v rows=%+v", err, visitRowsRejected)
	reshot, err := penVisits.Submit(ctx, penvisitsports.SubmitParams{TenantID: fxTenant, Actor: penvisitsdomain.Actor{UserID: dinakarID}, TaskID: visit.TaskID, ProofRef: captureVideo("task", visit.TaskID, dinakarID, "visit-2"), RowVersion: bounced.RowVersion, IdempotencyKey: "pv-visit-2", TraceID: "pv-visit-2"})
	story.Assert("the first visitor's re-shoot is accepted for review", err == nil && reshot.Status == penvisitsdomain.StatusPendingVerification && reshot.RowVersion > bounced.RowVersion, "err=%v visit=%+v", err, reshot)
	relay()
	items := fx.countRows(`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_module=$2 AND source_ref_id=$3::uuid`, fxTenant, penvisitsapp.VerificationModule, visit.TaskID)
	story.Assert("the re-shoot minted a SECOND verifier item (a retry would have collapsed)", items == 2, "items=%d", items)

	// ---------------------------------------------------------------------------
	story.Step("The verifier APPROVES the re-shoot: the visit is verified and the deworming task CLOSES",
		"pen_visit.verified rides the approve transaction; the durable consumer closes the linked task "+
			"(work_state completed, once, with an audit row). A redelivery changes nothing.")
	verdict(itemFor(penvisitsapp.VerificationModule, penvisitsapp.VerificationRefType, visit.TaskID), verificationdomain.DecisionApproved, "", "visit-2")
	verified, err := penRepo.GetTask(ctx, fxTenant, visit.TaskID)
	story.Assert("the visit is verified on both dimensions", err == nil && verified.IsVerified() && verified.WorkState == penvisitsdomain.WorkStateCompleted, "err=%v visit=%+v", err, verified)
	status, workState = taskStates(deworming.TaskID)
	story.Assert("the deworming task is CLOSED now -- every video of its chain is verified", status == pccaredomain.StatusCompleted && workState == pccaredomain.WorkStateCompleted, "status=%q work_state=%q", status, workState)
	closeAudits := fx.countRows(`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_type='pc_care_task' AND resource_id=$2 AND action='pc_care.task.pen_visit_verified'`, fxTenant, deworming.TaskID)
	story.Assert("exactly one closure audit row", closeAudits == 1, "audits=%d", closeAudits)
	relay()
	closeAudits = fx.countRows(`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_type='pc_care_task' AND resource_id=$2 AND action='pc_care.task.pen_visit_verified'`, fxTenant, deworming.TaskID)
	story.Assert("a redelivered pen_visit.verified is a no-op", closeAudits == 1, "audits=%d", closeAudits)
	rowsToday, err = pcSource.ListRows(ctx, boardQuery(today, ""))
	story.Assert("the task's board row reads DONE at last", err == nil && boardState(rowsToday, deworming.TaskID) == workboarddomain.WorkStateCompleted, "err=%v state=%q", err, boardState(rowsToday, deworming.TaskID))
	finalRow, err := pcCare.GetTask(ctx, ceo, deworming.TaskID)
	story.Assert("the task read's clock is closed at last", err == nil && finalRow.WorkState == pccaredomain.WorkStateCompleted, "err=%v row=%+v", err, finalRow)
	finalVisit, err := penVisits.GetTask(ctx, fxTenant, penvisitsdomain.Actor{UserID: dinakarID}, visit.TaskID)
	story.Assert("the visit itself reads verified on the visitor's own list", err == nil && finalVisit.IsVerified(), "err=%v visit=%+v", err, finalVisit)
	var completedEvents int
	if err := fx.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='pc_care.task.completed' AND aggregate_id=$2::uuid`, fxTenant, deworming.TaskID).Scan(&completedEvents); err != nil {
		t.Fatalf("count completed events: %v", err)
	}
	story.Assert("pc_care.task.completed fired once, at the task's own approval", completedEvents == 1, "events=%d", completedEvents)

	// ---------------------------------------------------------------------------
	story.Step("The OTHER order: a ticks-removal task whose visit is verified before its own clips",
		"Same pen, same day, a second category. Its visit is the SAME visit row (one pen, one day, one "+
			"visit) -- already verified. When the verifier finally approves the ticks clips, the task closes "+
			"in that one step, because the visit is already in.")
	pcNow = biztime.BusinessDayStart(time.Now()).Add(10 * time.Hour)
	ticks := planAndSubmit(pccaredomain.CategoryTicksRemoval, "tk")
	late, _, err := penRepo.Materialize(ctx, fxTenant, today, tomorrow, time.Now())
	story.Assert("the late submit links onto the pen's ONE (already verified) visit rather than raising another", err == nil && late.Created == 0, "err=%v result=%+v", err, late)
	linkedTicks, err := penRepo.ForSources(ctx, fxTenant, penvisitsdomain.SourceKindPCCareTask, []string{ticks.TaskID})
	story.Assert("the ticks task is linked to the same visit", err == nil && linkedTicks[ticks.TaskID].TaskID == visit.TaskID, "err=%v linked=%+v", err, linkedTicks)
	status, workState = taskStates(ticks.TaskID)
	story.Assert("the ticks task is still with the verifier (a verified visit never closes unverified work)", status == pccaredomain.StatusPendingVerification && workState == pccaredomain.WorkStateScheduled, "status=%q work_state=%q", status, workState)
	verdict(itemFor("pc_care", "pc_care_task", ticks.TaskID), verificationdomain.DecisionApproved, "", "tk-1")
	status, workState = taskStates(ticks.TaskID)
	story.Assert("approving the ticks clips closes the task in one step -- its visit was already verified", status == pccaredomain.StatusCompleted && workState == pccaredomain.WorkStateCompleted, "status=%q work_state=%q", status, workState)

	// ---------------------------------------------------------------------------
	story.Step("A hoof-trimming task never submitted is never closed by the visit alone",
		"Planned in the same pen, nobody filmed it. The verified visit closes nothing it is not entitled to.")
	hoof, err := pcCare.CreateTask(ctx, ceo, pccareapp.CreateTaskInput{Category: pccaredomain.CategoryHoofTrimming, ParkID: fxPark, ShedID: shedID, PlannedBusinessDate: today, AssigneeUserIDs: []string{operatorID}, IdempotencyKey: "pv-plan-hoof", ActorID: ceoID, ActorType: "human"})
	story.Assert("the hoof-trimming task is planned", err == nil, "err=%v", err)
	if err == nil {
		status, workState = taskStates(hoof.TaskID)
		story.Assert("it is open and untouched by the pen's verified visit", status == pccaredomain.StatusOpen && workState == pccaredomain.WorkStateScheduled, "status=%q work_state=%q", status, workState)
	}
}

// pccareBoardSource builds the PC Care Work Board source over the fixture pool.
func pccareBoardSource(fx *Fixture) workboardports.Source {
	return pccareBoardSourceFor(fx.Pool)
}

func boardState(rows []workboarddomain.Row, sourceID string) workboarddomain.WorkState {
	for _, r := range rows {
		if r.SourceID == sourceID {
			return r.WorkState
		}
	}
	return ""
}

func boardSubtitle(rows []workboarddomain.Row, sourceID string) string {
	for _, r := range rows {
		if r.SourceID == sourceID {
			return r.Subtitle
		}
	}
	return ""
}

func hasTask(rows []pccareports.TaskRow, taskID string) bool {
	for _, r := range rows {
		if r.TaskID == taskID {
			return true
		}
	}
	return false
}

func pccareBoardSourceFor(pool *pgxpool.Pool) workboardports.Source {
	return pccareboard.New(pool, 10*time.Second)
}

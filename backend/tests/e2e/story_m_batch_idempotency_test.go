package e2e

import (
	"errors"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryM_BatchIdempotency drives the drive-submission idempotency contract through the
// real SOP submission entrypoint and VaccinationSubmissionBridge. It covers the three cases the
// platform mandates for every write: first submit materializes one recorded dose; exact replay
// returns the original submission with no duplicate completion; same key + DIFFERENT payload is
// rejected with no new side effect. A retried mobile drive submit must never double-record a goat.
func TestKernelStoryM_BatchIdempotency(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-m", "Batch idempotency: re-submit a drive, no duplicate completions",
		"A drive submission carries an idempotency key per goat dose. The kernel must make re-submission "+
			"safe: the first submit records the completion; an exact replay of the same submit returns the "+
			"original completion without creating a duplicate; a replay that reuses the key with a different "+
			"payload is rejected outright. Flaky networks and retried mobile submits can never double-count "+
			"a vaccination.")
	defer story.Finish()
	story.Certify("backend kernel")

	const (
		shedID     = "ed000000-0000-4000-8000-000000000001"
		stageID    = "ed000000-0000-4000-8000-000000000002"
		operatorID = "ed000000-0000-4000-8000-000000000003"
		parkHeadID = "ed000000-0000-4000-8000-000000000004"
		verifierID = "ed000000-0000-4000-8000-000000000005"
		goatID     = "ed000000-0000-4000-8000-000000000006"
		itemID     = "ed000000-0000-4000-8000-000000000008"
		lotID      = "ed000000-0000-4000-8000-000000000009"
	)

	story.Step("Build a real one-shed drive through generation and sweep",
		"One shed/workforce/protocol/goat/stock topology, generated and swept into a single drive batch "+
			"-- the same real path Story C uses.")
	fx.SeedShed(shedID, "E2E-M", stageID)
	fx.SeedWorkforce(operatorID, parkHeadID, verifierID, shedID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_m", 21, 0, nil)

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-M', 'E2E Story M vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, lotID, fxTenant, itemID, shedID)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	if _, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now); err != nil {
		t.Fatalf("generate: %v", err)
	}
	sopHarness := newVaccinationSOPHarness(t, fx)
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: sopHarness.Service, actorID: operatorID}, fx.Inv)
	if _, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{
		SOPVersionID: canonicalVaccinationSOPVersion, VaccineItemID: itemID, DosesPerGoat: 1,
	}, now.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	oblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	taskID := fx.scanText(`SELECT sop_task_id::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2::uuid`, fxTenant, batchID)
	story.Assert("sweeper created the executable SOP task", taskID != "", "task_id=%q", taskID)

	const key = "e2e-story-m-submit"
	body, err := sopHarness.submissionRequest(taskID, shedID, operatorID, lotID, []string{goatID}, now, key, "subcutaneous")
	if err != nil {
		t.Fatalf("create canonical proof/submission body: %v", err)
	}
	submit := func(request sopdomain.SubmitTaskRequest, trace string) (*sopdomain.SubmissionResponse, error) {
		return sopHarness.Service.SubmitTask(fx.Ctx, sopports.SubmitTaskCommand{
			TenantID: fxTenant, ActorID: operatorID, TaskID: taskID, Body: request,
		}, trace)
	}

	story.Step("First submit applies",
		"Canonical proof and the per-goat form enter through SOP SubmitTask; the submission bridge records exactly one completion.")
	first, err := submit(body, "story-m-first")
	firstSubmissionID := ""
	if first != nil {
		firstSubmissionID = first.Submission.SubmissionID
	}
	story.Assert("first SOP submit recorded a completion", err == nil && firstSubmissionID != "", "submission_id=%q err=%v", firstSubmissionID, err)
	if err != nil {
		return
	}
	afterFirst := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2::uuid`, fxTenant, oblID)
	story.Assert("exactly one completion row exists", afterFirst == 1, "rows=%d", afterFirst)

	story.Step("Exact replay is a durable no-op",
		"Re-submitting the identical SOP payload under the same key returns the original submission and does not re-run a completed fanout.")
	replay, err := submit(body, "story-m-replay")
	story.Assert("exact replay ran without error", err == nil, "err=%v", err)
	replaySubmissionID := ""
	if replay != nil {
		replaySubmissionID = replay.Submission.SubmissionID
	}
	story.Assert("exact replay returned the original SOP submission", replaySubmissionID == firstSubmissionID,
		"first=%q replay=%q", firstSubmissionID, replaySubmissionID)
	afterReplay := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2::uuid`, fxTenant, oblID)
	story.Assert("still exactly one completion row after exact replay", afterReplay == 1, "rows=%d", afterReplay)
	story.Assert("still exactly one durable SOP submission", fx.countRows(`SELECT count(*) FROM sop_submissions WHERE tenant_id=$1 AND task_id=$2::uuid AND idempotency_key=$3`, fxTenant, taskID, key) == 1,
		"task=%s key=%s", taskID, key)

	story.Step("Same key, DIFFERENT payload is rejected",
		"Reusing the SOP key with a changed route/site must be rejected with an idempotency conflict and must not mutate the bridged completion.")
	conflictBody := body
	conflictBody.Answers = make(map[string]any, len(body.Answers))
	for field, value := range body.Answers {
		conflictBody.Answers[field] = value
	}
	conflictBody.Answers["route_site"] = "intramuscular"
	_, err = submit(conflictBody, "story-m-conflict")
	var conflictErr *sopapp.Error
	story.Assert("same-key-different-payload is rejected with the SOP idempotency_conflict contract",
		errors.As(err, &conflictErr) && conflictErr.Code == "idempotency_conflict", "err=%v", err)
	afterConflict := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2::uuid`, fxTenant, oblID)
	story.Assert("still exactly one completion row after the rejected replay", afterConflict == 1, "rows=%d", afterConflict)
	routeStored := fx.scanText(`SELECT route_site FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2::uuid`, fxTenant, oblID)
	story.Assert("the original payload is intact (route unchanged by the rejected replay)", routeStored == "subcutaneous", "route_site=%q", routeStored)

	story.Step("No duplicate obligation completion downstream",
		"The obligation still maps to exactly one recorded completion -- the drive cannot double-complete "+
			"the goat no matter how many times it is re-submitted.")
	oblCompletions := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("exactly one completion for the obligation", oblCompletions == 1, "rows=%d", oblCompletions)
}

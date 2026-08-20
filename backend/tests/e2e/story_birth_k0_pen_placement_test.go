package e2e

import (
	"testing"
	"time"

	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	tasksports "github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// TestKernelStory_NewbornPenIsRecordedWhenTheParkHasNoKidPen is the production-path proof for the
// FALLBACK half of the 2026-08-20 maintainer rule: a newborn is placed in its park's K0 pen, and
// when the park has no K0 pen the birth is still recorded and the kid's care workflow carries a
// "Record shed" step that finishes the placement.
//
// The defect this closes: the birth form offered the FULL park -> shed -> pen cascade and the birth
// write validated nothing about the shed, so a K0 kid could be filed into a Buck pen, an F2 pen or
// an ICU pen. The rejection half is proved against the real handler in
// counts/adapters/http/app_birth_placement_test.go. THIS story proves the half that only a database
// can prove: that completing the fallback step actually moves the animal, tags the pen, writes the
// animal's own history, emits the location event, and makes the park's NEXT birth place
// automatically -- all in one transaction.
//
//	PRODUCER  tasks.OpenBirthWorkflows -> repository.needsShedPlacement (ground truth: is this kid
//	          already in a K0 pen?) -> TemplateBirthKidAt(needsShedPlacement)
//	          tasks.AnswerAction(record_shed) -> identity.RelocateGoatsToShedInTx
//	          • goat_identity_events + goats.shed_id + goat_shed_partitions + goat_location_history
//	          • per-animal goat.location.changed outbox row
//	          • shed_partitions.animal_stage_id <- K0, in the SAME transaction
//
// Nothing derived is seeded: the workflow, its steps, the placement, the history row, the outbox row
// and the pen tag are all PRODUCED by the same service the app calls. Only external input facts
// (park, sheds, pens, stage vocabulary, the kid itself) are inserted.
func TestKernelStory_NewbornPenIsRecordedWhenTheParkHasNoKidPen(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-birth-k0-pen-placement",
		"A newborn's pen is recorded when its park has no kid pen",
		"A newborn belongs in its park's KID PEN — a pen whose authored tag is K0. When the park has "+
			"none, the birth is still recorded (a birth is a real event and losing it over missing "+
			"configuration would make the herd register lie) and the kid's care workflow carries a "+
			"'Record shed' step. Completing that step moves the kid into the pen the operator names, "+
			"tags that pen K0, writes the animal's own location history and emits its location event — "+
			"all in one transaction — so the park's NEXT birth places automatically and opens with no "+
			"such step.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx
	// A FIXED business date. Vaccination time grain is the business DAY, never an hour offset from
	// now, so a fixture anchored to the clock would pass or fail depending on when it ran.
	bornOn := time.Date(2026, 8, 20, 0, 0, 0, 0, biztime.DefaultLocation())

	const (
		buckShedID  = "00000000-0000-4000-8000-0000000b0001"
		k0StageID   = "00000000-0000-4000-8000-0000000b0011"
		buckStageID = "00000000-0000-4000-8000-0000000b0012"
		firstKid    = "00000000-0000-4000-8000-0000000b0101"
		secondKid   = "00000000-0000-4000-8000-0000000b0102"
		operatorID  = "00000000-0000-4000-8000-0000000b0201"
	)

	// ---------------------------------------------------------------- setup (external facts only)
	story.Step("A park whose only pens are a Buck pen and an untagged pen",
		"Godel 1 is a partitioned shed. 'Part 1' is tagged Buck; 'Part 2' carries no tag at all. "+
			"Neither is a kid pen, so this park cannot place a newborn automatically.")

	seedCohortShed(fx, buckShedID, "GODEL-1", buckStageID, "Buck", "adult")
	// The human shed name the operator reads, distinct from its location_code. "Godel 1" ends in a
	// digit and is a WHOLE shed name, not a partition -- exactly the case that must never render as
	// "Godel - 1".
	fx.exec("shed display name",
		`UPDATE locations SET name = 'Godel 1' WHERE tenant_id=$1 AND location_id=$2`, fxTenant, buckShedID)
	fx.exec("k0 stage vocabulary",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status, age_band)
		 VALUES ($1, $2, 'K0', 'K0 newborns', 'active', 'kid')`,
		k0StageID, fxTenant)
	// The pen CATALOG: 'Part 1' is a Buck pen, 'Part 2' is deliberately untagged. partition_label is
	// the HUMAN label; normalized_label is the internal matching key. Rendering the key would show
	// the operator "Godel 1 - 1" instead of "Godel 1 - Part 1".
	fx.exec("buck pen",
		`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, animal_stage_id, status, source)
		 VALUES ($1, $2, 'Part 1', '1', $3, 'active', 'manual')`,
		fxTenant, buckShedID, buckStageID)
	fx.exec("untagged pen",
		`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, animal_stage_id, status, source)
		 VALUES ($1, $2, 'Part 2', '2', NULL, 'active', 'manual')`,
		fxTenant, buckShedID)

	// The kid, placed where the operator could reach: the Buck pen. That is exactly the defect the
	// rejection half now prevents at raise time, and exactly the state this fallback repairs.
	fx.SeedGoat(GoatSpec{
		GoatID: firstKid, ShedID: buckShedID, Stage: "K0", AgeBand: "kid",
		Breed: "beetal", DOB: &bornOn, EntryDate: &bornOn, OriginType: "birth",
	})
	fx.exec("first kid pen residency",
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
		 VALUES ($1, $2, $3, 'Part 1', 'Godel 1')`,
		fxTenant, firstKid, buckShedID)

	// The SAME wiring bootstrap/api.go and eventwiring use: the tasks repository carrying identity's
	// transaction seam. Nothing here is a test double.
	tasksRepo := taskspg.NewRepository(fx.Pool, 10*time.Second).
		WithIdentityTxWriter(identitypg.NewRepository(fx.Pool, 10*time.Second))
	tasks := tasksapp.NewService(tasksRepo, nil)

	// ------------------------------------------------------------------- the step is owed and open
	story.Step("The kid's care workflow carries a Record shed step",
		"Whether the step is needed is read from GROUND TRUTH — the pen the kid is actually in — not "+
			"from whether the park had a kid pen at raise time. The kid sits in a Buck pen, so the "+
			"placement is owed.")

	openErr := tasks.OpenBirthWorkflows(ctx, tasksapp.OpenBirthWorkflowsInput{
		TenantID: fxTenant, GoatID: firstKid, OccurredAt: time.Now().UTC(),
	})
	story.Assert("the birth workflow opened", openErr == nil, "err=%v", openErr)

	workflowID, actionID := findRecordShedAction(fx, firstKid)
	story.Assert("the kid's workflow carries a Record shed step", actionID != "",
		"workflow_id=%s action_id=%s", workflowID, actionID)

	// -------------------------------------------------------------------------- completing the step
	story.Step("Completing it places the kid and tags the pen",
		"The operator names the untagged pen 'Godel 1 - Part 2'. One transaction moves the kid, "+
			"upserts its pen residency, writes its location history, emits goat.location.changed, and "+
			"tags that pen K0.")

	// Record shed is the LAST immediate main step, so the medical work comes first: a kid is cleaned,
	// dipped, fed and weighed in the minutes after delivery, and a pen question must not hold that
	// behind a configuration gap. The sequence gate is real, so the story walks it through the same
	// service the phone calls rather than reaching around it.
	completeMainStepsBefore(fx, tasks, workflowID, actionID, operatorID)

	part2 := "Part 2"
	answer := tasksdomain.FormatRecordedPenAnswer(buckShedID, &part2)
	_, answerErr := tasks.AnswerAction(ctx, tasksapp.AnswerActionInput{
		TenantID: fxTenant, WorkflowID: workflowID, ActionID: actionID,
		AnswerValue: answer, AnsweredBy: operatorID,
		IdempotencyKey: "e2e-record-shed-1", RequestFingerprint: "fp-1",
	})
	story.Assert("the step was accepted", answerErr == nil, "err=%v", answerErr)

	residency := fx.scanText(
		`SELECT COALESCE(partition_label,'') FROM goat_shed_partitions WHERE tenant_id=$1 AND goat_id=$2`,
		fxTenant, firstKid)
	story.Assert("the kid now lives in the pen the operator named", residency == "Part 2",
		"partition_label=%s", residency)

	// THE OUTPUT STRING, on a DB round trip. Not field presence, and not a pure-Go formatter test —
	// both weaker forms have passed in this repository while real output was wrong.
	shedName := fx.scanText(`SELECT name FROM locations WHERE tenant_id=$1 AND location_id=$2`, fxTenant, buckShedID)
	display := oploc.OperationalLocation{ShedName: shedName, PartitionLabel: residency}.Display()
	story.Assert("its operational location reads as the farm writes it", display == "Godel 1 - Part 2",
		"display=%q (the human label, never the normalized matching key)", display)

	penTag := fx.scanText(`
SELECT COALESCE(lookup.stage_code,'')
FROM shed_partitions pen
LEFT JOIN animal_stage_lookup lookup
       ON lookup.tenant_id = pen.tenant_id AND lookup.animal_stage_id = pen.animal_stage_id
WHERE pen.tenant_id=$1 AND pen.shed_id=$2 AND pen.normalized_label='2'`, fxTenant, buckShedID)
	story.Assert("the pen is now the park's kid pen", penTag == "K0", "stage_code=%s", penTag)

	buckTag := fx.scanText(`
SELECT COALESCE(lookup.stage_code,'')
FROM shed_partitions pen
LEFT JOIN animal_stage_lookup lookup
       ON lookup.tenant_id = pen.tenant_id AND lookup.animal_stage_id = pen.animal_stage_id
WHERE pen.tenant_id=$1 AND pen.shed_id=$2 AND pen.normalized_label='1'`, fxTenant, buckShedID)
	story.Assert("a pen that already had a tag is left alone", buckTag == "Buck",
		"stage_code=%s (a placement must not re-purpose a pen somebody configured)", buckTag)

	history := fx.scanText(`
SELECT count(*)::text FROM goat_location_history
WHERE tenant_id=$1 AND goat_id=$2 AND reason='newborn_pen_recorded'`, fxTenant, firstKid)
	story.Assert("the move is on the animal's own timeline", history == "1", "rows=%s", history)

	events := fx.scanText(`
SELECT count(*)::text FROM outbox_messages
WHERE tenant_id=$1 AND event_type='goat.location.changed' AND aggregate_id=$2`, fxTenant, firstKid)
	story.Assert("a per-animal location event was published", events == "1",
		"rows=%s (vaccination re-keys the kid's schedule from this event)", events)

	// -------------------------------------------------------------------------------- idempotency
	story.Step("Completing it twice does not place the kid twice",
		"The action row is the idempotency gate: an exact replay returns the original result and a "+
			"completed step refuses a second write, so the relocation cannot run again.")

	_, replayErr := tasks.AnswerAction(ctx, tasksapp.AnswerActionInput{
		TenantID: fxTenant, WorkflowID: workflowID, ActionID: actionID,
		AnswerValue: answer, AnsweredBy: operatorID,
		IdempotencyKey: "e2e-record-shed-1", RequestFingerprint: "fp-1",
	})
	story.Assert("the exact replay is accepted without re-running the placement", replayErr == nil,
		"err=%v", replayErr)

	replayHistory := fx.scanText(`
SELECT count(*)::text FROM goat_location_history
WHERE tenant_id=$1 AND goat_id=$2 AND reason='newborn_pen_recorded'`, fxTenant, firstKid)
	story.Assert("still exactly one location-history row", replayHistory == "1", "rows=%s", replayHistory)

	replayEvents := fx.scanText(`
SELECT count(*)::text FROM outbox_messages
WHERE tenant_id=$1 AND event_type='goat.location.changed' AND aggregate_id=$2`, fxTenant, firstKid)
	story.Assert("still exactly one location event", replayEvents == "1", "rows=%s", replayEvents)

	// --------------------------------------------------------------------------- the payoff: next
	story.Step("The park's next birth needs no Record shed step",
		"The fallback tagged a pen K0, so a kid born into it is already correctly placed. The step is "+
			"resolved from ground truth, which is what makes this self-healing rather than a one-off "+
			"repair.")

	fx.SeedGoat(GoatSpec{
		GoatID: secondKid, ShedID: buckShedID, Stage: "K0", AgeBand: "kid",
		Breed: "beetal", DOB: &bornOn, EntryDate: &bornOn, OriginType: "birth",
	})
	fx.exec("second kid placed in the now-K0 pen",
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
		 VALUES ($1, $2, $3, 'Part 2', 'Godel 1')`,
		fxTenant, secondKid, buckShedID)

	nextErr := tasks.OpenBirthWorkflows(ctx, tasksapp.OpenBirthWorkflowsInput{
		TenantID: fxTenant, GoatID: secondKid, OccurredAt: time.Now().UTC(),
	})
	story.Assert("the second birth workflow opened", nextErr == nil, "err=%v", nextErr)

	_, nextAction := findRecordShedAction(fx, secondKid)
	story.Assert("it carries NO Record shed step", nextAction == "",
		"action_id=%s (a kid already in a kid pen is finished with placement)", nextAction)

	_ = tasksports.Repository(tasksRepo) // the story drives the real port, not a narrowed test seam
}

// findRecordShedAction returns the kid workflow id and its record_shed action id ("" when the step
// is absent). It reads the rows the production open path wrote; nothing is seeded here.
func findRecordShedAction(f *Fixture, goatID string) (workflowID, actionID string) {
	f.T.Helper()
	workflowID = f.scanText(`
SELECT COALESCE(min(workflow_id::text),'') FROM workflow_instances
WHERE tenant_id=$1 AND subject_goat_id=$2 AND template_key='birth_kid'`, fxTenant, goatID)
	if workflowID == "" {
		return "", ""
	}
	actionID = f.scanText(`
SELECT COALESCE(min(action_id::text),'') FROM workflow_actions
WHERE tenant_id=$1 AND workflow_id=$2 AND action_key='record_shed'`, fxTenant, workflowID)
	return workflowID, actionID
}

// completeMainStepsBefore drives every main-section operator step ahead of `stopAtActionID` through
// the production answer/complete service, so the placement step reaches its own write with the
// sequence gate genuinely satisfied. Nothing is seeded: each row is written by the same code path
// the operator's phone calls.
func completeMainStepsBefore(f *Fixture, tasks *tasksapp.Service, workflowID, stopAtActionID, operatorID string) {
	f.T.Helper()
	rows, err := f.Pool.Query(f.Ctx, `
SELECT action_id::text, action_key, action_type, requires_video, seq
FROM workflow_actions
WHERE tenant_id=$1 AND workflow_id=$2 AND section='main' AND action_type <> 'approval'
ORDER BY seq`, fxTenant, workflowID)
	if err != nil {
		f.T.Fatalf("list main actions: %v", err)
	}
	type step struct {
		id, key, kind string
		needsVideo    bool
		seq           int
	}
	var steps []step
	for rows.Next() {
		var st step
		if err := rows.Scan(&st.id, &st.key, &st.kind, &st.needsVideo, &st.seq); err != nil {
			rows.Close()
			f.T.Fatalf("scan main action: %v", err)
		}
		steps = append(steps, st)
	}
	rows.Close()

	for _, st := range steps {
		if st.id == stopAtActionID {
			return
		}
		proof := ""
		if st.needsVideo {
			proof = "proof/e2e-birth-" + st.key + ".mp4"
		}
		key := "e2e-birth-prereq-" + st.key
		var stepErr error
		switch st.kind {
		case tasksdomain.ActionTypeQuestion, tasksdomain.ActionTypeQuestionSelect:
			answer := "yes"
			if st.key == tasksdomain.ActionKeyTakeWeight {
				answer = "3.2"
			}
			_, stepErr = tasks.AnswerAction(f.Ctx, tasksapp.AnswerActionInput{
				TenantID: fxTenant, WorkflowID: workflowID, ActionID: st.id,
				AnswerValue: answer, ProofRef: proof, AnsweredBy: operatorID,
				IdempotencyKey: key, RequestFingerprint: key,
			})
		default:
			_, stepErr = tasks.CompleteAction(f.Ctx, tasksapp.CompleteActionInput{
				TenantID: fxTenant, WorkflowID: workflowID, ActionID: st.id,
				ProofRef: proof, CompletedBy: operatorID,
				IdempotencyKey: key, RequestFingerprint: key,
			})
		}
		if stepErr != nil {
			f.T.Fatalf("prerequisite step %s: %v", st.key, stepErr)
		}
	}
}

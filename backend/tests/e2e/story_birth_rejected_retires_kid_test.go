package e2e

import (
	"encoding/json"
	"testing"
	"time"

	countsboardsource "github.com/vgoats/goatos/backend/internal/counts/adapters/boardsource"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/eventwiring"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	workboardports "github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// TestKernelStory_RejectedBirthCancelsTheWorkAndRetiresTheKid is the production-path proof of the
// 2026-09-25 maintainer decision that SUPERSEDES the "Web approval rejects | Existing children remain
// canonical but count-ineligible | Work remains available" row of docs/decisions/birth-death-workflows.md.
//
// Live repro on the throwaway clone: a birth was raised (kid with temp tag CBE-42777), the relay
// opened the birth_mother (6 actions) and birth_kid (18 actions) workflows, the approver REJECTED
// it -- and both workflows stayed open with pending steps while the kid stayed alive in the herd.
//
//	PRODUCER  counts.ApprovalService.SubmitBirthRequest -> identity CreateAdminGoatInTx
//	          (goat.created, counts.birth.reported) -> relay -> birth workflows + capture items
//	          counts.ApprovalService.Decide(reject) -> ONE transaction:
//	            goat_births.count_status=rejected, each kid retired through identity's terminal
//	            exit (goat.exited, lifecycle inactive, exit_reason recorded_in_error, identifiers
//	            retired), counts.birth.rejected
//	          relay -> tasks cancels the birth workflows and withdraws their pending step items;
//	                   counts withdraws the report's pending capture items
//
// Only external input facts are inserted (sheds, the mother). Every workflow, action, verification
// item, identifier and outbox row is produced by the same services production runs.
func TestKernelStory_RejectedBirthCancelsTheWorkAndRetiresTheKid(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-birth-rejected-retires-kid",
		"A rejected birth cancels its follow-up work and retires the kid",
		"When the approver rejects a birth report, the operator's kid and mother care workflows are "+
			"cancelled, any proof still waiting for the verifier is withdrawn, and the kid the report "+
			"created leaves the live register through identity's normal exit -- kept for audit, its "+
			"temporary tag retired so it can never be scanned again.")
	defer story.Finish()
	story.Certify("backend kernel")
	ctx := fx.Ctx

	const (
		kidShed    = "00000000-0000-4000-8000-0000000c0001"
		k0StageID  = "00000000-0000-4000-8000-0000000c0011"
		motherID   = "00000000-0000-4000-8000-0000000c0101"
		operatorID = "00000000-0000-4000-8000-0000000c0201"
		approverID = "00000000-0000-4000-8000-0000000c0202"
		tempTag    = "CBE-42777"
		captureRef = "00000000-0000-4000-8000-0000000c0301"
	)
	bornOn := time.Date(2026, 9, 24, 0, 0, 0, 0, biztime.DefaultLocation())
	seedCohortShed(fx, kidShed, "KIDPEN-1", k0StageID, "K0", "kid")
	motherDOB := bornOn.AddDate(-3, 0, 0)
	fx.SeedGoat(GoatSpec{GoatID: motherID, ShedID: kidShed, Stage: "Mother", AgeBand: "adult", Breed: "beetal", DOB: &motherDOB, EntryDate: &motherDOB})

	identityRepo := identitypg.NewRepository(fx.Pool, 10*time.Second)
	countsRepo := countspg.NewRepository(fx.Pool, 10*time.Second).
		WithIdentityTxWriter(identityRepo).
		WithDeathEvidenceTxGate(taskspg.NewRepository(fx.Pool, 10*time.Second))
	approvals := countsapp.NewApprovalService(countsRepo, fx.Identity, nil)
	workflows := eventwiring.NewWorkflowConsumerService(fx.Pool, 10*time.Second, nil)

	// ------------------------------------------------------------------------- report the birth
	story.Step("The operator reports a single-kid birth",
		"The report creates the kid in the register (count-ineligible until approved) with a temporary tag, "+
			"and carries one form photo for the verifier.")
	stage, litter := "K0", 1
	mother := motherID
	child := identityports.CreateAdminGoatCommand{
		TenantID: fxTenant, ActorID: operatorID, ClientIdempotencyKey: "birth-reject-child-1",
		StoredIdempotencyKey: fxTenant + ":identity.admin.goat_create:birth-reject-child-1",
		IdempotencyScope:     "identity.admin.goat_create", RequestHash: "hash-birth-reject-child-1", TraceID: "trace-e2e-birth-reject",
		Identifiers: []identityports.AdminGoatCreateIdentifier{{
			IdentifierType: "temporary_tag", IdentifierValue: tempTag, NormalizedValue: tempTag, ScopeKey: "global", IsPrimary: true,
		}},
		CustodianPartyID: fxParty, ParkID: fxPark, ShedID: kidShed,
		Species: "goat", Breed: strPtrE2E("beetal"), Sex: "female", DOB: &bornOn, OriginType: "birth", EntryDate: bornOn,
		ManagementStage: &stage, DamID: &mother, LitterSize: &litter,
		EvidenceRefs: []identitydomain.EvidenceRef{},
	}
	payload, _ := json.Marshal(map[string]any{"species": "goat", "park_id": fxPark, "shed_id": kidShed, "dam_id": motherID})
	submitted, err := approvals.SubmitBirthRequest(ctx, countsdomain.ApprovalRequestSubmission{
		TenantID: fxTenant, RequestType: countsdomain.ApprovalRequestTypeBirth, Payload: payload,
		RaisedByUserID: operatorID, RaisedAt: bornOn.Add(9 * time.Hour),
		IdempotencyKey: "birth-reject-submit-1", RequestFingerprint: "birth-reject-submit-1-fp",
		Capture: &countsdomain.ApprovalCapture{
			Proofs: authored.ProofRefs{"newborn_photo": captureRef},
			Evidence: authored.Evidence{Media: []authored.EvidenceMedia{{
				Key: "newborn_photo", Ref: captureRef, Kind: "photo", Label: "Newborn with the mother",
			}}},
		},
	}, []identityports.CreateAdminGoatCommand{child})
	if !story.Assert("the birth was submitted", err == nil && len(submitted.Children) == 1, "err=%v", err) {
		return
	}
	requestID := submitted.Approval.ApprovalRequestID
	kidID := submitted.Children[0].GoatID
	relayUntilSettled(fx)

	openFlows := fx.scanText(`SELECT count(*)::text FROM workflow_instances
WHERE tenant_id=$1 AND birth_event_id=$2 AND template_key IN ('birth_kid','birth_mother') AND state='open'`, fxTenant, requestID)
	story.Assert("the kid and mother workflows opened from goat.created", openFlows == "2", "open=%s", openFlows)

	// One kid step recorded: its proof goes to the verifier the moment it is recorded.
	stepID := fx.scanText(`SELECT COALESCE(min(a.action_id::text),'') FROM workflow_actions a
JOIN workflow_instances w ON w.workflow_id = a.workflow_id
WHERE w.tenant_id=$1 AND w.subject_goat_id=$2 AND w.template_key='birth_kid' AND a.action_key=$3`,
		fxTenant, kidID, tasksdomain.ActionKeyKidClean)
	kidFlow := fx.scanText(`SELECT workflow_id::text FROM workflow_instances WHERE tenant_id=$1 AND subject_goat_id=$2 AND template_key='birth_kid'`, fxTenant, kidID)
	_, stepErr := workflows.AnswerAction(ctx, tasksapp.AnswerActionInput{
		TenantID: fxTenant, WorkflowID: kidFlow, ActionID: stepID, AnswerValue: "yes",
		ProofRef: "proof/e2e-birth-reject-kid-clean.mp4", AnsweredBy: operatorID,
		IdempotencyKey: "birth-reject-kid-clean", RequestFingerprint: "birth-reject-kid-clean",
	})
	story.Assert("the kid-clean step was recorded", stepErr == nil, "err=%v", stepErr)
	pendingStep := fx.scanText(`SELECT count(*)::text FROM verification_items
WHERE tenant_id=$1 AND source_ref_type='workflow_birth_action' AND source_ref_id=$2 AND status='pending'`, fxTenant, stepID)
	story.Assert("its proof is waiting for the verifier", pendingStep == "1", "pending=%s", pendingStep)
	pendingCapture := fx.scanText(`SELECT count(*)::text FROM verification_items
WHERE tenant_id=$1 AND source_ref_type='birth_capture' AND source_ref_id=$2 AND status='pending'`, fxTenant, requestID)
	story.Assert("the report's form photo is waiting for the verifier", pendingCapture == "1", "pending=%s", pendingCapture)

	// ------------------------------------------------------------------------------- reject it
	story.Step("The approver rejects the birth",
		"One decision transaction marks the litter rejected, retires the kid through identity's exit, "+
			"and announces counts.birth.rejected; the relay then cancels the work and withdraws the items.")
	_, _, decideErr := approvals.Decide(ctx, countsapp.DecisionInput{
		TenantID: fxTenant, ApprovalRequestID: requestID, Approve: false, Reason: "Duplicate report of an existing kid",
		DecidedByUserID: approverID, IdempotencyKey: "birth-reject-decide-1", RequestFingerprint: "birth-reject-decide-1-fp",
		DecidableTypes: []string{countsdomain.ApprovalRequestTypeBirth, countsdomain.ApprovalRequestTypeDeath},
	})
	if !story.Assert("the rejection was accepted", decideErr == nil, "err=%v", decideErr) {
		return
	}
	relayUntilSettled(fx)

	assertRejectedState := func(label string) {
		t.Helper()
		states := fx.scanText(`SELECT COALESCE(string_agg(template_key || '=' || state, ',' ORDER BY template_key), '') FROM workflow_instances
WHERE tenant_id=$1 AND birth_event_id=$2`, fxTenant, requestID)
		story.Assert(label+": both workflows are cancelled", states == "birth_kid=canceled,birth_mother=canceled", "states=%s", states)
		openActions := fx.scanText(`SELECT count(*)::text FROM workflow_actions a JOIN workflow_instances w ON w.workflow_id=a.workflow_id
WHERE w.tenant_id=$1 AND w.birth_event_id=$2 AND a.status IN ('pending','in_review','rework')`, fxTenant, requestID)
		story.Assert(label+": no step is left for the operator", openActions == "0", "open actions=%s", openActions)
		staged := fx.scanText(`SELECT count(*)::text FROM workflow_actions a JOIN workflow_instances w ON w.workflow_id=a.workflow_id
WHERE w.tenant_id=$1 AND w.birth_event_id=$2 AND a.status='canceled' AND (a.proof_ref IS NOT NULL OR a.proof_refs <> '[]'::jsonb)`, fxTenant, requestID)
		story.Assert(label+": cancelled steps carry no staged proof", staged == "0", "staged=%s", staged)
		items := fx.scanText(`SELECT COALESCE(string_agg(source_ref_type || '=' || status, ',' ORDER BY source_ref_type), '') FROM verification_items
WHERE tenant_id=$1 AND ((source_ref_type='workflow_birth_action' AND source_ref_id=$2) OR (source_ref_type='birth_capture' AND source_ref_id=$3))`,
			fxTenant, stepID, requestID)
		story.Assert(label+": the pending verifier items are withdrawn", items == "birth_capture=withdrawn,workflow_birth_action=withdrawn", "items=%s", items)
		kid := fx.scanText(`SELECT lifecycle_status || '/' || COALESCE(exit_reason,'') || '/' || (exited_at IS NOT NULL)::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, kidID)
		story.Assert(label+": the kid left the live register, kept for audit", kid == "inactive/recorded_in_error/true", "kid=%s", kid)
		tag := fx.scanText(`SELECT status FROM goat_identifiers WHERE tenant_id=$1 AND goat_id=$2 AND normalized_value=$3`, fxTenant, kidID, tempTag)
		story.Assert(label+": the temporary tag is retired", tag == "retired", "status=%s", tag)
		litterStatus := fx.scanText(`SELECT count_status FROM goat_births WHERE tenant_id=$1 AND child_goat_id=$2`, fxTenant, kidID)
		story.Assert(label+": the litter is count-rejected", litterStatus == "rejected", "count_status=%s", litterStatus)
		openObligations := fx.scanText(`SELECT count(*)::text FROM obligation_instances
WHERE tenant_id=$1 AND target_id=$2 AND status IN ('scheduled','due','deferred','batched')`, fxTenant, kidID)
		story.Assert(label+": the kid owes no open obligation", openObligations == "0", "open=%s", openObligations)
		resolved, resolveErr := fx.Identity.ResolveIdentifier(ctx, identityports.ResolveIdentifierParams{
			TenantID: fxTenant, IdentifierType: "temporary_tag", NormalizedValue: tempTag,
		}, "trace-e2e-birth-reject-scan")
		scannable := resolveErr == nil && resolved != nil && resolved.GoatSummary != nil && resolved.GoatSummary.GoatID == kidID
		story.Assert(label+": scanning the retired tag no longer finds the kid", !scannable,
			"err=%v result=%+v", resolveErr, resolved)
		tagged, listErr := fx.Identity.ListTemporaryTaggedGoats(ctx, identityapp.ListTemporaryTaggedGoatsInput{TenantID: fxTenant, Limit: 50})
		listed := false
		if tagged != nil {
			for _, item := range tagged.Items {
				if item.GoatID == kidID {
					listed = true
				}
			}
		}
		story.Assert(label+": the kid is off the to-be-tagged register list", listErr == nil && !listed, "err=%v", listErr)
		motherState := fx.scanText(`SELECT lifecycle_status FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, motherID)
		story.Assert(label+": the mother is untouched", motherState == "alive", "mother=%s", motherState)
	}
	assertRejectedState("after the relay")

	boardRows, boardErr := countsboardsource.NewApprovals(fx.Pool, 10*time.Second).ListRows(ctx, workboardports.SourceQuery{
		TenantID: fxTenant, ParkID: fxPark, BusinessDate: bornOn.Format("2006-01-02"), Limit: 50,
	})
	onBoard := false
	for _, row := range boardRows {
		if row.SourceID == requestID {
			onBoard = true
		}
	}
	story.Assert("the rejected birth is off the Work Board", boardErr == nil && !onBoard, "err=%v rows=%d", boardErr, len(boardRows))

	exited := fx.scanText(`SELECT count(*)::text FROM outbox_messages WHERE tenant_id=$1 AND event_type='goat.exited' AND aggregate_id=$2`, fxTenant, kidID)
	story.Assert("the kid's exit was announced through identity's goat.exited", exited == "1", "rows=%s", exited)
	rejected := fx.scanText(`SELECT count(*)::text FROM outbox_messages WHERE tenant_id=$1 AND event_type='counts.birth.rejected' AND aggregate_id=$2 AND status='published'`, fxTenant, requestID)
	story.Assert("counts.birth.rejected was published once", rejected == "1", "rows=%s", rejected)

	// ------------------------------------------------------------------------------ redelivery
	story.Step("A redelivered rejection changes nothing",
		"At-least-once delivery: the same counts.birth.rejected reaches every consumer again.")
	var envelope []byte
	if err := fx.Pool.QueryRow(ctx, `SELECT payload::text::bytea FROM outbox_messages WHERE tenant_id=$1 AND event_type='counts.birth.rejected' AND aggregate_id=$2`,
		fxTenant, requestID).Scan(&envelope); err == nil {
		var env struct {
			EventID string          `json:"event_id"`
			Payload json.RawMessage `json:"payload"`
		}
		_ = json.Unmarshal(envelope, &env)
		replayErr := fx.Bus.Publish(ctx, eventbus.Event{
			ID: env.EventID, Type: "counts.birth.rejected", TenantID: fxTenant, Key: requestID,
			Payload: env.Payload, OccurredAt: time.Now().UTC(),
		})
		story.Assert("the redelivery is accepted", replayErr == nil, "err=%v", replayErr)
	} else {
		story.Assert("the counts.birth.rejected envelope exists", false, "err=%v", err)
	}
	assertRejectedState("after the redelivery")

	// ---------------------------------------------------------------- a fresh birth still approves
	story.Step("Re-raising the birth creates a fresh kid, and approving it works as before",
		"The rejected kid stays retired; the new report creates its own kid, which approval keeps alive.")
	child2 := child
	child2.ClientIdempotencyKey, child2.StoredIdempotencyKey, child2.RequestHash =
		"birth-reject-child-2", fxTenant+":identity.admin.goat_create:birth-reject-child-2", "hash-birth-reject-child-2"
	child2.Identifiers = []identityports.AdminGoatCreateIdentifier{{
		IdentifierType: "temporary_tag", IdentifierValue: "CBE-42778", NormalizedValue: "CBE-42778", ScopeKey: "global", IsPrimary: true,
	}}
	second, err := approvals.SubmitBirthRequest(ctx, countsdomain.ApprovalRequestSubmission{
		TenantID: fxTenant, RequestType: countsdomain.ApprovalRequestTypeBirth, Payload: payload,
		RaisedByUserID: operatorID, RaisedAt: bornOn.Add(10 * time.Hour),
		IdempotencyKey: "birth-reject-submit-2", RequestFingerprint: "birth-reject-submit-2-fp",
	}, []identityports.CreateAdminGoatCommand{child2})
	if !story.Assert("the second birth was submitted", err == nil && len(second.Children) == 1 && second.Children[0].GoatID != kidID, "err=%v", err) {
		return
	}
	relayUntilSettled(fx)
	_, _, approveErr := approvals.Decide(ctx, countsapp.DecisionInput{
		TenantID: fxTenant, ApprovalRequestID: second.Approval.ApprovalRequestID, Approve: true,
		DecidedByUserID: approverID, IdempotencyKey: "birth-approve-decide-2", RequestFingerprint: "birth-approve-decide-2-fp",
		DecidableTypes: []string{countsdomain.ApprovalRequestTypeBirth, countsdomain.ApprovalRequestTypeDeath},
	})
	story.Assert("the second birth was approved", approveErr == nil, "err=%v", approveErr)
	relayUntilSettled(fx)
	secondKid := fx.scanText(`SELECT g.lifecycle_status || '/' || b.count_status FROM goats g JOIN goat_births b ON b.child_goat_id=g.goat_id
WHERE g.tenant_id=$1 AND g.goat_id=$2`, fxTenant, second.Children[0].GoatID)
	story.Assert("the approved kid is alive and counted", secondKid == "alive/approved", "kid=%s", secondKid)
	secondFlows := fx.scanText(`SELECT count(*)::text FROM workflow_instances WHERE tenant_id=$1 AND birth_event_id=$2 AND state='open'`,
		fxTenant, second.Approval.ApprovalRequestID)
	story.Assert("the approved birth keeps its open care workflows", secondFlows == "2", "open=%s", secondFlows)
}

func strPtrE2E(s string) *string { return &s }

// relayUntilSettled drains the outbox like the always-running production relay: a handler that
// timed out over the remote test tunnel is retried on the next pass (the relay re-queues it with
// backoff), exactly as production would pick it up a moment later.
func relayUntilSettled(fx *Fixture) {
	fx.T.Helper()
	for pass := 0; pass < 6; pass++ {
		fx.RelayOutboxEvents()
		pending := fx.scanText(`SELECT count(*)::text FROM outbox_messages WHERE tenant_id=$1 AND status <> 'published'`, fxTenant)
		if pending == "0" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	fx.T.Logf("outbox not fully settled: %s", fx.scanText(`SELECT COALESCE(string_agg(event_type || ':' || status || ':' || COALESCE(left(last_error,120),''), ' | '),'') FROM outbox_messages WHERE tenant_id=$1 AND status <> 'published'`, fxTenant))
}

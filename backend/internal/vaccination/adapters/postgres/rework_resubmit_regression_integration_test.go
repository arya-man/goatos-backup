package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	invpg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	invapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
)

// TestReworkResubmitSiblingInProgressReplaySurvivesDuplicateKey is the regression test for the
// THIRD occurrence of the "operator's rework resubmit is silently lost" defect class: the PEND-1
// sibling in_progress bulk insert in obligation_status_events replays the same
// (tenant_id, idempotency_key) for an obligation that was flipped to in_progress once (as a
// sibling of an earlier completion in the same drive batch), later REJECTED and REOPENED back to
// 'due' (CompletionService.RejectExisting -> obligation.ReopenObligation), and then becomes a
// sibling of a THIRD completion's accept in the same batch. Before the ON CONFLICT (tenant_id,
// idempotency_key) DO NOTHING fix (vaccination/adapters/postgres/repository.go ~L849 and
// obligation/adapters/postgres/repository.go ~L4954), that second insert attempt hit
// obligation_status_events_idempotency_idx_v2 and returned a duplicate-key error, aborting the
// WHOLE accept transaction -- so g3's own legitimate completion (unrelated to the rework) would
// also be silently rolled back along with it.
//
// Batch of 4 goats: g1, g2, g3, g4.
//  1. Accept g2 first -> flips g1, g3, g4 (all still scheduled/due) to in_progress, inserting THREE
//     obligation_status_events rows keyed "<obligation_id>:in_progress".
//  2. Accept g1 (closes itself; siblings g3/g4 already in_progress, no new inserts).
//  3. Reject g1's completion -> RejectExisting reopens g1's obligation back to 'due'. g1 now carries
//     a pre-existing "g1:in_progress" event row AND a 'due' status again.
//  4. Accept g3 (closes itself). Its sibling scan finds g1 back in ('scheduled','due') and tries to
//     re-insert "g1:in_progress" -- the exact replay that used to 500 the whole accept.
func TestReworkResubmitSiblingInProgressReplaySurvivesDuplicateKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-REWORK-SIB', 'Rework resubmit sibling test', 'vaccine', 'dose')`, impItem, impTenant); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', DATE '2026-12-31')`, impLot, impTenant, impItem, impCbe); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.rework.sibling", Name: "Rework sibling", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"stage":"K1"},` +
		`"source":{"source_system":"pc","source_ref":"PC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	const g1 = "30000000-0000-4000-8000-0000000000f1"
	const g2 = "30000000-0000-4000-8000-0000000000f2"
	const g3 = "30000000-0000-4000-8000-0000000000f3"
	const g4 = "30000000-0000-4000-8000-0000000000f4"
	for _, g := range []string{g1, g2, g3, g4} {
		seedGenGoat(t, ctx, pool, g, "alive")
	}

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	if res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf); err != nil || res.Generated != 4 {
		t.Fatalf("generate: res=%+v err=%v", res, err)
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(obl, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: impItem, DosesPerGoat: 1}
	dueBefore := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if sres, err := sweep.SweepVersion(ctx, impTenant, versionID, cfg, dueBefore); err != nil || sres.Batches != 1 {
		t.Fatalf("sweep: res=%+v err=%v", sres, err)
	}

	batchID := scanText(t, ctx, pool, `SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 LIMIT 1`, impTenant)
	obFor := func(goat string) string {
		return scanText(t, ctx, pool, `SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, goat)
	}
	ob1, ob2, ob3 := obFor(g1), obFor(g2), obFor(g3)

	vaccService := vaccapp.NewService(vacc)
	completion := vaccapp.NewCompletionService(vaccService, obl, reserver)
	doses := int32(1)
	mk := func(ob, goat, key string) vaccdomain.NewCompletion {
		batch, lot := batchID, impLot
		return vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: ob, GoatID: goat, BatchID: &batch,
			VaccineInventoryLotID: &lot, Doses: &doses, RouteSite: "SC", AdministeredAt: asOf,
			ColdChainVerified: true, IdempotencyKey: key,
		}
	}
	// recordAndClose mirrors sopbridge.OnTaskSubmitted's real production sequence: RECORD the
	// completion (status='recorded', the SOP submission fanout's own write), then close the
	// obligation via obligation.Repository.MarkCompleted directly (closeObligationsForCompletions'
	// exact call) -- the PEND-1 redesign where the obligation axis closes on RECORD, not on
	// verifier decision. This is what actually triggers the sibling in_progress bulk insert this
	// test targets (obligation/adapters/postgres/repository.go's MarkCompleted, not the vaccination
	// atomic accept path).
	recordAndClose := func(ob, goat, key string) string {
		cid, applied, err := vaccService.RecordCompletion(ctx, mk(ob, goat, key))
		if err != nil || !applied {
			t.Fatalf("record %s: applied=%v err=%v", goat, applied, err)
		}
		if _, err := obl.MarkCompleted(ctx, impTenant, ob); err != nil {
			t.Fatalf("mark completed %s: %v", goat, err)
		}
		return cid
	}

	// Step 1: record+close g2 first -- flips g1, g3, g4 (all still open) to in_progress in one bulk
	// insert.
	recordAndClose(ob2, g2, "rework-sib-g2")
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob1); got != "in_progress" {
		t.Fatalf("precondition: ob1 status = %s, want in_progress", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
		impTenant, ob1); got != 1 {
		t.Fatalf("precondition: ob1 in_progress event count = %d, want 1", got)
	}

	// Step 2: record+close g1 -- closes itself.
	g1CompletionID := recordAndClose(ob1, g1, "rework-sib-g1-attempt1")

	// Step 3: reject g1's completion -- reopens ob1 back to 'due'. This is the REWORK trigger: the
	// production equivalent of a verifier bouncing g1's shed submission back to the operator.
	rr, err := completion.RejectExisting(ctx, impTenant, g1CompletionID, "video unclear, rework", nil)
	if err != nil || !rr.Applied {
		t.Fatalf("reject g1: %+v err=%v", rr, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob1); got != "due" {
		t.Fatalf("ob1 status after reject = %s, want due (reopened)", got)
	}
	// The stale in_progress event from step 1 must still be sitting on ob1, unmodified.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
		impTenant, ob1); got != 1 {
		t.Fatalf("ob1 in_progress event count after reject = %d, want 1 (unchanged)", got)
	}

	// Step 4: record+close g3 -- an UNRELATED, ordinary completion in the same batch. Its sibling
	// scan finds ob1 back in ('scheduled','due') and attempts to re-insert the SAME idempotency key
	// ("<ob1>:in_progress") that step 1 already wrote. THIS is the call that 500'd in production
	// before the ON CONFLICT fix -- silently losing g3's own legitimate, unrelated completion along
	// with it, exactly the defect signature the maintainer described ("completions ... saved; no
	// verification item; nothing reached the verifier").
	g3CompletionID, applied, err := vaccService.RecordCompletion(ctx, mk(ob3, g3, "rework-sib-g3"))
	if err != nil || !applied || g3CompletionID == "" {
		t.Fatalf("record g3: applied=%v err=%v", applied, err)
	}
	if _, err := obl.MarkCompleted(ctx, impTenant, ob3); err != nil {
		t.Fatalf("mark completed g3 (unrelated completion in the same batch as the reworked g1) failed: %v -- "+
			"the sibling in_progress replay for the reopened g1 must be a no-op, not an aborting error", err)
	}

	// The replayed sibling insert for ob1 must stay a no-op: exactly one in_progress event, not two.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
		impTenant, ob1); got != 1 {
		t.Fatalf("ob1 in_progress event count after g3's replayed sibling scan = %d, want 1 (no duplicate)", got)
	}
	// g3's own completion must be genuinely recorded, not silently lost by an aborted transaction.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2`,
		impTenant, ob3); got != 1 {
		t.Fatalf("g3 completion rows = %d, want 1 -- g3's own work must not be lost by ob1's sibling replay", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob3); got != "completed" {
		t.Fatalf("ob3 status = %s, want completed", got)
	}
	// ob1 itself genuinely IS back in progress (the batch is still running with it open again) --
	// that state transition is real and correct. What must NOT duplicate is the EVENT LOG entry for
	// it, already asserted above.
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, ob1); got != "in_progress" {
		t.Fatalf("ob1 status after g3's sibling replay = %s, want in_progress", got)
	}
}

// TestVaccinationSubmissionBridgeReworkResubmitFreshVerificationItemNoDuplicatesForUntouched drives
// the REAL production shed-submission fanout entrypoint (sopbridge.VaccinationSubmissionBridge.
// OnTaskSubmitted, wired to the real vaccination, obligation and verification Postgres repos -- the
// exact composition backend/internal/bootstrap/api.go wires in production) through the cumulative
// per-shed resubmit pattern the Android client actually sends: a shed of 2 goats is submitted, one
// goat's completion is REJECTED (the verifier's rework verdict), and the WHOLE shed (both goats) is
// submitted again, because the client re-sends the cumulative shed payload rather than a
// single-animal delta.
//
// Asserts the three things the maintainer's defect class keeps breaking:
//  1. the resubmit fanout completes without a duplicate-key error;
//  2. the reworked goat gets a FRESH pending verification item under the new submission;
//  3. the untouched goat (already accepted) gets no second completion and no duplicate item.
func TestVaccinationSubmissionBridgeReworkResubmitFreshVerificationItemNoDuplicatesForUntouched(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)
	verification := verificationpg.NewRepository(pool, 5*time.Second)
	vaccService := vaccapp.NewService(vacc)
	completion := vaccapp.NewCompletionService(vaccService, obl, nil)

	const g1 = "30000000-0000-4000-8000-0000000000a1" // stays accepted throughout ("untouched")
	const g2 = "30000000-0000-4000-8000-0000000000a2" // gets rejected then reworked
	seedGenGoat(t, ctx, pool, g1, "alive")
	seedGenGoat(t, ctx, pool, g2, "alive")

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.rework.resubmit.fanout", Name: "Rework Resubmit Fanout", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	sopID := scanText(t, ctx, pool,
		`INSERT INTO sop_definitions (tenant_id, code, name, description, status)
		 VALUES ($1, 'vaccination.rework.resubmit', 'Vaccination Rework Resubmit', 'Rework resubmit regression', 'active')
		 RETURNING sop_id::text`, impTenant)
	sopVersionID := scanText(t, ctx, pool,
		`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, 1, 'rework resubmit v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false,"subject_scope":"animal","types":["video"],"minimum_count":0}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)
		 RETURNING sop_version_id::text`, impTenant, sopID)
	taskID := scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id)
		 VALUES ($1, $2, $3, 'vaccination_drive', 'Rework Resubmit Drive', 'park', $4)
		 RETURNING task_id::text`, impTenant, sopID, sopVersionID, impCbe)

	obFor := make(map[string]string)
	for _, g := range []string{g1, g2} {
		obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: g, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
			IdempotencyKey: "rework-resubmit-fanout-" + g, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", g, applied, err)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND obligation_id=$3`,
			taskID, impTenant, obligationID); err != nil {
			t.Fatalf("link obligation task %s: %v", g, err)
		}
		obFor[g] = obligationID
	}

	mkSubmission := func(key string, goatIDs []string) string {
		proofRefs := make([]any, 0, len(goatIDs))
		for _, g := range goatIDs {
			proofRefs = append(proofRefs, map[string]any{
				"proof_id":     "proof-" + key + "-" + g,
				"proof_type":   "video",
				"subject_type": "goat",
				"subject_id":   g,
				"upload_state": "completed",
			})
		}
		proofRefsJSON, err := json.Marshal(proofRefs)
		if err != nil {
			t.Fatalf("marshal proof refs: %v", err)
		}
		submissionID := scanText(t, ctx, pool,
			`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state, proof_refs)
			 VALUES ($1, $2, $3, $4, $5, '{"goat_ids":[]}'::jsonb, 'accepted', $6::jsonb)
			 RETURNING submission_id::text`,
			impTenant, taskID, sopVersionID, impParty, key, string(proofRefsJSON))
		for _, g := range goatIDs {
			if _, err := pool.Exec(ctx,
				`INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state)
				 VALUES ($1, $2, $3, $4, 'dose', 'accepted')`,
				impTenant, submissionID, taskID, g); err != nil {
				t.Fatalf("submission item %s: %v", g, err)
			}
		}
		return submissionID
	}

	bridge := sopbridge.NewVaccinationSubmissionBridge(vaccService).
		WithVerificationProducer(verification).
		WithObligationCompleter(obl)
	task := sopdomain.TaskSummary{TaskID: taskID, SOPCode: "vaccination.drive"}

	// --- First submission: whole shed, both goats.
	sub1ID := mkSubmission("rework-resubmit-sub-1", []string{g1, g2})
	if err := bridge.OnTaskSubmitted(ctx, impTenant, task, sopdomain.SubmissionSummary{SubmissionID: sub1ID, SubmittedBy: impParty}); err != nil {
		t.Fatalf("first shed submission fanout: %v", err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obFor[g1]); got != "completed" {
		t.Fatalf("ob(g1) after first submission = %s, want completed", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obFor[g2]); got != "completed" {
		t.Fatalf("ob(g2) after first submission = %s, want completed", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_ref_type='vaccination_goat' AND source_ref_id=$2`,
		impTenant, g1); got != 1 {
		t.Fatalf("g1 verification items after first submission = %d, want 1", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_ref_type='vaccination_goat' AND source_ref_id=$2`,
		impTenant, g2); got != 1 {
		t.Fatalf("g2 verification items after first submission = %d, want 1", got)
	}

	// --- Verifier rejects g2 (the rework verdict): reopens g2's obligation, leaves g1 alone.
	// RejectExisting archives the rejected completion into vaccination_completion_rejections and
	// DELETEs it from vaccination_completions (see repository.go's reject path) -- it does not
	// leave a 'rejected' row behind in the live table.
	if err := completion.ApplyGoatVerification(ctx, impTenant, sub1ID, g2, "rejected", "video unclear, rework", nil); err != nil {
		t.Fatalf("reject g2 via ApplyGoatVerification: %v", err)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND goat_id=$2`, impTenant, g2); got != 0 {
		t.Fatalf("g2 live completion rows after reject = %d, want 0 (moved to the rejections archive)", got)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completion_rejections WHERE tenant_id=$1 AND goat_id=$2`, impTenant, g2); got != 1 {
		t.Fatalf("g2 archived rejection rows = %d, want 1", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obFor[g2]); got != "due" {
		t.Fatalf("ob(g2) after reject = %s, want due (reopened)", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obFor[g1]); got != "completed" {
		t.Fatalf("ob(g1) after g2's reject = %s, want still completed (untouched)", got)
	}

	// --- Resubmit: the CUMULATIVE shed payload, both goats again -- exactly what the Android client
	// sends (it re-sends the whole shed roster, not a single-animal delta).
	sub2ID := mkSubmission("rework-resubmit-sub-2", []string{g1, g2})
	if err := bridge.OnTaskSubmitted(ctx, impTenant, task, sopdomain.SubmissionSummary{SubmissionID: sub2ID, SubmittedBy: impParty}); err != nil {
		t.Fatalf("resubmit shed fanout failed (this is the defect: a resubmit must NEVER hit a "+
			"duplicate-key error and abort, silently losing the operator's rework): %v", err)
	}

	// g1 is untouched: no new completion materialized under the resubmit, so no second verification
	// item either. Exactly one completion, exactly one item -- no duplicates.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND goat_id=$2`, impTenant, g1); got != 1 {
		t.Fatalf("g1 completion rows after resubmit = %d, want 1 (untouched, no duplicate work)", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_ref_type='vaccination_goat' AND source_ref_id=$2`,
		impTenant, g1); got != 1 {
		t.Fatalf("g1 verification items after resubmit = %d, want 1 (no duplicate item for the untouched animal)", got)
	}

	// g2 is the reworked animal: a NEW completion under sub2 (the rejected attempt lives in the
	// archive table, not vaccination_completions), and a FRESH pending verification item distinct
	// from the first (rejected) one.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND goat_id=$2`, impTenant, g2); got != 1 {
		t.Fatalf("g2 live completion rows after resubmit = %d, want 1 (the fresh rework)", got)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completion_rejections WHERE tenant_id=$1 AND goat_id=$2`, impTenant, g2); got != 1 {
		t.Fatalf("g2 archived rejection rows after resubmit = %d, want 1 (the rejected attempt stays immutable history)", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_ref_type='vaccination_goat' AND source_ref_id=$2`,
		impTenant, g2); got != 2 {
		t.Fatalf("g2 verification items after resubmit = %d, want 2 (original + fresh rework item)", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_ref_type='vaccination_goat' AND source_ref_id=$2 AND source_submission_id=$3 AND status='pending'`,
		impTenant, g2, sub2ID); got != 1 {
		t.Fatalf("g2 fresh pending verification item under sub2 = %d, want 1 -- the reworked animal's proof must reach the verifier", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obFor[g2]); got != "completed" {
		t.Fatalf("ob(g2) after resubmit = %s, want completed again", got)
	}
}

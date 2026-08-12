package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

const (
	skeletonSOPID        = "b0000000-0000-4000-8000-000000000001" // seeded by migration 000075
	skeletonSOPVersionID = "b0000000-0000-4000-8000-000000000002"
)

// TestListRecordedCompletionsByTask checks the SOP verify fan-out source: it returns only the
// still-recorded completions captured under a task's submissions (accepted ones are excluded).
func TestListRecordedCompletionsByTask(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)
	svc := vaccapp.NewService(vacc)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.fanout", Name: "Fanout", Category: "vaccination", Status: "draft",
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

	const ga = "30000000-0000-4000-8000-0000000000a8"
	const gb = "30000000-0000-4000-8000-0000000000b8"
	seedGenGoat(t, ctx, pool, ga, "alive")
	seedGenGoat(t, ctx, pool, gb, "alive")

	mkObl := func(goat, key string) string {
		id, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", goat, applied, err)
		}
		return id
	}
	obA := mkObl(ga, "fo-a")
	obB := mkObl(gb, "fo-b")

	// SOP task + submission + one item per goat (reuses the seeded vaccination SOP skeleton).
	taskID := scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id)
		 VALUES ($1, $2, $3, 'vaccination_drive', 'Drive', 'park', $4) RETURNING task_id::text`,
		impTenant, skeletonSOPID, skeletonSOPVersionID, impCbe)
	subID := scanText(t, ctx, pool,
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers)
		 VALUES ($1, $2, $3, $4, 'fo-sub', '{}'::jsonb) RETURNING submission_id::text`,
		impTenant, taskID, skeletonSOPVersionID, impParty)
	itemA := scanText(t, ctx, pool,
		`INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state)
		 VALUES ($1, $2, $3, $4, 'dose', 'needs_review') RETURNING item_id::text`,
		impTenant, subID, taskID, ga)
	itemB := scanText(t, ctx, pool,
		`INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state)
		 VALUES ($1, $2, $3, $4, 'dose', 'accepted') RETURNING item_id::text`,
		impTenant, subID, taskID, gb)

	doses := int32(1)
	record := func(ob, goat, item, key string) string {
		it := item
		cid, applied, err := svc.RecordCompletion(ctx, vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: ob, GoatID: goat, SopSubmissionItemID: &it,
			Doses: &doses, RouteSite: "SC", AdministeredAt: time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
			Status: "recorded", IdempotencyKey: key,
		})
		if err != nil || !applied {
			t.Fatalf("record %s: applied=%v err=%v", goat, applied, err)
		}
		return cid
	}
	cidA := record(obA, ga, itemA, "fo-c-a")
	cidB := record(obB, gb, itemB, "fo-c-b")

	// Accept B so only A remains 'recorded'.
	if _, applied, err := vacc.AcceptCompletion(ctx, impTenant, cidB, nil, nil); err != nil || !applied {
		t.Fatalf("accept B: applied=%v err=%v", applied, err)
	}

	ids, err := vacc.ListRecordedCompletionsByTask(ctx, impTenant, taskID)
	if err != nil {
		t.Fatalf("list by task: %v", err)
	}
	if len(ids) != 1 || ids[0] != cidA {
		t.Fatalf("want only recorded completion %s, got %v", cidA, ids)
	}
}

func TestRecordCompletionsFromVaccinationSessionTask(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.session.fanout", Name: "Session Fanout", Category: "vaccination", Status: "draft",
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
	withdrawalDays := int32(5)
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`), WithdrawalDays: &withdrawalDays,
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if got := scanText(t, ctx, pool, `SELECT COALESCE(withdrawal_days::text, '') FROM protocol_rules WHERE tenant_id=$1 AND rule_id=$2`, impTenant, ruleID); got != "5" {
		t.Fatalf("protocol rule withdrawal_days = %q, want 5", got)
	}

	const goatID = "30000000-0000-4000-8000-0000000000c8"
	seedGenGoat(t, ctx, pool, goatID, "alive")
	obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
		DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "session-fanout-obligation", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("obligation: applied=%v err=%v", applied, err)
	}

	sopID := scanText(t, ctx, pool,
		`INSERT INTO sop_definitions (tenant_id, code, name, description, status)
		 VALUES ($1, 'vaccination.session', 'Vaccination Session', 'Session fanout regression', 'active')
		 RETURNING sop_id::text`, impTenant)
	sopVersionID := scanText(t, ctx, pool,
		`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, 1, 'session v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false,"subject_scope":"batch","types":["video"],"minimum_count":0}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)
		 RETURNING sop_version_id::text`, impTenant, sopID)
	taskID := scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id)
		 VALUES ($1, $2, $3, 'vaccination_session', 'Session', 'park', $4)
		 RETURNING task_id::text`, impTenant, sopID, sopVersionID, impCbe)
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND obligation_id=$3`,
		taskID, impTenant, obligationID); err != nil {
		t.Fatalf("link obligation task: %v", err)
	}
	submissionID := scanText(t, ctx, pool,
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state)
		 VALUES ($1, $2, $3, $4, 'session-fanout-submission',
		   '{"dose_ml_given":1,"cold_chain_verified":true,"adverse_reaction":false,"administered_at":"2026-06-23T00:00:00Z"}'::jsonb,
		   'accepted')
		 RETURNING submission_id::text`, impTenant, taskID, sopVersionID, impParty)
	if _, err := pool.Exec(ctx,
		`INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state, result)
		 VALUES ($1, $2, $3, $4, 'dose', 'accepted', '{"administered_at":"2026-06-24T04:35:12.345Z"}'::jsonb)`,
		impTenant, submissionID, taskID, goatID); err != nil {
		t.Fatalf("submission item: %v", err)
	}

	count, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("RecordCompletionsFromSubmission() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("materialized count = %d, want 1", count)
	}
	replay, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("RecordCompletionsFromSubmission() replay error = %v", err)
	}
	if replay != 1 {
		t.Fatalf("replay materialized count = %d, want existing 1", replay)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND sop_submission_item_id IN (
		   SELECT item_id FROM sop_submission_items WHERE tenant_id=$1 AND submission_id=$2
		 )`, impTenant, submissionID); got != 1 {
		t.Fatalf("completion rows = %d, want 1", got)
	}
	if got := scanText(t, ctx, pool,
		`SELECT (administered_at AT TIME ZONE 'UTC')::text
		   FROM vaccination_completions
		  WHERE tenant_id=$1
		    AND sop_submission_item_id IN (
		      SELECT item_id FROM sop_submission_items WHERE tenant_id=$1 AND submission_id=$2
		    )`, impTenant, submissionID); !strings.HasPrefix(got, "2026-06-24 04:35:12.345") {
		t.Fatalf("administered_at = %q, want scan timestamp", got)
	}
	if got := scanText(t, ctx, pool,
		`SELECT COALESCE(withdrawal_until_date::text, '')
		   FROM vaccination_completions
		  WHERE tenant_id=$1
		    AND sop_submission_item_id IN (
		      SELECT item_id FROM sop_submission_items WHERE tenant_id=$1 AND submission_id=$2
		    )`, impTenant, submissionID); got != "2026-06-29" {
		t.Fatalf("withdrawal_until_date = %q, want 2026-06-29", got)
	}

	oldProofID := "91000000-0000-4000-8000-00000000f101"
	currentProofID := "91000000-0000-4000-8000-00000000f102"
	if _, err := pool.Exec(ctx, `
INSERT INTO proof_artifacts (
  proof_id, tenant_id, storage_provider, object_key, content_hash, mime_type, size_bytes,
  upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, metadata
) VALUES
  ($1::uuid, $3::uuid, 'local', 'fanout/old-proof', 'sha256:old', 'video/mp4', 10,
   'completed', 'task', $4::uuid, 'goat', $5::uuid, 'video',
   jsonb_build_object('superseded_by_proof_id', $2::text)),
  ($2::uuid, $3::uuid, 'local', 'fanout/current-proof', 'sha256:current', 'video/mp4', 20,
   'completed', 'task', $4::uuid, 'goat', $5::uuid, 'video', '{}'::jsonb)`, oldProofID, currentProofID, impTenant, taskID, goatID); err != nil {
		t.Fatalf("seed replacement proof artifacts: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE sop_submissions
SET proof_refs = jsonb_build_array(jsonb_build_object(
  'proof_id', $1::text,
  'proof_type', 'video',
  'subject_type', 'goat',
  'subject_id', $3::text,
  'upload_state', 'completed'
))
WHERE tenant_id = $2::uuid
  AND submission_id = $4::uuid`, currentProofID, impTenant, goatID, submissionID); err != nil {
		t.Fatalf("seed replacement proof refs: %v", err)
	}
	completions, err := vacc.ListSubmissionCompletions(ctx, impTenant, submissionID)
	if err != nil {
		t.Fatalf("ListSubmissionCompletions() error = %v", err)
	}
	if len(completions) != 1 {
		t.Fatalf("submission completions = %d, want 1", len(completions))
	}
	if got := completions[0].ProofRefIDs; len(got) != 1 || got[0] != currentProofID {
		t.Fatalf("proof refs = %v, want current proof only %s", got, currentProofID)
	}
}

func TestRecordCompletionsFromSubmissionMaterializesEveryVaccineObligationForOneScanItem(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.multiobligation.fanout", Name: "Multi Obligation Fanout", Category: "vaccination", Status: "draft",
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
	ruleET, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "et_tt_w1", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule et: %v", err)
	}
	ruleSP, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "sheep_pox_w1", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule sheep pox: %v", err)
	}

	const goatID = "30000000-0000-4000-8000-0000000001c8"
	seedGenGoat(t, ctx, pool, goatID, "alive")
	sopID := scanText(t, ctx, pool,
		`INSERT INTO sop_definitions (tenant_id, code, name, description, status)
		 VALUES ($1, 'vaccination.multiobligation', 'Vaccination Multi Obligation', 'Multi obligation fanout regression', 'active')
		 RETURNING sop_id::text`, impTenant)
	sopVersionID := scanText(t, ctx, pool,
		`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, 1, 'drive v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false,"subject_scope":"batch","types":["video"],"minimum_count":0}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)
		 RETURNING sop_version_id::text`, impTenant, sopID)
	taskID := scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id)
		 VALUES ($1, $2, $3, 'vaccination_drive', 'Multi Obligation Drive', 'park', $4)
		 RETURNING task_id::text`, impTenant, sopID, sopVersionID, impCbe)

	for i, ruleID := range []string{ruleET, ruleSP} {
		obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: fmt.Sprintf("multi-obligation-%d", i), Sequence: int32(i + 1),
		})
		if err != nil || !applied {
			t.Fatalf("obligation %d: applied=%v err=%v", i, applied, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND obligation_id=$3`, taskID, impTenant, obligationID); err != nil {
			t.Fatalf("link obligation %d: %v", i, err)
		}
	}

	submissionID := seedAcceptedSubmissionWithItems(t, ctx, pool, taskID, sopVersionID, "multi-obligation-submission", goatID)
	count, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("RecordCompletionsFromSubmission() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("materialized item count = %d, want 1 scan item covered by two completions", count)
	}
	replay, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("RecordCompletionsFromSubmission() replay error = %v", err)
	}
	if replay != 1 {
		t.Fatalf("replay materialized item count = %d, want existing scan item covered", replay)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND sop_submission_item_id IN (
		   SELECT item_id FROM sop_submission_items WHERE tenant_id=$1 AND submission_id=$2
		 )`, impTenant, submissionID); got != 2 {
		t.Fatalf("completion rows = %d, want 2", got)
	}
}

func TestRecordCompletionsFromSubmissionClosesNeighborBatchObligationsFromScanAnchor(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, versionID, ruleET, ruleSP := seedTwoRuleVaccinationProtocol(t, ctx, proto, "vaccination.neighborbatch.fanout")
	const goatID = "30000000-0000-4000-8000-0000000002c8"
	seedGenGoat(t, ctx, pool, goatID, "alive")
	_ = protoID

	taskID, sopVersionID := seedVaccinationDriveTask(t, ctx, pool, "vaccination.neighborbatch", "Neighbor Batch Current Task")
	targetBatch := scanText(t, ctx, pool,
		`INSERT INTO obligation_batches (tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date)
		 VALUES ($1, $2, 'shed', $3, 'planned', DATE '2026-07-02') RETURNING batch_id::text`,
		impTenant, versionID, impShed)
	targetObligations := make([]string, 0, 2)
	for i, ruleID := range []string{ruleET, ruleSP} {
		obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID, BatchID: &targetBatch,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: impShed,
			DueAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: fmt.Sprintf("neighbor-batch-%d", i), Sequence: int32(i + 1),
		})
		if err != nil || !applied {
			t.Fatalf("neighbor obligation %d: applied=%v err=%v", i, applied, err)
		}
		targetObligations = append(targetObligations, obligationID)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_task_scan_attempts (tenant_id, task_id, field_key, tag, normalized_tag, goat_id, obligation_id, outcome, tag_role, reason, captured_by, idempotency_key)
VALUES ($1, $2, 'goat_ids', 'NEIGHBOR-RFID-1', 'neighbor-rfid-1', $3, $4, 'accepted', 'primary', 'neighbor_partition', $5, 'neighbor-anchor-1')`,
		impTenant, taskID, goatID, targetObligations[0], impParty); err != nil {
		t.Fatalf("neighbor scan attempt: %v", err)
	}

	submissionID := seedAcceptedSubmissionWithItems(t, ctx, pool, taskID, sopVersionID, "neighbor-batch-submission", goatID)
	count, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("RecordCompletionsFromSubmission() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("materialized item count = %d, want 1 neighbor item covered", count)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND batch_id=$2`, impTenant, targetBatch); got != 2 {
		t.Fatalf("neighbor batch completion rows = %d, want 2", got)
	}
}

func TestRecordCompletionsFromSubmissionClosesStandaloneSameDayRFIDObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	_, versionID, ruleET, ruleSP := seedTwoRuleVaccinationProtocol(t, ctx, proto, "vaccination.standalone.fanout")
	const goatID = "30000000-0000-4000-8000-0000000003c8"
	seedGenGoat(t, ctx, pool, goatID, "alive")
	taskID, sopVersionID := seedVaccinationDriveTask(t, ctx, pool, "vaccination.standalone", "Standalone RFID Task")

	sameDayObligations := make([]string, 0, 2)
	for i, ruleID := range []string{ruleET, ruleSP} {
		obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: fmt.Sprintf("standalone-same-day-%d", i), Sequence: int32(i + 1),
		})
		if err != nil || !applied {
			t.Fatalf("standalone obligation %d: applied=%v err=%v", i, applied, err)
		}
		sameDayObligations = append(sameDayObligations, obligationID)
	}
	futureObligation, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleET,
		TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
		DueAt: time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "standalone-future-day", Sequence: 3,
	})
	if err != nil || !applied {
		t.Fatalf("future obligation: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_task_scan_attempts (tenant_id, task_id, field_key, tag, normalized_tag, goat_id, obligation_id, outcome, tag_role, reason, captured_by, idempotency_key)
VALUES ($1, $2, 'goat_ids', 'RFID-STANDALONE-1', 'rfid-standalone-1', $3, $4, 'accepted', 'primary', 'neighbor_partition', $5, 'standalone-anchor-1')`,
		impTenant, taskID, goatID, sameDayObligations[0], impParty); err != nil {
		t.Fatalf("standalone scan attempt: %v", err)
	}

	submissionID := seedAcceptedSubmissionWithItems(t, ctx, pool, taskID, sopVersionID, "standalone-submission", goatID)
	count, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("RecordCompletionsFromSubmission() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("materialized item count = %d, want 1 standalone item covered", count)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=ANY($2::uuid[])`, impTenant, sameDayObligations); got != 2 {
		t.Fatalf("same-day standalone completions = %d, want 2", got)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, futureObligation); got != 0 {
		t.Fatalf("future standalone completion rows = %d, want 0", got)
	}
}

func seedTwoRuleVaccinationProtocol(t *testing.T, ctx context.Context, proto *protopg.Repository, code string) (protoID, versionID, ruleET, ruleSP string) {
	t.Helper()
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition %s: %v", code, err)
	}
	versionID, err = proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version %s: %v", code, err)
	}
	ruleET, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "et_tt_w1", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule et %s: %v", code, err)
	}
	ruleSP, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "sheep_pox_w1", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule sheep pox %s: %v", code, err)
	}
	return protoID, versionID, ruleET, ruleSP
}

func seedVaccinationDriveTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code, title string) (taskID, sopVersionID string) {
	t.Helper()
	sopID := scanText(t, ctx, pool,
		`INSERT INTO sop_definitions (tenant_id, code, name, description, status)
		 VALUES ($1, $2, $3, 'fanout regression', 'active')
		 RETURNING sop_id::text`, impTenant, code, title)
	sopVersionID = scanText(t, ctx, pool,
		`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, 1, 'drive v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false,"subject_scope":"batch","types":["video"],"minimum_count":0}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)
		 RETURNING sop_version_id::text`, impTenant, sopID)
	taskID = scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id)
		 VALUES ($1, $2, $3, 'vaccination_drive', $4, 'park', $5)
		 RETURNING task_id::text`, impTenant, sopID, sopVersionID, title, impCbe)
	return taskID, sopVersionID
}

func seedAcceptedSubmissionWithItems(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, sopVersionID, key string, goatIDs ...string) string {
	t.Helper()
	submissionID := scanText(t, ctx, pool,
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state)
		 VALUES ($1, $2, $3, $4, $5,
		   '{"dose_ml_given":1,"cold_chain_verified":true,"adverse_reaction":false,"administered_at":"2026-06-23T00:00:00Z"}'::jsonb,
		   'accepted')
		 RETURNING submission_id::text`, impTenant, taskID, sopVersionID, impParty, key)
	for _, goatID := range goatIDs {
		if _, err := pool.Exec(ctx,
			`INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state, result)
			 VALUES ($1, $2, $3, $4, $5, 'accepted', '{"administered_at":"2026-06-24T04:35:12.345Z"}'::jsonb)`,
			impTenant, submissionID, taskID, goatID, goatID); err != nil {
			t.Fatalf("submission item %s: %v", goatID, err)
		}
	}
	return submissionID
}

func TestRecordCompletionsFromSubmissionSkipsTerminalObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.terminal.fanout", Name: "Terminal Fanout", Category: "vaccination", Status: "draft",
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

	const goatID = "30000000-0000-4000-8000-0000000000c9"
	seedGenGoat(t, ctx, pool, goatID, "alive")
	obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
		DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Status: "completed", IdempotencyKey: "terminal-fanout-obligation", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("obligation: applied=%v err=%v", applied, err)
	}

	sopID := scanText(t, ctx, pool,
		`INSERT INTO sop_definitions (tenant_id, code, name, description, status)
		 VALUES ($1, 'vaccination.terminal', 'Vaccination Terminal', 'Terminal fanout regression', 'active')
		 RETURNING sop_id::text`, impTenant)
	sopVersionID := scanText(t, ctx, pool,
		`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, 1, 'terminal v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false,"subject_scope":"batch","types":["video"],"minimum_count":0}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)
		 RETURNING sop_version_id::text`, impTenant, sopID)
	taskID := scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id)
		 VALUES ($1, $2, $3, 'vaccination_drive', 'Terminal Drive', 'park', $4)
		 RETURNING task_id::text`, impTenant, sopID, sopVersionID, impCbe)
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND obligation_id=$3`,
		taskID, impTenant, obligationID); err != nil {
		t.Fatalf("link obligation task: %v", err)
	}
	submissionID := scanText(t, ctx, pool,
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state)
		 VALUES ($1, $2, $3, $4, 'terminal-fanout-submission',
		   '{"dose_ml_given":1,"cold_chain_verified":true,"adverse_reaction":false,"administered_at":"2026-06-23T00:00:00Z"}'::jsonb,
		   'accepted')
		 RETURNING submission_id::text`, impTenant, taskID, sopVersionID, impParty)
	if _, err := pool.Exec(ctx,
		`INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state)
		 VALUES ($1, $2, $3, $4, 'dose', 'accepted')`,
		impTenant, submissionID, taskID, goatID); err != nil {
		t.Fatalf("submission item: %v", err)
	}

	count, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("completed terminal fanout count=%d err=%v, want no-op success", count, err)
	}
	if count != 0 {
		t.Fatalf("completed terminal fanout count=%d, want 0 because completed obligations are not eligible", count)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obligationID); got != 0 {
		t.Fatalf("terminal obligation completion rows = %d, want 0", got)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET status='missed' WHERE tenant_id=$1 AND obligation_id=$2`,
		impTenant, obligationID); err != nil {
		t.Fatalf("mark obligation missed for late fanout: %v", err)
	}
	count, err = vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil || count != 1 {
		t.Fatalf("missed late fanout count=%d err=%v, want one materialized completion", count, err)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obligationID); got != 1 {
		t.Fatalf("missed obligation completion rows = %d, want 1", got)
	}
}

func TestRecordCompletionsFromSubmissionIgnoresSubmittedItemsWithoutOpenObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.partial.fanout", Name: "Partial Fanout", Category: "vaccination", Status: "draft",
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

	const goatA = "30000000-0000-4000-8000-0000000000d8"
	const goatB = "30000000-0000-4000-8000-0000000000e8"
	seedGenGoat(t, ctx, pool, goatA, "alive")
	seedGenGoat(t, ctx, pool, goatB, "alive")

	sopID := scanText(t, ctx, pool,
		`INSERT INTO sop_definitions (tenant_id, code, name, description, status)
		 VALUES ($1, 'vaccination.partial', 'Vaccination Partial', 'Partial fanout regression', 'active')
		 RETURNING sop_id::text`, impTenant)
	sopVersionID := scanText(t, ctx, pool,
		`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, 1, 'drive v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false,"subject_scope":"batch","types":["video"],"minimum_count":0}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)
		 RETURNING sop_version_id::text`, impTenant, sopID)
	taskID := scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id)
		 VALUES ($1, $2, $3, 'vaccination_drive', 'Drive', 'park', $4)
		 RETURNING task_id::text`, impTenant, sopID, sopVersionID, impCbe)

	mkObligation := func(goatID, key string) string {
		obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", goatID, applied, err)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND obligation_id=$3`,
			taskID, impTenant, obligationID); err != nil {
			t.Fatalf("link obligation task: %v", err)
		}
		return obligationID
	}
	mkObligation(goatA, "partial-fanout-obligation-a")

	submissionID := scanText(t, ctx, pool,
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state)
		 VALUES ($1, $2, $3, $4, 'partial-fanout-submission',
		   '{"dose_ml_given":1,"cold_chain_verified":true,"adverse_reaction":false,"administered_at":"2026-06-23T00:00:00Z"}'::jsonb,
		   'accepted')
		 RETURNING submission_id::text`, impTenant, taskID, sopVersionID, impParty)
	for _, goatID := range []string{goatA, goatB} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO sop_submission_items (tenant_id, submission_id, task_id, goat_id, item_key, state)
			 VALUES ($1, $2, $3, $4, $5, 'accepted')`,
			impTenant, submissionID, taskID, goatID, goatID); err != nil {
			t.Fatalf("submission item %s: %v", goatID, err)
		}
	}

	count, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("first fanout count=%d err=%v, want no-op for goat without open matching obligation", count, err)
	}
	if count != 1 {
		t.Fatalf("first fanout count=%d, want 1 eligible item materialized", count)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND sop_submission_item_id IN (
		   SELECT item_id FROM sop_submission_items WHERE tenant_id=$1 AND submission_id=$2
		 )`, impTenant, submissionID); got != 1 {
		t.Fatalf("completion rows after partial fanout = %d, want 1", got)
	}

	mkObligation(goatB, "partial-fanout-obligation-b")
	count, err = vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("retry fanout error = %v", err)
	}
	if count != 2 {
		t.Fatalf("retry materialized count = %d, want 2", count)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND sop_submission_item_id IN (
		   SELECT item_id FROM sop_submission_items WHERE tenant_id=$1 AND submission_id=$2
		 )`, impTenant, submissionID); got != 2 {
		t.Fatalf("completion rows after repaired retry = %d, want 2", got)
	}
}

func TestRecordCompletionsFromSubmissionSkipsAlreadyCompletedNeighborItems(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	_, versionID, ruleET, ruleSP := seedTwoRuleVaccinationProtocol(t, ctx, proto, "vaccination.completedneighbor.fanout")
	taskID, sopVersionID := seedVaccinationDriveTask(t, ctx, pool, "vaccination.completedneighbor", "Completed Neighbor Task")

	const ownGoat = "30000000-0000-4000-8000-0000000004c8"
	const neighborGoat = "30000000-0000-4000-8000-0000000005c8"
	seedGenGoat(t, ctx, pool, ownGoat, "alive")
	seedGenGoat(t, ctx, pool, neighborGoat, "alive")

	ownObligations := make([]string, 0, 2)
	for i, ruleID := range []string{ruleET, ruleSP} {
		obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: ownGoat, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: fmt.Sprintf("completed-neighbor-open-%d", i), Sequence: int32(i + 1),
		})
		if err != nil || !applied {
			t.Fatalf("own obligation %d: applied=%v err=%v", i, applied, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND obligation_id=$3`, taskID, impTenant, obligationID); err != nil {
			t.Fatalf("link own obligation %d: %v", i, err)
		}
		ownObligations = append(ownObligations, obligationID)
	}
	for i, ruleID := range []string{ruleET, ruleSP} {
		obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: neighborGoat, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC), Status: "completed", IdempotencyKey: fmt.Sprintf("completed-neighbor-terminal-%d", i), Sequence: int32(i + 1),
		})
		if err != nil || !applied {
			t.Fatalf("neighbor obligation %d: applied=%v err=%v", i, applied, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO sop_task_scan_attempts (tenant_id, task_id, field_key, tag, normalized_tag, goat_id, obligation_id, outcome, tag_role, reason, captured_by, idempotency_key)
VALUES ($1, $2, 'goat_ids', $3, lower($3), $4, $5, 'accepted', 'primary', 'neighbor_partition', $6, $7)`,
			impTenant, taskID, fmt.Sprintf("NEIGHBOR-DONE-%d", i), neighborGoat, obligationID, impParty, fmt.Sprintf("completed-neighbor-anchor-%d", i)); err != nil {
			t.Fatalf("neighbor completed scan anchor %d: %v", i, err)
		}
	}

	submissionID := seedAcceptedSubmissionWithItems(t, ctx, pool, taskID, sopVersionID, "completed-neighbor-submission", ownGoat, neighborGoat)
	count, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("RecordCompletionsFromSubmission() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("materialized item count = %d, want only the own goat with open obligations", count)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=ANY($2::uuid[])`, impTenant, ownObligations); got != 2 {
		t.Fatalf("own goat completions = %d, want 2", got)
	}
	if got := countRowsVacc(t, ctx, pool, `
SELECT count(*)
FROM vaccination_completions
WHERE tenant_id=$1
  AND sop_submission_item_id IN (
    SELECT item_id FROM sop_submission_items WHERE tenant_id=$1 AND submission_id=$2 AND goat_id=$3
  )`, impTenant, submissionID, neighborGoat); got != 0 {
		t.Fatalf("already completed neighbor completion rows = %d, want 0 new rows", got)
	}
}

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

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
		`SELECT administered_at AT TIME ZONE 'UTC'
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
   'completed', 'task', $4::uuid, 'goat', $5::uuid, 'video', '{}'::jsonb);
UPDATE sop_submissions
SET proof_refs = jsonb_build_array(jsonb_build_object(
  'proof_id', $2::text,
  'proof_type', 'video',
  'subject_type', 'goat',
  'subject_id', $5::text,
  'upload_state', 'completed'
))
WHERE tenant_id = $3::uuid
  AND submission_id = $6::uuid`, oldProofID, currentProofID, impTenant, taskID, goatID, submissionID); err != nil {
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
	if err == nil || !strings.Contains(err.Error(), "materialized 0 of 1") {
		t.Fatalf("terminal fanout count=%d err=%v, want partial materialization error", count, err)
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

func TestRecordCompletionsFromSubmissionRecordsEveryObligationForOneScannedGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.multi.fanout", Name: "Multi Fanout", Category: "vaccination", Status: "draft",
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
	mkRule := func(doseCode string, seq int32) string {
		ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: doseCode, Sequence: seq,
			TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
			EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("rule %s: %v", doseCode, err)
		}
		return ruleID
	}
	ruleA := mkRule("fmd", 1)
	ruleB := mkRule("hs", 2)
	ruleC := mkRule("ppr", 3)

	const goatID = "30000000-0000-4000-8000-0000000000f8"
	seedGenGoat(t, ctx, pool, goatID, "alive")

	sopID := scanText(t, ctx, pool,
		`INSERT INTO sop_definitions (tenant_id, code, name, description, status)
		 VALUES ($1, 'vaccination.drive', 'Vaccination Drive', 'Multi obligation fanout', 'active')
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
		 VALUES ($1, $2, $3, 'vaccination_drive', 'Multi Drive', 'park', $4)
		 RETURNING task_id::text`, impTenant, sopID, sopVersionID, impCbe)

	for i, ruleID := range []string{ruleA, ruleB, ruleC} {
		obligationID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
			IdempotencyKey: "multi-fanout-obligation-" + string(rune('a'+i)), Sequence: int32(i + 1),
		})
		if err != nil || !applied {
			t.Fatalf("obligation %d: applied=%v err=%v", i, applied, err)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND obligation_id=$3`,
			taskID, impTenant, obligationID); err != nil {
			t.Fatalf("link obligation task: %v", err)
		}
	}

	submissionID := scanText(t, ctx, pool,
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state)
		 VALUES ($1, $2, $3, $4, 'multi-fanout-submission',
		   '{"administered_at":"2026-06-23T00:00:00Z"}'::jsonb,
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
	if count != 3 {
		t.Fatalf("materialized count = %d, want 3 obligations for one scanned goat", count)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND sop_submission_item_id IN (
		   SELECT item_id FROM sop_submission_items WHERE tenant_id=$1 AND submission_id=$2
		 )`, impTenant, submissionID); got != 3 {
		t.Fatalf("completion rows = %d, want 3", got)
	}

	replay, err := vacc.RecordCompletionsFromSubmission(ctx, impTenant, taskID, submissionID, impParty)
	if err != nil {
		t.Fatalf("RecordCompletionsFromSubmission() replay error = %v", err)
	}
	if replay != 3 {
		t.Fatalf("replay materialized count = %d, want existing 3", replay)
	}
}

func TestRecordCompletionsFromSubmissionFailsPartialMaterialization(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "materialized 1 of 2") {
		t.Fatalf("first fanout count=%d err=%v, want partial materialization error", count, err)
	}
	if count != 1 {
		t.Fatalf("first fanout count=%d, want 1 materialized row", count)
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

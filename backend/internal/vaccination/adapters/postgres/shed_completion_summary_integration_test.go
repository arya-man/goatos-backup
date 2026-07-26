package postgres

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// ---------------------------------------------------------------------------
// Adversarial regression tests for ShedCompletionSummary (aggregate projection).
//
// These prove the four dimensions the aggregate-projection guard requires:
//   - cardinality (OneToMany): a goat with many proof clips / a rule with many
//     dimension rows must not inflate expected/handled/proof_ready or the
//     vaccine breakdown.
//   - pagination (PageBoundary/MultiPage): counts are whole-shed totals, never
//     truncated by a page size.
//   - scope (ScopeHierarchy/ParkScope): the summary is scoped to the one shed
//     drive and does not bleed animals from another shed in the same park.
//   - status (StatusBuckets): expected excludes terminal obligations; over-scan
//     and under-scan both block submit; submit is enabled only when
//     handled == expected == proof_ready.
// ---------------------------------------------------------------------------

// scsFixture provisions a published protocol version + rule and returns their ids
// so each test can attach a shed drive (SOP task + batch + obligations) to them.
func scsSeedProtocol(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (versionID, ruleID string) {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.scs", Name: "SCS", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err = proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"source":{"source_system":"pc","source_ref":"PC §1","review_status":"approved","approved_by":"R"}}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return versionID, ruleID
}

// scsSeedDrive creates a shed-scoped SOP task and its obligation batch, linked, and
// returns (taskID, batchID). shedID must already exist (seedShedOperational).
func scsSeedDrive(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, shedID, code string, lot *string) (taskID, batchID string) {
	t.Helper()
	sopID := scanText(t, ctx, pool,
		`INSERT INTO sop_definitions (tenant_id, code, name, description, status)
		 VALUES ($1, $2, 'Vaccination Drive', 'shed completion summary test', 'active')
		 RETURNING sop_id::text`, impTenant, "vaccination.drive."+code)
	sopVersionID := scanText(t, ctx, pool,
		`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[{"key":"goat_ids","type":"goat_scan","repeat":true}]}'::jsonb,
		   '{"required":true,"subject_scope":"goat","types":["video"],"minimum_count":1}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)
		 RETURNING sop_version_id::text`, impTenant, sopID)
	taskID = scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id, state)
		 VALUES ($1, $2, $3, 'vaccination_drive', 'Drive', 'shed', $4, 'assigned')
		 RETURNING task_id::text`, impTenant, sopID, sopVersionID, shedID)
	batchID = scanText(t, ctx, pool,
		`INSERT INTO obligation_batches (tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id, primary_inventory_lot_id)
		 VALUES ($1, $2, 'tenant', $1, 'in_progress', 0, $3, $4)
		 RETURNING batch_id::text`, impTenant, versionID, taskID, lot)
	return taskID, batchID
}

// scsSeedParkDrive creates the shape produced by the local sweeper today: the SOP
// task/batch are park-scoped, but the obligations inside it still belong to one
// concrete shed through goat placement. The mobile shed-completion summary must
// show that human shed name, never the generic task title.
func scsSeedParkDrive(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, parkID, code string, lot *string) (taskID, batchID string) {
	t.Helper()
	sopID := scanText(t, ctx, pool,
		`INSERT INTO sop_definitions (tenant_id, code, name, description, status)
		 VALUES ($1, $2, 'Vaccination Drive', 'park-scoped shed completion summary test', 'active')
		 RETURNING sop_id::text`, impTenant, "vaccination.park.drive."+code)
	sopVersionID := scanText(t, ctx, pool,
		`INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[{"key":"goat_ids","type":"goat_scan","repeat":true}]}'::jsonb,
		   '{"required":true,"subject_scope":"goat","types":["video"],"minimum_count":1}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)
		 RETURNING sop_version_id::text`, impTenant, sopID)
	taskID = scanText(t, ctx, pool,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id, state)
		 VALUES ($1, $2, $3, 'vaccination_drive', 'Vaccination drive', 'park', $4, 'assigned')
		 RETURNING task_id::text`, impTenant, sopID, sopVersionID, parkID)
	batchID = scanText(t, ctx, pool,
		`INSERT INTO obligation_batches (tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id, primary_inventory_lot_id)
		 VALUES ($1, $2, 'park', $3, 'in_progress', 0, $4, $5)
		 RETURNING batch_id::text`, impTenant, versionID, parkID, taskID, lot)
	return taskID, batchID
}

// scsSeedRuleDim inserts one protocol_rule_dimensions row (a selector) with a vaccine label.
func scsSeedRuleDim(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, selectorKey, vaccineType string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO protocol_rule_dimensions (tenant_id, protocol_version_id, rule_id, category, selector_key, vaccine_type)
		 VALUES ($1, $2, $3, 'vaccination', $4, $5)`, impTenant, versionID, ruleID, selectorKey, vaccineType); err != nil {
		t.Fatalf("rule dimension %s: %v", selectorKey, err)
	}
}

func TestShedCompletionSummaryParkScopedTaskUsesObligationShedAndRuleDSLVaccine(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	if _, err := pool.Exec(ctx,
		`UPDATE protocol_versions
		 SET rule_dsl = jsonb_set(rule_dsl, '{vaccine}', '{"name":"ET+TT","code":"ET+TT"}'::jsonb, true)
		 WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); err != nil {
		t.Fatalf("patch rule_dsl vaccine: %v", err)
	}
	const shed = "32000000-0000-4000-8000-0000000000c2"
	seedShedOperational(t, ctx, pool, shed, "SHED-C", true, false, false)
	taskID, batchID := scsSeedParkDrive(t, ctx, pool, versionID, impCbe, "park-scope", nil)

	const goatID = "33000000-0000-4000-8000-0000000000c1"
	seedGoatAtShed(t, ctx, pool, goatID, shed)
	scsSeedObligation(t, ctx, pool, versionID, ruleID, batchID, goatID, "scheduled", 1)

	vacc := NewRepository(pool, 5*time.Second)
	got, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, "")
	if err != nil {
		t.Fatalf("ShedCompletionSummary: %v", err)
	}
	if got.ShedName != "SHED-C" {
		t.Fatalf("shed name = %q, want SHED-C from obligation goat placement", got.ShedName)
	}
	if len(got.VaccineBreakdown) != 1 || got.VaccineBreakdown[0].Vaccine != "ET+TT" || got.VaccineBreakdown[0].Count != 1 {
		t.Fatalf("vaccine breakdown = %+v, want ET+TT x1 from rule_dsl fallback", got.VaccineBreakdown)
	}
}

// scsSeedObligation inserts one obligation on the batch for a goat with an explicit status/sequence.
func scsSeedObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, batchID, goatID, status string, seq int) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO obligation_instances
		   (tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sequence, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'tenant', $1, DATE '2026-06-23', $6, $7, $8)`,
		impTenant, versionID, ruleID, batchID, goatID, status, seq,
		"scs:"+batchID+":"+goatID+":"+status+":"+time.Now().Format("150405.000000000")+":"+strconv.Itoa(seq)); err != nil {
		t.Fatalf("obligation %s/%s: %v", goatID, status, err)
	}
}

func scsSeedScan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, goatID, tag string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO sop_task_scan_captures
		   (tenant_id, task_id, field_key, tag, normalized_tag, goat_id, captured_by, idempotency_key)
		 VALUES ($1, $2, '__scan_roster__', $3, $3, $4, $5, $6)`,
		impTenant, taskID, tag, goatID, impParty, "scs-scan:"+taskID+":"+tag); err != nil {
		t.Fatalf("scan %s: %v", goatID, err)
	}
}

func scsSeedGoatProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, goatID, objectKey, uploadState string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO proof_artifacts
		   (tenant_id, storage_provider, object_key, scope_type, scope_id, subject_type, subject_id, proof_type, upload_state)
		 VALUES ($1, 'local', $2, 'task', $3, 'goat', $4, 'video', $5)`,
		impTenant, objectKey, taskID, goatID, uploadState); err != nil {
		t.Fatalf("proof %s: %v", goatID, err)
	}
}

func scsSwitchTaskToShedLevelProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`UPDATE sop_versions sv
		    SET proof_policy = '{"required":true,"proof_mode":"shed_level_video","subject_scope":"shed","types":["video"],"minimum_count":1,"maximum_count":5,"maximum_count_per_subject":5}'::jsonb
		   FROM sop_tasks st
		  WHERE st.tenant_id = sv.tenant_id
		    AND st.sop_version_id = sv.sop_version_id
		    AND st.tenant_id = $1
		    AND st.task_id = $2`,
		impTenant, taskID); err != nil {
		t.Fatalf("switch task to shed-level proof: %v", err)
	}
}

func scsSeedShedProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, shedID, objectKey, uploadState string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO proof_artifacts
		   (tenant_id, storage_provider, object_key, scope_type, scope_id, subject_type, subject_id, proof_type, upload_state)
		 VALUES ($1, 'local', $2, 'shed', $3, 'shed', $3, 'video', $4)`,
		impTenant, objectKey, shedID, uploadState); err != nil {
		t.Fatalf("shed proof %s: %v", shedID, err)
	}
}

func scsSeedShedProofReturningID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, objectKey, uploadState string) string {
	t.Helper()
	return scanText(t, ctx, pool,
		`INSERT INTO proof_artifacts
		   (tenant_id, storage_provider, object_key, scope_type, scope_id, subject_type, subject_id, proof_type, upload_state)
		 VALUES ($1, 'local', $2, 'shed', $3, 'shed', $3, 'video', $4)
		 RETURNING proof_id::text`,
		impTenant, objectKey, shedID, uploadState)
}

func scsSeedShedSubmission(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, shedID, proofID, state string) {
	t.Helper()
	proofRefs := fmt.Sprintf(`[{"proof_id":%q,"proof_type":"video","subject_type":"shed","subject_id":%q,"upload_state":"completed"}]`, proofID, shedID)
	if _, err := pool.Exec(ctx,
		`INSERT INTO sop_submissions
		   (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state, submitted_at)
		 SELECT st.tenant_id, st.task_id, st.sop_version_id, $3, $4, '{}'::jsonb, $5::jsonb, $6, now()
		   FROM sop_tasks st
		  WHERE st.tenant_id = $1 AND st.task_id = $2`,
		impTenant, taskID, impParty, "scs-submit:"+taskID+":"+shedID+":"+proofID, proofRefs, state); err != nil {
		t.Fatalf("shed submission %s/%s: %v", shedID, state, err)
	}
}

// TestShedCompletionSummaryShedLevelProofOneToManyPageBoundaryParkScopeStatusBuckets
// is the shed-video-mode sibling of the older per-goat proof adversarial tests.
// It covers the aggregate guard dimensions touched by the proof-mode projection:
// OneToMany proof rows must count only completed shed clips, PageBoundary counts
// must stay whole-shed totals, ParkScope must not bleed another shed in the same
// park, and StatusBuckets must still exclude terminal obligations.
func TestShedCompletionSummaryShedLevelProofOneToManyPageBoundaryParkScopeStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	const shedA = "35000000-0000-4000-8000-0000000000a1"
	const shedB = "35000000-0000-4000-8000-0000000000b1"
	seedShedOperational(t, ctx, pool, shedA, "SHED-A", true, false, false)
	seedShedOperational(t, ctx, pool, shedB, "SHED-B", true, false, false)
	scsSeedRuleDim(t, ctx, pool, versionID, ruleID, "sel-a", "ET+TT")
	scsSeedRuleDim(t, ctx, pool, versionID, ruleID, "sel-b", "ET+TT")

	taskA, batchA := scsSeedDrive(t, ctx, pool, versionID, shedA, "shed-proof-a", nil)
	taskB, batchB := scsSeedDrive(t, ctx, pool, versionID, shedB, "shed-proof-b", nil)
	scsSwitchTaskToShedLevelProof(t, ctx, pool, taskA)
	scsSwitchTaskToShedLevelProof(t, ctx, pool, taskB)

	const totalA = 25 // more than one phone page, but the summary is not page-limited.
	for i := 0; i < totalA; i++ {
		goatID := "35000000-0000-4000-8000-0000000001" + fmt.Sprintf("%02d", i)
		seedGoatAtShed(t, ctx, pool, goatID, shedA)
		scsSeedObligation(t, ctx, pool, versionID, ruleID, batchA, goatID, "scheduled", 1)
		scsSeedScan(t, ctx, pool, taskA, goatID, "A"+strconv.Itoa(i))
	}
	// Shed B proves scope isolation. If the summary bleeds same-park/sibling-shed rows,
	// shed A's expected count would become 28.
	for i := 0; i < 3; i++ {
		goatID := "35000000-0000-4000-8000-0000000002" + fmt.Sprintf("%02d", i)
		seedGoatAtShed(t, ctx, pool, goatID, shedB)
		scsSeedObligation(t, ctx, pool, versionID, ruleID, batchB, goatID, "scheduled", 1)
		scsSeedScan(t, ctx, pool, taskB, goatID, "B"+strconv.Itoa(i))
	}
	// Terminal obligation in shed A must not inflate expected_count.
	const terminalGoat = "35000000-0000-4000-8000-000000000099"
	seedGoatAtShed(t, ctx, pool, terminalGoat, shedA)
	scsSeedObligation(t, ctx, pool, versionID, ruleID, batchA, terminalGoat, "completed", 99)

	// Multiple shed proof artifacts are allowed up to five. Proof readiness in shed mode is
	// the completed shed proof count, not one proof per goat.
	scsSeedShedProof(t, ctx, pool, taskA, shedA, "shed-clip-a", "completed")
	scsSeedShedProof(t, ctx, pool, taskA, shedA, "shed-clip-b", "completed")
	scsSeedShedProof(t, ctx, pool, taskA, shedA, "shed-clip-pending", "pending")
	scsSeedShedProof(t, ctx, pool, taskB, shedB, "shed-b-clip", "completed")

	vacc := NewRepository(pool, 5*time.Second)
	got, err := vacc.ShedCompletionSummary(ctx, impTenant, taskA, "")
	if err != nil {
		t.Fatalf("ShedCompletionSummary: %v", err)
	}
	if got.ProofMode != "shed_level_video" {
		t.Fatalf("proof mode = %q, want shed_level_video", got.ProofMode)
	}
	if got.ExpectedCount != totalA || got.HandledCount != totalA {
		t.Fatalf("wrong shed totals: expected=%d handled=%d, want %d/%d", got.ExpectedCount, got.HandledCount, totalA, totalA)
	}
	if got.ProofReadyCount != 2 {
		t.Fatalf("shed proof count = %d, want 2 completed shed clips only", got.ProofReadyCount)
	}
	if len(got.VaccineBreakdown) != 1 || got.VaccineBreakdown[0].Vaccine != "ET+TT" || got.VaccineBreakdown[0].Count != totalA {
		t.Fatalf("vaccine breakdown = %+v, want ET+TT x%d", got.VaccineBreakdown, totalA)
	}
	if !got.SubmitEnabled || got.BlockingReason != nil {
		t.Fatalf("shed-level submit should be enabled with all scans and 1..5 shed videos: enabled=%v reason=%v", got.SubmitEnabled, got.BlockingReason)
	}
}

func TestShedCompletionSummaryShedLevelSubmitStateOneToManyPageBoundaryParkScopeStatusBucketsPerShedSubmissionNotParentTask(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	const shedA = "36000000-0000-4000-8000-0000000000a1"
	const shedB = "36000000-0000-4000-8000-0000000000b1"
	seedShedOperational(t, ctx, pool, shedA, "Old Yashoda", true, false, false)
	seedShedOperational(t, ctx, pool, shedB, "Godel 1", true, false, false)

	taskID, batchID := scsSeedParkDrive(t, ctx, pool, versionID, impCbe, "per-shed-submit-state", nil)
	scsSwitchTaskToShedLevelProof(t, ctx, pool, taskID)
	if _, err := pool.Exec(ctx, `UPDATE sop_tasks SET state='needs_review' WHERE tenant_id=$1 AND task_id=$2`, impTenant, taskID); err != nil {
		t.Fatalf("mark parent submitted: %v", err)
	}

	for i, pair := range []struct {
		shedID string
		prefix string
	}{{shedA, "old"}, {shedB, "godel"}} {
		goatID := fmt.Sprintf("36000000-0000-4000-8000-0000000001%02d", i)
		seedGoatAtShed(t, ctx, pool, goatID, pair.shedID)
		scsSeedObligation(t, ctx, pool, versionID, ruleID, batchID, goatID, "scheduled", i+1)
		scsSeedScan(t, ctx, pool, taskID, goatID, pair.prefix+"-tag")
	}
	oldProofID := scsSeedShedProofReturningID(t, ctx, pool, shedA, "old-yashoda-submitted", "completed")
	scsSeedShedSubmission(t, ctx, pool, taskID, shedA, oldProofID, "needs_review")
	scsSeedShedProof(t, ctx, pool, taskID, shedB, "godel-uploaded-not-submitted", "completed")

	vacc := NewRepository(pool, 5*time.Second)
	oldYashoda, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, shedA)
	if err != nil {
		t.Fatalf("old yashoda summary: %v", err)
	}
	godel, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, shedB)
	if err != nil {
		t.Fatalf("godel summary: %v", err)
	}
	if oldYashoda.SubmitState != "submitted" {
		t.Fatalf("old yashoda submit_state = %q, want submitted", oldYashoda.SubmitState)
	}
	if godel.ProofReadyCount != 1 || !godel.SubmitEnabled {
		t.Fatalf("godel readiness = proof %d enabled %v, want uploaded and ready", godel.ProofReadyCount, godel.SubmitEnabled)
	}
	if godel.SubmitState != "draft" {
		t.Fatalf("godel inherited parent/submitted shed state: submit_state = %q, want draft", godel.SubmitState)
	}
}

// TestShedCompletionSummaryOneToMany proves the aggregate does not double-count when a goat
// carries MULTIPLE proof clips and its rule has MULTIPLE dimension rows. A naive COUNT(*) over a
// JOIN to protocol_rule_dimensions (2 rows) or proof_artifacts (2 clips) would inflate the counts.
func TestShedCompletionSummaryOneToManyScanProofRegression(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	seedShedOperational(t, ctx, pool, impShed, "SHED-1", true, false, false)
	// Two selector rows for the SAME rule — the vaccine breakdown must collapse to one label per rule.
	scsSeedRuleDim(t, ctx, pool, versionID, ruleID, "sel-a", "PPR")
	scsSeedRuleDim(t, ctx, pool, versionID, ruleID, "sel-b", "PPR")

	taskID, batchID := scsSeedDrive(t, ctx, pool, versionID, impShed, "otm", nil)

	const g1 = "31000000-0000-4000-8000-0000000000a1"
	seedGenGoat(t, ctx, pool, g1, "alive")
	scsSeedObligation(t, ctx, pool, versionID, ruleID, batchID, g1, "scheduled", 1)
	// Same goat scanned once (its unique captures already deduped by the scan unique index), but
	// TWO completed proof clips — proof_ready must count the goat once, not twice.
	scsSeedScan(t, ctx, pool, taskID, g1, "TAG-1")
	scsSeedGoatProof(t, ctx, pool, taskID, g1, "clip-a", "completed")
	scsSeedGoatProof(t, ctx, pool, taskID, g1, "clip-b", "completed")

	vacc := NewRepository(pool, 5*time.Second)
	got, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, "")
	if err != nil {
		t.Fatalf("ShedCompletionSummary: %v", err)
	}
	if got.ExpectedCount != 1 || got.HandledCount != 1 || got.ProofReadyCount != 1 {
		t.Fatalf("counts double-counted: expected=%d handled=%d proof=%d, want 1/1/1", got.ExpectedCount, got.HandledCount, got.ProofReadyCount)
	}
	if len(got.VaccineBreakdown) != 1 || got.VaccineBreakdown[0].Vaccine != "PPR" || got.VaccineBreakdown[0].Count != 1 {
		t.Fatalf("vaccine breakdown fanned out: %+v, want one PPR x1", got.VaccineBreakdown)
	}
	if got.ShedName != "SHED-1" {
		t.Fatalf("shed name = %q, want human SHED-1 (never a raw UUID)", got.ShedName)
	}
	if !got.SubmitEnabled || got.BlockingReason != nil {
		t.Fatalf("submit should be enabled at 1/1/1: enabled=%v reason=%v", got.SubmitEnabled, got.BlockingReason)
	}
}

// TestShedCompletionSummaryPageBoundary proves the counts are whole-shed totals: a drive with far
// more animals than any UI page size (25 > the 20-row phone page) reports all 25, and there is no
// LIMIT/OFFSET truncating the count.
func TestShedCompletionSummaryPageBoundaryScanProofRegression(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	seedShedOperational(t, ctx, pool, impShed, "SHED-1", true, false, false)
	scsSeedRuleDim(t, ctx, pool, versionID, ruleID, "sel-a", "PPR")
	taskID, batchID := scsSeedDrive(t, ctx, pool, versionID, impShed, "page", nil)

	const total = 25 // beyond a 20-row page boundary
	for i := 0; i < total; i++ {
		goatID := "31000000-0000-4000-8000-0000000001" + fmt.Sprintf("%02d", i)
		seedGenGoat(t, ctx, pool, goatID, "alive")
		scsSeedObligation(t, ctx, pool, versionID, ruleID, batchID, goatID, "scheduled", 1)
		scsSeedScan(t, ctx, pool, taskID, goatID, "T"+strconv.Itoa(i))
		scsSeedGoatProof(t, ctx, pool, taskID, goatID, "clip-"+strconv.Itoa(i), "completed")
	}

	vacc := NewRepository(pool, 5*time.Second)
	got, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, "")
	if err != nil {
		t.Fatalf("ShedCompletionSummary: %v", err)
	}
	if got.ExpectedCount != total || got.HandledCount != total || got.ProofReadyCount != total {
		t.Fatalf("page-truncated counts: expected=%d handled=%d proof=%d, want %d each", got.ExpectedCount, got.HandledCount, got.ProofReadyCount, total)
	}
	if !got.SubmitEnabled {
		t.Fatalf("submit should be enabled when all %d animals handled+proofed", total)
	}
}

// TestShedCompletionSummaryParkScope proves scope isolation: two sheds under the same park each have
// their own drive; the summary for shed A must count only shed A's animals, never shed B's.
func TestShedCompletionSummaryParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	const shedA = "32000000-0000-4000-8000-0000000000a1"
	const shedB = "32000000-0000-4000-8000-0000000000b1"
	seedShedOperational(t, ctx, pool, shedA, "SHED-A", true, false, false)
	seedShedOperational(t, ctx, pool, shedB, "SHED-B", true, false, false)
	scsSeedRuleDim(t, ctx, pool, versionID, ruleID, "sel-a", "PPR")

	taskA, batchA := scsSeedDrive(t, ctx, pool, versionID, shedA, "a", nil)
	taskB, batchB := scsSeedDrive(t, ctx, pool, versionID, shedB, "b", nil)

	// Shed A: 2 animals. Shed B: 3 animals. Both fully scanned+proofed.
	aGoats := []string{"33000000-0000-4000-8000-0000000000a1", "33000000-0000-4000-8000-0000000000a2"}
	bGoats := []string{"33000000-0000-4000-8000-0000000000b1", "33000000-0000-4000-8000-0000000000b2", "33000000-0000-4000-8000-0000000000b3"}
	for i, g := range aGoats {
		seedGoatAtShed(t, ctx, pool, g, shedA)
		scsSeedObligation(t, ctx, pool, versionID, ruleID, batchA, g, "scheduled", 1)
		scsSeedScan(t, ctx, pool, taskA, g, "A"+strconv.Itoa(i))
		scsSeedGoatProof(t, ctx, pool, taskA, g, "a-clip-"+strconv.Itoa(i), "completed")
	}
	for i, g := range bGoats {
		seedGoatAtShed(t, ctx, pool, g, shedB)
		scsSeedObligation(t, ctx, pool, versionID, ruleID, batchB, g, "scheduled", 1)
		scsSeedScan(t, ctx, pool, taskB, g, "B"+strconv.Itoa(i))
		scsSeedGoatProof(t, ctx, pool, taskB, g, "b-clip-"+strconv.Itoa(i), "completed")
	}

	vacc := NewRepository(pool, 5*time.Second)
	gotA, err := vacc.ShedCompletionSummary(ctx, impTenant, taskA, "")
	if err != nil {
		t.Fatalf("summary A: %v", err)
	}
	if gotA.ExpectedCount != 2 || gotA.HandledCount != 2 || gotA.ProofReadyCount != 2 {
		t.Fatalf("shed A bled shed B's animals: expected=%d handled=%d proof=%d, want 2/2/2", gotA.ExpectedCount, gotA.HandledCount, gotA.ProofReadyCount)
	}
	if gotA.ShedName != "SHED-A" {
		t.Fatalf("shed A name = %q, want SHED-A", gotA.ShedName)
	}
	gotB, err := vacc.ShedCompletionSummary(ctx, impTenant, taskB, "")
	if err != nil {
		t.Fatalf("summary B: %v", err)
	}
	if gotB.ExpectedCount != 3 || gotB.ShedName != "SHED-B" {
		t.Fatalf("shed B summary wrong: expected=%d name=%q, want 3/SHED-B", gotB.ExpectedCount, gotB.ShedName)
	}
}

// TestShedCompletionSummaryStatusBuckets proves the status matrix: expected excludes every terminal
// obligation status, and submit is strictly gated on handled == expected == proof_ready (over-scan
// and under-scan both block). The live obligation status set comes from the migration CHECK
// constraint: scheduled/due/in_progress/deferred/missed live; completed/waived/canceled/superseded
// terminal.
func TestShedCompletionSummaryStatusBucketsScanProofRegression(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	seedShedOperational(t, ctx, pool, impShed, "SHED-1", true, false, false)
	scsSeedRuleDim(t, ctx, pool, versionID, ruleID, "sel-a", "PPR")
	taskID, batchID := scsSeedDrive(t, ctx, pool, versionID, impShed, "status", nil)

	// Live obligations (count toward expected): scheduled, due, in_progress, deferred, missed.
	live := []string{"scheduled", "due", "in_progress", "deferred", "missed"}
	// Terminal obligations (excluded from expected): completed, waived, canceled, superseded.
	terminal := []string{"completed", "waived", "canceled", "superseded"}

	seq := 1
	liveGoats := make([]string, 0, len(live))
	for i, st := range live {
		g := "34000000-0000-4000-8000-0000000000" + fmt.Sprintf("%02d", i)
		seedGenGoat(t, ctx, pool, g, "alive")
		scsSeedObligation(t, ctx, pool, versionID, ruleID, batchID, g, st, seq)
		liveGoats = append(liveGoats, g)
		seq++
	}
	for i, st := range terminal {
		g := "34000000-0000-4000-8000-0000000000" + fmt.Sprintf("%02d", 50+i)
		seedGenGoat(t, ctx, pool, g, "alive")
		scsSeedObligation(t, ctx, pool, versionID, ruleID, batchID, g, st, seq)
		seq++
	}

	vacc := NewRepository(pool, 5*time.Second)

	// Phase 1: under-scan — only 3 of 5 live animals scanned+proofed → blocked.
	for i := 0; i < 3; i++ {
		scsSeedScan(t, ctx, pool, taskID, liveGoats[i], "S"+strconv.Itoa(i))
		scsSeedGoatProof(t, ctx, pool, taskID, liveGoats[i], "s-clip-"+strconv.Itoa(i), "completed")
	}
	under, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, "")
	if err != nil {
		t.Fatalf("under-scan summary: %v", err)
	}
	if under.ExpectedCount != int64(len(live)) {
		t.Fatalf("expected excluded/over-included terminal: expected=%d, want %d (live only)", under.ExpectedCount, len(live))
	}
	if under.SubmitEnabled || under.BlockingReason == nil {
		t.Fatalf("under-scan must block: enabled=%v reason=%v", under.SubmitEnabled, under.BlockingReason)
	}

	// Phase 2: exact — all 5 live animals scanned+proofed → enabled.
	for i := 3; i < len(liveGoats); i++ {
		scsSeedScan(t, ctx, pool, taskID, liveGoats[i], "S"+strconv.Itoa(i))
		scsSeedGoatProof(t, ctx, pool, taskID, liveGoats[i], "s-clip-"+strconv.Itoa(i), "completed")
	}
	exact, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, "")
	if err != nil {
		t.Fatalf("exact summary: %v", err)
	}
	if !exact.SubmitEnabled || exact.BlockingReason != nil {
		t.Fatalf("exact match must enable submit: enabled=%v reason=%v (handled=%d expected=%d proof=%d)",
			exact.SubmitEnabled, exact.BlockingReason, exact.HandledCount, exact.ExpectedCount, exact.ProofReadyCount)
	}

	// Phase 3: over-scan — scan a 6th animal that is NOT an expected (terminal-obligation) goat →
	// handled (6) > expected (5) must block, preventing the silent-drop data-loss path.
	extra := "34000000-0000-4000-8000-0000000000" + fmt.Sprintf("%02d", 50) // a completed-obligation goat
	scsSeedScan(t, ctx, pool, taskID, extra, "S-extra")
	scsSeedGoatProof(t, ctx, pool, taskID, extra, "s-clip-extra", "completed")
	over, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, "")
	if err != nil {
		t.Fatalf("over-scan summary: %v", err)
	}
	if over.HandledCount <= over.ExpectedCount {
		t.Fatalf("over-scan fixture invalid: handled=%d expected=%d (want handled>expected)", over.HandledCount, over.ExpectedCount)
	}
	if over.SubmitEnabled || over.BlockingReason == nil {
		t.Fatalf("over-scan must block: enabled=%v reason=%v", over.SubmitEnabled, over.BlockingReason)
	}
}

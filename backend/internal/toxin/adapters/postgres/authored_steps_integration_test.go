package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// Exercise real writes at the authored limit, including the video and reading
// paths. The old database constraint rejects step 8 despite successful publication.
func TestAuthoredTwentyStepRoundPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	dsl := domain.ToxinDSL{SchemaVersion: domain.ToxinSchemaVersion}
	for no := 1; no <= 20; no++ {
		kind := domain.StepKindVideo
		if no == 20 {
			kind = domain.StepKindPhotoReading
		}
		dsl.Steps = append(dsl.Steps, domain.ToxinDSLStep{No: no, Kind: kind, Title: fmt.Sprintf("Step %d", no), Instruction: "Record this step"})
	}
	if problems := domain.ValidateToxin(dsl); len(problems) != 0 {
		t.Fatal(problems)
	}
	_, err := pool.Exec(ctx, `UPDATE sop_versions v SET status='retired' FROM sop_definitions d WHERE d.tenant_id=v.tenant_id AND d.sop_id=v.sop_id AND d.tenant_id=$1 AND d.code='procurement.toxin_test' AND v.status='published'`, toxinTestTenant)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"toxin": dsl})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
 SELECT tenant_id, sop_id, 2, 'Twenty steps', 'published', $2::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, now()
 FROM sop_definitions WHERE tenant_id=$1 AND code='procurement.toxin_test'`, toxinTestTenant, raw)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.CreateTaskFromPurchase(ctx, toxinCreateParams(toxinTestPurchase)); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListTasks(ctx, ports.ListTasksParams{TenantID: toxinTestTenant, Limit: 20})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("list: %+v, %v", page, err)
	}
	task := page.Rows[0].Task
	if task.SOPVersion != 2 {
		t.Fatalf("version=%d", task.SOPVersion)
	}
	proc, err := NewProcedureSource(pool).ProcedureVersion(ctx, toxinTestTenant, task.SOPVersion)
	if err != nil {
		t.Fatal(err)
	}
	for no := 1; no < 20; no++ {
		_, err := repo.CompleteStep(ctx, ports.CompleteStepParams{TenantID: toxinTestTenant, TaskID: task.TaskID, StepNo: no, ProofRef: fmt.Sprintf("long-proof-%d", no), IdempotencyKey: fmt.Sprintf("long-step-%d", no), Now: time.Now(), Procedure: proc})
		if err != nil {
			t.Fatalf("step %d: %v", no, err)
		}
	}
	row, err := repo.SubmitReading(ctx, ports.SubmitParams{TenantID: toxinTestTenant, TaskID: task.TaskID, Outcome: domain.OutcomeNegative, StripPhotoRef: "long-strip", IdempotencyKey: "long-submit", Now: time.Now(), Procedure: proc})
	if err != nil {
		t.Fatal(err)
	}
	if row.Task.Status != domain.StatusPendingReview {
		t.Fatalf("status=%s", row.Task.Status)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM toxin_test_step_completions WHERE tenant_id=$1 AND task_id=$2`, toxinTestTenant, task.TaskID).Scan(&count); err != nil || count != 20 {
		t.Fatalf("completions=%d err=%v", count, err)
	}
	for _, no := range []int{0, 21} {
		_, err := pool.Exec(ctx, `INSERT INTO toxin_test_step_completions (tenant_id,task_id,step_no,proof_ref) VALUES ($1,$2,$3,$4)`, toxinTestTenant, task.TaskID, no, fmt.Sprintf("invalid-%d", no))
		if err == nil {
			t.Fatalf("database accepted out-of-range step %d", no)
		}
	}
}

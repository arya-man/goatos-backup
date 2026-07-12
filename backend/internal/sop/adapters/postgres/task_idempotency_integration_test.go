package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

func TestCreateTaskIsIdempotentByObligationBatchContext(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	sopVersionID := "b0000000-0000-4000-8000-000000000002"
	batchID := "86000000-0000-4000-8000-000000101001"
	cmd := ports.CreateTaskCommand{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "86000000-0000-4000-8000-000000101999",
		Body: domain.CreateTaskRequest{
			SOPVersionID: &sopVersionID,
			TaskType:     "vaccination",
			Title:        "Vaccination drive CBE",
			ScopeType:    "tenant",
			ScopeID:      "00000000-0000-4000-8000-000000000001",
			Priority:     "normal",
			Context:      map[string]any{"created_by": "obligation-sweeper", "obligation_batch_id": batchID},
		},
	}

	first, err := repo.CreateTask(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateTask first: %v", err)
	}
	replay, err := repo.CreateTask(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateTask replay: %v", err)
	}
	if replay.TaskID != first.TaskID {
		t.Fatalf("replay task_id=%s, want %s", replay.TaskID, first.TaskID)
	}
	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM sop_tasks
WHERE tenant_id = $1::uuid
  AND context ->> 'obligation_batch_id' = $2`, cmd.TenantID, batchID).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("batch-keyed SOP task rows=%d, want 1", count)
	}
}

func TestCreateTasksForBatchesIsSetBasedAndReplaySafe(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	const (
		tenantID    = "00000000-0000-4000-8000-000000000001"
		actorID     = "86000000-0000-4000-8000-000000101999"
		sopVersion  = "b0000000-0000-4000-8000-000000000002"
		firstBatch  = "86000000-0000-4000-8000-000000101101"
		secondBatch = "86000000-0000-4000-8000-000000101102"
	)
	tasks := []domain.BatchTaskRequest{
		{BatchID: firstBatch, TaskType: "vaccination", Title: "First drive", ScopeType: "tenant", ScopeID: tenantID},
		{BatchID: secondBatch, TaskType: "vaccination", Title: "Second drive", ScopeType: "tenant", ScopeID: tenantID},
	}
	first, err := repo.CreateTasksForBatches(ctx, tenantID, sopVersion, actorID, tasks)
	if err != nil {
		t.Fatalf("CreateTasksForBatches first: %v", err)
	}
	if len(first) != 2 || first[firstBatch] == "" || first[secondBatch] == "" {
		t.Fatalf("first mapping = %#v, want both batches", first)
	}
	replay, err := repo.CreateTasksForBatches(ctx, tenantID, sopVersion, actorID, tasks)
	if err != nil {
		t.Fatalf("CreateTasksForBatches replay: %v", err)
	}
	if replay[firstBatch] != first[firstBatch] || replay[secondBatch] != first[secondBatch] {
		t.Fatalf("replay mapping = %#v, want stable %#v", replay, first)
	}
	var taskCount, auditCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM sop_tasks WHERE tenant_id=$1::uuid AND context->>'obligation_batch_id'=ANY($2::text[])`, tenantID, []string{firstBatch, secondBatch}).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM audit_log WHERE tenant_id=$1::uuid AND action='sop.task.create' AND metadata->>'obligation_batch_id'=ANY($2::text[])`, tenantID, []string{firstBatch, secondBatch}).Scan(&auditCount); err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if taskCount != 2 || auditCount != 2 {
		t.Fatalf("task/audit rows = %d/%d, want exactly 2/2 after replay", taskCount, auditCount)
	}
}

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	transportOperator = "fd000000-0000-4000-8000-000000009001"
	otherOperator     = "fd000000-0000-4000-8000-000000009002"
	transportVerifier = "fd000000-0000-4000-8000-000000009003"
)

func TestFeedTransportDailyShedWorkflowRetainsRejectedAttempt(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	day := time.Date(2026, 7, 29, 0, 0, 0, 0, biztime.DefaultLocation())
	before, err := repo.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{TenantID: fdTenant, AsOf: day.Add(15*time.Hour + 29*time.Minute + 59*time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if before.Inserted != 0 {
		t.Fatalf("15:29 inserted=%d want 0", before.Inserted)
	}
	after, err := repo.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{TenantID: fdTenant, AsOf: day.Add(15*time.Hour + 30*time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if after.Inserted != 2 {
		t.Fatalf("15:30 inserted=%d want 2 active sheds", after.Inserted)
	}
	replay, err := repo.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{TenantID: fdTenant, AsOf: day.Add(16 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Inserted != 0 {
		t.Fatalf("replay inserted=%d want 0", replay.Inserted)
	}
	page, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	tasks := page.Items
	if len(tasks) != 2 {
		t.Fatalf("tasks=%d want 2", len(tasks))
	}
	if len(page.Filters.Parks) != 1 || len(page.Filters.Sheds) != 2 {
		t.Fatalf("filters=%+v want one park and two physical sheds", page.Filters)
	}
	task := tasks[0]
	filtered, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant,
		Day:      day,
		ActorID:  transportOperator,
		ParkID:   task.ParkID,
		ShedID:   task.ShedID,
		Status:   domain.TransportStatusDue,
		Limit:    20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Items) != 1 || filtered.Items[0].TaskID != task.TaskID {
		t.Fatalf("filtered tasks=%+v want task %s", filtered.Items, task.TaskID)
	}
	first, err := repo.SubmitTransportAttempt(ctx, ports.SubmitTransportParams{TenantID: fdTenant, TaskID: task.TaskID, ProofRef: "proof-live-1", OperatorID: transportOperator, IdempotencyKey: "transport-submit-0001", ActorID: transportOperator, ActorType: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != domain.TransportStatusVerificationDue || first.AttemptNo != 1 {
		t.Fatalf("first=%+v", first)
	}
	inReview, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant,
		Day:      day,
		ActorID:  transportOperator,
		Status:   domain.TransportStatusVerificationDue,
		Limit:    20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inReview.Items) != 1 || inReview.Items[0].TaskID != task.TaskID {
		t.Fatalf("verification_due tasks=%+v want task %s", inReview.Items, task.TaskID)
	}
	_, err = repo.SubmitTransportAttempt(ctx, ports.SubmitTransportParams{TenantID: fdTenant, TaskID: task.TaskID, ProofRef: "proof-other", OperatorID: otherOperator, IdempotencyKey: "transport-submit-other", ActorID: otherOperator})
	if !errors.Is(err, ports.ErrTransportAssignedToAnotherOperator) && !errors.Is(err, ports.ErrTransportTaskNotActionable) {
		t.Fatalf("other operator err=%v", err)
	}
	applied, err := repo.BounceTransportForRework(ctx, ports.BounceTransportParams{TenantID: fdTenant, AttemptID: first.AttemptID, Reason: "video unclear", TraceID: "verdict-1"})
	if err != nil || !applied {
		t.Fatalf("reject applied=%v err=%v", applied, err)
	}
	second, err := repo.SubmitTransportAttempt(ctx, ports.SubmitTransportParams{TenantID: fdTenant, TaskID: task.TaskID, ProofRef: "proof-live-2", OperatorID: transportOperator, IdempotencyKey: "transport-submit-0002", ActorID: transportOperator, ActorType: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if second.AttemptNo != 2 || second.AttemptID == first.AttemptID {
		t.Fatalf("second=%+v first=%+v", second, first)
	}
	applied, err = repo.ApplyVerifiedTransport(ctx, ports.ApplyTransportParams{TenantID: fdTenant, AttemptID: second.AttemptID, VerifiedBy: transportVerifier, TraceID: "verdict-2"})
	if err != nil || !applied {
		t.Fatalf("approve applied=%v err=%v", applied, err)
	}
	var taskStatus string
	var attempts, rejected, approved int
	if err = pool.QueryRow(ctx, `SELECT status FROM feed_transport_tasks WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, fdTenant, task.TaskID).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='rejected' AND proof_ref='proof-live-1' AND rejection_reason='video unclear'),count(*) FILTER(WHERE status='approved' AND proof_ref='proof-live-2') FROM feed_transport_attempts WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, fdTenant, task.TaskID).Scan(&attempts, &rejected, &approved); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "completed" || attempts != 2 || rejected != 1 || approved != 1 {
		t.Fatalf("status=%s attempts=%d rejected=%d approved=%d", taskStatus, attempts, rejected, approved)
	}
}

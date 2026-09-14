package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// The pen visit is the care task's LAST step (maintainer decision 2026-09-12): a pen task
// closes -- work_state 'completed' -- only when BOTH its own videos and its next-day pen visit
// are verified, in either order. These are the production-path proofs on the real schema:
//
//   - work verified FIRST: ApplyVerifiedTask leaves the clock open; PenVisitVerified closes it
//   - visit verified FIRST: PenVisitVerified touches nothing (the work is still with the
//     verifier); ApplyVerifiedTask sees the verified visit and closes in one step
//   - a shed-less task (vaccine inventory) owes no visit and closes on its own approval
//   - the kernel roll-forward does not roll a task whose own videos are verified
//   - the park's configured visitor finds the task on their worklist on the VISIT's day
//
// The visit row is written the way the pen-visit materializer writes it (a pen_visit_tasks
// row linked through pen_visit_task_sources), because that link is the contract the two
// modules share.

const (
	pcVisitor  = "9c000000-0000-4000-8000-000000007001"
	pcVisitor2 = "9c000000-0000-4000-8000-000000007002"
	pcVisitPf  = "9c000000-0000-4000-8000-000000007101"
)

// linkVisit writes the visit the materializer would raise for task on the day after
// sourceDate, in the given gate status, and returns its id. One visit per pen per source
// day (pen_visit_tasks_natural_uq), so each scenario below uses its own source day.
func linkVisit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, sourceDate, status string) string {
	t.Helper()
	var visitID string
	proof := "NULL"
	submitted := "NULL, NULL"
	if status != "open" {
		proof = "'" + pcVisitPf + "'::uuid"
		submitted = "'" + pcVisitor + "'::uuid, now()"
	}
	verified := "NULL, NULL"
	if status == "completed" {
		verified = "'" + pcVerifier + "'::uuid, now()"
	}
	workState := "scheduled"
	if status == "completed" {
		workState = "completed"
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO pen_visit_tasks (tenant_id, park_id, shed_id, partition_label, reasons, source_business_date, planned_business_date, due_business_date,
                             work_state, status, proof_ref, submitted_by, submitted_at, verified_by, verified_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, NULL, ARRAY['deworming'], $4::date, $4::date + 1, $4::date + 1, $5, $6, `+proof+`, `+submitted+`, `+verified+`)
RETURNING task_id::text`, pcTenant, pcPark, pcShedA, sourceDate, workState, status).Scan(&visitID); err != nil {
		t.Fatalf("seed visit: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pen_visit_task_sources (tenant_id, task_id, source_kind, source_ref_id) VALUES ($1::uuid, $2::uuid, 'pc_care_task', $3::uuid)`, pcTenant, visitID, taskID); err != nil {
		t.Fatalf("link visit: %v", err)
	}
	return visitID
}

func seedVisitors(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, display_code, display_name, status, user_id)
VALUES ($1::uuid, 'PC-VIS-1', 'Dinakar', 'active', $2::uuid), ($1::uuid, 'PC-VIS-2', 'Chandrakant', 'active', $3::uuid)
ON CONFLICT DO NOTHING`, pcTenant, pcVisitor, pcVisitor2); err != nil {
		t.Fatalf("seed visitors: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO pen_visit_park_assignees (tenant_id, park_id, user_id) VALUES ($1::uuid, $2::uuid, $3::uuid), ($1::uuid, $2::uuid, $4::uuid)
ON CONFLICT DO NOTHING`, pcTenant, pcPark, pcVisitor, pcVisitor2); err != nil {
		t.Fatalf("seed park visitors: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, size_bytes, upload_state, scope_type, scope_id, subject_type, proof_type, uploaded_by, metadata)
VALUES ($1::uuid, $2::uuid, 'local', 'visits/' || $1::text, 'video/mp4', 4096, 'completed', 'task', $1::uuid, 'other', 'video', $3::uuid, '{"capture_source":"in_app_camera"}'::jsonb)
ON CONFLICT (proof_id) DO NOTHING`, pcVisitPf, pcTenant, pcVisitor); err != nil {
		t.Fatalf("seed visit proof: %v", err)
	}
}

// submitPenWork plans one single-video pen task of the category (the natural key allows one
// per category, pen and day), scans one animal, films it and submits.
func submitPenWork(t *testing.T, ctx context.Context, repo *Repository, category, key string) ports.TaskRow {
	t.Helper()
	task := createPCTask(t, ctx, repo, category, key+"-create")
	scan, err := repo.ScanAnimal(ctx, ports.ScanAnimalParams{
		TenantID: pcTenant, TaskID: task.TaskID, ScannedIdentifier: "RFID-" + key, ScannedBy: pcOperator1,
		IdempotencyKey: key + "-scan", ActorID: pcOperator1, ActorType: "operator",
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := registerSlot(t, ctx, repo, task.TaskID, scan.AnimalRowID, domain.SlotVideo, "proof-"+key, pcOperator1, key+"-slot"); err != nil {
		t.Fatalf("slot: %v", err)
	}
	if _, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator1, IdempotencyKey: key + "-submit", ActorType: "operator"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	return task
}

func taskStates(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID string) (status, workState string, terminal bool) {
	t.Helper()
	if err := pool.QueryRow(ctx, `SELECT status, work_state, terminal_at IS NOT NULL FROM pc_care_tasks WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, taskID).Scan(&status, &workState, &terminal); err != nil {
		t.Fatalf("read task: %v", err)
	}
	return status, workState, terminal
}

func TestPenTaskClosesOnlyWhenBothTheWorkAndTheVisitAreVerified(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	seedVisitors(t, ctx, pool)

	// Order 1: the work's clips are approved first. The gate completes, the clock stays
	// open, and the completed event still fires (the field work IS verified).
	workFirst := submitPenWork(t, ctx, repo, domain.CategoryDeworming, "pc-close-a")
	if applied, err := repo.ApplyVerifiedTask(ctx, ports.ApplyVerifiedTaskParams{TenantID: pcTenant, TaskID: workFirst.TaskID, VerifiedBy: pcVerifier, TraceID: "t-a1"}); err != nil || !applied {
		t.Fatalf("apply work first = %v err %v", applied, err)
	}
	if status, workState, terminal := taskStates(t, ctx, pool, workFirst.TaskID); status != domain.StatusCompleted || workState != domain.WorkStateScheduled || terminal {
		t.Fatalf("after work approval = %s/%s terminal=%v, want completed/scheduled/false", status, workState, terminal)
	}
	// The next morning the visit exists and is owed; nothing closes yet. A verdict on the
	// visit while it is still open (a foreign/early event) is a no-op on the task.
	visitA := linkVisit(t, ctx, pool, workFirst.TaskID, "2026-08-21", "open")
	if err := repo.PenVisitVerified(ctx, pcTenant, []string{workFirst.TaskID}, visitA, "t-early"); err != nil {
		t.Fatalf("early PenVisitVerified: %v", err)
	}
	if _, workState, _ := taskStates(t, ctx, pool, workFirst.TaskID); workState != domain.WorkStateScheduled {
		t.Fatalf("an unverified visit must not close the task, got %s", workState)
	}
	// The kernel roll-forward leaves a verified-but-awaiting-visit task alone: its clock is
	// the visit's now, and the visit carries its own.
	if res, err := repo.SweepTaskRollForward(ctx, pcTenant, pcBusinessDay(2026, 8, 25), 200, 50); err != nil || res.RolledForward != 0 {
		t.Fatalf("sweep must not roll a verified task: %+v err %v", res, err)
	}
	// The visit is verified: the task closes, once, with an audit row; a redelivery is a no-op.
	if _, err := pool.Exec(ctx, `UPDATE pen_visit_tasks SET status = 'completed', work_state = 'completed', proof_ref = $3::uuid, submitted_by = $4::uuid, submitted_at = now(), verified_by = $4::uuid, verified_at = now() WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, visitA, pcVisitPf, pcVisitor); err != nil {
		t.Fatalf("verify visit: %v", err)
	}
	if err := repo.PenVisitVerified(ctx, pcTenant, []string{workFirst.TaskID}, visitA, "t-a2"); err != nil {
		t.Fatalf("PenVisitVerified: %v", err)
	}
	if status, workState, terminal := taskStates(t, ctx, pool, workFirst.TaskID); status != domain.StatusCompleted || workState != domain.WorkStateCompleted || !terminal {
		t.Fatalf("after visit approval = %s/%s terminal=%v, want completed/completed/true", status, workState, terminal)
	}
	if err := repo.PenVisitVerified(ctx, pcTenant, []string{workFirst.TaskID}, visitA, "t-a3"); err != nil {
		t.Fatalf("redelivered PenVisitVerified: %v", err)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1::uuid AND resource_type = 'pc_care_task' AND resource_id = $2 AND action = 'pc_care.task.pen_visit_verified'`, pcTenant, workFirst.TaskID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("pen visit close audit rows = %d err %v, want exactly 1 (idempotent redelivery)", audits, err)
	}

	// Order 2: the visit is verified BEFORE the work's clips (the verifier reviewed the
	// morning-after video first). PenVisitVerified finds no completed task to close; the
	// work's own approval then sees the verified visit and closes the clock itself.
	visitFirst := submitPenWork(t, ctx, repo, domain.CategoryTicksRemoval, "pc-close-b")
	visitB := linkVisit(t, ctx, pool, visitFirst.TaskID, "2026-08-19", "completed")
	if err := repo.PenVisitVerified(ctx, pcTenant, []string{visitFirst.TaskID}, visitB, "t-b1"); err != nil {
		t.Fatalf("PenVisitVerified before the work: %v", err)
	}
	if status, workState, _ := taskStates(t, ctx, pool, visitFirst.TaskID); status != domain.StatusPendingVerification || workState != domain.WorkStateScheduled {
		t.Fatalf("a verified visit must not close work still with the verifier: %s/%s", status, workState)
	}
	if applied, err := repo.ApplyVerifiedTask(ctx, ports.ApplyVerifiedTaskParams{TenantID: pcTenant, TaskID: visitFirst.TaskID, VerifiedBy: pcVerifier, TraceID: "t-b2"}); err != nil || !applied {
		t.Fatalf("apply after visit = %v err %v", applied, err)
	}
	if status, workState, terminal := taskStates(t, ctx, pool, visitFirst.TaskID); status != domain.StatusCompleted || workState != domain.WorkStateCompleted || !terminal {
		t.Fatalf("work approval with a verified visit = %s/%s terminal=%v, want completed/completed/true", status, workState, terminal)
	}

	// A visit that was SENT BACK does not close anything; the task waits for the re-shoot.
	sentBack := submitPenWork(t, ctx, repo, domain.CategoryAntiProtozoan, "pc-close-c")
	visitC := linkVisit(t, ctx, pool, sentBack.TaskID, "2026-08-18", "rework")
	if applied, err := repo.ApplyVerifiedTask(ctx, ports.ApplyVerifiedTaskParams{TenantID: pcTenant, TaskID: sentBack.TaskID, VerifiedBy: pcVerifier, TraceID: "t-c1"}); err != nil || !applied {
		t.Fatalf("apply with a sent-back visit = %v err %v", applied, err)
	}
	if err := repo.PenVisitVerified(ctx, pcTenant, []string{sentBack.TaskID}, visitC, "t-c2"); err != nil {
		t.Fatalf("PenVisitVerified on a sent-back visit: %v", err)
	}
	if status, workState, _ := taskStates(t, ctx, pool, sentBack.TaskID); status != domain.StatusCompleted || workState != domain.WorkStateScheduled {
		t.Fatalf("a sent-back visit must leave the task open: %s/%s", status, workState)
	}

	// The visit is a task of its own on the Tasks module (2026-09-14): the park's configured
	// visitor does NOT find the parent task on a PC Care worklist, and the operator's carry
	// list no longer resurfaces a verified task as work owed.
	cutoff := pcCutoff
	visitorList, err := repo.ListTasks(ctx, ports.ListTasksQuery{
		TenantID: pcTenant, TenantWide: true, DueBusinessDate: "2026-08-22",
		CurrentOrCarry: true, AssigneeUserID: pcVisitor2,
		Now: pcBusinessDay(2026, 8, 22), RemovalCutoff: cutoff, Limit: 50,
	})
	if err != nil || len(visitorList.Items) != 0 {
		t.Fatalf("a visitor who is not an assignee sees no PC Care work (the visit lives on Tasks), got %d err %v", len(visitorList.Items), err)
	}
	operatorCarry, err := repo.ListTasks(ctx, ports.ListTasksQuery{
		TenantID: pcTenant, TenantWide: true, DueBusinessDate: "2026-08-22",
		CurrentOrCarry: true, AssigneeUserID: pcOperator1,
		Now: pcBusinessDay(2026, 8, 22), RemovalCutoff: cutoff, Limit: 50,
	})
	if err != nil {
		t.Fatalf("operator carry: %v", err)
	}
	for _, row := range operatorCarry.Items {
		if row.Status == domain.StatusCompleted {
			t.Fatalf("a task whose own videos are verified must not carry forward as the operator's work: %+v", row)
		}
	}
}

// A task with no pen (vaccine inventory, park-grain) owes no visit and closes on its own
// approval exactly as before.
func TestShedLessTaskClosesOnItsOwnApproval(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	var taskID string
	if err := pool.QueryRow(ctx, `
INSERT INTO pc_care_tasks (tenant_id, category, park_id, shed_id, planned_business_date, due_business_date, work_state, status, idempotency_key, created_by, vaccine_label, submitted_by, submitted_at)
VALUES ($1::uuid, 'inventory_vaccine', $2::uuid, NULL, '2026-08-21', '2026-08-21', 'scheduled', 'pending_verification', 'pc-shedless', $3::uuid, 'FMD', $3::uuid, now())
RETURNING task_id::text`, pcTenant, pcPark, pcVerifier).Scan(&taskID); err != nil {
		t.Fatalf("seed shed-less task: %v", err)
	}
	if applied, err := repo.ApplyVerifiedTask(ctx, ports.ApplyVerifiedTaskParams{TenantID: pcTenant, TaskID: taskID, VerifiedBy: pcVerifier, TraceID: "t-s1"}); err != nil || !applied {
		t.Fatalf("apply = %v err %v", applied, err)
	}
	if status, workState, terminal := taskStates(t, ctx, pool, taskID); status != domain.StatusCompleted || workState != domain.WorkStateCompleted || !terminal {
		t.Fatalf("shed-less approval = %s/%s terminal=%v, want completed/completed/true", status, workState, terminal)
	}
}

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

func TestSubmitTaskAllowsSecondShedSubmitWhenSharedParkTaskAlreadyNeedsReview(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID   = "00000000-0000-4000-8000-000000000001"
		actorID    = "77000000-0000-4000-8000-000000000001"
		sopID      = "77000000-0000-4000-8000-000000000002"
		sopVersion = "77000000-0000-4000-8000-000000000003"
		taskID     = "77000000-0000-4000-8000-000000000004"
		parkID     = "77000000-0000-4000-8000-000000000005"
		shedOne    = "77000000-0000-4000-8000-000000000006"
		shedTwo    = "77000000-0000-4000-8000-000000000007"
	)

	execShedSubmitState(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'vaccination.shared_shed_submit_regression', 'Shared shed submit regression', 'active')`,
		sopID, tenantID)
	execShedSubmitState(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		sopVersion, tenantID, sopID)
	execShedSubmitState(t, ctx, pool, "shared task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, row_version)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Shared park task', 'needs_review', 'park', $5::uuid, 2)`,
		taskID, tenantID, sopID, sopVersion, parkID)
	execShedSubmitState(t, ctx, pool, "first shed submission",
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, '{}'::jsonb,
		   jsonb_build_array(jsonb_build_object('proof_id', 'old-yashoda-proof', 'proof_type', 'video', 'subject_type', 'shed', 'subject_id', $6::text)),
		   'needs_review')`,
		tenantID, taskID, sopVersion, actorID, "shed-submit:"+taskID+":scope:"+shedOne+":rv:1", shedOne)

	repo := NewRepository(pool, 5*time.Second)
	shedTwoSubjectID := shedTwo
	_, _, _, err := repo.SubmitTask(ctx, ports.SubmitTaskCommand{
		TenantID: tenantID,
		ActorID:  actorID,
		TaskID:   taskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   sopVersion,
			IdempotencyKey: "shed-submit:" + taskID + ":scope:" + shedTwo + ":rv:2",
			Answers:        map[string]any{},
			ProofRefs: []domain.ProofReference{{
				ProofID:     "godel-proof",
				ProofType:   "video",
				SubjectType: "shed",
				SubjectID:   &shedTwoSubjectID,
				UploadState: "completed",
			}},
		},
		TaskState: "needs_review",
		ItemState: "needs_review",
	})
	if err != nil {
		t.Fatalf("second shed SubmitTask while task already needs_review: %v", err)
	}

	var submissions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_submissions WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenantID, taskID).Scan(&submissions); err != nil {
		t.Fatal(err)
	}
	if submissions != 2 {
		t.Fatalf("submissions=%d want 2", submissions)
	}
}

func TestSubmitTaskRejectsFreshSubmitWhenSharedParkTaskAccepted(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID   = "00000000-0000-4000-8000-000000000001"
		actorID    = "77300000-0000-4000-8000-000000000001"
		sopID      = "77300000-0000-4000-8000-000000000002"
		sopVersion = "77300000-0000-4000-8000-000000000003"
		taskID     = "77300000-0000-4000-8000-000000000004"
		parkID     = "77300000-0000-4000-8000-000000000005"
		shedOne    = "77300000-0000-4000-8000-000000000006"
		shedTwo    = "77300000-0000-4000-8000-000000000007"
	)

	execShedSubmitState(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'vaccination.accepted_terminal_regression', 'Accepted terminal regression', 'active')`,
		sopID, tenantID)
	execShedSubmitState(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		sopVersion, tenantID, sopID)
	execShedSubmitState(t, ctx, pool, "accepted shared task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, row_version)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Accepted park task', 'accepted', 'park', $5::uuid, 3)`,
		taskID, tenantID, sopID, sopVersion, parkID)
	execShedSubmitState(t, ctx, pool, "existing accepted shed submission",
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, '{}'::jsonb,
		   jsonb_build_array(jsonb_build_object('proof_id', 'accepted-proof', 'proof_type', 'video', 'subject_type', 'shed', 'subject_id', $6::text)),
		   'accepted')`,
		tenantID, taskID, sopVersion, actorID, "shed-submit:"+taskID+":scope:"+shedOne+":rv:1", shedOne)

	repo := NewRepository(pool, 5*time.Second)
	shedTwoSubjectID := shedTwo
	_, _, _, err := repo.SubmitTask(ctx, ports.SubmitTaskCommand{
		TenantID: tenantID,
		ActorID:  actorID,
		TaskID:   taskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   sopVersion,
			IdempotencyKey: "shed-submit:" + taskID + ":scope:" + shedTwo + ":rv:2",
			Answers:        map[string]any{},
			ProofRefs: []domain.ProofReference{{
				ProofID:     "late-proof",
				ProofType:   "video",
				SubjectType: "shed",
				SubjectID:   &shedTwoSubjectID,
				UploadState: "completed",
			}},
		},
		TaskState: "needs_review",
		ItemState: "needs_review",
	})
	if err != ports.ErrConflict {
		t.Fatalf("fresh submit on accepted task error=%v want ErrConflict", err)
	}

	var submissions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_submissions WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenantID, taskID).Scan(&submissions); err != nil {
		t.Fatal(err)
	}
	if submissions != 1 {
		t.Fatalf("submissions=%d want original accepted row only", submissions)
	}
}

func execShedSubmitState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

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

func TestSubmitTaskShedProofFiltersOverBroadScanItemsToThatShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID   = "00000000-0000-4000-8000-000000000001"
		actorID    = "77200000-0000-4000-8000-000000000001"
		partyID    = "77200000-0000-4000-8000-000000000002"
		sopID      = "77200000-0000-4000-8000-000000000003"
		sopVersion = "77200000-0000-4000-8000-000000000004"
		taskID     = "77200000-0000-4000-8000-000000000005"
		parkID     = "77200000-0000-4000-8000-000000000006"
		shedOne    = "77200000-0000-4000-8000-000000000007"
		shedTwo    = "77200000-0000-4000-8000-000000000008"
		goatOne    = "77200000-0000-4000-8000-000000000009"
		goatTwo    = "77200000-0000-4000-8000-000000000010"
	)

	execShedSubmitState(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'park', 'PARK-SHED-FILTER', 'Shed Filter Park', 'active')`,
		parkID, tenantID)
	execShedSubmitState(t, ctx, pool, "shed one",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1::uuid, $2::uuid, 'shed', 'SHED-FILTER-1', 'Shed Filter One', $3::uuid, 'active')`,
		shedOne, tenantID, parkID)
	execShedSubmitState(t, ctx, pool, "shed two",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1::uuid, $2::uuid, 'shed', 'SHED-FILTER-2', 'Shed Filter Two', $3::uuid, 'active')`,
		shedTwo, tenantID, parkID)
	execShedSubmitState(t, ctx, pool, "party",
		`INSERT INTO parties (party_id, party_type, display_name, status)
		 VALUES ($1::uuid, 'org', 'Shed Filter Custodian', 'active')`,
		partyID)
	execShedSubmitState(t, ctx, pool, "goat one",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, shed_id, management_stage, health_status)
		 VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', $4::uuid, $5::uuid, $4::uuid, 'K1', 'healthy')`,
		goatOne, tenantID, partyID, shedOne, parkID)
	execShedSubmitState(t, ctx, pool, "goat two",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, shed_id, management_stage, health_status)
		 VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', $4::uuid, $5::uuid, $4::uuid, 'K1', 'healthy')`,
		goatTwo, tenantID, partyID, shedTwo, parkID)
	execShedSubmitState(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'vaccination.shed_filter_regression', 'Shed filter regression', 'active')`,
		sopID, tenantID)
	execShedSubmitState(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":true,"subject_scope":"shed","types":["video"],"minimum_count":1}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		sopVersion, tenantID, sopID)
	execShedSubmitState(t, ctx, pool, "shared task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, row_version)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Shared park task', 'in_progress', 'park', $5::uuid, 1)`,
		taskID, tenantID, sopID, sopVersion, parkID)

	repo := NewRepository(pool, 5*time.Second)
	shedOneSubjectID := shedOne
	_, _, _, err := repo.SubmitTask(ctx, ports.SubmitTaskCommand{
		TenantID: tenantID,
		ActorID:  actorID,
		TaskID:   taskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   sopVersion,
			IdempotencyKey: "shed-submit:" + taskID + ":scope:" + shedOne + ":rv:1",
			Answers:        map[string]any{},
			ProofRefs: []domain.ProofReference{{
				ProofID:     "shed-one-proof",
				ProofType:   "video",
				SubjectType: "shed",
				SubjectID:   &shedOneSubjectID,
				UploadState: "completed",
			}},
		},
		TaskState: "needs_review",
		ItemState: "needs_review",
		SubmissionItems: []ports.SubmissionItemInput{{
			GoatID:  goatOne,
			ItemKey: goatOne,
		}, {
			GoatID:  goatTwo,
			ItemKey: goatTwo,
		}},
	})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}

	var shedOneItems, shedTwoItems int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_submission_items WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND goat_id=$3::uuid`, tenantID, taskID, goatOne).Scan(&shedOneItems); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_submission_items WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND goat_id=$3::uuid`, tenantID, taskID, goatTwo).Scan(&shedTwoItems); err != nil {
		t.Fatal(err)
	}
	if shedOneItems != 1 || shedTwoItems != 0 {
		t.Fatalf("shed-scoped proof wrote wrong items: shed one=%d shed two=%d", shedOneItems, shedTwoItems)
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

func TestCompletedTaskProofRefsDoesNotRecoverParkScopedShedProofWithoutShedID(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID   = "00000000-0000-4000-8000-000000000001"
		sopID      = "77400000-0000-4000-8000-000000000001"
		sopVersion = "77400000-0000-4000-8000-000000000002"
		taskID     = "77400000-0000-4000-8000-000000000003"
		parkID     = "77400000-0000-4000-8000-000000000004"
		shedOne    = "77400000-0000-4000-8000-000000000005"
		shedTwo    = "77400000-0000-4000-8000-000000000006"
		proofOne   = "77400000-0000-4000-8000-000000000007"
		proofTwo   = "77400000-0000-4000-8000-000000000008"
	)

	execShedSubmitState(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'park', 'PARK-PROOF-RECOVERY', 'Proof Recovery Park', 'active')`,
		parkID, tenantID)
	execShedSubmitState(t, ctx, pool, "shed one",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1::uuid, $2::uuid, 'shed', 'PROOF-RECOVERY-1', 'Proof Recovery One', $3::uuid, 'active')`,
		shedOne, tenantID, parkID)
	execShedSubmitState(t, ctx, pool, "shed two",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1::uuid, $2::uuid, 'shed', 'PROOF-RECOVERY-2', 'Proof Recovery Two', $3::uuid, 'active')`,
		shedTwo, tenantID, parkID)
	execShedSubmitState(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'vaccination.blank_shed_recovery_regression', 'Blank shed recovery regression', 'active')`,
		sopID, tenantID)
	execShedSubmitState(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":true,"subject_scope":"shed","types":["video"],"minimum_count":1}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		sopVersion, tenantID, sopID)
	execShedSubmitState(t, ctx, pool, "park scoped task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, row_version)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Park scoped proof recovery', 'in_progress', 'park', $5::uuid, 1)`,
		taskID, tenantID, sopID, sopVersion, parkID)
	execShedSubmitState(t, ctx, pool, "shed one proof",
		`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type)
		 VALUES ($1::uuid, $2::uuid, 'gcs', 'proof-recovery/shed-one.mp4', 'video/mp4', 'completed', 'shed', $3::uuid, 'shed', $3::uuid, 'video')`,
		proofOne, tenantID, shedOne)
	execShedSubmitState(t, ctx, pool, "shed two proof",
		`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type)
		 VALUES ($1::uuid, $2::uuid, 'gcs', 'proof-recovery/shed-two.mp4', 'video/mp4', 'completed', 'shed', $3::uuid, 'shed', $3::uuid, 'video')`,
		proofTwo, tenantID, shedTwo)

	repo := NewRepository(pool, 5*time.Second)
	refs, err := repo.CompletedTaskProofRefs(ctx, tenantID, taskID, "shed", "")
	if err != nil {
		t.Fatalf("CompletedTaskProofRefs() error = %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("CompletedTaskProofRefs blank shed id returned %d refs, want 0: %#v", len(refs), refs)
	}
}

func execShedSubmitState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

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

func TestSubmitTaskShedScopedKeyFiltersPerGoatProofItemsToThatShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID   = "00000000-0000-4000-8000-000000000001"
		actorID    = "77500000-0000-4000-8000-000000000001"
		partyID    = "77500000-0000-4000-8000-000000000002"
		sopID      = "77500000-0000-4000-8000-000000000003"
		sopVersion = "77500000-0000-4000-8000-000000000004"
		taskID     = "77500000-0000-4000-8000-000000000005"
		parkID     = "77500000-0000-4000-8000-000000000006"
		shedOne    = "77500000-0000-4000-8000-000000000007"
		shedTwo    = "77500000-0000-4000-8000-000000000008"
		goatOne    = "77500000-0000-4000-8000-000000000009"
		goatTwo    = "77500000-0000-4000-8000-000000000010"
	)

	execShedSubmitState(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'park', 'PARK-SHED-PER-GOAT-FILTER', 'Per Goat Filter Park', 'active')`,
		parkID, tenantID)
	execShedSubmitState(t, ctx, pool, "shed one",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1::uuid, $2::uuid, 'shed', 'SHED-PER-GOAT-FILTER-1', 'Per Goat Filter One', $3::uuid, 'active')`,
		shedOne, tenantID, parkID)
	execShedSubmitState(t, ctx, pool, "shed two",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1::uuid, $2::uuid, 'shed', 'SHED-PER-GOAT-FILTER-2', 'Per Goat Filter Two', $3::uuid, 'active')`,
		shedTwo, tenantID, parkID)
	execShedSubmitState(t, ctx, pool, "party",
		`INSERT INTO parties (party_id, party_type, display_name, status)
		 VALUES ($1::uuid, 'org', 'Per Goat Filter Custodian', 'active')`,
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
		 VALUES ($1::uuid, $2::uuid, 'vaccination.per_goat_shed_key_filter_regression', 'Per-goat shed key filter regression', 'active')`,
		sopID, tenantID)
	execShedSubmitState(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":true,"subject_scope":"goat","types":["video"],"minimum_count":1}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		sopVersion, tenantID, sopID)
	execShedSubmitState(t, ctx, pool, "shared task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, row_version)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Shared park task', 'in_progress', 'park', $5::uuid, 1)`,
		taskID, tenantID, sopID, sopVersion, parkID)

	goatOneSubjectID := goatOne
	repo := NewRepository(pool, 5*time.Second)
	_, _, _, err := repo.SubmitTask(ctx, ports.SubmitTaskCommand{
		TenantID: tenantID,
		ActorID:  actorID,
		TaskID:   taskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   sopVersion,
			IdempotencyKey: "shed-submit:" + taskID + ":scope:" + shedOne + ":rv:1",
			Answers:        map[string]any{},
			ProofRefs: []domain.ProofReference{{
				ProofID:     "goat-one-proof",
				ProofType:   "video",
				SubjectType: "goat",
				SubjectID:   &goatOneSubjectID,
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
		t.Fatalf("shed-scoped per-goat proof wrote wrong items: shed one=%d shed two=%d", shedOneItems, shedTwoItems)
	}
}

func TestShedScopeFromSubmissionKeyIgnoresMalformedScope(t *testing.T) {
	if got := shedScopeFromSubmissionKey("shed-submit:task:scope:not-a-uuid:rv:1"); got != "" {
		t.Fatalf("malformed shed scope = %q, want empty", got)
	}
	if got := shedScopeFromSubmissionKey("shed-submit:task:scope:77500000-0000-4000-8000-000000000007:rv:1"); got != "77500000-0000-4000-8000-000000000007" {
		t.Fatalf("valid shed scope = %q", got)
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
	refs, err := repo.CompletedTaskProofRefs(ctx, tenantID, taskID, "shed", "", "")
	if err != nil {
		t.Fatalf("CompletedTaskProofRefs() error = %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("CompletedTaskProofRefs blank shed id returned %d refs, want 0: %#v", len(refs), refs)
	}
}

// TestReopenTaskForReworkUnblocksResubmitAfterVerifierRejection is the regression test for the
// P0 "rework cannot be resubmitted" incident: a task accepted terminally could never take another
// submission (TestSubmitTaskRejectsFreshSubmitWhenSharedParkTaskAccepted above proves that guard is
// intentional), but nothing ever moved a task OUT of 'accepted' when a verifier rejected an animal
// and its obligation was reopened. ReopenTaskForRework is that missing transition. This proves the
// full cycle: accepted -> (verifier rejection) reopen -> resubmit succeeds.
func TestReopenTaskForReworkUnblocksResubmitAfterVerifierRejection(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID   = "00000000-0000-4000-8000-000000000001"
		actorID    = "77400000-0000-4000-8000-000000000001"
		verifierID = "77400000-0000-4000-8000-000000000009"
		sopID      = "77400000-0000-4000-8000-000000000002"
		sopVersion = "77400000-0000-4000-8000-000000000003"
		taskID     = "77400000-0000-4000-8000-000000000004"
		shedID     = "77400000-0000-4000-8000-000000000005"
		goatID     = "77400000-0000-4000-8000-000000000006"
	)

	execShedSubmitState(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'vaccination.rework_reopen_regression', 'Rework reopen regression', 'active')`,
		sopID, tenantID)
	execShedSubmitState(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		sopVersion, tenantID, sopID)
	execShedSubmitState(t, ctx, pool, "accepted task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, row_version)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Rework reopen task', 'accepted', 'shed', $5::uuid, 11)`,
		taskID, tenantID, sopID, sopVersion, shedID)
	execShedSubmitState(t, ctx, pool, "prior accepted submission",
		`INSERT INTO sop_submissions (tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, '{}'::jsonb, '[]'::jsonb, 'accepted')`,
		tenantID, taskID, sopVersion, actorID, "shed-submit:"+taskID+":initial")

	repo := NewRepository(pool, 5*time.Second)

	// A fresh submission still 409s while the task remains terminally 'accepted' -- this is the
	// exact behavior the incident reported, and it must stay intact for a task that is genuinely
	// done.
	_, _, _, err := repo.SubmitTask(ctx, ports.SubmitTaskCommand{
		TenantID: tenantID,
		ActorID:  actorID,
		TaskID:   taskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   sopVersion,
			IdempotencyKey: "shed-submit:" + taskID + ":before-reopen",
			Answers:        map[string]any{},
			ProofRefs:      []domain.ProofReference{},
		},
		TaskState: "needs_review",
		ItemState: "needs_review",
	})
	if err != ports.ErrConflict {
		t.Fatalf("submit before reopen: error=%v want ErrConflict", err)
	}

	// Verifier rejects a goat's proof: the owning vertical (vaccination) reopens the goat's
	// obligation on its own side; ReopenTaskForRework is the SOP-side compensation that must run
	// alongside it.
	priorSubmissionID := ""
	if err := pool.QueryRow(ctx, `SELECT submission_id::text FROM sop_submissions WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenantID, taskID).Scan(&priorSubmissionID); err != nil {
		t.Fatalf("resolve prior submission id: %v", err)
	}
	if err := repo.ReopenTaskForRework(ctx, tenantID, priorSubmissionID, goatID, verifierID); err != nil {
		t.Fatalf("ReopenTaskForRework() error = %v", err)
	}

	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM sop_tasks WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenantID, taskID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "rework_requested" {
		t.Fatalf("task state after reopen = %q, want rework_requested", state)
	}

	// The rework submission that was dead on arrival before now succeeds.
	submission, task, replay, err := repo.SubmitTask(ctx, ports.SubmitTaskCommand{
		TenantID: tenantID,
		ActorID:  actorID,
		TaskID:   taskID,
		Body: domain.SubmitTaskRequest{
			SOPVersionID:   sopVersion,
			IdempotencyKey: "shed-submit:" + taskID + ":after-reopen",
			Answers:        map[string]any{},
			ProofRefs:      []domain.ProofReference{},
		},
		TaskState: "needs_review",
		ItemState: "needs_review",
	})
	if err != nil {
		t.Fatalf("submit after reopen: unexpected error = %v", err)
	}
	if replay {
		t.Fatalf("submit after reopen: unexpectedly classified as replay")
	}
	if submission.State != "needs_review" {
		t.Fatalf("submission.State = %q, want needs_review", submission.State)
	}
	if task.State != "needs_review" {
		t.Fatalf("task.State = %q, want needs_review", task.State)
	}

	// A second reopen call (idempotent replay of the reject event, or a sibling goat's rejection
	// landing after the task already moved on) is a safe no-op, not an error.
	if err := repo.ReopenTaskForRework(ctx, tenantID, priorSubmissionID, goatID, verifierID); err != nil {
		t.Fatalf("ReopenTaskForRework() second call error = %v", err)
	}
}

func execShedSubmitState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

// TestSharedParentTaskIsNotAcceptedWhileASiblingSubmissionHasOpenItems is the regression for the
// deadlock found in phone QA on 2026-08-08.
//
// One CPT task covered two sheds through two submissions. Mandela's submission had all 3 items
// approved; Castro's had one approved and one REJECTED. AcceptSubmissionItemVerification counts
// remaining items inside the CURRENT submission only, so Mandela closing flipped the SHARED task
// to 'accepted' while Castro still had open items. SubmitTask then refuses any submission on an
// accepted task, so the rework the verifier's rejection had just created could never be submitted:
// the operator's screen showed the shed done, with no way to redo the rejected animal.
//
// Asserts the whole sequence, because each half passed on its own before:
//   - closing one submission must NOT accept a task whose sibling submission is unresolved
//   - the rejection must move the item AND its submission to 'rejected' (schema forbids
//     'rework_requested' on both; only sop_tasks may use it)
//   - the task must end at 'rework_requested', never 'accepted'
//   - a resubmit must then be allowed rather than returning a write conflict
func TestSharedParentTaskIsNotAcceptedWhileASiblingSubmissionHasOpenItems(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID   = "00000000-0000-4000-8000-000000000001"
		actorID    = "77500000-0000-4000-8000-000000000001"
		verifierID = "77500000-0000-4000-8000-000000000009"
		sopID      = "77500000-0000-4000-8000-000000000002"
		sopVersion = "77500000-0000-4000-8000-000000000003"
		taskID     = "77500000-0000-4000-8000-000000000004"
		shedID     = "77500000-0000-4000-8000-000000000005"
		subA       = "77500000-0000-4000-8000-00000000000a" // Castro: 1 approved + 1 rejected
		subB       = "77500000-0000-4000-8000-00000000000b" // Mandela: all approved
		goatA1     = "77500000-0000-4000-8000-000000000011"
		goatA2     = "77500000-0000-4000-8000-000000000012"
		goatB1     = "77500000-0000-4000-8000-000000000021"
	)

	execShedSubmitState(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'vaccination.shared_parent_regression', 'Shared parent regression', 'active')`,
		sopID, tenantID)
	execShedSubmitState(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		sopVersion, tenantID, sopID)
	execShedSubmitState(t, ctx, pool, "shared parent task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, row_version)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Shared CPT task', 'needs_review', 'shed', $5::uuid, 3)`,
		taskID, tenantID, sopID, sopVersion, shedID)

	// Castro submitted FIRST so Mandela is the "newer" submission -- the shape that defeated the
	// pre-existing newer-submission guard.
	execShedSubmitState(t, ctx, pool, "castro submission",
		`INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, submitted_at, state, answers, proof_refs, row_version, idempotency_key)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, now() - interval '10 minutes', 'needs_review', '{}'::jsonb, '[]'::jsonb, 1, 'shared-parent-regression:castro')`,
		subA, tenantID, taskID, sopVersion, actorID)
	execShedSubmitState(t, ctx, pool, "mandela submission",
		`INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, submitted_at, state, answers, proof_refs, row_version, idempotency_key)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, now(), 'needs_review', '{}'::jsonb, '[]'::jsonb, 1, 'shared-parent-regression:mandela')`,
		subB, tenantID, taskID, sopVersion, actorID)

	execShedSubmitState(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, location_type, name, status)
		 VALUES ('77500000-0000-4000-8000-0000000000f0'::uuid, $1::uuid, 'park', 'Shared Parent Park', 'active')`,
		tenantID)
	execShedSubmitState(t, ctx, pool, "shed",
		`INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
		 VALUES ($1::uuid, $2::uuid, 'shed', 'Shared Parent Shed', '77500000-0000-4000-8000-0000000000f0'::uuid, 'active')`,
		shedID, tenantID)

	var custodian string
	if err := pool.QueryRow(ctx, `INSERT INTO parties (party_id, party_type, display_name, status)
		VALUES (gen_random_uuid(), 'org', 'Shared parent regression custodian', 'active') RETURNING party_id::text`).Scan(&custodian); err != nil {
		t.Fatalf("seed custodian: %v", err)
	}
	for _, g := range []string{goatA1, goatA2, goatB1} {
		execShedSubmitState(t, ctx, pool, "goat",
			`INSERT INTO goats (goat_id, tenant_id, species, sex, lifecycle_status, custodian_party_id, shed_id)
			 VALUES ($1::uuid, $2::uuid, 'goat', 'female', 'alive', $3::uuid, $4::uuid)`,
			g, tenantID, custodian, shedID)
	}

	for _, it := range []struct{ sub, goat string }{{subA, goatA1}, {subA, goatA2}, {subB, goatB1}} {
		execShedSubmitState(t, ctx, pool, "submission item",
			`INSERT INTO sop_submission_items (item_id, tenant_id, task_id, submission_id, goat_id, item_key, state, result)
			 VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat:' || $4::text, 'needs_review', '{}'::jsonb)`,
			tenantID, taskID, it.sub, it.goat)
	}

	repo := NewRepository(pool, 10*time.Second)

	// Mandela closes completely.
	if err := repo.AcceptSubmissionItemVerification(ctx, tenantID, subB, goatB1, verifierID); err != nil {
		t.Fatalf("accept mandela item: %v", err)
	}

	var taskState string
	if err := pool.QueryRow(ctx, `SELECT state FROM sop_tasks WHERE task_id = $1::uuid`, taskID).Scan(&taskState); err != nil {
		t.Fatalf("read task state: %v", err)
	}
	if taskState == "accepted" {
		t.Fatalf("shared task was accepted while the sibling submission still had open items -- this is the deadlock: the sibling shed can no longer submit its rework")
	}

	// Castro: one approved, one rejected.
	if err := repo.AcceptSubmissionItemVerification(ctx, tenantID, subA, goatA1, verifierID); err != nil {
		t.Fatalf("accept castro item: %v", err)
	}
	if err := repo.ReopenTaskForRework(ctx, tenantID, subA, goatA2, verifierID); err != nil {
		t.Fatalf("reject castro item: %v", err)
	}

	var itemState, subState string
	if err := pool.QueryRow(ctx,
		`SELECT state FROM sop_submission_items WHERE submission_id = $1::uuid AND goat_id = $2::uuid`,
		subA, goatA2).Scan(&itemState); err != nil {
		t.Fatalf("read item state: %v", err)
	}
	if itemState != "rejected" {
		t.Fatalf("rejected item state = %q, want %q -- a rejected goat left at needs_review is invisible to every roll-up", itemState, "rejected")
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM sop_submissions WHERE submission_id = $1::uuid`, subA).Scan(&subState); err != nil {
		t.Fatalf("read submission state: %v", err)
	}
	if subState != "rejected" {
		t.Fatalf("submission state = %q, want %q", subState, "rejected")
	}

	if err := pool.QueryRow(ctx, `SELECT state FROM sop_tasks WHERE task_id = $1::uuid`, taskID).Scan(&taskState); err != nil {
		t.Fatalf("read task state after rejection: %v", err)
	}
	if taskState != "rework_requested" {
		t.Fatalf("task state = %q, want %q -- SubmitTask refuses an accepted task, so the rework could never be submitted", taskState, "rework_requested")
	}
}

// A re-submit after a rejection creates a NEW submission carrying the same goats. Approving the
// goat in the newest cycle must also settle its items in the EARLIER cycles, or those stranded
// 'needs_review' rows hold the parent task open forever and no close button ever appears -- even
// though every animal's LATEST verdict is approved.
//
// Real shape, 2026-08-08: G-006004 rejected 19:54, re-submitted and rejected again 20:53,
// re-submitted and APPROVED 20:57. Three submissions, task stuck at needs_review.
func TestApprovingTheLatestCycleSettlesTheSupersededOnes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID   = "00000000-0000-4000-8000-000000000001"
		actorID    = "77600000-0000-4000-8000-000000000001"
		verifierID = "77600000-0000-4000-8000-000000000009"
		sopID      = "77600000-0000-4000-8000-000000000002"
		sopVersion = "77600000-0000-4000-8000-000000000003"
		taskID     = "77600000-0000-4000-8000-000000000004"
		shedID     = "77600000-0000-4000-8000-000000000005"
		subOld     = "77600000-0000-4000-8000-00000000000a" // first attempt, rejected
		subNew     = "77600000-0000-4000-8000-00000000000b" // re-submit, approved
		goat       = "77600000-0000-4000-8000-000000000011"
	)

	execShedSubmitState(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'vaccination.supersede_regression', 'Supersede regression', 'active')`,
		sopID, tenantID)
	execShedSubmitState(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":false}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		sopVersion, tenantID, sopID)
	execShedSubmitState(t, ctx, pool, "task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, row_version)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Supersede task', 'needs_review', 'shed', $5::uuid, 3)`,
		taskID, tenantID, sopID, sopVersion, shedID)
	execShedSubmitState(t, ctx, pool, "old submission",
		`INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, submitted_at, state, answers, proof_refs, row_version, idempotency_key)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, now() - interval '60 minutes', 'needs_review', '{}'::jsonb, '[]'::jsonb, 1, 'supersede:old')`,
		subOld, tenantID, taskID, sopVersion, actorID)
	execShedSubmitState(t, ctx, pool, "new submission",
		`INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, submitted_at, state, answers, proof_refs, row_version, idempotency_key)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, now(), 'needs_review', '{}'::jsonb, '[]'::jsonb, 1, 'supersede:new')`,
		subNew, tenantID, taskID, sopVersion, actorID)
	execShedSubmitState(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, location_type, name, status)
		 VALUES ('77600000-0000-4000-8000-0000000000f0'::uuid, $1::uuid, 'park', 'Supersede Park', 'active')`,
		tenantID)
	execShedSubmitState(t, ctx, pool, "shed",
		`INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
		 VALUES ($1::uuid, $2::uuid, 'shed', 'Supersede Shed', '77600000-0000-4000-8000-0000000000f0'::uuid, 'active')`,
		shedID, tenantID)

	var custodian string
	if err := pool.QueryRow(ctx, `INSERT INTO parties (party_id, party_type, display_name, status)
		VALUES (gen_random_uuid(), 'org', 'Supersede custodian', 'active') RETURNING party_id::text`).Scan(&custodian); err != nil {
		t.Fatalf("seed custodian: %v", err)
	}
	execShedSubmitState(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, species, sex, lifecycle_status, custodian_party_id, shed_id)
		 VALUES ($1::uuid, $2::uuid, 'goat', 'female', 'alive', $3::uuid, $4::uuid)`,
		goat, tenantID, custodian, shedID)

	// The SAME goat appears in both cycles -- that is what a re-submit does.
	for _, sub := range []string{subOld, subNew} {
		execShedSubmitState(t, ctx, pool, "submission item",
			`INSERT INTO sop_submission_items (item_id, tenant_id, task_id, submission_id, goat_id, item_key, state, result)
			 VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat:' || $4::text, 'needs_review', '{}'::jsonb)`,
			tenantID, taskID, sub, goat)
	}

	repo := NewRepository(pool, 10*time.Second)
	if err := repo.AcceptSubmissionItemVerification(ctx, tenantID, subNew, goat, verifierID); err != nil {
		t.Fatalf("accept newest cycle: %v", err)
	}

	var oldItemState string
	if err := pool.QueryRow(ctx,
		`SELECT state FROM sop_submission_items WHERE submission_id = $1::uuid AND goat_id = $2::uuid`,
		subOld, goat).Scan(&oldItemState); err != nil {
		t.Fatalf("read superseded item state: %v", err)
	}
	if oldItemState == "needs_review" {
		t.Fatalf("superseded item is still needs_review -- it strands the parent task open forever, so a drive whose every animal is APPROVED never becomes closeable")
	}

	var taskState string
	if err := pool.QueryRow(ctx, `SELECT state FROM sop_tasks WHERE task_id = $1::uuid`, taskID).Scan(&taskState); err != nil {
		t.Fatalf("read task state: %v", err)
	}
	if taskState != "accepted" {
		t.Fatalf("task state = %q, want %q -- every item is settled, so the drive must be closeable", taskState, "accepted")
	}
}

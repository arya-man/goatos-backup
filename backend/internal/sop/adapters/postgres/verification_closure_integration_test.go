package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	closureTenant     = "00000000-0000-4000-8000-000000000001"
	closurePark       = "73000000-0000-4000-8000-000000000001"
	closureShed       = "73000000-0000-4000-8000-000000000002"
	closureParty      = "73000000-0000-4000-8000-000000000003"
	closureGoatA      = "73000000-0000-4000-8000-000000000004"
	closureGoatB      = "73000000-0000-4000-8000-000000000005"
	closureSOP        = "73000000-0000-4000-8000-000000000006"
	closureSOPVersion = "73000000-0000-4000-8000-000000000007"
	closureTask       = "73000000-0000-4000-8000-000000000008"
	closureSubmission = "73000000-0000-4000-8000-000000000009"
	closureItemA      = "73000000-0000-4000-8000-000000000010"
	closureItemB      = "73000000-0000-4000-8000-000000000011"
	closureActor      = "73000000-0000-4000-8000-000000000012"
)

func TestAcceptSubmissionItemVerificationRollsUpOnlyAfterEveryGoatCloses(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVerificationClosure(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	if err := repo.AcceptSubmissionItemVerification(ctx, closureTenant, closureSubmission, closureGoatA, closureActor); err != nil {
		t.Fatalf("close first goat: %v", err)
	}
	assertClosureStates(t, ctx, pool, "accepted", "needs_review", "needs_review", "needs_review", 1, 0)

	if err := repo.AcceptSubmissionItemVerification(ctx, closureTenant, closureSubmission, closureGoatB, closureActor); err != nil {
		t.Fatalf("close final goat: %v", err)
	}
	assertClosureStates(t, ctx, pool, "accepted", "accepted", "accepted", "accepted", 2, 2)

	var submissionVersion, taskVersion int
	if err := pool.QueryRow(ctx, `SELECT row_version FROM sop_submissions WHERE tenant_id=$1 AND submission_id=$2`, closureTenant, closureSubmission).Scan(&submissionVersion); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT row_version FROM sop_tasks WHERE tenant_id=$1 AND task_id=$2`, closureTenant, closureTask).Scan(&taskVersion); err != nil {
		t.Fatal(err)
	}
	if err := repo.AcceptSubmissionItemVerification(ctx, closureTenant, closureSubmission, closureGoatB, closureActor); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	var replaySubmissionVersion, replayTaskVersion int
	if err := pool.QueryRow(ctx, `SELECT row_version FROM sop_submissions WHERE tenant_id=$1 AND submission_id=$2`, closureTenant, closureSubmission).Scan(&replaySubmissionVersion); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT row_version FROM sop_tasks WHERE tenant_id=$1 AND task_id=$2`, closureTenant, closureTask).Scan(&replayTaskVersion); err != nil {
		t.Fatal(err)
	}
	if replaySubmissionVersion != submissionVersion || replayTaskVersion != taskVersion {
		t.Fatalf("replay advanced row versions: submission %d->%d task %d->%d", submissionVersion, replaySubmissionVersion, taskVersion, replayTaskVersion)
	}
}

func assertClosureStates(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemA, itemB, submission, task string, wantAcceptedItems, wantAcceptedAggregates int) {
	t.Helper()
	var gotA, gotB, gotSubmission, gotTask string
	var acceptedItems, acceptedAggregates int
	if err := pool.QueryRow(ctx, `SELECT state FROM sop_submission_items WHERE tenant_id=$1 AND item_id=$2`, closureTenant, closureItemA).Scan(&gotA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM sop_submission_items WHERE tenant_id=$1 AND item_id=$2`, closureTenant, closureItemB).Scan(&gotB); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM sop_submissions WHERE tenant_id=$1 AND submission_id=$2`, closureTenant, closureSubmission).Scan(&gotSubmission); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM sop_tasks WHERE tenant_id=$1 AND task_id=$2`, closureTenant, closureTask).Scan(&gotTask); err != nil {
		t.Fatal(err)
	}
	if gotA == "accepted" {
		acceptedItems++
	}
	if gotB == "accepted" {
		acceptedItems++
	}
	if gotSubmission == "accepted" {
		acceptedAggregates++
	}
	if gotTask == "accepted" {
		acceptedAggregates++
	}
	if gotA != itemA || gotB != itemB || gotSubmission != submission || gotTask != task || acceptedItems != wantAcceptedItems || acceptedAggregates != wantAcceptedAggregates {
		t.Fatalf("states itemA=%s itemB=%s submission=%s task=%s; accepted items=%d aggregates=%d", gotA, gotB, gotSubmission, gotTask, acceptedItems, acceptedAggregates)
	}
}

func seedVerificationClosure(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	statements := []struct {
		label string
		sql   string
		args  []any
	}{
		{"park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1,$2,'park','PARK-CLOSE','Closure Park','active')`, []any{closurePark, closureTenant}},
		{"shed", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status) VALUES ($1,$2,'shed','SHED-CLOSE','Closure Shed',$3,'active')`, []any{closureShed, closureTenant, closurePark}},
		{"party", `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1,'org','Closure Custodian','active')`, []any{closureParty}},
		{"goat a", `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, shed_id, management_stage, health_status) VALUES ($1,$2,'alive','goat',$3,'female',$4,$5,$4,'K1','healthy')`, []any{closureGoatA, closureTenant, closureParty, closureShed, closurePark}},
		{"goat b", `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, shed_id, management_stage, health_status) VALUES ($1,$2,'alive','goat',$3,'female',$4,$5,$4,'K1','healthy')`, []any{closureGoatB, closureTenant, closureParty, closureShed, closurePark}},
		{"sop", `INSERT INTO sop_definitions (sop_id,tenant_id,code,name,status) VALUES ($1,$2,'vaccination.session','Vaccination session','active')`, []any{closureSOP, closureTenant}},
		{"version", `INSERT INTO sop_versions (sop_version_id,tenant_id,sop_id,version,version_label,status,form_dsl,proof_policy,validation_report) VALUES ($1,$2,$3,1,'v1','published','{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,'{"required":true,"verify_before_apply":true}'::jsonb,'{"valid":true}'::jsonb)`, []any{closureSOPVersion, closureTenant, closureSOP}},
		{"task", `INSERT INTO sop_tasks (task_id,tenant_id,sop_id,sop_version_id,task_type,title,state,scope_type,scope_id) VALUES ($1,$2,$3,$4,'vaccination','Vaccination closure','needs_review','shed',$5)`, []any{closureTask, closureTenant, closureSOP, closureSOPVersion, closureShed}},
		{"submission", `INSERT INTO sop_submissions (submission_id,tenant_id,task_id,sop_version_id,submitted_by,idempotency_key,answers,state) VALUES ($1,$2,$3,$4,$5,'closure-submission','{}'::jsonb,'needs_review')`, []any{closureSubmission, closureTenant, closureTask, closureSOPVersion, closureActor}},
		{"item a", `INSERT INTO sop_submission_items (item_id,tenant_id,submission_id,task_id,goat_id,item_key,state) VALUES ($1,$2,$3,$4,$5,'goat-a','needs_review')`, []any{closureItemA, closureTenant, closureSubmission, closureTask, closureGoatA}},
		{"item b", `INSERT INTO sop_submission_items (item_id,tenant_id,submission_id,task_id,goat_id,item_key,state) VALUES ($1,$2,$3,$4,$5,'goat-b','needs_review')`, []any{closureItemB, closureTenant, closureSubmission, closureTask, closureGoatB}},
	}
	for _, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("%s: %v", stmt.label, err)
		}
	}
}

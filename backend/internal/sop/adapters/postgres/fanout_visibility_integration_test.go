package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

const (
	fanoutTenant     = "00000000-0000-4000-8000-000000000001"
	fanoutPark       = "72000000-0000-4000-8000-000000000001"
	fanoutShed       = "72000000-0000-4000-8000-000000000002"
	fanoutParty      = "72000000-0000-4000-8000-000000000003"
	fanoutGoat       = "72000000-0000-4000-8000-000000000004"
	fanoutSOP        = "72000000-0000-4000-8000-000000000005"
	fanoutSOPVersion = "72000000-0000-4000-8000-000000000006"
	fanoutTask       = "72000000-0000-4000-8000-000000000007"
	fanoutSubmission = "72000000-0000-4000-8000-000000000008"
	fanoutItem       = "72000000-0000-4000-8000-000000000009"
	fanoutActor      = "72000000-0000-4000-8000-000000000010"
)

func TestListAgedFailedSubmissionFanoutsSurfacesMissingVaccinationCompletions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedFailedSubmissionFanout(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	items, err := repo.ListAgedFailedSubmissionFanouts(ctx, ports.ListAgedFailedSubmissionFanoutsParams{
		TenantID:      fanoutTenant,
		UpdatedBefore: time.Date(2026, 6, 24, 11, 30, 0, 0, time.UTC),
		Now:           time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListAgedFailedSubmissionFanouts() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%d want 1: %#v", len(items), items)
	}
	got := items[0]
	if got.TaskID != fanoutTask || got.SubmissionID != fanoutSubmission || got.SOPCode != "vaccination.session" {
		t.Fatalf("identity = %#v", got)
	}
	if got.EligibleSubmissionItems != 1 || got.MaterializedCompletionCount != 0 || got.MissingCompletionCount != 1 {
		t.Fatalf("completion counts = %#v", got)
	}
	if got.AgeSeconds != 3600 {
		t.Fatalf("age seconds = %d want 3600", got.AgeSeconds)
	}

	items, err = repo.ListAgedFailedSubmissionFanouts(ctx, ports.ListAgedFailedSubmissionFanoutsParams{
		TenantID:      fanoutTenant,
		UpdatedBefore: time.Date(2026, 6, 24, 10, 59, 0, 0, time.UTC),
		Now:           time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("newer cutoff query error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("newer cutoff items=%d want 0: %#v", len(items), items)
	}
}

func seedFailedSubmissionFanout(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execFanout(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'PARK-FANOUT', 'Fanout Park', 'active')`,
		fanoutPark, fanoutTenant)
	execFanout(t, ctx, pool, "shed",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-FANOUT', 'Fanout Shed', $3, 'active')`,
		fanoutShed, fanoutTenant, fanoutPark)
	execFanout(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status)
		 VALUES ($1, 'org', 'Fanout Custodian', 'active')`,
		fanoutParty)
	execFanout(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, health_status)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K1', 'healthy')`,
		fanoutGoat, fanoutTenant, fanoutParty, fanoutShed, fanoutPark)
	execFanout(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, description, status)
		 VALUES ($1, $2, 'vaccination.session', 'Vaccination session fanout', 'Fanout visibility regression.', 'active')`,
		fanoutSOP, fanoutTenant)
	execFanout(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, $3, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":true,"subject_scope":"batch","types":["video"],"minimum_count":1,"verify_before_apply":true}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		fanoutSOPVersion, fanoutTenant, fanoutSOP)
	execFanout(t, ctx, pool, "sop task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, assigned_to, scope_type, scope_id)
		 VALUES ($1, $2, $3, $4, 'vaccination_drive', 'Vaccination fanout drive', 'needs_review', $5, 'shed', $6)`,
		fanoutTask, fanoutTenant, fanoutSOP, fanoutSOPVersion, fanoutActor, fanoutShed)
	execFanout(t, ctx, pool, "submission",
		`INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key,
		   answers, proof_refs, state, validation_report, submitted_at)
		 VALUES ($1, $2, $3, $4, $5, 'fanout-visibility-submission',
		   '{}'::jsonb, '[]'::jsonb, 'needs_review', '{"valid":true}'::jsonb, TIMESTAMPTZ '2026-06-24 10:50:00+00')`,
		fanoutSubmission, fanoutTenant, fanoutTask, fanoutSOPVersion, fanoutActor)
	execFanout(t, ctx, pool, "submission item",
		`INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state)
		 VALUES ($1, $2, $3, $4, $5, 'goat-1', 'needs_review')`,
		fanoutItem, fanoutTenant, fanoutSubmission, fanoutTask, fanoutGoat)
	execFanout(t, ctx, pool, "failed fanout",
		`INSERT INTO sop_task_submission_fanouts (tenant_id, task_id, submission_id, status, requested_by, attempt_count, last_error, updated_at)
		 VALUES ($1, $2, $3, 'failed', $4, 2, 'vaccination: materialized 0 of 1 eligible submission items', TIMESTAMPTZ '2026-06-24 11:00:00+00')`,
		fanoutTenant, fanoutTask, fanoutSubmission, fanoutActor)
}

func execFanout(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

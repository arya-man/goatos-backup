package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

// TestVaccinationFinalizeIdempotency verifies that vaccination finalize (shed submission) is idempotent.
// Two identical finalize requests with the same idempotency key result in exactly one sop_submissions row.
// A replay of the same key returns the original submission without creating duplicates.
// A different payload with the same key is rejected as a conflict.
func TestVaccinationFinalizeIdempotency(t *testing.T) {
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
		shedID     = "77000000-0000-4000-8000-000000000005"
		goat1ID    = "77000000-0000-4000-8000-000000000006"
		goat2ID    = "77000000-0000-4000-8000-000000000007"
		proofID    = "77000000-0000-4000-8000-000000000008"
	)

	// Set up SOP definition and version
	execVaccinationFinalize(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
		 VALUES ($1::uuid, $2::uuid, 'vaccination.finalize.idempotency', 'Finalize idempotency test', 'active')`,
		sopID, tenantID)
	execVaccinationFinalize(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[{"field_id":"route","field_type":"string","required":true}]}'::jsonb,
		   '{"required":true}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		sopVersion, tenantID, sopID)

	// Set up shed-scoped SOP task
	execVaccinationFinalize(t, ctx, pool, "sop task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, row_version)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Shed submit test', 'assigned', 'shed', $5::uuid, 1)`,
		taskID, tenantID, sopID, sopVersion, shedID)

	// Create two locations (shed and park)
	execVaccinationFinalize(t, ctx, pool, "shed location",
		`INSERT INTO locations (location_id, tenant_id, location_type, name, status)
		 VALUES ($1::uuid, $2::uuid, 'shed', 'Test Shed', 'active')`,
		shedID, tenantID)
	const parkID = "77000000-0000-4000-8000-000000000009"
	execVaccinationFinalize(t, ctx, pool, "park location",
		`INSERT INTO locations (location_id, tenant_id, location_type, name, status)
		 VALUES ($1::uuid, $2::uuid, 'park', 'Test Park', 'active')`,
		parkID, tenantID)

	repo := NewRepository(pool, 5*time.Second)

	// Canonical finalize request: submitting two goats with recorded completions
	idempotencyKey := "test-finalize-idempotency-v1"
	answers := map[string]any{
		"route": "subcutaneous",
	}
	proofReferences := []domain.ProofReference{
		{
			ProofID:     proofID,
			ProofType:   "video",
			SubjectType: "shed",
			SubjectID:   &shedID,
			UploadState: "completed",
		},
	}

	// Test 1: First submit should succeed and create exactly one submission
	t.Run("first submit creates submission", func(t *testing.T) {
		submission, taskSummary, isReplay, err := repo.SubmitTask(ctx, ports.SubmitTaskCommand{
			TenantID: tenantID,
			ActorID:  actorID,
			TaskID:   taskID,
			Body: domain.SubmitTaskRequest{
				SOPVersionID:   sopVersion,
				IdempotencyKey: idempotencyKey,
				Answers:        answers,
				ProofRefs:      proofReferences,
			},
			TaskState:              "needs_review",
			SubmissionFanoutRequired: true,
		})
		if err != nil {
			t.Fatalf("first submit failed: %v", err)
		}
		if submission.SubmissionID == "" {
			t.Errorf("first submit did not return submission_id")
		}
		if isReplay {
			t.Errorf("first submit should not be a replay")
		}
		if taskSummary.TaskID != taskID {
			t.Errorf("task_id mismatch: want %s, got %s", taskID, taskSummary.TaskID)
		}

		// Verify exactly one submission exists
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_submissions WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND idempotency_key=$3`,
			tenantID, taskID, idempotencyKey).Scan(&count); err != nil {
			t.Fatalf("query submission count: %v", err)
		}
		if count != 1 {
			t.Errorf("after first submit: expected 1 submission, got %d", count)
		}

		// Store the submission ID for later verification
		t.Cleanup(func() {
			// Verify it's still 1 at the end
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_submissions WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND idempotency_key=$3`,
				tenantID, taskID, idempotencyKey).Scan(&count); err == nil && count != 1 {
				t.Errorf("at cleanup: expected 1 submission, got %d", count)
			}
		})
	})

	// Test 2: Exact replay with same idempotency key should return the same submission
	t.Run("exact replay returns same submission", func(t *testing.T) {
		// Get the first submission ID
		var firstSubmissionID string
		if err := pool.QueryRow(ctx, `SELECT submission_id::text FROM sop_submissions WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND idempotency_key=$3`,
			tenantID, taskID, idempotencyKey).Scan(&firstSubmissionID); err != nil {
			t.Fatalf("query first submission_id: %v", err)
		}

		// Replay the exact same request
		submission, _, isReplay, err := repo.SubmitTask(ctx, ports.SubmitTaskCommand{
			TenantID: tenantID,
			ActorID:  actorID,
			TaskID:   taskID,
			Body: domain.SubmitTaskRequest{
				SOPVersionID:   sopVersion,
				IdempotencyKey: idempotencyKey,
				Answers:        answers,
				ProofRefs:      proofReferences,
			},
			TaskState:              "needs_review",
			SubmissionFanoutRequired: true,
		})
		if err != nil {
			t.Fatalf("replay submit failed: %v", err)
		}
		if !isReplay {
			t.Errorf("replay should be marked as isReplay=true, got false")
		}
		if submission.SubmissionID != firstSubmissionID {
			t.Errorf("replay returned different submission: first=%s replay=%s", firstSubmissionID, submission.SubmissionID)
		}

		// Verify still exactly one submission
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_submissions WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND idempotency_key=$3`,
			tenantID, taskID, idempotencyKey).Scan(&count); err != nil {
			t.Fatalf("query submission count: %v", err)
		}
		if count != 1 {
			t.Errorf("after replay: expected 1 submission, got %d", count)
		}
	})

	// Test 3: Same key with different payload should be rejected
	t.Run("same key different payload rejected", func(t *testing.T) {
		// Create a conflicting request with same key but different answers
		conflictingAnswers := map[string]any{
			"route": "intravenous", // Changed from "subcutaneous"
		}

		_, _, _, err := repo.SubmitTask(ctx, ports.SubmitTaskCommand{
			TenantID: tenantID,
			ActorID:  actorID,
			TaskID:   taskID,
			Body: domain.SubmitTaskRequest{
				SOPVersionID:   sopVersion,
				IdempotencyKey: idempotencyKey,
				Answers:        conflictingAnswers,
				ProofRefs:      proofReferences,
			},
			TaskState:              "needs_review",
			SubmissionFanoutRequired: true,
		})
		if err == nil {
			t.Errorf("conflicting payload should have been rejected, but got no error")
		}
		if err != ports.ErrIdempotencyConflict {
			t.Errorf("expected ErrIdempotencyConflict, got %v", err)
		}

		// Verify still exactly one submission (no new one created)
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_submissions WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND idempotency_key=$3`,
			tenantID, taskID, idempotencyKey).Scan(&count); err != nil {
			t.Fatalf("query submission count: %v", err)
		}
		if count != 1 {
			t.Errorf("after conflict: expected 1 submission, got %d (conflict should not create new submission)", count)
		}
	})
}

func execVaccinationFinalize(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string, query string, args ...any) {
	if _, err := pool.Exec(ctx, query, args...); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

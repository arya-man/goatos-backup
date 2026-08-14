package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestDoneCountUnionDisjointPaths verifies that done_count correctly aggregates disjoint completion paths
// (e.g., some animals completed-only + other animals proof-only = union of all done, not max).
// This test uses the VaccinationScheduleCanonical query with ExecutionProjection.
func TestDoneCountUnionDisjointPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	// Set up base project data once
	seedVaccinationExecutionProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	testCases := []struct {
		name          string
		scenario      func() string // Returns batch ID
		expectedDone  int
		expectedOpen  int
		expectedState string
	}{
		{
			name: "5 targets 1 proof only",
			scenario: func() string {
				bid := fmt.Sprintf("e2b00000-0000-4000-8000-%012d", time.Now().UnixNano()%1000000000000)
				execProjectionSQL(t, ctx, pool, "batch",
					`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
					 VALUES ($1, $2, $3, 'shed', $4, 'in_progress', DATE '2026-06-24', $5)`,
					bid, testTenant, testVersion, testShed, testOperator)
				tid := taskFor(bid)
				execProjectionSQL(t, ctx, pool, "task",
					`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, assigned_to, scope_type, scope_id, context)
					 VALUES ($1, $2, $3, $4, 'vaccination', 'T', 'assigned', $5, 'shed', $6, jsonb_build_object('obligation_batch_id',$7::text))`,
					tid, testTenant, testVaccinationSOP, testVaccinationSOPVer, testOperator, testShed, bid)
				execProjectionSQL(t, ctx, pool, "link task batch",
					`UPDATE obligation_batches SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, tid, testTenant, bid)
				// Create 5 obligations, mark 1 with proof
				for i := 1; i <= 5; i++ {
					gid := fmt.Sprintf("e2c00001-0000-4000-8000-%012d", i)
					insertProjectionGoat(t, ctx, pool, gid, testShed, testPark)
					oid := fmt.Sprintf("e2d00001-0000-4000-8000-%012d", i)
					execProjectionSQL(t, ctx, pool, "obligation",
						`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
						 target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
						 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, TIMESTAMPTZ '2026-06-24 00:00:00+00', 'in_progress', $8, $9)`,
						oid, testTenant, testVersion, testRule, bid, gid, testShed, fmt.Sprintf("key-proof1-%d", i), i)
					if i == 1 {
						execProjectionSQL(t, ctx, pool, "proof",
							`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, created_at, uploaded_at)
							 VALUES (gen_random_uuid(), $1, 'local', 'k/'||$2::text, 'video/mp4', 'completed', 'task', $3::uuid, 'goat', $2::uuid, 'video', $4, TIMESTAMPTZ '2026-06-24 09:30:00+00', TIMESTAMPTZ '2026-06-24 09:30:00+00')`,
							testTenant, gid, taskFor(bid), testOperator)
					}
				}
				execProjectionSQL(t, ctx, pool, "link obligations",
					`UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, taskFor(bid), testTenant, bid)
				return bid
			},
			expectedDone:  1,
			expectedOpen:  4,
			expectedState: "in_progress",
		},
		{
			name: "5 all proofed",
			scenario: func() string {
				bid := fmt.Sprintf("e2b00001-0000-4000-8000-%012d", time.Now().UnixNano()%1000000000000)
				execProjectionSQL(t, ctx, pool, "batch",
					`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
					 VALUES ($1, $2, $3, 'shed', $4, 'in_progress', DATE '2026-06-24', $5)`,
					bid, testTenant, testVersion, testShed, testOperator)
				tid := taskFor(bid)
				execProjectionSQL(t, ctx, pool, "task",
					`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, assigned_to, scope_type, scope_id, context)
					 VALUES ($1, $2, $3, $4, 'vaccination', 'T', 'assigned', $5, 'shed', $6, jsonb_build_object('obligation_batch_id',$7::text))`,
					tid, testTenant, testVaccinationSOP, testVaccinationSOPVer, testOperator, testShed, bid)
				execProjectionSQL(t, ctx, pool, "link task batch",
					`UPDATE obligation_batches SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, tid, testTenant, bid)
				// Create 5 obligations, all with proof
				for i := 1; i <= 5; i++ {
					gid := fmt.Sprintf("e2c00005-0000-4000-8000-%012d", i)
					insertProjectionGoat(t, ctx, pool, gid, testShed, testPark)
					oid := fmt.Sprintf("e2d00005-0000-4000-8000-%012d", i)
					execProjectionSQL(t, ctx, pool, "obligation",
						`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
						 target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
						 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, TIMESTAMPTZ '2026-06-24 00:00:00+00', 'in_progress', $8, $9)`,
						oid, testTenant, testVersion, testRule, bid, gid, testShed, fmt.Sprintf("key-proof5-%d", i), i)
					execProjectionSQL(t, ctx, pool, "proof",
						`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, created_at, uploaded_at)
						 VALUES (gen_random_uuid(), $1, 'local', 'k5/'||$2::text, 'video/mp4', 'completed', 'task', $3::uuid, 'goat', $2::uuid, 'video', $4, TIMESTAMPTZ '2026-06-24 09:30:00+00', TIMESTAMPTZ '2026-06-24 09:30:00+00')`,
						testTenant, gid, taskFor(bid), testOperator)
				}
				execProjectionSQL(t, ctx, pool, "link obligations",
					`UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, taskFor(bid), testTenant, bid)
				return bid
			},
			expectedDone:  5,
			expectedOpen:  0,
			expectedState: "verification_pending",
		},
		{
			name: "3 completed + 2 proof disjoint union",
			scenario: func() string {
				bid := fmt.Sprintf("e2b00002-0000-4000-8000-%012d", time.Now().UnixNano()%1000000000000)
				execProjectionSQL(t, ctx, pool, "batch",
					`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
					 VALUES ($1, $2, $3, 'shed', $4, 'in_progress', DATE '2026-06-24', $5)`,
					bid, testTenant, testVersion, testShed, testOperator)
				tid := taskFor(bid)
				execProjectionSQL(t, ctx, pool, "task",
					`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, assigned_to, scope_type, scope_id, context)
					 VALUES ($1, $2, $3, $4, 'vaccination', 'T', 'assigned', $5, 'shed', $6, jsonb_build_object('obligation_batch_id',$7::text))`,
					tid, testTenant, testVaccinationSOP, testVaccinationSOPVer, testOperator, testShed, bid)
				execProjectionSQL(t, ctx, pool, "link task batch",
					`UPDATE obligation_batches SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, tid, testTenant, bid)
				// Create 5 obligations: first 3 completed, last 2 proof-only
				for i := 1; i <= 5; i++ {
					gid := fmt.Sprintf("e2c00009-0000-4000-8000-%012d", i)
					insertProjectionGoat(t, ctx, pool, gid, testShed, testPark)
					oid := fmt.Sprintf("e2d00009-0000-4000-8000-%012d", i)
					execProjectionSQL(t, ctx, pool, "obligation",
						`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
						 target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
						 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, TIMESTAMPTZ '2026-06-24 00:00:00+00', 'in_progress', $8, $9)`,
						oid, testTenant, testVersion, testRule, bid, gid, testShed, fmt.Sprintf("key-disj-%d", i), i)
					if i <= 3 {
						// Completed path
						cid := fmt.Sprintf("e2e00009-0000-4000-8000-%012d", i)
						execProjectionSQL(t, ctx, pool, "completion",
							`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
							 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-24 09:00:00+00', 'recorded', $6, $7)`,
							cid, testTenant, oid, bid, gid, fmt.Sprintf("key-comp-%d", i), testOperator)
					} else {
						// Proof-only path
						execProjectionSQL(t, ctx, pool, "proof",
							`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, created_at, uploaded_at)
							 VALUES (gen_random_uuid(), $1, 'local', 'kd/'||$2::text, 'video/mp4', 'completed', 'task', $3::uuid, 'goat', $2::uuid, 'video', $4, TIMESTAMPTZ '2026-06-24 09:30:00+00', TIMESTAMPTZ '2026-06-24 09:30:00+00')`,
							testTenant, gid, taskFor(bid), testOperator)
					}
				}
				execProjectionSQL(t, ctx, pool, "link obligations",
					`UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, taskFor(bid), testTenant, bid)
				return bid
			},
			expectedDone:  5, // Union: 3 completed + 2 proof = 5 done
			expectedOpen:  0,
			expectedState: "verification_pending",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			batchID := tc.scenario()

			// Use ListVaccinationExecutionPage which returns ExecutionProjection
			q := domain.ExecutionQuery{
				TenantID:   testTenant,
				AsOf:       time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
				DueBefore:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				Limit:      1000,
			}

			page, err := repo.ListVaccinationExecutionPage(ctx, q)
			if err != nil {
				t.Fatalf("ListVaccinationExecutionPage: %v", err)
			}

			// Find the batch in results
			var found *domain.ExecutionProjection
			for i := range page.Rows {
				if page.Rows[i].BatchID != nil && *page.Rows[i].BatchID == batchID {
					found = &page.Rows[i]
					break
				}
			}

			if found == nil {
				t.Fatalf("batch %s not found in results (got %d rows)", batchID, len(page.Rows))
			}

			// Verify done_count
			if found.DoneCount != tc.expectedDone {
				t.Errorf("done_count: want %d, got %d", tc.expectedDone, found.DoneCount)
			}

			// Verify open count: obligation_count - done_count - deferred_count - missed_count - canceled_count
			openCount := found.ObligationCount - found.DoneCount - found.DeferredCount - found.MissedCount - found.CanceledCount
			if openCount != tc.expectedOpen {
				t.Errorf("open_count: want %d, got %d (obligation=%d - done=%d - deferred=%d)",
					tc.expectedOpen, openCount, found.ObligationCount, found.DoneCount, found.DeferredCount)
			}

			// Verify work_state
			if found.WorkState != domain.WorkState(tc.expectedState) {
				t.Errorf("work_state: want %s, got %s", tc.expectedState, found.WorkState)
			}
		})
	}
}

// taskFor derives a stable per-batch sop_task uuid from a batch uuid (flip first byte group).
func taskFor(batchID string) string {
	return "e2f" + batchID[3:]
}

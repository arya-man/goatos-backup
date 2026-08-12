package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// 000152 restores the shed grain: unstarted PEN tasks retire so the shed's own task takes over,
// while any pen task an operator already filmed keeps its status and its evidence. Replay must not
// bump a row_version, and a whole-shed task must never be touched.
func TestFeedTransportShedGrainRestoreRetiresUnstartedPenWorkAndKeepsFilmedEvidence(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant          = "f1520000-0000-4000-8000-000000000001"
		park            = "f1520000-0000-4000-8000-000000003001"
		shed            = "f1520000-0000-4000-8000-000000004001"
		penUnstarted    = "f1520000-0000-4000-8000-000000005001"
		penPending      = "f1520000-0000-4000-8000-000000005002"
		penRework       = "f1520000-0000-4000-8000-000000005003"
		penCompleted    = "f1520000-0000-4000-8000-000000005004"
		wholeShedDue    = "f1520000-0000-4000-8000-000000005005"
		pendingAttempt  = "f1520000-0000-4000-8000-000000006001"
		rejectedAttempt = "f1520000-0000-4000-8000-000000006002"
		operator        = "f1520000-0000-4000-8000-000000009001"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Feed Transport Shed Grain Restore', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, tenant)
	exec(`INSERT INTO locations (location_id, tenant_id, parent_location_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, NULL, 'park', 'F152-P', 'F152 Park', 'active'),
       ($3::uuid, $1::uuid, $2::uuid, 'shed', 'F152-S', 'F152 Shed', 'active')
ON CONFLICT (location_id) DO NOTHING`, tenant, park, shed)
	exec(`INSERT INTO feed_transport_tasks
  (task_id, tenant_id, park_id, shed_id, partition_label, business_date, scheduled_at, status, completed_at)
VALUES
  ($4::uuid, $1::uuid, $2::uuid, $3::uuid, 'Part 1', '2026-08-20', '2026-08-20 15:30 Asia/Kolkata', 'due', NULL),
  ($5::uuid, $1::uuid, $2::uuid, $3::uuid, 'Part 2', '2026-08-20', '2026-08-20 15:30 Asia/Kolkata', 'verification_due', NULL),
  ($6::uuid, $1::uuid, $2::uuid, $3::uuid, 'Part 3', '2026-08-20', '2026-08-20 15:30 Asia/Kolkata', 'rework', NULL),
  ($7::uuid, $1::uuid, $2::uuid, $3::uuid, 'Part 4', '2026-08-20', '2026-08-20 15:30 Asia/Kolkata', 'completed', '2026-08-20 16:00 Asia/Kolkata'),
  ($8::uuid, $1::uuid, $2::uuid, $3::uuid, '', '2026-08-21', '2026-08-21 15:30 Asia/Kolkata', 'due', NULL)`,
		tenant, park, shed, penUnstarted, penPending, penRework, penCompleted, wholeShedDue)
	exec(`INSERT INTO feed_transport_attempts
  (attempt_id, tenant_id, task_id, attempt_no, proof_ref, operator_id, status, rejection_reason, idempotency_key)
VALUES
  ($3::uuid, $1::uuid, $2::uuid, 1, 'pending-proof', $5::uuid, 'verification_due', NULL, 'f152-pending-proof'),
  ($4::uuid, $1::uuid, $6::uuid, 1, 'rejected-proof', $5::uuid, 'rejected', 'video unclear', 'f152-rejected-proof')`,
		tenant, penPending, pendingAttempt, rejectedAttempt, operator, penRework)
	exec(`UPDATE feed_transport_tasks
SET current_attempt_id = CASE task_id WHEN $2::uuid THEN $3::uuid WHEN $4::uuid THEN $5::uuid END,
    operator_id = $6::uuid
WHERE tenant_id=$1::uuid AND task_id IN ($2::uuid, $4::uuid)`,
		tenant, penPending, pendingAttempt, penRework, rejectedAttempt, operator)

	raw, err := os.ReadFile("000155_feed_transport_restore_shed_grain.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("replay 000152: %v", err)
	}

	want := map[string]string{
		penUnstarted: "retired",
		// Filmed work is evidence. Retiring any of these would throw away a video an operator
		// actually recorded, and one of them a verifier has already judged.
		penPending:   "verification_due",
		penRework:    "rework",
		penCompleted: "completed",
		// A whole-shed task is already the right grain and must be left alone.
		wholeShedDue: "due",
	}
	versions := map[string]int{}
	for id, wantStatus := range want {
		var status string
		var version int
		if err := pool.QueryRow(ctx, `
SELECT status, row_version
FROM feed_transport_tasks
WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenant, id).Scan(&status, &version); err != nil {
			t.Fatalf("query task %s: %v", id, err)
		}
		if status != wantStatus {
			t.Fatalf("task %s status = %q, want %q", id, status, wantStatus)
		}
		versions[id] = version
	}

	var attempts, pendingProof, rejectedProof int
	if err := pool.QueryRow(ctx, `
SELECT count(*),
       count(*) FILTER (WHERE attempt_id=$2::uuid AND status='verification_due' AND proof_ref='pending-proof'),
       count(*) FILTER (WHERE attempt_id=$3::uuid AND status='rejected' AND proof_ref='rejected-proof' AND rejection_reason='video unclear')
FROM feed_transport_attempts
WHERE tenant_id=$1::uuid`, tenant, pendingAttempt, rejectedAttempt).Scan(&attempts, &pendingProof, &rejectedProof); err != nil {
		t.Fatalf("query attempts: %v", err)
	}
	if attempts != 2 || pendingProof != 1 || rejectedProof != 1 {
		t.Fatalf("attempt evidence attempts=%d pending=%d rejected=%d, want preserved 2/1/1", attempts, pendingProof, rejectedProof)
	}

	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("replay 000152 again: %v", err)
	}
	for id, wantVersion := range versions {
		var got int
		if err := pool.QueryRow(ctx, `
SELECT row_version
FROM feed_transport_tasks
WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenant, id).Scan(&got); err != nil {
			t.Fatalf("query replayed version for %s: %v", id, err)
		}
		if got != wantVersion {
			t.Fatalf("task %s row_version = %d after replay, want unchanged %d", id, got, wantVersion)
		}
	}
}

package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestFeedTransportRolloutRepairRestoresActionableStateWithoutDeletingEvidence(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant          = "f1460000-0000-4000-8000-000000000001"
		park            = "f1460000-0000-4000-8000-000000003001"
		shed            = "f1460000-0000-4000-8000-000000004001"
		due             = "f1460000-0000-4000-8000-000000005001"
		pending         = "f1460000-0000-4000-8000-000000005002"
		rework          = "f1460000-0000-4000-8000-000000005003"
		completed       = "f1460000-0000-4000-8000-000000005004"
		pendingAttempt  = "f1460000-0000-4000-8000-000000006002"
		rejectedAttempt = "f1460000-0000-4000-8000-000000006003"
		operator        = "f1460000-0000-4000-8000-000000009001"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Feed Transport Repair', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, tenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'F146-P', 'F146 Park', 'active'),
       ($3::uuid, $1::uuid, 'shed', 'F146-S', 'F146 Shed', 'active')
ON CONFLICT (location_id) DO NOTHING`, tenant, park, shed)
	exec(`UPDATE locations SET parent_location_id=$2::uuid WHERE tenant_id=$1::uuid AND location_id=$3::uuid`, tenant, park, shed)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')
ON CONFLICT DO NOTHING`, tenant, shed)
	exec(`INSERT INTO feed_transport_tasks
  (task_id, tenant_id, park_id, shed_id, partition_label, business_date, scheduled_at, status, completed_at)
VALUES
  ($4::uuid, $1::uuid, $2::uuid, $3::uuid, '', '2026-08-10', '2026-08-10 15:30 Asia/Kolkata', 'completed', NULL),
  ($5::uuid, $1::uuid, $2::uuid, $3::uuid, '', '2026-08-11', '2026-08-11 15:30 Asia/Kolkata', 'completed', NULL),
  ($6::uuid, $1::uuid, $2::uuid, $3::uuid, '', '2026-08-12', '2026-08-12 15:30 Asia/Kolkata', 'completed', NULL),
  ($7::uuid, $1::uuid, $2::uuid, $3::uuid, '', '2026-08-13', '2026-08-13 15:30 Asia/Kolkata', 'completed', '2026-08-13 16:00 Asia/Kolkata')`,
		tenant, park, shed, due, pending, rework, completed)
	exec(`INSERT INTO feed_transport_attempts
  (attempt_id, tenant_id, task_id, attempt_no, proof_ref, operator_id, status, rejection_reason, idempotency_key)
VALUES
  ($3::uuid, $1::uuid, $2::uuid, 1, 'pending-proof', $5::uuid, 'verification_due', NULL, 'pending-proof-key'),
  ($4::uuid, $1::uuid, $6::uuid, 1, 'rejected-proof', $5::uuid, 'rejected', 'video unclear', 'rejected-proof-key')`,
		tenant, pending, pendingAttempt, rejectedAttempt, operator, rework)
	exec(`UPDATE feed_transport_tasks
SET current_attempt_id = CASE task_id WHEN $2::uuid THEN $3::uuid WHEN $4::uuid THEN $5::uuid END,
    operator_id = $6::uuid
WHERE tenant_id=$1::uuid AND task_id IN ($2::uuid, $4::uuid)`,
		tenant, pending, pendingAttempt, rework, rejectedAttempt, operator)

	raw, err := os.ReadFile("000146_feed_transport_partition_rollout_repair.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("replay 000146: %v", err)
	}

	got := map[string]string{}
	rows, err := pool.Query(ctx, `
SELECT task_id::text, status
FROM feed_transport_tasks
WHERE tenant_id=$1::uuid
ORDER BY business_date`, tenant)
	if err != nil {
		t.Fatalf("query tasks: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan task: %v", err)
		}
		got[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("task rows: %v", err)
	}
	if got[due] != "retired" || got[pending] != "verification_due" || got[rework] != "rework" || got[completed] != "completed" {
		t.Fatalf("recovered statuses = %#v, want retired/verification_due/rework/completed", got)
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

	versions := map[string]int{}
	rows, err = pool.Query(ctx, `
SELECT task_id::text, row_version
FROM feed_transport_tasks
WHERE tenant_id=$1::uuid`, tenant)
	if err != nil {
		t.Fatalf("query repaired versions: %v", err)
	}
	for rows.Next() {
		var id string
		var version int
		if err := rows.Scan(&id, &version); err != nil {
			t.Fatalf("scan repaired version: %v", err)
		}
		versions[id] = version
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("repaired version rows: %v", err)
	}

	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("replay 000146 again: %v", err)
	}
	for id, want := range versions {
		var gotVersion int
		if err := pool.QueryRow(ctx, `
SELECT row_version
FROM feed_transport_tasks
WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenant, id).Scan(&gotVersion); err != nil {
			t.Fatalf("query replayed version for %s: %v", id, err)
		}
		if gotVersion != want {
			t.Fatalf("task %s row_version = %d after replay, want unchanged %d", id, gotVersion, want)
		}
	}
}

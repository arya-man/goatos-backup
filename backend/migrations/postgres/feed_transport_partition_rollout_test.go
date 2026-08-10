package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestFeedTransportPartitionMigrationPreservesInFlightEvidenceAndReplaysCleanly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant              = "f1430000-0000-4000-8000-000000000001"
		park                = "f1430000-0000-4000-8000-000000003001"
		shed                = "f1430000-0000-4000-8000-000000004001"
		legacyDue           = "f1430000-0000-4000-8000-000000005001"
		legacyVerification  = "f1430000-0000-4000-8000-000000005002"
		partitionTask       = "f1430000-0000-4000-8000-000000005003"
		completedTask       = "f1430000-0000-4000-8000-000000005004"
		verificationAttempt = "f1430000-0000-4000-8000-000000006001"
		operator            = "f1430000-0000-4000-8000-000000009001"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Feed Transport Partition Replay', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, tenant)
	exec(`INSERT INTO locations (location_id, tenant_id, parent_location_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, NULL, 'park', 'F143-P', 'F143 Park', 'active'),
       ($3::uuid, $1::uuid, $2::uuid, 'shed', 'F143-S', 'F143 Shed', 'active')
ON CONFLICT (location_id) DO NOTHING`, tenant, park, shed)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')
ON CONFLICT DO NOTHING`, tenant, shed)
	exec(`INSERT INTO feed_transport_tasks
  (task_id, tenant_id, park_id, shed_id, partition_label, business_date, scheduled_at, status, completed_at)
VALUES
  ($4::uuid, $1::uuid, $2::uuid, $3::uuid, '', '2026-08-20', '2026-08-20 15:30 Asia/Kolkata', 'due', NULL),
  ($5::uuid, $1::uuid, $2::uuid, $3::uuid, '', '2026-08-21', '2026-08-21 15:30 Asia/Kolkata', 'verification_due', NULL),
  ($6::uuid, $1::uuid, $2::uuid, $3::uuid, 'Part 1', '2026-08-22', '2026-08-22 15:30 Asia/Kolkata', 'due', NULL),
  ($7::uuid, $1::uuid, $2::uuid, $3::uuid, '', '2026-08-23', '2026-08-23 15:30 Asia/Kolkata', 'completed', '2026-08-23 16:00 Asia/Kolkata')`,
		tenant, park, shed, legacyDue, legacyVerification, partitionTask, completedTask)
	exec(`INSERT INTO feed_transport_attempts
  (attempt_id, tenant_id, task_id, attempt_no, proof_ref, operator_id, status, idempotency_key)
VALUES ($3::uuid, $1::uuid, $2::uuid, 1, 'pending-proof', $4::uuid, 'verification_due', 'f143-pending-proof')`,
		tenant, legacyVerification, verificationAttempt, operator)
	exec(`UPDATE feed_transport_tasks
SET current_attempt_id=$3::uuid, operator_id=$4::uuid
WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenant, legacyVerification, verificationAttempt, operator)

	raw, err := os.ReadFile("000143_feed_transport_partition_tasks.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	apply := func() {
		t.Helper()
		runNoTransactionMigration(t, ctx, pool, migrationUp(string(raw)))
	}
	apply()

	wantStatus := map[string]string{
		legacyDue:          "retired",
		legacyVerification: "verification_due",
		partitionTask:      "due",
		completedTask:      "completed",
	}
	versions := map[string]int{}
	for id, want := range wantStatus {
		var status string
		var version int
		if err := pool.QueryRow(ctx, `
SELECT status, row_version
FROM feed_transport_tasks
WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenant, id).Scan(&status, &version); err != nil {
			t.Fatalf("query task %s: %v", id, err)
		}
		if status != want {
			t.Fatalf("task %s status = %q, want %q", id, status, want)
		}
		versions[id] = version
	}

	apply()
	for id, want := range versions {
		var version int
		if err := pool.QueryRow(ctx, `
SELECT row_version
FROM feed_transport_tasks
WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, tenant, id).Scan(&version); err != nil {
			t.Fatalf("query replayed task %s: %v", id, err)
		}
		if version != want {
			t.Fatalf("task %s row_version = %d after replay, want unchanged %d", id, version, want)
		}
	}

	var valid bool
	if err := pool.QueryRow(ctx, `
SELECT i.indisvalid
FROM pg_index i
JOIN pg_class c ON c.oid=i.indexrelid
WHERE c.oid='public.feed_transport_tasks_daily_location_uq'::regclass`).Scan(&valid); err != nil {
		t.Fatalf("query partition arbiter validity: %v", err)
	}
	if !valid {
		t.Fatal("feed_transport_tasks_daily_location_uq is invalid after replay")
	}
}

func runNoTransactionMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	defer conn.Release()

	for _, stmt := range splitTopLevelStatements(sql) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("run migration statement (%.80s...): %v", strings.TrimSpace(stmt), err)
		}
	}
}

func TestStagingDeployQuiescesOldWritersBeforeFeedTransportContractMigration(t *testing.T) {
	raw, err := os.ReadFile("../../../tools/deploy/stg-clouddeploy-task.sh")
	if err != nil {
		t.Fatalf("read staging deploy task: %v", err)
	}
	script := string(raw)
	start := strings.Index(script, "deploy() {")
	if start < 0 {
		t.Fatal("deploy() function is missing")
	}
	deploy := script[start:]

	assertOrderedTokens(t, deploy,
		"capture_serving_revisions \"$API_SERVICE\"",
		"--image=\"$BACKEND_IMAGE\"",
		"--ingress=internal",
		"capture_serving_revisions \"$KERNEL_WORKER_SERVICE\"",
		"GOATOS_WORKER_STAGES_ENABLED=false",
		"drain_replaced_revisions \"$API_SERVICE\"",
		"drain_replaced_revisions \"$KERNEL_WORKER_SERVICE\"",
		"gcloud run jobs execute \"$MIGRATE_JOB\"",
		"--ingress=all",
		"GOATOS_WORKER_STAGES_ENABLED=true",
	)

	if strings.Contains(script, `if ! main "$@"`) {
		t.Fatal("main is wrapped in `if !`; Bash errexit cannot observe migration failures")
	}
	if !strings.Contains(script, "trap write_failed_on_exit EXIT") {
		t.Fatal("EXIT trap does not publish a failed Cloud Deploy result")
	}
}

func assertOrderedTokens(t *testing.T, content string, tokens ...string) {
	t.Helper()
	offset := 0
	for _, token := range tokens {
		i := strings.Index(content[offset:], token)
		if i < 0 {
			t.Fatalf("missing or out-of-order token %q", token)
		}
		offset += i + len(token)
	}
}

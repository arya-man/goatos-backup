package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

func TestDirtyProjectionWorkerCoalescesAndPublishesShedShard(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	seedShedProjectionParityFixture(t, ctx, pool)

	repo := NewRepository(pool, 10*time.Second)
	asOf := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	dueBefore := asOf.Add(defaultExecutionHorizon)
	execProjectionSQL(t, ctx, pool, "clear bootstrap invalidations", `DELETE FROM projection_dirty_scopes`)
	execProjectionSQL(t, ctx, pool, "enqueue bootstrap shed", `SELECT vaccination_projection_enqueue_shed($1::uuid,$2::uuid,'test_bootstrap',$3::timestamptz)`,
		testTenant, testShed, asOf)
	bootstrap, err := repo.ProcessDirtyProjectionScopes(ctx, domain.DirtyProjectionWorkerRequest{
		WorkerID: "dirty-projection-bootstrap-test", Limit: 10, LeaseFor: time.Minute, AsOf: asOf, DueBefore: dueBefore,
	})
	if err != nil || bootstrap.Completed != 1 {
		t.Fatalf("automatic bootstrap worker result=%#v err=%v", bootstrap, err)
	}
	var shedInitial, executionInitial, operationsInitial int64
	if err := pool.QueryRow(ctx, `SELECT
 (SELECT serving_projection_version FROM vaccination_shed_projection_state WHERE tenant_id=$1::uuid),
 (SELECT serving_projection_version FROM vaccination_execution_projection_state WHERE tenant_id=$1::uuid),
 (SELECT serving_projection_version FROM vaccination_operations_projection_state WHERE tenant_id=$1::uuid)`, testTenant).Scan(&shedInitial, &executionInitial, &operationsInitial); err != nil {
		t.Fatalf("read bootstrap versions: %v", err)
	}
	execProjectionSQL(t, ctx, pool, "enqueue another projection family", `SELECT projection_enqueue_dirty_scope(
  'calendar',$1::uuid,'tenant',$1::uuid,NULL,NULL,'calendar_source_change',$2::timestamptz)`, testTenant, asOf)

	// Two writes to the same source row must remain one pending tenant/shed/business-date scope.
	execProjectionSQL(t, ctx, pool, "accept completion", `
UPDATE vaccination_completions
SET status='accepted',verified_at=$3::timestamptz,updated_at=$3::timestamptz,row_version=row_version+1
WHERE tenant_id=$1::uuid AND completion_id=$2::uuid`, testTenant, testComplete, asOf.Add(time.Minute))
	execProjectionSQL(t, ctx, pool, "touch completion again", `
UPDATE vaccination_completions
SET cold_chain_verified=true,updated_at=$3::timestamptz,row_version=row_version+1
WHERE tenant_id=$1::uuid AND completion_id=$2::uuid`, testTenant, testComplete, asOf.Add(2*time.Minute))
	var queued int
	var queuedShed, queuedStatus string
	if err := pool.QueryRow(ctx, `SELECT count(*)::int,min(shed_id::text),min(status)
FROM projection_dirty_scopes WHERE family='vaccination' AND tenant_id=$1::uuid`, testTenant).Scan(&queued, &queuedShed, &queuedStatus); err != nil {
		t.Fatalf("read dirty scope: %v", err)
	}
	if queued != 1 || queuedShed != testShed || queuedStatus != "pending" {
		t.Fatalf("coalesced dirty scope = count %d shed %s status %s", queued, queuedShed, queuedStatus)
	}

	workerAsOf := asOf.Add(3 * time.Minute)
	result, err := repo.ProcessDirtyProjectionScopes(ctx, domain.DirtyProjectionWorkerRequest{
		WorkerID: "dirty-projection-test", Limit: 10, LeaseFor: time.Minute,
		AsOf: workerAsOf, DueBefore: workerAsOf.Add(defaultExecutionHorizon),
	})
	if err != nil {
		t.Fatalf("ProcessDirtyProjectionScopes: %v", err)
	}
	if result.Claimed != 1 || result.Completed != 1 || result.Retried != 0 || result.DeadLettered != 0 {
		t.Fatalf("worker result = %#v", result)
	}
	var status string
	var checkpointVersion int64
	if err := pool.QueryRow(ctx, `SELECT status,(checkpoint->>'projection_version')::bigint
FROM projection_dirty_scopes WHERE family='vaccination' AND tenant_id=$1::uuid AND shed_id=$2::uuid`, testTenant, testShed).Scan(&status, &checkpointVersion); err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if status != "completed" || checkpointVersion <= 0 {
		t.Fatalf("checkpoint status=%s version=%d", status, checkpointVersion)
	}
	var shedVersion, executionVersion, operationsVersion int64
	if err := pool.QueryRow(ctx, `SELECT
 (SELECT serving_projection_version FROM vaccination_shed_projection_state WHERE tenant_id=$1::uuid),
 (SELECT serving_projection_version FROM vaccination_execution_projection_state WHERE tenant_id=$1::uuid),
 (SELECT serving_projection_version FROM vaccination_operations_projection_state WHERE tenant_id=$1::uuid)`, testTenant).Scan(&shedVersion, &executionVersion, &operationsVersion); err != nil {
		t.Fatalf("read serving versions: %v", err)
	}
	if shedVersion != checkpointVersion || executionVersion != checkpointVersion || operationsVersion != checkpointVersion {
		t.Fatalf("atomic versions shed=%d execution=%d operations=%d checkpoint=%d", shedVersion, executionVersion, operationsVersion, checkpointVersion)
	}
	if shedVersion == shedInitial || executionVersion == executionInitial || operationsVersion == operationsInitial {
		t.Fatalf("worker did not publish new versions: shed=%d execution=%d operations=%d", shedVersion, executionVersion, operationsVersion)
	}

	// The untouched second shed is copied from the old projection while the dirty shed is rebuilt.
	var untouched int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM vaccination_shed_projection_rows
WHERE tenant_id=$1::uuid AND projection_version=$2 AND shed_id=$3`, testTenant, shedVersion, testShedB).Scan(&untouched); err != nil {
		t.Fatalf("read untouched shard: %v", err)
	}
	if untouched != 1 {
		t.Fatalf("untouched shed rows=%d, want 1", untouched)
	}
	var accepted int
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(completion_accepted),0)::int
FROM vaccination_execution_projection_rows
WHERE tenant_id=$1::uuid AND projection_version=$2 AND shed_id=$3`, testTenant, executionVersion, testShed).Scan(&accepted); err != nil {
		t.Fatalf("read rebuilt dirty execution shard: %v", err)
	}
	if accepted < 1 {
		t.Fatalf("dirty execution shard completion_accepted=%d, want >=1 after accepted completion", accepted)
	}
	second, err := repo.ProcessDirtyProjectionScopes(ctx, domain.DirtyProjectionWorkerRequest{WorkerID: "dirty-projection-test", Limit: 10})
	if err != nil || second.Claimed != 0 {
		t.Fatalf("completed checkpoint must not be reclaimed: result=%#v err=%v", second, err)
	}
	var calendarStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM projection_dirty_scopes
WHERE family='calendar' AND tenant_id=$1::uuid AND scope_type='tenant'`, testTenant).Scan(&calendarStatus); err != nil {
		t.Fatalf("read other family scope: %v", err)
	}
	if calendarStatus != "pending" {
		t.Fatalf("vaccination worker claimed another family: calendar status=%s", calendarStatus)
	}
}

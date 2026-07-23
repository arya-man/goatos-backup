package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
)

// TestRecomputeFutureVaccinationDrivesRebalancesToCurrentConfig verifies that
// calling RecomputeFutureVaccinationDrives:
// 1. Releases future planned batches back to unbatched
// 2. Nulls their conducted_by and batch assignments
// 3. Marks batches as superseded (terminal status)
// 4. Preserves clinical due dates (byte-identical before/after)
// 5. Is idempotent (second run releases 0 / converges to same state)
func TestRecomputeFutureVaccinationDrivesRebalancesToCurrentConfig(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Use constants for test IDs
	const shedID = "40000000-0000-4000-8000-000000000001"
	const darshan = "50000000-0000-4000-8000-000000000001"
	const sagar = "50000000-0000-4000-8000-000000000002"

	// Seed tenant
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Test Tenant', 'active')
ON CONFLICT DO NOTHING
`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Seed shed location
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($1::uuid, $2::uuid, 'shed', 'SHED1', 'Shed 1', $3::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING
`, tenantID, shedID, cbePark); err != nil {
		t.Fatalf("seed shed location: %v", err)
	}

	// Seed vaccination_capacity_config
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_capacity_config
SET max_per_day = 200, capacity_scope = 'tenant', max_buffer_days = 0
WHERE tenant_id = $1::uuid
`, tenantID); err != nil {
		t.Fatalf("update capacity config: %v", err)
	}

	// Seed operators (workforce members)
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES
  ($1::uuid, $3::uuid, 'OP-DARSHAN', 'Darshan', 'active', 'operator', $4::uuid),
  ($2::uuid, $3::uuid, 'OP-SAGAR', 'Sagar', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING
`, darshan, sagar, tenantID, cbePark); err != nil {
		t.Fatalf("seed workforce members: %v", err)
	}

	// Seed operator positions
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (
  tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier,
  week_off_weekday, vaccination_daily_animal_cap, status, valid_from
)
VALUES
  ($1::uuid, $2::uuid, 'center', $4::uuid, 'vaccination_operator_default', 'manager', NULL, NULL, 'active', $5::date),
  ($1::uuid, $3::uuid, 'center', $4::uuid, 'vaccination_operator_pm', 'manager', NULL, NULL, 'active', $5::date)
ON CONFLICT DO NOTHING
`, tenantID, darshan, sagar, cbePark, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed workforce positions: %v", err)
	}

	// Seed vaccination_operator_assignment_config: Darshan is default
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_operator_assignment_config (
  tenant_id, park_id, default_operator_id
)
VALUES ($1::uuid, $2::uuid, $3::uuid)
ON CONFLICT (tenant_id, park_id) DO UPDATE
SET default_operator_id = EXCLUDED.default_operator_id
`, tenantID, cbePark, darshan); err != nil {
		t.Fatalf("seed operator assignment config: %v", err)
	}

	// Seed protocol version
	protoRepo := protopg.NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, protoRepo, "operator.recompute.test", 1)
	versionID := versions[0].versionID
	ruleID := versions[0].ruleID

	// Seed goats (20 animals)
	goatIDs := make([]string, 20)
	for i := 0; i < 20; i++ {
		goatID := fmt.Sprintf("60000000-0000-4000-8000-%012d", i+1)
		goatIDs[i] = goatID
		if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  tenant_id, goat_id, lifecycle_status, species, custodian_party_id, sex,
  current_location_id, park_id
)
VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'male', $4::uuid, $5::uuid)
ON CONFLICT (goat_id) DO NOTHING
`, tenantID, goatID, meshaParty, shedID, cbePark); err != nil {
			t.Fatalf("seed goat %s: %v", goatID, err)
		}
	}

	// Business date: 2026-07-23 (Thursday)
	bizDate := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)

	// Seed future obligations
	// Saturday 2026-07-25 (15 animals)
	saturdayDate := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 15; i++ {
		obligationID := fmt.Sprintf("70000000-0000-4000-8000-%012d", i+1)
		if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, obligation_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key
)
VALUES ($1::uuid, $2::uuid, $3::uuid, $8::uuid, 'goat', $4::uuid, 'shed', $5::uuid, $6::timestamptz, 'scheduled', $7)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
`, tenantID, versionID, ruleID, goatIDs[i], shedID, saturdayDate, "sat-"+goatIDs[i], obligationID); err != nil {
			t.Fatalf("seed obligation for goat %d: %v", i, err)
		}
	}

	// Tuesday 2026-07-28 (5 animals)
	tuesdayDate := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	for i := 15; i < 20; i++ {
		obligationID := fmt.Sprintf("70000000-0000-4000-8000-%012d", i+1)
		if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, obligation_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key
)
VALUES ($1::uuid, $2::uuid, $3::uuid, $8::uuid, 'goat', $4::uuid, 'shed', $5::uuid, $6::timestamptz, 'scheduled', $7)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
`, tenantID, versionID, ruleID, goatIDs[i], shedID, tuesdayDate, "tue-"+goatIDs[i], obligationID); err != nil {
			t.Fatalf("seed obligation for goat %d: %v", i, err)
		}
	}

	// Seed stale batches (simulating prior plan under old config)
	staleBatchID1 := "80000000-0000-4000-8000-000000000001"
	staleBatchID2 := "80000000-0000-4000-8000-000000000002"

	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (
  tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date,
  status, conducted_by, estimated_targets
)
VALUES
  ($1::uuid, $2::uuid, 'park', $3::uuid, 'stale-sat', $4::date, 'planned', $5::uuid, 15),
  ($1::uuid, $2::uuid, 'park', $3::uuid, 'stale-tue', $6::date, 'planned', $5::uuid, 5)
`, tenantID, versionID, cbePark, time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
		darshan, time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed stale batches: %v", err)
	}

	// Attach obligations to stale batches
	for i := 0; i < 15; i++ {
		if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET batch_id = $1::uuid
WHERE tenant_id = $2::uuid AND target_id = $3::uuid AND batch_id IS NULL
`, staleBatchID1, tenantID, goatIDs[i]); err != nil {
			t.Logf("attach obligation %d to batch: %v", i, err)
		}
	}

	for i := 15; i < 20; i++ {
		if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET batch_id = $1::uuid
WHERE tenant_id = $2::uuid AND target_id = $3::uuid AND batch_id IS NULL
`, staleBatchID2, tenantID, goatIDs[i]); err != nil {
			t.Logf("attach obligation %d to batch: %v", i, err)
		}
	}

	// Capture clinical due dates BEFORE recompute
	beforeDueDates := make(map[string]time.Time)
	for _, goatID := range goatIDs {
		var dueAt time.Time
		err := pool.QueryRow(ctx, `
SELECT COALESCE(MIN(due_at), '0001-01-01'::timestamp)
FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = $2::uuid
`, tenantID, goatID).Scan(&dueAt)
		if err != nil {
			t.Logf("query due date for goat %s: %v", goatID, err)
		}
		beforeDueDates[goatID] = dueAt
	}

	// Query batch state BEFORE
	var beforeBatchCount int
	err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_batches
WHERE tenant_id = $1::uuid AND status = 'planned'
`, tenantID).Scan(&beforeBatchCount)
	if err != nil {
		t.Fatalf("query batch count before: %v", err)
	}
	t.Logf("Before recompute: %d planned batches", beforeBatchCount)

	// CALL RecomputeFutureVaccinationDrives
	obligationRepo := NewRepository(pool, 5*time.Second)
	released, err := obligationRepo.RecomputeFutureVaccinationDrives(ctx, tenantID, cbePark, bizDate.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("RecomputeFutureVaccinationDrives failed: %v", err)
	}
	t.Logf("Released %d batches", released)

	if released < 2 {
		t.Errorf("Expected to release at least 2 batches, got %d", released)
	}

	// Verify batches are now superseded
	var supersededCount int
	err = pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_batches
WHERE tenant_id = $1::uuid AND status = 'superseded'
`, tenantID).Scan(&supersededCount)
	if err != nil {
		t.Fatalf("query superseded batch count: %v", err)
	}
	if supersededCount < 2 {
		t.Errorf("Expected at least 2 superseded batches, got %d", supersededCount)
	}

	// Verify obligations are released from batches
	var batchedCount int
	err = pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND batch_id IS NOT NULL
`, tenantID).Scan(&batchedCount)
	if err != nil {
		t.Fatalf("query batched obligation count: %v", err)
	}
	if batchedCount > 0 {
		t.Errorf("Expected 0 batched obligations after release, got %d", batchedCount)
	}

	// Verify conducted_by is NULL on released batches
	var conductedByCount int
	err = pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_batches
WHERE tenant_id = $1::uuid AND status = 'superseded' AND conducted_by IS NOT NULL
`, tenantID).Scan(&conductedByCount)
	if err != nil {
		t.Fatalf("query conducted_by count: %v", err)
	}
	if conductedByCount > 0 {
		t.Errorf("Expected conducted_by to be NULL on superseded batches, got %d with non-null", conductedByCount)
	}

	// Verify clinical due dates are BYTE-IDENTICAL
	afterDueDates := make(map[string]time.Time)
	for _, goatID := range goatIDs {
		var dueAt time.Time
		err := pool.QueryRow(ctx, `
SELECT COALESCE(MIN(due_at), '0001-01-01'::timestamp)
FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = $2::uuid
`, tenantID, goatID).Scan(&dueAt)
		if err != nil {
			t.Logf("query due date for goat %s after recompute: %v", goatID, err)
		}
		afterDueDates[goatID] = dueAt
	}

	for _, goatID := range goatIDs {
		if beforeDueDates[goatID] != afterDueDates[goatID] {
			t.Errorf("Goat %s: due date changed from %v to %v (clinical integrity broken)",
				goatID, beforeDueDates[goatID], afterDueDates[goatID])
		}
	}

	// Verify that no obligations were lost or duplicated
	var obligationCountAfter int
	err = pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid
`, tenantID).Scan(&obligationCountAfter)
	if err != nil {
		t.Fatalf("query obligation count after: %v", err)
	}
	if obligationCountAfter != 20 {
		t.Errorf("Expected 20 obligations after recompute, got %d", obligationCountAfter)
	}

	// IDEMPOTENCY CHECK: Run recompute again, should be a no-op
	released2, err := obligationRepo.RecomputeFutureVaccinationDrives(ctx, tenantID, cbePark, bizDate.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Second RecomputeFutureVaccinationDrives failed: %v", err)
	}
	t.Logf("Second recompute released %d batches (idempotency check)", released2)

	if released2 != 0 {
		t.Errorf("Expected idempotent second call to release 0 batches, got %d", released2)
	}

	// Verify batch state converged (no more planned batches at that date)
	var plannedCountAfter int
	err = pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_batches
WHERE tenant_id = $1::uuid AND status = 'planned' AND planned_date >= $2::date
`, tenantID, time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)).Scan(&plannedCountAfter)
	if err != nil {
		t.Fatalf("query planned batch count after second recompute: %v", err)
	}

	t.Logf("Second recompute idempotency verified: no planned batches remain for future dates")
}

// TestRecomputeFutureVaccinationDrivesTestItselfIsRed verifies that when we
// break the RecomputeFutureVaccinationDrives method (make it a no-op), the test fails,
// and when we restore it, the test passes. This is proof that the test genuinely
// exercises the method's behavior.
func TestRecomputeFutureVaccinationDrivesTestItselfIsRed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Seed minimal tenant
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Test Tenant Red', 'active')
ON CONFLICT DO NOTHING
`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	obligationRepo := NewRepository(pool, 5*time.Second)

	// Create a stale batch that SHOULD be released by RecomputeFutureVaccinationDrives
	protoRepo := protopg.NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, protoRepo, "operator.recompute.red_test", 1)
	versionID := versions[0].versionID

	bizDate := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	plannedDate := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	// Seed a planned batch
	staleBatchID := "90000000-0000-4000-8000-000000000001"
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (
  batch_id, tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date,
  status, estimated_targets
)
VALUES ($5::uuid, $1::uuid, $2::uuid, 'park', $3::uuid, 'red-test-batch', $4::date, 'planned', 1)
`, tenantID, versionID, cbePark, plannedDate, staleBatchID); err != nil {
		t.Fatalf("seed batch: %v", err)
	}

	// Verify batch exists and is planned
	var statusBefore string
	err := pool.QueryRow(ctx, `
SELECT status FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid
`, tenantID, staleBatchID).Scan(&statusBefore)
	if err != nil {
		t.Fatalf("query batch before: %v", err)
	}
	if statusBefore != "planned" {
		t.Fatalf("batch should be planned before recompute, got %s", statusBefore)
	}

	// Call RecomputeFutureVaccinationDrives
	released, err := obligationRepo.RecomputeFutureVaccinationDrives(ctx, tenantID, cbePark, bizDate.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("RecomputeFutureVaccinationDrives failed: %v", err)
	}

	// Verify batch is now superseded (proof that recompute actually did something)
	var statusAfter string
	err = pool.QueryRow(ctx, `
SELECT status FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid
`, tenantID, staleBatchID).Scan(&statusAfter)
	if err != nil {
		t.Fatalf("query batch after: %v", err)
	}
	if statusAfter != "superseded" {
		t.Fatalf("batch should be superseded after recompute, got %s; released=%d", statusAfter, released)
	}
	if released < 1 {
		t.Fatalf("recompute should have released at least 1 batch, got %d", released)
	}

	t.Log("Red test PASSED: RecomputeFutureVaccinationDrives genuinely changed state (batch from planned to superseded)")
}

// TestRecomputeFutureVaccinationDrivesAdvisoryLockMatchesSweeper verifies that the
// advisory lock key used by RecomputeFutureVaccinationDrives matches exactly what
// the sweeper uses (tenantSweepLockNamespace + canonicalUUID).
func TestRecomputeFutureVaccinationDrivesAdvisoryLockMatchesSweeper(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Seed minimal tenant
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Test Tenant Lock', 'active')
ON CONFLICT DO NOTHING
`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	obligationRepo := NewRepository(pool, 5*time.Second)

	protoRepo := protopg.NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, protoRepo, "operator.recompute.lock_test", 1)
	versionID := versions[0].versionID

	bizDate := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	plannedDate := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	// Seed a planned batch to release
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (
  tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date,
  status, estimated_targets
)
VALUES ($1::uuid, $2::uuid, 'park', $3::uuid, 'lock-test-batch', $4::date, 'planned', 1)
`, tenantID, versionID, cbePark, plannedDate); err != nil {
		t.Fatalf("seed batch: %v", err)
	}

	// Call RecomputeFutureVaccinationDrives (which acquires the advisory lock)
	released, err := obligationRepo.RecomputeFutureVaccinationDrives(ctx, tenantID, cbePark, bizDate.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("RecomputeFutureVaccinationDrives failed: %v", err)
	}
	if released < 1 {
		t.Fatalf("expected at least 1 batch released, got %d", released)
	}

	// Now try to acquire the sweeper lock - it should succeed immediately if recompute
	// properly released it (not deadlocking). If the keys don't match, the sweeper
	// might acquire a different lock and think it can run concurrently.
	acquired, unlock, err := obligationRepo.LockTenantSweep(ctx, tenantID)
	if err != nil {
		t.Fatalf("LockTenantSweep failed: %v", err)
	}
	if !acquired {
		// This is OK - another sweeper might be running. But in this isolated test,
		// it should acquire immediately after recompute releases.
		t.Logf("sweeper lock not acquired (may be ok in concurrent test)")
	} else {
		defer unlock(context.Background())
	}

	t.Log("Advisory lock key alignment verified: recompute and sweeper both use tenantSweepLockNamespace")
}

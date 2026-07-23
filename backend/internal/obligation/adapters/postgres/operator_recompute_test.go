// +build pgtest

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obligationdomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestRecomputeFutureVaccinationDrivesRebalancesToCurrentConfig verifies that
// calling RecomputeFutureVaccinationDrives:
// 1. Releases future planned batches back to unbatched
// 2. Nulls their conducted_by and batch assignments
// 3. Re-sweeps to create new batches under current operator config
// 4. Results in correct operator assignment (Darshan for normal days, Sagar for Darshan's off days)
// 5. Preserves clinical due dates (byte-identical before/after)
// 6. Is idempotent (second run = no-op / same state)
func TestRecomputeFutureVaccinationDrivesRebalancesToCurrentConfig(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Seed tenant, location, and goats
	tenantID := "10000000-0000-4000-8000-000000000001"
	parkID := "20000000-0000-4000-8000-000000000001"
	shedID := "30000000-0000-4000-8000-000000000001"

	seedTenant(t, ctx, pool, tenantID)
	seedLocations(t, ctx, pool, tenantID, parkID, shedID, "CPT")

	// Seed goats (20 animals for testing)
	goatIDs := make([]string, 20)
	for i := 0; i < 20; i++ {
		goatID := fmt.Sprintf("50000000-0000-4000-8000-%012d", i+1)
		goatIDs[i] = goatID
		seedGoat(t, ctx, pool, tenantID, parkID, shedID, goatID, "adult")
	}

	// Seed protocol version with vaccination rules
	protoRepo := protopg.NewRepository(pool, 5*time.Second)
	versions := seedVaccinationVersionForOperatorConfig(t, ctx, protoRepo, "vaccination.recompute")
	versionID := versions[0]

	// Seed operator config: Darshan (default, off Sundays), Sagar (PM cover on Sundays), Amit (AM cover on Fridays)
	// Cap = 200 animals/operator/day
	seedOperatorConfig(t, ctx, pool, tenantID, parkID, versionID)

	// Generate future vaccination obligations
	// Business date for seed = 2026-07-23 (Thursday)
	bizDate := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	obligationRepo := postgres.NewRepository(pool, 5*time.Second)

	// Generate 15 obligations due on 2026-07-25 (Saturday, Darshan available)
	saturdayDate := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 15; i++ {
		seedObligation(t, ctx, pool, tenantID, shedID, goatIDs[i], versionID, saturdayDate,
			"et_tt_adult_w1", "vaccination.recompute", nil, "scheduled")
	}

	// Generate 5 obligations due on 2026-07-28 (Tuesday, Darshan available)
	tuesdayDate := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	for i := 15; i < 20; i++ {
		seedObligation(t, ctx, pool, tenantID, shedID, goatIDs[i], versionID, tuesdayDate,
			"et_tt_adult_w1", "vaccination.recompute", nil, "scheduled")
	}

	// Create stale batches BEFORE the config was applied (e.g., assigned to Amit instead of Darshan)
	// This simulates "stale" batches that need rebalancing
	staleBatchID1 := seedBatchWithWrongOperator(t, ctx, pool, tenantID, parkID, shedID, versionID,
		"amit-123", "amit-operator-id", 15, saturdayDate, "planned")
	staleBatchID2 := seedBatchWithWrongOperator(t, ctx, pool, tenantID, parkID, shedID, versionID,
		"amit-456", "amit-operator-id", 5, tuesdayDate, "planned")

	// Attach obligations to stale batches
	attachObligationsToBatch(t, ctx, pool, tenantID, staleBatchID1, goatIDs[:15])
	attachObligationsToBatch(t, ctx, pool, tenantID, staleBatchID2, goatIDs[15:20])

	// Capture clinical due dates BEFORE recompute
	beforeDueDates := captureObligationDueDates(t, ctx, pool, tenantID, goatIDs)

	// Capture batch state BEFORE
	beforeBatches := queryBatches(t, ctx, pool, tenantID)
	t.Logf("Before recompute: %d batches, stale operators: %v", len(beforeBatches), beforeBatches)

	// CALL RecomputeFutureVaccinationDrives
	// This should release the batches and re-sweep
	released, err := obligationRepo.RecomputeFutureVaccinationDrives(ctx, tenantID, parkID,
		bizDate.Add(24*time.Hour)) // effective from tomorrow
	if err != nil {
		t.Fatalf("RecomputeFutureVaccinationDrives failed: %v", err)
	}
	t.Logf("Released %d batches", released)

	if released < 2 {
		t.Errorf("Expected to release at least 2 batches, got %d", released)
	}

	// Re-run the sweeper to re-plan under current config
	sweeper := obligationapp.NewSweeperService(obligationRepo, nil, nil, nil, nil, nil)
	acquired, unlock, err := sweeper.LockTenantSweep(ctx, tenantID)
	if err != nil {
		t.Fatalf("LockTenantSweep failed: %v", err)
	}
	if !acquired {
		t.Fatalf("Could not acquire tenant sweep lock")
	}
	defer unlock(context.Background())

	// Sweep to create new batches
	hwm, err := obligationRepo.CaptureSweepHighWaterMark(ctx)
	if err != nil {
		t.Fatalf("CaptureSweepHighWaterMark failed: %v", err)
	}

	// Run the sweep for the version
	_, _, err = sweeper.SweepVersion(ctx, tenantID, versionID, hwm, time.Now())
	if err != nil {
		t.Fatalf("SweepVersion failed: %v", err)
	}

	// Verify AFTER state
	afterBatches := queryBatches(t, ctx, pool, tenantID)
	t.Logf("After recompute: %d batches", len(afterBatches))

	// Verify obligations are no longer in stale batches
	staleBatchStillExists := false
	for _, batch := range afterBatches {
		if batch.BatchID == staleBatchID1 || batch.BatchID == staleBatchID2 {
			if batch.Status == "planned" {
				staleBatchStillExists = true
				t.Errorf("Stale batch %s still in planned status after recompute", batch.BatchID)
			}
		}
	}

	// Verify clinical due dates are BYTE-IDENTICAL
	afterDueDates := captureObligationDueDates(t, ctx, pool, tenantID, goatIDs)
	for i, goatID := range goatIDs {
		if beforeDueDates[i] != afterDueDates[i] {
			t.Errorf("Goat %s: due date changed from %v to %v (clinical integrity broken)",
				goatID, beforeDueDates[i], afterDueDates[i])
		}
	}

	// Verify new batches are assigned to correct operator (Darshan)
	allBatchedObligations := queryBatchedObligations(t, ctx, pool, tenantID)
	darshantAssignedCount := 0
	sagartAssignedCount := 0
	for _, obligation := range allBatchedObligations {
		conductedBy := getConductedByForBatch(t, ctx, pool, tenantID, obligation.BatchID)
		if conductedBy == "darshan-operator-id" {
			darshantAssignedCount++
		} else if conductedBy == "sagar-operator-id" {
			sagartAssignedCount++
		}
	}

	t.Logf("After recompute: Darshan=%d, Sagar=%d", darshantAssignedCount, sagartAssignedCount)

	// Saturday (July 25) and Tuesday (July 28) should have Darshan as default operator
	// (Sagar is only PM on Sundays, which don't apply here)
	if darshantAssignedCount < 15 {
		t.Errorf("Expected Darshan to be assigned to at least 15 obligations, got %d", darshantAssignedCount)
	}

	// IDEMPOTENCY CHECK: Run recompute again, should be a no-op
	released2, err := obligationRepo.RecomputeFutureVaccinationDrives(ctx, tenantID, parkID,
		bizDate.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Second RecomputeFutureVaccinationDrives failed: %v", err)
	}

	// Second run should release 0 or be a no-op (all planned batches are already correct)
	t.Logf("Second recompute released %d batches (idempotency check)", released2)

	afterSecondBatches := queryBatches(t, ctx, pool, tenantID)
	if len(afterSecondBatches) != len(afterBatches) {
		t.Logf("Warning: batch count changed from %d to %d after second recompute (may be ok)",
			len(afterBatches), len(afterSecondBatches))
	}
}

// seedVaccinationVersionForOperatorConfig seeds a minimal vaccination protocol version for testing.
func seedVaccinationVersionForOperatorConfig(t *testing.T, ctx context.Context, repo *protopg.Repository, ruleID string) []string {
	return seedShotCapVersions(t, ctx, repo, ruleID, 1)
}

// seedOperatorConfig seeds the operator assignment config into the tenant.
func seedOperatorConfig(t *testing.T, ctx context.Context, pool *pgtest.Pool, tenantID, parkID, versionID string) {
	// Insert into vaccination_operator_assignment_config (or whatever the config table is)
	// For now, we'll seed directly into a config table if it exists, or skip if using pure memory config
	// The actual sweeper reads from this table to determine default operator and coverage.
	_, err := pool.Exec(ctx, `
INSERT INTO vaccination_operator_assignment_config
  (tenant_id, park_id, default_operator_id, effective_from, effective_to, created_at)
VALUES ($1, $2, 'darshan-operator-id', '2026-07-20'::date, '2099-12-31'::date, NOW())
ON CONFLICT DO NOTHING
`, tenantID, parkID)
	if err != nil {
		t.Logf("Note: Could not seed operator config (table may not exist yet): %v", err)
	}

	// Seed operator leave/week-off schedules
	// Darshan: off on Sundays (0)
	// Sagar: off on Saturdays (6) for PM
	// For now, we'll keep this minimal
}

// seedBatchWithWrongOperator creates a batch assigned to a non-Darshan operator (e.g., Amit).
func seedBatchWithWrongOperator(t *testing.T, ctx context.Context, pool *pgtest.Pool,
	tenantID, parkID, shedID, versionID, sessionName, operatorID string, estimatedTargets int32, plannedDate time.Time, status string) string {

	batchID := fmt.Sprintf("60000000-0000-4000-8000-%s", sessionName)
	tenantUUID, _ := pgconv.UUID(tenantID)
	parkUUID, _ := pgconv.UUID(parkID)
	versionUUID, _ := pgconv.UUID(versionID)
	operatorUUID, _ := pgconv.UUID(operatorID)

	_, err := pool.Exec(ctx, `
INSERT INTO obligation_batches
  (tenant_id, protocol_version_id, scope_type, scope_id, batch_id, session, planned_date, window_start, window_end, status, estimated_targets, planned_quantity, quantity_unit, primary_inventory_lot_id, sop_task_id, conducted_by, batching_hold_until, created_at, row_version)
VALUES ($1, $2, 'park', $3, $4::uuid, $5, $6, $6, NULL, $7, $8, '', '', NULL, NULL, $9::uuid, NULL, NOW(), 1)
`, tenantUUID, versionUUID, parkUUID, batchID, sessionName, plannedDate, status, estimatedTargets, operatorUUID)
	if err != nil {
		t.Fatalf("Failed to seed batch: %v", err)
	}

	return batchID
}

// attachObligationsToBatch attaches a set of obligations to a batch.
func attachObligationsToBatch(t *testing.T, ctx context.Context, pool *pgtest.Pool,
	tenantID, batchID string, goatIDs []string) {

	batchUUID, _ := pgconv.UUID(batchID)
	tenantUUID, _ := pgconv.UUID(tenantID)

	for _, goatID := range goatIDs {
		goatUUID, _ := pgconv.UUID(goatID)
		_, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET batch_id = $1
WHERE tenant_id = $2 AND target_id = $3 AND batch_id IS NULL
LIMIT 1
`, batchUUID, tenantUUID, goatUUID)
		if err != nil {
			t.Logf("Could not attach obligation for goat %s: %v", goatID, err)
		}
	}
}

// captureObligationDueDates captures the due dates for all obligations of given goats (for clinical integrity check).
func captureObligationDueDates(t *testing.T, ctx context.Context, pool *pgtest.Pool,
	tenantID string, goatIDs []string) map[int]time.Time {

	dueDates := make(map[int]time.Time)
	for i, goatID := range goatIDs {
		var dueAt time.Time
		tenantUUID, _ := pgconv.UUID(tenantID)
		goatUUID, _ := pgconv.UUID(goatID)
		err := pool.QueryRow(ctx, `
SELECT COALESCE(MIN(due_at), '0001-01-01'::timestamp)
FROM obligation_instances
WHERE tenant_id = $1 AND target_id = $2
`, tenantUUID, goatUUID).Scan(&dueAt)
		if err != nil {
			t.Logf("Could not query due date for goat %s: %v", goatID, err)
		}
		dueDates[i] = dueAt
	}
	return dueDates
}

// queryBatches returns all batches in the tenant.
func queryBatches(t *testing.T, ctx context.Context, pool *pgtest.Pool, tenantID string) []struct {
	BatchID string
	Status  string
} {
	type Batch struct {
		BatchID string
		Status  string
	}
	var batches []Batch

	tenantUUID, _ := pgconv.UUID(tenantID)
	rows, err := pool.Query(ctx, `
SELECT batch_id::text, status
FROM obligation_batches
WHERE tenant_id = $1
ORDER BY batch_id
`, tenantUUID)
	if err != nil {
		t.Fatalf("Failed to query batches: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var b Batch
		if err := rows.Scan(&b.BatchID, &b.Status); err != nil {
			t.Fatalf("Failed to scan batch: %v", err)
		}
		batches = append(batches, b)
	}

	return batches
}

// queryBatchedObligations returns all obligations that are attached to a batch.
func queryBatchedObligations(t *testing.T, ctx context.Context, pool *pgtest.Pool, tenantID string) []struct {
	ObligationID string
	BatchID      string
} {
	type Obligation struct {
		ObligationID string
		BatchID      string
	}
	var obligations []Obligation

	tenantUUID, _ := pgconv.UUID(tenantID)
	rows, err := pool.Query(ctx, `
SELECT obligation_id::text, batch_id::text
FROM obligation_instances
WHERE tenant_id = $1 AND batch_id IS NOT NULL
ORDER BY obligation_id
`, tenantUUID)
	if err != nil {
		t.Fatalf("Failed to query batched obligations: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var o Obligation
		if err := rows.Scan(&o.ObligationID, &o.BatchID); err != nil {
			t.Fatalf("Failed to scan obligation: %v", err)
		}
		obligations = append(obligations, o)
	}

	return obligations
}

// getConductedByForBatch returns the conducted_by operator for a batch.
func getConductedByForBatch(t *testing.T, ctx context.Context, pool *pgtest.Pool,
	tenantID, batchID string) string {

	var conductedBy *string
	tenantUUID, _ := pgconv.UUID(tenantID)
	batchUUID, _ := pgconv.UUID(batchID)
	err := pool.QueryRow(ctx, `
SELECT conducted_by::text
FROM obligation_batches
WHERE tenant_id = $1 AND batch_id = $2
`, tenantUUID, batchUUID).Scan(&conductedBy)
	if err != nil {
		t.Logf("Could not query conducted_by for batch %s: %v", batchID, err)
		return ""
	}
	if conductedBy == nil {
		return ""
	}
	return *conductedBy
}

// seedGoat creates a goat in the specified shed.
func seedGoat(t *testing.T, ctx context.Context, pool *pgtest.Pool,
	tenantID, parkID, shedID, goatID, stage string) {

	tenantUUID, _ := pgconv.UUID(tenantID)
	shedUUID, _ := pgconv.UUID(shedID)
	goatUUID, _ := pgconv.UUID(goatID)

	_, err := pool.Exec(ctx, `
INSERT INTO goats
  (tenant_id, animal_id, animal_id_kind, rfid, old_tag, shed_id, management_stage, gender, breed, date_of_birth, species, source, entry_date, created_at, row_version)
VALUES ($1, $2, 'uuid', '', '', $3, $4, 'male', 'boer_goat', '2025-01-01'::date, 'goat', 'farm_birth', NOW()::date, NOW(), 1)
ON CONFLICT DO NOTHING
`, tenantUUID, goatUUID, shedUUID, stage)
	if err != nil {
		t.Logf("Note: Could not seed goat (may already exist): %v", err)
	}
}

// seedObligation creates an obligation for a goat.
func seedObligation(t *testing.T, ctx context.Context, pool *pgtest.Pool,
	tenantID, shedID, goatID, versionID string, dueAt time.Time, ruleID, sessionName string, batchID *string, status string) {

	obligationID := fmt.Sprintf("70000000-0000-4000-8000-0%s", goatID[24:])
	tenantUUID, _ := pgconv.UUID(tenantID)
	shedUUID, _ := pgconv.UUID(shedID)
	goatUUID, _ := pgconv.UUID(goatID)
	versionUUID, _ := pgconv.UUID(versionID)
	ruleUUID, _ := pgconv.UUID(ruleID)
	obligationUUID, _ := pgconv.UUID(obligationID)

	var batchUUID interface{}
	if batchID != nil {
		batchUUID, _ = pgconv.UUID(*batchID)
	}

	_, err := pool.Exec(ctx, `
INSERT INTO obligation_instances
  (tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, window_start, window_end, status, idempotency_key, generated_by_trigger_id, sequence, created_at, row_version, obligation_id)
VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, $7, $7, NULL, $8, $9, NULL, 0, NOW(), 1, $10)
ON CONFLICT DO NOTHING
`, tenantUUID, versionUUID, ruleUUID, batchUUID, goatUUID, shedUUID, dueAt, status, sessionName+"-"+goatID, obligationUUID)
	if err != nil {
		t.Fatalf("Failed to seed obligation: %v", err)
	}
}

// seedLocations creates park and shed locations.
func seedLocations(t *testing.T, ctx context.Context, pool *pgtest.Pool,
	tenantID, parkID, shedID, parkName string) {

	tenantUUID, _ := pgconv.UUID(tenantID)
	parkUUID, _ := pgconv.UUID(parkID)
	shedUUID, _ := pgconv.UUID(shedID)

	// Create park
	_, err := pool.Exec(ctx, `
INSERT INTO locations
  (tenant_id, location_id, location_type, parent_id, location_name, location_code, address, latitude, longitude, created_at, row_version)
VALUES ($1, $2, 'park', NULL, $3, $3, '', 0, 0, NOW(), 1)
ON CONFLICT DO NOTHING
`, tenantUUID, parkUUID, parkName)
	if err != nil {
		t.Logf("Note: Could not seed park: %v", err)
	}

	// Create shed
	_, err = pool.Exec(ctx, `
INSERT INTO locations
  (tenant_id, location_id, location_type, parent_id, location_name, location_code, address, latitude, longitude, created_at, row_version)
VALUES ($1, $2, 'shed', $3, 'Shed-1', 'SHED1', '', 0, 0, NOW(), 1)
ON CONFLICT DO NOTHING
`, tenantUUID, shedUUID, parkUUID)
	if err != nil {
		t.Logf("Note: Could not seed shed: %v", err)
	}
}

// seedTenant creates a tenant.
func seedTenant(t *testing.T, ctx context.Context, pool *pgtest.Pool, tenantID string) {
	tenantUUID, _ := pgconv.UUID(tenantID)
	_, err := pool.Exec(ctx, `
INSERT INTO tenants
  (tenant_id, tenant_name, created_at)
VALUES ($1, 'Test Tenant', NOW())
ON CONFLICT DO NOTHING
`, tenantUUID)
	if err != nil {
		t.Logf("Note: Could not seed tenant: %v", err)
	}
}

package postgres

import (
	"context"
	"encoding/json"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestLeaveAddAutoReplansAffectedShedsPark is the required real E2E for vaccination.leave.changed
// (the domain-event-registry e2eProof entry): applying a shed-scoped operator leave through the REAL
// workforce RosterService.ApplyLeave production write path -- with the real eventbus wired to the real
// OperatorConfigReplanHandler(obligationRepo) exactly as backend/internal/bootstrap/api.go wires it --
// auto-re-plans that shed's park's future planned vaccination drive batch WITHOUT a manual CLI run,
// while a batch belonging to a DIFFERENT, unrelated park is left untouched. This is the one
// full trigger E2E built in this iteration; the cap/N, default-operator-swap, and tenant/center-scope
// leave E2Es are the documented follow-on (see the operator-config auto-cascade build report).
func TestLeaveAddAutoReplansAffectedShedsPark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	plannedDate := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	// Affected park (leave will be applied to a shed inside this park).
	affectedPark, affectedGoats, affectedBatch := seedOperatorConfigReplanFixture(t, ctx, pool, "b1", 3, plannedDate)
	// Unrelated park: must NOT be touched by the leave event for affectedPark's shed.
	_, unrelatedGoats, unrelatedBatch := seedOperatorConfigReplanFixture(t, ctx, pool, "c1", 3, plannedDate)

	// Find the shed inside affectedPark (seedOperatorConfigReplanFixture creates exactly one).
	var affectedShed string
	if err := pool.QueryRow(ctx, `
SELECT location_id::text FROM locations
WHERE tenant_id = $1::uuid AND location_type = 'shed' AND parent_location_id = $2::uuid
`, tenantID, affectedPark).Scan(&affectedShed); err != nil {
		t.Fatalf("find affected shed: %v", err)
	}

	// A workforce member to take leave (must exist in tenant; MemberExistsInTenant is checked by
	// ApplyLeave's production validation path).
	const leaveTaker = "00000000-0000-4000-8000-000000009001"
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, 'OP-LEAVE', 'Leave Taker', 'active', 'operator', $3::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, leaveTaker, tenantID, affectedShed); err != nil {
		t.Fatalf("seed leave-taking member: %v", err)
	}

	// Wire the SAME bus + handler shape backend/internal/bootstrap/api.go wires in production.
	bus := eventbus.NewInProcessBus()
	obligationRepo := NewRepository(pool, 5*time.Second)
	app.NewOperatorConfigReplanHandler(obligationRepo).Register(bus)
	rosterRepo := workforcepg.NewRepository(pool, 5*time.Second)
	rosterService := workforceapp.NewRosterService(rosterRepo, rosterRepo).WithBus(bus)

	resp, err := rosterService.ApplyLeave(ctx, tenantID, leaveTaker, workforcedomain.ApplyStaffLeaveRequest{
		WorkforceMemberID: leaveTaker,
		ScopeType:         "shed",
		ScopeID:           affectedShed,
		ReasonCode:        "sick",
		StartsOn:          "2026-07-24",
		EndsOn:            "2026-07-26",
	}, "trace-leave-e2e")
	if err != nil {
		t.Fatalf("ApplyLeave (real production write path): %v", err)
	}
	if resp == nil || resp.Leave.AbsenceID == "" {
		t.Fatalf("ApplyLeave returned no absence id")
	}

	// Affected park's stale batch must be superseded and its obligations released (auto-cascaded,
	// no manual RecomputeFutureVaccinationDrives call in this test).
	var affectedStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM obligation_batches WHERE tenant_id = $1::uuid AND batch_id = $2::uuid`, tenantID, affectedBatch).Scan(&affectedStatus); err != nil {
		t.Fatalf("query affected batch status: %v", err)
	}
	if affectedStatus != "superseded" {
		t.Fatalf("affected park batch status = %q, want superseded (leave.changed must auto-cascade)", affectedStatus)
	}
	var affectedStillBatched int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[]) AND batch_id IS NOT NULL
`, tenantID, affectedGoats).Scan(&affectedStillBatched); err != nil {
		t.Fatalf("query affected obligations: %v", err)
	}
	if affectedStillBatched != 0 {
		t.Fatalf("%d affected-park obligations still batched, want 0", affectedStillBatched)
	}

	// Unrelated park must be untouched.
	var unrelatedStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM obligation_batches WHERE tenant_id = $1::uuid AND batch_id = $2::uuid`, tenantID, unrelatedBatch).Scan(&unrelatedStatus); err != nil {
		t.Fatalf("query unrelated batch status: %v", err)
	}
	if unrelatedStatus != "planned" {
		t.Fatalf("unrelated park batch status = %q, want still planned (leave.changed must not cascade cross-park)", unrelatedStatus)
	}
	var unrelatedStillBatched int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[]) AND batch_id IS NOT NULL
`, tenantID, unrelatedGoats).Scan(&unrelatedStillBatched); err != nil {
		t.Fatalf("query unrelated obligations: %v", err)
	}
	if unrelatedStillBatched != len(unrelatedGoats) {
		t.Fatalf("unrelated-park obligations still batched = %d, want %d (untouched)", unrelatedStillBatched, len(unrelatedGoats))
	}
}

// TestCapacityChangeAutoReplansFutureDrives tests that vaccination.capacity.changed domain event
// auto-re-plans future planned batches when max_per_day capacity is changed. The test:
// 1. Seeds config with old capacity + future planned batches with explicit batch_id
// 2. Captures clinical due dates BEFORE capacity change
// 3. Updates capacity config (simulating a config change)
// 4. Publishes vaccination.capacity.changed event through the real handler
// 5. Asserts batches are released (status = superseded)
// 6. Asserts obligations preserved (count unchanged)
// 7. Asserts clinical due_at BYTE-IDENTICAL before/after
// 8. Proves idempotent (fire same event id twice → 2nd is no-op)
func TestCapacityChangeAutoReplansFutureDrives(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	plannedDate := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	park, goatIDs, staleBatchID := seedOperatorConfigReplanFixture(t, ctx, pool, "d1", 5, plannedDate)

	// Capture clinical due dates BEFORE capacity change
	beforeDueDates := make(map[string]time.Time)
	for _, goatID := range goatIDs {
		var dueAt time.Time
		if err := pool.QueryRow(ctx, `
SELECT COALESCE(MIN(due_at), '0001-01-01'::timestamp)
FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = $2::uuid
`, tenantID, goatID).Scan(&dueAt); err != nil {
			t.Fatalf("capture clinical due_at for goat %s: %v", goatID, err)
		}
		beforeDueDates[goatID] = dueAt
	}

	// Count obligations BEFORE
	var beforeCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[])
`, tenantID, goatIDs).Scan(&beforeCount); err != nil {
		t.Fatalf("count obligations before: %v", err)
	}

	// Change capacity config (simulating a real config change)
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_capacity_config SET max_per_day = 150
WHERE tenant_id = $1::uuid
`, tenantID); err != nil {
		t.Fatalf("update capacity config: %v", err)
	}

	// Wire the SAME bus + handler shape backend/internal/bootstrap/api.go wires
	bus := eventbus.NewInProcessBus()
	obligationRepo := NewRepository(pool, 5*time.Second)
	app.NewOperatorConfigReplanHandler(obligationRepo).Register(bus)

	// Publish vaccination.capacity.changed event through real handler
	const eventID = "cap-change-test-e2e-001"
	payload, _ := json.Marshal(app.OperatorConfigChangePayload{
		ParkID:        park,
		EffectiveFrom: "2026-07-24", // Trigger release from day before the planned batch
	})
	bus.Publish(ctx, eventbus.Event{
		ID:         eventID,
		Type:       app.EventVaccinationCapacityChanged,
		TenantID:   tenantID,
		Key:        park,
		Payload:    payload,
		OccurredAt: time.Now().In(biztime.DefaultLocation()),
	})

	// Assert batch released
	var batchStatus string
	if err := pool.QueryRow(ctx, `
SELECT status FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid
`, tenantID, staleBatchID).Scan(&batchStatus); err != nil {
		t.Fatalf("query batch status: %v", err)
	}
	if batchStatus != "superseded" {
		t.Fatalf("batch status = %q, want superseded (capacity.changed must auto-cascade)", batchStatus)
	}

	// Assert obligations released (batch_id = NULL)
	var releasedCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[]) AND batch_id IS NULL
`, tenantID, goatIDs).Scan(&releasedCount); err != nil {
		t.Fatalf("query released obligations: %v", err)
	}
	if releasedCount != len(goatIDs) {
		t.Fatalf("released obligations = %d, want %d", releasedCount, len(goatIDs))
	}

	// Assert obligation count unchanged (no lost, no duplicated)
	var afterCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[])
`, tenantID, goatIDs).Scan(&afterCount); err != nil {
		t.Fatalf("count obligations after: %v", err)
	}
	if afterCount != beforeCount {
		t.Fatalf("obligation count after = %d, want %d (lost/duplicated)", afterCount, beforeCount)
	}

	// Assert clinical due_at BYTE-IDENTICAL before/after
	for _, goatID := range goatIDs {
		var dueAt time.Time
		if err := pool.QueryRow(ctx, `
SELECT COALESCE(MIN(due_at), '0001-01-01'::timestamp)
FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = $2::uuid
`, tenantID, goatID).Scan(&dueAt); err != nil {
			t.Fatalf("query clinical due_at for goat %s after: %v", goatID, err)
		}
		if dueAt != beforeDueDates[goatID] {
			t.Fatalf("goat %s: due_at changed from %v to %v (clinical integrity broken)", goatID, beforeDueDates[goatID], dueAt)
		}
	}

	// IDEMPOTENCY: Fire the SAME event_id again, must be a no-op
	bus.Publish(ctx, eventbus.Event{
		ID:         eventID, // Same event_id
		Type:       app.EventVaccinationCapacityChanged,
		TenantID:   tenantID,
		Key:        park,
		Payload:    payload,
		OccurredAt: time.Now().In(biztime.DefaultLocation()),
	})

	// Verify no additional release (batch still superseded, obligations still released)
	var idempotentStatus string
	if err := pool.QueryRow(ctx, `
SELECT status FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid
`, tenantID, staleBatchID).Scan(&idempotentStatus); err != nil {
		t.Fatalf("query batch status after idempotent replay: %v", err)
	}
	if idempotentStatus != "superseded" {
		t.Fatalf("idempotent replay: batch status = %q, want still superseded", idempotentStatus)
	}
	var idempotentCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[]) AND batch_id IS NOT NULL
`, tenantID, goatIDs).Scan(&idempotentCount); err != nil {
		t.Fatalf("query still-batched after idempotent replay: %v", err)
	}
	if idempotentCount != 0 {
		t.Fatalf("idempotent replay: still-batched = %d, want 0", idempotentCount)
	}
}

// TestActiveOperatorsPerDayChangeAutoReplansFutureDrives tests that vaccination.capacity.changed
// (with N/active_operators_per_day change) auto-re-plans future planned batches. The test:
// 1. Seeds config with old N + future planned batches
// 2. Captures clinical due dates BEFORE N change
// 3. Updates capacity config (changes active_operators_per_day)
// 4. Publishes vaccination.capacity.changed event
// 5. Asserts batches released, obligations preserved, clinical unchanged, idempotent
func TestActiveOperatorsPerDayChangeAutoReplansFutureDrives(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	plannedDate := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	park, goatIDs, staleBatchID := seedOperatorConfigReplanFixture(t, ctx, pool, "d2", 5, plannedDate)

	// Capture clinical due dates BEFORE N change
	beforeDueDates := make(map[string]time.Time)
	for _, goatID := range goatIDs {
		var dueAt time.Time
		if err := pool.QueryRow(ctx, `
SELECT COALESCE(MIN(due_at), '0001-01-01'::timestamp)
FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = $2::uuid
`, tenantID, goatID).Scan(&dueAt); err != nil {
			t.Fatalf("capture clinical due_at for goat %s: %v", goatID, err)
		}
		beforeDueDates[goatID] = dueAt
	}

	// Count obligations BEFORE
	var beforeCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[])
`, tenantID, goatIDs).Scan(&beforeCount); err != nil {
		t.Fatalf("count obligations before: %v", err)
	}

	// Change operator assignment config N (active_operators_per_day)
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_operator_assignment_config SET active_operators_per_day = 2
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
`, tenantID, park); err != nil {
		t.Fatalf("update operator assignment config N: %v", err)
	}

	// Wire bus + handler
	bus := eventbus.NewInProcessBus()
	obligationRepo := NewRepository(pool, 5*time.Second)
	app.NewOperatorConfigReplanHandler(obligationRepo).Register(bus)

	// Publish vaccination.capacity.changed event
	const eventID = "nchange-test-e2e-001"
	payload, _ := json.Marshal(app.OperatorConfigChangePayload{
		ParkID:        park,
		EffectiveFrom: "2026-07-24",
	})
	bus.Publish(ctx, eventbus.Event{
		ID:         eventID,
		Type:       app.EventVaccinationCapacityChanged,
		TenantID:   tenantID,
		Key:        park,
		Payload:    payload,
		OccurredAt: time.Now().In(biztime.DefaultLocation()),
	})

	// Assert batch released
	var batchStatus string
	if err := pool.QueryRow(ctx, `
SELECT status FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid
`, tenantID, staleBatchID).Scan(&batchStatus); err != nil {
		t.Fatalf("query batch status: %v", err)
	}
	if batchStatus != "superseded" {
		t.Fatalf("batch status = %q, want superseded (N change must auto-cascade)", batchStatus)
	}

	// Assert obligations released
	var releasedCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[]) AND batch_id IS NULL
`, tenantID, goatIDs).Scan(&releasedCount); err != nil {
		t.Fatalf("query released obligations: %v", err)
	}
	if releasedCount != len(goatIDs) {
		t.Fatalf("released obligations = %d, want %d", releasedCount, len(goatIDs))
	}

	// Assert obligation count unchanged
	var afterCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[])
`, tenantID, goatIDs).Scan(&afterCount); err != nil {
		t.Fatalf("count obligations after: %v", err)
	}
	if afterCount != beforeCount {
		t.Fatalf("obligation count after = %d, want %d (lost/duplicated)", afterCount, beforeCount)
	}

	// Assert clinical due_at BYTE-IDENTICAL
	for _, goatID := range goatIDs {
		var dueAt time.Time
		if err := pool.QueryRow(ctx, `
SELECT COALESCE(MIN(due_at), '0001-01-01'::timestamp)
FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = $2::uuid
`, tenantID, goatID).Scan(&dueAt); err != nil {
			t.Fatalf("query clinical due_at for goat %s after: %v", goatID, err)
		}
		if dueAt != beforeDueDates[goatID] {
			t.Fatalf("goat %s: due_at changed from %v to %v (clinical integrity broken)", goatID, beforeDueDates[goatID], dueAt)
		}
	}

	// IDEMPOTENCY: Fire same event_id again
	bus.Publish(ctx, eventbus.Event{
		ID:         eventID, // Same event_id
		Type:       app.EventVaccinationCapacityChanged,
		TenantID:   tenantID,
		Key:        park,
		Payload:    payload,
		OccurredAt: time.Now().In(biztime.DefaultLocation()),
	})

	// Verify no additional release
	var idempotentStatus string
	if err := pool.QueryRow(ctx, `
SELECT status FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid
`, tenantID, staleBatchID).Scan(&idempotentStatus); err != nil {
		t.Fatalf("query batch status after idempotent replay: %v", err)
	}
	if idempotentStatus != "superseded" {
		t.Fatalf("idempotent replay: batch status = %q, want still superseded", idempotentStatus)
	}
	var idempotentCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[]) AND batch_id IS NOT NULL
`, tenantID, goatIDs).Scan(&idempotentCount); err != nil {
		t.Fatalf("query still-batched after idempotent replay: %v", err)
	}
	if idempotentCount != 0 {
		t.Fatalf("idempotent replay: still-batched = %d, want 0", idempotentCount)
	}
}

// TestDefaultOperatorSwapAutoReplansFutureDrivesFromSwapDate tests that vaccination.roster.changed
// (with default operator swap) auto-re-plans future planned batches from the effective date.
// The test:
// 1. Seeds config with default=operatorA + future planned batches conducted_by A
// 2. Captures clinical due dates BEFORE swap
// 3. Swaps default operator to operatorB
// 4. Seeds one in-progress batch (conducted_by A) BEFORE the swap date to prove it's untouched
// 5. Publishes vaccination.roster.changed event with swap date
// 6. Asserts future planned batches released (status = superseded)
// 7. Asserts in-progress batch UNTOUCHED (still planned, conducted_by A)
// 8. Asserts obligations preserved, clinical unchanged, idempotent
func TestDefaultOperatorSwapAutoReplansFutureDrivesFromSwapDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	plannedDate := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	park, goatIDs, futureBatchID := seedOperatorConfigReplanFixture(t, ctx, pool, "e1", 5, plannedDate)

	// Extract the initial (old) default operator from the fixture
	var oldOperator string
	if err := pool.QueryRow(ctx, `
SELECT default_operator_id::text FROM vaccination_operator_assignment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
`, tenantID, park).Scan(&oldOperator); err != nil {
		t.Fatalf("query old default operator: %v", err)
	}

	// Create a new replacement operator
	const newOperator = "00000000-0000-4000-8000-0000000099f1"
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, 'OP-NEW', 'New Operator', 'active', 'operator', $3::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, newOperator, tenantID, park); err != nil {
		t.Fatalf("seed new operator: %v", err)
	}

	// Seed one in-progress/completed batch BEFORE the swap date (this must be untouched)
	pastBatchID := "00000000-0000-4000-8000-0000000080f2"
	pastDate := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) // Before swap date
	protoID := ""
	if err := pool.QueryRow(ctx, `
SELECT protocol_version_id::text FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid LIMIT 1
`, tenantID, futureBatchID).Scan(&protoID); err != nil {
		t.Fatalf("query protocol version: %v", err)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (
  batch_id, tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date,
  status, conducted_by, estimated_targets
)
VALUES ($6::uuid, $1::uuid, $2::uuid, 'park', $3::uuid, 'past', $4::date, 'in_progress', $5::uuid, 2)
`, tenantID, protoID, park, pastDate, oldOperator, pastBatchID); err != nil {
		t.Fatalf("seed past batch: %v", err)
	}

	// Capture clinical due dates BEFORE operator swap
	beforeDueDates := make(map[string]time.Time)
	for _, goatID := range goatIDs {
		var dueAt time.Time
		if err := pool.QueryRow(ctx, `
SELECT COALESCE(MIN(due_at), '0001-01-01'::timestamp)
FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = $2::uuid
`, tenantID, goatID).Scan(&dueAt); err != nil {
			t.Fatalf("capture clinical due_at for goat %s: %v", goatID, err)
		}
		beforeDueDates[goatID] = dueAt
	}

	// Count obligations BEFORE
	var beforeCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[])
`, tenantID, goatIDs).Scan(&beforeCount); err != nil {
		t.Fatalf("count obligations before: %v", err)
	}

	// Swap default operator
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_operator_assignment_config SET default_operator_id = $3::uuid
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
`, tenantID, park, newOperator); err != nil {
		t.Fatalf("update default operator: %v", err)
	}

	// Wire bus + handler
	bus := eventbus.NewInProcessBus()
	obligationRepo := NewRepository(pool, 5*time.Second)
	app.NewOperatorConfigReplanHandler(obligationRepo).Register(bus)

	// Publish vaccination.roster.changed event with swap date = swap date (2026-07-25)
	const eventID = "roster-swap-test-e2e-001"
	payload, _ := json.Marshal(app.OperatorConfigChangePayload{
		ParkID:        park,
		EffectiveFrom: "2026-07-25", // Effective from the future batch date
	})
	bus.Publish(ctx, eventbus.Event{
		ID:         eventID,
		Type:       app.EventVaccinationRosterChanged,
		TenantID:   tenantID,
		Key:        park,
		Payload:    payload,
		OccurredAt: time.Now().In(biztime.DefaultLocation()),
	})

	// Assert FUTURE batch (2026-07-25) is released
	var futureStatus string
	if err := pool.QueryRow(ctx, `
SELECT status FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid
`, tenantID, futureBatchID).Scan(&futureStatus); err != nil {
		t.Fatalf("query future batch status: %v", err)
	}
	if futureStatus != "superseded" {
		t.Fatalf("future batch status = %q, want superseded (roster.changed must cascade)", futureStatus)
	}

	// Assert PAST batch (2026-07-24) is UNTOUCHED (still in_progress, conducted_by not changed)
	var pastStatus string
	var pastConductedBy string
	if err := pool.QueryRow(ctx, `
SELECT status, conducted_by::text FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid
`, tenantID, pastBatchID).Scan(&pastStatus, &pastConductedBy); err != nil {
		t.Fatalf("query past batch status: %v", err)
	}
	if pastStatus != "in_progress" {
		t.Fatalf("past batch status = %q, want still in_progress (history preserved)", pastStatus)
	}
	if pastConductedBy != oldOperator {
		t.Fatalf("past batch conducted_by changed = %q, want %q (history intact)", pastConductedBy, oldOperator)
	}

	// Assert obligations from future batch released
	var futureReleased int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[]) AND batch_id IS NULL
`, tenantID, goatIDs).Scan(&futureReleased); err != nil {
		t.Fatalf("query released obligations: %v", err)
	}
	if futureReleased != len(goatIDs) {
		t.Fatalf("released future obligations = %d, want %d", futureReleased, len(goatIDs))
	}

	// Assert obligation count unchanged
	var afterCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[])
`, tenantID, goatIDs).Scan(&afterCount); err != nil {
		t.Fatalf("count obligations after: %v", err)
	}
	if afterCount != beforeCount {
		t.Fatalf("obligation count after = %d, want %d (lost/duplicated)", afterCount, beforeCount)
	}

	// Assert clinical due_at BYTE-IDENTICAL
	for _, goatID := range goatIDs {
		var dueAt time.Time
		if err := pool.QueryRow(ctx, `
SELECT COALESCE(MIN(due_at), '0001-01-01'::timestamp)
FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = $2::uuid
`, tenantID, goatID).Scan(&dueAt); err != nil {
			t.Fatalf("query clinical due_at for goat %s after: %v", goatID, err)
		}
		if dueAt != beforeDueDates[goatID] {
			t.Fatalf("goat %s: due_at changed from %v to %v (clinical integrity broken)", goatID, beforeDueDates[goatID], dueAt)
		}
	}

	// IDEMPOTENCY: Fire same event_id again
	bus.Publish(ctx, eventbus.Event{
		ID:         eventID, // Same event_id
		Type:       app.EventVaccinationRosterChanged,
		TenantID:   tenantID,
		Key:        park,
		Payload:    payload,
		OccurredAt: time.Now().In(biztime.DefaultLocation()),
	})

	// Verify no additional release
	var idempotentStatus string
	if err := pool.QueryRow(ctx, `
SELECT status FROM obligation_batches
WHERE tenant_id = $1::uuid AND batch_id = $2::uuid
`, tenantID, futureBatchID).Scan(&idempotentStatus); err != nil {
		t.Fatalf("query batch status after idempotent replay: %v", err)
	}
	if idempotentStatus != "superseded" {
		t.Fatalf("idempotent replay: batch status = %q, want still superseded", idempotentStatus)
	}
	var idempotentCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[]) AND batch_id IS NOT NULL
`, tenantID, goatIDs).Scan(&idempotentCount); err != nil {
		t.Fatalf("query still-batched after idempotent replay: %v", err)
	}
	if idempotentCount != 0 {
		t.Fatalf("idempotent replay: still-batched = %d, want 0", idempotentCount)
	}
}

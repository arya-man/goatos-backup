package postgres

import (
	"context"
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

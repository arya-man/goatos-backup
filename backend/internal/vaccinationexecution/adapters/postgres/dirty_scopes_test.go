package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// testShedIncA/testShedIncB are a second/third shed pair (distinct from testShed/testShedB used by
// the shed_projection_test.go parity test) so the bounded-incremental-projector tests in this file
// never disturb another test's fixture.
const (
	testShedIncA = "70000000-0000-4000-8000-000000000101"
	testShedIncB = "70000000-0000-4000-8000-000000000102"
)

// seedIncrementalShardSheds creates the two shed locations these tests exercise, under testPark
// (already seeded by seedVaccinationExecutionProjection). Call after seedVaccinationExecutionProjection.
func seedIncrementalShardSheds(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "shed inc A",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-INC-A', 'Inc Shed A', $3, 'active')`,
		testShedIncA, testTenant, testPark)
	execProjectionSQL(t, ctx, pool, "shed inc B",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-INC-B', 'Inc Shed B', $3, 'active')`,
		testShedIncB, testTenant, testPark)
}

// TestEnqueueDirtyShedCoalesces proves the durable dirty-scope queue coalesces repeated dirties of
// the same shed into ONE pending row (the partial unique index
// vaccination_projection_dirty_scopes_coalesce_uidx), never a duplicate row per write.
func TestEnqueueDirtyShedCoalesces(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "goat_shifted"); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "vaccination_completed"); err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "goat_exited"); err != nil {
		t.Fatalf("third enqueue: %v", err)
	}

	pending := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "pending")
	if pending != 1 {
		t.Fatalf("pending dirty scopes for shed = %d, want 1 (coalesced)", pending)
	}
	reason := dirtyScopeReason(t, ctx, pool, testTenant, testShedIncA)
	if reason != "goat_exited" {
		t.Fatalf("reason = %q, want the latest enqueue reason %q", reason, "goat_exited")
	}

	// A different shed enqueues its own separate row.
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncB, "goat_shifted"); err != nil {
		t.Fatalf("enqueue shed B: %v", err)
	}
	if got := countDirtyScopes(t, ctx, pool, testTenant, testShedIncB, "pending"); got != 1 {
		t.Fatalf("pending dirty scopes for shed B = %d, want 1", got)
	}
}

// TestClaimDirtyScopesSkipLockedNoDoubleClaim proves FOR UPDATE SKIP LOCKED lets two concurrent
// claims split disjoint scopes and never double-claim the same scope: claim once (simulating
// worker 1), leave the lease outstanding, and prove a second claim (worker 2) does not see it.
func TestClaimDirtyScopesSkipLockedNoDoubleClaim(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	if err := repo.EnqueueDirtySheds(ctx, testTenant, []string{testShedIncA, testShedIncB}, "seed"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	now := time.Now()
	firstClaim, err := repo.ClaimDirtyScopes(ctx, "worker-1", "", 1, now)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if len(firstClaim) != 1 {
		t.Fatalf("first claim size = %d, want 1", len(firstClaim))
	}
	firstShed := firstClaim[0].ShedID

	secondClaim, err := repo.ClaimDirtyScopes(ctx, "worker-2", "", 10, now)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(secondClaim) != 1 {
		t.Fatalf("second claim size = %d, want 1 (the other shed only)", len(secondClaim))
	}
	if secondClaim[0].ShedID == firstShed {
		t.Fatalf("second claim returned the same shed %s that worker-1 already leased -- double claim", firstShed)
	}
	if secondClaim[0].ShedID != testShedIncA && secondClaim[0].ShedID != testShedIncB {
		t.Fatalf("unexpected shed claimed: %s", secondClaim[0].ShedID)
	}

	leased := countDirtyScopes(t, ctx, pool, testTenant, firstShed, "leased")
	if leased != 1 {
		t.Fatalf("leased scopes for %s = %d, want 1", firstShed, leased)
	}

	// A third claim attempt sees nothing pending left (both sheds are now leased).
	thirdClaim, err := repo.ClaimDirtyScopes(ctx, "worker-3", "", 10, now)
	if err != nil {
		t.Fatalf("third claim: %v", err)
	}
	if len(thirdClaim) != 0 {
		t.Fatalf("third claim size = %d, want 0 (nothing left pending)", len(thirdClaim))
	}
}

// testTenantB is a second tenant for TestClaimDirtyScopesTenantFilterPreventsCrossTenantStarvation --
// distinct from testTenant (seedVaccinationExecutionProjection's tenant). Dirty-scope rows only need a
// tenant_id FK to tenants(tenant_id); they carry no FK to locations, so this tenant needs no park/shed
// fixtures of its own.
const testTenantB = "00000000-0000-4000-8000-000000000099"

// TestClaimDirtyScopesTenantFilterPreventsCrossTenantStarvation is the C5-003 regression test: before
// the fix, ClaimDirtyScopes had no tenant filter, so a shared claim query ordered purely by
// (next_attempt_at, dirty_scope_id) let another tenant's dirty rows consume the whole -limit claim
// batch and starve the configured tenant -- exactly the failure mode a single-tenant deployment job
// (GOATOS_TENANT_ID set) hits in production. This seeds MORE dirty scopes for tenant B, enqueued
// FIRST so they would sort ahead of tenant A's single scope under the old unfiltered query, then
// proves a tenant-A-scoped claim with a limit equal to tenant B's row count (1) still claims exactly
// tenant A's scope, (2) never claims or mutates any tenant B row, and (3) tenant A is not starved.
func TestClaimDirtyScopesTenantFilterPreventsCrossTenantStarvation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status, created_at, updated_at)
VALUES ($1::uuid, 'Tenant B (dirty-scope isolation test)', 'active', now(), now())`, testTenantB); err != nil {
		t.Fatalf("seed tenant B: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)

	// Tenant B enqueues first (3 sheds) -- smaller dirty_scope_id / earlier next_attempt_at, so an
	// unfiltered global ORDER BY would return these before tenant A's row below.
	tenantBShedIDs := []string{
		"90000000-0000-4000-8000-000000000201",
		"90000000-0000-4000-8000-000000000202",
		"90000000-0000-4000-8000-000000000203",
	}
	if err := repo.EnqueueDirtySheds(ctx, testTenantB, tenantBShedIDs, "seed_tenant_b"); err != nil {
		t.Fatalf("enqueue tenant B sheds: %v", err)
	}

	// Tenant A (testTenant) enqueues one shed AFTER tenant B's rows.
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "seed_tenant_a"); err != nil {
		t.Fatalf("enqueue tenant A shed: %v", err)
	}

	now := time.Now()
	// limit == len(tenantBShedIDs): under the pre-fix unfiltered query, this limit would be entirely
	// consumed by tenant B's 3 rows (enqueued first), leaving tenant A's row unclaimed this run.
	claimed, err := repo.ClaimDirtyScopes(ctx, "worker-tenant-a", testTenant, len(tenantBShedIDs), now)
	if err != nil {
		t.Fatalf("tenant-scoped claim: %v", err)
	}

	// (1) Only tenant A's scope is claimed.
	if len(claimed) != 1 {
		t.Fatalf("claimed = %#v, want exactly 1 (tenant A's own scope, never any tenant B row)", claimed)
	}
	if claimed[0].TenantID != testTenant || claimed[0].ShedID != testShedIncA {
		t.Fatalf("claimed scope = %#v, want tenant=%s shed=%s", claimed[0], testTenant, testShedIncA)
	}

	// (2) Tenant B's rows were neither claimed nor mutated: still pending, attempt_count unchanged.
	for _, shedID := range tenantBShedIDs {
		status, attempts := dirtyScopeStatusAndAttempts(t, ctx, pool, testTenantB, shedID)
		if status != "pending" {
			t.Fatalf("tenant B shed %s status = %q, want pending (must not be claimed by tenant A's filtered claim)", shedID, status)
		}
		if attempts != 0 {
			t.Fatalf("tenant B shed %s attempt_count = %d, want 0 (untouched)", shedID, attempts)
		}
	}
	if pending := countDirtyScopes(t, ctx, pool, testTenantB, tenantBShedIDs[0], "pending"); pending != 1 {
		t.Fatalf("tenant B shed %s pending count = %d, want 1", tenantBShedIDs[0], pending)
	}

	// (3) Tenant A is not starved: its one dirty scope was fully drained despite tenant B's rows
	// dominating the unfiltered ordering and despite a claim limit sized to tenant B's row count.
	if leased := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "leased"); leased != 1 {
		t.Fatalf("tenant A shed %s leased count = %d, want 1 (claimed, not starved)", testShedIncA, leased)
	}
	if pending := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "pending"); pending != 0 {
		t.Fatalf("tenant A shed %s pending count = %d, want 0 (already claimed)", testShedIncA, pending)
	}
}

// TestRebuildShedShardTouchesOnlyClaimedShed is the core P0-B proof: seed two sheds, run the
// full-tenant bootstrap recompute, then mutate shed A's data and call RebuildShedShard(A) only.
// Shed A's projection row must reflect the mutation; shed B's row AND its shard_state.projected_at
// must be COMPLETELY UNCHANGED -- proving the incremental rebuild never copies/touches any other
// shed's row and never restamps a tenant-wide timestamp (the two rejected designs in the handoff
// doc).
func TestRebuildShedShardTouchesOnlyClaimedShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	seedIncrementalShardSheds(t, ctx, pool)

	// Shed A: one SCHEDULED goat (due_at in the future relative to asOf below), so after the
	// mutation below there is still an open (non-overdue) cell whose due_at seeds next_transition_at.
	insertProjectionGoat(t, ctx, pool, "70000000-0000-4000-8000-000000000110", testShedIncA, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000111", testShedIncA,
		"70000000-0000-4000-8000-000000000110", "scheduled", "2026-07-10 00:00:00+00", "shard-a-1")

	// Shed B: one due goat, untouched throughout this test.
	insertProjectionGoat(t, ctx, pool, "70000000-0000-4000-8000-000000000120", testShedIncB, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000121", testShedIncB,
		"70000000-0000-4000-8000-000000000120", "scheduled", "2026-06-20 00:00:00+00", "shard-b-1")

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{TenantID: testTenant, AsOf: asOf}); err != nil {
		t.Fatalf("bootstrap RecomputeShedProjection: %v", err)
	}

	servingVersion, ok := repo.shedProjectionServingVersion(ctx, testTenant)
	if !ok {
		t.Fatalf("expected a serving version after bootstrap recompute")
	}

	beforeB := readShedRow(t, ctx, pool, testTenant, servingVersion, testShedIncB)
	beforeBState := readShardState(t, ctx, pool, testTenant, testShedIncB)

	// Mutate shed A only: add a second, OVERDUE goat.
	insertProjectionGoat(t, ctx, pool, "70000000-0000-4000-8000-000000000112", testShedIncA, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000113", testShedIncA,
		"70000000-0000-4000-8000-000000000112", "scheduled", "2026-06-01 00:00:00+00", "shard-a-2")

	result, err := repo.RebuildShedShard(ctx, domain.RebuildShedShardRequest{TenantID: testTenant, ShedID: testShedIncA, AsOf: asOf})
	if err != nil {
		t.Fatalf("RebuildShedShard(A): %v", err)
	}
	if result.Deferred {
		t.Fatalf("RebuildShedShard(A) deferred unexpectedly")
	}
	if !result.RowPresent {
		t.Fatalf("RebuildShedShard(A) row_present = false, want true (shed has alive animals)")
	}
	if result.ProjectionVersion != servingVersion {
		t.Fatalf("RebuildShedShard(A) wrote projection_version %d, want the existing serving version %d", result.ProjectionVersion, servingVersion)
	}

	afterA := readShedRow(t, ctx, pool, testTenant, servingVersion, testShedIncA)
	if afterA.Animals != 2 {
		t.Fatalf("shed A animals = %d, want 2 after the incremental rebuild picked up the new goat", afterA.Animals)
	}
	if afterA.Status != domain.ShedStatusOverdue {
		t.Fatalf("shed A status = %s, want overdue", afterA.Status)
	}

	afterB := readShedRow(t, ctx, pool, testTenant, servingVersion, testShedIncB)
	if afterB.Animals != beforeB.Animals || afterB.Status != beforeB.Status || afterB.DueAnimals != beforeB.DueAnimals {
		t.Fatalf("shed B row changed after rebuilding ONLY shed A: before=%#v after=%#v", beforeB, afterB)
	}

	afterBState := readShardState(t, ctx, pool, testTenant, testShedIncB)
	if !afterBState.ProjectedAt.Equal(beforeBState.ProjectedAt) {
		t.Fatalf("shed B shard_state.projected_at changed (%s -> %s) after rebuilding ONLY shed A -- proves a tenant-wide restamp, not per-shed", beforeBState.ProjectedAt, afterBState.ProjectedAt)
	}

	shedAState := readShardState(t, ctx, pool, testTenant, testShedIncA)
	if !shedAState.ProjectedAt.After(beforeBState.ProjectedAt.Add(-time.Millisecond)) {
		t.Fatalf("shed A shard_state.projected_at (%s) should reflect the just-completed incremental rebuild", shedAState.ProjectedAt)
	}
	if shedAState.NextTransitionAt == nil {
		t.Fatalf("shed A shard_state.next_transition_at should be set (open cells exist)")
	}
}

// TestRebuildShedShardEmptyShedDeletesRow proves a shed that has lost all its alive animals gets its
// projection row DELETEd (not left stale) and its shard state marked row_present=false.
func TestRebuildShedShardEmptyShedDeletesRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	seedIncrementalShardSheds(t, ctx, pool)

	goatID := "70000000-0000-4000-8000-000000000130"
	insertProjectionGoat(t, ctx, pool, goatID, testShedIncA, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000131", testShedIncA, goatID, "scheduled", "2026-06-20 00:00:00+00", "shard-empty-1")

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{TenantID: testTenant, AsOf: asOf}); err != nil {
		t.Fatalf("bootstrap RecomputeShedProjection: %v", err)
	}
	servingVersion, ok := repo.shedProjectionServingVersion(ctx, testTenant)
	if !ok {
		t.Fatalf("expected serving version")
	}
	if row, found := tryReadShedRow(t, ctx, pool, testTenant, servingVersion, testShedIncA); !found || row.Animals != 1 {
		t.Fatalf("precondition failed: shed A row = %#v found=%v", row, found)
	}

	// The goat exits (no longer alive).
	execProjectionSQL(t, ctx, pool, "exit goat", `UPDATE goats SET lifecycle_status = 'dead' WHERE tenant_id = $1 AND goat_id = $2`, testTenant, goatID)

	result, err := repo.RebuildShedShard(ctx, domain.RebuildShedShardRequest{TenantID: testTenant, ShedID: testShedIncA, AsOf: asOf})
	if err != nil {
		t.Fatalf("RebuildShedShard: %v", err)
	}
	if result.RowPresent {
		t.Fatalf("RebuildShedShard row_present = true, want false (no alive animals left)")
	}
	if _, found := tryReadShedRow(t, ctx, pool, testTenant, servingVersion, testShedIncA); found {
		t.Fatalf("projection row for shed A still present after it lost all alive animals")
	}
	state := readShardState(t, ctx, pool, testTenant, testShedIncA)
	if state.RowPresent {
		t.Fatalf("shard state row_present = true, want false")
	}
}

// TestRebuildShedShardDeferredWithoutServingVersion proves an incremental rebuild is a safe no-op
// (Deferred=true) when the tenant has never been bootstrapped by RecomputeShedProjection -- there is
// no serving version to UPSERT a row into.
func TestRebuildShedShardDeferredWithoutServingVersion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	result, err := repo.RebuildShedShard(ctx, domain.RebuildShedShardRequest{TenantID: testTenant, ShedID: testShed})
	if err != nil {
		t.Fatalf("RebuildShedShard: %v", err)
	}
	if !result.Deferred {
		t.Fatalf("expected Deferred=true with no serving projection version yet")
	}
}

// TestEnqueueDueTransitionsTimeDriven proves a shed whose next_transition_at has already passed is
// picked up by EnqueueDueTransitions (the time-driven pass) purely because time moved on, without
// any new write, and that a subsequent RebuildShedShard reflects the transition (scheduled ->
// overdue as the clock passes due_at).
func TestEnqueueDueTransitionsTimeDriven(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	seedIncrementalShardSheds(t, ctx, pool)

	goatID := "70000000-0000-4000-8000-000000000140"
	insertProjectionGoat(t, ctx, pool, goatID, testShedIncA, testPark)
	// due_at just ahead of "yesterday" asOf so the shed is scheduled at bootstrap, but the boundary
	// (window_start defaults to due_at) is already in the past relative to "today".
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000141", testShedIncA, goatID, "scheduled", "2026-06-24 06:00:00+00", "time-driven-1")

	repo := NewRepository(pool, 5*time.Second)
	bootstrapAsOf := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC) // before due_at -> scheduled
	if _, err := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{TenantID: testTenant, AsOf: bootstrapAsOf}); err != nil {
		t.Fatalf("bootstrap RecomputeShedProjection: %v", err)
	}
	// First incremental rebuild computes and stores next_transition_at for shed A (the recompute
	// bootstrap itself leaves next_transition_at NULL -- only RebuildShedShard computes it).
	if _, err := repo.RebuildShedShard(ctx, domain.RebuildShedShardRequest{TenantID: testTenant, ShedID: testShedIncA, AsOf: bootstrapAsOf}); err != nil {
		t.Fatalf("seed RebuildShedShard: %v", err)
	}
	state := readShardState(t, ctx, pool, testTenant, testShedIncA)
	if state.NextTransitionAt == nil {
		t.Fatalf("expected next_transition_at to be set after the seed rebuild")
	}

	// Move the shard's next_transition_at into the past directly (simulates time having passed
	// without touching the shard state's projected_at/updated_at bookkeeping columns otherwise).
	execProjectionSQL(t, ctx, pool, "backdate next_transition_at",
		`UPDATE vaccination_shed_shard_state SET next_transition_at = now() - interval '1 hour' WHERE tenant_id = $1 AND shed_id = $2`,
		testTenant, testShedIncA)

	enqueued, err := repo.EnqueueDueTransitions(ctx, testTenant, 100)
	if err != nil {
		t.Fatalf("EnqueueDueTransitions: %v", err)
	}
	if enqueued < 1 {
		t.Fatalf("EnqueueDueTransitions enqueued = %d, want >= 1", enqueued)
	}
	if got := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "pending"); got != 1 {
		t.Fatalf("pending dirty scopes for shed A = %d, want 1 (time-driven enqueue)", got)
	}

	// Rebuilding with "now" (after due_at) must show the transition took effect purely from the
	// passage of time -- no new obligation/goat write happened between the two rebuilds.
	nowAsOf := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	result, err := repo.RebuildShedShard(ctx, domain.RebuildShedShardRequest{TenantID: testTenant, ShedID: testShedIncA, AsOf: nowAsOf})
	if err != nil {
		t.Fatalf("RebuildShedShard after time-driven enqueue: %v", err)
	}
	if result.Deferred || !result.RowPresent {
		t.Fatalf("unexpected rebuild result: %#v", result)
	}
	servingVersion, _ := repo.shedProjectionServingVersion(ctx, testTenant)
	row := readShedRow(t, ctx, pool, testTenant, servingVersion, testShedIncA)
	if row.Status != domain.ShedStatusOverdue {
		t.Fatalf("shed status = %s, want overdue after time alone crossed due_at", row.Status)
	}
}

// TestMarkDirtyScopeFailedRetriesThenDeadLetters proves a repeatedly failing rebuild increments
// attempt_count on each failure and dead-letters the scope once max_attempts is exhausted, matching
// the outbox ClaimPending/MarkFailed dead-letter contract this queue mirrors.
func TestMarkDirtyScopeFailedRetriesThenDeadLetters(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "seed"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	execProjectionSQL(t, ctx, pool, "tight attempt budget",
		`UPDATE vaccination_projection_dirty_scopes SET max_attempts = 3 WHERE tenant_id = $1 AND shed_id = $2 AND status = 'pending'`,
		testTenant, testShedIncA)

	now := time.Now()
	for attempt := 1; attempt <= 3; attempt++ {
		claimed, err := repo.ClaimDirtyScopes(ctx, "worker-retry", "", 10, now)
		if err != nil {
			t.Fatalf("claim attempt %d: %v", attempt, err)
		}
		if len(claimed) != 1 {
			t.Fatalf("claim attempt %d size = %d, want 1", attempt, len(claimed))
		}
		if err := repo.MarkDirtyScopeFailed(ctx, claimed[0].DirtyScopeID, fmt.Sprintf("simulated failure %d", attempt), now); err != nil {
			t.Fatalf("mark failed attempt %d: %v", attempt, err)
		}
		now = now.Add(4 * time.Minute) // past every backoff window so the next claim is ready
	}

	status, attemptCount := dirtyScopeStatusAndAttempts(t, ctx, pool, testTenant, testShedIncA)
	if status != "dead_letter" {
		t.Fatalf("status = %q, want dead_letter after exhausting max_attempts", status)
	}
	if attemptCount != 3 {
		t.Fatalf("attempt_count = %d, want 3", attemptCount)
	}

	// A dead-lettered scope is excluded from the coalescing partial index, so re-dirtying the same
	// shed creates a FRESH pending row rather than reviving the dead-lettered one.
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "retry_after_dlq"); err != nil {
		t.Fatalf("re-enqueue after dead-letter: %v", err)
	}
	if got := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "pending"); got != 1 {
		t.Fatalf("pending dirty scopes after re-enqueue = %d, want 1", got)
	}
}

// TestMarkDirtyScopeDoneDeletesUnchangedLease proves the normal-path behavior of the P2 guarded
// MarkDirtyScopeDone: a scope that was claimed and completed with NO concurrent re-dirty (updated_at
// still equals leased_at) is deleted, exactly as before the guard was added.
func TestMarkDirtyScopeDoneDeletesUnchangedLease(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "seed"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := repo.ClaimDirtyScopes(ctx, "worker-1", "", 10, time.Now())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed size = %d, want 1", len(claimed))
	}

	if err := repo.MarkDirtyScopeDone(ctx, claimed[0].DirtyScopeID); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	if got := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "pending"); got != 0 {
		t.Fatalf("pending scopes after done = %d, want 0", got)
	}
	if got := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "leased"); got != 0 {
		t.Fatalf("leased scopes after done = %d, want 0 (deleted)", got)
	}
}

// TestMarkDirtyScopeDonePreservesConcurrentReDirty is the P2 regression test for the
// MarkDirtyScopeDone guard. Before the fix, MarkDirtyScopeDone deleted strictly by
// (dirty_scope_id, status='leased') -- if a NEW write re-dirtied the SAME shed while a worker's
// rebuild for it was still in flight (the coalescing UPSERT in EnqueueDirtySheds updates the
// already-leased row's reason/updated_at rather than creating a second row), that newer signal was
// silently dropped the instant the in-flight rebuild completed and called MarkDirtyScopeDone,
// because the row was deleted regardless. This proves the newer signal survives: after a completion
// races a concurrent re-dirty, the scope is reset to pending (not deleted) with the re-dirty's own
// reason, so the next claim rebuilds the shed again for the write that arrived mid-flight.
func TestMarkDirtyScopeDonePreservesConcurrentReDirty(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "original_reason"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := repo.ClaimDirtyScopes(ctx, "worker-1", "", 10, time.Now())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed size = %d, want 1", len(claimed))
	}
	dirtyScopeID := claimed[0].DirtyScopeID

	// A NEW write re-dirties shed A WHILE the worker above is still (notionally) mid-rebuild for the
	// original reason. The coalescing UPSERT updates THIS SAME leased row (reason + updated_at),
	// never resetting it to pending or creating a second row.
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "concurrent_write"); err != nil {
		t.Fatalf("concurrent re-dirty enqueue: %v", err)
	}
	if got := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "leased"); got != 1 {
		t.Fatalf("leased scopes after concurrent re-dirty = %d, want 1 (still the same row, coalesced)", got)
	}

	// The original worker finishes its rebuild (for "original_reason") and marks the scope done.
	if err := repo.MarkDirtyScopeDone(ctx, dirtyScopeID); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	if got := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "leased"); got != 0 {
		t.Fatalf("leased scopes after done = %d, want 0 (no longer leased)", got)
	}
	pending := countDirtyScopes(t, ctx, pool, testTenant, testShedIncA, "pending")
	if pending != 1 {
		t.Fatalf("pending scopes after done = %d, want 1 -- the concurrent re-dirty must NOT be silently dropped", pending)
	}
	if reason := dirtyScopeReason(t, ctx, pool, testTenant, testShedIncA); reason != "concurrent_write" {
		t.Fatalf("reason after done = %q, want the concurrent re-dirty's reason %q preserved", reason, "concurrent_write")
	}

	// The re-enqueued scope is claimable again (a fresh worker will rebuild shed A for the write that
	// arrived mid-flight, instead of that signal being lost forever).
	second, err := repo.ClaimDirtyScopes(ctx, "worker-2", "", 10, time.Now())
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(second) != 1 || second[0].ShedID != testShedIncA {
		t.Fatalf("second claim = %#v, want exactly shed A claimable again", second)
	}
}

// TestReclaimExpiredDirtyScopeLeasesRetriesThenDeadLetters is the P2 regression test proving
// ReclaimExpiredDirtyScopeLeases now increments attempt_count (and eventually dead-letters) exactly
// like MarkDirtyScopeFailed's explicit-failure path. Before the fix, a worker that reliably crashed
// mid-rebuild for the SAME shed (never reaching MarkDirtyScopeFailed) would have its lease reclaimed
// back to pending with attempt_count untouched forever -- pending -> leased -> expired -> pending in
// an unbounded loop that never reaches max_attempts and never dead-letters.
func TestReclaimExpiredDirtyScopeLeasesRetriesThenDeadLetters(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	if err := repo.EnqueueDirtyShed(ctx, testTenant, testShedIncA, "seed"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	execProjectionSQL(t, ctx, pool, "tight attempt budget",
		`UPDATE vaccination_projection_dirty_scopes SET max_attempts = 2 WHERE tenant_id = $1 AND shed_id = $2 AND status = 'pending'`,
		testTenant, testShedIncA)

	now := time.Now()
	for attempt := 1; attempt <= 2; attempt++ {
		claimed, err := repo.ClaimDirtyScopes(ctx, "worker-crash-loop", "", 10, now)
		if err != nil {
			t.Fatalf("claim attempt %d: %v", attempt, err)
		}
		if len(claimed) != 1 {
			t.Fatalf("claim attempt %d size = %d, want 1", attempt, len(claimed))
		}
		// Simulate a worker that crashed mid-rebuild: the lease expires WITHOUT ever calling
		// MarkDirtyScopeFailed or MarkDirtyScopeDone.
		expiredAt := now.Add(1 * time.Minute)
		execProjectionSQL(t, ctx, pool, "expire lease",
			`UPDATE vaccination_projection_dirty_scopes SET lease_expires_at = $3::timestamptz WHERE tenant_id = $1 AND shed_id = $2 AND status = 'leased'`,
			testTenant, testShedIncA, expiredAt.Add(-time.Second))

		reclaimed, err := repo.ReclaimExpiredDirtyScopeLeases(ctx, expiredAt)
		if err != nil {
			t.Fatalf("reclaim attempt %d: %v", attempt, err)
		}
		if reclaimed != 1 {
			t.Fatalf("reclaimed attempt %d = %d, want 1", attempt, reclaimed)
		}
		now = expiredAt.Add(4 * time.Minute) // past every backoff/lease window for the next claim
	}

	status, attemptCount := dirtyScopeStatusAndAttempts(t, ctx, pool, testTenant, testShedIncA)
	if status != "dead_letter" {
		t.Fatalf("status = %q, want dead_letter after two crash-loop reclaims exhausted max_attempts=2", status)
	}
	if attemptCount != 2 {
		t.Fatalf("attempt_count = %d, want 2", attemptCount)
	}
}

// TestShedSummaryCanonicalReadIgnoresShardStaleness proves the 5k-50k-envelope flip: ShedSummary now
// serves directly from canonical tables and is no longer gated on projection shard freshness at all, so
// a stale/backdated (or dead-lettered) shard_state row for one shed must NOT take down that shed's read,
// any other shed's read, or the unfiltered tenant-wide read. The projection shard-staleness machinery
// (AnyShedShardStaleInScope) is retained as projector infrastructure and still unit-tested directly, but
// it no longer wires into the request path. See
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md.
func TestShedSummaryCanonicalReadIgnoresShardStaleness(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	seedIncrementalShardSheds(t, ctx, pool)

	goatA := "70000000-0000-4000-8000-000000000170"
	insertProjectionGoat(t, ctx, pool, goatA, testShedIncA, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000171", testShedIncA, goatA, "scheduled", "2026-06-20 00:00:00+00", "scope-a-1")
	goatB := "70000000-0000-4000-8000-000000000172"
	insertProjectionGoat(t, ctx, pool, goatB, testShedIncB, testPark)
	insertShedScopedObligation(t, ctx, pool, "70000000-0000-4000-8000-000000000173", testShedIncB, goatB, "scheduled", "2026-06-20 00:00:00+00", "scope-b-1")

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	dueBefore := asOf.Add(30 * 24 * time.Hour)
	if _, err := repo.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{TenantID: testTenant, AsOf: asOf}); err != nil {
		t.Fatalf("bootstrap RecomputeShedProjection: %v", err)
	}
	// Rebuild both sheds incrementally so each has its own shard_state row to independently backdate.
	if _, err := repo.RebuildShedShard(ctx, domain.RebuildShedShardRequest{TenantID: testTenant, ShedID: testShedIncA, AsOf: asOf}); err != nil {
		t.Fatalf("RebuildShedShard(A): %v", err)
	}
	if _, err := repo.RebuildShedShard(ctx, domain.RebuildShedShardRequest{TenantID: testTenant, ShedID: testShedIncB, AsOf: asOf}); err != nil {
		t.Fatalf("RebuildShedShard(B): %v", err)
	}

	// Backdate ONLY shed A's shard_state beyond the staleness TTL.
	execProjectionSQL(t, ctx, pool, "backdate shed A shard state",
		`UPDATE vaccination_shed_shard_state SET projected_at = now() - interval '1 hour' WHERE tenant_id = $1 AND shed_id = $2`,
		testTenant, testShedIncA)

	shedA, shedB := testShedIncA, testShedIncB

	if _, err := repo.ShedSummary(ctx, domain.ShedSummaryQuery{TenantID: testTenant, ShedID: &shedB, AsOf: asOf, DueBefore: dueBefore, Limit: 1}); err != nil {
		t.Fatalf("ShedSummary(shed B) = %v, want success", err)
	}
	// Shed A's shard_state is backdated past the staleness TTL, but the canonical read is not gated on
	// shard freshness, so it still serves.
	if _, err := repo.ShedSummary(ctx, domain.ShedSummaryQuery{TenantID: testTenant, ShedID: &shedA, AsOf: asOf, DueBefore: dueBefore, Limit: 1}); err != nil {
		t.Fatalf("ShedSummary(shed A) err = %v, want success (canonical read ignores shard staleness)", err)
	}
	if _, err := repo.ShedSummary(ctx, domain.ShedSummaryQuery{TenantID: testTenant, AsOf: asOf, DueBefore: dueBefore, Limit: 200}); err != nil {
		t.Fatalf("unfiltered ShedSummary err = %v, want success (canonical read ignores shard staleness)", err)
	}
}

// ---- fixture + assertion helpers ----

// insertShedScopedObligation is like insertProjectionObligation but scopes BOTH target_id (goat)
// and scope_id (shed) to shedID, so the shard-scoped incremental query (which filters purely on
// goats.shed_id, ignoring obligation scope_id) and any future scope_id-based query agree.
func insertShedScopedObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID, shedID, goatID, status, dueAt, key string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "shed-scoped obligation "+obligationID,
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, NULL, 'goat', $5, 'shed', $6, $7::timestamptz, $8, $9, 1)`,
		obligationID, testTenant, testVersion, testRule, goatID, shedID, dueAt, status, key)
}

func countDirtyScopes(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, shedID, status string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM vaccination_projection_dirty_scopes
WHERE tenant_id = $1::uuid AND shed_id = $2 AND status = $3`, tenantID, shedID, status).Scan(&count); err != nil {
		t.Fatalf("count dirty scopes: %v", err)
	}
	return count
}

func dirtyScopeReason(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, shedID string) string {
	t.Helper()
	var reason string
	if err := pool.QueryRow(ctx, `
SELECT reason FROM vaccination_projection_dirty_scopes
WHERE tenant_id = $1::uuid AND shed_id = $2 AND status IN ('pending', 'leased')`, tenantID, shedID).Scan(&reason); err != nil {
		t.Fatalf("read dirty scope reason: %v", err)
	}
	return reason
}

func dirtyScopeStatusAndAttempts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, shedID string) (string, int) {
	t.Helper()
	var status string
	var attempts int
	if err := pool.QueryRow(ctx, `
SELECT status, attempt_count FROM vaccination_projection_dirty_scopes
WHERE tenant_id = $1::uuid AND shed_id = $2
ORDER BY dirty_scope_id DESC LIMIT 1`, tenantID, shedID).Scan(&status, &attempts); err != nil {
		t.Fatalf("read dirty scope status: %v", err)
	}
	return status, attempts
}

func readShedRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, projectionVersion int64, shedID string) domain.ShedSummaryProjection {
	t.Helper()
	row, found := tryReadShedRow(t, ctx, pool, tenantID, projectionVersion, shedID)
	if !found {
		t.Fatalf("shed row not found: tenant=%s version=%d shed=%s", tenantID, projectionVersion, shedID)
	}
	return row
}

func tryReadShedRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, projectionVersion int64, shedID string) (domain.ShedSummaryProjection, bool) {
	t.Helper()
	var row domain.ShedSummaryProjection
	var capacityStatus, shedStatus string
	var lastDone, nextDue *time.Time
	err := pool.QueryRow(ctx, `
SELECT park_id, park_name, shed_id, shed_name, animals, due_animals, open_cells, sessions,
       capacity_status, shed_status, last_done, next_due
FROM vaccination_shed_projection_rows
WHERE tenant_id = $1::uuid AND projection_version = $2::bigint AND shed_id = $3`,
		tenantID, projectionVersion, shedID).Scan(
		&row.ParkID, &row.ParkName, &row.ShedID, &row.ShedName, &row.Animals, &row.DueAnimals, &row.OpenCells, &row.Sessions,
		&capacityStatus, &shedStatus, &lastDone, &nextDue)
	if err != nil {
		return domain.ShedSummaryProjection{}, false
	}
	row.Capacity = domain.CapacityStatus(capacityStatus)
	row.Status = domain.ShedStatus(shedStatus)
	row.LastDone, row.NextDue = lastDone, nextDue
	return row, true
}

type shardStateRow struct {
	ProjectedAt      time.Time
	AsOf             time.Time
	NextTransitionAt *time.Time
	RowPresent       bool
}

func readShardState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, shedID string) shardStateRow {
	t.Helper()
	var out shardStateRow
	if err := pool.QueryRow(ctx, `
SELECT projected_at, as_of, next_transition_at, row_present
FROM vaccination_shed_shard_state
WHERE tenant_id = $1::uuid AND shed_id = $2`, tenantID, shedID).Scan(
		&out.ProjectedAt, &out.AsOf, &out.NextTransitionAt, &out.RowPresent); err != nil {
		t.Fatalf("read shard state for %s: %v", shedID, err)
	}
	return out
}

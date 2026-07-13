package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const multiBatchGoat = "10000000-0000-4000-8000-0000000000cc"

// seedTwoRuleProtocol creates a vaccination protocol version with two rules (two doses).
// A single goat then lands one open obligation per rule, and the sweeper groups by
// (scope, rule, window) so those obligations land in two DISTINCT batches — the setup
// needed to exercise the set-based UNNEST cancellation repair across multiple batches.
func seedTwoRuleProtocol(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (versionID, ruleA, ruleB string) {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.multibatch", Name: "MultiBatch", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err = proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleA, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("ruleA: %v", err)
	}
	ruleB, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "booster", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("ruleB: %v", err)
	}
	return versionID, ruleA, ruleB
}

// batchTargets returns batch_id -> estimated_targets for every batch of a protocol version.
func batchTargets(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID string) map[string]int {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT batch_id::text, estimated_targets FROM obligation_batches WHERE protocol_version_id=$1`, versionID)
	if err != nil {
		t.Fatalf("batch targets: %v", err)
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			t.Fatalf("scan batch target: %v", err)
		}
		out[id] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("batch targets rows: %v", err)
	}
	return out
}

// seedMultiBatchGoat installs a shed, a two-rule protocol, one goat, its two open obligations,
// and runs the real sweeper so each obligation is attached to its own batch. Returns the version id.
func seedMultiBatchGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	seedParkConsolidationShed(t, ctx, pool, parkShedA, "PARK-SHED-A")
	repo := NewRepository(pool, 5*time.Second)
	versionID, ruleA, ruleB := seedTwoRuleProtocol(t, ctx, pool)
	seedReserveGoats(t, ctx, pool, parkShedA, cbePark, multiBatchGoat)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	insertShedObligation(t, ctx, repo, versionID, ruleA, multiBatchGoat, parkShedA, "mb-primary", due)
	insertShedObligation(t, ctx, repo, versionID, ruleB, multiBatchGoat, parkShedA, "mb-booster", due)

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, defaultParkSweepConfig(),
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	before := batchTargets(t, ctx, pool, versionID)
	if len(before) != 2 {
		t.Fatalf("setup: want 2 distinct batches for the goat, got %d", len(before))
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE target_id=$1 AND batch_id IS NOT NULL`, multiBatchGoat); got != 2 {
		t.Fatalf("setup: want 2 batched obligations, got %d", got)
	}
	return versionID
}

// TestCancelOpenForGoatBulkDecrementsMultipleBatches proves the set-based UNNEST cancellation
// repair decrements EVERY affected batch in one statement (not just the first), inserts exactly
// one canceled status event per obligation, and replays idempotently.
func TestCancelOpenForGoatBulkDecrementsMultipleBatches(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	versionID := seedMultiBatchGoat(t, ctx, pool)
	before := batchTargets(t, ctx, pool, versionID)

	n, err := repo.CancelOpenForGoat(ctx, tenantID, multiBatchGoat, "ineligible_after_exit")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if n != 2 {
		t.Fatalf("cancelled = %d, want 2", n)
	}

	// EVERY batch that held a cancelled obligation must be decremented by exactly its count (1 here).
	after := batchTargets(t, ctx, pool, versionID)
	if len(after) != len(before) {
		t.Fatalf("batch count changed: before %d, after %d", len(before), len(after))
	}
	for id, was := range before {
		if after[id] != was-1 {
			t.Fatalf("batch %s estimated_targets: was %d, want %d, got %d", id, was, was-1, after[id])
		}
	}

	// Bulk status-event insert: exactly one canceled event per obligation.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_status_events
WHERE tenant_id=$1 AND event_type='canceled'
  AND obligation_id IN (SELECT obligation_id FROM obligation_instances WHERE target_id=$2)`,
		tenantID, multiBatchGoat); got != 2 {
		t.Fatalf("canceled events = %d, want 2", got)
	}

	// Idempotent replay: nothing re-cancelled, no double decrement, no duplicate events.
	n2, err := repo.CancelOpenForGoat(ctx, tenantID, multiBatchGoat, "ineligible_after_exit")
	if err != nil {
		t.Fatalf("re-cancel: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("replay cancelled = %d, want 0", n2)
	}
	replay := batchTargets(t, ctx, pool, versionID)
	for id, want := range after {
		if replay[id] != want {
			t.Fatalf("batch %s changed on replay: want %d, got %d", id, want, replay[id])
		}
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_status_events
WHERE tenant_id=$1 AND event_type='canceled'
  AND obligation_id IN (SELECT obligation_id FROM obligation_instances WHERE target_id=$2)`,
		tenantID, multiBatchGoat); got != 2 {
		t.Fatalf("canceled events after replay = %d, want 2 (idempotent)", got)
	}
}

// TestCancelOpenForGoatRollsBackOnStatusEventFailure proves the whole cancellation is atomic:
// a failure during the bulk status-event insert must roll back the status flips, the batch
// decrements, and the outbox writes — leaving zero partial state. Failure is injected
// deterministically via a BEFORE INSERT trigger on the ephemeral test database.
func TestCancelOpenForGoatRollsBackOnStatusEventFailure(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	versionID := seedMultiBatchGoat(t, ctx, pool)
	before := batchTargets(t, ctx, pool, versionID)

	// Force the bulk status-event insert to fail mid-transaction.
	if _, err := pool.Exec(ctx, `
CREATE FUNCTION fail_status_event() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'injected status-event failure'; END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER fail_status_event_trg BEFORE INSERT ON obligation_status_events
FOR EACH ROW EXECUTE FUNCTION fail_status_event();`); err != nil {
		t.Fatalf("install fault trigger: %v", err)
	}

	if _, err := repo.CancelOpenForGoat(ctx, tenantID, multiBatchGoat, "ineligible_after_exit"); err == nil {
		t.Fatalf("expected cancel to fail from injected trigger, got nil error")
	}

	// Remove the fault so the assertions (and the recovery cancel) can proceed.
	if _, err := pool.Exec(ctx, `
DROP TRIGGER fail_status_event_trg ON obligation_status_events;
DROP FUNCTION fail_status_event();`); err != nil {
		t.Fatalf("drop fault trigger: %v", err)
	}

	// FULL ROLLBACK — no status flip.
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE target_id=$1 AND status='canceled'`, multiBatchGoat); got != 0 {
		t.Fatalf("canceled obligations after rollback = %d, want 0", got)
	}
	// No batch decrement — every batch stays at its pre-cancel target.
	after := batchTargets(t, ctx, pool, versionID)
	for id, was := range before {
		if after[id] != was {
			t.Fatalf("batch %s estimated_targets changed despite rollback: was %d, got %d", id, was, after[id])
		}
	}
	// No status events, no outbox writes.
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND event_type='canceled'`, tenantID); got != 0 {
		t.Fatalf("canceled status events after rollback = %d, want 0", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='goat.obligations_canceled'`, tenantID); got != 0 {
		t.Fatalf("canceled outbox messages after rollback = %d, want 0", got)
	}

	// Recovery: with the fault removed the same call now succeeds and decrements both batches.
	n, err := repo.CancelOpenForGoat(ctx, tenantID, multiBatchGoat, "ineligible_after_exit")
	if err != nil {
		t.Fatalf("cancel after fault removed: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovery cancelled = %d, want 2", n)
	}
	recovered := batchTargets(t, ctx, pool, versionID)
	for id, was := range before {
		if recovered[id] != was-1 {
			t.Fatalf("batch %s after recovery: was %d, want %d, got %d", id, was, was-1, recovered[id])
		}
	}
}

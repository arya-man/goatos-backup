package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestInsertDeferredObligationCreatesRowAndEvent is R50-006's happy path: the first call creates
// the obligation_instances row and its initial 'deferred' obligation_status_events row atomically
// in one transaction.
func TestInsertDeferredObligationCreatesRowAndEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// seed() creates testGoatID + a draft protocol version + rule but no obligation row of its own
	// idempotency key here -- reuse its version/rule via a fresh obligation with a distinct key.
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	occurredAt := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	in := domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), Status: "deferred",
		IdempotencyKey: "obl-deferred-r50006", Sequence: 2,
	}
	obligationID, applied, err := repo.InsertDeferredObligation(ctx, in, "sick", occurredAt)
	if err != nil {
		t.Fatalf("insert deferred: %v", err)
	}
	if !applied || obligationID == "" {
		t.Fatalf("insert deferred: applied=%v id=%q, want applied=true", applied, obligationID)
	}
	if got := scanStatus(t, ctx, pool, obligationID); got != "deferred" {
		t.Fatalf("obligation status = %s, want deferred", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND idempotency_key=$2`,
		tenantID, "obl-deferred-r50006"); got != 1 {
		t.Fatalf("obligation row count = %d, want exactly 1", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`,
		tenantID, obligationID); got != 1 {
		t.Fatalf("deferred event count = %d, want exactly 1", got)
	}
}

// TestInsertDeferredObligationReplayRepairsMissingEvent is R50-006's repair path: a legacy replay
// where the obligation_instances row already exists but its deferred obligation_status_events row
// is missing (lost/never written) must repair the event WITHOUT duplicating the obligation row.
func TestInsertDeferredObligationReplayRepairsMissingEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	occurredAt := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	in := domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), Status: "deferred",
		IdempotencyKey: "obl-deferred-r50006-repair", Sequence: 2,
	}
	obligationID, applied, err := repo.InsertDeferredObligation(ctx, in, "sick", occurredAt)
	if err != nil || !applied {
		t.Fatalf("first insert deferred: applied=%v err=%v", applied, err)
	}

	// Simulate the lost-event scenario: the row exists, but its deferred status event does not
	// (deleted here to stand in for a historical partial-write/legacy gap).
	if _, err := pool.Exec(ctx,
		`DELETE FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`,
		tenantID, obligationID); err != nil {
		t.Fatalf("delete deferred event: %v", err)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`,
		tenantID, obligationID); got != 0 {
		t.Fatalf("precondition: deferred event count = %d, want 0 after delete", got)
	}

	// Replay with the SAME input (same idempotency key + same occurredAt, so the same eventKey is
	// recomputed): the row already exists, so InsertObligationInstance is a no-op (applied=false),
	// but the missing event must be repaired.
	replayID, replayApplied, err := repo.InsertDeferredObligation(ctx, in, "sick", occurredAt)
	if err != nil {
		t.Fatalf("replay insert deferred: %v", err)
	}
	if replayApplied {
		t.Fatalf("replay should report applied=false (no NEW obligation row created)")
	}
	if replayID != obligationID {
		t.Fatalf("replay obligation id = %q, want same id %q (no duplicate obligation)", replayID, obligationID)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND idempotency_key=$2`,
		tenantID, "obl-deferred-r50006-repair"); got != 1 {
		t.Fatalf("obligation row count after replay = %d, want exactly 1 (never duplicated)", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`,
		tenantID, obligationID); got != 1 {
		t.Fatalf("deferred event count after replay = %d, want exactly 1 (repaired, not duplicated)", got)
	}
}

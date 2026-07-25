package kernelstages

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// BUG-016: a goat created without its `goat.created` event gets NO vaccination
// obligations (that event is the sole SM-1 generation trigger) and, before this
// stage existed, nothing but a human running `backfill-goat-created` by hand
// ever repaired it. This test creates the goat bypassing the event-emitting
// path and proves the SCHEDULED reconciliation detects and repairs the gap with
// no manual CLI invocation.
func TestGoatCreatedRecoveryStageRepairsLostEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 5 * time.Second}, Logger: logger}

	const (
		tenant    = "00000000-0000-4000-8000-0000000016a1"
		custodian = "00000000-0000-4000-8000-0000000016a2"
		goatID    = "00000000-0000-4000-8000-0000000016a4"
	)
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'bug016-test', 'active') ON CONFLICT DO NOTHING`, tenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'org', 'bug016-custodian', 'active') ON CONFLICT DO NOTHING`, custodian); err != nil {
		t.Fatalf("seed custodian party: %v", err)
	}
	// Direct table write == the "lost event" scenario: the row exists, the
	// canonical goat.created event never landed.
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, entry_date)
VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', current_date)`,
		goatID, tenant, custodian); err != nil {
		t.Fatalf("seed goat: %v", err)
	}

	// RED baseline (and the pre-fix world): detect-only mode alerts but repairs
	// nothing, so the goat stays without its trigger event.
	t.Setenv("GOATOS_GOAT_CREATED_RECOVERY_MODE", "detect")
	if err := NewGoatCreatedRecoveryStage(deps, tenant).Run(ctx); err != nil {
		t.Fatalf("detect run: %v", err)
	}
	if got := countCreatedEvents(t, ctx, pool, tenant, goatID); got != 0 {
		t.Fatalf("detect-only mode must not write; got %d goat.created events", got)
	}

	// GREEN: the scheduled stage in its default (repair) mode recovers the event
	// through the same production path as the manual CLI.
	t.Setenv("GOATOS_GOAT_CREATED_RECOVERY_MODE", "")
	if err := NewGoatCreatedRecoveryStage(deps, tenant).Run(ctx); err != nil {
		t.Fatalf("repair run: %v", err)
	}
	if got := countCreatedEvents(t, ctx, pool, tenant, goatID); got != 1 {
		t.Fatalf("expected the lost goat.created event to be recovered; got %d", got)
	}
	var outbox int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'goat.created' AND status = 'pending'`,
		tenant, goatID).Scan(&outbox); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outbox != 1 {
		t.Fatalf("recovery must publish the event downstream; got %d pending outbox rows", outbox)
	}

	// Re-running the scheduled stage must be a no-op, not a duplicate event.
	if err := NewGoatCreatedRecoveryStage(deps, tenant).Run(ctx); err != nil {
		t.Fatalf("second repair run: %v", err)
	}
	if got := countCreatedEvents(t, ctx, pool, tenant, goatID); got != 1 {
		t.Fatalf("recovery is not idempotent; got %d goat.created events", got)
	}
}

func countCreatedEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, goatID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM goat_identity_events
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND event_type = 'goat.created'`, tenant, goatID).Scan(&n); err != nil {
		t.Fatalf("count goat.created events: %v", err)
	}
	return n
}

package chainproof

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/eventwiring"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The chain fixtures' external inputs. Ids are fixed so a failure names the same row every run.
const (
	chainTenant    = "7c0f1a2b-0000-4000-8000-000000000001"
	chainCustodian = "7c0f1a2b-0000-4000-8000-0000000000c0"
	chainPark      = "7c0f1a2b-0000-4000-8000-0000000000cb"
)

var chainEventAt = time.Date(2026, 7, 27, 9, 30, 0, 0, time.UTC)

// newChainDB starts the package's migrated Postgres and seeds the two rows every chain needs:
// the tenant and its custodian party. Nothing derived is seeded.
func newChainDB(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Chain Proof Tenant', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, chainTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Chain Proof Custodian', 'active')
ON CONFLICT (party_id) DO NOTHING`, chainCustodian); err != nil {
		t.Fatalf("seed custodian: %v", err)
	}
	return pool, ctx
}

// seedPark inserts the park a farm code resolves to. External input: a location the farm has.
func seedPark(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code, name string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', $3, $4, 'active')
ON CONFLICT (location_id) DO NOTHING`, chainPark, chainTenant, code, name); err != nil {
		t.Fatalf("seed park: %v", err)
	}
}

// workflowBus returns the production in-process bus with the workflow consumers registered by the
// ONE function every bus process calls, plus the tasks service those consumers write through.
func workflowBus(t *testing.T, pool *pgxpool.Pool) eventbus.Bus {
	t.Helper()
	log := slog.Default()
	bus := eventbus.NewInProcessBus()
	eventwiring.RegisterWorkflowConsumers(bus, eventwiring.NewWorkflowConsumerService(pool, 0, log), log)
	return bus
}

func f64(v float64) *float64 { return &v }

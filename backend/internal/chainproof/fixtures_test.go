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

// seedChainGoat inserts one animal. External input.
func seedChainGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, lifecycle string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, species, sex, breed, lifecycle_status, custodian_party_id,
                   origin_type, dob, entry_date, exited_at, exit_reason)
VALUES ($1::uuid, $2::uuid, 'goat', 'female', 'Boer', $3, $4::uuid,
        'birth', DATE '2026-07-01', DATE '2026-07-01',
        CASE WHEN $3 <> 'alive' THEN now() END,
        CASE WHEN $3 <> 'alive' THEN 'died' END)
ON CONFLICT (goat_id) DO NOTHING`, goatID, chainTenant, lifecycle, chainCustodian); err != nil {
		t.Fatalf("seed goat %s: %v", goatID, err)
	}
}

func workflowState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowID string) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM workflow_instances WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`,
		chainTenant, workflowID).Scan(&state); err != nil {
		t.Fatalf("read workflow state: %v", err)
	}
	return state
}

func goatLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx, `SELECT lifecycle_status FROM goats WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		chainTenant, goatID).Scan(&s); err != nil {
		t.Fatalf("read lifecycle: %v", err)
	}
	return s
}

// seedDirectorWithPhone makes ONE director reachable by push: a member, the tenant-scoped role
// grant the audience resolver reads, and a registered device. All three are external HRMS facts.
// Without a reachable device an upward notifier resolves nobody and logs a warning, so a test
// asserting the push would pass for the wrong reason -- or fail for a roster gap rather than a
// broken chain.
func seedDirectorWithPhone(t *testing.T, ctx context.Context, pool *pgxpool.Pool, memberID, userID, role string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIR', 'Chain Director', 'active', $4)
ON CONFLICT (workforce_member_id) DO NOTHING`, memberID, chainTenant, userID, role); err != nil {
		t.Fatalf("seed director: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, $3, 'tenant', $1::uuid, 'active', now() - interval '30 days')
ON CONFLICT DO NOTHING`, chainTenant, userID, role); err != nil {
		t.Fatalf("grant the director role: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_member_devices (tenant_id, workforce_member_id, app_install_id, app_version, fcm_token, platform, status, notifications_enabled)
VALUES ($1::uuid, $2::uuid, 'chain-install-' || left($2::text, 8), '1.0.0', 'chain-token-' || left($2::text, 8), 'android', 'active', true)
ON CONFLICT DO NOTHING`, chainTenant, memberID); err != nil {
		t.Fatalf("register the director's phone: %v", err)
	}
}

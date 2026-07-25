package kernelstages

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// TestHousekeepingStagesRunAgainstRealPostgres exercises the two daily
// housekeeping stages against a migration-complete Postgres. On empty tables
// they must run cleanly (0 rows deleted, no error), proving the stages reuse the
// real schema/queries and satisfy the StageRunner contract end to end.
func TestHousekeepingStagesRunAgainstRealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 5 * time.Second}, Logger: logger}

	processed := NewProcessedEventSweeperStage(deps, "")
	if got := processed.Name(); got != "domain-event-processed-sweeper" {
		t.Fatalf("unexpected processed sweeper name: %q", got)
	}
	if err := processed.Run(ctx); err != nil {
		t.Fatalf("processed-event sweeper stage failed against real Postgres: %v", err)
	}

	idempotency := NewIdempotencyKeySweeperStage(deps, "")
	if got := idempotency.Name(); got != "idempotency-key-sweeper" {
		t.Fatalf("unexpected idempotency sweeper name: %q", got)
	}
	if err := idempotency.Run(ctx); err != nil {
		t.Fatalf("idempotency-key sweeper stage failed against real Postgres: %v", err)
	}

	proof := NewProofRetentionSweeperStage(deps)
	if got := proof.Name(); got != "proof-retention-sweeper" {
		t.Fatalf("unexpected proof sweeper name: %q", got)
	}
	if err := proof.Run(ctx); err != nil {
		t.Fatalf("proof-retention sweeper stage failed against real Postgres: %v", err)
	}
}

// TestIdempotencyKeySweeperDeletesExpiredKeys inserts an expired and a live
// idempotency key, runs the housekeeping stage, and asserts only the expired one
// is deleted — proving the reused delete query targets the right rows.
func TestIdempotencyKeySweeperDeletesExpiredKeys(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 5 * time.Second}, Logger: logger}

	tenant := "11111111-1111-1111-1111-111111111111"
	// idempotency_keys.tenant_id has a NOT-NULL-optional FK to tenants; seed the
	// tenant so the insert satisfies the foreign key.
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'kernelstages-test', 'active') ON CONFLICT DO NOTHING`, tenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	expiredKey := "kernelstages-expired-key"
	liveKey := "kernelstages-live-key"
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status, expires_at)
VALUES ($1, $3::uuid, 'kernelstages-test', 'hash', 'started', $4),
       ($2, $3::uuid, 'kernelstages-test', 'hash', 'started', $5)`,
		expiredKey, liveKey, tenant, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("seed idempotency keys: %v", err)
	}

	if err := NewIdempotencyKeySweeperStage(deps, tenant).Run(ctx); err != nil {
		t.Fatalf("idempotency-key sweeper stage failed: %v", err)
	}

	var remaining []string
	rows, err := pool.Query(ctx, `SELECT idempotency_key FROM idempotency_keys WHERE tenant_id = $1::uuid ORDER BY idempotency_key`, tenant)
	if err != nil {
		t.Fatalf("query remaining keys: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan remaining key: %v", err)
		}
		remaining = append(remaining, key)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate remaining keys: %v", err)
	}

	if len(remaining) != 1 || remaining[0] != liveKey {
		t.Fatalf("expected only the live key to remain, got %v", remaining)
	}
}

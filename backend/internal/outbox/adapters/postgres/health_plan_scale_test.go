package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestOutboxHealthQueryPlanUsesIndexesAtScale is the at-scale plan proof for outboxHealthSQL
// (GET /operations/kernel-health). goatos-stg carries ~362k outbox rows for one tenant, almost all
// of them published; the former single FILTERed aggregate seq-scanned all of them (~4 s cold). This
// loads 500k rows with that shape ANALYZEs,
// and proves every scalar subquery is index-answered: the status counts and pending / failure stamps
// by outbox_messages_tenant_status_attempt_idx / replay_guard_idx, MAX(published_at) by 000428's
// outbox_messages_tenant_published_at_idx.
func TestOutboxHealthQueryPlanUsesIndexesAtScale(t *testing.T) {
	if os.Getenv("GOATOS_SCALE_CERT") == "" {
		t.Skip("scale certification gate — set GOATOS_SCALE_CERT=1 (make scale-cert)")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// One tenant, as on goatos-stg: 500k rows, ~96% published, 12.5k pending, 2k failed,
	// 1k dead letters, 1k publishing, 1k discarded. Each outbox row points at a real
	// goat_identity_events row (the outbox event-existence trigger enforces it).
	seedOutboxGoat(t, pool)
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identity_events (identity_event_id, tenant_id, goat_id, event_type, event_version,
  occurred_at, recorded_at, payload, idempotency_key)
SELECT ('30000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid, $1::uuid, $2::uuid,
       'goat.created', 1, now(), now(), '{}'::jsonb, 'outbox-scale-event-' || g
FROM generate_series(1, 500000) g`, meshaTenant, outboxGoatID); err != nil {
		t.Fatalf("seed 500k goat identity events: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO outbox_messages (tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, status, published_at, created_at, updated_at)
SELECT $1::uuid, ('30000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid, 'goat.created', '1.0.0',
       'goat', $2::uuid, 'identity.events', '{}'::jsonb, '{"source":"outbox-scale"}'::jsonb, 'outbox-scale-' || g,
       CASE WHEN g % 500 = 1 THEN 'dead_letter'
            WHEN g % 250 = 2 THEN 'failed'
            WHEN g % 500 = 3 THEN 'publishing'
            WHEN g % 500 = 4 THEN 'discarded'
            WHEN g % 40 = 5 THEN 'pending'
            ELSE 'published' END,
       CASE WHEN g % 500 IN (1, 3, 4) OR g % 250 = 2 OR g % 40 = 5 THEN NULL
            ELSE now() - (g || ' seconds')::interval END,
       now() - (g || ' seconds')::interval,
       now() - (g || ' seconds')::interval
FROM generate_series(1, 500000) g`, meshaTenant, outboxGoatID); err != nil {
		t.Fatalf("seed 500k outbox rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE outbox_messages`); err != nil {
		t.Fatalf("analyze outbox_messages: %v", err)
	}

	plan := pgtest.ExplainAnalyzeAtScale(t, ctx, pool, outboxHealthSQL, meshaTenant)
	plan.AssertNoSeqScan(t, "outboxHealthSQL @500k outbox rows", 200, "outbox_messages")
}

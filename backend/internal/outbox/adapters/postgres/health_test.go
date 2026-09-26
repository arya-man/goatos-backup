package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestOutboxHealthProbesMatchFilteredAggregate pins the 2026-09-19 rewrite of Health(): seven
// index-bound probes must answer exactly what the old single FILTERed aggregate answered -- the
// four status counts, the oldest pending/failure stamps, and the newest published stamp -- and the
// published MAX must ride outbox_messages_tenant_published_at_idx (migration 000428) rather than walk the
// 'published' bucket, which is ~the whole table on a live tenant.
func TestOutboxHealthProbesMatchFilteredAggregate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// meshaTenant + outboxGoatID: outbox_messages carries a trigger that requires the event to exist
	// in goat_identity_events for the tenant, so every row is registered there first (the same
	// discipline insertOutboxMessage in repository_integration_test.go follows).
	tenant := meshaTenant
	seedOutboxGoat(t, pool)
	base := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	insert := func(status string, createdAt, updatedAt time.Time, publishedAt *time.Time) {
		t.Helper()
		eventID := uuid.NewString()
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_identity_events (
  identity_event_id, tenant_id, goat_id, event_type, event_version, occurred_at, recorded_at, payload, idempotency_key
) VALUES ($1, $2, $3, 'goat.created', 1, $4, $4, '{}'::jsonb, $5)`,
			eventID, tenant, outboxGoatID, createdAt, "health-event-"+eventID); err != nil {
			t.Fatalf("insert identity event: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO outbox_messages (
  outbox_id, tenant_id, event_id, event_type, schema_version,
  aggregate_type, aggregate_id, topic, payload, headers, idempotency_key,
  trace_id, status, attempt_count, next_attempt_at, created_at, updated_at, published_at
) VALUES (
  $1, $2, $3, 'goat.created', '1.0.0',
  'goat', $4, 'identity.events', $5::jsonb, '{}'::jsonb, $6,
  $7, $8, 0, $9, $9, $10, $11
)`, uuid.NewString(), tenant, eventID, outboxGoatID, string(validEnvelopePayload(t, eventID, "trace-health")), "health-"+eventID,
			"trace-health", status, createdAt, updatedAt, publishedAt); err != nil {
			t.Fatalf("insert %s: %v", status, err)
		}
	}
	// Two pending (oldest at base), one publishing, one failed (updated base+3m), one dead_letter
	// (updated base+1m -> the oldest failure), three published with the newest at base+9m, and a
	// discarded row that must count nowhere.
	insert("pending", base, base, nil)
	insert("pending", base.Add(5*time.Minute), base.Add(5*time.Minute), nil)
	insert("publishing", base.Add(2*time.Minute), base.Add(2*time.Minute), nil)
	insert("failed", base.Add(3*time.Minute), base.Add(3*time.Minute), nil)
	insert("dead_letter", base.Add(1*time.Minute), base.Add(1*time.Minute), nil)
	for _, m := range []int{4, 9, 7} {
		at := base.Add(time.Duration(m) * time.Minute)
		insert("published", at, at, &at)
	}
	insert("discarded", base.Add(20*time.Minute), base.Add(20*time.Minute), nil)

	// GOLDEN: the seven probes must equal the single FILTERed aggregate they replaced, row for row.
	var (
		oldPending, oldPublishing, oldFailed, oldDead        int64
		oldOldestPending, oldOldestFailure, oldLastPublished *time.Time
	)
	if err := pool.QueryRow(ctx, `
SELECT
  COUNT(*) FILTER (WHERE status = 'pending')::bigint,
  COUNT(*) FILTER (WHERE status = 'publishing')::bigint,
  COUNT(*) FILTER (WHERE status = 'failed')::bigint,
  COUNT(*) FILTER (WHERE status = 'dead_letter')::bigint,
  MIN(created_at) FILTER (WHERE status = 'pending'),
  MIN(updated_at) FILTER (WHERE status IN ('failed', 'dead_letter')),
  MAX(published_at) FILTER (WHERE status = 'published')
FROM outbox_messages
WHERE tenant_id = $1::uuid`, tenant).Scan(&oldPending, &oldPublishing, &oldFailed, &oldDead, &oldOldestPending, &oldOldestFailure, &oldLastPublished); err != nil {
		t.Fatalf("legacy aggregate: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	health, err := repo.Health(ctx, tenant, base.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if health.PendingCount != oldPending || health.PublishingCount != oldPublishing || health.FailedCount != oldFailed || health.DeadLetterCount != oldDead {
		t.Fatalf("counts = pending %d publishing %d failed %d dead %d, legacy aggregate says %d/%d/%d/%d",
			health.PendingCount, health.PublishingCount, health.FailedCount, health.DeadLetterCount, oldPending, oldPublishing, oldFailed, oldDead)
	}
	sameStamp := func(a, b *time.Time) bool { return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b)) }
	if !sameStamp(health.OldestPendingAt, oldOldestPending) || !sameStamp(health.OldestFailureAt, oldOldestFailure) || !sameStamp(health.LastPublishedAt, oldLastPublished) {
		t.Fatalf("stamps = %v/%v/%v, legacy aggregate says %v/%v/%v", health.OldestPendingAt, health.OldestFailureAt, health.LastPublishedAt, oldOldestPending, oldOldestFailure, oldLastPublished)
	}
	// And the fixture itself must be visible through both: the seeded rows above are the floor.
	if health.PendingCount < 2 || health.PublishingCount < 1 || health.FailedCount < 1 || health.DeadLetterCount < 1 {
		t.Fatalf("fixture rows missing from health: %+v", health)
	}
	if health.OldestPendingAt == nil || health.OldestPendingAt.After(base) {
		t.Fatalf("oldest_pending_at = %v, want <= %v", health.OldestPendingAt, base)
	}
	if health.LastPublishedAt == nil || health.LastPublishedAt.Before(base.Add(9*time.Minute)) {
		t.Fatalf("last_published_at = %v, want >= %v", health.LastPublishedAt, base.Add(9*time.Minute))
	}
	if health.Status != "degraded" {
		t.Fatalf("status = %q, want degraded (failed/dead-letter rows present)", health.Status)
	}

	// An empty tenant answers zero counts and NULL stamps, exactly as the FILTERed aggregate did.
	empty, err := repo.Health(ctx, uuid.NewString(), base)
	if err != nil {
		t.Fatalf("empty health: %v", err)
	}
	if empty.PendingCount != 0 || empty.OldestPendingAt != nil || empty.OldestFailureAt != nil || empty.LastPublishedAt != nil || empty.Status != "healthy" {
		t.Fatalf("empty tenant health = %+v, want zero counts, nil stamps, healthy", empty)
	}

	// Plan guard: the published MAX must be served by the partial index from migration 000428, and
	// no probe may scan the table.
	rows, err := pool.Query(ctx, "EXPLAIN (COSTS OFF) "+outboxHealthSQL, tenant)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	plan := strings.Join(lines, "\n")
	if strings.Contains(plan, "Seq Scan on outbox_messages") {
		t.Fatalf("health probe scans outbox_messages:\n%s", plan)
	}
	if !strings.Contains(plan, "outbox_messages_tenant_published_at_idx") {
		t.Fatalf("published MAX does not use outbox_messages_tenant_published_at_idx (migration 000428):\n%s", plan)
	}
}

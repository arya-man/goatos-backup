// Package chaintest joins a producer to its consumer the way production does, so ONE test can
// prove a registered domain-event chain end to end.
//
// Why it exists. An audit of the 57 chains in context/architecture/domain-event-registry.json
// found the same shape over and over: a producer-side test that counts an outbox row, beside a
// consumer-side test that HAND-BUILDS the event and calls the handler directly. Both are green,
// and nothing joins them -- so deleting the producer's emission, or no-op'ing the handler, leaves
// every named proof passing while the event silently stops flowing. AGENTS.md already states the
// rule this restores: separate producer and consumer tests are not closure.
//
// What it does NOT do is as important. It seeds nothing, decides nothing, and knows no event type.
// It reads the rows the producer's own transaction committed to outbox_messages, decodes each one
// with eventbus.EventFromEnvelope -- the SAME decode internal/domainconsumer/app uses on a live
// Pub/Sub message -- and publishes it on the bus the production RegisterXConsumers wired. The
// derived business result therefore comes from the real handler, which is what
// tools/agent-hooks/check-e2e-kernel-integrity.sh requires of an E2E.
//
// It imports "testing" and is meant for _test.go callers only, mirroring platform/pgtest.
package chaintest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// pendingSQL reads the outbox exactly as the relay does: this tenant's undelivered rows, oldest
// first, so a chain that emits several events reaches its consumers in the order they were
// committed. The payload column holds the full domain-event envelope.
const pendingSQL = `
SELECT event_id::text, event_type, payload::text
FROM outbox_messages
WHERE tenant_id = $1::uuid AND status = 'pending'
ORDER BY created_at, event_id`

// Drain publishes every pending outbox row for tenantID onto bus and returns the event types it
// dispatched, in order. It marks each row published, so a second Drain in the same test carries
// only what the step under test newly emitted.
//
// It deliberately does NOT assert that anything was drained: a chain test asserts the CONSUMER's
// state changed, and a Drain that silently carries nothing is precisely the failure the
// consumer-side assertion must catch. Use DrainExpecting when the point of the step is that a
// particular event was emitted at all.
func Drain(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bus eventbus.Bus, tenantID string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, pendingSQL, tenantID)
	if err != nil {
		t.Fatalf("chaintest: read outbox: %v", err)
	}
	type pending struct {
		id, eventType, envelope string
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.eventType, &p.envelope); err != nil {
			rows.Close()
			t.Fatalf("chaintest: scan outbox row: %v", err)
		}
		batch = append(batch, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("chaintest: read outbox: %v", err)
	}
	delivered := make([]string, 0, len(batch))
	for _, p := range batch {
		// The production decode. A producer that writes an envelope this cannot read has a real
		// defect, so the failure is loud rather than a skipped row.
		event, err := eventbus.EventFromEnvelope([]byte(p.envelope), eventbus.Event{
			ID: p.id, Type: p.eventType, TenantID: tenantID,
		})
		if err != nil {
			t.Fatalf("chaintest: decode %s envelope: %v", p.eventType, err)
		}
		if err := bus.Publish(ctx, event); err != nil {
			t.Fatalf("chaintest: consumer of %s failed: %v", p.eventType, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE outbox_messages SET status = 'published' WHERE event_id = $1::uuid`, p.id); err != nil {
			t.Fatalf("chaintest: mark delivered: %v", err)
		}
		delivered = append(delivered, p.eventType)
	}
	return delivered
}

// DrainExpecting drains and fails unless eventType was among the events carried. It is the
// producer half of a chain assertion: deleting the emission turns THIS red, while the consumer
// state assertion that follows turns red when the handler is no-op'd. One test, both directions.
func DrainExpecting(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bus eventbus.Bus, tenantID, eventType string) []string {
	t.Helper()
	delivered := Drain(t, ctx, pool, bus, tenantID)
	for _, got := range delivered {
		if got == eventType {
			return delivered
		}
	}
	t.Fatalf("chaintest: the producer emitted no %s; the outbox carried %v.\n"+
		"The chain is broken at the PRODUCER: nothing reached the consumer, so no downstream assertion can be trusted.", eventType, delivered)
	return delivered
}

package app_test

// This file lives in the app_test package (not app) so it can import the real Postgres adapter
// (internal/domainconsumer/adapters/postgres) without an import cycle - that adapter package
// imports domainconsumer/app.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	consumerpg "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/postgres"
	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestServiceDoesNotReplayHandlerSideEffectsAfterMarkProcessedFailureAndRedelivery is the real-
// Postgres reproduction of C35-024: a handler side effect commits, the terminal MarkProcessed call
// fails once (a transient finalize error, injected via a thin decorator around the real store), and
// a redelivery of the exact same event must NOT re-invoke bus.Publish - it must reclaim the durable
// 'effects_committed' marker written by the real adapter and only retry the finalize.
//
// Before the fix this test fails: the fault-injected MarkProcessed failure causes the consumer to
// call MarkFailed, which sets status='failed'; BeginProcessing's reclaim then treats 'failed' rows
// as safe to fully retry and calls bus.Publish again on redelivery, so the handler runs twice.
func TestServiceDoesNotReplayHandlerSideEffectsAfterMarkProcessedFailureAndRedelivery(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	baseStore := consumerpg.NewProcessedEventStore(pool, 5*time.Second)
	store := &flakyMarkProcessedStore{ProcessedEventStore: baseStore, remainingFailures: 1}

	bus := eventbus.NewInProcessBus()
	calls := 0
	bus.Subscribe("goat.created", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error {
		calls++
		return nil
	}))
	service := consumerapp.NewService(bus, integrationValidator(t)).WithProcessedEventStore(store)

	const eventID = "60000000-0000-4000-8000-000000009001"
	message := consumerapp.Message{
		ID:   "msg-replay-guard-pg-1",
		Data: integrationEnvelope(t, eventID, "goat.created", "10000000-0000-4000-8000-000000009001"),
		Attributes: map[string]string{
			"event_type": "goat.created",
			"tenant_id":  "00000000-0000-4000-8000-000000000001",
		},
	}

	// First delivery: handler side effect commits, but the finalize (MarkProcessed) fails.
	if err := service.HandleMessage(ctx, message); err == nil {
		t.Fatal("expected first delivery to surface the synthetic finalize failure")
	}
	if calls != 1 {
		t.Fatalf("calls after first delivery = %d, want 1", calls)
	}
	status := scanStatus(t, ctx, pool, eventID)
	if status != "effects_committed" {
		t.Fatalf("status after first delivery = %q, want effects_committed (must not fall back to failed)", status)
	}

	// Redelivery of the exact same message must NOT re-invoke the handler: the side effect was
	// already durably committed, so only the finalize retry should run.
	message.DeliveryAttempt = 1
	if err := service.HandleMessage(ctx, message); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls after redelivery = %d, want 1 (handler must not replay)", calls)
	}
	status = scanStatus(t, ctx, pool, eventID)
	if status != "processed" {
		t.Fatalf("status after redelivery = %q, want processed", status)
	}
}

// flakyMarkProcessedStore wraps the real Postgres-backed store and fails the first N calls to
// MarkProcessed, simulating a transient finalize error (e.g. a dropped connection) without ever
// touching what the real BeginProcessing/MarkEffectsCommitted/MarkFailed SQL actually persists.
type flakyMarkProcessedStore struct {
	*consumerpg.ProcessedEventStore
	remainingFailures int
}

func (s *flakyMarkProcessedStore) MarkProcessed(ctx context.Context, event consumerapp.ProcessedEvent) error {
	if s.remainingFailures > 0 {
		s.remainingFailures--
		return errors.New("synthetic transient finalize failure")
	}
	return s.ProcessedEventStore.MarkProcessed(ctx, event)
}

func scanStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM domain_event_processed_events WHERE subscription_id = 'direct' AND event_id = $1`, eventID).Scan(&status); err != nil {
		t.Fatalf("scan status: %v", err)
	}
	return status
}

func integrationValidator(t *testing.T) *outboxapp.EnvelopeValidator {
	t.Helper()
	validator, err := outboxapp.NewEnvelopeValidator(filepath.Join("..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	return validator
}

func integrationEnvelope(t *testing.T, eventID, eventType, aggregateID string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"event_id":        eventID,
		"event_type":      eventType,
		"schema_version":  "1.0.0",
		"schema_ref":      "contracts/jsonschema/domain-event-envelope.schema.json",
		"aggregate_type":  "goat",
		"aggregate_id":    aggregateID,
		"occurred_at":     "2026-06-27T01:02:03.000000Z",
		"recorded_at":     "2026-06-27T01:02:04.000000Z",
		"producer":        map[string]any{"service": "goatos-test", "module": "domainconsumer"},
		"idempotency_key": "tenant:domain-consumer-test:" + eventID,
		"actor":           map[string]any{"actor_type": "system_rule", "actor_id": nil, "actor_ref": "test"},
		"subject_type":    "goat",
		"subject_id":      aggregateID,
		"visibility_scope": map[string]any{
			"tenant_id": "00000000-0000-4000-8000-000000000001",
		},
		"evidence_refs": []map[string]string{},
		"payload": map[string]any{
			"goat_id":    aggregateID,
			"scope_type": "shed",
			"scope_id":   "00000000-0000-4000-8000-000000004001",
		},
		"trace_id": "trace-domain-consumer-integration-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

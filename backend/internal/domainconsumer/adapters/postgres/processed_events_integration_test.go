package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestProcessedEventStoreDecisions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	store := NewProcessedEventStore(pool, 5*time.Second)
	event := consumerapp.ProcessedEvent{
		TenantID:        "00000000-0000-4000-8000-000000000001",
		EventID:         "60000000-0000-4000-8000-000000000101",
		EventType:       "goat.created",
		SubscriptionID:  "goatos-dev-domain-events",
		MessageID:       "msg-101",
		DeliveryAttempt: 1,
		Now:             time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC),
	}
	decision, err := store.BeginProcessing(ctx, event)
	if err != nil {
		t.Fatalf("BeginProcessing first: %v", err)
	}
	if decision != consumerapp.ProcessDecisionClaimed {
		t.Fatalf("first decision=%s want claimed", decision)
	}
	decision, err = store.BeginProcessing(ctx, event)
	if err != nil {
		t.Fatalf("BeginProcessing active retry: %v", err)
	}
	if decision != consumerapp.ProcessDecisionInProgress {
		t.Fatalf("active retry decision=%s want in_progress", decision)
	}
	if err := store.MarkProcessed(ctx, event); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	decision, err = store.BeginProcessing(ctx, event)
	if err != nil {
		t.Fatalf("BeginProcessing processed retry: %v", err)
	}
	if decision != consumerapp.ProcessDecisionAlreadyProcessed {
		t.Fatalf("processed retry decision=%s want already_processed", decision)
	}

	staleEvent := event
	staleEvent.EventID = "60000000-0000-4000-8000-000000000103"
	staleEvent.MessageID = "msg-103"
	staleEvent.Now = time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	decision, err = store.BeginProcessing(ctx, staleEvent)
	if err != nil || decision != consumerapp.ProcessDecisionClaimed {
		t.Fatalf("stale first decision=%s err=%v want claimed", decision, err)
	}
	activeRetry := staleEvent
	activeRetry.MessageID = "msg-103-redelivery-active"
	activeRetry.DeliveryAttempt = 2
	activeRetry.Now = staleEvent.Now.Add(5 * time.Minute)
	decision, err = store.BeginProcessing(ctx, activeRetry)
	if err != nil {
		t.Fatalf("BeginProcessing active retry: %v", err)
	}
	if decision != consumerapp.ProcessDecisionInProgress {
		t.Fatalf("active retry decision=%s want in_progress", decision)
	}
	staleRetry := staleEvent
	staleRetry.MessageID = "msg-103-redelivery-stale"
	staleRetry.DeliveryAttempt = 3
	staleRetry.Now = staleEvent.Now.Add(20 * time.Minute)
	decision, err = store.BeginProcessing(ctx, staleRetry)
	if err != nil {
		t.Fatalf("BeginProcessing stale retry: %v", err)
	}
	if decision != consumerapp.ProcessDecisionClaimed {
		t.Fatalf("stale retry decision=%s want claimed", decision)
	}

	failedEvent := event
	failedEvent.EventID = "60000000-0000-4000-8000-000000000102"
	decision, err = store.BeginProcessing(ctx, failedEvent)
	if err != nil || decision != consumerapp.ProcessDecisionClaimed {
		t.Fatalf("failed first decision=%s err=%v want claimed", decision, err)
	}
	if err := store.MarkFailed(ctx, failedEvent, "temporary handler failure"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	decision, err = store.BeginProcessing(ctx, failedEvent)
	if err != nil {
		t.Fatalf("BeginProcessing failed retry: %v", err)
	}
	if decision != consumerapp.ProcessDecisionClaimed {
		t.Fatalf("failed retry decision=%s want claimed", decision)
	}
}

func TestProcessedEventStoreFinalizationRequiresActiveProcessingClaim(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	store := NewProcessedEventStore(pool, 5*time.Second)
	event := consumerapp.ProcessedEvent{
		TenantID:        "00000000-0000-4000-8000-000000000001",
		EventID:         "60000000-0000-4000-8000-000000000201",
		EventType:       "goat.created",
		SubscriptionID:  "goatos-dev-domain-events",
		MessageID:       "msg-201",
		DeliveryAttempt: 1,
		Now:             time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC),
	}
	decision, err := store.BeginProcessing(ctx, event)
	if err != nil || decision != consumerapp.ProcessDecisionClaimed {
		t.Fatalf("BeginProcessing decision=%s err=%v want claimed", decision, err)
	}
	if err := store.MarkProcessed(ctx, event); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	if err := store.MarkProcessed(ctx, event); !errors.Is(err, consumerapp.ErrProcessedEventFinalizationLost) {
		t.Fatalf("MarkProcessed on finalized row err=%v, want ErrProcessedEventFinalizationLost", err)
	}
	if err := store.MarkFailed(ctx, event, "late failure"); !errors.Is(err, consumerapp.ErrProcessedEventFinalizationLost) {
		t.Fatalf("MarkFailed on finalized row err=%v, want ErrProcessedEventFinalizationLost", err)
	}

	missing := event
	missing.EventID = "60000000-0000-4000-8000-000000000202"
	if err := store.MarkProcessed(ctx, missing); !errors.Is(err, consumerapp.ErrProcessedEventFinalizationLost) {
		t.Fatalf("MarkProcessed on missing row err=%v, want ErrProcessedEventFinalizationLost", err)
	}
	if err := store.MarkFailed(ctx, missing, "missing"); !errors.Is(err, consumerapp.ErrProcessedEventFinalizationLost) {
		t.Fatalf("MarkFailed on missing row err=%v, want ErrProcessedEventFinalizationLost", err)
	}
}

func TestProcessedEventStoreSweepProcessedBeforeDeletesOnlyOldProcessedRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	store := NewProcessedEventStore(pool, 5*time.Second)
	tenantID := "00000000-0000-4000-8000-000000000001"
	subscriptionID := "goatos-dev-domain-events"
	before := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

	mk := func(id string, now time.Time) consumerapp.ProcessedEvent {
		return consumerapp.ProcessedEvent{
			TenantID:        tenantID,
			EventID:         id,
			EventType:       "goat.created",
			SubscriptionID:  subscriptionID,
			MessageID:       "msg-" + id,
			DeliveryAttempt: 1,
			Now:             now,
		}
	}
	oldProcessed := mk("processed-old", before.Add(-time.Hour))
	futureProcessed := mk("processed-future", before.Add(time.Hour))
	oldFailed := mk("failed-old", before.Add(-2*time.Hour))
	oldProcessing := mk("processing-old", before.Add(-3*time.Hour))

	for _, event := range []consumerapp.ProcessedEvent{oldProcessed, futureProcessed, oldFailed, oldProcessing} {
		decision, err := store.BeginProcessing(ctx, event)
		if err != nil || decision != consumerapp.ProcessDecisionClaimed {
			t.Fatalf("BeginProcessing %s decision=%s err=%v, want claimed", event.EventID, decision, err)
		}
	}
	if err := store.MarkProcessed(ctx, oldProcessed); err != nil {
		t.Fatalf("MarkProcessed old: %v", err)
	}
	if err := store.MarkProcessed(ctx, futureProcessed); err != nil {
		t.Fatalf("MarkProcessed future: %v", err)
	}
	if err := store.MarkFailed(ctx, oldFailed, "handler failed"); err != nil {
		t.Fatalf("MarkFailed old: %v", err)
	}

	dryRunCount, err := store.SweepProcessedBefore(ctx, tenantID, before, 10, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dryRunCount != 1 {
		t.Fatalf("dry run count = %d, want 1", dryRunCount)
	}
	if got := countProcessedEvents(t, ctx, pool); got != 4 {
		t.Fatalf("dry run deleted rows, count = %d", got)
	}

	deleted, err := store.SweepProcessedBefore(ctx, tenantID, before, 10, false)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if got := countProcessedEvents(t, ctx, pool); got != 3 {
		t.Fatalf("remaining rows = %d, want 3", got)
	}
	for _, event := range []consumerapp.ProcessedEvent{futureProcessed, oldFailed, oldProcessing} {
		if got := countProcessedEvent(t, ctx, pool, event.EventID); got != 1 {
			t.Fatalf("event %s count = %d, want 1", event.EventID, got)
		}
	}
	if got := countProcessedEvent(t, ctx, pool, oldProcessed.EventID); got != 0 {
		t.Fatalf("old processed event count = %d, want 0", got)
	}
}

func countProcessedEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM domain_event_processed_events`).Scan(&count); err != nil {
		t.Fatalf("count processed events: %v", err)
	}
	return count
}

func countProcessedEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM domain_event_processed_events WHERE event_id = $1`, eventID).Scan(&count); err != nil {
		t.Fatalf("count event %s: %v", eventID, err)
	}
	return count
}

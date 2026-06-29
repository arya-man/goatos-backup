package postgres

import (
	"context"
	"testing"
	"time"

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
	if decision != consumerapp.ProcessDecisionInProgress {
		t.Fatalf("stale retry decision=%s want in_progress", decision)
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

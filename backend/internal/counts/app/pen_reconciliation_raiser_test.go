package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

type fakePenReconciliationRaiseRepo struct {
	calls []domain.PenReconciliationRaiseCommand
	out   int
	err   error
}

func (f *fakePenReconciliationRaiseRepo) RaisePenReconciliationCards(
	_ context.Context, in domain.PenReconciliationRaiseCommand,
) (int, error) {
	f.calls = append(f.calls, in)
	return f.out, f.err
}

// TestPenReconciliationRaiserRaisesForTheSubmittedBucket pins the trigger contract: one
// weighing.shed_submission.completed event asks the repository to reconcile exactly that
// tenant's bucket, anchored to the submit instant.
func TestPenReconciliationRaiserRaisesForTheSubmittedBucket(t *testing.T) {
	repo := &fakePenReconciliationRaiseRepo{out: 2}
	raiser := NewPenReconciliationRaiser(repo, nil, func() time.Time {
		return time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	})

	event := eventbus.Event{
		ID: "event-1", Type: EventWeighingShedSubmissionCompletedForReconciliation,
		TenantID: "tenant-1", OccurredAt: time.Now(),
		Payload: []byte(`{"tenant_id":"tenant-1","campaign_id":"campaign-1","campaign_shed_id":"bucket-1","completed_at":"2026-09-02T09:30:00Z"}`),
	}
	if err := raiser.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(repo.calls) != 1 {
		t.Fatalf("raise calls = %d, want 1", len(repo.calls))
	}
	got := repo.calls[0]
	if got.TenantID != "tenant-1" || got.CampaignID != "campaign-1" || got.CampaignShedID != "bucket-1" {
		t.Fatalf("raise command = %+v", got)
	}
	if !got.RaisedAt.Equal(time.Date(2026, 9, 2, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("raised_at = %v, want the event's completed_at", got.RaisedAt)
	}
}

// TestPenReconciliationRaiserIgnoresOtherEventsAndEmptyBuckets pins that foreign event types
// and payloads naming no bucket raise nothing — and return nil rather than retrying forever.
func TestPenReconciliationRaiserIgnoresOtherEventsAndEmptyBuckets(t *testing.T) {
	repo := &fakePenReconciliationRaiseRepo{}
	raiser := NewPenReconciliationRaiser(repo, nil, nil)

	if err := raiser.HandleEvent(context.Background(), eventbus.Event{
		Type: "weighing.shed.reopened", TenantID: "tenant-1", Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("foreign event: %v", err)
	}
	if err := raiser.HandleEvent(context.Background(), eventbus.Event{
		Type: EventWeighingShedSubmissionCompletedForReconciliation, TenantID: "tenant-1",
		Payload: []byte(`{"tenant_id":"tenant-1"}`),
	}); err != nil {
		t.Fatalf("bucketless payload: %v", err)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("raise calls = %d, want 0", len(repo.calls))
	}
}

// TestPenReconciliationRaiserFallsBackToEnvelopeTenant pins that a payload missing tenant_id
// still reconciles under the envelope's tenant rather than dropping the event.
func TestPenReconciliationRaiserFallsBackToEnvelopeTenant(t *testing.T) {
	repo := &fakePenReconciliationRaiseRepo{}
	raiser := NewPenReconciliationRaiser(repo, nil, nil)
	if err := raiser.HandleEvent(context.Background(), eventbus.Event{
		Type: EventWeighingShedSubmissionCompletedForReconciliation, TenantID: "tenant-env",
		Payload: []byte(`{"campaign_shed_id":"bucket-9"}`),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(repo.calls) != 1 || repo.calls[0].TenantID != "tenant-env" {
		t.Fatalf("raise calls = %+v, want one under tenant-env", repo.calls)
	}
}

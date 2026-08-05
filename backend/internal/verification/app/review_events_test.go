package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

type fakeReviewEventRepo struct {
	inserted []domain.ReviewEvent
}

func (f *fakeReviewEventRepo) InsertReviewEvents(_ context.Context, batch domain.ReviewEventBatch) (int, error) {
	before := len(f.inserted)
	seen := map[string]bool{}
	for _, e := range f.inserted {
		seen[e.ClientEventID] = true
	}
	for _, e := range batch.Events {
		if seen[e.ClientEventID] {
			continue
		}
		f.inserted = append(f.inserted, e)
		seen[e.ClientEventID] = true
	}
	return len(f.inserted) - before, nil
}

func (f *fakeReviewEventRepo) ItemReviewFacts(_ context.Context, _, _ string) ([]domain.ItemReviewFacts, error) {
	return nil, nil
}

func newServiceWithItem(t *testing.T, category string) (*Service, *fakeRepo, domain.Item) {
	t.Helper()
	repo := newFakeRepo()
	svc := NewService(repo, fakeMedia{})
	if err := svc.RegisterCategory(domain.CategoryDefinition{
		Vertical: "preventive_care", Module: "vaccination", Category: category,
		NavigationModule: "vaccination", NavigationModuleLabel: "Vaccination",
		PageKey: "review-events-test", PageLabel: "Review events test",
	}); err != nil {
		t.Fatalf("register category: %v", err)
	}
	created, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: category,
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now(),
		IdempotencyKey: "idem-" + category,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	return svc, repo, created.Item
}

func validBatchInput(itemID string) ReviewEventBatchInput {
	return ReviewEventBatchInput{
		ItemID:        itemID,
		SessionID:     "sess-1",
		EventType:     string(domain.ReviewEventItemOpened),
		OccurredAt:    time.Now().Format(time.RFC3339),
		ClientEventID: "00000000-0000-4000-a000-000000000001",
	}
}

func TestRecordReviewEventsRejectsItemOutsideAuthorizedCategories(t *testing.T) {
	svc, _, item := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}

	_, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001",
		[]string{"weighing_proof"}, // authorized for a DIFFERENT category than the item's
		[]ReviewEventBatchInput{validBatchInput(item.ItemID)})

	appErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T (%v)", err, err)
	}
	if appErr.HTTPStatus != 403 {
		t.Fatalf("status = %d, want 403 for out-of-scope item", appErr.HTTPStatus)
	}
	if len(reviewRepo.inserted) != 0 {
		t.Fatal("no events should be persisted when authorization fails")
	}
}

func TestRecordReviewEventsAllowsItemInAuthorizedCategory(t *testing.T) {
	svc, _, item := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}

	inserted, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001",
		[]string{"vaccination_proof"},
		[]ReviewEventBatchInput{validBatchInput(item.ItemID)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("inserted = %d, want 1", inserted)
	}
}

func TestRecordReviewEventsIdempotentReplayInsertsNothingNew(t *testing.T) {
	svc, _, item := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}
	input := []ReviewEventBatchInput{validBatchInput(item.ItemID)}

	first, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001", nil, input)
	if err != nil || first != 1 {
		t.Fatalf("first call: inserted=%d err=%v, want 1/nil", first, err)
	}
	replay, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001", nil, input)
	if err != nil {
		t.Fatalf("replay call: %v", err)
	}
	if replay != 0 {
		t.Fatalf("replay inserted = %d, want 0 (idempotent replay of same client_event_id)", replay)
	}
}

func TestRecordReviewEventsRejectsBatchOverCap(t *testing.T) {
	svc, _, item := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}
	inputs := make([]ReviewEventBatchInput, MaxReviewEventBatch+1)
	for i := range inputs {
		in := validBatchInput(item.ItemID)
		in.ClientEventID = uuidutilNth(i)
		inputs[i] = in
	}
	_, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001", nil, inputs)
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "batch_too_large" {
		t.Fatalf("expected batch_too_large error, got %v", err)
	}
}

func TestRecordReviewEventsRejectsFutureOccurredAt(t *testing.T) {
	svc, _, item := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}
	in := validBatchInput(item.ItemID)
	in.OccurredAt = time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	_, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001", nil, []ReviewEventBatchInput{in})
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "occurred_at_in_future" {
		t.Fatalf("expected occurred_at_in_future error, got %v", err)
	}
}

func TestRecordReviewEventsRejectsUnknownEventType(t *testing.T) {
	svc, _, item := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}
	in := validBatchInput(item.ItemID)
	in.EventType = "not_a_real_event"
	_, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001", nil, []ReviewEventBatchInput{in})
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "invalid_event_type" {
		t.Fatalf("expected invalid_event_type error, got %v", err)
	}
}

// uuidutilNth mints a deterministic, distinct UUID-shaped string for cap-test fan-out.
func uuidutilNth(i int) string {
	return fmt.Sprintf("00000000-0000-4000-c000-%012d", i)
}

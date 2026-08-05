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

func TestReviewEventsIngestRejectsItemOutsideAuthorizedCategories(t *testing.T) {
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

func TestReviewEventsIngestAllowsItemInAuthorizedCategory(t *testing.T) {
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

func TestReviewEventsIngestIdempotentReplayInsertsNothingNew(t *testing.T) {
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

func TestReviewEventsIngestRejectsBatchOverCap(t *testing.T) {
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

func TestReviewEventsIngestRejectsFutureOccurredAt(t *testing.T) {
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

func TestReviewEventsIngestRejectsUnknownEventType(t *testing.T) {
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

func strPtr(s string) *string { return &s }

// TestRecordReviewEventsMixedBatchInsertsQueueAndItemScopedEvents is the real bug report
// (2026-08-06): a queue_opened event fires before any item exists and must NOT carry an item_id.
// A batch mixing one null-item queue_opened with an item-scoped video_play must insert BOTH.
func TestReviewEventsIngestMixedBatchInsertsQueueAndItemScopedEvents(t *testing.T) {
	svc, _, item := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}

	queueOpened := ReviewEventBatchInput{
		// ItemID intentionally empty -- this is the exact shape the browser sends for
		// queue_opened, and the exact shape that used to 400 invalid_json.
		SessionID:     "sess-1",
		EventType:     string(domain.ReviewEventQueueOpened),
		OccurredAt:    time.Now().Format(time.RFC3339),
		ClientEventID: "00000000-0000-4000-a000-000000000010",
		Payload:       domain.ReviewEventPayload{Category: strPtr("vaccination_proof"), Status: strPtr("pending")},
	}
	itemScoped := validBatchInput(item.ItemID)
	itemScoped.EventType = string(domain.ReviewEventVideoPlay)
	itemScoped.ClientEventID = "00000000-0000-4000-a000-000000000011"

	inserted, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001",
		nil, []ReviewEventBatchInput{queueOpened, itemScoped})
	if err != nil {
		t.Fatalf("mixed batch: unexpected error: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("inserted = %d, want 2 (queue-scoped + item-scoped both land)", inserted)
	}
	if len(reviewRepo.inserted) != 2 {
		t.Fatalf("stored events = %d, want 2", len(reviewRepo.inserted))
	}
	var sawQueueScoped, sawItemScoped bool
	for _, e := range reviewRepo.inserted {
		if e.EventType == domain.ReviewEventQueueOpened {
			if e.ItemID != nil {
				t.Fatalf("queue_opened event stored with non-nil item_id: %v", *e.ItemID)
			}
			sawQueueScoped = true
		}
		if e.EventType == domain.ReviewEventVideoPlay {
			if e.ItemID == nil || *e.ItemID != item.ItemID {
				t.Fatalf("video_play event stored with wrong/nil item_id: %v", e.ItemID)
			}
			sawItemScoped = true
		}
	}
	if !sawQueueScoped || !sawItemScoped {
		t.Fatalf("expected both a queue-scoped and an item-scoped row, got queue=%v item=%v", sawQueueScoped, sawItemScoped)
	}
}

// TestRecordReviewEventsRejectsItemScopedEventWithNullItemID is the item-scoped half of the
// nullability rule: item_opened/video_play/etc MUST carry an item_id; a missing one is a precise
// field error, not a silent acceptance and not invalid_json.
func TestReviewEventsIngestRejectsItemScopedEventWithNullItemID(t *testing.T) {
	svc, _, _ := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}
	in := ReviewEventBatchInput{
		SessionID: "sess-1", EventType: string(domain.ReviewEventVideoPlay),
		OccurredAt: time.Now().Format(time.RFC3339), ClientEventID: "00000000-0000-4000-a000-000000000020",
		// ItemID intentionally empty.
	}
	_, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001", nil, []ReviewEventBatchInput{in})
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "invalid_item_id" {
		t.Fatalf("expected invalid_item_id error, got %v", err)
	}
	if len(appErr.FieldErrors) != 1 || appErr.FieldErrors[0].Field != "item_id" {
		t.Fatalf("expected a field_errors entry naming item_id, got %+v", appErr.FieldErrors)
	}
	if len(reviewRepo.inserted) != 0 {
		t.Fatal("no events should be persisted when validation fails")
	}
}

// TestRecordReviewEventsRejectsQueueOpenedWithBogusItemID is the exact reported failure mode: a
// queue_opened event carrying the literal placeholder string "queue" as item_id must be rejected
// with a precise field error naming item_id -- never the generic invalid_json the coordinator
// found costing real debugging time.
func TestReviewEventsIngestRejectsQueueOpenedWithBogusItemID(t *testing.T) {
	svc, _, _ := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}
	in := ReviewEventBatchInput{
		ItemID: "queue", SessionID: "sess-1", EventType: string(domain.ReviewEventQueueOpened),
		OccurredAt: time.Now().Format(time.RFC3339), ClientEventID: "00000000-0000-4000-a000-000000000021",
		Payload: domain.ReviewEventPayload{Category: strPtr("vaccination_proof")},
	}
	_, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001", nil, []ReviewEventBatchInput{in})
	appErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T (%v)", err, err)
	}
	if appErr.Code == "invalid_json" {
		t.Fatalf("must not answer invalid_json for a well-formed body with a bad item_id, got %v", appErr)
	}
	if appErr.Code != "invalid_item_id" {
		t.Fatalf("expected invalid_item_id error, got %v", appErr)
	}
	if len(appErr.FieldErrors) != 1 || appErr.FieldErrors[0].Field != "item_id" {
		t.Fatalf("expected a field_errors entry naming item_id, got %+v", appErr.FieldErrors)
	}
	if len(reviewRepo.inserted) != 0 {
		t.Fatal("no events should be persisted when validation fails")
	}
}

// TestRecordReviewEventsRejectsQueueOpenedMissingCategory ensures the funnel-attribution
// requirement (payload.category) is actually enforced, not merely documented.
func TestReviewEventsIngestRejectsQueueOpenedMissingCategory(t *testing.T) {
	svc, _, _ := newServiceWithItem(t, "vaccination_proof")
	reviewRepo := &fakeReviewEventRepo{}
	in := ReviewEventBatchInput{
		SessionID: "sess-1", EventType: string(domain.ReviewEventQueueOpened),
		OccurredAt: time.Now().Format(time.RFC3339), ClientEventID: "00000000-0000-4000-a000-000000000022",
	}
	_, err := svc.RecordReviewEvents(context.Background(), reviewRepo, testTenant, "00000000-0000-4000-b000-000000000001", nil, []ReviewEventBatchInput{in})
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "invalid_payload" {
		t.Fatalf("expected invalid_payload error, got %v", err)
	}
}

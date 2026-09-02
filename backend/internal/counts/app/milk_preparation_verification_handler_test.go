package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

type verdictStoreStub struct {
	approved, reworked domain.MilkPreparationVerdictCommand
}

func (*verdictStoreStub) SubmitMilkPreparation(context.Context, domain.MilkPreparationSubmission) (domain.MilkPreparationSubmissionResult, error) {
	return domain.MilkPreparationSubmissionResult{}, nil
}
func (s *verdictStoreStub) ApplyVerifiedMilkPreparation(_ context.Context, in domain.MilkPreparationVerdictCommand) (bool, error) {
	s.approved = in
	return true, nil
}
func (s *verdictStoreStub) BounceMilkPreparationForRework(_ context.Context, in domain.MilkPreparationVerdictCommand) (bool, error) {
	s.reworked = in
	return true, nil
}
func TestMilkPreparationVerdictApprovesOnlyItsOwnRefType(t *testing.T) {
	store := &verdictStoreStub{}
	handler := NewMilkPreparationVerificationHandler(store)
	event := eventbus.Event{ID: "event-1", Type: eventMilkPreparationVerdictApproved, TenantID: "tenant", OccurredAt: time.Now(), Payload: []byte(`{"verified_by":"verifier","source":{"module":"milk_preparation","ref_type":"milk_preparation_completion","ref_id":"completion-1"}}`)}
	if err := handler.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.approved.CompletionID != "completion-1" || store.approved.VerifiedBy != "verifier" {
		t.Fatalf("command=%+v", store.approved)
	}
	event.Payload = []byte(`{"verified_by":"verifier","source":{"module":"feed","ref_type":"feed_packing_completion","ref_id":"wrong"}}`)
	if err := handler.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.approved.CompletionID != "completion-1" {
		t.Fatalf("foreign verdict was consumed: %+v", store.approved)
	}
}

// TestMilkPreparationVerdictHasNoFeedStockFanOut pins the 2026-09-02 removal of the 2026-08-22
// recorder seam. UHT stock depletes from the preparation itself, on SUBMIT, through the
// feed_effective_external_consumption view (maintainer decision 2026-08-27, migration 000216), so
// this handler must apply the verdict and NOTHING else. The retired seam wrote a second
// feed_external_consumption row on approve, keyed at preparation_date while the view books the
// same milk at feeding_date, and double-deducted it. This test is the structural half -- the
// handler's dependency surface is exactly the completion store -- and
// TestKernelStory_UhtMilkStockDepletesOnce is the behavioural half on a real database.
func TestMilkPreparationVerdictHasNoFeedStockFanOut(t *testing.T) {
	handlerType := reflect.TypeOf(MilkPreparationVerificationHandler{})
	if handlerType.NumField() != 1 || handlerType.Field(0).Name != "store" {
		t.Fatalf("handler fields = %+v, want exactly the completion store: a second dependency here "+
			"is a feed-stock (or other) fan-out, and the workflow already owns the UHT fact", handlerType)
	}
	// An approve applies the verdict through the store and returns; nothing else is reachable.
	store := &verdictStoreStub{}
	handler := NewMilkPreparationVerificationHandler(store)
	event := eventbus.Event{ID: "event-1", Type: eventMilkPreparationVerdictApproved, TenantID: "tenant", OccurredAt: time.Now(),
		Payload: []byte(`{"verified_by":"verifier","source":{"module":"milk_preparation","ref_type":"milk_preparation_completion","ref_id":"completion-1"}}`)}
	if err := handler.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.approved.CompletionID != "completion-1" {
		t.Fatalf("approve must still apply: %+v", store.approved)
	}
}

package app

import (
	"context"
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

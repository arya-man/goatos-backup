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
	consumption        domain.MilkPreparationUHTConsumption
	consumptionOK      bool
	consumptionReads   int
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
func (s *verdictStoreStub) VerifiedUHTConsumption(context.Context, string, string) (domain.MilkPreparationUHTConsumption, bool, error) {
	s.consumptionReads++
	return s.consumption, s.consumptionOK, nil
}

type uhtRecorderStub struct {
	recorded []domain.MilkPreparationUHTConsumption
	fail     error
}

func (r *uhtRecorderStub) RecordVerifiedUHTConsumption(_ context.Context, in domain.MilkPreparationUHTConsumption) error {
	if r.fail != nil {
		return r.fail
	}
	r.recorded = append(r.recorded, in)
	return nil
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

// TestMilkPreparationApproveForwardsUHTConsumptionToFeedStock pins the
// 2026-08-22 decision that the app's verified UHT answer is the feed stock
// ledger's consumption source: an APPROVE forwards the litres (duplicates
// included, so at-least-once delivery converges), a REWORK forwards nothing,
// and a recorder failure fails the handler so the bus redelivers.
func TestMilkPreparationApproveForwardsUHTConsumptionToFeedStock(t *testing.T) {
	approvedPayload := []byte(`{"verified_by":"verifier","source":{"module":"milk_preparation","ref_type":"milk_preparation_completion","ref_id":"completion-1"}}`)
	fact := domain.MilkPreparationUHTConsumption{
		TenantID: "tenant", ParkID: "park-1", CompletionID: "completion-1",
		PreparationDate: "2026-08-22", AttemptNo: 2, UHTMilkQuantityLitres: 28,
	}

	t.Run("ApproveRecordsEvenOnDuplicateDelivery", func(t *testing.T) {
		store := &verdictStoreStub{consumption: fact, consumptionOK: true}
		recorder := &uhtRecorderStub{}
		handler := NewMilkPreparationVerificationHandler(store).WithUHTRecorder(recorder)
		event := eventbus.Event{ID: "event-1", Type: eventMilkPreparationVerdictApproved, TenantID: "tenant", OccurredAt: time.Now(), Payload: approvedPayload}
		if err := handler.HandleEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
		if err := handler.HandleEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
		if len(recorder.recorded) != 2 || recorder.recorded[0] != fact {
			t.Fatalf("recorded=%+v, want the fact forwarded on both deliveries", recorder.recorded)
		}
	})

	t.Run("NotCompletedRecordsNothing", func(t *testing.T) {
		store := &verdictStoreStub{consumptionOK: false}
		recorder := &uhtRecorderStub{}
		handler := NewMilkPreparationVerificationHandler(store).WithUHTRecorder(recorder)
		if err := handler.HandleEvent(context.Background(), eventbus.Event{ID: "event-2", Type: eventMilkPreparationVerdictApproved, TenantID: "tenant", OccurredAt: time.Now(), Payload: approvedPayload}); err != nil {
			t.Fatal(err)
		}
		if len(recorder.recorded) != 0 {
			t.Fatalf("recorded=%+v, want nothing for a not-completed row", recorder.recorded)
		}
	})

	t.Run("ReworkNeverTouchesTheRecorder", func(t *testing.T) {
		store := &verdictStoreStub{consumption: fact, consumptionOK: true}
		recorder := &uhtRecorderStub{}
		handler := NewMilkPreparationVerificationHandler(store).WithUHTRecorder(recorder)
		reworkPayload := []byte(`{"verified_by":"verifier","reason":"blurry","source":{"module":"milk_preparation","ref_type":"milk_preparation_completion","ref_id":"completion-1"}}`)
		if err := handler.HandleEvent(context.Background(), eventbus.Event{ID: "event-3", Type: eventMilkPreparationVerdictRework, TenantID: "tenant", OccurredAt: time.Now(), Payload: reworkPayload}); err != nil {
			t.Fatal(err)
		}
		if len(recorder.recorded) != 0 || store.consumptionReads != 0 {
			t.Fatalf("rework must not read or record consumption (recorded=%+v reads=%d)", recorder.recorded, store.consumptionReads)
		}
	})

	t.Run("RecorderFailureFailsTheHandlerForRedelivery", func(t *testing.T) {
		store := &verdictStoreStub{consumption: fact, consumptionOK: true}
		recorder := &uhtRecorderStub{fail: context.DeadlineExceeded}
		handler := NewMilkPreparationVerificationHandler(store).WithUHTRecorder(recorder)
		if err := handler.HandleEvent(context.Background(), eventbus.Event{ID: "event-4", Type: eventMilkPreparationVerdictApproved, TenantID: "tenant", OccurredAt: time.Now(), Payload: approvedPayload}); err == nil {
			t.Fatal("recorder failure must surface so the bus redelivers")
		}
	})

	t.Run("NilRecorderKeepsVerdictWorking", func(t *testing.T) {
		store := &verdictStoreStub{consumption: fact, consumptionOK: true}
		handler := NewMilkPreparationVerificationHandler(store)
		if err := handler.HandleEvent(context.Background(), eventbus.Event{ID: "event-5", Type: eventMilkPreparationVerdictApproved, TenantID: "tenant", OccurredAt: time.Now(), Payload: approvedPayload}); err != nil {
			t.Fatal(err)
		}
		if store.approved.CompletionID != "completion-1" {
			t.Fatalf("verdict must still apply without a recorder: %+v", store.approved)
		}
	})
}

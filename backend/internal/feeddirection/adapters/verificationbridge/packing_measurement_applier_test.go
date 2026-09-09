package verificationbridge

import (
	"context"
	"errors"
	"testing"

	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// THE VERIFIER IS WARNED, NOT TOLD (maintainer decision 2026-09-09). These pin the applier's four
// edges: a reading more than 500 g from the plan is refused BEFORE any write with a direction-only
// notice per flagged item; the same approve re-sent acknowledged lands and marks exactly the flagged
// readings confirmed; readings inside the tolerance (and items with no readable plan) land on the
// first press untouched; and the plan read is what gates it, so an unreadable plan checks nothing.

type recordingPackingStore struct {
	planned    map[string]float64
	plannedErr error
	recorded   []feeddirectionports.RecordPackingVerifiedQuantitiesParams
}

func (s *recordingPackingStore) RecordPackingVerifiedQuantities(_ context.Context, p feeddirectionports.RecordPackingVerifiedQuantitiesParams) error {
	s.recorded = append(s.recorded, p)
	return nil
}

func (s *recordingPackingStore) PackingVerifiedQuantitiesRecorded(context.Context, string, string) (bool, error) {
	return len(s.recorded) > 0, nil
}

func (s *recordingPackingStore) PackingPlannedQuantities(context.Context, string, string) (map[string]float64, error) {
	return s.planned, s.plannedErr
}

func packingApply(acknowledged bool, entries ...verificationdomain.MeasurementEntry) verificationapp.MeasurementApply {
	return verificationapp.MeasurementApply{
		TenantID:   "tenant-1",
		VerifierID: "verifier-1",
		Source:     verificationdomain.SourceRef{Module: "feed", RefType: "feed_packing_completion", RefID: "completion-1"},
		Entries:    entries,
		Fields: []verificationdomain.MeasurementField{
			{Key: "concentrate", Label: "Concentrate"},
			{Key: "hay", Label: "Hay"},
		},
		VarianceAcknowledged: acknowledged,
		IdempotencyKey:       "verdict-1:measurement",
	}
}

func TestPackingApplierRefusesAnOutOfToleranceReadingOnceWithDirectionOnly(t *testing.T) {
	store := &recordingPackingStore{planned: map[string]float64{"concentrate": 2, "hay": 1}}
	applier := NewPackingMeasurementApplier(store)

	err := applier.ApplyMeasurement(context.Background(), packingApply(false,
		verificationdomain.MeasurementEntry{Key: "concentrate", Value: 2.6}, // 600 g over
		verificationdomain.MeasurementEntry{Key: "hay", Value: 0.4},         // 600 g short
	))
	var confirm *verificationapp.MeasurementConfirmationRequired
	if !errors.As(err, &confirm) {
		t.Fatalf("want MeasurementConfirmationRequired, got %v", err)
	}
	if len(store.recorded) != 0 {
		t.Fatalf("a refusal must write nothing; recorded %d sets", len(store.recorded))
	}
	if confirm.Message != feeddirectiondomain.PackingEntryConfirmMessage {
		t.Errorf("headline = %q, want the producer's confirm sentence", confirm.Message)
	}
	if len(confirm.Fields) != 2 {
		t.Fatalf("fields = %+v, want both flagged entries", confirm.Fields)
	}
	if confirm.Fields[0].Key != "concentrate" || confirm.Fields[0].Code != feeddirectiondomain.PackingEntryAbovePlan {
		t.Errorf("first notice = %+v, want concentrate above_plan", confirm.Fields[0])
	}
	if confirm.Fields[1].Key != "hay" || confirm.Fields[1].Code != feeddirectiondomain.PackingEntryBelowPlan {
		t.Errorf("second notice = %+v, want hay below_plan", confirm.Fields[1])
	}
	for _, field := range confirm.Fields {
		// Blind entry survives the warning: no figure from the plan, no gap.
		for _, leak := range []string{"2 kg", "1 kg", "0.6", "600"} {
			if contains(field.Message, leak) {
				t.Errorf("notice %q leaks the plan or the gap (%q)", field.Message, leak)
			}
		}
	}
}

func TestPackingApplierRecordsAcknowledgedReadingsAndMarksOnlyTheFlaggedOnes(t *testing.T) {
	store := &recordingPackingStore{planned: map[string]float64{"concentrate": 2, "hay": 1}}
	applier := NewPackingMeasurementApplier(store)

	if err := applier.ApplyMeasurement(context.Background(), packingApply(true,
		verificationdomain.MeasurementEntry{Key: "concentrate", Value: 2.6}, // flagged, confirmed
		verificationdomain.MeasurementEntry{Key: "hay", Value: 1.2},         // inside tolerance
	)); err != nil {
		t.Fatalf("acknowledged approve must land: %v", err)
	}
	if len(store.recorded) != 1 {
		t.Fatalf("recorded sets = %d, want 1", len(store.recorded))
	}
	got := store.recorded[0]
	if got.CompletionID != "completion-1" || got.RecordedBy != "verifier-1" || got.IdempotencyKey != "verdict-1:measurement" {
		t.Errorf("params = %+v, want the item's own completion, the verifier, and the derived key", got)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries = %+v, want both readings", got.Entries)
	}
	conc, hay := got.Entries[0], got.Entries[1]
	if conc.FeedItemLabel != "Concentrate" || conc.EnteredKg != 2.6 || conc.PlannedKg == nil || *conc.PlannedKg != 2 || !conc.VarianceAcknowledged {
		t.Errorf("concentrate = %+v, want 2.6 against plan 2, acknowledged", conc)
	}
	if hay.FeedItemLabel != "Hay" || hay.EnteredKg != 1.2 || hay.PlannedKg == nil || *hay.PlannedKg != 1 || hay.VarianceAcknowledged {
		t.Errorf("hay = %+v, want 1.2 against plan 1, NOT acknowledged (it was never flagged)", hay)
	}
}

func TestPackingApplierLandsInToleranceReadingsOnTheFirstPress(t *testing.T) {
	store := &recordingPackingStore{planned: map[string]float64{"concentrate": 2}}
	applier := NewPackingMeasurementApplier(store)

	if err := applier.ApplyMeasurement(context.Background(), packingApply(false,
		verificationdomain.MeasurementEntry{Key: "concentrate", Value: 2.5}, // exactly the tolerance
		verificationdomain.MeasurementEntry{Key: "hay", Value: 9},           // no plan for hay: unchecked
	)); err != nil {
		t.Fatalf("first press inside tolerance must land: %v", err)
	}
	if len(store.recorded) != 1 {
		t.Fatalf("recorded sets = %d, want 1", len(store.recorded))
	}
	got := store.recorded[0].Entries
	if got[0].VarianceAcknowledged || got[1].VarianceAcknowledged {
		t.Errorf("nothing was flagged, so nothing is acknowledged: %+v", got)
	}
	if got[0].PlannedKg == nil || *got[0].PlannedKg != 2 {
		t.Errorf("concentrate keeps the plan it was checked against: %+v", got[0])
	}
	if got[1].PlannedKg != nil {
		t.Errorf("hay had no readable plan and must carry none, not zero: %+v", got[1])
	}
}

func TestPackingApplierChecksNothingWhenNoPlanIsReadable(t *testing.T) {
	store := &recordingPackingStore{planned: nil}
	applier := NewPackingMeasurementApplier(store)
	if err := applier.ApplyMeasurement(context.Background(), packingApply(false,
		verificationdomain.MeasurementEntry{Key: "concentrate", Value: 40},
	)); err != nil {
		t.Fatalf("a completion with no readable plan is the judge-the-video case: %v", err)
	}
	if len(store.recorded) != 1 {
		t.Fatalf("recorded sets = %d, want 1", len(store.recorded))
	}
}

func TestPackingApplierStopsTheApproveWhenThePlanReadFails(t *testing.T) {
	store := &recordingPackingStore{plannedErr: feeddirectionports.ErrPackingCompletionNotFound}
	applier := NewPackingMeasurementApplier(store)
	err := applier.ApplyMeasurement(context.Background(), packingApply(false,
		verificationdomain.MeasurementEntry{Key: "concentrate", Value: 2},
	))
	if !errors.Is(err, feeddirectionports.ErrPackingCompletionNotFound) {
		t.Fatalf("want the store's error to stop the approve, got %v", err)
	}
	if len(store.recorded) != 0 {
		t.Fatal("nothing may be written when the completion cannot be read")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

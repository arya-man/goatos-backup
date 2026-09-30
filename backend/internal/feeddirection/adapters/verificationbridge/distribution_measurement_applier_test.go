package verificationbridge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// THE DISTRIBUTION VERIFIER RECORDS THE TOTAL FEED (maintainer decision 2026-09-28). These pin the
// applier's edges: a total more than 5% from the planned pen-session total is refused BEFORE any
// write with a direction-only notice; the same approve re-sent acknowledged lands and is marked
// confirmed; a total inside the band lands on the first press; no readable plan checks nothing; and
// a slipped digit is a field problem, never a server error.

type recordingDistributionStore struct {
	planned    float64
	hasPlan    bool
	plannedErr error
	recorded   []feeddirectionports.RecordDistributionVerifiedFeedParams
}

func (s *recordingDistributionStore) DistributionPlannedFeedKg(context.Context, string, string) (float64, bool, error) {
	return s.planned, s.hasPlan, s.plannedErr
}

func (s *recordingDistributionStore) RecordDistributionVerifiedFeed(_ context.Context, p feeddirectionports.RecordDistributionVerifiedFeedParams) error {
	s.recorded = append(s.recorded, p)
	return nil
}

func (s *recordingDistributionStore) DistributionVerifiedFeedRecorded(context.Context, string, string) (bool, error) {
	return len(s.recorded) > 0, nil
}

func distributionApply(acknowledged bool, totalKg float64) verificationapp.MeasurementApply {
	return verificationapp.MeasurementApply{
		TenantID:             "tenant-1",
		VerifierID:           "verifier-1",
		Source:               verificationdomain.SourceRef{Module: "feed", RefType: feeddirectiondomain.VerificationRefTypeFeed, RefID: "completion-1"},
		Entries:              []verificationdomain.MeasurementEntry{{Key: feeddirectiondomain.DistributionTotalFeedKey, Value: totalKg}},
		Fields:               distributionMeasurementFields(),
		VarianceAcknowledged: acknowledged,
		IdempotencyKey:       "verdict-1:measurement",
	}
}

func TestDistributionApplierRefusesATotalMoreThanFivePercentOffWithDirectionOnly(t *testing.T) {
	store := &recordingDistributionStore{planned: 80, hasPlan: true}
	applier := NewDistributionMeasurementApplier(store)

	err := applier.ApplyMeasurement(context.Background(), distributionApply(false, 90)) // 12.5% over
	var confirm *verificationapp.MeasurementConfirmationRequired
	if !errors.As(err, &confirm) {
		t.Fatalf("want MeasurementConfirmationRequired, got %v", err)
	}
	if len(store.recorded) != 0 {
		t.Fatalf("a refusal must write nothing; recorded %d", len(store.recorded))
	}
	if confirm.Message != feeddirectiondomain.DistributionEntryConfirmMessage {
		t.Errorf("headline = %q, want the distribution confirm sentence", confirm.Message)
	}
	if len(confirm.Fields) != 1 || confirm.Fields[0].Key != feeddirectiondomain.DistributionTotalFeedKey ||
		confirm.Fields[0].Code != feeddirectiondomain.DistributionEntryAbovePlan {
		t.Fatalf("notice = %+v, want total_feed total_above_plan", confirm.Fields)
	}
	// BLIND: nothing she is shown may carry the plan or the gap.
	for _, text := range []string{confirm.Message, confirm.Fields[0].Message} {
		if strings.Contains(text, "80") || strings.Contains(text, "10") || strings.Contains(text, "kg") {
			t.Errorf("verifier-facing text %q leaks a figure", text)
		}
	}

	below := applier.ApplyMeasurement(context.Background(), distributionApply(false, 70)) // 12.5% under
	if !errors.As(below, &confirm) || confirm.Fields[0].Code != feeddirectiondomain.DistributionEntryBelowPlan {
		t.Fatalf("under-plan total: want total_below_plan, got %v", below)
	}
}

func TestDistributionApplierRecordsAnAcknowledgedTotalAsConfirmed(t *testing.T) {
	store := &recordingDistributionStore{planned: 80, hasPlan: true}
	applier := NewDistributionMeasurementApplier(store)

	if err := applier.ApplyMeasurement(context.Background(), distributionApply(true, 90)); err != nil {
		t.Fatalf("acknowledged approve: %v", err)
	}
	if len(store.recorded) != 1 {
		t.Fatalf("recorded %d, want 1", len(store.recorded))
	}
	got := store.recorded[0]
	if got.CompletionID != "completion-1" || got.EnteredKg != 90 || !got.VarianceAcknowledged ||
		got.PlannedKg == nil || *got.PlannedKg != 80 || got.RecordedBy != "verifier-1" {
		t.Fatalf("recorded %+v, want completion-1 90 kg confirmed against plan 80 by verifier-1", got)
	}
}

func TestDistributionApplierRecordsATotalInsideTheBandOnTheFirstPress(t *testing.T) {
	store := &recordingDistributionStore{planned: 80, hasPlan: true}
	applier := NewDistributionMeasurementApplier(store)

	if err := applier.ApplyMeasurement(context.Background(), distributionApply(false, 83.5)); err != nil { // 4.4% over
		t.Fatalf("in-band approve: %v", err)
	}
	if len(store.recorded) != 1 || store.recorded[0].VarianceAcknowledged {
		t.Fatalf("recorded %+v, want one unconfirmed reading", store.recorded)
	}
	// Even when she ticked the box, a reading inside the band is never marked as questioned.
	if err := applier.ApplyMeasurement(context.Background(), distributionApply(true, 80)); err != nil {
		t.Fatalf("acknowledged in-band approve: %v", err)
	}
	if store.recorded[1].VarianceAcknowledged {
		t.Fatal("an in-band reading must not be marked as a confirmed variance")
	}
}

func TestDistributionApplierWithNoReadablePlanChecksNothing(t *testing.T) {
	store := &recordingDistributionStore{hasPlan: false}
	applier := NewDistributionMeasurementApplier(store)

	if err := applier.ApplyMeasurement(context.Background(), distributionApply(false, 450)); err != nil {
		t.Fatalf("no-plan approve: %v", err)
	}
	if len(store.recorded) != 1 || store.recorded[0].PlannedKg != nil {
		t.Fatalf("recorded %+v, want the reading stored with no plan", store.recorded)
	}
}

func TestDistributionApplierRefusesASlippedDigitAsAFieldProblem(t *testing.T) {
	store := &recordingDistributionStore{planned: 80, hasPlan: true}
	applier := NewDistributionMeasurementApplier(store)

	err := applier.ApplyMeasurement(context.Background(), distributionApply(false, 800000))
	var verr *verificationapp.Error
	if !errors.As(err, &verr) || verr.HTTPStatus != 422 {
		t.Fatalf("want a 422 field error, got %v", err)
	}
	if len(store.recorded) != 0 {
		t.Fatal("an out-of-range total must write nothing")
	}
}

func TestDistributionApplierStopsWhenThePlanCannotBeRead(t *testing.T) {
	store := &recordingDistributionStore{plannedErr: feeddirectionports.ErrDistributionCompletionNotFound}
	applier := NewDistributionMeasurementApplier(store)

	err := applier.ApplyMeasurement(context.Background(), distributionApply(false, 80))
	if !errors.Is(err, feeddirectionports.ErrDistributionCompletionNotFound) {
		t.Fatalf("want ErrDistributionCompletionNotFound, got %v", err)
	}
	if len(store.recorded) != 0 {
		t.Fatal("an unknown completion must write nothing")
	}
}

// Every NEW distribution item carries exactly the one blind box -- names only, no planned figure.
func TestDistributionVerificationCarriesOneBlindTotalFeedBox(t *testing.T) {
	recorder := &recordingVerificationCreator{}
	if err := New(recorder).EnqueueFeedDistributionVerification(context.Background(), feeddirectionapp.FeedDistributionVerificationEnqueueRequest{
		TenantID:             "tenant-1",
		CompletionID:         "completion-1",
		ShedID:               "shed-1",
		SessionNo:            1,
		TargetDate:           time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC),
		FeedWeightProofRef:   "weight-proof",
		DistributionProofRef: "distribution-proof",
		WaterProofRef:        "water-proof",
		IdempotencyKey:       "feed-distribution-verification:completion-1:1",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	fields := recorder.items[0].MeasurementFields
	if len(fields) != 1 || fields[0].Key != feeddirectiondomain.DistributionTotalFeedKey ||
		fields[0].Label != feeddirectiondomain.DistributionTotalFeedLabel {
		t.Fatalf("MeasurementFields = %+v, want the one total_feed box", fields)
	}
}

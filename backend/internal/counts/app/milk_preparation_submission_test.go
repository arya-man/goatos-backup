package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

type milkPreparationStoreStub struct {
	submitted domain.MilkPreparationSubmission
}

func (s *milkPreparationStoreStub) SubmitMilkPreparation(_ context.Context, in domain.MilkPreparationSubmission) (domain.MilkPreparationSubmissionResult, error) {
	s.submitted = in
	return domain.MilkPreparationSubmissionResult{CompletionID: "completion-1", Status: domain.MilkPreparationVerificationPending, AttemptNo: 1, NeedsEnqueue: true}, nil
}
func (*milkPreparationStoreStub) ApplyVerifiedMilkPreparation(context.Context, domain.MilkPreparationVerdictCommand) (bool, error) {
	return false, nil
}
func (*milkPreparationStoreStub) BounceMilkPreparationForRework(context.Context, domain.MilkPreparationVerdictCommand) (bool, error) {
	return false, nil
}
func (*milkPreparationStoreStub) VerifiedUHTConsumption(context.Context, string, string) (domain.MilkPreparationUHTConsumption, bool, error) {
	return domain.MilkPreparationUHTConsumption{}, false, nil
}

type milkPreparationProofValidatorStub struct {
	steps []domain.MilkPreparationStepProof
}

func (v *milkPreparationProofValidatorStub) ValidateMilkPreparationProofs(_ context.Context, _, _ string, steps []domain.MilkPreparationStepProof) error {
	v.steps = steps
	return nil
}

type milkPreparationEnqueuerStub struct {
	request MilkPreparationVerificationEnqueueRequest
}

func (e *milkPreparationEnqueuerStub) EnqueueMilkPreparationVerification(_ context.Context, in MilkPreparationVerificationEnqueueRequest) error {
	e.request = in
	return nil
}

func TestSubmitMilkPreparationEnqueuesAllFiveStepVideosTogether(t *testing.T) {
	store := &milkPreparationStoreStub{}
	validator := &milkPreparationProofValidatorStub{}
	enqueuer := &milkPreparationEnqueuerStub{}
	service := NewHerdRegisterService(&fakeRepo{})
	service.milkPreparationStore = store
	service.WithMilkPreparationProofValidator(validator).WithMilkPreparationVerificationEnqueuer(enqueuer)
	prep := time.Date(2026, 7, 29, 0, 0, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	proofs := domain.MilkPreparationProofs{
		GoatMilkQuantityProofRef: "goat", BoilingTemperatureProofRef: "boil", CooledTemperatureProofRef: "cool",
		UHTMilkQuantityProofRef: "uht", CitricAcidMixingProofRef: "citric",
	}
	_, err := service.SubmitMilkPreparation(context.Background(), domain.MilkPreparationSubmission{
		TenantID: "tenant", ParkID: "park", PreparationDate: prep, GoatMilkUsed: true,
		Answers:     domain.MilkPreparationAnswers{MorningMilkCollectedLitres: 4, EveningMilkCollectedLitres: 3, GoatMilkQuantityLitres: 2, BoilingTemperatureC: 100, CooledTemperatureC: 38, UHTMilkQuantityLitres: 8, CitricAcidGrams: 44},
		Proofs:      proofs,
		SubmittedBy: "operator", IdempotencyKey: "attempt-key",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	want := []string{"goat", "boil", "cool", "uht", "citric"}
	got := make([]string, 0, len(enqueuer.request.StepProofs))
	for _, step := range enqueuer.request.StepProofs {
		got = append(got, step.ProofRef)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("refs=%v want=%v", got, want)
	}
	if store.submitted.FeedingDate.Format("2006-01-02") != "2026-07-30" {
		t.Fatalf("feeding=%s", store.submitted.FeedingDate)
	}
	if enqueuer.request.ParkID != "park" {
		t.Fatalf("verification farm=%q", enqueuer.request.ParkID)
	}
}

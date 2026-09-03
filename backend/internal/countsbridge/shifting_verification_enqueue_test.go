package countsbridge

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type capturingVerificationCreator struct{ item verificationdomain.CreateItem }

func (c *capturingVerificationCreator) CreateItem(_ context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error) {
	c.item = in
	return verificationdomain.CreateItemResult{}, nil
}

func TestHighPriorityShiftingEnqueuesThreeVideosInOneVerificationItem(t *testing.T) {
	capture := &capturingVerificationCreator{}
	bridge := NewShiftingVerificationEnqueuer(capture)
	want := []string{"proof-shifting", "proof-packing", "proof-feeding"}
	err := bridge.EnqueueShiftingMoveVerification(context.Background(), countsapp.ShiftingVerificationEnqueueRequest{
		TenantID: "tenant", ShiftingEventID: "event", OperatorID: "operator",
		ParkID: "park", ShedID: "shed", MediaRefs: want, SubjectLabel: "one animal",
		CapturedAt: time.Now(), IdempotencyKey: "one-item",
	})
	if err != nil {
		t.Fatalf("enqueue shifting verification: %v", err)
	}
	if !reflect.DeepEqual(capture.item.MediaRefs, want) {
		t.Fatalf("media_refs=%v, want all three proofs together %v", capture.item.MediaRefs, want)
	}
	if capture.item.Source.RefType != "shifting_event" || capture.item.Source.RefID != "event" {
		t.Fatalf("source=%+v, want one shifting_event verification item", capture.item.Source)
	}
}

func TestMilkPreparationEnqueuesFiveVideosInStepOrderOnOneItem(t *testing.T) {
	capture := &capturingVerificationCreator{}
	bridge := NewMilkPreparationVerificationEnqueuer(capture)
	want := []string{"goat", "boil", "cool", "uht", "citric"}
	steps := make([]countsdomain.MilkPreparationStepProof, 0, len(want))
	for i, ref := range want {
		steps = append(steps, countsdomain.MilkPreparationStepProof{StepCode: fmt.Sprintf("step-%d", i), ProofRef: ref})
	}
	err := bridge.EnqueueMilkPreparationVerification(context.Background(), countsapp.MilkPreparationVerificationEnqueueRequest{
		TenantID: "tenant", CompletionID: "completion", ParkID: "park", OperatorID: "operator",
		AttemptNo: 1, StepProofs: steps, CapturedAt: time.Now(), IdempotencyKey: "milk-attempt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(capture.item.MediaRefs, want) {
		t.Fatalf("media_refs=%v want=%v", capture.item.MediaRefs, want)
	}
	if capture.item.Source.RefType != "milk_preparation_completion" {
		t.Fatalf("source=%+v", capture.item.Source)
	}
	if capture.item.ParkID == nil || *capture.item.ParkID != "park" || capture.item.ShedID != nil {
		t.Fatalf("farm-only scope park=%v shed=%v", capture.item.ParkID, capture.item.ShedID)
	}
}

// The verifier judges each quantity video against the number the operator ENTERED, so the item
// must carry the milk litres and citric acid grams as context rows. Mutation-tested when written:
// dropping the ContextRows attach in the bridge turns this red.
func TestMilkPreparationItemCarriesEnteredMilkAndCitricAcid(t *testing.T) {
	capture := &capturingVerificationCreator{}
	bridge := NewMilkPreparationVerificationEnqueuer(capture)
	err := bridge.EnqueueMilkPreparationVerification(context.Background(), countsapp.MilkPreparationVerificationEnqueueRequest{
		TenantID: "tenant", CompletionID: "completion", ParkID: "park", OperatorID: "operator",
		AttemptNo: 1, GoatMilkUsed: true,
		Answers: countsdomain.MilkPreparationAnswers{
			MorningMilkCollectedLitres: 6, EveningMilkCollectedLitres: 4,
			GoatMilkQuantityLitres: 8, BoilingTemperatureC: 95, CooledTemperatureC: 40,
			UHTMilkQuantityLitres: 12.5, CitricAcidGrams: 112.75,
		},
		CapturedAt: time.Now(), IdempotencyKey: "milk-context",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []verificationdomain.ContextRow{
		{Label: "Milk used", Value: "20.5 L (goat 8 L + UHT 12.5 L)"},
		{Label: "Citric acid", Value: "112.75 g"},
	}
	if !reflect.DeepEqual(capture.item.ContextRows, want) {
		t.Fatalf("context_rows=%v want=%v", capture.item.ContextRows, want)
	}
}

// A legacy client that sent no answers must state nothing rather than claim zero litres.
func TestMilkPreparationItemWithoutAnswersAttachesNoContext(t *testing.T) {
	capture := &capturingVerificationCreator{}
	bridge := NewMilkPreparationVerificationEnqueuer(capture)
	err := bridge.EnqueueMilkPreparationVerification(context.Background(), countsapp.MilkPreparationVerificationEnqueueRequest{
		TenantID: "tenant", CompletionID: "completion", ParkID: "park", OperatorID: "operator",
		AttemptNo: 1, CapturedAt: time.Now(), IdempotencyKey: "milk-no-answers",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(capture.item.ContextRows) != 0 {
		t.Fatalf("context_rows=%v, want none for an answer-less submission", capture.item.ContextRows)
	}
}

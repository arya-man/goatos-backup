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

package countsbridge

import (
	"context"
	"reflect"
	"testing"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
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

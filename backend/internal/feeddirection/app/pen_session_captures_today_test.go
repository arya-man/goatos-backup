package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// DEPLOY-DAY PARITY: today the distribution captures read offers every capture on file for the
// pen-session, including one a verifier sent back; hiding sent-back captures is recommended in
// docs/decisions/feed-sop.md but not enabled.
type capturesValidator struct {
	fakeProofValidator
	slots []ports.CapturedProofSlot
}

func (c *capturesValidator) ListPenSessionCaptures(context.Context, ports.PenSessionCaptureQuery) ([]ports.CapturedProofSlot, error) {
	return c.slots, nil
}

func TestCapturesReadStillOffersSentBackCapturesAsToday(t *testing.T) {
	now := time.Now()
	validator := &capturesValidator{slots: []ports.CapturedProofSlot{
		{FieldKey: domain.SlotFeedWeightPhoto, ProofID: "rejected-photo", CapturedAt: now},
		{FieldKey: domain.SlotFeedVideo, ProofID: "rejected-video", CapturedAt: now},
		{FieldKey: domain.SlotWaterVideo, ProofID: "new-water", CapturedAt: now},
	}}
	store := &fakeDistributionStore{}
	store.statuses = []ports.SessionCompletionStatus{{ShedID: shedA, PartitionLabel: "1", SessionNo: 1, Workflow: domain.WorkflowNormal, Status: domain.DistributionStatusRework}}
	res, err := NewService(nil, nil).WithDistributionStore(store).WithProofValidator(validator).ListPenSessionCaptures(context.Background(), PenSessionCapturesInput{
		TenantID: testTenant, ParkID: testPark, ShedID: shedA, PartitionLabel: "1", SessionNo: 1, TargetDate: targetDate(), Workflow: domain.WorkflowNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Slots) != 3 || res.SessionStatus != domain.DistributionStatusRework {
		t.Fatalf("slots = %+v status %q, want all three captures offered as today", res.Slots, res.SessionStatus)
	}
}

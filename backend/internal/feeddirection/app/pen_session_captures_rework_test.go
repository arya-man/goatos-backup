package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// After a verifier sends a distribution pen-session back, the captures read still found the
// rejected uploads in the proof register (same pen-session key) and every phone re-adopted them as
// "Proof ready" -- so the re-submit named the very captures the verifier rejected (2026-09-17 E2E).
// The read now leaves out any capture the session was sent back with; teammates see empty slots and
// re-shoot.

type capturesValidator struct {
	fakeProofValidator
	slots []ports.CapturedProofSlot
}

func (c *capturesValidator) ListPenSessionCaptures(context.Context, ports.PenSessionCaptureQuery) ([]ports.CapturedProofSlot, error) {
	return c.slots, nil
}

type sentBackDistributionStore struct {
	fakeDistributionStore
	sentBack authored.ProofRefs
}

func (s *sentBackDistributionStore) SentBackDistributionProofs(context.Context, ports.PenSessionCaptureQuery) (authored.ProofRefs, error) {
	return s.sentBack, nil
}

func TestCapturesReadLeavesOutTheCapturesTheSessionWasSentBackWith(t *testing.T) {
	now := time.Now()
	validator := &capturesValidator{slots: []ports.CapturedProofSlot{
		{FieldKey: domain.SlotFeedWeightPhoto, ProofID: "rejected-photo", CapturedAt: now},
		{FieldKey: domain.SlotFeedVideo, ProofID: "rejected-video", CapturedAt: now},
		{FieldKey: domain.SlotWaterVideo, ProofID: "new-water", CapturedAt: now},
	}}
	store := &sentBackDistributionStore{sentBack: authored.ProofRefs{
		domain.SlotFeedWeightPhoto: "rejected-photo", domain.SlotFeedVideo: "rejected-video", domain.SlotWaterVideo: "rejected-water",
	}}
	store.statuses = []ports.SessionCompletionStatus{{ShedID: shedA, PartitionLabel: "1", SessionNo: 1, Workflow: domain.WorkflowNormal, Status: domain.DistributionStatusRework}}
	svc := NewService(nil, nil).WithDistributionStore(store).WithProofValidator(validator)
	res, err := svc.ListPenSessionCaptures(context.Background(), PenSessionCapturesInput{
		TenantID: testTenant, ParkID: testPark, ShedID: shedA, PartitionLabel: "1", SessionNo: 1, TargetDate: targetDate(), Workflow: domain.WorkflowNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Slots) != 1 || res.Slots[0].ProofID != "new-water" {
		t.Fatalf("slots = %+v, want only the capture taken after the session was sent back", res.Slots)
	}
}

package app

// A weighing verification item MUST carry the campaign's park.
//
// The generic verification notification consumer treats a blank park as non-routable and produces
// NO push at all, so an item enqueued without one is a silently dropped "proof waiting" alert. The
// park is a property of the campaign, so the service resolves it from the campaign the observation
// was written against.
//
// projection-review: park resolution is a 1:1 lookup, not an aggregate. Producer key =
// {tenant_id, campaign_id} (the PK of weighing_campaigns, exactly one row); consumer match key =
// {tenant_id, campaign_id}; multiplicity 1:1, so no row can fan out and no count is derived.

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

func TestRecordShedObservationEnqueuesVerificationWithCampaignPark(t *testing.T) {
	repo := &shedObservationRepo{}
	enqueuer := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enqueuer)
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	if _, err := service.RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
		CampaignID:       "00000000-0000-4000-8000-000000000501",
		CampaignShedID:   "00000000-0000-4000-8000-000000000801",
		WeightKg:         250,
		AnimalCount:      10,
		ProofArtifactID:  proofOne,
		ProofArtifactIDs: []string{proofOne, proofTwo},
		IdempotencyKey:   "shed-lumpsum-park-1",
	}); err != nil {
		t.Fatalf("record shed observation: %v", err)
	}
	if enqueuer.received.ParkID != testPark {
		t.Fatalf("enqueued verification park=%q, want %q", enqueuer.received.ParkID, testPark)
	}
}

func TestRecordAnimalObservationEnqueuesVerificationWithCampaignPark(t *testing.T) {
	repo := &animalObservationRepo{}
	enqueuer := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enqueuer)
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	if _, err := service.RecordAnimalObservation(context.Background(), operator, domain.RecordAnimalObservation{
		CampaignID:        "00000000-0000-4000-8000-000000000501",
		CampaignShedID:    "00000000-0000-4000-8000-000000000801",
		ScannedIdentifier: "RFID-FREEFLOW-PARK-1",
		WeightKg:          12.3,
		ProofArtifactID:   proofOne,
		IdempotencyKey:    "scan-freeflow-park-1",
	}); err != nil {
		t.Fatalf("record animal observation: %v", err)
	}
	if enqueuer.received.ParkID != testPark {
		t.Fatalf("enqueued verification park=%q, want %q", enqueuer.received.ParkID, testPark)
	}
}
